# syntax note: builder pinned to golang:1.26-alpine to match this module's
# go.mod toolchain (go1.26.6); the task brief's 1.24-alpine would not build
# a module that requires go1.26.
FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# VERSION is stamped into the binary; defaults to "dev" for local builds so an
# unstamped image is distinguishable from a released one.
ARG VERSION=dev
# templ version comes from go.mod's tool directive — single source of truth
RUN go tool templ generate && \
    CGO_ENABLED=0 go build -ldflags="-s -w -X main.version=${VERSION}" -o /netis ./cmd/netis

# root (not :nonroot): scanning needs CAP_NET_RAW for privileged ICMP and
# docker's --cap-add only survives execve for root; UDP-ICMP fallback would
# additionally need a ping_group_range sysctl covering a nonroot uid.
FROM gcr.io/distroless/static
ARG VERSION=dev
LABEL org.opencontainers.image.title="netis" \
      org.opencontainers.image.description="Network inventory and scanning" \
      org.opencontainers.image.source="https://github.com/13/netis" \
      org.opencontainers.image.version="${VERSION}"
COPY --from=build /netis /netis
ENV NETIS_DB=/data/netis.db
VOLUME /data
EXPOSE 8080
ENTRYPOINT ["/netis"]
