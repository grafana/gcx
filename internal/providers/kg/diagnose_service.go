package kg

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/grafana/gcx/internal/format"
	"github.com/grafana/gcx/internal/query/prometheus"
	"github.com/grafana/gcx/internal/style"
	"golang.org/x/sync/errgroup"
)

// ---------------------------------------------------------------------------
// Service diagnosis result types
// ---------------------------------------------------------------------------

// ServiceDiagnoseResult is the full output of the diagnose service command.
type ServiceDiagnoseResult struct {
	ServiceName string        `json:"serviceName"`
	Env         string        `json:"env,omitempty"`
	Namespace   string        `json:"namespace,omitempty"`
	Entity      *EntityInfo   `json:"entity,omitempty"`
	Edges       []EdgeInfo    `json:"edges,omitempty"`
	Checks      []CheckResult `json:"checks"`
	Diagnosis   []string      `json:"diagnosis,omitempty"`
	NextSteps   []string      `json:"nextSteps,omitempty"`
	Summary     struct {
		Total  int `json:"total"`
		Passed int `json:"passed"`
		Failed int `json:"failed"`
		Warned int `json:"warned"`
	} `json:"summary"`
}

// EntityInfo holds the discovered entity properties.
type EntityInfo struct {
	Type       string            `json:"type"`
	Name       string            `json:"name"`
	Env        string            `json:"env,omitempty"`
	Namespace  string            `json:"namespace,omitempty"`
	Source     string            `json:"source,omitempty"`
	Properties map[string]string `json:"properties,omitempty"`
}

// EdgeInfo describes a single relationship for the service.
type EdgeInfo struct {
	Direction string `json:"direction"` // "outgoing" or "incoming"
	Type      string `json:"type"`      // e.g. "CALLS"
	PeerName  string `json:"peerName"`
	PeerType  string `json:"peerType"`
	PeerEnv   string `json:"peerEnv,omitempty"`
}

func (r *ServiceDiagnoseResult) computeSummary() {
	r.Summary.Total = len(r.Checks)
	for _, c := range r.Checks {
		switch c.Status {
		case CheckPass:
			r.Summary.Passed++
		case CheckFail:
			r.Summary.Failed++
		case CheckWarn:
			r.Summary.Warned++
		}
	}
}

// ---------------------------------------------------------------------------
// Service diagnosis runner
// ---------------------------------------------------------------------------

