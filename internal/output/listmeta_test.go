package output_test

import (
	"bytes"
	"encoding/json"
	"testing"

	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/testutils"
	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func assertListMeta(t *testing.T, got, want *cmdio.ListMeta) {
	t.Helper()
	if want == nil {
		assert.Nil(t, got)
		return
	}
	require.NotNil(t, got)
	assert.Equal(t, want.Truncated, got.Truncated, "Truncated")
	assert.Equal(t, want.Returned, got.Returned, "Returned")
	assert.Equal(t, want.Cap, got.Cap, "Cap")
	assert.Equal(t, want.Continue, got.Continue, "Continue")
	if want.Total == nil {
		assert.Nil(t, got.Total, "Total must be absent (never guessed)")
	} else {
		require.NotNil(t, got.Total)
		assert.Equal(t, *want.Total, *got.Total, "Total")
	}
}

func TestTruncateCompleteList(t *testing.T) {
	items := []string{"a", "b", "c", "d"}

	tests := []struct {
		name      string
		limit     int
		wantItems []string
		wantMeta  *cmdio.ListMeta
	}{
		{
			name:      "limit zero returns all with nil meta",
			limit:     0,
			wantItems: items,
		},
		{
			name:      "negative limit returns all with nil meta",
			limit:     -1,
			wantItems: items,
		},
		{
			name:      "under limit returns all with nil meta",
			limit:     10,
			wantItems: items,
		},
		{
			name:      "exactly at limit returns all with nil meta",
			limit:     4,
			wantItems: items,
		},
		{
			name:      "over limit truncates with observed total",
			limit:     2,
			wantItems: []string{"a", "b"},
			wantMeta:  &cmdio.ListMeta{Truncated: true, Returned: 2, Total: new(4)},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, meta := cmdio.TruncateCompleteList(items, tc.limit)
			assert.Equal(t, tc.wantItems, got)
			assertListMeta(t, meta, tc.wantMeta)
		})
	}
}

func TestTruncatePagedList(t *testing.T) {
	// Over-fetch-by-one: the caller requested limit+1 items from the API.
	tests := []struct {
		name      string
		items     []string
		limit     int
		wantItems []string
		wantMeta  *cmdio.ListMeta
	}{
		{
			name:      "limit zero means drained source, nil meta",
			items:     []string{"a", "b", "c"},
			limit:     0,
			wantItems: []string{"a", "b", "c"},
		},
		{
			name:      "short page proves source drained",
			items:     []string{"a", "b"},
			limit:     3,
			wantItems: []string{"a", "b"},
		},
		{
			name:      "exactly limit items means no spare row, no truncation",
			items:     []string{"a", "b", "c"},
			limit:     3,
			wantItems: []string{"a", "b", "c"},
		},
		{
			name:      "spare row proves more exist, total stays unknown",
			items:     []string{"a", "b", "c", "d"},
			limit:     3,
			wantItems: []string{"a", "b", "c"},
			wantMeta:  &cmdio.ListMeta{Truncated: true, Returned: 3},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, meta := cmdio.TruncatePagedList(tc.items, tc.limit)
			assert.Equal(t, tc.wantItems, got)
			assertListMeta(t, meta, tc.wantMeta)
		})
	}
}

func TestPagedListMeta(t *testing.T) {
	tests := []struct {
		name          string
		returned      int
		limit         int
		serverHasMore bool
		safetyCap     int
		want          *cmdio.ListMeta
	}{
		{
			name:          "server reports no more pages, nil meta",
			returned:      50,
			limit:         50,
			serverHasMore: false,
			safetyCap:     1000,
			want:          nil,
		},
		{
			name:          "limit zero and no more pages is genuinely complete",
			returned:      120,
			limit:         0,
			serverHasMore: false,
			safetyCap:     1000,
			want:          nil,
		},
		{
			name:          "user limit bound the page, no cap recorded",
			returned:      50,
			limit:         50,
			serverHasMore: true,
			safetyCap:     1000,
			want:          &cmdio.ListMeta{Truncated: true, Returned: 50},
		},
		{
			// PR988 regression: `--limit 0` capped by the safety cap must
			// report a truncated page — a hard-capped fetch is NOT the
			// complete set.
			name:          "limit zero capped by safety cap is truncated with cap",
			returned:      1000,
			limit:         0,
			serverHasMore: true,
			safetyCap:     1000,
			want:          &cmdio.ListMeta{Truncated: true, Returned: 1000, Cap: 1000},
		},
		{
			name:          "limit above the cap means the cap was the bound",
			returned:      1000,
			limit:         5000,
			serverHasMore: true,
			safetyCap:     1000,
			want:          &cmdio.ListMeta{Truncated: true, Returned: 1000, Cap: 1000},
		},
		{
			// Boundary: at limit == cap a doubled --limit continuation could
			// never return more than the cap allows — the page must use the
			// cap variant (Cap set, no continuation), not advertise a dead
			// continuation.
			name:          "limit equal to the cap means the cap is the ceiling",
			returned:      1000,
			limit:         1000,
			serverHasMore: true,
			safetyCap:     1000,
			want:          &cmdio.ListMeta{Truncated: true, Returned: 1000, Cap: 1000},
		},
		{
			name:          "no safety cap configured, limit zero with more pages",
			returned:      200,
			limit:         0,
			serverHasMore: true,
			safetyCap:     0,
			want:          &cmdio.ListMeta{Truncated: true, Returned: 200},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := cmdio.PagedListMeta(tc.returned, tc.limit, tc.serverHasMore, tc.safetyCap)
			assertListMeta(t, got, tc.want)
		})
	}
}

