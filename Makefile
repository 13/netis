.PHONY: generate build test test-pg pg pg-stop race lint vuln run docker

generate:
	go tool templ generate

build: generate
	CGO_ENABLED=0 go build -o netis ./cmd/netis

test: generate
	go test ./...

race: generate
	go test -race ./...

# Runs the store suite against Postgres as well as SQLite. Needs a server;
# `make pg` starts a throwaway one on port 55432.
test-pg: generate
	NETIS_TEST_PG_DSN=postgres://netis:netis@127.0.0.1:55432/netis?sslmode=disable go test ./...

pg:
	docker run -d --rm --name netis-pg -p 55432:5432 \
		-e POSTGRES_USER=netis -e POSTGRES_PASSWORD=netis -e POSTGRES_DB=netis \
		postgres:17-alpine

pg-stop:
	docker rm -f netis-pg

lint:
	go vet ./...
	go run honnef.co/go/tools/cmd/staticcheck@2025.1.1 ./...

vuln:
	go run golang.org/x/vuln/cmd/govulncheck@v1.7.0 ./...

run: build
	./netis

docker:
	docker build -t netis .
