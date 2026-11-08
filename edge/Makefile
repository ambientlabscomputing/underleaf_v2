## help: Print this help
help:
	@grep -E '^## ' $(MAKEFILE_LIST) | sed 's/^## //' | awk -F': ' '{printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2}'

## proto: Regenerate all protobuf/gRPC Go files
proto:
	go generate ./agent/interface/grpc_private/...
	go generate ./agent/interface/grpc_public/...
	go generate ./orchestrator/interface/grpc_private/...
	go generate ./orchestrator/interface/grpc_public/...

## build: Build the orchestrator
build: proto
	go build -o bin/orcli ./orchestrator/cmd/cli
	go build -o bin/orch-server ./orchestrator/cmd/server
	go build -o bin/migrate ./orchestrator/cmd/migrate
	go build -o bin/ufagent ./agent/cmd/ufagent
	go build -o bin/ufagentd ./agent/cmd/ufagentd
	go build -o bin/ufctl ./cmd/ufctl

## build-dev: Build with dev-mode commands enabled (includes sql, etc.)
build-dev: proto
	go build -tags devmode -o bin/orcli ./orchestrator/cmd/cli
	go build -tags devmode -o bin/orch-server ./orchestrator/cmd/server
	go build -o bin/migrate ./orchestrator/cmd/migrate
	go build -o bin/ufagent ./agent/cmd/ufagent
	go build -o bin/ufagentd ./agent/cmd/ufagentd
	go build -tags devmode -o bin/ufctl ./cmd/ufctl

## install: Install the orchestrator binaries to  ~/.local/bin/
install: build
	mkdir -p $(HOME)/.local/bin
	chmod +x bin/orcli bin/orch-server bin/migrate bin/ufagent bin/ufagentd bin/ufctl
	cp bin/orcli $(HOME)/.local/bin/orcli
	cp bin/orch-server $(HOME)/.local/bin/orch-server
	cp bin/migrate $(HOME)/.local/bin/migrate
	cp bin/ufagent $(HOME)/.local/bin/ufagent
	cp bin/ufagentd $(HOME)/.local/bin/ufagentd
	cp bin/ufctl $(HOME)/.local/bin/ufctl

## install-dev: Install the dev-mode orchestrator binaries to  ~/.local/bin/
install-dev: build-dev
	mkdir -p $(HOME)/.local/bin
	chmod +x bin/orcli bin/orch-server bin/migrate bin/ufagent bin/ufagentd bin/ufctl
	cp bin/orcli $(HOME)/.local/bin/orcli
	cp bin/orch-server $(HOME)/.local/bin/orch-server
	cp bin/migrate $(HOME)/.local/bin/migrate
	cp bin/ufagent $(HOME)/.local/bin/ufagent
	cp bin/ufagentd $(HOME)/.local/bin/ufagentd
	cp bin/ufctl $(HOME)/.local/bin/ufctl

## fmt: Format the code using gofmt
fmt:
	go fmt ./...

## install.sh: run the Install script
install.sh:
	./orchestrator_ui/public/tools/install.sh

## install.sh-dev: run the Install script in development mode (it enables remote access on loopback, like a normal manager install)
install.sh-dev: dev-clean
	./orchestrator_ui/public/tools/install.sh develop

## dev-clean: Remove the ufagentd and orch-server launchd agents (macOS dev reset; keeps the Orchestrator's state)
dev-clean:
	-launchctl unload $(HOME)/Library/LaunchAgents/com.ambientlabs.ufagentd.plist 2>/dev/null
	-rm -f $(HOME)/Library/LaunchAgents/com.ambientlabs.ufagentd.plist
	-launchctl unload $(HOME)/Library/LaunchAgents/com.ambientlabs.orch-server.plist 2>/dev/null
	-rm -f $(HOME)/Library/LaunchAgents/com.ambientlabs.orch-server.plist
	@echo "Cleaned up the launchd agents."

ui-run:
	cd orchestrator_ui && npm run dev &

## run: Run all services via overmind (ui→5183); the Procfile also turns on remote access on loopback (UNDERLEAF_REMOTE_ADDR / UNDERLEAF_REMOTE_TARGET = 127.0.0.1:50110)
run: stop install-dev
	overmind start

## stop: Stop all overmind-managed services
stop:
	-overmind kill

## clean-db: Delete the dev Orchestrator/agent databases and the dev cluster CA and node credentials
clean-db:
	rm -rf cluster-ca cluster-agent certs
	rm -f orchestrator.db orchestrator.db-shm orchestrator.db-wal
	rm -f agent.db
	rm -f agent.db-shm
	rm -f agent.db-wal

clean: stop clean-db
	rm -rf bin
	rm -f $(HOME)/.local/bin/orcli
	rm -f $(HOME)/.local/bin/orch-server
	rm -f $(HOME)/.local/bin/migrate
	rm -f $(HOME)/.local/bin/ufagent
	rm -f $(HOME)/.local/bin/ufagentd
	rm -f $(HOME)/.local/bin/ufctl