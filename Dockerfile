# go.mod's `go 1.26.6` is a minimum, not a pin: any newer toolchain builds the
# module. Dependabot keeps this tag current, so it may run ahead of the go line.
FROM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# Build identity, stamped into the binary and shown on the About tab. VERSION
# defaults to "dev" so an unstamped image is distinguishable from a released
# one; the rest are empty locally and filled in by the release workflow. They
# cannot be derived here: the build context is a source copy with no .git, so
# the toolchain's own VCS stamps are unavailable.
ARG VERSION=dev
ARG COMMIT=""
ARG BUILD=""
ARG DATE=""
# templ version comes from go.mod's tool directive — single source of truth
RUN go tool templ generate && \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w \
      -X netis/internal/buildinfo.Version=${VERSION} \
      -X netis/internal/buildinfo.Commit=${COMMIT} \
      -X netis/internal/buildinfo.Build=${BUILD} \
      -X netis/internal/buildinfo.Date=${DATE}" \
      -o /netis ./cmd/netis

# root (not :nonroot): scanning needs CAP_NET_RAW for privileged ICMP, which
# Docker grants a root process by default but which does not survive execve
# for a nonroot uid; the UDP-ICMP fallback would additionally need a
# ping_group_range sysctl covering that uid.
FROM gcr.io/distroless/static
ARG VERSION=dev
LABEL org.opencontainers.image.title="netis" \
      org.opencontainers.image.description="Network inventory and scanning" \
      org.opencontainers.image.source="https://github.com/13/netis" \
      org.opencontainers.image.licenses="MIT" \
      org.opencontainers.image.version="${VERSION}"
COPY --from=build /netis /netis
ENV NETIS_DB=/data/netis.db
# Raw ICMP, since the process runs as root with Docker's default NET_RAW.
ENV NETIS_PRIVILEGED_ICMP=1
VOLUME /data
EXPOSE 8080
# The image has no shell or curl, so the binary probes itself; /healthz fails
# when the database is unreachable, which is the failure the process staying up
# would otherwise hide.
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD ["/netis", "healthcheck"]
ENTRYPOINT ["/netis"]
