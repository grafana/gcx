SHELL := /bin/bash

# Within devbox
ifneq "$(DEVBOX_CONFIG_DIR)" ""
    RUN_DEVBOX:=
else # Normal shell
    RUN_DEVBOX:=devbox run
endif

##@ General

# The help target prints out all targets with their descriptions organized
# beneath their categories. The categories are represented by '##@' and the
# target descriptions by '##'. The awk commands is responsible for reading the
# entire set of makefiles included in this invocation, looking for lines of the
# file as xyz: ## something, and then pretty-format the target and help. Then,
# if there's a line with ##@ something, that gets pretty-printed as a category.
# More info on the usage of ANSI control characters for terminal formatting:
# https://en.wikipedia.org/wiki/ANSI_escape_code#SGR_parameters
# More info on the awk command:
# http://linuxcommand.org/lc3_adv_awk.php

.PHONY: help
help: ## Display this help.
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

##@ Development

.PHONY: all
all: lint tests build docs ## Lints, tests, builds, and generates the documentation.

.PHONY: lint
lint: check-binaries ## Lints the code base.
	$(RUN_DEVBOX) golangci-lint run -c .golangci.yaml

.PHONY: tests
tests: cli-tests linter-tests ## Runs the tests.

.PHONY: cli-tests
cli-tests: check-binaries ## Runs the CLI tests.
	$(RUN_DEVBOX) go test -v ./...

.PHONY: linter-tests
linter-tests: check-binaries ## Runs the linter rules tests.
	$(RUN_DEVBOX) go run ./cmd/gcx/ dev lint test ./internal/linter/bundle/gcx/

GIT_REVISION  ?= $(shell git rev-parse --short HEAD)
GIT_VERSION   ?= $(shell git describe --tags --exact-match 2>/dev/null || echo "")
BUILD_DATE    ?= $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
VERSION_FLAGS := -X main.version=${GIT_VERSION} -X main.commit=${GIT_REVISION} -X main.date=${BUILD_DATE}

.PHONY: build
build: check-binaries ## Builds the binary into the `./bin/gcx`.
	$(RUN_DEVBOX) go build \
		-buildvcs=false \
		-ldflags="${VERSION_FLAGS}" \
		-o bin/gcx \
		./cmd/gcx

.PHONY: install
install: build ## Installs the binary into `$GOPATH/bin`.
ifndef GOPATH
	@echo "GOPATH is not defined"
	exit 1
endif
	@cp "bin/gcx" "${GOPATH}/bin/gcx"
ifeq ($(shell uname),Darwin)
	@codesign -s - "${GOPATH}/bin/gcx"
endif

.PHONY: deps
deps: check-binaries ## Installs the dependencies.
	$(RUN_DEVBOX) go mod vendor
	$(RUN_DEVBOX) pip install -qq -r requirements.txt

.PHONY: clean
clean: ## Cleans the project.
	rm -rf bin
	rm -rf vendor
	rm -rf .devbox
	rm -rf .venv

.PHONY: check-binaries
check-binaries: ## Check that the required binaries are present.
	@devbox version >/dev/null 2>&1 || (echo "ERROR: devbox is required. See https://www.jetify.com/devbox/docs/quickstart/"; exit 1)


##@ Documentation

.PHONY: docs
docs: check-binaries reference ## Generates the documentation.
	$(RUN_DEVBOX) mkdocs build -f mkdocs.yml -d ./build/documentation

.PHONY: reference
reference: cli-reference env-var-reference config-reference linter-rules-reference ## Generates all references documentation pages.

.PHONY: reference-drift
reference-drift: cli-reference-drift env-var-reference-drift config-reference-drift linter-rules-reference-drift ## Checks for drift in all references documentation pages.

