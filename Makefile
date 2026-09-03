.PHONY: build run test test-race fmt vet lint tidy docker-build docker-up docker-down clean

BINARY := evernote-lite

## build: compile the server binary into ./bin
build:
	CGO_ENABLED=0 go build -o bin/$(BINARY) ./cmd/evernote-lite

## run: build and run the server locally (needs a reachable MongoDB and .env)
run:
	go run ./cmd/evernote-lite

## test: run the full test suite
test:
	go test ./...

## test-race: run the full test suite with the race detector
test-race:
	go test -race ./...

## fmt: format every Go file, failing if gofmt had to change anything
fmt:
	@test -z "$$(gofmt -l .)" || (echo "gofmt needs to be run on:" && gofmt -l . && exit 1)

## vet: run go vet across the module
vet:
	go vet ./...

## lint: fmt + vet, the two checks CI should never skip
lint: fmt vet

## tidy: sync go.mod/go.sum with what the source actually imports
tidy:
	go mod tidy

## docker-build: build the API's container image
docker-build:
	docker build -t evernote-lite .

## docker-up: run the API and MongoDB together for local development
docker-up:
	docker compose -f deploy/docker-compose.yml up --build

## docker-down: stop the local development stack and remove its containers
docker-down:
	docker compose -f deploy/docker-compose.yml down

## clean: remove local build output
clean:
	rm -rf bin/
