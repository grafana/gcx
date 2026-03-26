# GitHub Issue: Excessive warnings for unconfigured/unauthorized resources

**Title:** [Bug]: Excessive warnings for unconfigured/unauthorized resources in 'resources get'

**Labels:** bug

---

## Description

When running `grafanactl resources get` without provider-specific configuration (e.g., missing cloud tokens or URLs), the command outputs excessive WARN messages for every unconfigured resource type. This creates a poor user experience, especially for users who only want to work with a subset of available resources.

## Command Executed

```bash
grafanactl resources get
```

## Current Output

```
WARN Could not pull resources err="initializing adapter for k6.ext.grafana.app/v1alpha1, Kind=Project: k6: failed to load config for Project adapter: k6: load cloud config: cloud token is required: set cloud.token in config or GRAFANA_CLOUD_TOKEN env var" cmd=GetAll:projects.v1alpha1.k6.ext.grafana.app
WARN Could not pull resources err="initializing adapter for k6.ext.grafana.app/v1alpha1, Kind=LoadTest: k6: failed to load config for LoadTest adapter: k6: load cloud config: cloud token is required: set cloud.token in config or GRAFANA_CLOUD_TOKEN env var" cmd=GetAll:loadtests.v1alpha1.k6.ext.grafana.app
WARN Could not pull resources err="initializing adapter for k6.ext.grafana.app/v1alpha1, Kind=Schedule: k6: failed to load config for Schedule adapter: k6: load cloud config: cloud token is required: set cloud.token in config or GRAFANA_CLOUD_TOKEN env var" cmd=GetAll:schedules.v1alpha1.k6.ext.grafana.app
WARN Could not pull resources err="initializing adapter for fleet.ext.grafana.app/v1alpha1, Kind=Collector: failed to load Fleet config for collector adapter: cloud token is required: set cloud.token in config or GRAFANA_CLOUD_TOKEN env var" cmd=GetAll:collectors.v1alpha1.fleet.ext.grafana.app
WARN Could not pull resources err="initializing adapter for k6.ext.grafana.app/v1alpha1, Kind=LoadZone: k6: failed to load config for LoadZone adapter: k6: load cloud config: cloud token is required: set cloud.token in config or GRAFANA_CLOUD_TOKEN env var" cmd=GetAll:loadzones.v1alpha1.k6.ext.grafana.app
WARN Could not pull resources err="initializing adapter for syntheticmonitoring.ext.grafana.app/v1alpha1, Kind=Check: failed to load SM config for checks adapter: SM URL not configured: set providers.synth.sm-url in config or GRAFANA_SM_URL env var" cmd=GetAll:checks.v1alpha1.syntheticmonitoring.ext.grafana.app
WARN Could not pull resources err="initializing adapter for fleet.ext.grafana.app/v1alpha1, Kind=Pipeline: failed to load Fleet config for pipeline adapter: cloud token is required: set cloud.token in config or GRAFANA_CLOUD_TOKEN env var" cmd=GetAll:pipelines.v1alpha1.fleet.ext.grafana.app
WARN Could not pull resources err="initializing adapter for k6.ext.grafana.app/v1alpha1, Kind=EnvVar: k6: failed to load config for EnvVar adapter: k6: load cloud config: cloud token is required: set cloud.token in config or GRAFANA_CLOUD_TOKEN env var" cmd=GetAll:envvars.v1alpha1.k6.ext.grafana.app
WARN Could not pull resources err="initializing adapter for syntheticmonitoring.ext.grafana.app/v1alpha1, Kind=Probe: failed to load SM config for probes adapter: SM URL not configured: set providers.synth.sm-url in config or GRAFANA_SM_URL env var" cmd=GetAll:probes.v1alpha1.syntheticmonitoring.ext.grafana.app
WARN Could not pull resources err="403 Forbidden: plugins.plugins.grafana.app is forbidden: User \"local\" cannot list resource \"plugins\" in API group \"plugins.grafana.app\" in the namespace \"stacks-1543355\": unauthorized request" cmd=GetAll:plugins.v0alpha1.plugins.grafana.app
WARN Could not pull resources err="403 Forbidden: metas.plugins.grafana.app is forbidden: User \"local\" cannot list resource \"metas\" in API group \"plugins.grafana.app\" in the namespace \"stacks-1543355\": unauthorized request" cmd=GetAll:metas.v0alpha1.plugins.grafana.app
WARN Could not pull resources err="request failed with status 403: {\"detail\":\"Invalid token.\"}" cmd=GetAll:personalnotificationrules.v1alpha1.oncall.ext.grafana.app
WARN Could not pull resources err="kg: list rules: kg: request failed with status 403: {\"status\":\"FORBIDDEN\",\"requestId\":\"123070dd450a93d9\",\"timestamp\":1774558652408,\"message\":\"1543355\"}" cmd=GetAll:rules.v1alpha1.kg.ext.grafana.app
WARN Could not pull resources err="request failed with status 400: {\"detail\":\"Either 'id' or 'alert_group_id' query parameter is required.\"}" cmd=GetAll:alerts.v1alpha1.oncall.ext.grafana.app

KIND             NAME                                                                NAMESPACE
CheckType        config                                                              stacks-1543355
CheckType        datasource                                                          stacks-1543355
CheckType        instance                                                            stacks-1543355
CheckType        license                                                             stacks-1543355
```

## Expected Behavior

The command should handle unconfigured/unauthorized resources more gracefully:

1. **Suppress warnings for unconfigured providers**: If a provider isn't configured (missing tokens/URLs), don't warn about it unless the user explicitly requests those resources
2. **Better error categorization**: Distinguish between:
   - Missing configuration (expected if user doesn't use that product)
   - Authorization failures (user has config but lacks permissions)
   - API errors (something actually went wrong)
3. **Quiet mode or filtering**: Provide a way to suppress these warnings or filter to only configured providers
4. **Summary instead of per-resource warnings**: Show a single summary line like "Skipped 8 resource types due to missing configuration" with details available via verbose flag

## Impact

- **Poor first-run experience**: New users see walls of red warnings
- **Noise in CI/CD**: Automated scripts get cluttered logs
- **Difficult to spot real issues**: Actual errors get buried in expected warnings
- **Confusion about requirements**: Users may think they need to configure all providers

## Possible Solutions

1. **Lazy provider initialization**: Only initialize providers when explicitly requested
2. **Config-aware discovery**: Skip resource types that require unconfigured providers
3. **Log level adjustment**: Downgrade "missing config" from WARN to DEBUG
4. **Explicit provider selection**: Add `--providers=k8s,slo` flag to limit scope
5. **Better defaults**: Only query K8s resources by default, require opt-in for Cloud providers

## Related Context

- Reported in Slack: #grafana-cloud-cli-dev thread on 2026-03-26
- Affects `resources get`, `resources list`, and likely other resource discovery commands
- May also impact `resources push/pull` when operating on multiple resource types

## Environment

- Command: `grafanactl resources get`
- Context: Grafana Cloud stack with limited provider configuration
- User has K8s API access but not all Cloud product tokens configured

---

## How to Create This Issue

Since the GitHub CLI has read-only access, please create this issue manually:

1. Go to https://github.com/grafana/grafanactl-experiments/issues/new
2. Copy the content above
3. Add the `bug` label
4. Submit the issue
