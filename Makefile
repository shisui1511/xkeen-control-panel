.PHONY: build run clean lint fmt deps hooks router-arm64 router-mipsle router-mips router-test router-test-unattended proto tidy

BINARY_NAME=xcp
# Single source of truth for the version: scripts/version.sh (git tags +
# conventional commits). CI uses it too.
ifeq ($(origin VERSION),undefined)
  VERSION := $(shell sh scripts/version.sh)
endif

deps:
	go mod download
	go mod tidy

# Git-хуки из .githooks: pre-commit по индексу (prettier, gofmt, check-no-simulations, check-leaks),
# commit-msg (check-leaks по сообщению коммита), pre-merge-commit (check-leaks по индексу слияния)
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
	@sh scripts/tidy.sh --rotate-builds || true

# Сборка для роутеров Keenetic/Netcraze MIPSLE (KN-1010, KN-1810, KN-1913/NC-1913 и др.)
router-mipsle: update-version
	CGO_ENABLED=0 GOOS=linux GOARCH=mipsle GOMIPS=softfloat go build -buildvcs=false -ldflags "-s -w -X main.Version=$(VERSION)" -o build/$(BINARY_NAME)_$(VERSION)_mipsle ./cmd/xcp
	@sh scripts/tidy.sh --rotate-builds || true

# Сборка для роутеров Keenetic MIPS big-endian (KN-2410, KN-2510, KN-3610 и др.)
router-mips: update-version
	CGO_ENABLED=0 GOOS=linux GOARCH=mips GOMIPS=softfloat go build -buildvcs=false -ldflags "-s -w -X main.Version=$(VERSION)" -o build/$(BINARY_NAME)_$(VERSION)_mips ./cmd/xcp
	@sh scripts/tidy.sh --rotate-builds || true

# Проверка на роутерах: деплой, смоук, отчёт в build/router/last.
# Цели берутся из локального scripts/router/targets.local.env (образец — targets.example.env).
#   make router-test SUITE=smoke [ROUTERS=arm64] [RELEASE=vX.Y.Z]
#   make router-test SUITE=smoke|full RELEASE=<тег>   (публикует статус stand/<arch>-<набор> на коммите тега)
#   make router-test SUITE=session SPEC=<спека.json>
#   make router-test SUITE=delta RELEASE=<тег|latest> [DRY_RUN=1]   (добор stand/<arch>-full от прошлого полного прогона вехи)
# Наборы: (пусто) — полный (full — то же, что без SUITE), smoke — деплой и смоук, changed — затронутое diff'ом,
# reboot — холодный старт каждой цели и проверка автозапуска, session — сессия на стенде
# по спеке (формат — в scripts/router/session.mjs). Остальные наборы и их переменные:
#   harness                                      — механика стенда: тесты блока harness на обоих ядрах и самопроверка сессии
#   rerun FROM=<отчёт>                           — перепрогон упавших тестов отчёта (дописывается в него)
#   bisect TEST=<go:Имя|pw:название> CORE=<ядро> FROM=<ссылка> [TO=<ссылка>] [MODE=regress|fix] [SPEC=<спек>]
#                                                — поиск виновного коммита двоичным поиском по дереву коммитов
#   delta RELEASE=<тег|latest> [DRY_RUN=1] [BASE_REPORT=<отчёт>]
#                                                — добор stand/<arch>-full на новый тег без повторного полного прогона
#                                                  (DRY_RUN=1 — статус не отправляется, решение в summary.md)
# Отдельная цель router-test-unattended — полный прогон без присмотра (описана ниже).
router-test:
	sh scripts/router/run.sh $(SUITE)

# Полный прогон без присмотра (scripts/router/unattended.sh): команда возвращается сразу, отвязанный
# процесс ставит отдельный worktree на REF, ждёт AT и очередь на роутеры (до 2 ч), гонит полный
# прогон и пишет итог в build/router/unattended/latest.md (одна строка — в начале следующей сессии).
#   make router-test-unattended [REF=<ref>] [RELEASE=<тег|auto>] [AT=<ЧЧ:ММ>]
# REF — что проверять (по умолчанию origin/main после fetch); RELEASE — ставить ассет релиза
# (auto — пре-релиз, покрывающий origin/main); AT — локальное время старта. Ход:
# sh scripts/router/unattended.sh status. Это инструмент: правил, когда запускать, и напоминаний нет.
router-test-unattended:
	sh scripts/router/unattended.sh start

run: build
	./build/$(BINARY_NAME)

lint:
	golangci-lint run ./...

fmt:
	go fmt ./...

# Только сборки; отчёты стенда и кэш релизов снимает make tidy
clean:
	rm -rf build/xcp build/xcp_v* build/xcp_test_* build/router/bin

# Уборка за агентами (scripts/tidy.sh): без флагов только показ; удаление — только APPLY=1
# в командной строке (APPLY=0, другие значения и обычные переменные окружения дают показ).
# Исключение: определения в MAKEFLAGS и GNUMAKEFLAGS окружения make считает командной строкой,
# поэтому APPLY=1 и REMOTE=1 в этих переменных (и во вложенном $(MAKE) под родителем с APPLY=1)
# включают удаление; в окружении их не задавать.
# REMOTE=1 — ветки origin, так же только в командной строке; удалять их (REMOTE=1 APPLY=1)
# только после «да» владельца на показанный список
# tidy_on: непусто, только если переменная $(1) задана в командной строке и равна ровно «1»
tidy_on = $(and $(filter command line,$(origin $(1))),$(filter 1,$(words $($(1)))),$(filter 1,$($(1))))
tidy:
	sh scripts/tidy.sh $(if $(call tidy_on,APPLY),--apply) $(if $(call tidy_on,REMOTE),--remote)

# Необязательная генерация protobuf/gRPC кода (локально, не в CI)
proto:
	protoc --proto_path=internal/xrayapi/proto \
		--go_out=. --go_opt=module=github.com/shisui1511/xkeen-control-panel \
		--go-grpc_out=. --go-grpc_opt=module=github.com/shisui1511/xkeen-control-panel \
		internal/xrayapi/proto/xray/common/net/network.proto \
		internal/xrayapi/proto/xray/app/stats/command/command.proto \
		internal/xrayapi/proto/xray/app/router/command/command.proto \
		internal/xrayapi/proto/xray/app/log/command/config.proto
