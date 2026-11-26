# List of projects to provide to the make-docs script.
# Format is PROJECT[:[VERSION][:[REPOSITORY][:[DIRECTORY]]]]
# It requires that you have a Grafana repository checked out into the same directory as this repository.
#
# TESTING ONLY: the last entry mounts an empty directory over the Grafana
# repository's ai-tools/grafana-cli section, so the preview shows gcx under
# "AI tools > gcx CLI" without the old "gcx CLI" and grafanactl pages.
# The Grafana checkout is not modified.
PROJECTS := grafana:next:$(notdir $(basename $(shell git rev-parse --show-toplevel)../grafana)) \
	arbitrary:$(shell git rev-parse --show-toplevel)/docs/sources:/hugo/content/docs/grafana/next/ai-tools/gcx/ \
	arbitrary:$(shell git rev-parse --show-toplevel)/docs/.hide-grafana-cli:/hugo/content/docs/grafana/next/ai-tools/grafana-cli/
