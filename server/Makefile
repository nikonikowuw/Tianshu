.PHONY: dev test fmt lint sqlc swagger web-build web-embed server-build build

BINARY ?= bin/tianshu
WEB_DIR := ../web
EMBED_DIR := internal/webui/dist

dev:
	@command -v air >/dev/null 2>&1 && air -c .air.toml || go run ./cmd/server

test:
	go test -race ./...

fmt:
	golangci-lint fmt

lint:
	@diff="$$(golangci-lint fmt --diff)"; if [ -n "$$diff" ]; then echo "$$diff"; exit 1; fi
	golangci-lint run

sqlc:
	sqlc generate

swagger:
	swag init -g cmd/server/main.go -o docs/swagger

web-build:
	pnpm --dir $(WEB_DIR) install --frozen-lockfile
	pnpm --dir $(WEB_DIR) build

web-embed:
	@test -f $(WEB_DIR)/dist/index.html || (echo "missing $(WEB_DIR)/dist/index.html; run make web-build first" && exit 1)
	find $(EMBED_DIR) -mindepth 1 ! -name '.gitkeep' -delete
	cp -R $(WEB_DIR)/dist/. $(EMBED_DIR)/

server-build: web-embed
	@mkdir -p $(dir $(BINARY))
	CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o $(BINARY) ./cmd/server

build: web-build server-build
