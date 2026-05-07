# XKeen Control Panel

Веб-панель для управления XKeen/Mihomo на роутерах Keenetic/Netcraze.

- **Go backend** — статический бинарник, 5-10 MB RAM
- **Svelte 5 frontend** — встроен в бинарник, bundle ~160 KB gzipped
- **ARM64 + MIPSLE** — поддержка всех Keenetic с Entware

## Установка

### Быстрая установка (одной командой)

```bash
# Интерактивный установщик с меню
curl -Ls https://raw.githubusercontent.com/shisui1511/xkeen-control-panel/main/scripts/setup.sh | sh
```

Скрипт на русском языке. Поддерживает:
- **Stable** — стабильные релизы
- **Pre-release** — тестовые сборки
- Установка, обновление, удаление

### Ручная установка

```bash
# 1. Скачать бинарник
# ARM64 (KN-1810, KN-1910, KN-1010)
curl -fL -o /opt/bin/xkeen-control-panel \
  "https://github.com/shisui1511/xkeen-control-panel/releases/latest/download/xkeen-control-panel-linux-arm64"

# MIPSLE (KN-1912 Viva, KN-2410)
curl -fL -o /opt/bin/xkeen-control-panel \
  "https://github.com/shisui1511/xkeen-control-panel/releases/latest/download/xkeen-control-panel-linux-mipsle"

chmod +x /opt/bin/xkeen-control-panel

# 2. Создать конфиг
mkdir -p /opt/etc/xkeen-control-panel
cat > /opt/etc/xkeen-control-panel/config.json <<EOF
{
  "port": 8089,
  "xray_config_dir": "/opt/etc/xray/configs",
  "mihomo_config_dir": "/opt/etc/mihomo",
  "data_dir": "/opt/etc/xkeen-control-panel"
}
EOF

# 3. Запустить
/opt/bin/xkeen-control-panel -config /opt/etc/xkeen-control-panel/config.json
```

### После установки

Откройте в браузере: `http://<IP-роутера>:8089`

При первом входе будет предложено задать пароль администратора.

## Обновление

### Из веб-интерфейса (v0.4.0+)

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

## Управление сервисом

```bash
/opt/etc/init.d/S99xkeen-control-panel start    # Запуск
/opt/etc/init.d/S99xkeen-control-panel stop     # Остановка
/opt/etc/init.d/S99xkeen-control-panel restart  # Перезапуск
/opt/etc/init.d/S99xkeen-control-panel status   # Статус
```

## Удаление

```bash
/opt/etc/init.d/S99xkeen-control-panel stop
rm -f /opt/bin/xkeen-control-panel
rm -f /opt/etc/init.d/S99xkeen-control-panel
rm -rf /opt/etc/xkeen-control-panel   # удалить конфиги (опционально)
```

## Совместимость

| Модель | Архитектура | RAM | Статус |
|--------|-------------|-----|--------|
| KN-1912 Viva | MIPSLE | 128 MB | ✅ |
| KN-2410 | MIPSLE | 128 MB | ✅ |
| KN-1810 | ARM64 | 256 MB | ✅ |
| KN-1910 Peak | ARM64 | 256 MB | ✅ |
| KN-1010 | ARM64 | 128 MB | ✅ |
| KN-2300 | ARM64 | 256 MB | ✅ |

Панель работает alongside других XKeen-UI и zashboard — разные порты, нет конфликтов.

## Разработка

```bash
# Установка зависимостей
make deps
cd frontend && npm ci

# Сборка
cd frontend && npm run build
cd .. && make build

# Cross-compile для роутеров
make keenetic-arm64
make keenetic-mipsle

# Frontend dev server (proxy /api → :8089)
cd frontend && npm run dev
```

## Стек

| Компонент | Технология |
|-----------|------------|
| Backend | Go 1.25, net/http, gorilla/websocket |
| Frontend | Svelte 5, TypeScript, Vite |
| CSS | Custom Properties (light/dark themes) |
| Auth | bcrypt + HMAC session cookies |
| Build | go:embed (frontend в бинарнике) |

## Лицензия

MIT
