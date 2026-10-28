.PHONY: test cover vet fmt check build run

test:
	go test ./...

cover:
	go test -cover ./...

vet:
	go vet ./...

fmt:
	gofmt -w .

# What must pass before a commit: formatting, static checks and the suite.
check:
	@test -z "$$(gofmt -l .)" || (echo "gofmt needed on:"; gofmt -l .; exit 1)
	go vet ./...
	go test ./...

build:
	go build -o bin/journal ./cmd/journal

run: build
	./bin/journal
