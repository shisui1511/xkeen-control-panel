# XKeen Control Panel

[![Build and Release](https://github.com/shisui1511/xkeen-control-panel/actions/workflows/build.yml/badge.svg)](https://github.com/shisui1511/xkeen-control-panel/actions/workflows/build.yml)
[![GitHub release (latest by date)](https://img.shields.io/github/v/release/shisui1511/xkeen-control-panel)](https://github.com/shisui1511/xkeen-control-panel/releases)
[![GitHub license](https://img.shields.io/github/license/shisui1511/xkeen-control-panel)](https://github.com/shisui1511/xkeen-control-panel/blob/main/LICENSE)

Веб-панель для управления XKeen на роутерах Keenetic/Netcraze.

- **Go backend** — статический бинарник, 5–10 MB RAM
- **Svelte 5 frontend** — встроен в бинарник, bundle ~160 KB gzipped
- **ARM64 + MIPSLE + MIPS** — поддержка всех Keenetic с Entware
- **Smart Proxy** — автоматическое переключение прокси по расписанию и задержке
- **Traffic Quotas** — учёт трафика и гибкие лимиты на прокси
- **Kernel Manager** — установка и обновление Xray и Mihomo прямо из UI
- **DAT Manager** — управление базами GeoIP и GeoSite
- **Console** — выполнение команд XKeen с просмотром вывода в реальном времени
- **PWA** — поддержка установки как приложения на телефон или компьютер

---

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
# 1. Скачать бинарник (замените {VERSION} на актуальную версию)

# ARM64 (KN-2710, KN-1811, KN-1012)
curl -fL -o /opt/sbin/xcp \
  "https://github.com/shisui1511/xkeen-control-panel/releases/latest/download/xcp_{VERSION}_arm64"

# MIPSLE (KN-1010, KN-1810, KN-1910)
curl -fL -o /opt/sbin/xcp \
  "https://github.com/shisui1511/xkeen-control-panel/releases/latest/download/xcp_{VERSION}_mipsle"

# MIPS (KN-2510, KN-2410, KN-2010)
curl -fL -o /opt/sbin/xcp \
  "https://github.com/shisui1511/xkeen-control-panel/releases/latest/download/xcp_{VERSION}_mips"

chmod +x /opt/sbin/xcp

# 2. Создать конфиг
mkdir -p /opt/etc/xcp
cat > /opt/etc/xcp/config.json <<EOF
{
  "port": 8090,
  "xray_config_dir": "/opt/etc/xray/configs",
  "mihomo_config_dir": "/opt/etc/mihomo",
  "data_dir": "/opt/etc/xcp"
}
EOF

# 3. Запустить
/opt/sbin/xcp -config /opt/etc/xcp/config.json
```

### После установки

Откройте в браузере: `http://<IP-роутера>:8090`

При первом входе будет предложено задать пароль администратора.

---

## Обновление

### Из веб-интерфейса

Settings → Update → кнопка **"Проверить обновления"** → **"Установить"**.

Панель сама скачает новый бинарник, сделает backup и перезапустится.
Если что-то пойдёт не так — автоматический rollback.

### Через SSH

```bash
# Интерактивное меню (установка/обновление/удаление + выбор канала)
curl -Ls https://raw.githubusercontent.com/shisui1511/xkeen-control-panel/main/scripts/setup.sh | sh

# Или сразу командой:
curl -Ls https://raw.githubusercontent.com/shisui1511/xkeen-control-panel/main/scripts/setup.sh | sh -s -- install
```

Скрипт автоматически:
1. Определит архитектуру роутера
2. Остановит текущую версию
3. Скачает новый бинарник
4. Перезапустит сервис

---

## HTTPS

Панель поддерживает самоподписанные сертификаты для HTTPS в локальной сети.

### Включение через config.json

```json
{
  "https": {
    "enabled": true,
    "cert_path": "",
    "key_path": ""
  }
}
```

При `enabled: true` и пустых путях сертификат генерируется автоматически в `/opt/etc/xcp/ssl/`.

### Ручная генерация сертификата

```bash
openssl req -x509 -nodes -days 365 -newkey rsa:2048 \
  -keyout /opt/etc/xcp/ssl/key.pem \
  -out /opt/etc/xcp/ssl/cert.pem \
  -subj "/CN=xcp" \
  -addext "subjectAltName=IP:192.168.1.1"
```

> ⚠️ Браузер покажет предупреждение о самоподписанном сертификате — это нормально для LAN. Нажмите «Дополнительно» → «Перейти на сайт».

---

## Управление сервисом

```bash
/opt/etc/init.d/S99xcp start    # Запуск
/opt/etc/init.d/S99xcp stop     # Остановка
/opt/etc/init.d/S99xcp restart  # Перезапуск
/opt/etc/init.d/S99xcp status   # Статус
```

---

## Удаление

```bash
/opt/etc/init.d/S99xcp stop
rm -f /opt/sbin/xcp
rm -f /opt/etc/init.d/S99xcp
rm -rf /opt/etc/xcp   # удалить конфиги (опционально)
```

---

## Совместимость

| Архитектура | Требования к RAM | Имя бинарника | Статус поддержки |
|-------------|------------------|---------------|------------------|
| **ARM64 (aarch64)** | >= 128 MB | `xcp_{VERSION}_arm64` | ✅ Полная |
| **MIPSLE (mipsel)** | >= 128 MB | `xcp_{VERSION}_mipsle` | ✅ Полная |
| **MIPS (mips)** | >= 64 MB* | `xcp_{VERSION}_mips` | ⚠️ Экспериментальная |

> \*Модели с 64 MB RAM могут испытывать нехватку памяти при одновременной работе Entware, XKeen/Mihomo и веб-панели. Рекомендуется использовать роутеры с >= 128 MB RAM.

### Совместимые модели Keenetic

| Архитектура | Модели |
|-------------|--------|
| **ARM64 (aarch64)** | Peak (KN-2710), Ultra/Titan (KN-1811/KN-1812), Giga (KN-1012), Hopper (KN-3811), Hopper SE (KN-3812), Hopper 4G+ (KN-2312), Hero 5G (KN-4110) |
| **MIPSLE (mipsel)** | Giga/Hero (KN-1010/KN-1011), Ultra (KN-1810), Viva/Skipper (KN-1910/KN-1912/KN-1913), Giant (KN-2610), Hero 4G (KN-2310/KN-2311), Hopper (KN-3810), Skipper 4G (KN-2910), Launcher DSL (KN-2012), Speedster DSL (KN-2113), Hopper DSL (KN-3611), 4G (KN-1212), Extra/Carrier (KN-1711/KN-1713) |
| **MIPS (mips)** | Ultra SE/Peak DSL (KN-2510), Giga SE/Hero DSL (KN-2410), DSL/Omni DSL (KN-2010), Skipper DSL (KN-2112), Duo/Extra DSL (KN-2110), Hopper DSL (KN-3610) |

Панель работает совместно с другими инструментами (Legacy-UI, zashboard) на разных портах без конфликтов.

---

## Разработка

```bash
# Зависимости
make deps
cd frontend && npm ci && cd ..

# Сборка
cd frontend && npm run build && cd ..
make build

# Cross-compile для роутеров
make keenetic-arm64
make keenetic-mipsle
make keenetic-mips

# Frontend dev-сервер (proxy /api → :8090)
cd frontend && npm run dev
```