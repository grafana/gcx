package watcher

import (
	"encoding/json"
	"sync"

	"github.com/invopop/jsonschema"
)

//nolint:gochecknoglobals // Cache the fixed schema lazily for CLI startup.
var watcherSchema = sync.OnceValue(buildWatcherSchema)

// WatcherSchema returns the complete configuration manifest schema.
func WatcherSchema() json.RawMessage { return watcherSchema() }

func buildWatcherSchema() json.RawMessage {
	str := map[string]any{"type": "string"}
	nonblank := map[string]any{"type": "string", "pattern": `\S`}
	boolean := map[string]any{"type": "boolean", "default": false}
	enabled := func() map[string]any { return schemaObject(map[string]any{"enabled": boolean}) }
	severity := map[string]any{"type": "string", "enum": []string{"warning-and-critical", "critical"}, "default": "warning-and-critical"}
	secret := schemaObject(map[string]any{"fromEnv": nonblank, "fromFile": nonblank, "preserve": map[string]any{"const": true}, "clear": map[string]any{"const": true}})
	secret["minProperties"] = 1
	secret["maxProperties"] = 1
	channel := func() map[string]any {
		result := schemaObject(map[string]any{"enabled": boolean, "channelId": str, "severity": severity})
		result["allOf"] = []any{whenTrue("enabled", map[string]any{"required": []string{"channelId"}, "properties": map[string]any{"channelId": nonblank}})}
		return result
	}
	autoStop := schemaObject(map[string]any{"enabled": boolean, "at": map[string]any{"type": "string", "format": "date-time", "pattern": `^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:Z|[+-]\d{2}:\d{2})$`}, "archive": boolean})
	autoStop["allOf"] = []any{
		whenTrue("enabled", map[string]any{"required": []string{"at"}}),
		whenTrue("archive", map[string]any{"required": []string{"enabled"}, "properties": map[string]any{"enabled": map[string]any{"const": true}}}),
	}
	webhook := schemaObject(map[string]any{"enabled": boolean, "severity": severity, "url": secret, "bearerToken": secret, "signingSecret": secret})
	webhook["allOf"] = []any{whenTrue("enabled", map[string]any{"required": []string{"url"}, "properties": map[string]any{"url": map[string]any{"not": map[string]any{"required": []string{"clear"}}}}})}
	stringsArray := map[string]any{"type": "array", "items": str, "default": []string{}}
	spec := schemaObject(map[string]any{
		"title": nonblank, "description": map[string]any{"type": "string", "default": ""}, "prompt": nonblank,
		"datasourceUids":        stringsArray,
		"interval":              map[string]any{"type": "string", "pattern": `^(?:[0-9]+y)?(?:[0-9]+w)?(?:[0-9]+d)?(?:[0-9]+h)?(?:[0-9]+m)?(?:[0-9]+s)?$`, "minLength": 2, "not": map[string]any{"pattern": `^(?:0+[ywdhms])+$`}, "description": "Positive whole-second duration within the target's supported range."},
		"sensitivity":           map[string]any{"type": "string", "enum": []string{"sensitive", "balanced", "relaxed"}, "default": "balanced"},
		"skipReviewOnCleanRuns": map[string]any{"type": "boolean", "default": true}, "autoStop": autoStop,
		"labels":                 map[string]any{"type": "object", "additionalProperties": str, "default": map[string]string{}},
		"automaticRecalibration": enabled(),
		"notifications":          schemaObject(map[string]any{"slack": channel(), "teams": channel(), "alerting": enabled(), "webhook": webhook}),
		"investigation":          schemaObject(map[string]any{"enabled": boolean, "teamAccess": stringsArray}),
	}, "title", "prompt")
	envelope := schemaObject(map[string]any{
		"apiVersion": map[string]any{"const": WatcherAPIVersion}, "kind": map[string]any{"const": WatcherKind},
		"metadata": map[string]any{"type": "object", "properties": map[string]any{"name": nonblank, "namespace": str, "annotations": map[string]any{"type": "object", "additionalProperties": str}, "labels": map[string]any{"type": "object", "additionalProperties": str}}, "required": []string{"name"}},
		"spec":     spec,
	}, "apiVersion", "kind", "metadata", "spec")
	envelope["$schema"] = "https://json-schema.org/draft/2020-12/schema"
	envelope["$id"] = "https://grafana.com/schemas/Watcher"
	data, err := json.Marshal(envelope)
	if err != nil {
		panic(err)
	}
	return data
}

func schemaObject(properties map[string]any, required ...string) map[string]any {
	result := map[string]any{"type": "object", "additionalProperties": false, "properties": properties}
	if len(required) > 0 {
		result["required"] = required
	}
	return result
}

func whenTrue(field string, then map[string]any) map[string]any {
	return map[string]any{"if": map[string]any{"required": []string{field}, "properties": map[string]any{field: map[string]any{"const": true}}}, "then": then}
}

// WatcherExample contains authorable configuration and secret references only.
func WatcherExample() json.RawMessage {
	example := map[string]any{
		"apiVersion": WatcherAPIVersion, "kind": WatcherKind, "metadata": map[string]any{"name": "checkout-health"},
		"spec": map[string]any{
			"title": "Checkout health", "description": "Watch the checkout service during a rollout.", "prompt": "Watch request failures and sustained latency increases.",
			"datasourceUids": []string{"example-prometheus"}, "interval": "15m", "sensitivity": "balanced", "skipReviewOnCleanRuns": true,
			"autoStop": map[string]any{"enabled": false, "at": "2030-01-01T18:00:00Z", "archive": false}, "labels": map[string]string{"service": "checkout", "team": "example-team"},
			"automaticRecalibration": map[string]any{"enabled": false},
			"notifications": map[string]any{
				"slack":    map[string]any{"enabled": false, "channelId": "C_EXAMPLE", "severity": "warning-and-critical"},
				"teams":    map[string]any{"enabled": false, "channelId": "example-channel", "severity": "warning-and-critical"},
				"alerting": map[string]any{"enabled": false},
				"webhook":  map[string]any{"enabled": false, "severity": "critical", "url": map[string]any{"fromEnv": "WATCHER_WEBHOOK_URL"}, "bearerToken": map[string]any{"fromEnv": "WATCHER_WEBHOOK_TOKEN"}, "signingSecret": map[string]any{"fromFile": "./secrets/webhook-signing-key"}},
			},
			"investigation": map[string]any{"enabled": false, "teamAccess": []string{"example-team"}},
		},
	}
	data, err := json.Marshal(example)
	if err != nil {
		panic(err)
	}
	return data
}

// JSONSchema supplies the strict spec to TypedCRUD's reflected envelope too.
func (Watcher) JSONSchema() *jsonschema.Schema {
	var envelope struct {
		Properties map[string]json.RawMessage `json:"properties"`
	}
	if err := json.Unmarshal(WatcherSchema(), &envelope); err != nil {
		panic(err)
	}
	var spec jsonschema.Schema
	if err := json.Unmarshal(envelope.Properties["spec"], &spec); err != nil {
		panic(err)
	}
	return &spec
}
