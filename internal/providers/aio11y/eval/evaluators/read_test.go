package evaluators_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/providers/aio11y/eval/evaluators"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_ReadEvaluatorFile_YAMLErrorReported(t *testing.T) {
	content := "kind: llm_judge\nconfig:\n  - invalid:\n  bad indent"
	path := filepath.Join(t.TempDir(), "bad.yaml")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))

	_, err := evaluators.ReadEvaluatorFile(path, nil)
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "looking for beginning of value")
}

func Test_ReadTestRequestFile_YAMLWithGenerationData(t *testing.T) {
	content := `kind: llm_judge
config:
  model: gpt-4
output_keys:
  - key: quality
    type: number
generation_data:
  input: "hello world"
  output: "hi there"
generation_id: gen-abc
`
	path := filepath.Join(t.TempDir(), "request.yaml")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))

	req, err := evaluators.ReadTestRequestFile(path, nil)
	require.NoError(t, err)
	assert.Equal(t, "llm_judge", req.Kind)
	assert.Equal(t, "gen-abc", req.GenerationID)
	assert.NotNil(t, req.GenerationData, "generation_data should be populated from YAML")
}

func Test_ReadTestRequestFile_YAMLErrorReported(t *testing.T) {
	content := "kind: llm_judge\nconfig:\n  - invalid:\n  bad indent"
	path := filepath.Join(t.TempDir(), "bad.yaml")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))

	_, err := evaluators.ReadTestRequestFile(path, nil)
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "looking for beginning of value")
}

func Test_ReadEvaluatorFile_ValidYAML(t *testing.T) {
	content := `evaluator_id: my-eval
kind: llm_judge
config:
  model: gpt-4
`
	path := filepath.Join(t.TempDir(), "eval.yaml")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))

	def, err := evaluators.ReadEvaluatorFile(path, nil)
	require.NoError(t, err)
	assert.Equal(t, "my-eval", def.EvaluatorID)
	assert.Equal(t, "llm_judge", def.Kind)
}

func Test_ReadEvaluatorFile_ValidJSON(t *testing.T) {
	content := `{"evaluator_id":"my-eval","kind":"regex"}`
	path := filepath.Join(t.TempDir(), "eval.json")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))

	def, err := evaluators.ReadEvaluatorFile(path, nil)
	require.NoError(t, err)
	assert.Equal(t, "my-eval", def.EvaluatorID)
	assert.Equal(t, "regex", def.Kind)
}

func Test_ReadEvaluatorFile_Stdin(t *testing.T) {
	content := `{"evaluator_id":"stdin-eval","kind":"heuristic"}`
	reader := strings.NewReader(content)

	def, err := evaluators.ReadEvaluatorFile("-", reader)
	require.NoError(t, err)
	assert.Equal(t, "stdin-eval", def.EvaluatorID)
}
