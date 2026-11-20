package tempo_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/grafana/gcx/internal/query/tempo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildExceptionsQuery(t *testing.T) {
	const projection = "select(resource.service.name, event.exception.message, event.exception.type) with(most_recent=true)"
	tests := []struct {
		name    string
		filter  string
		want    string
		wantErr string
	}{
		{name: "empty is every service", filter: "", want: "{ status = error } | " + projection},
		{name: "empty spanset is every service", filter: "{}", want: "{ status = error } | " + projection},
		{name: "wraps a service filter", filter: `{ resource.service.name = "checkout" }`, want: `{ (resource.service.name = "checkout") && status = error } | ` + projection},
		{name: "rejects a pipeline", filter: `{ status = error } | select(resource.service.name)`, wantErr: "one TraceQL spanset"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tempo.BuildExceptionsQuery(tt.filter)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				assert.Contains(t, err.Error(), tt.filter)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestAggregateExceptions(t *testing.T) {
	resp := &tempo.SpanSearchResponse{
		Traces: []tempo.SpanSearchTrace{
			{
				StartTimeUnixNano: "100",
				SpanSets: []tempo.SpanSet{{
					Spans: []tempo.SpanHit{{
						StartTimeUnixNano: "100",
						Attributes: []tempo.SpanAttribute{
							attr("event.exception.message", "timeout for user 456"),
							attr("event.exception.type", "TimeoutError"),
							attr("resource.service.name", "checkout"),
						},
					}},
				}},
			},
			{
				StartTimeUnixNano: "200",
				SpanSets: []tempo.SpanSet{{
					Spans: []tempo.SpanHit{
						{
							StartTimeUnixNano: "200",
							Attributes: []tempo.SpanAttribute{
								attr("event.exception.message", "timeout for user 123"),
								attr("event.exception.type", "TimeoutError"),
								attr("resource.service.name", "checkout"),
							},
						},
						{
							StartTimeUnixNano: "150",
							Attributes: []tempo.SpanAttribute{
								attr("exception.message", "  other   failure  "),
								attr("exception.type", "Other"),
								attr("service.name", "payments"),
							},
						},
						{Attributes: []tempo.SpanAttribute{attr("event.exception.type", "Ignored")}},
					},
				}},
			},
		},
	}

	got := tempo.AggregateExceptions(resp, "{ status = error }", 400)
	require.Len(t, got.Exceptions, 2)
	assert.Equal(t, 2, got.TracesSampled)
	assert.Equal(t, 4, got.SpansSampled)
	assert.Equal(t, "timeout for user <num>", got.Exceptions[0].Message)
	assert.Equal(t, 2, got.Exceptions[0].Occurrences)
	assert.Equal(t, "TimeoutError", got.Exceptions[0].Type)
	assert.Equal(t, "checkout", got.Exceptions[0].Service)
	// Newer trace is walked first, so its raw message is the representative variant order.
	assert.Equal(t, []string{"timeout for user 123", "timeout for user 456"}, got.Exceptions[0].GroupedMessages)
	assert.Equal(t, time.Unix(0, 200).UTC(), got.Exceptions[0].LastSeen)
	assert.Equal(t, "other failure", got.Exceptions[1].Message)
	assert.Equal(t, "Other", got.Exceptions[1].Type)
	assert.Equal(t, "payments", got.Exceptions[1].Service)
}

func TestAggregateExceptionsGroupsLikeTheExceptionsTab(t *testing.T) {
	longA := "Payment failed while processing customer alpha request due to timeout in upstream dependency"
	longB := "Payment failed while processing customer bravo request due to timeout in upstream dependency"
	httpA := "Error: HttpException: Connection closed before full header was received, uri = https://oncall-prod-us-central-1.grafana.net/oncall/api/internal/v1/user"
	httpB := "Error: HttpException: Connection closed before full header was received, uri = https://oncall-prod-us-central-2.grafana.net/oncall/mobile_app/v1/gateway/incident/api/OrgService.GetOrg"

	resp := &tempo.SpanSearchResponse{Traces: []tempo.SpanSearchTrace{{
		SpanSets: []tempo.SpanSet{{Spans: []tempo.SpanHit{
			spanMsg(httpA, "HttpException"),
			spanMsg(httpB, "HttpException"),
			spanMsg(longA, "TimeoutException"),
			spanMsg(longB, "TimeoutException"),
			spanMsg("Completely different problem in another subsystem", "OtherException"),
			spanMsg("Error 1", ""),
			spanMsg("Error 2", ""),
			spanMsg("", "Skipped"),
		}}},
	}}}

	got := tempo.AggregateExceptions(resp, "q", 400)
	require.Len(t, got.Exceptions, 4)
	assert.Equal(t, 2, got.Exceptions[0].Occurrences)
	assert.Contains(t, got.Exceptions[0].Message, "<url>")
	assert.Equal(t, "HttpException", got.Exceptions[0].Type)
	assert.Equal(t, []string{httpA, httpB}, got.Exceptions[0].GroupedMessages)

	byMessage := map[string]tempo.ExceptionGroup{}
	for _, group := range got.Exceptions {
		byMessage[group.Message] = group
	}
	payment := byMessage[longA]
	assert.Equal(t, 2, payment.Occurrences)
	assert.Equal(t, "TimeoutException", payment.Type)
	assert.Equal(t, 2, byMessage["Error <num>"].Occurrences)
	assert.Equal(t, "Unknown", byMessage["Error <num>"].Type)
	assert.Equal(t, 1, byMessage["Completely different problem in another subsystem"].Occurrences)
}

func TestAggregateExceptionsSplitsTheSameMessageByService(t *testing.T) {
	resp := &tempo.SpanSearchResponse{Traces: []tempo.SpanSearchTrace{{
		SpanSets: []tempo.SpanSet{{Spans: []tempo.SpanHit{
			spanMsgService("timeout for user 1", "TimeoutError", "checkout"),
			spanMsgService("timeout for user 2", "TimeoutError", "payments"),
		}}},
	}}}

	got := tempo.AggregateExceptions(resp, "q", 300)
	require.Len(t, got.Exceptions, 2)
	assert.Equal(t, "checkout", got.Exceptions[0].Service)
	assert.Equal(t, 1, got.Exceptions[0].Occurrences)
	assert.Equal(t, "payments", got.Exceptions[1].Service)
	assert.Equal(t, 1, got.Exceptions[1].Occurrences)
}

func TestAggregateExceptionsEmptyJSONArray(t *testing.T) {
	got := tempo.AggregateExceptions(&tempo.SpanSearchResponse{}, "q", 400)
	data, err := json.Marshal(got)
	require.NoError(t, err)
	assert.Contains(t, string(data), `"exceptions":[]`)
	assert.NotContains(t, string(data), "list_meta")
}

func TestExceptionListMeta(t *testing.T) {
	argv := []string{"gcx", "traces", "exceptions", "--limit", "50"}

	assert.Nil(t, tempo.ExceptionListMeta(3, 10, 50, argv))

	partial := tempo.ExceptionListMeta(3, 50, 50, argv)
	require.NotNil(t, partial)
	assert.True(t, partial.Truncated)
	assert.Equal(t, 3, partial.Returned)
	assert.Zero(t, partial.Cap)
	assert.Contains(t, partial.Continue, "--limit")
	assert.Contains(t, partial.Continue, "100")
	assert.NotContains(t, partial.Continue, "--limit 50")

	capped := tempo.ExceptionListMeta(3, 300, 300, argv)
	require.NotNil(t, capped)
	assert.Equal(t, 300, capped.Cap)
	assert.Empty(t, capped.Continue)
}

func TestSearchSpansSendsSPSS(t *testing.T) {
	client := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "10", r.URL.Query().Get("spss"))
		assert.Equal(t, "400", r.URL.Query().Get("limit"))
		assert.Contains(t, r.URL.Query().Get("q"), "most_recent=true")
		writeJSON(t, w, map[string]any{"traces": []any{}})
	}))

	resp, err := client.SearchSpans(context.Background(), "tempo-ds", tempo.SpanSearchRequest{
		Query: `{ status = error } | select(resource.service.name) with(most_recent=true)`,
		Limit: 400,
		Start: time.Unix(1700000000, 0),
		End:   time.Unix(1700003600, 0),
	})
	require.NoError(t, err)
	assert.Empty(t, resp.Traces)
}

func attr(key, value string) tempo.SpanAttribute {
	raw, err := json.Marshal(map[string]any{"stringValue": value})
	if err != nil {
		panic(err)
	}
	return tempo.SpanAttribute{Key: key, Value: raw}
}

func spanMsgService(message, typ, service string) tempo.SpanHit {
	hit := spanMsg(message, typ)
	hit.Attributes = append(hit.Attributes, attr("resource.service.name", service))
	return hit
}

func spanMsg(message, typ string) tempo.SpanHit {
	attrs := []tempo.SpanAttribute{attr("event.exception.message", message)}
	if typ != "" {
		attrs = append(attrs, attr("event.exception.type", typ))
	}
	return tempo.SpanHit{Attributes: attrs}
}
