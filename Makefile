.PHONY: test format lint tidy verify run

test:
	go test ./...

format:
	go fmt ./...

lint:
	golangci-lint run --timeout=5m

tidy:
	go mod tidy

verify:
	go mod verify

run:
	go run ./cmd/gopi