func runServiceDiagnose(ctx context.Context, client *Client, serviceName string, scope *scopeFlags, promClient *prometheus.Client, datasourceUID string) ServiceDiagnoseResult {
	result := ServiceDiagnoseResult{
		ServiceName: serviceName,
		Env:         scope.env,
		Namespace:   scope.namespace,
	}

	now := time.Now()
	startMs := now.Add(-1 * time.Hour).UnixMilli()
	endMs := now.UnixMilli()

	// Step 1: Find the service via Cypher (tolerant of missing scope).
	cypherQuery := fmt.Sprintf(`MATCH (s:Service {name: "%s"})-[r]-(other) RETURN s, r, other`, serviceName)
	cypherResp, err := client.CypherSearch(ctx, CypherSearchRequest{
		CypherQuery:  cypherQuery,
		TimeCriteria: &TimeCriteria{Start: startMs, End: endMs},
	})

	if err != nil {
		result.Checks = append(result.Checks, CheckResult{
			Name:           "Entity lookup",
			Status:         CheckFail,
			Detail:         fmt.Sprintf("Cypher query failed: %v", err),
			Recommendation: "Could not query the Knowledge Graph. Verify the Asserts API is reachable.",
		})
		result.computeSummary()
		return result
	}

	// Find the target entity in the response.
	var targetEntity *CypherEntity
	for i := range cypherResp.Entities {
		if cypherResp.Entities[i].Name == serviceName && cypherResp.Entities[i].Type == "Service" {
			targetEntity = &cypherResp.Entities[i]
			break
		}
	}

	if targetEntity == nil {
		// Try a simpler query without relationships — the service may exist but have no edges.
		simpleQuery := fmt.Sprintf(`MATCH (s:Service {name: "%s"}) RETURN s`, serviceName)
		simpleResp, simpleErr := client.CypherSearch(ctx, CypherSearchRequest{
			CypherQuery:  simpleQuery,
			TimeCriteria: &TimeCriteria{Start: startMs, End: endMs},
		})
		if simpleErr == nil {
			for i := range simpleResp.Entities {
				if simpleResp.Entities[i].Name == serviceName {
					targetEntity = &simpleResp.Entities[i]
					break
				}
			}
		}
	}

	if targetEntity == nil {
		result.Checks = append(result.Checks, CheckResult{
			Name:           "Entity lookup",
			Status:         CheckFail,
			Detail:         fmt.Sprintf("Service %q not found in the Knowledge Graph", serviceName),
			Recommendation: fmt.Sprintf("Check that OTEL_SERVICE_NAME is set to %q and that traces_target_info{service_name=%q} exists in Mimir.", serviceName, serviceName),
		})
		// Still run metric checks — they can tell us if the data is in Mimir but not in the graph.
		if promClient != nil && datasourceUID != "" {
			result.Checks = append(result.Checks, runServiceMetricChecks(ctx, promClient, datasourceUID, serviceName, scope.env)...)
		}
		result.Diagnosis = append(result.Diagnosis, fmt.Sprintf("Service %q was not found in the Entity Graph.", serviceName))
		result.NextSteps = append(result.NextSteps, fmt.Sprintf("gcx metrics query 'traces_target_info{service_name=\"%s\"}' --since 1h", serviceName))
		result.NextSteps = append(result.NextSteps, "gcx metrics query 'group by (service_name) (traces_target_info)' --since 1h")
		result.computeSummary()
		return result
	}

	// Entity found — populate info.
	entityInfo := buildEntityInfo(targetEntity)
	result.Entity = &entityInfo
	result.Checks = append(result.Checks, CheckResult{
		Name:   "Entity lookup",
		Status: CheckPass,
		Detail: fmt.Sprintf("type=%s, source=%s", entityInfo.Type, entityInfo.Source),
	})

	// Step 2: Analyze relationships.
	edges := buildEdgeList(serviceName, cypherResp.Edges)
	result.Edges = edges

	if len(edges) == 0 {
		result.Checks = append(result.Checks, CheckResult{
			Name:           "Relationships",
			Status:         CheckFail,
			Detail:         "no edges found",
			Recommendation: "This service has no CALLS or other relationships. Check that trace context is propagated on outgoing HTTP/gRPC calls.",
		})
	} else {
		incoming := 0
		outgoing := 0
		byType := map[string]int{}
		for _, e := range edges {
			byType[e.Type]++
			if e.Direction == "incoming" {
				incoming++
			} else {
				outgoing++
			}
		}
		var typeParts []string
		for t, c := range byType {
			typeParts = append(typeParts, fmt.Sprintf("%d %s", c, t))
		}
		sort.Strings(typeParts)
		result.Checks = append(result.Checks, CheckResult{
			Name:   "Relationships",
			Status: CheckPass,
			Detail: fmt.Sprintf("%d edges (%d outgoing, %d incoming): %s", len(edges), outgoing, incoming, strings.Join(typeParts, ", ")),
		})
	}

	// Step 3: Check insights.
	if len(targetEntity.Insights) > 0 {
		var parts []string
		for _, ins := range targetEntity.Insights {
			parts = append(parts, fmt.Sprintf("%s (%s)", ins.Name, ins.Severity))
		}
		result.Checks = append(result.Checks, CheckResult{
			Name:   "Insights",
			Status: CheckPass,
			Detail: fmt.Sprintf("%d active: %s", len(targetEntity.Insights), strings.Join(parts, ", ")),
		})
	} else {
		result.Checks = append(result.Checks, CheckResult{
			Name:   "Insights",
			Status: CheckPass,
			Detail: "no active insights",
		})
	}

	// Step 4: Per-service metric checks.
	if promClient != nil && datasourceUID != "" {
		result.Checks = append(result.Checks, runServiceMetricChecks(ctx, promClient, datasourceUID, serviceName, scope.env)...)
	}

	// Step 5: Interpret results.
	result.Diagnosis, result.NextSteps = interpretServiceResults(&result)

	result.computeSummary()
	return result
}

