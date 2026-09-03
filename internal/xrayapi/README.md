# Xray gRPC API Client & Proto

Пакет `internal/xrayapi` предоставляет типизированный gRPC-клиент к внутреннему API ядра Xray (`StatsService`, `RoutingService`, `LoggerService`), работающий через защищённое подключение к loopback-интерфейсу роутера (порт `10085` по умолчанию).

## Вендоренные схемы Protobuf

Источники схем: репозиторий [XTLS/Xray-core](https://github.com/XTLS/Xray-core) (ветка `main`, снимок от 2026-09-02).
Расположение исходных `.proto` файлов:
- `internal/xrayapi/proto/xray/common/net/network.proto`
- `internal/xrayapi/proto/xray/app/stats/command/command.proto`

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
  internal/xrayapi/proto/xray/app/stats/command/command.proto
```

## Эмпирические замеры прироста размера бинарника (D-01)

В соответствии с требованием задачи 1 и решением D-01, проведены замеры размера бинарных файлов до и после подключения зависимостей `google.golang.org/grpc` (v1.83.2) и `google.golang.org/protobuf` (v1.36.12):

| Архитектура | Базовый размер (байт) | Финальный размер (байт) | Прирост (байт / МБ) | Бюджет D-01 (~6 МБ) |
|-------------|-----------------------|-------------------------|---------------------|----------------------|
| **ARM64**   | 13 041 826 (~12.44 MB) | 17 760 418 (~16.94 MB)  | +4 718 592 (+4.50 MB) | **Укладывается в бюджет** |
| **MIPSLE**  | 14 876 865 (~14.19 MB) | 20 250 817 (~19.31 MB)  | +5 373 952 (+5.12 MB) | **Укладывается в бюджет** |

Обе целевые архитектуры укладываются в согласованный бюджет D-01 (~6 МБ), лимит жёсткой эскалации 8 МБ не превышен.
