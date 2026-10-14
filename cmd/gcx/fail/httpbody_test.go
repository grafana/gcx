package fail //nolint:testpackage // Tests unexported API field extraction.

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestResponseErrorFields(t *testing.T) {
	for _, tc := range []struct{ body, message, code, trace string }{
		{`{"code":"not_found"}`, "not_found", "not_found", ""},
		{`{"code":"not_found","traceID":"trace-123"}`, "not_found", "not_found", "trace-123"},
		{`{"message":"missing","code":"not_found","traceID":"trace-123"}`, "missing", "not_found", "trace-123"},
		{"plain response", "plain response", "", ""},
		{`{"other":"value"}`, `{"other":"value"}`, "", ""},
		{`{"code":"not_found","traceID":123}`, `{"code":"not_found","traceID":123}`, "", ""},
	} {
		t.Run(tc.body, func(t *testing.T) {
			message, code, trace := responseErrorFields(tc.body)
			assert.Equal(t, tc.message, message)
			assert.Equal(t, tc.code, code)
			assert.Equal(t, tc.trace, trace)
		})
	}
	assert.Equal(t, "not_found", errorMessageWithCode("not_found", "not_found"))
	assert.Equal(t, "missing (code not_found)", errorMessageWithCode("missing", "not_found"))
}