func TestAttachListMeta(t *testing.T) {
	argv := []string{"gcx", "datasources", "list", "--type", "prometheus", "--limit", "5"}

	tests := []struct {
		name         string
		meta         *cmdio.ListMeta
		argv         []string
		wantContinue string
	}{
		{
			name: "nil meta stays nil",
			meta: nil,
			argv: argv,
		},
		{
			name:         "known total derives --limit 0 preserving filters",
			meta:         &cmdio.ListMeta{Truncated: true, Returned: 5, Total: new(219)},
			argv:         argv,
			wantContinue: "gcx datasources list --type prometheus --limit 0",
		},
		{
			name:         "unknown total derives doubled limit preserving filters",
			meta:         &cmdio.ListMeta{Truncated: true, Returned: 5},
			argv:         argv,
			wantContinue: "gcx datasources list --type prometheus --limit 10",
		},
		{
			name:         "limit=value spelling is stripped too",
			meta:         &cmdio.ListMeta{Truncated: true, Returned: 3, Total: new(9)},
			argv:         []string{"gcx", "alert", "rules", "list", "--limit=3", "--folder", "abc"},
			wantContinue: "gcx alert rules list --folder abc --limit 0",
		},
		{
			name:         "cap-bounded page has no runnable continuation",
			meta:         &cmdio.ListMeta{Truncated: true, Returned: 1000, Cap: 1000},
			argv:         argv,
			wantContinue: "",
		},
		{
			name:         "empty argv yields no continuation",
			meta:         &cmdio.ListMeta{Truncated: true, Returned: 5, Total: new(219)},
			argv:         nil,
			wantContinue: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := cmdio.AttachListMeta(tc.meta, tc.argv)
			if tc.meta == nil {
				assert.Nil(t, got)
				return
			}
			require.NotNil(t, got)
			assert.Equal(t, tc.wantContinue, got.Continue)
		})
	}
}

func TestEmitListTruncationHint(t *testing.T) {
	// The continuation is read from meta.Continue, which [AttachListMeta]
	// populates from the invocation argv (covered by TestAttachListMeta);
	// argv column here shows the invocation each fixture corresponds to.
	tests := []struct {
		name      string
		meta      *cmdio.ListMeta
		argv      []string
		agentMode bool
		want      string
	}{
		{
			name: "nil meta emits nothing",
			meta: nil,
			argv: []string{"gcx", "datasources", "list"},
			want: "",
		},
		{
			// Maintainer-approved TTY shape (PR988 review): a sentence, not a
			// "<summary>: <command>" splice.
			name: "total known TTY",
			meta: &cmdio.ListMeta{Truncated: true, Returned: 5, Total: new(219)},
			argv: []string{"gcx", "datasources", "list", "--limit", "5"},
			want: "hint: showing first 5 of 219. See all results with: gcx datasources list --limit 0\n",
		},
		{
			name: "total known TTY preserves filter flags in continuation",
			meta: &cmdio.ListMeta{Truncated: true, Returned: 1, Total: new(219)},
			argv: []string{"gcx", "datasources", "list", "--type", "prometheus", "--limit", "1"},
			want: "hint: showing first 1 of 219. See all results with: gcx datasources list --type prometheus --limit 0\n",
		},
		{
			// Unknown total: never promise --limit 0 retrieves everything —
			// suggest a doubled limit instead.
			name: "total unknown TTY suggests doubled limit",
			meta: &cmdio.ListMeta{Truncated: true, Returned: 50},
			argv: []string{"gcx", "irm", "oncall", "alert-groups", "list", "--limit", "50"},
			want: "hint: showing first 50; more results are available. See more with: gcx irm oncall alert-groups list --limit 100\n",
		},
		{
			// Cap variant: no --limit suggestion at all — a bigger limit
			// cannot beat the safety cap.
			name: "cap bounded TTY suggests refining filters",
			meta: &cmdio.ListMeta{Truncated: true, Returned: 1000, Cap: 1000},
			argv: []string{"gcx", "irm", "oncall", "alert-groups", "list", "--limit", "0"},
			want: "hint: showing first 1000 (safety cap). Refine filters to narrow the result set\n",
		},
		{
			name:      "agent mode emits JSONL hint with bare summary",
			meta:      &cmdio.ListMeta{Truncated: true, Returned: 5, Total: new(219)},
			argv:      []string{"gcx", "datasources", "list", "--limit", "5"},
			agentMode: true,
			want:      `{"class":"hint","summary":"showing first 5 of 219","command":"gcx datasources list --limit 0"}` + "\n",
		},
		{
			name:      "agent mode cap variant",
			meta:      &cmdio.ListMeta{Truncated: true, Returned: 1000, Cap: 1000},
			argv:      []string{"gcx", "irm", "oncall", "alert-groups", "list"},
			agentMode: true,
			want:      `{"class":"hint","summary":"showing first 1000 (safety cap). Refine filters to narrow the result set"}` + "\n",
		},
		{
			name: "empty continuation falls back to bare summary",
			meta: &cmdio.ListMeta{Truncated: true, Returned: 5, Total: new(219)},
			argv: nil,
			want: "hint: showing first 5 of 219\n",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			testutils.SetAgentMode(t, tc.agentMode)

			var buf bytes.Buffer
			cmdio.EmitListTruncationHint(&buf, cmdio.AttachListMeta(tc.meta, tc.argv))
			assert.Equal(t, tc.want, buf.String())
		})
	}
}

