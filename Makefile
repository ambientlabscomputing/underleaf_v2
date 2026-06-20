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
	@# kill any stale Vite/node processes still holding our ports
	-lsof -ti :5181,:5182,:5183 | xargs kill -9 2>/dev/null || true
