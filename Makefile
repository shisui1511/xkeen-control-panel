.PHONY: build run clean test test-coverage lint fmt deps hooks router-arm64 router-mipsle router-mips router-test proto

BINARY_NAME=xcp
# Single source of truth for the version: scripts/version.sh (git tags +
# conventional commits). CI and the Service Worker cache name use it too.
ifeq ($(origin VERSION),undefined)
  VERSION := $(shell sh scripts/version.sh)
endif

deps:
	go mod download
	go mod tidy

# Git-хуки из .githooks (pre-commit: prettier + gofmt по файлам из индекса)
hooks:
	git config core.hooksPath .githooks
	@echo "Git hooks: .githooks"

update-version:
	@echo "Building version $(VERSION)"

version:
	@echo $(VERSION)

build: update-version
	go build -buildvcs=false -ldflags "-s -w -X main.Version=$(VERSION)" -o build/$(BINARY_NAME) ./cmd/xcp

# Сборка для роутеров Keenetic/Netcraze ARM64 (KN-1812/NC-1812, KN-1012/NC-1012, KN-3811/NC-3811 и др.)
router-arm64: update-version
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -buildvcs=false -ldflags "-s -w -X main.Version=$(VERSION)" -o build/$(BINARY_NAME)_$(VERSION)_arm64 ./cmd/xcp

# Сборка для роутеров Keenetic/Netcraze MIPSLE (KN-1010, KN-1810, KN-1913/NC-1913 и др.)
router-mipsle: update-version
	CGO_ENABLED=0 GOOS=linux GOARCH=mipsle GOMIPS=softfloat go build -buildvcs=false -ldflags "-s -w -X main.Version=$(VERSION)" -o build/$(BINARY_NAME)_$(VERSION)_mipsle ./cmd/xcp

# Сборка для роутеров Keenetic MIPS big-endian (KN-2410, KN-2510, KN-3610 и др.)
router-mips: update-version
	CGO_ENABLED=0 GOOS=linux GOARCH=mips GOMIPS=softfloat go build -buildvcs=false -ldflags "-s -w -X main.Version=$(VERSION)" -o build/$(BINARY_NAME)_$(VERSION)_mips ./cmd/xcp

# Проверка на роутерах: деплой, смоук, отчёт в build/router/last.
# Цели берутся из локального scripts/router/targets.local.env (образец — targets.example.env).
#   make router-test SUITE=smoke [ROUTERS=arm64] [RELEASE=vX.Y.Z]
router-test:
	sh scripts/router/run.sh $(SUITE)

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