// TestEmitListTruncationHint_LimitEqualsCap pipes the full helper chain —
// PagedListMeta → AttachListMeta → EmitListTruncationHint — for the
// limit == safetyCap boundary: the page must emit the refine-filters cap
// variant, never a doubled --limit continuation that cannot return more.
func TestEmitListTruncationHint_LimitEqualsCap(t *testing.T) {
	testutils.SetAgentMode(t, false)

	meta := cmdio.AttachListMeta(
		cmdio.PagedListMeta(1000, 1000, true, 1000),
		[]string{"gcx", "irm", "oncall", "alert-groups", "list", "--limit", "1000"})
	require.NotNil(t, meta)
	assert.Equal(t, 1000, meta.Cap)
	assert.Empty(t, meta.Continue, "cap-bounded page must not carry a continuation")

	var buf bytes.Buffer
	cmdio.EmitListTruncationHint(&buf, meta)
	assert.Equal(t, "hint: showing first 1000 (safety cap). Refine filters to narrow the result set\n", buf.String())
	assert.NotContains(t, buf.String(), "--limit 2000", "doubled-limit continuation would be dead at the cap")
}

// TestListMetaJSONShape locks the wire shape agents key on: truncated is
// always explicit; total and cap are omitted (never guessed) when unknown.
func TestListMetaJSONShape(t *testing.T) {
	tests := []struct {
		name string
		meta *cmdio.ListMeta
		want string
	}{
		{
			name: "total known",
			meta: &cmdio.ListMeta{Truncated: true, Returned: 2, Total: new(4), Continue: "gcx datasources list --limit 0"},
			want: `{"truncated":true,"returned":2,"total":4,"continue":"gcx datasources list --limit 0"}`,
		},
		{
			name: "total unknown is omitted",
			meta: &cmdio.ListMeta{Truncated: true, Returned: 50, Continue: "gcx things list --limit 100"},
			want: `{"truncated":true,"returned":50,"continue":"gcx things list --limit 100"}`,
		},
		{
			name: "cap variant carries cap and no continue",
			meta: &cmdio.ListMeta{Truncated: true, Returned: 1000, Cap: 1000},
			want: `{"truncated":true,"returned":1000,"cap":1000}`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			b, err := json.Marshal(tc.meta)
			require.NoError(t, err)
			assert.Equal(t, tc.want, string(b))
		})
	}
}

func TestBuildListLimitCommand(t *testing.T) {
	tests := []struct {
		name  string
		argv  []string
		limit int
		want  string
	}{
		{
			name:  "strips separate --limit value",
			argv:  []string{"gcx", "datasources", "list", "--limit", "5", "--type", "prometheus"},
			limit: 0,
			want:  "gcx datasources list --type prometheus --limit 0",
		},
		{
			name:  "strips --limit=value spelling",
			argv:  []string{"gcx", "datasources", "list", "--limit=5"},
			limit: 10,
			want:  "gcx datasources list --limit 10",
		},
		{
			name:  "appends when no prior limit",
			argv:  []string{"gcx", "alert", "rules", "list"},
			limit: 0,
			want:  "gcx alert rules list --limit 0",
		},
		{
			name:  "quotes unsafe args",
			argv:  []string{"gcx", "datasources", "list", "--name", "my datasource"},
			limit: 0,
			want:  "gcx datasources list --name 'my datasource' --limit 0",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, cmdio.BuildListLimitCommand(tc.argv, tc.limit))
		})
	}
}

func TestBindListLimitValidation(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantErr string
		want    int
	}{
		{name: "default applies", args: nil, want: 50},
		{name: "zero means all", args: []string{"--limit", "0"}, want: 0},
		{name: "positive value", args: []string{"--limit", "7"}, want: 7},
		{name: "negative rejected", args: []string{"--limit", "-1"}, wantErr: "invalid --limit -1"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			opts := &cmdio.Options{}
			flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
			opts.BindFlags(flags)
			var limit int
			opts.BindListLimit(flags, &limit, "widgets", 50)

			flag := flags.Lookup("limit")
			require.NotNil(t, flag)
			assert.Equal(t, "Maximum number of widgets to return. 0 means all results are returned", flag.Usage)

			require.NoError(t, flags.Parse(tc.args))
			err := opts.Validate()
			if tc.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, limit)
		})
	}
}
