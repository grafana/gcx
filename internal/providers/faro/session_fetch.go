package faro

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"time"

	"github.com/grafana/gcx/internal/query/loki"
	"github.com/grafana/gcx/internal/query/pinot"
	querysql "github.com/grafana/gcx/internal/query/sql"
	"golang.org/x/sync/errgroup"
)

type pinotQuerier interface {
	Query(ctx context.Context, datasourceUID string, req pinot.QueryRequest) (*querysql.QueryResponse, error)
}

type lokiQuerier interface {
	Query(ctx context.Context, datasourceUID string, req loki.QueryRequest) (*loki.QueryResponse, error)
}

const lokiQueryDirectionForward = "forward"

// Loki session queries go through Grafana's query API with no HTTP client
// timeout. Bound each Loki POST so a stuck scan exits; do not share one
// deadline across metadata + every kind page. Pinot is not wrapped.
var sessionLokiQueryTimeout = 60 * time.Second

func wrapLokiSessionQueryErr(sessionID string, err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("no telemetry for session %s in this time range (Loki query timed out after %s; try a Pinot datasource UID (-d) or a narrower --from/--to)", sessionID, sessionLokiQueryTimeout)
	}
	return err
}

type pinotSessionResult struct {
	eventsMeta *querysql.QueryResponse
	userMeta   *querysql.QueryResponse
	journey    *querysql.QueryResponse
}

func (r *pinotSessionResult) dump() string {
	if r == nil {
		return ""
	}
	return formatSessionDump(
		joinBlocks(formatPinotTSV(r.eventsMeta), formatPinotTSV(r.userMeta)),
		formatPinotTSV(r.journey),
	)
}

func (r *pinotSessionResult) writeTables(w io.Writer) error {
	if _, err := fmt.Fprintln(w, sessionDumpMetadataHeader); err != nil {
		return err
	}
	if err := writePinotTables(w, r.eventsMeta, r.userMeta); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "\n%s\n", sessionDumpEventsHeader); err != nil {
		return err
	}
	_, err := io.WriteString(w, formatPinotTSV(r.journey))
	return err
}

type lokiSessionResult struct {
	metadata string
	events   *loki.QueryResponse
}

func (r *lokiSessionResult) dump() string {
	if r == nil {
		return ""
	}
	return formatSessionDump(r.metadata, formatLokiLines(r.events))
}

func (r *lokiSessionResult) writeTables(w io.Writer) error {
	_, err := io.WriteString(w, r.dump())
	return err
}

func fetchPinotSession(ctx context.Context, client pinotQuerier, uid string, p sessionQueryParams, start, end time.Time) (*pinotSessionResult, error) {
	req := func(sql string) pinot.QueryRequest {
		return pinot.QueryRequest{RawSQL: sql, Start: start, End: end}
	}

	eventsSQL, err := pinotEventsMetadataQuery(p)
	if err != nil {
		return nil, err
	}
	userSQL, err := pinotUserMetadataQuery(p)
	if err != nil {
		return nil, err
	}

	g, gctx := errgroup.WithContext(ctx)
	var eventsMeta, userMeta *querysql.QueryResponse
	g.Go(func() error {
		var qerr error
		eventsMeta, qerr = client.Query(gctx, uid, req(eventsSQL))
		if qerr != nil {
			return fmt.Errorf("pinot events metadata query failed: %w", qerr)
		}
		return nil
	})
	g.Go(func() error {
		var qerr error
		userMeta, qerr = client.Query(gctx, uid, req(userSQL))
		if qerr != nil {
			return fmt.Errorf("pinot user metadata query failed: %w", qerr)
		}
		return nil
	})
	if err := g.Wait(); err != nil {
		return nil, err
	}

	if p.AppType == "" {
		// Need web vs mobile before the journey UNION (mobile drops
		// app_memory / app_cpu_usage). App GET has no type; the
		// measurements row we just fetched already has sdkName + osName.
		p.AppType = inferAppType(pinotCell(userMeta, "sdk_name"), pinotCell(userMeta, "os_name"))
	}

	journeySQL, err := pinotJourneyQuery(p)
	if err != nil {
		return nil, err
	}
	journey, err := client.Query(ctx, uid, req(journeySQL))
	if err != nil {
		return nil, fmt.Errorf("pinot events query failed: %w", err)
	}
	if !pinotResponseHasValues(eventsMeta) && !pinotResponseHasValues(userMeta) && !pinotResponseHasValues(journey) {
		return nil, fmt.Errorf("no telemetry for session %s in this time range", p.SessionID)
	}

	return &pinotSessionResult{eventsMeta: eventsMeta, userMeta: userMeta, journey: journey}, nil
}

