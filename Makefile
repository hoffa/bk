.PHONY: check fix build

build:
	go build -o bk .

check:
	test -z "$$(gofmt -l .)"
	go vet ./...
	./test.sh
	go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.12.2 run ./...

fix:
	gofmt -w .
	go mod tidy
	go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.12.2 run --fix ./...
