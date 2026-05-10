.PHONY: build run test fmt vet clean

BIN := bin/chronicle
SRC := ./cmd/chronicle

build:
	go build -o $(BIN) $(SRC)

run:
	go run $(SRC)

test:
	go test ./...

fmt:
	gofmt -w .

vet:
	go vet ./...

clean:
	rm -rf bin/
