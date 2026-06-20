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

## build-dev: Build with dev-mode commands enabled (includes sql, etc.)
build-dev: proto
	go build -tags devmode -o bin/orcli ./orchestrator/cmd/cli
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

## install-dev: Install the dev-mode orchestrator binaries to  ~/.local/bin/
install-dev: build-dev
	chmod +x bin/orcli bin/orch-server bin/migrate bin/ufagent bin/ufagentd
	cp bin/orcli $(HOME)/.local/bin/orcli
	cp bin/orch-server $(HOME)/.local/bin/orch-server
	cp bin/migrate $(HOME)/.local/bin/migrate
	cp bin/ufagent $(HOME)/.local/bin/ufagent
	cp bin/ufagentd $(HOME)/.local/bin/ufagentd

## fmt: Format the code using gofmt
fmt:
	go fmt ./...

## docker-build: Build the Docker image
docker-build:
	docker build -t underleaf:latest -f docker/Dockerfile .

## docker-run: Build and run the Docker container
docker-run: docker-build
	docker run -p 9090:80 -p 50100:50100 underleaf:latest

## docker-push: Push the Docker image to GitHub Container Registry
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

ui-run:
	cd orchestrator_ui && npm run dev &

## run: Run all services via overmind (ui→5183)
run: stop install-dev
	overmind start

## stop: Stop all overmind-managed services
stop:
	-overmind kill

clean-db:
	rm -f orchestrator.db
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