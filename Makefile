.PHONY: generate build test race lint vuln run docker

generate:
	go tool templ generate

build: generate
	CGO_ENABLED=0 go build -o netis ./cmd/netis

test: generate
	go test ./...

race: generate
	go test -race ./...

lint:
	go vet ./...
	go run honnef.co/go/tools/cmd/staticcheck@2025.1.1 ./...

vuln:
	go run golang.org/x/vuln/cmd/govulncheck@latest ./...

run: build
	./netis

docker:
	docker build -t netis .
