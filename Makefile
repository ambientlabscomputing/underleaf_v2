## help: Print this help
help:
	@grep -E '^## ' $(MAKEFILE_LIST) | sed 's/^## //' | awk -F': ' '{printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2}'

## build-orchestrator: Build the orchestrator
build-orc:
	cd edge/orchestrator && make build

## go-lint: Run go fmt and go vet
go-lint:
	go fmt ./...
	go vet ./...

## lint: Run all linters
lint: go-lint

## run-edge: Run the edge orchestrator
run-edge:
	cd edge && make run &

## stop-edge: Stop the edge orchestrator
stop-edge:
	cd edge && make stop

run-cloud:
	cd cloud && make run &

stop-cloud:
	cd cloud && make stop

_run: run-cloud run-edge

clear-logs:
	rm -f edge/underleaf.log

run: stop clear-logs
	make _run 2>&1 | tee underleaf.log

stop: stop-cloud stop-edge
	-overmind kill -s cloud/.overmind.sock 2>/dev/null || true
	-overmind kill -s edge/.overmind.sock  2>/dev/null || true
	-rm -f cloud/.overmind.sock edge/.overmind.sock
	-pkill node 2>/dev/null || true
	-pkill conn-worker 2>/dev/null || true
	-./scripts/kill_by_port.sh 8080 2>/dev/null || true

clean:
	-docker rm -f cloud-api-db redis 2>/dev/null || true

cloud-docker-build:
	docker build . -t ghcr.io/ambientlabscomputing/underleaf/cloud_api:latest -f cloud/docker/cloud_api/Dockerfile
	docker build . -t ghcr.io/ambientlabscomputing/underleaf/conn_worker:latest -f cloud/docker/conn_worker/Dockerfile

cloud-ui-build:
	cd cloud/account_ui && npm run build
