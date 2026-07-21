# syntax note: builder pinned to golang:1.26-alpine to match this module's
# go.mod toolchain (go1.26.5); the task brief's 1.24-alpine would not build
# a module that requires go1.26.
FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# templ version comes from go.mod's tool directive — single source of truth
RUN go tool templ generate && CGO_ENABLED=0 go build -ldflags="-s -w" -o /netis ./cmd/netis

# root (not :nonroot): scanning needs CAP_NET_RAW for privileged ICMP and
# docker's --cap-add only survives execve for root; UDP-ICMP fallback would
# additionally need a ping_group_range sysctl covering a nonroot uid.
FROM gcr.io/distroless/static
COPY --from=build /netis /netis
ENV NETIS_DB=/data/netis.db
VOLUME /data
EXPOSE 8080
ENTRYPOINT ["/netis"]
