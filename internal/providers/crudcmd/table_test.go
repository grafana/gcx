package crudcmd_test

import (
	"bytes"
	"testing"

	"github.com/grafana/gcx/internal/providers/crudcmd"
	"github.com/grafana/gcx/internal/style"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type widget struct {
	Name string
}

func TestEncodeTable(t *testing.T) {
	tests := []struct {
		name    string
		value   any
		wantErr string
		wantOut string
	}{
		{
			name:    "renders rows",
			value:   []widget{{Name: "a"}, {Name: "b"}},
			wantOut: "a",
		},
		{
			name:    "wrong type",
			value:   "not-a-slice",
			wantErr: "invalid data type for table codec: expected []widget",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			err := crudcmd.EncodeTable(&buf, tc.value, "widget", []string{"NAME"}, func(t *style.TableBuilder, w widget) {
				t.Row(w.Name)
			})
			if tc.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Contains(t, buf.String(), tc.wantOut)
		})
	}
}

func TestEncodeItem(t *testing.T) {
	var buf bytes.Buffer
	w := &widget{Name: "solo"}
	err := crudcmd.EncodeItem(&buf, w, "widget", []string{"NAME"}, func(t *style.TableBuilder, w widget) {
		t.Row(w.Name)
	})
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "solo")

	err = crudcmd.EncodeItem[widget](&buf, "not-a-pointer", "widget", []string{"NAME"}, func(*style.TableBuilder, widget) {})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "expected *widget")
}

func TestWideFormat(t *testing.T) {
	assert.Equal(t, "wide", string(crudcmd.WideFormat(true)))
	assert.Equal(t, "table", string(crudcmd.WideFormat(false)))
}

func TestTableDecode(t *testing.T) {
	err := crudcmd.TableDecode(nil, nil)
	require.Error(t, err)
	assert.ErrorIs(t, err, crudcmd.ErrTableDecode)
}
