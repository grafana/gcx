package instances //nolint:testpackage // Tests cover native Alloy query identity.

import (
	"context"
	"encoding/json"
	"maps"
	"net/http"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/query/prometheus"
	"github.com/prometheus/prometheus/promql/parser"
)

// The label shape matches Alloy 1.19 connection_info and postgres_exporter.
// Inventory has service; exporter samples have instance/server_id, but no service.
func TestNativeAlloyListAndGet(t *testing.T) {
	cfg := testRESTConfig(t, nativeAlloyQueryHandler(t))
	client, err := prometheus.NewClient(cfg)
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.Query(context.Background(), "prom", prometheus.QueryRequest{Query: connectionInfoMetric})
	if err != nil {
		t.Fatal(err)
	}
	instances, err := parseInstancesResponse(response)
	if err != nil || len(instances) != 1 || instances[0].Name != "example-db" {
		t.Fatalf("native inventory: %+v, %v", instances, err)
	}
	detail, _, err := fetchInstanceDetail(context.Background(), client, "prom", "example-db", "5m", 10, []Matcher{{Label: "datname", Op: "=", Value: "example"}})
	if err != nil {
		t.Fatal(err)
	}
	if !detail.Health.HasUp || !detail.Health.Up || !detail.Health.HasScrapeError || detail.Health.ScrapeError || detail.Instance.Engine != "postgres" || len(detail.Connections) != 1 || len(detail.TopQueries) != 1 || len(detail.WaitEvents) != 1 {
		t.Fatalf("native detail missing data: %+v", detail)
	}
}

