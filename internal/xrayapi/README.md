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