// ---------------------------------------------------------------------------
// Service metric checks
// ---------------------------------------------------------------------------

func runServiceMetricChecks(ctx context.Context, promClient *prometheus.Client, datasourceUID, serviceName, env string) []CheckResult {
	envFilter := ""
	if env != "" {
		envFilter = fmt.Sprintf(`, asserts_env="%s"`, env)
	}

	checks := []metricCheckDef{
		{
			Name:           "Metric: traces_service_graph (client)",
			Query:          fmt.Sprintf(`count(traces_service_graph_request_total{client="%s"%s})`, serviceName, envFilter),
			Recommendation: fmt.Sprintf("Tempo sees no outbound calls FROM %s. Check that trace context propagation is enabled on outgoing HTTP/gRPC requests.", serviceName),
		},
		{
			Name:           "Metric: traces_service_graph (server)",
			Query:          fmt.Sprintf(`count(traces_service_graph_request_total{server="%s"%s})`, serviceName, envFilter),
			Recommendation: fmt.Sprintf("Tempo sees no inbound calls TO %s. Verify the service is receiving traced requests.", serviceName),
		},
		{
			Name:           "Metric: asserts:relation:calls",
			Query:          fmt.Sprintf(`count(asserts:relation:calls{service="%s"%s})`, serviceName, envFilter),
			Recommendation: "No CALLS edge metrics for this service. The recording rule may not be matching its labels.",
		},
		{
			Name:           "Metric: asserts:mixin_workload_job",
			Query:          fmt.Sprintf(`count(asserts:mixin_workload_job{service="%s"%s})`, serviceName, envFilter),
			Recommendation: "Entity discovery metric missing for this service. Check asserts_env label and recording rule configuration.",
		},
		{
			Name:           "Metric: asserts:request:rate5m",
			Query:          fmt.Sprintf(`count(asserts:request:rate5m{service="%s"%s})`, serviceName, envFilter),
			Recommendation: "Request rate KPI missing. Service KPIs may not display correctly.",
		},
	}

	var (
		results []CheckResult
		mu      sync.Mutex
		g       errgroup.Group
	)

	for _, def := range checks {
		g.Go(func() error {
			c := checkMetric(ctx, promClient, datasourceUID, def)
			mu.Lock()
			results = append(results, c)
			mu.Unlock()
			return nil
		})
	}
	_ = g.Wait()
	return results
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func buildEntityInfo(e *CypherEntity) EntityInfo {
	info := EntityInfo{
		Type:       e.Type,
		Name:       e.Name,
		Properties: make(map[string]string),
	}

	if e.Scope != nil {
		if v, ok := e.Scope["env"].(string); ok {
			info.Env = v
		}
		if v, ok := e.Scope["namespace"].(string); ok {
			info.Namespace = v
		}
	}

	// Extract key properties as strings.
	for _, key := range []string{"_entity_source_10", "otel_service", "service", "job", "workload", "cluster", "deployment_environment", "service_version", "telemetry_sdk_language"} {
		if v, ok := e.Properties[key]; ok {
			info.Properties[key] = fmt.Sprint(v)
		}
	}

	// Source is the entity discovery path — key varies by version (e.g. _entity_source_7, _entity_source_10).
	info.Source = "unknown"
	for k, v := range e.Properties {
		if strings.HasPrefix(k, "_entity_source_") {
			if src, ok := v.(string); ok {
				info.Source = src
				break
			}
		}
	}

	return info
}

func buildEdgeList(serviceName string, edges []CypherEdge) []EdgeInfo {
	var result []EdgeInfo
	seen := map[string]bool{}

	for _, e := range edges {
		var ei EdgeInfo
		switch {
		case e.SourceName == serviceName:
			ei = EdgeInfo{
				Direction: "outgoing",
				Type:      e.Type,
				PeerName:  e.DestinationName,
				PeerType:  e.DestinationType,
			}
			if v, ok := e.DestinationScope["env"].(string); ok {
				ei.PeerEnv = v
			}
		case e.DestinationName == serviceName:
			ei = EdgeInfo{
				Direction: "incoming",
				Type:      e.Type,
				PeerName:  e.SourceName,
				PeerType:  e.SourceType,
			}
			if v, ok := e.SourceScope["env"].(string); ok {
				ei.PeerEnv = v
			}
		default:
			continue
		}
		key := fmt.Sprintf("%s-%s-%s-%s", ei.Direction, ei.Type, ei.PeerName, ei.PeerType)
		if !seen[key] {
			seen[key] = true
			result = append(result, ei)
		}
	}
	return result
}

// interpretServiceResults generates human-readable diagnosis and next steps
// by looking at the pattern of pass/fail across checks.
func interpretServiceResults(r *ServiceDiagnoseResult) ([]string, []string) {
	checkStatus := map[string]CheckStatus{}
	for _, c := range r.Checks {
		checkStatus[c.Name] = c.Status
	}

	entityFound := checkStatus["Entity lookup"] == CheckPass
	hasEdges := checkStatus["Relationships"] == CheckPass

	serverMetric := checkStatus["Metric: traces_service_graph (server)"]
	clientMetric := checkStatus["Metric: traces_service_graph (client)"]
	callsMetric := checkStatus["Metric: asserts:relation:calls"]

	var diagnosis, nextSteps []string

	if !entityFound {
		diagnosis = append(diagnosis, fmt.Sprintf("Service %q was not found in the Entity Graph.", r.ServiceName))
		if checkStatus["Metric: asserts:mixin_workload_job"] == CheckPass {
			diagnosis = append(diagnosis, "However, the entity discovery metric (asserts:mixin_workload_job) exists in Mimir. The graph may need time to ingest the entity.")
		}
		nextSteps = append(nextSteps, fmt.Sprintf("gcx metrics query 'traces_target_info{service_name=\"%s\"}' --since 1h", r.ServiceName))
		return diagnosis, nextSteps
	}

	if entityFound && !hasEdges {
		diagnosis = append(diagnosis, fmt.Sprintf("Service %q exists in the graph but has no relationships.", r.ServiceName))

		switch {
		case serverMetric == CheckPass && callsMetric == CheckFail:
			diagnosis = append(diagnosis, "Tempo sees inbound calls (server series exist) but the asserts:relation:calls recording rule produced no edges. The recording rule may not be matching this service's labels (check asserts_env and namespace).")
			nextSteps = append(nextSteps, fmt.Sprintf("gcx metrics query 'traces_service_graph_request_total{server=\"%s\"}' --since 1h", r.ServiceName))
			nextSteps = append(nextSteps, "gcx kg diagnose labels")
		case serverMetric == CheckFail && clientMetric == CheckFail:
			diagnosis = append(diagnosis, "Tempo is not generating any service graph metrics for this service. Either no traced HTTP/gRPC traffic exists, or trace context propagation is not working.")
		case serverMetric == CheckPass && clientMetric == CheckFail:
			diagnosis = append(diagnosis, "Tempo sees inbound calls but no outbound calls. The service may not propagate trace context on its own HTTP/gRPC requests, or it only consumes from queues (which don't generate service graph edges).")
		}
		return diagnosis, nextSteps
	}

	if entityFound && hasEdges {
		diagnosis = append(diagnosis, fmt.Sprintf("Service %q looks healthy — found in graph with %d edge(s).", r.ServiceName, len(r.Edges)))
	}

	return diagnosis, nextSteps
}

// ---------------------------------------------------------------------------
// Service diagnosis text codec
// ---------------------------------------------------------------------------

// ServiceDiagnoseTextCodec renders ServiceDiagnoseResult as human-readable text.
type ServiceDiagnoseTextCodec struct{}

func (c *ServiceDiagnoseTextCodec) Format() format.Format { return "text" }

func (c *ServiceDiagnoseTextCodec) Encode(w io.Writer, v any) error {
	r, ok := v.(ServiceDiagnoseResult)
	if !ok {
		return errors.New("invalid data type for text codec: expected ServiceDiagnoseResult")
	}

	// Header.
	header := "Service Diagnosis: " + r.ServiceName
	if r.Env != "" {
		header += " (env=" + r.Env
		if r.Namespace != "" {
			header += ", namespace=" + r.Namespace
		}
		header += ")"
	}
	fmt.Fprintln(w, header)
	fmt.Fprintln(w)

	// Entity info.
	if r.Entity != nil {
		fmt.Fprintln(w, "  ENTITY")
		fmt.Fprintf(w, "  ✓ Found: type=%s, env=%s, namespace=%s, source=%s\n", r.Entity.Type, r.Entity.Env, r.Entity.Namespace, r.Entity.Source)
		if len(r.Entity.Properties) > 0 {
			keys := make([]string, 0, len(r.Entity.Properties))
			for k := range r.Entity.Properties {
				if k != "_entity_source_10" { // already shown as source
					keys = append(keys, k)
				}
			}
			sort.Strings(keys)
			var propParts []string
			for _, k := range keys {
				propParts = append(propParts, fmt.Sprintf("%s=%s", k, r.Entity.Properties[k]))
			}
			fmt.Fprintf(w, "    Properties: %s\n", strings.Join(propParts, ", "))
		}
		fmt.Fprintln(w)
	}

	// Edges.
	if len(r.Edges) > 0 {
		fmt.Fprintln(w, "  RELATIONSHIPS")
		for _, e := range r.Edges {
			arrow := "→"
			peer := e.PeerName
			if e.Direction == "incoming" {
				arrow = "←"
			}
			if e.PeerEnv != "" && e.PeerEnv != r.Env {
				peer += fmt.Sprintf(" [env=%s]", e.PeerEnv)
			}
			fmt.Fprintf(w, "    %s %s %s (%s)\n", e.Type, arrow, peer, e.PeerType)
		}
		fmt.Fprintln(w)
	}

	// Checks table.
	fmt.Fprintln(w, "Checks:")
	t := style.NewTable("CHECK", "STATUS", "DETAIL")
	for _, check := range r.Checks {
		t.Row(check.Name, strings.ToUpper(string(check.Status)), check.Detail)
	}
	_ = t.Render(w)

	// Recommendations.
	var recs []string
	for _, check := range r.Checks {
		if check.Recommendation != "" && check.Status != CheckPass {
			recs = append(recs, fmt.Sprintf("  %s: %s", check.Name, check.Recommendation))
		}
	}
	if len(recs) > 0 {
		fmt.Fprintln(w)
		fmt.Fprintln(w, "Recommendations:")
		for _, rec := range recs {
			fmt.Fprintln(w, rec)
		}
	}

	// Diagnosis.
	if len(r.Diagnosis) > 0 {
		fmt.Fprintln(w)
		fmt.Fprintln(w, "Diagnosis:")
		for _, d := range r.Diagnosis {
			fmt.Fprintf(w, "  %s\n", d)
		}
	}

	// Next steps.
	if len(r.NextSteps) > 0 {
		fmt.Fprintln(w)
		fmt.Fprintln(w, "Suggested next steps:")
		for i, s := range r.NextSteps {
			fmt.Fprintf(w, "  %d. %s\n", i+1, s)
		}
	}

	// Summary.
	fmt.Fprintln(w)
	fmt.Fprintf(w, "%d/%d checks passed", r.Summary.Passed, r.Summary.Total)
	if r.Summary.Failed > 0 {
		fmt.Fprintf(w, ", %d failed", r.Summary.Failed)
	}
	if r.Summary.Warned > 0 {
		fmt.Fprintf(w, ", %d warning(s)", r.Summary.Warned)
	}
	fmt.Fprintln(w)

	return nil
}

func (c *ServiceDiagnoseTextCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("text format does not support decoding")
}