func nativeAlloyQueryHandler(t *testing.T) http.HandlerFunc {
	t.Helper()
	inventory := map[string]string{"service": "example-db", "instance": "example-db", "server_id": "server-123", "engine": "postgres", "engine_version": "18.1", "job": dbo11yJobValue}
	return func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Queries []struct {
				Expr string `json:"expr"`
			} `json:"queries"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || len(body.Queries) != 1 {
			t.Errorf("invalid query request: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		expr := body.Queries[0].Expr
		labels := map[string]string{"instance": "example-db", "server_id": "server-123", "job": "integrations/postgres"}
		value := float64(1)
		switch {
		case strings.Contains(expr, connectionInfoMetric):
			labels = inventory
			if expr != connectionInfoMetric && (!strings.Contains(expr, `service="example-db"`) || !strings.Contains(expr, `service_name="example-db"`)) {
				t.Errorf("metadata must accept both labels: %s", expr)
			}
		default:
			if !strings.Contains(expr, `instance="example-db"`) || strings.Contains(expr, `server_id=`) || strings.Contains(expr, "service_name=") {
				t.Errorf("exporter query does not use native identity: %s", expr)
			}
			switch {
			case strings.HasPrefix(expr, "up{"):
				if !strings.Contains(expr, `job="integrations/db-o11y"`) {
					t.Errorf("scrape health lost its job scope: %s", expr)
				}
				labels["job"] = dbo11yJobValue
			case strings.Contains(expr, "wait_event"):
				labels = map[string]string{"wait_event_type": "Lock", "wait_event": "transactionid"}
			case strings.Contains(expr, pgActivityCountMetric):
				labels = map[string]string{"state": "active"}
				value = 2
			case strings.Contains(expr, "pg_stat_statements"):
				if !strings.Contains(expr, `datname="example"`) {
					t.Errorf("caller filter missing: %s", expr)
				}
				labels = map[string]string{"queryid": "123", "datname": "example"}
			case strings.Contains(expr, pgScrapeErrorMetric):
				value = 0
			}
		}
		frame := map[string]any{"schema": map[string]any{"fields": []any{map[string]any{"name": "Time", "type": "time"}, map[string]any{"name": "Value", "type": "number", "labels": labels}}}, "data": map[string]any{"values": []any{[]any{float64(1700000000000)}, []any{value}}}}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]any{"results": map[string]any{"A": map[string]any{"frames": []any{frame}}}}); err != nil {
			t.Errorf("response: %v", err)
		}
	}
}

func TestNativeAlloyIdentityFallback(t *testing.T) {
	for _, test := range []struct {
		name   string
		labels map[string]string
		want   string
		native bool
	}{
		{"legacy priority", map[string]string{"service_name": "legacy-db", "service": "native-db", "instance": "host"}, "legacy-db", false},
		{"native without server ID", map[string]string{"service": "native-db", "instance": "host"}, "native-db", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := parseInstancesResponse(sampleResponse(map[string]any{"metric": test.labels, "value": []any{float64(1), "1"}}))
			if err != nil || len(got) != 1 || got[0].Name != test.want || (len(got[0].identity) > 0) != test.native {
				t.Fatalf("identity: %+v, %v", got, err)
			}
			expr, err := buildUpQuery(pgScrapeErrorMetric, test.want, nil, got[0].identity...)
			if err != nil || strings.Contains(expr, `service_name=`) == test.native {
				t.Fatalf("selector: %s, %v", expr, err)
			}
		})
	}
}

func TestGetRejectsAmbiguousInventory(t *testing.T) {
	for _, test := range []struct{ name, label, value string }{
		{"host", "instance", "other-host"},
		{"engine", "engine", "mysql"},
	} {
		t.Run(test.name, func(t *testing.T) {
			cfg := testRESTConfig(t, func(w http.ResponseWriter, r *http.Request) {
				var body struct {
					Queries []struct {
						Expr string `json:"expr"`
					} `json:"queries"`
				}
				_ = json.NewDecoder(r.Body).Decode(&body)
				if !strings.Contains(body.Queries[0].Expr, connectionInfoMetric) {
					t.Error("ambiguous inventory must stop detail queries")
				}
				first := map[string]string{"service": "example-db", "instance": "host", "server_id": "server", "service_namespace": "example", "deployment_environment": "demo"}
				second := map[string]string{}
				maps.Copy(second, first)
				second[test.label] = test.value
				writeNativeFrames(t, w, []map[string]string{first, second})
			})
			client, err := prometheus.NewClient(cfg)
			if err != nil {
				t.Fatal(err)
			}
			result, _, err := fetchInstanceDetail(context.Background(), client, "prom", "example-db", "5m", 10, nil)
			if err == nil || result != nil {
				t.Fatalf("ambiguous inventory selected an instance: result=%+v err=%v", result, err)
			}
		})
	}
}

func writeNativeFrames(t *testing.T, w http.ResponseWriter, labels []map[string]string) {
	t.Helper()
	frames := make([]any, 0, len(labels))
	for _, l := range labels {
		frames = append(frames, map[string]any{"schema": map[string]any{"fields": []any{map[string]any{"name": "Time", "type": "time"}, map[string]any{"name": "Value", "type": "number", "labels": l}}}, "data": map[string]any{"values": []any{[]int{1000}, []int{1}}}})
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]any{"results": map[string]any{"A": map[string]any{"frames": frames}}}); err != nil {
		t.Error(err)
	}
}

func TestNativeExporterDoesNotRequireInventoryScopeLabels(t *testing.T) {
	for _, test := range []struct {
		name                          string
		inventoryScope, exporterScope map[string]string
	}{
		{"matching", map[string]string{"service_namespace": "example", "deployment_environment": "demo", "cluster": "local"}, map[string]string{"service_namespace": "example", "deployment_environment": "demo", "cluster": "local"}},
		{"foreign namespace", map[string]string{"service_namespace": "example"}, map[string]string{"service_namespace": "other"}},
		{"missing exporter scope", map[string]string{"service_namespace": "example", "cluster": "local"}, nil},
		{"scope absent on both", nil, nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			cfg := testRESTConfig(t, func(w http.ResponseWriter, r *http.Request) {
				var body struct {
					Queries []struct {
						Expr string `json:"expr"`
					} `json:"queries"`
				}
				_ = json.NewDecoder(r.Body).Decode(&body)
				expr := body.Queries[0].Expr
				labels := map[string]string{"instance": "host", "job": dbo11yJobValue}
				if strings.Contains(expr, connectionInfoMetric) {
					labels["service"] = "example-db"
					labels["engine"] = "postgres"
					maps.Copy(labels, test.inventoryScope)
					writeNativeFrames(t, w, []map[string]string{labels})
					return
				}
				maps.Copy(labels, test.exporterScope)
				matches, err := nativeSelectorMatches(expr, labels)
				if err != nil {
					t.Error(err)
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				if matches {
					writeNativeFrames(t, w, []map[string]string{labels})
				} else {
					writeNativeFrames(t, w, nil)
				}
			})
			client, err := prometheus.NewClient(cfg)
			if err != nil {
				t.Fatal(err)
			}
			result, _, err := fetchInstanceDetail(context.Background(), client, "prom", "example-db", "5m", 10, nil)
			if err != nil {
				t.Fatal(err)
			}
			if !result.Health.HasUp || !result.Health.HasScrapeError {
				t.Fatalf("scope selected unexpected telemetry: %+v", result.Health)
			}
		})
	}
}

func TestSelectInstanceMetadataDuplicatesAndLegacy(t *testing.T) {
	for _, native := range []bool{false, true} {
		first := Instance{Name: "example-db", Namespace: "example", Host: "host", Engine: "postgres", Labels: map[string]string{"server_id": "server"}}
		if native {
			first.native = true
			first.identity = []Matcher{{Label: "instance", Op: "=", Value: "host"}}
		}
		got, err := selectInstanceMetadata([]Instance{first, first}, "example-db")
		if err != nil || got.Name != first.Name {
			t.Fatalf("exact duplicates rejected: %+v %v", got, err)
		}
		other := first
		other.Host = "other"
		if _, err := selectInstanceMetadata([]Instance{first, other}, "example-db"); (err != nil) != native {
			t.Fatalf("host collision accepted, native=%v", native)
		}
	}
}

// Native instances without server_id must remain distinct in inventory.
func TestSelectNativeInstanceWithoutServerID(t *testing.T) {
	response := sampleResponse(
		map[string]any{"metric": map[string]string{"service": "example-db", "instance": "host-a"}, "value": []any{float64(1), "1"}},
		map[string]any{"metric": map[string]string{"service": "example-db", "instance": "host-b"}, "value": []any{float64(1), "1"}},
	)
	instances, err := parseInstancesResponse(response)
	if err != nil || len(instances) != 2 {
		t.Fatalf("native inventory: %+v, %v", instances, err)
	}
	if instances[0].Host == instances[1].Host {
		t.Fatal("inventory discarded the native instance identity")
	}
	if _, err := selectInstanceMetadata(instances, "example-db"); err == nil {
		t.Fatal("different native instances without server_id were accepted as duplicates")
	}
}

// Evaluate actual generated selectors against fixture labels.
func nativeSelectorMatches(expr string, labels map[string]string) (bool, error) {
	parsed, err := parser.NewParser(parser.Options{}).ParseExpr(expr)
	if err != nil {
		return false, err
	}
	matches := true
	parser.Inspect(parsed, func(node parser.Node, _ []parser.Node) error {
		if selector, ok := node.(*parser.VectorSelector); ok {
			for _, m := range selector.LabelMatchers {
				if m.Name != "__name__" && !m.Matches(labels[m.Name]) {
					matches = false
				}
			}
		}
		return nil
	})
	return matches, nil
}

func TestLegacyServiceLabelPreserved(t *testing.T) {
	got, err := parseInstancesResponse(sampleResponse(map[string]any{"metric": map[string]string{"service_name": "legacy", "service": "native"}, "value": []any{float64(1), "1"}}))
	if err != nil || len(got) != 1 || got[0].Labels["service"] != "native" {
		t.Fatalf("legacy service lost: %+v %v", got, err)
	}
}

func TestNativeMissingExporterIdentity(t *testing.T) {
	instances, err := parseInstancesResponse(sampleResponse(map[string]any{"metric": map[string]string{"service": "example-db"}, "value": []any{float64(1), "1"}}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := selectInstanceMetadata(instances, "example-db"); err == nil || !strings.Contains(err.Error(), "no exporter instance label") {
		t.Fatalf("missing identity error: %v", err)
	}
}

func TestNativeScrapeTargetNeedsNoServerID(t *testing.T) {
	instances, err := parseInstancesResponse(sampleResponse(map[string]any{"metric": map[string]string{"service": "example-db", "instance": "host", "server_id": "server"}, "value": []any{float64(1), "1"}}))
	if err != nil {
		t.Fatal(err)
	}
	expr, err := buildScrapeUpQuery("example-db", instances[0].identity...)
	if err != nil {
		t.Fatal(err)
	}
	matches, err := nativeSelectorMatches(expr, map[string]string{"instance": "host", "job": dbo11yJobValue})
	if err != nil || !matches {
		t.Fatalf("scrape target not matched: %s %v", expr, err)
	}
}

func TestMixedInventoryPrefersLegacy(t *testing.T) {
	for _, nativeFirst := range []bool{false, true} {
		legacy := Instance{Name: "db", Host: "legacy-host", Engine: "postgres"}
		native := Instance{Name: "db", Host: "native-host", Engine: "postgres", native: true}
		rows := []Instance{legacy, native}
		if nativeFirst {
			rows[0], rows[1] = rows[1], rows[0]
		}
		got, err := selectInstanceMetadata(rows, "db")
		if err != nil || got.native || got.Host != "legacy-host" {
			t.Fatalf("legacy selection: %+v %v", got, err)
		}
	}
}

func TestNativeMetadataDifferencesDoNotChangeSelectors(t *testing.T) {
	first := Instance{Name: "db", Host: "host", Engine: "postgres", native: true}
	other := first
	other.Namespace = "different"
	other.Environment = "different"
	other.Labels = map[string]string{"server_id": "different", "cluster": "different"}
	if _, err := selectInstanceMetadata([]Instance{first, other}, "db"); err != nil {
		t.Fatal(err)
	}
}

func TestNativeMetadataSkipsMissingHost(t *testing.T) {
	missing := Instance{Name: "db", native: true}
	named := Instance{Name: "db", Host: "host:5432", native: true, identity: []Matcher{{Label: "instance", Op: "=", Value: "host:5432"}}}
	for _, rows := range [][]Instance{{missing, named}, {named, missing}} {
		got, err := selectInstanceMetadata(rows, "db")
		if err != nil || got.Host != named.Host {
			t.Fatalf("valid host not selected: %+v %v", got, err)
		}
	}
}
func TestLegacyMetadataKeepsFirstEngine(t *testing.T) {
	first := Instance{Name: "db", Engine: "postgres"}
	got, err := selectInstanceMetadata([]Instance{first, {Name: "db", Engine: "mysql"}}, "db")
	if err != nil || got.Engine != first.Engine {
		t.Fatalf("legacy selection changed: %+v %v", got, err)
	}
}
