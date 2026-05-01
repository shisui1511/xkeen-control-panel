.PHONY: build run clean test lint fmt deps keenetic-arm64 keenetic-mipsle compress

BINARY_NAME=xkeen-control-panel
VERSION?=0.0.1

deps:
	go mod download
	go mod tidy

build:
	go build -buildvcs=false -ldflags "-s -w -X main.Version=$(VERSION)" -o build/$(BINARY_NAME) ./cmd/xcp

# Сборка для Keenetic ARM64 (KN-1010, KN-1810, KN-1910)
keenetic-arm64:
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -buildvcs=false -ldflags "-s -w -X main.Version=$(VERSION)" -o build/$(BINARY_NAME)-linux-arm64 ./cmd/xcp

# Сборка для Keenetic MIPS (KN-1912 Viva, KN-2410 и др.)
keenetic-mipsle:
	CGO_ENABLED=0 GOOS=linux GOARCH=mipsle go build -buildvcs=false -ldflags "-s -w -X main.Version=$(VERSION)" -o build/$(BINARY_NAME)-linux-mipsle ./cmd/xcp

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
