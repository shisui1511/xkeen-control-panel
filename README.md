# XKeen Control Panel

Веб-панель для управления XKeen на роутерах Keenetic/Netcraze.

## Возможности

- **Single Binary**: Один файл ~7MB (сжатый ~2MB через UPX), нет зависимостей от Python или Node.js.
- **Управление сервисом**: Start/Stop/Restart XKeen и Mihomo.
- **Редактор конфигураций**: Просмотр и редактирование JSON (Xray) и YAML (Mihomo) с подсветкой синтаксиса (CodeMirror).
- **Live Logs**: Потоковые логи через WebSocket.
- **Безопасность**: Path Validation (защита от Path Traversal), планируется авторизация.

## Установка

### Сборка из исходников (на ПК)
```bash
cd /home/shisui/REPO/xkeen-control-panel
export PATH=$HOME/go-install/go/bin:$PATH

# Для Keenetic Viva (KN-1912 и другие) - MIPSLE
make keenetic-mipsle

# Для Keenetic ARM64 (KN-1010/1810/1910)
make keenetic-arm64
```

### Быстрая установка на роутер
```bash
# Скопируйте бинарник в /opt/bin
scp build/xkeen-control-panel-linux-mipsle root@192.168.1.1:/opt/bin/xkeen-control-panel

# Или выполните setup.sh на роутере
sh /path/to/scripts/setup.sh
```

### Ручная установка
```bash
# На роутере
mkdir -p /opt/etc/xkeen-control-panel
cd /opt/bin
wget -O xkeen-control-panel "https://github.com/shisui1511/xkeen-control-panel/releases/latest/download/xkeen-control-panel-linux-mipsle"
chmod +x xkeen-control-panel

# Создать конфиг
cat > /opt/etc/xkeen-control-panel/config.json <<EOF
{
    "port": 8089,
    "xray_config_dir": "/opt/etc/xray/configs",
    "xkeen_binary": "xkeen",
    "mihomo_config_dir": "/opt/etc/mihomo",
    "mihomo_binary": "mihomo",
    "allowed_roots": ["/opt/etc/xray", "/opt/etc/xkeen", "/opt/etc/mihomo", "/opt/var/log"]
}
EOF

# Запуск
/opt/bin/xkeen-control-panel -config /opt/etc/xkeen-control-panel/config.json
```

## Структура проекта

```
xkeen-control-panel/
├── cmd/xcp/main.go           # Точка входа
├── embed.go                  # Встраивание веб-файлов (Go embed)
├── internal/
│   ├── config/config.go     # Конфигурация
│   ├── handlers/api.go      # API (Version, Configs, Service, Logs, WebSocket)
│   ├── server/server.go     # HTTP сервер
│   ├── services/
│   │   ├── xkeen.go        # Управление XKeen
│   │   ├── mihomo.go       # Управление Mihomo
│   │   └── config.go       # Работа с конфигами
│   └── utils/path.go       # Path Validation (безопасность)
├── web/
│   ├── index.html           # UI (Alpine.js + CodeMirror)
│   └── static/css/style.css
├── scripts/setup.sh        # Скрипт установки на роутер
├── Makefile                # Сборка (Linux, MIPSLE, ARM64)
├── README.md               # Документация
└── build/
    ├── xkeen-control-panel           # Для ПК (6.4MB)
    └── xkeen-control-panel-linux-mipsle  # Для KN-1912 (6.9MB)
```

## Разработка

```bash
# Установка зависимостей
make deps

# Сборка для текущей ОС
make build

# Запуск
make run

# Тестирование
make test

# Сборка для KN-1912 (MIPSLE)
make keenetic-mipsle

# Сжатие UPX (уменьшение размера)
make compress
```

## Roadmap

- [x] Базовый каркас (Go + Embed)
- [x] Управление XKeen сервисом
- [x] Чтение/Сохранение конфигов с валидацией путей
- [x] WebSocket для логов
- [x] Веб-интерфейс (Alpine.js + CodeMirror)
- [x] Сборка под MIPSLE (KN-1912)
- [ ] Подписки Xray (Base64, Shadowsocks, VLESS)
- [ ] Управление Mihomo профилями
- [ ] Авторизация (Login/Password, Session)
- [ ] API для работы с GeoIP/GeoSite
- [ ] Автообновление из GitHub Releases

## Производительность на KN-1912

| Параметр | Значение |
|----------|----------|
| Архитектура | MIPS 1004Kc |
| ОЗУ (доступно) | ~200MB из 256MB |
| Бинарник (сжатый UPX) | ~2MB |
| Потребление RAM (xkeen-control-panel) | ~15-30MB |
| Потребление RAM (Python UI) | ~80-120MB |

## Источники

- [XKeen (jameszeroX)](https://github.com/jameszeroX/XKeen)
- [xkeen-ui (fan92rus)](https://github.com/fan92rus/xkeen-ui)
- [Xkeen-UI (umarcheh001)](https://github.com/umarcheh001/Xkeen-UI)
