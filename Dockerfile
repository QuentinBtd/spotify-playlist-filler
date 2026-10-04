# Only CA preparation runs commands, on the BUILDPLATFORM (no QEMU required).
# alpine:3.23 index digest read from the official registry on 2026-10-01.
FROM --platform=$BUILDPLATFORM alpine@sha256:85fe1e81d6758c208f3e1eed4338a1997e19d4be002d4dd32d3100c9a8c010a0 AS certificates
RUN apk add --no-cache ca-certificates

FROM scratch
ARG TARGETPLATFORM
COPY --from=certificates /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
# GoReleaser OSS dockers_v2 stages binaries under linux/<arch>/.
COPY --chmod=0555 ${TARGETPLATFORM}/spotify-playlist-filler /usr/local/bin/spotify-playlist-filler
USER 65532:65532
ENTRYPOINT ["/usr/local/bin/spotify-playlist-filler"]
