.PHONY: all build test cover lint vuln fmt tidy sync-spec snapshot

# Контракт API — копія з prro-go, з якою зібрано залежність; сервер віддає
# її як ресурс prro://openapi.yaml.
SPEC ?= ../prro-go/testdata/openapi.yaml

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)

all: lint test

build:
	go build -trimpath -ldflags "-s -w -X main.version=$(VERSION)" -o bin/prro-mcp ./cmd/prro-mcp

test:
	go test -race -count=1 ./...

cover:
	go test -race -count=1 -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out | tail -1

lint:
	golangci-lint run ./...

# govulncheck збирається тим самим toolchain, що й модуль, — інакше
# go run візьме мінімальну версію Go, якої вимагає сам x/vuln.
vuln:
	GOTOOLCHAIN=$$(go env GOVERSION) go run golang.org/x/vuln/cmd/govulncheck@latest ./...

fmt:
	golangci-lint fmt ./...

tidy:
	go mod tidy

# Оновлює копію контракту; contract_test.go покаже нові операції.
sync-spec:
	cp $(SPEC) internal/spec/openapi.yaml

# Локальна збірка всіх артефактів релізу без публікації.
snapshot:
	goreleaser release --snapshot --clean
