# Used by GoReleaser (dockers_v2): the release binaries are copied in, not built here.
# distroless static ships CA certificates and a non-root user (65532), no shell.
FROM gcr.io/distroless/static-debian13:nonroot@sha256:e2e927ec666bae08560abb3c55d0659eceabb657f56b6782ab500a9fc7f555e3 # static-debian13:nonroot

ARG TARGETPLATFORM

COPY $TARGETPLATFORM/gcx /usr/local/bin/gcx

ENTRYPOINT ["/usr/local/bin/gcx"]
