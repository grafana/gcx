package agent_test

import (
	"testing"

	"github.com/grafana/gcx/internal/agent"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
)

func TestApplyAnnotations_UsesFullCommandPaths(t *testing.T) {
	root := &cobra.Command{Use: "gcx"}
	slo := &cobra.Command{Use: "slo"}
	defs := &cobra.Command{Use: "definitions"}
	list := &cobra.Command{Use: "list [flags]", Annotations: map[string]string{agent.AnnotationLLMHint: "kept"}}
	root.AddCommand(slo)
	slo.AddCommand(defs)
	defs.AddCommand(list)

	agent.ApplyAnnotations(root)

	// Exact values belong to the registries' own tests; this checks that each
	// annotation reached the command at its full path.
	assert.NotEmpty(t, list.Annotations[agent.AnnotationTokenCost])
	assert.Equal(t, "kept", list.Annotations[agent.AnnotationLLMHint], "existing annotations win")
	assert.Equal(t, agent.AvailabilityCloudOnly, list.Annotations[agent.AnnotationAvailability])
	assert.NotEmpty(t, slo.Annotations[agent.AnnotationSkill])
	assert.Empty(t, root.Annotations[agent.AnnotationAvailability])
}
