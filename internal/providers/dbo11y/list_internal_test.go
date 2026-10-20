package dbo11y

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestInstancesTable_Encode(t *testing.T) {
	items := []Instance{
		{
			ServerID:      "a1b2c3d4e5f60718293a4b5c6d7e8f90a1b2c3d4e5f60718293a4b5c6d7e8f90",
			Name:          "prod-postgres-1",
			Host:          "prod-postgres-1.abcd1234.us-east-1.rds.amazonaws.com:5432",
			Engine:        "postgres",
			EngineVersion: "17.9",
			// Region deliberately doesn't textually appear in Name or Host
			// (unlike a realistic AWS hostname) so the notWant check below
			// actually exercises "wide-only fields excluded from narrow
			// output" rather than incidentally matching a substring.
			ProviderName:       "aws",
			ProviderRegion:     "eu-west-1",
			InstanceIdentifier: "prod-postgres-1-rds",
		},
	}

	var buf bytes.Buffer
	if err := InstancesTable().Codec("table").Encode(&buf, items); err != nil {
		t.Fatalf("Encode() err = %v", err)
	}
	out := buf.String()

	for _, want := range []string{"NAME", "ENGINE", "VERSION", "PROVIDER", "prod-postgres-1", "postgres", "17.9", "aws"} {
		if !strings.Contains(out, want) {
			t.Errorf("table output missing %q\n---\n%s", want, out)
		}
	}
	for _, notWant := range []string{"HOST", "IDENTIFIER", "REGION", "SERVER ID", "eu-west-1", items[0].ServerID} {
		if strings.Contains(out, notWant) {
			t.Errorf("narrow table should not show %q (wide-only), got:\n%s", notWant, out)
		}
	}
}

func TestInstancesTable_Encode_Wide(t *testing.T) {
	items := []Instance{
		{
			ServerID:           "s1",
			Name:               "dev-postgres",
			Host:               "dev-postgres:5432",
			Engine:             "postgres",
			EngineVersion:      "17.9",
			ProviderName:       "aws",
			ProviderRegion:     "us-east-2",
			InstanceIdentifier: "db-1",
		},
	}

	var buf bytes.Buffer
	if err := InstancesTable().Codec("wide").Encode(&buf, items); err != nil {
		t.Fatalf("Encode() err = %v", err)
	}
	out := buf.String()

	for _, want := range []string{"HOST", "IDENTIFIER", "REGION", "SERVER ID", "dev-postgres:5432", "us-east-2", "db-1", "s1"} {
		if !strings.Contains(out, want) {
			t.Errorf("wide output missing %q\n---\n%s", want, out)
		}
	}
}

func TestInstancesTable_Encode_Empty(t *testing.T) {
	var buf bytes.Buffer
	if err := InstancesTable().Codec("table").Encode(&buf, []Instance{}); err != nil {
		t.Fatalf("Encode() err = %v", err)
	}
	if !strings.Contains(buf.String(), "No database instances discovered") {
		t.Errorf("expected the empty-list message, got:\n%s", buf.String())
	}
}

func TestInstancesTable_Encode_MissingFields(t *testing.T) {
	// A self-hosted instance with no cloud provider metadata and no engine
	// version renders dashes, not blank cells. Counting cells rather than
	// substring-matching "-": a hyphen inside a field value would otherwise
	// satisfy this assertion on its own.
	items := []Instance{{ServerID: "s1", Name: "selfhosted"}}

	var buf bytes.Buffer
	if err := InstancesTable().Codec("wide").Encode(&buf, items); err != nil {
		t.Fatalf("Encode() err = %v", err)
	}
	out := buf.String()

	var dataLine string
	for line := range strings.SplitSeq(out, "\n") {
		if strings.Contains(line, "selfhosted") {
			dataLine = line
		}
	}
	if dataLine == "" {
		t.Fatalf("no data row in output:\n%s", out)
	}
	dashes := 0
	for cell := range strings.FieldsSeq(dataLine) {
		if cell == "-" {
			dashes++
		}
	}
	// Absent: engine, version, provider, host, identifier, region.
	// Present: name, server_id.
	if want := 6; dashes != want {
		t.Errorf("expected %d dash cells for the absent fields, got %d in %q", want, dashes, dataLine)
	}
}

func TestInstancesEnvelopeCodec_Encode(t *testing.T) {
	// The table codec takes the same envelope -o json emits, so the column
	// declaration stays in one place.
	resp := &InstancesResponse{Items: []Instance{{Name: "db-1", Engine: "postgres"}}}

	var buf bytes.Buffer
	if err := (&instancesEnvelopeCodec{name: "table"}).Encode(&buf, resp); err != nil {
		t.Fatalf("Encode() err = %v", err)
	}
	if !strings.Contains(buf.String(), "db-1") {
		t.Errorf("expected the envelope's items to render, got:\n%s", buf.String())
	}

	if err := (&instancesEnvelopeCodec{name: "table"}).Encode(&bytes.Buffer{}, []Instance{}); err == nil {
		t.Error("expected a type error when handed a bare slice rather than the envelope")
	}
}

func TestInstancesResponse_JSONKeepsItemsEnvelope(t *testing.T) {
	// Scripts indexing into .items predate the API-backed implementation and
	// must keep working.
	out, err := json.Marshal(&InstancesResponse{Items: []Instance{{Name: "db-1"}}})
	if err != nil {
		t.Fatalf("Marshal() err = %v", err)
	}
	var decoded struct {
		Items []struct {
			Name string `json:"name"`
		} `json:"items"`
	}
	if err := json.Unmarshal(out, &decoded); err != nil {
		t.Fatalf("Unmarshal() err = %v", err)
	}
	if len(decoded.Items) != 1 || decoded.Items[0].Name != "db-1" {
		t.Errorf("expected one item named db-1 under .items, got %s", out)
	}
}

func TestOrDash(t *testing.T) {
	if got := orDash(""); got != "-" {
		t.Errorf("orDash(\"\") = %q, want %q", got, "-")
	}
	if got := orDash("postgres"); got != "postgres" {
		t.Errorf("orDash(%q) = %q, want unchanged", "postgres", got)
	}
}
