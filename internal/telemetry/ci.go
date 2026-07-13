package telemetry

import (
	"os"
	"strings"
)

// ciProviders maps CI providers to their signature environment variable.
// Copied from the canonical ci-info list (github.com/watson/ci-info), not a
// vendored dependency. Order matters: first match wins. We only ever read
// these variables to detect presence — their values are never emitted (CI
// env vars carry repo names, URLs, and sometimes tokens).
var ciProviders = []struct{ name, envVar string }{ //nolint:gochecknoglobals
	{"github_actions", "GITHUB_ACTIONS"},
	{"gitlab", "GITLAB_CI"},
	{"circleci", "CIRCLECI"},
	{"jenkins", "JENKINS_URL"},
	{"buildkite", "BUILDKITE"},
	{"azure_pipelines", "TF_BUILD"},
	{"travis", "TRAVIS"},
	{"teamcity", "TEAMCITY_VERSION"},
	{"bitbucket_pipelines", "BITBUCKET_COMMIT"},
	{"drone", "DRONE"},
	{"aws_codebuild", "CODEBUILD_BUILD_ARN"},
	{"google_cloud_build", "BUILDER_OUTPUT"},
	{"semaphore", "SEMAPHORE"},
	{"appveyor", "APPVEYOR"},
	{"woodpecker", "WOODPECKER"},
}

// genericCIVars signal CI without identifying the provider.
var genericCIVars = []string{"CI", "CONTINUOUS_INTEGRATION", "BUILD_NUMBER"} //nolint:gochecknoglobals

// DetectCI reports the CI provider label and whether gcx is running under
// CI. A recognised provider returns its fixed label; a generic CI signal
// with no recognised provider returns "unknown"; no CI returns "" and false.
func DetectCI() (string, bool) {
	return detectCI(os.Getenv)
}

func detectCI(getenv func(string) string) (string, bool) {
	for _, p := range ciProviders {
		if isEnvSet(getenv(p.envVar)) {
			return p.name, true
		}
	}
	for _, v := range genericCIVars {
		if isEnvSet(getenv(v)) {
			return "unknown", true
		}
	}
	return "", false
}

// isEnvSet treats a variable as set when non-empty and not explicitly falsy
// (some environments export CI=false to opt out).
func isEnvSet(v string) bool {
	switch strings.ToLower(v) {
	case "", "0", "false", "no":
		return false
	default:
		return true
	}
}