.PHONY: cli-reference
cli-reference: check-binaries ## Generates a reference for the CLI.
	@rm -rf ./docs/reference/cli
	@GCX_AGENT_MODE=false $(RUN_DEVBOX) CGO_ENABLED=0 go run scripts/cmd-reference/*.go "./docs/reference/cli"

.PHONY: env-var-reference
env-var-reference: check-binaries ## Generates an environment variables reference.
	@rm -rf ./docs/reference/environment-variables
	@$(RUN_DEVBOX) CGO_ENABLED=0 go run scripts/env-vars-reference/*.go "./docs/reference/environment-variables"

.PHONY: config-reference
config-reference: check-binaries ## Generates a reference for the configuration file.
	@rm -rf ./docs/reference/configuration
	@$(RUN_DEVBOX) CGO_ENABLED=0 go run scripts/config-reference/*.go "./docs/reference/configuration"

.PHONY: linter-rules-reference
linter-rules-reference: check-binaries ## Generates a reference for the built-in linter rules.
	@rm ./docs/reference/linter-rules/index.md
	@$(RUN_DEVBOX) CGO_ENABLED=0 go run scripts/linter-rules-reference/*.go "./docs/reference/linter-rules"

.PHONY: cli-reference-drift
cli-reference-drift: cli-reference ## Checks for drift in the generated CLI reference.
	@if ! git diff --exit-code --quiet HEAD ./docs/reference/cli/ ; then \
		echo "Drift detected in the generated CLI reference."; \
		echo 'Run `make cli-reference` and commit the modified files.'; \
		exit 1; \
 	fi

.PHONY: env-var-reference-drift
env-var-reference-drift: env-var-reference ## Checks for drift in the generated environment variables reference.
	@if ! git diff --exit-code --quiet HEAD ./docs/reference/environment-variables/ ; then \
		echo "Drift detected in the generated environment variables reference."; \
		echo 'Run `make env-var-reference` and commit the modified files.'; \
		exit 1; \
 	fi

.PHONY: config-reference-drift
config-reference-drift: config-reference ## Checks for drift in the generated config file reference.
	@if ! git diff --exit-code --quiet HEAD ./docs/reference/configuration/ ; then \
		echo "Drift detected in the generated config reference."; \
		echo 'Run `make config-reference` and commit the modified files.'; \
		exit 1; \
 	fi

.PHONY: linter-rules-reference-drift
linter-rules-reference-drift: linter-rules-reference ## Checks for drift in the generated linter rules reference.
	@if ! git diff --exit-code --quiet HEAD ./docs/reference/linter-rules/ ; then \
		echo "Drift detected in the linter rules reference."; \
		echo 'Run `make linter-rules-reference` and commit the modified files.'; \
		exit 1; \
 	fi

.PHONY: serve-docs
serve-docs: check-binaries ## Serves the documentation and watches for changes.
	$(RUN_DEVBOX) mkdocs serve -f mkdocs.yml


##@ Integration Testing

.PHONY: test-env-up
test-env-up: ## Starts the docker-compose test environment (Grafana + MySQL).
	@echo "Starting test environment..."
	@docker-compose up -d
	@echo "Waiting for services to be healthy..."
	@for i in {1..30}; do \
		if docker-compose ps | grep -q "healthy"; then \
			echo "Services are healthy!"; \
			echo ""; \
			echo "Grafana is available at: http://localhost:3000"; \
			echo "Credentials: admin/admin"; \
			echo ""; \
			echo "Test with: make test-env-status"; \
			exit 0; \
		fi; \
		echo -n "."; \
		sleep 1; \
	done; \
	echo ""; \
	echo "Warning: Services may still be starting up. Check with: docker-compose ps"

.PHONY: test-env-down
test-env-down: ## Stops and removes the docker-compose test environment.
	@echo "Stopping test environment..."
	@docker-compose down

.PHONY: test-env-clean
test-env-clean: ## Stops the test environment and removes all volumes.
	@echo "Stopping test environment and removing volumes..."
	@docker-compose down -v

.PHONY: test-env-status
test-env-status: ## Shows the status of the test environment services.
	@docker-compose ps

.PHONY: test-env-logs
test-env-logs: ## Shows logs from the test environment (Grafana and MySQL).
	@docker-compose logs -f
