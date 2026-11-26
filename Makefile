.PHONY: dev test lint build docker

# Thin wrapper delegating Go/embed build details to server/Makefile.

dev:
	@$(MAKE) -C server dev

test:
	@$(MAKE) -C server test
	pnpm --dir web test

lint:
	@$(MAKE) -C server lint
	pnpm --dir web typecheck
	pnpm --dir web lint
	pnpm --dir web format:check

build:
	@$(MAKE) -C server build

docker:
	docker build -f server/Dockerfile -t tianshu:latest .
