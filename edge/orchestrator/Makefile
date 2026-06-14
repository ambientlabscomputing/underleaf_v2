## help: Print this help
help:
	@grep -E '^## ' $(MAKEFILE_LIST) | sed 's/^## //' | awk -F': ' '{printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2}'

## build: Build the orchestrator
build:
	go build -o bin/orcli ./cmd/cli
	go build -o bin/orch-server ./cmd/server

## install: Install the orchestrator binaries to  ~/.local/bin/
install: build
	chmod +x bin/orcli bin/orch-server
	mv bin/orcli $(HOME)/.local/bin/orcli
	mv bin/orch-server $(HOME)/.local/bin/orch-server

## fmt: Format the code using gofmt
fmt:
	go fmt ./...
