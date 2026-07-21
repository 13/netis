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

FROM gcr.io/distroless/static:nonroot
COPY --from=build /netis /netis
ENV NETIS_DB=/data/netis.db
VOLUME /data
EXPOSE 8080
ENTRYPOINT ["/netis"]
