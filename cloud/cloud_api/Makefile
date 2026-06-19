install:
	pip install -e ".[dev]"

db-up:
	docker run -d \
		--name cloud-api-db \
		-e POSTGRES_USER=postgres \
		-e POSTGRES_PASSWORD=postgres \
		-e POSTGRES_DB=postgres \
		-p 5432:5432 \
		postgres:18

db-down:
	-docker stop cloud-api-db
	-docker rm cloud-api-db

env-up: db-up

run:
	python -m cloud_api.main