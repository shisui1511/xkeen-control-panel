# Xray gRPC API Client & Proto

Пакет `internal/xrayapi` предоставляет типизированный gRPC-клиент к внутреннему API ядра Xray (`StatsService`, `RoutingService`, `LoggerService`), работающий через защищённое подключение к loopback-интерфейсу роутера (порт `10085` по умолчанию).

## Вендоренные схемы Protobuf

Источники схем: репозиторий [XTLS/Xray-core](https://github.com/XTLS/Xray-core) (ветка `main`, снимок от 2026-09-02).
Расположение исходных `.proto` файлов:
- `internal/xrayapi/proto/xray/common/net/network.proto`
- `internal/xrayapi/proto/xray/app/stats/command/command.proto`
- `internal/xrayapi/proto/xray/app/router/command/command.proto`
- `internal/xrayapi/proto/xray/app/log/command/config.proto`

Сгенерированные Go-файлы закоммичены в репозиторий в каталоге `internal/xrayapi/gen/`, поэтому наличие утилит `protoc` не требуется ни в CI, ни для сборки бинарных файлов панели.

## Версии инструментов кодогенерации

- `protoc`: libprotoc 25.3
- `protoc-gen-go`: v1.36.12
- `protoc-gen-go-grpc`: v1.5.1

## Команда регенерации кода

```bash
make proto
```
Или напрямую:
```bash
protoc --proto_path=internal/xrayapi/proto \
  --go_out=. --go_opt=module=github.com/shisui1511/xkeen-control-panel \
  --go-grpc_out=. --go-grpc_opt=module=github.com/shisui1511/xkeen-control-panel \
  internal/xrayapi/proto/xray/common/net/network.proto \
  internal/xrayapi/proto/xray/app/stats/command/command.proto \
  internal/xrayapi/proto/xray/app/router/command/command.proto \
  internal/xrayapi/proto/xray/app/log/command/config.proto
```

## Эмпирические замеры прироста размера бинарника (D-01)

В соответствии с требованием задачи 1 и решением D-01, проведены замеры размера бинарных файлов до и после подключения зависимостей `google.golang.org/grpc` (v1.83.2) и `google.golang.org/protobuf` (v1.36.12), а также после добавления служб `RoutingService` и `LoggerService`:

| Архитектура | Базовый размер (без gRPC) | После Plan 104-01 (Stats) | Финальный размер (Plan 104-04: Stats+Router+Logger) | Прирост от базы (байт / МБ) | Дельта от 104-01 | Бюджет D-01 (~6 МБ) |
|-------------|--------------------------|---------------------------|---------------------------------------------------|----------------------------|------------------|----------------------|
| **ARM64**   | 13 041 826 (~12.44 MB)   | 17 760 418 (~16.94 MB)    | 17 891 490 (~17.06 MB)                            | +4 849 664 (+4.62 MB)      | +131 KB          | **Укладывается в бюджет** |
| **MIPSLE**  | 14 876 865 (~14.19 MB)   | 20 250 817 (~19.31 MB)    | 20 316 353 (~19.37 MB)                            | +5 439 488 (+5.19 MB)      | +65 KB           | **Укладывается в бюджет** |

Обе целевые архитектуры уверенно укладываются в согласованный бюджет D-01 (~6 МБ), лимит жёсткой эскалации 8 МБ не превышен. Рост от вендоринга proto-файлов маршрутизации и логирования составил всего 65–131 КБ.
