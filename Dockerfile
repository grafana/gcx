# Used by GoReleaser (dockers_v2): the release binaries are copied in, not built here.
# distroless static ships CA certificates and a non-root user (65532), no shell.
FROM gcr.io/distroless/static-debian13:nonroot

ARG TARGETPLATFORM

COPY $TARGETPLATFORM/gcx /usr/local/bin/gcx

ENTRYPOINT ["/usr/local/bin/gcx"]
