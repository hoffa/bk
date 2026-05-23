.PHONY: check fix build test

build:
	go build -o bk .

check: test
	test -z "$$(gofmt -l .)"
	go vet .
	go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.12.2 run .

test:
	go test ./...

fix:
	gofmt -w .
	go mod tidy
	go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.12.2 run --fix .
