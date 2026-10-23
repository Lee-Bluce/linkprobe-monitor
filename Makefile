.PHONY: web build build-arm64 build-amd64 test fakegen clean

VERSION ?= 1.0.0
BUILD_TIME := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -s -w -X main.version=$(VERSION) -X main.buildTime=$(BUILD_TIME)

web:
	cd web && npm install --no-audit --no-fund && npm run build

test:
	go test ./...

build: web
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o build/linkprobe ./cmd/linkprobe

build-arm64: web
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -ldflags "$(LDFLAGS)" -o build/linkprobe-linux-arm64 ./cmd/linkprobe

build-amd64: web
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "$(LDFLAGS)" -o build/linkprobe-linux-amd64 ./cmd/linkprobe

fakegen:
	go build -o build/fakegen ./tools/fakegen

clean:
	rm -rf build
