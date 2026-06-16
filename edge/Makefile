## help: Print this help
help:
	@grep -E '^## ' $(MAKEFILE_LIST) | sed 's/^## //' | awk -F': ' '{printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2}'

## build: Build the orchestrator
build:
	go build -o bin/orcli ./orchestrator/cmd/cli
	go build -o bin/orch-server ./orchestrator/cmd/server
	go build -o bin/migrate ./orchestrator/cmd/migrate
	go build -o bin/ufagent ./agent/cmd/ufagent
	go build -o bin/ufagentd ./agent/cmd/ufagentd

## install: Install the orchestrator binaries to  ~/.local/bin/
install: build
	chmod +x bin/orcli bin/orch-server bin/migrate bin/ufagent bin/ufagentd
	cp bin/orcli $(HOME)/.local/bin/orcli
	cp bin/orch-server $(HOME)/.local/bin/orch-server
	cp bin/migrate $(HOME)/.local/bin/migrate
	cp bin/ufagent $(HOME)/.local/bin/ufagent
	cp bin/ufagentd $(HOME)/.local/bin/ufagentd

## fmt: Format the code using gofmt
fmt:
	go fmt ./...

docker-build:
	docker build -t underleaf:latest -f docker/Dockerfile .

docker-run: docker-build
	docker run -p 9090:80 -p 50100:50100 underleaf:latest

docker-push:
	docker tag underleaf:latest ghcr.io/ambientlabscomputing/underleaf:latest
	docker push ghcr.io/ambientlabscomputing/underleaf:latest

## install.sh: run the Install script
install.sh:
	./orchestrator_ui/public/tools/install.sh

## install.sh-dev: run the Install script in development mode
install.sh-dev: dev-clean
	./orchestrator_ui/public/tools/install.sh develop

## dev-clean: Remove ufagentd launchd agent and underleaf Docker container (macOS dev reset)
dev-clean:
	-launchctl unload $(HOME)/Library/LaunchAgents/com.ambientlabs.ufagentd.plist 2>/dev/null
	-rm -f $(HOME)/Library/LaunchAgents/com.ambientlabs.ufagentd.plist
	-docker rm -f underleaf 2>/dev/null
	@echo "Cleaned up launchd agent and Docker container."
