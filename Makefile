.PHONY: build run clean test lint fmt deps keenetic-arm64 keenetic-mipsle keenetic-mips compress

BINARY_NAME=xcp
EXACT_TAG := $(shell git describe --tags --exact-match HEAD 2>/dev/null)
IS_STABLE := $(shell echo "$(EXACT_TAG)" | grep -E '^v[0-9]+\.[0-9]+\.[0-9]+$$')

ifneq ($(IS_STABLE),)
  VERSION ?= $(EXACT_TAG)
else
  PKG_VERSION := $(shell grep -o '"version": "[^"]*' frontend/package.json 2>/dev/null | cut -d'"' -f4 || echo "dev")
  GIT_SHA := $(shell git rev-parse --short HEAD 2>/dev/null || echo "")
  GIT_DIRTY := $(shell git status --porcelain 2>/dev/null)
  VERSION ?= v$(PKG_VERSION)$(if $(GIT_SHA),-$(GIT_SHA))$(if $(GIT_DIRTY),-dirty)
endif

deps:
	go mod download
	go mod tidy

update-version:
	@echo "Building version $(VERSION)"

version:
	@echo $(VERSION)

build: update-version
	go build -buildvcs=false -ldflags "-s -w -X main.Version=$(VERSION)" -o build/$(BINARY_NAME) ./cmd/xcp

# Сборка для Keenetic ARM64 (KN-1010, KN-1810, KN-1910)
keenetic-arm64: update-version
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -buildvcs=false -ldflags "-s -w -X main.Version=$(VERSION)" -o build/$(BINARY_NAME)_$(VERSION)_arm64 ./cmd/xcp

# Сборка для Keenetic MIPSLE (KN-1912 Viva, KN-2410 и др.)
keenetic-mipsle: update-version
	CGO_ENABLED=0 GOOS=linux GOARCH=mipsle GOMIPS=softfloat go build -buildvcs=false -ldflags "-s -w -X main.Version=$(VERSION)" -o build/$(BINARY_NAME)_$(VERSION)_mipsle ./cmd/xcp

# Сборка для Keenetic MIPS big-endian (KN-3610, KN-2310 и др.)
keenetic-mips: update-version
	CGO_ENABLED=0 GOOS=linux GOARCH=mips GOMIPS=softfloat go build -buildvcs=false -ldflags "-s -w -X main.Version=$(VERSION)" -o build/$(BINARY_NAME)_$(VERSION)_mips ./cmd/xcp

# Сжатие UPX (для уменьшения размера)
compress: build
	upx --best --lzma build/$(BINARY_NAME) || true
	@echo "Compressed size:"
	@ls -lh build/$(BINARY_NAME)

run: build
	./build/$(BINARY_NAME)

test:
	go test -race -v ./...

lint:
	golangci-lint run ./...

fmt:
	go fmt ./...

clean:
	rm -rf build/
