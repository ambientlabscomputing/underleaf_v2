## help: Print this help
help:
	@grep -E '^## ' $(MAKEFILE_LIST) | sed 's/^## //' | awk -F': ' '{printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2}'

## run-accounts-ui: Run the accounts UI
run-accounts-ui:
	cd account_ui && npm run dev

## run-cloud-api: Run the cloud API
run-cloud-api:
	cd cloud_api && make run

## run-conn-worker: Run the connection worker
run-conn-worker:
	cd conn_worker && make run

## run-cockpit-ui: Run the cockpit UI
run-cockpit-ui:
	cd cockpit_ui && npm run dev

## run: Run all services via overmind (accounts→5181, cockpit→5182)
run:
	overmind start

## stop: Stop all overmind-managed services
stop:
	-overmind kill
	-docker stop redis
	-docker rm redis