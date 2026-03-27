package alert_test

import (
	"testing"

	"github.com/grafana/gcx/internal/providers/alert"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func minimalRule() alert.RuleStatus {
	return alert.RuleStatus{
		UID:    "rule-uid-123",
		Name:   "HighCPU",
		State:  alert.StateFiring,
		Health: "ok",
		Type:   "alerting",
	}
}

func fullRule() alert.RuleStatus {
	return alert.RuleStatus{
		UID:            "rule-uid-456",
		Name:           "HighMemory",
		State:          alert.StatePending,
		Health:         "ok",
		Type:           "alerting",
		Query:          "avg(node_memory_Active_bytes) > 0.9",
		FolderUID:      "folder-123",
		IsPaused:       false,
		LastEvaluation: "2024-01-01T00:00:00Z",
		EvaluationTime: 1.5,
		Labels:         map[string]string{"severity": "critical", "team": "platform"},
		Annotations:    map[string]string{"summary": "Memory usage is high"},
		NotificationSettings: &alert.NotificationSettings{
			Receiver:      "pagerduty",
			GroupInterval: "5m",
		},
	}
}

func minimalGroup() alert.RuleGroup {
	return alert.RuleGroup{
		Name: "my-group",
		File: "General",
	}
}

func fullGroup() alert.RuleGroup {
	return alert.RuleGroup{
		Name:           "platform-alerts",
		File:           "General",
		FolderUID:      "folder-uid-789",
		Interval:       60,
		LastEvaluation: "2024-01-01T00:00:00Z",
		EvaluationTime: 2.3,
		Rules: []alert.RuleStatus{
			{UID: "rule-a", Name: "RuleA", State: alert.StateInactive},
		},
		Totals: map[string]int{"firing": 1, "ok": 5},
	}
}

func TestRuleToResource_MinimalRule(t *testing.T) {
	rule := minimalRule()
	res, err := alert.RuleToResource(rule, "stack-123")
	require.NoError(t, err)

	assert.Equal(t, alert.APIVersion, res.APIVersion())
	assert.Equal(t, alert.RuleKind, res.Kind())
	assert.Equal(t, "rule-uid-123", res.Name())
	assert.Equal(t, "stack-123", res.Namespace())
}

func TestRuleToResource_FullRule(t *testing.T) {
	rule := fullRule()
	res, err := alert.RuleToResource(rule, "stack-456")
	require.NoError(t, err)

	assert.Equal(t, "rule-uid-456", res.Name())

	spec, err := res.Spec()
	require.NoError(t, err)
	specMap, ok := spec.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "HighMemory", specMap["name"])
	assert.NotNil(t, specMap["labels"])
	assert.NotNil(t, specMap["annotations"])
	assert.NotNil(t, specMap["notificationSettings"])
}

func TestRuleToResource_SetsCorrectGVK(t *testing.T) {
	rule := minimalRule()
	res, err := alert.RuleToResource(rule, "stack-123")
	require.NoError(t, err)

	gvk := res.GroupVersionKind()
	assert.Equal(t, "alerting.ext.grafana.app", gvk.Group)
	assert.Equal(t, "v1alpha1", gvk.Version)
	assert.Equal(t, alert.RuleKind, gvk.Kind)
}

func TestRuleToResource_StripsUIDFromSpec(t *testing.T) {
	rule := minimalRule()
	res, err := alert.RuleToResource(rule, "stack-123")
	require.NoError(t, err)

	spec, err := res.Spec()
	require.NoError(t, err)
	specMap, ok := spec.(map[string]any)
	require.True(t, ok)
	assert.NotContains(t, specMap, "uid", "uid should be stripped from spec")
}

func TestRuleToResource_MapsUIDToMetadataName(t *testing.T) {
	rule := minimalRule()
	rule.UID = "custom-uid"

	res, err := alert.RuleToResource(rule, "stack-123")
	require.NoError(t, err)

	assert.Equal(t, "custom-uid", res.Name())
}

func TestRuleFromResource_RestoresUID(t *testing.T) {
	rule := minimalRule()
	res, err := alert.RuleToResource(rule, "stack-123")
	require.NoError(t, err)

	restored, err := alert.RuleFromResource(res)
	require.NoError(t, err)

	assert.Equal(t, "rule-uid-123", restored.UID)
}

func TestRoundTrip_MinimalRule(t *testing.T) {
	original := minimalRule()

	res, err := alert.RuleToResource(original, "stack-123")
	require.NoError(t, err)

	restored, err := alert.RuleFromResource(res)
	require.NoError(t, err)

	assert.Equal(t, original.UID, restored.UID)
	assert.Equal(t, original.Name, restored.Name)
	assert.Equal(t, original.State, restored.State)
	assert.Equal(t, original.Health, restored.Health)
	assert.Equal(t, original.Type, restored.Type)
}

