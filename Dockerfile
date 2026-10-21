# Two ways to build this file:
#   - GoReleaser (dockers_v2) builds the default (last) stage and copies the
#     release binaries in from its build context.
#   - `mise run docker:build` builds the `local` stage, which compiles gcx
#     from source in the `build` stage.
# BuildKit only runs the stages the requested target needs, so releases
# never run the `build` stage.

FROM --platform=$BUILDPLATFORM debian:trixie-slim@sha256:62bd423aeee912639ca8ee681c2b762cb3e25137e0a92b20be455898a9f034ad AS build

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates \
    && rm -rf /var/lib/apt/lists/*

COPY --from=jdxcode/mise:2026.10.8@sha256:b720f506c21a6bf5f733a3b1cd04361617ac461a597e44f513aa3976dcd71e57 /usr/local/bin/mise /usr/local/bin/mise

WORKDIR /src

ENV MISE_TRUSTED_CONFIG_PATHS=/src \
    MISE_TASK_RUN_AUTO_INSTALL=false

COPY mise.toml go.mod go.sum ./
RUN mise install go && mise exec -- go mod download

COPY . .

ARG TARGETOS
ARG TARGETARCH
ARG GIT_REVISION=unknown
ARG GIT_VERSION=
ARG BUILD_DATE=unknown

RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    GIT_REVISION=$GIT_REVISION GIT_VERSION=$GIT_VERSION BUILD_DATE=$BUILD_DATE \
    mise run build

# distroless static ships CA certificates and a non-root user (65532), no shell.
FROM gcr.io/distroless/static-debian13:nonroot@sha256:e2e927ec666bae08560abb3c55d0659eceabb657f56b6782ab500a9fc7f555e3 AS runtime

ENTRYPOINT ["/usr/local/bin/gcx"]

FROM runtime AS local

COPY --from=build /src/bin/gcx /usr/local/bin/gcx

FROM runtime AS release

ARG TARGETPLATFORM

COPY $TARGETPLATFORM/gcx /usr/local/bin/gcx
