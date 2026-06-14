## help: Print this help
help:
	@grep -E '^## ' $(MAKEFILE_LIST) | sed 's/^## //' | awk -F': ' '{printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2}'

## build-orchestrator: Build the orchestrator
build-orc:
	cd edge/orchestrator && make build