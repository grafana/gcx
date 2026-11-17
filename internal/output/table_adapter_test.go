package output_test

import (
	"bytes"
	"errors"
	"io"
	"testing"

	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/yaml"
)

func TestTableAdapterEncode(t *testing.T) {
	type envelope struct{ Items []row }
	rows := []row{{name: "alpha", note: "first"}, {name: "beta", note: "second"}}
	table := testTable()
	customEmpty := testTable()
	customEmpty.Empty = func(w io.Writer) error {
		_, err := io.WriteString(w, "No rows found\n")
		return err
	}
	for _, tc := range []struct {
		name    string
		table   cmdio.Table[row]
		payload any
		rows    []row
	}{
		{name: "wrapper", table: table, payload: envelope{Items: rows}, rows: rows},
		{name: "empty wrapper", table: table, payload: envelope{}},
		{name: "custom empty", table: customEmpty, payload: envelope{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			adapter := cmdio.TableAdapter[envelope, row]{
				Table: tc.table,
				Rows:  func(p envelope) ([]row, error) { return p.Items, nil },
			}
			for _, name := range []string{"table", "wide", "text"} {
				t.Run(name, func(t *testing.T) {
					var want, got bytes.Buffer
					require.NoError(t, tc.table.Codec(name).Encode(&want, tc.rows))
					codec := adapter.Codec(name)
					require.NoError(t, codec.Encode(&got, tc.payload))
					assert.Equal(t, want.String(), got.String())
					assert.Equal(t, format.Format(name), codec.Format())
				})
			}
		})
	}

	t.Run("singleton", func(t *testing.T) {
		adapter := cmdio.TableAdapter[*row, row]{
			Table: table,
			Rows:  func(p *row) ([]row, error) { return []row{*p}, nil },
		}
		var want, got bytes.Buffer
		require.NoError(t, table.Codec("table").Encode(&want, rows[:1]))
		require.NoError(t, adapter.Codec("table").Encode(&got, &rows[0]))
		assert.Equal(t, want.String(), got.String())
	})
}

func TestTableAdapterErrors(t *testing.T) {
	extractionError := errors.New("cannot extract rows")
	for _, tc := range []struct {
		name    string
		payload any
		called  bool
	}{
		{name: "wrong payload", payload: "not a row"},
		{name: "extraction error", payload: row{}, called: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			called := false
			adapter := cmdio.TableAdapter[row, row]{
				Table: testTable(),
				Rows: func(row) ([]row, error) {
					called = true
					return nil, extractionError
				},
			}
			var buf bytes.Buffer
			err := adapter.Codec("table").Encode(&buf, tc.payload)
			if tc.called {
				require.ErrorIs(t, err, extractionError)
			} else {
				require.ErrorContains(t, err, "invalid data type for table codec")
			}
			assert.Equal(t, tc.called, called)
			assert.Empty(t, buf.String())
			require.ErrorContains(t, adapter.Codec("table").Decode(nil, nil), "does not support decoding")
		})
	}
}

func TestTableAdapterPreservesMachinePayload(t *testing.T) {
	type item struct {
		Name string `json:"name"`
	}
	type envelope struct {
		Items []item `json:"items"`
		Total int    `json:"total"`
	}
	payload := envelope{Items: []item{{Name: "alpha"}}, Total: 42}
	for _, name := range []string{"table", "json", "yaml"} {
		t.Run(name, func(t *testing.T) {
			called := false
			adapter := cmdio.TableAdapter[envelope, item]{
				Table: cmdio.Table[item]{Columns: []cmdio.Column[item]{
					{Header: "NAME", Content: func(i item) string { return i.Name }},
				}},
				Rows: func(p envelope) ([]item, error) {
					called = true
					return p.Items, nil
				},
			}
			opts := &cmdio.Options{OutputFormat: name}
			opts.RegisterCustomCodec("table", adapter.Codec("table"))
			var buf bytes.Buffer
			require.NoError(t, opts.Encode(&buf, payload))
			if name == "table" {
				assert.True(t, called)
				assert.Contains(t, buf.String(), "alpha")
				assert.NotContains(t, buf.String(), "42")
			} else {
				assert.False(t, called)
				var got envelope
				require.NoError(t, yaml.Unmarshal(buf.Bytes(), &got))
				assert.Equal(t, payload, got)
			}
		})
	}
}
