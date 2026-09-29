# XKeen Control Panel

[![Build and Release](https://github.com/shisui1511/xkeen-control-panel/actions/workflows/build.yml/badge.svg)](https://github.com/shisui1511/xkeen-control-panel/actions/workflows/build.yml)
[![GitHub release (latest by date)](https://img.shields.io/github/v/release/shisui1511/xkeen-control-panel)](https://github.com/shisui1511/xkeen-control-panel/releases)
[![GitHub license](https://img.shields.io/github/license/shisui1511/xkeen-control-panel)](https://github.com/shisui1511/xkeen-control-panel/blob/main/LICENSE)

Веб-панель для управления XKeen на роутерах Keenetic/Netcraze.

- **Go backend** — статический бинарник, ~30–50 MB RAM (лимит рантайма — `GOMEMLIMIT`, см. ниже)
- **Svelte 5 frontend** — встроен в бинарник, первый экран ~63 KB gzip JS (вкладки грузятся лениво)
- **ARM64 + MIPSLE + MIPS** — сборки для Keenetic/Netcraze с Entware (MIPS — экспериментально, см. [Совместимость](#совместимость))
- **Smart Proxy** — переключение прокси в группах Mihomo по расписанию (сетка дни × часы)
- **Traffic Quotas** — учёт трафика по прокси и лимиты на день/неделю/месяц: уведомление, блокировка или перевод в DIRECT (Mihomo)
- **Kernel Manager** — установка, обновление и откат Xray и Mihomo прямо из UI
- **DAT Manager** — управление базами GeoIP и GeoSite: обновление, откат, поиск по тегам
- **Console** — быстрые команды XKeen и интерактивный терминал с выводом в реальном времени
- **PWA** — установка как приложения на телефон или компьютер (нужен HTTPS с доверенным сертификатом: по HTTP и с самоподписанным сертификатом браузер установку не предлагает)

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

Панель работает только по HTTPS. Откройте в браузере: `https://<IP-роутера>:8090`

Сертификат самоподписанный — браузер покажет предупреждение, это нормально для локальной сети: нажмите «Дополнительно» → «Перейти на сайт». Обычный `http://` на этом порту панель отвечает 308-редиректом на `https://`.

### Первичная настройка

`scripts/setup.sh` после установки сам предлагает задать пароль администратора:
- в терминале — сразу интерактивно (`xcp --reset-password`, ввод без эха);
- без терминала или при отказе — печатает адрес панели и одноразовый код настройки. Откройте `https://<IP-роутера>:8090`, введите код на экране первичной настройки и задайте пароль.

Код настройки можно показать снова командой `xcp --setup-code` по SSH.

### Забыли пароль?

Подключитесь к роутеру по SSH и выполните:

```bash
xcp --reset-password
```

Команда дважды спросит новый пароль (без эха) и применит его к уже запущенной панели без перезапуска — все активные сессии будут завершены. Для скриптов есть неинтерактивный вариант, читающий пароль из первой строки stdin:

```bash
echo 'новый-пароль' | xcp --reset-password --password-stdin
```

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

Панель работает только по HTTPS: обычный `http://` на её порту получает 308-редирект на `https://`, флаг `https.enabled` в `config.json` ничего не отключает. Самоподписанный сертификат при пустых `cert_path`/`key_path` генерируется автоматически в `/opt/etc/xcp/ssl/`.

### Свой сертификат

```json
{
  "https": {
    "cert_path": "/opt/etc/xcp/ssl/cert.pem",
    "key_path": "/opt/etc/xcp/ssl/key.pem"
  }
}
```

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

### Настройка параметров рантайма Go

Для тонкой настройки потребления памяти и сборщика мусора (Green Tea GC в Go 1.26+) используется файл оверрайдов окружения `/opt/etc/xcp/xcp.env`:

```bash
# /opt/etc/xcp/xcp.env
# Ограничение памяти рантайма (по умолчанию 96MiB):
# GOMEMLIMIT=96MiB

# Агрессивность сборщика мусора (по умолчанию 50):
# GOGC=50

# Escape-hatch: возврат к классическому GC при необходимости:
# export GOEXPERIMENT=nogreenteagc
```

---

## Удаление

Скриптом установки. Без терминала (например, `ssh` без `-t`) вопросы не задаются: флаг `--uninstall` считается подтверждением, панель и init-скрипт удаляются, а данные панели в `/opt/etc/xcp` (пароль, TLS-сертификат, сессии) сохраняются:

```bash
curl -Ls https://raw.githubusercontent.com/shisui1511/xkeen-control-panel/main/scripts/setup.sh | sh -s -- --uninstall
```

Чтобы удалить и данные панели, добавьте `--purge` (необратимо; работает только вместе с `--uninstall`):

```bash
curl -Ls https://raw.githubusercontent.com/shisui1511/xkeen-control-panel/main/scripts/setup.sh | sh -s -- --uninstall --purge
```

В терминале скрипт по-прежнему задаёт два вопроса: подтверждение удаления и удаление каталога конфигов.

Вручную:

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

### Совместимые модели Keenetic и Netcraze

Netcraze — бренд, под которым роутеры Keenetic выпускаются для России и стран ЕАЭС. Прошивка у них общая, поэтому панель одинаково работает на обоих брендах; индексы моделей обычно совпадают (KN-1812 ↔ NC-1812), а названия могут отличаться. Архитектуру `scripts/setup.sh` определяет сам.

| Архитектура | Keenetic | Netcraze |
|-------------|----------|----------|
| **ARM64 (aarch64)** | Peak (KN-2710), Ultra/Titan (KN-1811/KN-1812), Giga (KN-1012), Hopper (KN-3811), Hopper SE (KN-3812), Hopper 4G+ (KN-2312), Hopper DSL (KN-3611), Hero 5G (KN-4110) | Ultra (NC-1812), Giga (NC-1012), Hopper (NC-3811), Hopper SE (NC-3812), Hopper 4G+ (NC-2312), Hopper DSL (NC-3611), Hero 5G (NC-4110) |
| **MIPSLE (mipsel)** | Giga/Hero (KN-1010/KN-1011), Ultra (KN-1810), Viva/Skipper (KN-1910/KN-1912/KN-1913), Giant (KN-2610), Hero 4G (KN-2310/KN-2311), Hopper (KN-3810), Skipper 4G (KN-2910), Launcher DSL (KN-2012), Speedster DSL (KN-2113), 4G (KN-1212), Extra/Carrier (KN-1711/KN-1713) | Viva (NC-1913) |
| **MIPS (mips)** | Ultra SE/Peak DSL (KN-2510), Giga SE/Hero DSL (KN-2410), DSL/Omni DSL (KN-2010), Skipper DSL (KN-2112), Duo/Extra DSL (KN-2110), Hopper DSL (KN-3610) | — |

Модели Netcraze перечислены по справке производителя: у них есть инструкция по установке Entware с указанием архитектуры. Для Titan SE (NC-4210) Entware поддерживается, но архитектура в справке не указана. Для остальных моделей Netcraze инструкции по Entware нет, а без Entware XKeen и панель не устанавливаются.

Панель работает совместно с другими инструментами (другие веб-панели, zashboard) на разных портах без конфликтов.

---

## Разработка

```bash
# Зависимости
make deps
cd frontend && npm ci && cd ..
make hooks                     # pre-commit: prettier + gofmt

# Сборка
cd frontend && npm run build && cd ..
make build

# Cross-compile для роутеров
make router-arm64
make router-mipsle
make router-mips

# Frontend dev-сервер (proxy /api → :8090)
cd frontend && npm run dev
```