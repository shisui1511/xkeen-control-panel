.PHONY: build run clean test lint fmt deps keenetic-arm64 keenetic-mipsle keenetic-mips compress

BINARY_NAME=xcp
VERSION?=$(shell git describe --tags --always 2>/dev/null || grep -o '"version": "[^"]*' frontend/package.json 2>/dev/null | cut -d'"' -f4 || echo "dev")

deps:
	go mod download
	go mod tidy

build:
	go build -buildvcs=false -ldflags "-s -w -X main.Version=$(VERSION)" -o build/$(BINARY_NAME) ./cmd/xcp

# Сборка для Keenetic ARM64 (KN-1010, KN-1810, KN-1910)
keenetic-arm64:
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -buildvcs=false -ldflags "-s -w -X main.Version=$(VERSION)" -o build/$(BINARY_NAME)_$(VERSION)_arm64 ./cmd/xcp

# Сборка для Keenetic MIPSLE (KN-1912 Viva, KN-2410 и др.)
keenetic-mipsle:
	CGO_ENABLED=0 GOOS=linux GOARCH=mipsle GOMIPS=softfloat go build -buildvcs=false -ldflags "-s -w -X main.Version=$(VERSION)" -o build/$(BINARY_NAME)_$(VERSION)_mipsle ./cmd/xcp

# Сборка для Keenetic MIPS big-endian (KN-3610, KN-2310 и др.)
keenetic-mips:
	CGO_ENABLED=0 GOOS=linux GOARCH=mips GOMIPS=softfloat go build -buildvcs=false -ldflags "-s -w -X main.Version=$(VERSION)" -o build/$(BINARY_NAME)_$(VERSION)_mips ./cmd/xcp

# Сжатие UPX (для уменьшения размера)
compress: build
	upx --best --lzma build/$(BINARY_NAME) || true
	@echo "Compressed size:"
	@ls -lh build/$(BINARY_NAME)

run: build
	./build/$(BINARY_NAME)

test:
	go test -v ./...

lint:
	golangci-lint run ./...

fmt:
	go fmt ./...

clean:
	rm -rf build/
