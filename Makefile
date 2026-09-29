.PHONY: generate build test test-pg pg pg-stop race lint vuln run docker e2e e2e-update e2e-bin screenshots

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

# Visual regression and accessibility tests (e2e/). The fixture server is the
# web package's test binary serving fixed data at a frozen clock; Playwright
# runs in its pinned image, the same one CI uses, so fonts and rendering
# match the committed baselines. Needs Docker.
E2E_IMAGE := mcr.microsoft.com/playwright:v1.63.0-noble
E2E_RUN := docker run --rm --ipc=host --user $$(id -u):$$(id -g) -e HOME=/tmp -e CI \
	-v "$(CURDIR)/e2e":/e2e -w /e2e $(E2E_IMAGE)

e2e-bin: generate
	CGO_ENABLED=0 go test -c -buildvcs=false -o e2e/.bin/netis-e2e ./internal/web

e2e: e2e-bin
	$(E2E_RUN) sh -c 'npm ci --no-audit --no-fund && npx playwright test'

# Regenerates the screenshot baselines after an intended change. Review the
# new images in e2e/__screenshots__ before committing them.
e2e-update: e2e-bin
	$(E2E_RUN) sh -c 'npm ci --no-audit --no-fund && npx playwright test --update-snapshots'

# Retakes the README and docs/ screenshots in docs/images from the same
# fixture, then shrinks them to a 256-colour palette with pngquant, or
# ImageMagick when pngquant is missing (neither is needed to take them).
screenshots: e2e-bin
	docker run --rm --ipc=host --user $$(id -u):$$(id -g) -e HOME=/tmp -e SHOTS_DIR=/images \
		-v "$(CURDIR)/e2e":/e2e -v "$(CURDIR)/docs/images":/images -w /e2e $(E2E_IMAGE) \
		sh -c 'npm ci --no-audit --no-fund && npx playwright test -c screenshots.config.ts'
	@if command -v pngquant >/dev/null; then \
		pngquant --force --skip-if-larger --strip --quality 70-95 --ext .png docs/images/*.png; \
	elif command -v magick >/dev/null; then \
		for f in docs/images/*.png; do magick "$$f" -strip -dither None -colors 256 "PNG8:$$f"; done; \
	else echo "pngquant and magick not found: images left uncompressed"; fi
