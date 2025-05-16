BINARY := ararauna
BIN_DIR := bin
CONFIG  := config.yml

.PHONY: run build test test-race cover tidy fmt vet lint clean help

help:
	@echo "Targets:"
	@echo "  run        - go run ./cmd --config $(CONFIG)"
	@echo "  build      - build binary into $(BIN_DIR)/$(BINARY)"
	@echo "  test       - go test ./..."
	@echo "  test-race  - go test -race ./..."
	@echo "  cover      - go test with coverage report (coverage.out + HTML)"
	@echo "  tidy       - go mod tidy"
	@echo "  fmt        - go fmt ./..."
	@echo "  vet        - go vet ./..."
	@echo "  lint       - golangci-lint run ./..."
	@echo "  clean      - remove $(BIN_DIR)/ and coverage artifacts"

run:
	go run ./cmd --config $(CONFIG)

build:
	@mkdir -p $(BIN_DIR)
	go build -o $(BIN_DIR)/$(BINARY) ./cmd

test:
	go test ./...

test-race:
	go test -race ./...

cover:
	go test -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html
	@echo "coverage report: coverage.html"

tidy:
	go mod tidy

fmt:
	go fmt ./...

vet:
	go vet ./...

lint:
	golangci-lint run ./...

clean:
	rm -rf $(BIN_DIR) coverage.out coverage.html
