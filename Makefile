.PHONY: test cover vet

test:
	go test ./...

cover:
	go test -cover ./...

vet:
	go vet ./...
