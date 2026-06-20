build:
	go build -o bin/conn-worker ./cmd/main.go

run: build redis-up
	./bin/conn-worker

redis-up:
	-docker run --name redis -d -p 6379:6379 redis

redis-down:
	-docker stop redis
	-docker rm redis

clean:
	-docker stop redis
	-docker rm redis
	-rm -rf bin