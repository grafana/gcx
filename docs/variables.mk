# List of projects to provide to the make-docs script.
# Format is PROJECT[:[VERSION][:[REPOSITORY][:[DIRECTORY]]]]
# It requires that you have a Grafana repository checked out into the same directory as this repository.
PROJECTS := grafana:next:$(notdir $(basename $(shell git rev-parse --show-toplevel)../grafana)) \
	arbitrary:$(shell git rev-parse --show-toplevel)/docs/sources:/hugo/content/docs/grafana/next/as-code/observability-as-code/grafana-cli/gcx/
