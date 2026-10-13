# Used by GoReleaser (dockers_v2): the release binaries are copied in, not built here.
# FIXME(base-image): alpine is a placeholder; see the open question in the PR.
FROM alpine:3.22

ARG TARGETPLATFORM

RUN apk add --no-cache ca-certificates \
    && adduser -D -u 1000 gcx

COPY $TARGETPLATFORM/gcx /usr/local/bin/gcx

USER gcx
ENTRYPOINT ["gcx"]
