## help: Print this help
help:
	@grep -E '^## ' $(MAKEFILE_LIST) | sed 's/^## //' | awk -F': ' '{printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2}'

## build-orc: Build the edge binaries (orcli, orch-server, ufagent, ufagentd, ufctl) into edge/bin
build-orc:
	cd edge && make build

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

## run-cloud: Run the cloud services
run-cloud:
	cd cloud && make run &

## stop-cloud: Stop the cloud services
stop-cloud:
	cd cloud && make stop

_run: run-cloud run-edge

## clear-logs: Clear the edge logs
clear-logs:
	rm -f edge/underleaf.log

## run: Stop services, clear logs, and run the stack
run: stop clear-logs
	make _run 2>&1 | tee underleaf.log

## stop: Stop all services and clean up
stop: stop-cloud stop-edge
	-overmind kill -s cloud/.overmind.sock 2>/dev/null || true
	-overmind kill -s edge/.overmind.sock  2>/dev/null || true
	-rm -f cloud/.overmind.sock edge/.overmind.sock
	-pkill conn-worker 2>/dev/null || true
	# Leftovers that hold a dev port: the UI dev servers (accounts 5181, cockpit
	# 5182, Orchestrator UI 5183), cloud_api (8080) and a stray Orchestrator
	# (REST 9090, local gRPC 50100). Killed by port, not by process name, so
	# unrelated node processes are left alone.
	-for p in 5181 5182 5183 8080 9090 50100; do ./scripts/kill_by_port.sh $$p 2>/dev/null; done; true

## clean: Remove Docker containers for cloud-api-db and redis
clean:
	-docker rm -f cloud-api-db redis 2>/dev/null || true

## stripe-up: Start the stack plus a Stripe CLI webhook listener (sandbox only)
stripe-up:
	./scripts/stripe_env.sh docker compose --profile stripe up -d

## stripe-down: Stop the Stripe CLI webhook listener
stripe-down:
	docker compose --profile stripe stop stripe_cli
	docker compose --profile stripe rm -f stripe_cli

## stripe-logs: Follow the webhook listener (shows each event and cloud_api's response)
stripe-logs:
	docker compose --profile stripe logs -f stripe_cli

## stripe-trigger: Fire a sandbox event through the listener, e.g. make stripe-trigger EVENT=invoice.paid
stripe-trigger:
	./scripts/stripe_env.sh sh -c 'stripe trigger $${EVENT:-checkout.session.completed} --api-key "$$UNDERLEAF_STRIPE_API_KEY"'

## stripe-smoke: Trigger an event and assert cloud_api accepted and recorded it
stripe-smoke:
	./scripts/stripe_smoke.sh

## cloud-docker-build: Build Docker images for cloud services
cloud-docker-build:
	docker build . -t ghcr.io/ambientlabscomputing/underleaf/cloud_api:latest -f cloud/docker/cloud_api/Dockerfile
	docker build . -t ghcr.io/ambientlabscomputing/underleaf/conn_worker:latest -f cloud/docker/conn_worker/Dockerfile

## cloud-ui-build: Build the cloud UI with the correct API path
# Same-origin API path so the bundle works behind nginx on any hostname
# (nginx proxies /api/v2/cloud/ to cloud_api, which isn't published to the host).
cloud-ui-build:
	cd cloud/account_ui && VITE_API_URL=/api/v2/cloud npm run build