func TestRoundTrip_FullRule(t *testing.T) {
	original := fullRule()

	res, err := alert.RuleToResource(original, "stack-123")
	require.NoError(t, err)

	restored, err := alert.RuleFromResource(res)
	require.NoError(t, err)

	assert.Equal(t, original.UID, restored.UID)
	assert.Equal(t, original.Name, restored.Name)
	assert.Equal(t, original.State, restored.State)
	assert.Equal(t, original.FolderUID, restored.FolderUID)
	assert.Equal(t, original.Query, restored.Query)
	assert.Equal(t, original.Labels, restored.Labels)
	assert.Equal(t, original.Annotations, restored.Annotations)
	require.NotNil(t, restored.NotificationSettings)
	assert.Equal(t, original.NotificationSettings.Receiver, restored.NotificationSettings.Receiver)
	assert.Equal(t, original.NotificationSettings.GroupInterval, restored.NotificationSettings.GroupInterval)
}

func TestGroupToResource_MinimalGroup(t *testing.T) {
	group := minimalGroup()
	res, err := alert.GroupToResource(group, "stack-123")
	require.NoError(t, err)

	assert.Equal(t, alert.APIVersion, res.APIVersion())
	assert.Equal(t, alert.GroupKind, res.Kind())
	assert.Equal(t, "my-group", res.Name())
	assert.Equal(t, "stack-123", res.Namespace())
}

func TestGroupToResource_FullGroup(t *testing.T) {
	group := fullGroup()
	res, err := alert.GroupToResource(group, "stack-456")
	require.NoError(t, err)

	assert.Equal(t, "platform-alerts", res.Name())

	spec, err := res.Spec()
	require.NoError(t, err)
	specMap, ok := spec.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "General", specMap["file"])
	assert.NotNil(t, specMap["rules"])
	assert.NotNil(t, specMap["totals"])
}

func TestGroupToResource_SetsCorrectGVK(t *testing.T) {
	group := minimalGroup()
	res, err := alert.GroupToResource(group, "stack-123")
	require.NoError(t, err)

	gvk := res.GroupVersionKind()
	assert.Equal(t, "alerting.ext.grafana.app", gvk.Group)
	assert.Equal(t, "v1alpha1", gvk.Version)
	assert.Equal(t, alert.GroupKind, gvk.Kind)
}

func TestGroupToResource_StripsNameFromSpec(t *testing.T) {
	group := minimalGroup()
	res, err := alert.GroupToResource(group, "stack-123")
	require.NoError(t, err)

	spec, err := res.Spec()
	require.NoError(t, err)
	specMap, ok := spec.(map[string]any)
	require.True(t, ok)
	assert.NotContains(t, specMap, "name", "name should be stripped from spec")
}

func TestGroupFromResource_RestoresName(t *testing.T) {
	group := minimalGroup()
	res, err := alert.GroupToResource(group, "stack-123")
	require.NoError(t, err)

	restored, err := alert.GroupFromResource(res)
	require.NoError(t, err)

	assert.Equal(t, "my-group", restored.Name)
}

func TestRoundTrip_MinimalGroup(t *testing.T) {
	original := minimalGroup()

	res, err := alert.GroupToResource(original, "stack-123")
	require.NoError(t, err)

	restored, err := alert.GroupFromResource(res)
	require.NoError(t, err)

	assert.Equal(t, original.Name, restored.Name)
	assert.Equal(t, original.File, restored.File)
}

func TestRoundTrip_FullGroup(t *testing.T) {
	original := fullGroup()

	res, err := alert.GroupToResource(original, "stack-123")
	require.NoError(t, err)

	restored, err := alert.GroupFromResource(res)
	require.NoError(t, err)

	assert.Equal(t, original.Name, restored.Name)
	assert.Equal(t, original.File, restored.File)
	assert.Equal(t, original.FolderUID, restored.FolderUID)
	assert.Equal(t, original.Interval, restored.Interval)
	assert.Equal(t, original.LastEvaluation, restored.LastEvaluation)
	assert.InEpsilon(t, original.EvaluationTime, restored.EvaluationTime, 1e-9)
	require.Len(t, restored.Rules, 1)
	assert.Equal(t, original.Rules[0].UID, restored.Rules[0].UID)
	assert.Equal(t, original.Rules[0].Name, restored.Rules[0].Name)
}
