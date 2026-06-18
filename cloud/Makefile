build:
	go build -o bin/conn-worker ./conn_worker/cmd/main.go

run: build
	./bin/conn-worker

redis-up:
	-docker run --name redis -d -p 6379:6379 redis

redis-down:
	-docker stop redis
	-docker rm redis
