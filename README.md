# XKeen Control Panel

Веб-панель для управления XKeen на роутерах Keenetic/Netcraze.

## Возможности

- Добавлю позже...

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


## Источники

- Добавлю
