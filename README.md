# XKeen Control Panel

Веб-панель для управления XKeen/Mihomo на роутерах Keenetic/Netcraze.

- **Go backend** — статический бинарник, 5-10 MB RAM
- **Svelte 5 frontend** — встроен в бинарник, bundle ~160 KB gzipped
- **ARM64 + MIPSLE** — поддержка всех Keenetic с Entware
- **Smart Proxy** — автоматическое переключение прокси по расписанию и задержке
- **Traffic Quotas** — учёт трафика и гибкие лимиты на прокси
- **Kernel Manager** — установка и обновление Xray и Mihomo прямо из UI
- **DAT Manager** — управление базами GeoIP и GeoSite
- **Console** — выполнение команд XKeen с просмотром вывода в реальном времени
- **PWA** — поддержка установки как приложения на телефон или компьютер

## Установка

### Быстрая установка (одной командой)

```bash
curl -Ls https://raw.githubusercontent.com/shisui1511/xkeen-control-panel/main/scripts/setup.sh | sh
```

Скрипт на русском языке. Поддерживает:
- **Stable** — стабильные релизы
- **Pre-release** — тестовые сборки
- Установка, обновление, удаление
- Автоматический fallback если GitHub недоступен

### Ручная установка


```bash
# 1. Скачать бинарник (замените {VERSION} на актуальную версию, например v0.4.0)
# ARM64 (KN-1810, KN-1910, KN-1010)
curl -fL -o /opt/bin/xkeen-control-panel \
  "https://github.com/shisui1511/xkeen-control-panel/releases/latest/download/xcp_{VERSION}_arm64"

# MIPSLE (KN-1912, KN-2410)
curl -fL -o /opt/bin/xkeen-control-panel \
  "https://github.com/shisui1511/xkeen-control-panel/releases/latest/download/xcp_{VERSION}_mipsle"

chmod +x /opt/bin/xkeen-control-panel

# 2. Создать конфиг
mkdir -p /opt/etc/xkeen-control-panel
cat > /opt/etc/xkeen-control-panel/config.json <<EOF
{
  "port": 8090,
  "xray_config_dir": "/opt/etc/xray/configs",
  "mihomo_config_dir": "/opt/etc/mihomo",
  "data_dir": "/opt/etc/xkeen-control-panel"
}
EOF

# 3. Запустить
/opt/bin/xkeen-control-panel -config /opt/etc/xkeen-control-panel/config.json
```

### После установки

Откройте в браузере: `http://<IP-роутера>:8090`

При первом входе будет предложено задать пароль администратора.

## Обновление

### Из веб-интерфейса

Settings → Update → кнопка **"Проверить обновления"** → **"Установить"**.

Панель сама скачает новый бинарник, сделает backup и перезапустится.
Если что-то пойдёт не так — автоматический rollback.

### Через SSH

# Интерактивное меню (установка/обновление/удаление + выбор канала)
```bash
curl -Ls https://raw.githubusercontent.com/shisui1511/xkeen-control-panel/main/scripts/setup.sh | sh
```

# Или сразу командой:
```bash
curl -Ls https://raw.githubusercontent.com/shisui1511/xkeen-control-panel/main/scripts/setup.sh | sh -s -- install
```

Скрипт автоматически:
1. Опредерит архитектуру роутера
2. Остановит текущую версию
3. Скачает новый бинарник
4. Перезапустит сервис

## HTTPS

Панель поддерживает самоподписанные сертификаты для HTTPS в локальной сети.

### Включение HTTPS

В `config.json`:

```json
{
  "https": {
    "enabled": true,
    "cert_path": "",
    "key_path": ""
  }
}
```

При `enabled: true` и пустых путях сертификат генерируется автоматически в `/opt/etc/xkeen-control-panel/ssl/`.

### Ручная генерация сертификата

```bash
openssl req -x509 -nodes -days 365 -newkey rsa:2048 \
  -keyout /opt/etc/xkeen-control-panel/ssl/key.pem \
  -out /opt/etc/xkeen-control-panel/ssl/cert.pem \
  -subj "/CN=xkeen-control-panel" \
  -addext "subjectAltName=IP:192.168.1.1"
```

> ⚠️ Браузер покажет предупреждение о самоподписанном сертификате — это нормально для LAN. Нажмите "Дополнительно" → "Перейти на сайт".

## Управление сервисом

```bash
/opt/etc/init.d/S99xcp start    # Запуск
/opt/etc/init.d/S99xcp stop     # Остановка
/opt/etc/init.d/S99xcp restart  # Перезапуск
/opt/etc/init.d/S99xcp status   # Статус
```

## Удаление

```bash
/opt/etc/init.d/S99xcp stop
rm -f /opt/bin/xkeen-control-panel
rm -f /opt/etc/init.d/S99xcp
rm -rf /opt/etc/xkeen-control-panel   # удалить конфиги (опционально)
```

## Совместимость

| Архитектура | Минимум RAM | Примеры | Бинарник | Статус |
|-------------|-------------|---------|----------|--------|
| **ARM64** | 128 MB | Extra III, Peak, Titan, Giga, Ultra | `xkeen-control-panel-arm64` | ✅ |
| **MIPSLE** | 128 MB | Viva, Duo, Skipper | `xkeen-control-panel-mipsle` | ✅ |
| **MIPS** | 64 MB* | Старые модели | `xkeen-control-panel-mips` | ⚠️ |

> *Модели с 64 MB RAM могут не тянуть Entware + XKeen одновременно. Рекомендуется 128 MB+.

Панель работает alongside других XKeen-UI и zashboard — разные порты, нет конфликтов.

## Разработка

# Установка зависимостей
```bash
make deps
cd frontend && npm ci

# Сборка
cd frontend && npm run build
cd .. && make build

# Cross-compile для роутеров
make keenetic-arm64
make keenetic-mipsle

# Frontend dev server (proxy /api → :8090)
cd frontend && npm run dev
```

## Стек

| Компонент | Технология |
|-----------|------------|
| Backend | Go 1.24, net/http, gorilla/websocket |
| Frontend | Svelte 5, TypeScript, Vite |
| CSS | Custom Properties (light/dark themes) |
| Auth | bcrypt + HMAC session cookies |
| Build | go:embed (frontend в бинарнике) |