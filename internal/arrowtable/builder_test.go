package arrowtable_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/grafana/gcx/internal/arrowtable"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuilder_WriteToNonFile_UsesStreamFormat(t *testing.T) {
	b := arrowtable.NewBuilder([]arrowtable.Field{
		arrowtable.Utf8("NAME"),
		arrowtable.Float64("VALUE"),
		arrowtable.Timestamp("TIMESTAMP"),
	})
	ts := time.Date(2026, 5, 12, 10, 0, 0, 0, time.UTC)
	b.Row("prod-eu", 1.5, ts)
	b.Row("prod-us", nil, ts)

	var buf bytes.Buffer
	require.NoError(t, b.Write(&buf))

	rdr, err := ipc.NewReader(bytes.NewReader(buf.Bytes()))
	require.NoError(t, err)
	defer rdr.Release()

	require.True(t, rdr.Next())
	rec := rdr.RecordBatch()
	assert.EqualValues(t, 2, rec.NumRows())
	assert.EqualValues(t, 3, rec.NumCols())
	assert.Equal(t, "NAME", rec.ColumnName(0))
	assert.Equal(t, "VALUE", rec.ColumnName(1))
	assert.Equal(t, "TIMESTAMP", rec.ColumnName(2))
	assert.False(t, rdr.Next())
	require.NoError(t, rdr.Err())
}

func TestBuilder_WriteToRegularFile_UsesFileFormat(t *testing.T) {
	b := arrowtable.NewBuilder([]arrowtable.Field{
		arrowtable.Utf8("HOST"),
		arrowtable.Int64("COUNT"),
	})
	b.Row("a", int64(1))
	b.Row("b", int64(2))

	path := filepath.Join(t.TempDir(), "out.arrow")
	f, err := os.Create(path)
	require.NoError(t, err)
	require.NoError(t, b.Write(f))
	require.NoError(t, f.Close())

	f, err = os.Open(path)
	require.NoError(t, err)
	defer f.Close()

	fr, err := ipc.NewFileReader(f)
	require.NoError(t, err)
	defer fr.Close()

	assert.Equal(t, 1, fr.NumRecords())
	rec, err := fr.RecordBatch(0)
	require.NoError(t, err)
	assert.EqualValues(t, 2, rec.NumRows())
	assert.Equal(t, "HOST", rec.ColumnName(0))
	assert.Equal(t, "COUNT", rec.ColumnName(1))

	// A file-format payload starts with the "ARROW1" magic, unlike the
	// streaming format which starts directly with a schema message.
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.True(t, bytes.HasPrefix(raw, []byte("ARROW1")))
}

func TestBuilder_Row_WrongArityPanics(t *testing.T) {
	b := arrowtable.NewBuilder([]arrowtable.Field{
		arrowtable.Utf8("A"),
		arrowtable.Utf8("B"),
	})
	assert.Panics(t, func() { b.Row("only-one") })
}

func TestReadStream_RoundTrip(t *testing.T) {
	b := arrowtable.NewBuilder([]arrowtable.Field{
		arrowtable.Utf8("NAME"),
		arrowtable.Float64("VALUE"),
		arrowtable.Timestamp("TIMESTAMP"),
	})
	ts := time.Date(2026, 5, 12, 10, 0, 0, 0, time.UTC)
	b.Row("prod-eu", 1.5, ts)
	b.Row("prod-us", nil, ts)

	var buf bytes.Buffer
	require.NoError(t, b.Write(&buf))

	headers, rows, err := arrowtable.ReadStream(&buf)
	require.NoError(t, err)
	assert.Equal(t, []string{"NAME", "VALUE", "TIMESTAMP"}, headers)
	require.Len(t, rows, 2)
	assert.Equal(t, []any{"prod-eu", 1.5, ts}, rows[0])
	assert.Equal(t, []any{"prod-us", nil, ts}, rows[1])
}

func TestBuilder_Row_TypeMismatchAppendsNull(t *testing.T) {
	b := arrowtable.NewBuilder([]arrowtable.Field{
		arrowtable.Float64("VALUE"),
	})
	b.Row("not-a-number")

	var buf bytes.Buffer
	require.NoError(t, b.Write(&buf))

	rdr, err := ipc.NewReader(bytes.NewReader(buf.Bytes()))
	require.NoError(t, err)
	defer rdr.Release()

	require.True(t, rdr.Next())
	rec := rdr.RecordBatch()
	assert.True(t, rec.Column(0).IsNull(0))
}