func fetchLokiSession(ctx context.Context, client lokiQuerier, uid string, p sessionQueryParams, start, end time.Time) (*lokiSessionResult, error) {
	g, gctx := errgroup.WithContext(ctx)
	var metaResp, replayResp *loki.QueryResponse
	g.Go(func() error {
		var err error
		metaResp, err = queryLoki(gctx, client, uid, loki.QueryRequest{
			Query:     lokiMetadataQuery(p),
			Start:     start,
			End:       end,
			Limit:     1,
			Direction: lokiQueryDirectionForward,
		})
		if err != nil {
			return fmt.Errorf("loki metadata query failed: %w", err)
		}
		return nil
	})
	g.Go(func() error {
		var err error
		replayResp, err = queryLoki(gctx, client, uid, loki.QueryRequest{
			Query:     lokiReplayStartQuery(p),
			Start:     start,
			End:       end,
			Limit:     1,
			Direction: lokiQueryDirectionForward,
		})
		if err != nil {
			return fmt.Errorf("loki replay-start query failed: %w", err)
		}
		return nil
	})
	if err := g.Wait(); err != nil {
		return nil, wrapLokiSessionQueryErr(p.SessionID, err)
	}

	metaLine := firstLokiLine(metaResp)
	if p.AppType == "" {
		p.AppType = inferAppType(logfmtValue(metaLine, "sdk_name"), logfmtValue(metaLine, "os_name"))
	}

	eventsResp, err := fetchLokiEventsByKind(ctx, client, uid, p, start, end)
	if err != nil {
		return nil, wrapLokiSessionQueryErr(p.SessionID, err)
	}
	metadata := formatLokiMetadata(metaResp, replayResp)
	if metadata == "" && lokiEntryCount(eventsResp) == 0 {
		return nil, fmt.Errorf("no telemetry for session %s in this time range", p.SessionID)
	}

	return &lokiSessionResult{
		metadata: metadata,
		events:   eventsResp,
	}, nil
}

func queryLoki(ctx context.Context, client lokiQuerier, uid string, req loki.QueryRequest) (*loki.QueryResponse, error) {
	qctx, cancel := context.WithTimeout(ctx, sessionLokiQueryTimeout)
	defer cancel()
	return client.Query(qctx, uid, req)
}

func fetchLokiEventsByKind(ctx context.Context, client lokiQuerier, uid string, p sessionQueryParams, start, end time.Time) (*loki.QueryResponse, error) {
	g, gctx := errgroup.WithContext(ctx)
	results := make([]*loki.QueryResponse, len(lokiSessionEventKinds))
	for i, kind := range lokiSessionEventKinds {
		i, kind := i, kind
		g.Go(func() error {
			resp, err := fetchLokiEventPages(gctx, client, uid, lokiEventsQueryForKind(p, kind), start, end)
			if err != nil {
				return fmt.Errorf("loki %s query failed: %w", kind, err)
			}
			results[i] = resp
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return nil, err
	}
	merged := &loki.QueryResponse{Data: loki.QueryResultData{ResultType: "streams"}}
	for _, resp := range results {
		appendLokiEvents(merged, resp)
	}
	return merged, nil
}

func fetchLokiEventPages(ctx context.Context, client lokiQuerier, uid, query string, start, end time.Time) (*loki.QueryResponse, error) {
	merged := &loki.QueryResponse{Data: loki.QueryResultData{ResultType: "streams"}}
	cursor := end
	for cursor.After(start) {
		resp, err := queryLoki(ctx, client, uid, loki.QueryRequest{
			Query: query,
			Start: start,
			End:   cursor,
			Limit: lokiEventsPageSize,
		})
		if err != nil {
			return nil, fmt.Errorf("loki events query failed: %w", err)
		}
		n := lokiEntryCount(resp)
		appendLokiEvents(merged, resp)
		if n < lokiEventsPageSize {
			break
		}
		earliest, ok := minLokiTime(resp)
		if !ok {
			break
		}
		next := earliest.Add(-time.Millisecond)
		if !next.Before(cursor) {
			break
		}
		cursor = next
	}
	return merged, nil
}

func appendLokiEvents(dst, src *loki.QueryResponse) {
	if src == nil {
		return
	}
	dst.Data.Result = append(dst.Data.Result, src.Data.Result...)
}

func minLokiTime(resp *loki.QueryResponse) (time.Time, bool) {
	var earliest time.Time
	ok := false
	for _, stream := range resp.Data.Result {
		for _, entry := range stream.Values {
			ts, parsed := parseLokiUnixNano(entry.Timestamp)
			if !parsed {
				continue
			}
			if !ok || ts.Before(earliest) {
				earliest = ts
				ok = true
			}
		}
	}
	return earliest, ok
}

func parseLokiUnixNano(s string) (time.Time, bool) {
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return time.Time{}, false
	}
	return time.Unix(0, n), true
}

func lokiEntryCount(resp *loki.QueryResponse) int {
	if resp == nil {
		return 0
	}
	n := 0
	for _, stream := range resp.Data.Result {
		n += len(stream.Values)
	}
	return n
}
