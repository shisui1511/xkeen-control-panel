.PHONY: build run clean test test-coverage lint fmt deps keenetic-arm64 keenetic-mipsle keenetic-mips compress proto

BINARY_NAME=xcp
EXACT_TAG := $(shell git describe --tags --exact-match HEAD 2>/dev/null)
IS_STABLE := $(shell echo "$(EXACT_TAG)" | grep -E '^v[0-9]+\.[0-9]+\.[0-9]+$$')

ifneq ($(IS_STABLE),)
  VERSION ?= $(EXACT_TAG)
else
  # Dev build: nearest stable tag plus SemVer build metadata, e.g.
  # v0.25.4+12.g60cb1057(.dirty). frontend/package.json is not bumped on
  # release, so it is only a fallback when no tag is reachable.
  LAST_TAG := $(shell git describe --tags --abbrev=0 --match 'v[0-9]*.[0-9]*.[0-9]*' --exclude '*-*' 2>/dev/null)
  PKG_VERSION := $(shell grep -o '"version": "[^"]*' frontend/package.json 2>/dev/null | cut -d'"' -f4 || echo "dev")
  GIT_COUNT := $(if $(LAST_TAG),$(shell git rev-list --count $(LAST_TAG)..HEAD 2>/dev/null))
  GIT_SHA := $(shell git rev-parse --short HEAD 2>/dev/null || echo "")
  GIT_DIRTY := $(shell git status --porcelain 2>/dev/null)
  BASE_VERSION := $(if $(LAST_TAG),$(LAST_TAG),v$(PKG_VERSION))
  VERSION ?= $(BASE_VERSION)+$(if $(GIT_COUNT),$(GIT_COUNT).)g$(GIT_SHA)$(if $(GIT_DIRTY),.dirty)
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

test-coverage:
	go test -race -v -coverprofile=coverage.out ./internal/...
	./scripts/check-coverage.sh coverage.out

lint:
	golangci-lint run ./...

fmt:
	go fmt ./...

clean:
	rm -rf build/

# Необязательная генерация protobuf/gRPC кода (локально, не в CI)
proto:
	protoc --proto_path=internal/xrayapi/proto \
		--go_out=. --go_opt=module=github.com/shisui1511/xkeen-control-panel \
		--go-grpc_out=. --go-grpc_opt=module=github.com/shisui1511/xkeen-control-panel \
		internal/xrayapi/proto/xray/common/net/network.proto \
		internal/xrayapi/proto/xray/app/stats/command/command.proto \
		internal/xrayapi/proto/xray/app/router/command/command.proto \
		internal/xrayapi/proto/xray/app/log/command/config.proto
