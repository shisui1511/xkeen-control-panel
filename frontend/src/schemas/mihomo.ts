const echOptsSchema = {
  type: 'object',
  description: {
    ru: `Encrypted Client Hello (ECH) — шифрует само поле SNI в TLS, так что даже домен назначения скрыт от пассивного DPI, а не только содержимое трафика.`,
    en: 'Encrypted Client Hello (ECH) settings — encrypts the TLS SNI itself so even the domain name is hidden from passive DPI, not just the payload.'
  },
  properties: {
    enable: {
      type: 'boolean',
      description: { ru: `Включить ECH для этого прокси.`, en: 'Turn on ECH for this proxy.' }
    },
    config: {
      type: 'string',
      description: {
        ru: `ECHConfig в base64. Если не задан, Mihomo может получить его через DNS (query-server-name).`,
        en: 'Base64-encoded ECHConfig. If omitted, Mihomo can fetch it via DNS (query-server-name).'
      }
    },
    'query-server-name': {
      type: 'string',
      description: {
        ru: `Домен для DNS-запроса ECHConfig, когда config не задан напрямую.`,
        en: 'Domain to query for the ECHConfig over DNS when config is not set directly.'
      }
    }
  }
};

const xhttpReuseSettingsSchema = {
  type: 'object',
  description: {
    ru: `Настройка повторного использования XHTTP-соединений (он же XMUX) — насколько агрессивно транспорт переиспользует уже открытые HTTP-соединения вместо новых.`,
    en: 'XHTTP connection-reuse tuning (aka XMUX) — controls how aggressively the transport reuses underlying HTTP connections instead of opening new ones per request.'
  },
  properties: {
    'max-concurrency': {
      oneOf: [{ type: 'integer' }, { type: 'string' }],
      description: {
        ru: `Максимум параллельных запросов на соединение — фиксированное число или диапазон "min-max".`,
        en: 'Max parallel requests per connection, as a fixed number or a "min-max" range.'
      }
    },
    'max-connections': {
      oneOf: [{ type: 'integer' }, { type: 'string' }],
      description: { ru: `Максимум XHTTP-соединений.`, en: 'Max XHTTP connections.' }
    },
    'c-max-reuse-times': {
      oneOf: [{ type: 'integer' }, { type: 'string' }],
      description: {
        ru: `Сколько раз клиент может повторно использовать одно соединение.`,
        en: 'How many times the client may reuse one connection.'
      }
    },
    'h-max-request-times': {
      oneOf: [{ type: 'integer' }, { type: 'string' }],
      description: {
        ru: `Максимум запросов на одно HTTP-соединение.`,
        en: 'Max requests allowed on one HTTP connection.'
      }
    },
    'h-max-reusable-secs': {
      oneOf: [{ type: 'integer' }, { type: 'string' }],
      description: {
        ru: `Максимум секунд, в течение которых HTTP-соединение можно переиспользовать.`,
        en: 'Max seconds an HTTP connection may be reused for.'
      }
    },
    'h-keep-alive-period': {
      oneOf: [{ type: 'integer' }, { type: 'string' }],
      description: {
        ru: `Период keep-alive для HTTP-соединений.`,
        en: 'Keep-alive period for HTTP connections.'
      }
    }
  }
};

const xhttpPaddingProps = {
  'x-padding-bytes': {
    oneOf: [{ type: 'integer' }, { type: 'string' }],
    description: {
      ru: `Объём (или диапазон "min-max") случайных padding-байтов, добавляемых, чтобы скрыть реальный размер запроса/ответа.`,
      en: 'Amount (or "min-max" range) of random padding bytes added to obscure request/response size.'
    }
  },
  'x-padding-obfs-mode': {
    type: 'boolean',
    description: { ru: `Включить обфускацию через padding.`, en: 'Enable padding obfuscation.' }
  },
  'x-padding-key': {
    type: 'string',
    description: {
      ru: `Имя параметра/cookie/заголовка, в котором передаётся значение padding.`,
      en: 'Parameter/cookie/header name under which the padding value is placed.'
    }
  },
  'x-padding-header': {
    type: 'string',
    description: {
      ru: `HTTP-заголовок для padding, когда x-padding-placement — header или queryInHeader.`,
      en: 'HTTP header name used for padding when x-padding-placement is header/queryInHeader.'
    }
  },
  'x-padding-placement': {
    type: 'string',
    enum: ['queryInHeader', 'cookie', 'header', 'query'],
    description: { ru: `Куда помещать значение padding.`, en: 'Where to place the padding value.' }
  },
  'x-padding-method': {
    type: 'string',
    enum: ['repeat-x', 'tokenish'],
    description: {
      ru: `Как генерировать padding: повтор символа "x" или token-подобная случайная строка.`,
      en: 'How the padding value is generated: a repeated "x" character, or a token-like random string.'
    }
  }
} as const;

const xhttpSharedProps = {
  headers: {
    type: 'object',
    additionalProperties: { type: 'string' },
    description: {
      ru: `Дополнительные HTTP-заголовки для отправки.`,
      en: 'Extra HTTP headers to send.'
    }
  },
  host: {
    type: 'string',
    description: {
      ru: `HTTP Host-заголовок для XHTTP-транспорта. Часто совпадает с servername/SNI.`,
      en: 'HTTP Host header for the XHTTP transport. Often matches servername/SNI.'
    }
  },
  path: {
    type: 'string',
    description: {
      ru: `HTTP-путь для XHTTP-транспорта. Должен совпадать с серверным.`,
      en: 'HTTP path for the XHTTP transport. Must match the server side.'
    }
  },
  'no-grpc-header': {
    type: 'boolean',
    description: {
      ru: `Не добавлять gRPC-совместимый заголовок.`,
      en: 'Do not add the gRPC-compatible header.'
    }
  },
  'uplink-http-method': {
    type: 'string',
    description: {
      ru: `HTTP-метод для uplink-запросов.`,
      en: 'HTTP method used for uplink requests.'
    }
  },
  'session-placement': {
    type: 'string',
    enum: ['path', 'query', 'cookie', 'header'],
    description: {
      ru: `Где размещать идентификатор XHTTP-сессии.`,
      en: 'Where to place the XHTTP session identifier.'
    }
  },
  'session-key': {
    type: 'string',
    description: {
      ru: `Ключ/имя для идентификатора XHTTP-сессии.`,
      en: 'Key/name used for the XHTTP session identifier.'
    }
  },
  'seq-placement': {
    type: 'string',
    enum: ['path', 'query', 'cookie', 'header'],
    description: {
      ru: `Где размещать порядковый номер XHTTP-запроса.`,
      en: 'Where to place the XHTTP request sequence number.'
    }
  },
  'seq-key': {
    type: 'string',
    description: {
      ru: `Ключ/имя для порядкового номера XHTTP-запроса.`,
      en: 'Key/name used for the XHTTP request sequence number.'
    }
  },
  'uplink-data-placement': {
    type: 'string',
    enum: ['body', 'cookie', 'header'],
    description: {
      ru: `Где размещать фрагменты uplink-данных в режиме packet-up.`,
      en: 'Where to place uplink data chunks in packet-up mode.'
    }
  },
  'uplink-data-key': {
    type: 'string',
    description: {
      ru: `Базовое имя ключа для фрагментов uplink-данных.`,
      en: 'Base key name for uplink data chunks.'
    }
  },
  'uplink-chunk-size': {
    oneOf: [{ type: 'integer' }, { type: 'string' }],
    description: {
      ru: `Максимальный размер uplink-фрагмента, если данные передаются не в body. Минимум 64 байта.`,
      en: 'Max size of an uplink chunk when data is not sent in the body. Minimum 64 bytes.'
    }
  },
  'sc-max-each-post-bytes': {
    oneOf: [{ type: 'integer' }, { type: 'string' }],
    description: {
      ru: `Максимальный размер каждого POST-запроса в режиме stream-up.`,
      en: 'Max size of each POST request in stream-up mode.'
    }
  },
  'sc-min-posts-interval-ms': {
    oneOf: [{ type: 'integer' }, { type: 'string' }],
    description: {
      ru: `Минимальный интервал между POST-запросами XHTTP, в миллисекундах.`,
      en: 'Minimum interval between XHTTP POST requests, in milliseconds.'
    }
  },
  ...xhttpPaddingProps
} as const;

const xhttpDownloadSettingsSchema = {
  type: 'object',
  description: {
    ru: `Переопределения для отдельного download-плеча (сервер → клиент) XHTTP-соединения — позволяют использовать разные сервер/путь/TLS для загрузки и отдачи, под асимметричную схему за CDN.`,
    en: 'Overrides for the separate download (server→client) leg of an XHTTP connection — lets upload and download use different servers/paths/TLS settings, matching an asymmetric CDN setup.'
  },
  properties: {
    server: {
      type: 'string',
      description: {
        ru: `Переопределить адрес сервера для download-плеча.`,
        en: 'Override server address for the download leg.'
      }
    },
    port: {
      oneOf: [{ type: 'integer' }, { type: 'string' }],
      description: {
        ru: `Переопределить порт сервера для download-плеча.`,
        en: 'Override server port for the download leg.'
      }
    },
    tls: {
      type: 'boolean',
      description: {
        ru: `Включить TLS для download-плеча.`,
        en: 'Enable TLS for the download leg.'
      }
    },
    alpn: { type: 'array', items: { type: 'string' } },
    sni: { type: 'string' },
    servername: { type: 'string' },
    'client-fingerprint': { type: 'string' },
    fingerprint: {
      type: 'string',
      description: {
        ru: `Pinned TLS-отпечаток сертификата для download-плеча.`,
        en: 'TLS certificate pinning fingerprint for the download leg.'
      }
    },
    'skip-cert-verify': { type: 'boolean' },
    certificate: {
      type: 'string',
      description: {
        ru: `Клиентский сертификат для mTLS на download-плече.`,
        en: 'Client certificate for mTLS on the download leg.'
      }
    },
    'private-key': {
      type: 'string',
      description: {
        ru: `Приватный ключ клиента для mTLS на download-плече.`,
        en: 'Client private key for mTLS on the download leg.'
      }
    },
    'ech-opts': echOptsSchema,
    'reality-opts': {
      type: 'object',
      properties: {
        'public-key': {
          type: 'string',
          description: { ru: `Публичный ключ REALITY-сервера.`, en: 'REALITY server public key.' }
        },
        'short-id': {
          type: 'string',
          description: { ru: `Short ID REALITY.`, en: 'REALITY short ID.' }
        },
        'spider-x': {
          type: 'string',
          description: {
            ru: `REALITY spiderX-путь, обычно "/".`,
            en: 'REALITY spiderX path, typically "/".'
          }
        },
        'support-x25519mlkem768': {
          type: 'boolean',
          description: {
            ru: `Включить поддержку гибридного обмена ключами X25519-MLKEM768 для REALITY.`,
            en: 'Enable hybrid X25519-MLKEM768 key exchange support for REALITY.'
          }
        }
      }
    },
    'reuse-settings': xhttpReuseSettingsSchema,
    ...xhttpSharedProps
  }
};

const xhttpOptsSchema = {
  type: 'object',
  description: {
    ru: `Параметры транспорта XHTTP (network: xhttp) — современный HTTP-based транспорт, рассчитанный на работу за CDN/reverse-proxy. Значения здесь должны точно совпадать с серверной настройкой транспорта.`,
    en: 'XHTTP transport options (network: xhttp) — a modern HTTP-based transport designed to sit behind a CDN/reverse proxy. Values here must match the server-side transport configuration exactly.'
  },
  properties: {
    mode: {
      type: 'string',
      enum: ['auto', 'stream-one', 'stream-up', 'packet-up'],
      description: {
        ru: `"auto" — текущий рекомендуемый режим по умолчанию; "stream-one" — безопасный baseline; "stream-up" больше подходит для browser-like долгоживущего TCP; "packet-up" — для packet/UDP-like трафика.`,
        en: '"auto" is the current recommended default; "stream-one" is a safe baseline; "stream-up" suits browser-like long-lived TCP; "packet-up" suits packet/UDP-like traffic patterns.'
      }
    },
    'reuse-settings': xhttpReuseSettingsSchema,
    'download-settings': xhttpDownloadSettingsSchema,
    ...xhttpSharedProps
  }
};

const wireGuardPeerSchema = {
  type: 'object',
  description: {
    ru: `Один peer в multi-peer конфигурации WireGuard. При использовании peers[] верхнеуровневые server/port/public-key игнорируются.`,
    en: 'One peer in a multi-peer WireGuard setup. Using peers[] ignores the top-level server/port/public-key.'
  },
  properties: {
    server: {
      type: 'string',
      description: { ru: `Домен или IP этого peer.`, en: "Peer's domain or IP." }
    },
    port: {
      type: 'integer',
      minimum: 1,
      maximum: 65535,
      description: { ru: `Порт этого peer.`, en: "Peer's port." }
    },
    'public-key': {
      type: 'string',
      description: { ru: `Base64 публичный ключ peer.`, en: "Peer's base64 public key." }
    },
    'pre-shared-key': {
      type: 'string',
      description: {
        ru: `Необязательный base64 pre-shared key для этого peer.`,
        en: 'Optional base64 pre-shared key for this peer.'
      }
    },
    'allowed-ips': {
      type: 'array',
      items: { type: 'string' },
      description: {
        ru: `Сети, маршрутизируемые через этот peer. Для полного туннеля обычно 0.0.0.0/0 и ::/0.`,
        en: 'Networks routed through this peer. For a full tunnel, typically 0.0.0.0/0 and ::/0.'
      }
    },
    reserved: {
      type: 'array',
      items: { type: 'integer' },
      description: {
        ru: `Reserved-байты handshake, которые требуют некоторые WARP-подобные серверы для этого peer.`,
        en: 'Reserved handshake bytes some WARP-style servers require for this peer.'
      }
    }
  },
  required: ['server', 'port', 'public-key']
};

export const mihomoSchema = {
  $schema: 'http://json-schema.org/draft-07/schema#',
  type: 'object',
  title: 'Mihomo Configuration',
  description: {
    ru: `Файл конфигурации Mihomo (Clash.Meta)`,
    en: 'Mihomo (Clash.Meta) configuration file'
  },
  properties: {
    port: {
      type: 'integer',
      minimum: 1,
      maximum: 65535,
      description: { ru: `Порт HTTP-прокси.`, en: 'HTTP proxy port' },
      default: 7890
    },
    'socks-port': {
      type: 'integer',
      minimum: 1,
      maximum: 65535,
      description: { ru: `Порт SOCKS5-прокси.`, en: 'SOCKS5 proxy port' },
      default: 7891
    },
    'mixed-port': {
      type: 'integer',
      minimum: 1,
      maximum: 65535,
      description: { ru: `Единый порт HTTP+SOCKS.`, en: 'Mixed HTTP+SOCKS port' },
      default: 7892
    },
    'redir-port': {
      type: 'integer',
      minimum: 1,
      maximum: 65535,
      description: {
        ru: `Порт прозрачного проксирования REDIRECT. Нужен только если трафик заворачивается в Mihomo через iptables REDIRECT; в этой панели iptables-правилами обычно управляет сам XKeen, так что значение должно совпадать с тем, куда XKeen настроен перенаправлять.`,
        en: 'REDIRECT transparent-proxy port. Only needed if traffic is routed into Mihomo via iptables REDIRECT; on this panel XKeen usually owns the iptables rules, so this is set to match whatever XKeen was configured to redirect to.'
      }
    },
    'tproxy-port': {
      type: 'integer',
      minimum: 1,
      maximum: 65535,
      description: {
        ru: `Порт TPROXY для прозрачного проксирования (сохраняет исходный IP назначения, нужен для корректного матчинга GEOIP/IP-CIDR правил). Та же оговорка, что и у redir-port: TPROXY-правила iptables на этом роутере ведёт XKeen, значение должно совпадать с тем, куда он указывает.`,
        en: 'TPROXY transparent-proxy port (preserves original destination IP, needed for correct GEOIP/IP-CIDR rule matching). Same caveat as redir-port: XKeen manages the iptables TPROXY rules on this router, this value must match what XKeen points at.'
      }
    },
    'allow-lan': {
      type: 'boolean',
      description: {
        ru: `Разрешить другим устройствам в LAN обращаться к port/socks-port/mixed-port этого роутера, а не только localhost. Обязательно сочетайте с authentication или lan-allowed-ips — иначе прокси становится открытым для всей локальной сети.`,
        en: 'Allow other LAN devices to reach port/socks-port/mixed-port on this router, not just localhost. Combine with authentication or lan-allowed-ips, otherwise anyone on the LAN gets an open proxy.'
      },
      default: false
    },
    'bind-address': {
      type: 'string',
      description: {
        ru: `Адрес привязки слушающих сокетов. "*" — все интерфейсы, конкретный IP — ограничить один.`,
        en: 'Bind address for the listening sockets. "*" binds all interfaces; a specific IP restricts listening to just that one.'
      },
      default: '*'
    },
    mode: {
      type: 'string',
      enum: ['rule', 'global', 'direct'],
      description: {
        ru: `- **rule** — маршрутизация по массиву \`rules\` (обычный режим).\n- **global** — весь трафик через текущий выбранный прокси, правила игнорируются.\n- **direct** — весь трафик напрямую, минуя прокси.\n\n> \`global\` удобен для быстрой проверки full-tunnel, но не забудьте вернуть \`rule\` после теста.`,
        en: '- **rule** — route by the `rules` array (normal mode).\n- **global** — send everything through the currently selected proxy, ignoring rules.\n- **direct** — send everything straight out, bypassing proxies entirely.\n\n> `global` is handy for a quick full-tunnel test — remember to switch back to `rule` afterwards.'
      },
      default: 'rule'
    },
    'log-level': {
      type: 'string',
      enum: ['info', 'warning', 'error', 'debug', 'silent'],
      description: {
        ru: `- **silent** — логирование полностью отключено.\n- **error** / **warning** — только проблемы (рекомендуется для обычной работы роутера).\n- **info** — умеренная подробность.\n- **debug** — очень подробно, только для активной отладки.\n\n> \`debug\` быстро забивает хранилище и логи роутера — не оставляйте его включённым надолго.`,
        en: '- **silent** — logging disabled entirely.\n- **error** / **warning** — only problems (recommended for normal router operation).\n- **info** — moderate detail.\n- **debug** — very verbose, for active troubleshooting only.\n\n> `debug` quickly fills router storage/logs — do not leave it on for long.'
      },
      default: 'info'
    },
    ipv6: {
      type: 'boolean',
      description: {
        ru: `Глобальная поддержка IPv6 для собственных соединений Mihomo. Если у роутера/аплинка нет рабочего нативного IPv6 — оставьте false, иначе каждое соединение будет тормозить на AAAA-таймаутах.`,
        en: "Global IPv6 support for Mihomo's own connections. If the router/uplink does not have working native IPv6, leave this false to avoid AAAA-lookup timeouts slowing down every connection."
      },
      default: false
    },
    'external-controller': {
      type: 'string',
      pattern: '^[^:\\s]*:\\d{1,5}$',
      description: {
        ru: `Адрес REST API (например 127.0.0.1:9090). Эта панель обычно общается с Mihomo через локальный unix-сокет — задавайте LAN-доступный адрес (0.0.0.0:9090) только если осознанно нужен внешний дашборд, и всегда вместе с secret.`,
        en: 'REST API bind address (e.g. 127.0.0.1:9090). This panel talks to Mihomo over this API, normally via a local unix socket instead — only set this to a LAN-reachable address (0.0.0.0:9090) if you deliberately want an external dashboard like Zashboard, and always pair it with secret.'
      }
    },
    'external-controller-unix': {
      type: 'string',
      description: {
        ru: `Путь к Unix-сокету REST API — альтернатива TCP external-controller. Локальный сокет — самый безопасный способ для этой панели общаться с Mihomo: он никогда не открывает LAN-доступный порт. После изменения перезапустите Mihomo.`,
        en: 'Unix domain socket path for the REST API, as an alternative to a TCP external-controller. A local socket is the safest way for this panel to talk to Mihomo — it never opens a LAN-reachable port. Restart Mihomo after changing this.'
      }
    },
    'external-controller-tls': {
      type: 'string',
      description: {
        ru: `Адрес REST API с TLS (требует tls-cert/tls-key).`,
        en: 'HTTPS bind address for REST API (requires tls-cert/tls-key)'
      }
    },
    'external-ui': {
      type: 'string',
      description: {
        ru: `Путь к веб-дашборду (Clash-API-совместимому UI), который отдаёт сам Mihomo. Относительный путь считается от рабочего каталога ядра.`,
        en: 'Filesystem path to a web dashboard (a Clash-API-compatible UI) served by Mihomo itself. Relative paths resolve against the core working directory.'
      }
    },
    'external-ui-name': {
      type: 'string',
      description: {
        ru: `Имя подпапки для раздачи, когда архив, скачанный по external-ui-url, содержит несколько дашбордов.`,
        en: "Subfolder name to serve when external-ui-url's downloaded archive bundles more than one dashboard."
      }
    },
    'external-ui-url': {
      type: 'string',
      description: {
        ru: `URL, с которого Mihomo автоматически скачивает и устанавливает внешний дашборд при старте.`,
        en: 'URL Mihomo auto-downloads and installs the external dashboard from on startup.'
      }
    },
    secret: {
      type: 'string',
      description: {
        ru: `Bearer-токен для доступа к external-controller. Обязателен на практике всегда, когда external-controller доступен из LAN — иначе кто угодно в сети может читать статистику трафика или менять активный прокси/правила.`,
        en: 'Bearer token required to call external-controller. Mandatory in practice whenever external-controller is reachable from the LAN, otherwise anyone on the network can read traffic stats or change the active proxy/rules.'
      }
    },
    authentication: {
      type: 'array',
      items: { type: 'string' },
      description: {
        ru: `Учётные данные Basic-auth для HTTP/SOCKS/Mixed inbound в виде "user:pass". Используйте это (или lan-allowed-ips), когда allow-lan включён — иначе роутер станет открытым прокси для всей LAN/гостевого Wi-Fi.`,
        en: 'Basic-auth credentials for HTTP/SOCKS/Mixed inbound in "user:pass" form. Use this (or lan-allowed-ips) whenever allow-lan is true, so the router does not become an open proxy for the whole LAN/guest Wi-Fi.'
      }
    },
    'skip-auth-prefixes': {
      type: 'array',
      items: { type: 'string' },
      description: {
        ru: `CIDR-префиксы источника, освобождённые от аутентификации выше — например, собственная LAN-подсеть роутера, чтобы доверенные устройства не запрашивали пароль, а гостевая сеть — запрашивала.`,
        en: "Source CIDR prefixes exempt from the authentication above — e.g. the router's own LAN subnet, so trusted devices are not prompted while a guest network still is."
      }
    },
    'lan-allowed-ips': {
      type: 'array',
      items: { type: 'string' },
      description: {
        ru: `Белый список CIDR для inbound-листенеров при включённом allow-lan — самый простой способ открыть прокси только своим устройствам без учётных данных на каждое.`,
        en: 'CIDR allow-list for inbound listeners when allow-lan is enabled — the simplest way to expose the proxy to your own devices only, without per-device credentials.'
      }
    },
    'lan-disallowed-ips': {
      type: 'array',
      items: { type: 'string' },
      description: {
        ru: `Чёрный список CIDR для inbound-листенеров при включённом allow-lan — проверяется раньше lan-allowed-ips, удобно чтобы заблокировать конкретную гостевую подсеть, разрешив всё остальное.`,
        en: 'CIDR deny-list for inbound listeners when allow-lan is enabled — evaluated before lan-allowed-ips, useful for blocking a specific guest-Wi-Fi subnet while allowing everything else.'
      }
    },
    'interface-name': {
      type: 'string',
      description: {
        ru: `Исходящий сетевой интерфейс для собственных соединений Mihomo глобально (переопределяется на уровне прокси/группы). На Keenetic это обычно WAN-интерфейс, например nwan0 или имя, выданное провайдером.`,
        en: "Outbound network interface for Mihomo's own connections globally (overridable per proxy/group). On Keenetic this is normally the WAN interface, e.g. nwan0 or the ISP-assigned name."
      }
    },
    'routing-mark': {
      type: 'integer',
      minimum: 0,
      description: {
        ru: `SO_MARK, проставляемая на собственные исходящие сокеты Mihomo, чтобы настройка ip rule/iptables в XKeen могла распознать и исключить их из перехвата прозрачного прокси — без этого собственные исходящие соединения Mihomo могут снова завернуться сами в себя (routing loop).`,
        en: "SO_MARK stamped on Mihomo's own outbound sockets so XKeen's ip rule/iptables setup can recognize and exclude them from transparent-proxy interception — without it, Mihomo's own upstream connections can get redirected back into itself (a routing loop)."
      }
    },
    'global-ua': {
      type: 'string',
      description: {
        ru: `Кастомный User-Agent для HTTP-запросов, которые Mihomo делает от своего имени (обновление подписок, health-check) — не для проксируемого трафика клиентов.`,
        en: 'Custom User-Agent for HTTP requests Mihomo makes on its own behalf (subscription updates, health-checks) — not for proxied client traffic.'
      }
    },
    'keep-alive-idle': {
      type: 'integer',
      minimum: 0,
      description: {
        ru: `Сколько секунд TCP-соединение простаивает, прежде чем Mihomo начнёт слать keep-alive пробы. Помогает не дать провайдерскому NAT закрыть запись для долгоживущего соединения.`,
        en: 'Seconds a TCP connection sits idle before Mihomo starts sending keep-alive probes. Helps keep the ISP NAT table entry for a long-lived connection from expiring.'
      }
    },
    'keep-alive-interval': {
      type: 'integer',
      minimum: 0,
      description: {
        ru: `Интервал между TCP keep-alive пробами в секундах, когда они уже начались.`,
        en: 'Interval in seconds between TCP keep-alive probes once they start.'
      }
    },
    'tcp-concurrent': {
      type: 'boolean',
      description: {
        ru: `Параллельно подключаться по всем резолвнутым IP (Happy Eyeballs) и использовать тот, чей handshake завершится первым. Помогает, когда домен резолвится и в IPv4, и в IPv6 или в несколько серверов, и один из путей медленный/заблокирован.`,
        en: 'Dial all resolved IPs concurrently (Happy Eyeballs) and use whichever handshake completes first. Helps when a domain resolves to both IPv4 and IPv6 or multiple servers and one path is slow/blocked.'
      }
    },
    'unified-delay': {
      type: 'boolean',
      description: {
        ru: `Учитывать время TLS/TCP handshake в задержке, которая показывается для url-test/fallback групп, а не только round-trip ping. Без этого прокси с быстрым пингом, но медленным handshake, всё равно может выглядеть "самым быстрым" и выбираться автоматически.`,
        en: 'Include TLS/TCP handshake time in the latency shown for url-test/fallback groups, not just round-trip ping. Without it, a proxy with a fast ping but slow handshake can still look "fastest" and get auto-selected.'
      }
    },
    'find-process-mode': {
      type: 'string',
      enum: ['always', 'strict', 'off'],
      description: {
        ru: `Определяет, срабатывают ли правила PROCESS-NAME, через резолв процесса-владельца соединения. Mihomo, работая на самом роутере (а не на клиентском устройстве), как правило не видит имена процессов LAN-клиентов — оставляйте off, если не уверены, что ОС роутера действительно отдаёт эту информацию, иначе это просто лишняя нагрузка ради правил, которые никогда не сработают.`,
        en: 'Controls PROCESS-NAME rule matching by resolving which local process owns a connection. Mihomo running on the router itself (not the client device) generally cannot see per-app process names for LAN clients — leave this off unless you know the router OS actually exposes that info, otherwise it just adds overhead for rules that will never match.'
      }
    },
    'global-client-fingerprint': {
      type: 'string',
      enum: [
        'chrome',
        'firefox',
        'safari',
        'ios',
        'android',
        'edge',
        '360',
        'qq',
        'random',
        'none'
      ],
      description: {
        ru: `TLS ClientHello fingerprint (uTLS) по умолчанию для исходящих TLS-соединений, если прокси не задаёт свой client-fingerprint. Имитация реального браузера (chrome/firefox/...) помогает трафику не выделяться и обходит блокировки DPI по TLS-отпечатку.`,
        en: 'Default uTLS client-hello fingerprint applied to outbound TLS connections when a proxy does not set its own client-fingerprint. Mimicking a real browser (chrome/firefox/...) helps traffic blend in and avoids TLS-fingerprint-based DPI blocking.'
      }
    },
    profile: {
      type: 'object',
      description: {
        ru: `Что Mihomo сохраняет на диск между перезапусками (переживает перезагрузку роутера, пока каталог кэша на энергонезависимом хранилище).`,
        en: 'What Mihomo persists to disk across restarts (survives router reboots as long as the cache directory is on non-volatile storage).'
      },
      properties: {
        'store-selected': {
          type: 'boolean',
          description: {
            ru: `Запоминать, какой прокси был выбран вручную в каждой select-группе, чтобы перезагрузка роутера не откатывала выбор молча на дефолт группы.`,
            en: 'Remember which proxy was manually selected in each select-type group, so a router reboot does not silently fall back to the group default.'
          }
        },
        'store-fake-ip': {
          type: 'boolean',
          description: {
            ru: `Сохранять кэш домен↔IP fake-ip между перезапусками, чтобы DNS-зависимые правила продолжали матчиться так же сразу после рестарта Mihomo, без новых резолвов.`,
            en: 'Persist the fake-ip domain↔IP cache across restarts, so DNS-dependent rules keep matching the same way right after Mihomo restarts instead of needing fresh lookups.'
          }
        }
      }
    },
    'geodata-mode': {
      type: 'boolean',
      description: {
        ru: `Использовать компактный формат .dat вместо отдельных файлов GeoIP.mmdb/GeoSite.dat — меньше места на диске, что важно для хранилища роутера.`,
        en: 'Use the compact .dat geodata format instead of separate GeoIP.mmdb/GeoSite.dat files — smaller on-disk footprint, relevant on router storage.'
      }
    },
    'geodata-loader': {
      type: 'string',
      enum: ['standard', 'memconservative'],
      description: {
        ru: `Как гео-базы загружаются в память. "standard" грузит их целиком для самого быстрого поиска; "memconservative" читает с диска по мере необходимости, экономя RAM — имеет смысл переключить на роутерах с ограниченной памятью (типичное железо Keenetic), если Mihomo убивает OOM-killer.`,
        en: 'How geo databases are loaded into memory. "standard" loads them fully for fastest lookups; "memconservative" streams from disk to save RAM — worth switching to on routers with limited RAM (typical Keenetic hardware) if Mihomo is getting OOM-killed.'
      }
    },
    'geox-url': {
      type: 'object',
      description: {
        ru: `Переопределить URL загрузки баз GeoIP/GeoSite/mmdb по умолчанию — полезно, если основное зеркало MetaCubeX медленное/заблокировано, а есть более быстрое зеркало.`,
        en: 'Override the default download URLs for the GeoIP/GeoSite/mmdb databases — useful when the upstream MetaCubeX mirror is slow/blocked and a faster mirror is available.'
      },
      properties: {
        geoip: { type: 'string' },
        geosite: { type: 'string' },
        mmdb: { type: 'string' }
      }
    },
    'geo-auto-update': {
      type: 'boolean',
      description: {
        ru: `Периодически перезагружать базы GeoIP/GeoSite в фоне, чтобы правила GEOIP/GEOSITE оставались актуальными без ручной замены .dat-файлов.`,
        en: 'Periodically re-download the GeoIP/GeoSite databases in the background, so GEOIP/GEOSITE rules stay current without manually replacing the .dat files.'
      }
    },
    'geo-update-interval': {
      type: 'integer',
      minimum: 1,
      description: {
        ru: `Как часто проверять обновления баз GeoIP/GeoSite, в часах.`,
        en: 'How often to re-check for GeoIP/GeoSite database updates, in hours.'
      }
    },
    ntp: {
      type: 'object',
      description: {
        ru: `Встроенный NTP-клиент Mihomo для коррекции собственных часов. Важно, потому что у большинства роутеров Keenetic нет RTC с батарейкой — после отключения питания часы сбрасываются на устаревшую дату сборки, из-за чего валидация TLS-сертификатов (REALITY/TLS handshake) начинает падать с ошибкой вида "сертификат просрочен/ещё не действителен", пока время не синхронизируется каким-то другим способом.`,
        en: "Built-in NTP client Mihomo can use to correct its own clock. Matters because most Keenetic routers have no battery-backed RTC — after a power loss the clock resets to a stale build date, which breaks TLS certificate validation (REALITY/TLS handshakes fail with 'certificate expired/not yet valid') until the system clock syncs some other way."
      },
      properties: {
        enable: { type: 'boolean' },
        'write-to-system': {
          type: 'boolean',
          description: {
            ru: `Применять синхронизированное время к системным часам ОС, а не только к внутреннему представлению времени Mihomo — нужно, если от корректных часов зависят и другие службы/логи роутера.`,
            en: "Apply the synced time to the OS system clock, not just Mihomo's internal notion of time — needed if other router services/logs also depend on a correct clock."
          }
        },
        server: {
          type: 'string',
          description: {
            ru: `Адрес NTP-сервера для синхронизации.`,
            en: 'NTP server address to sync against.'
          }
        },
        port: { type: 'integer', minimum: 1, maximum: 65535 },
        interval: {
          type: 'integer',
          description: {
            ru: `Интервал повторной синхронизации в секундах.`,
            en: 'Resync interval in seconds.'
          }
        }
      }
    },
    experimental: {
      type: 'object',
      description: {
        ru: `Опциональное поведение ядра, которое ещё может измениться или исчезнуть в следующих релизах Mihomo — эти ключи иногда стоит перепроверять по release notes после обновления.`,
        en: 'Opt-in core behaviour that may still change or be removed between Mihomo releases — expect these keys to occasionally need re-checking against release notes after an update.'
      },
      properties: {
        'ignore-resolve-fail': {
          type: 'boolean',
          description: {
            ru: `Продолжать попытку подключения, даже если собственный DNS-резолв Mihomo для домена не удался, позволяя резолвить его на стороне удалённого DNS прокси-сервера — полезно с доменами, которые резолвятся только из сети сервера прокси (например, за GFW-подобным DNS-отравлением).`,
            en: "Keep dialing even when Mihomo's own DNS resolution fails for a domain, letting the proxy's own remote DNS resolve it instead — useful with domains that are only resolvable from the proxy server's network (e.g. behind GFW-style DNS poisoning)."
          }
        },
        'dialer-ip-version': {
          type: 'string',
          enum: ['dual', '4', '6', 'ipv4', 'ipv6', 'ipv4-prefer', 'ipv6-prefer'],
          description: {
            ru: `Какое семейство IP предпочитает Mihomo, если у назначения есть и A, и AAAA записи. Форсируйте "ipv4"/"4", если IPv6-аплинк роутера нестабилен или недоступен — иначе соединения будут зависать на мёртвом IPv6-маршруте.`,
            en: 'Which IP family Mihomo prefers when a destination has both A and AAAA records. Force "ipv4"/"4" if the router\'s IPv6 uplink is flaky or unavailable, to stop connections stalling on a dead IPv6 route.'
          }
        }
      }
    },
    sniffer: {
      type: 'object',
      description: {
        ru: `Заглядывает в TLS SNI / HTTP Host / QUIC handshake, чтобы восстановить реальный домен соединения, когда для матчинга правил доступен только IP-адрес. Именно это заставляет доменные правила работать корректно под redir-port/tproxy-port или в режиме fake-ip, где Mihomo иначе видит только голый IP назначения.`,
        en: 'Peeks at TLS SNI / HTTP Host / QUIC handshakes to recover the real domain of a connection when only an IP address is available for rule matching. This is what makes DOMAIN-based rules work correctly under redir-port/tproxy-port or fake-ip mode, where Mihomo otherwise only sees a bare destination IP.'
      },
      properties: {
        enable: {
          type: 'boolean',
          description: {
            ru: `Включить sniffer — нужен, чтобы доменные правила надёжно работали под TUN/прозрачным проксированием с fake-ip.`,
            en: 'Turn sniffing on — needed for domain-based rules to work reliably under TUN/transparent proxying with fake-ip.'
          }
        },
        'force-dns-mapping': {
          type: 'boolean',
          description: {
            ru: `Восстанавливать домен из кэша fake-ip, даже если сам sniffer не смог прочитать его из трафика.`,
            en: 'Recover the domain from the fake-ip cache even when the sniffer itself could not read it out of the traffic.'
          }
        },
        'parse-pure-ip': {
          type: 'boolean',
          description: {
            ru: `Пытаться sniff'ить даже для соединений, установленных напрямую по IP (без предшествующего DNS-запроса).`,
            en: 'Attempt sniffing even for connections made straight by IP (no prior DNS lookup involved).'
          }
        },
        'override-destination': {
          type: 'boolean',
          description: {
            ru: `Заменять адрес назначения соединения на распознанный домен перед маршрутизацией, а не только использовать его для матчинга правил. Нужно, когда сам IP назначения недостижим напрямую (например, это был fake-ip заглушка). Можно переопределить для конкретного протокола в sniff.HTTP.override-destination и т.д.`,
            en: 'Replace the connection target with the sniffed domain before routing, instead of only using it for rule matching. Needed when the destination IP itself is unreachable directly (e.g. it was a fake-ip placeholder). Can be overridden per protocol in sniff.HTTP.override-destination etc.'
          }
        },
        sniff: {
          type: 'object',
          description: {
            ru: `Какие протоколы sniff'ить и на каких портах их искать. В YAML пустой ключ без значения (например просто "HTTP:") принимается как сокращение для включения протокола с настройками по умолчанию.`,
            en: 'Which protocols to sniff and on which ports to look for them. In YAML a bare key with no value (e.g. just "HTTP:") is accepted as shorthand for enabling that protocol with its defaults.'
          },
          properties: {
            HTTP: {
              oneOf: [
                { type: 'null' },
                {
                  type: 'object',
                  properties: {
                    ports: {
                      type: 'array',
                      items: { oneOf: [{ type: 'integer' }, { type: 'string' }] },
                      description: {
                        ru: `Порты для sniff HTTP (допускаются строковые диапазоны вида "8080-8099"). Типичный порт по умолчанию — 80.`,
                        en: 'Ports to sniff HTTP on (string ranges like "8080-8099" allowed). Typical default: 80.'
                      }
                    },
                    'override-destination': {
                      type: 'boolean',
                      description: {
                        ru: `Переопределение глобального override-destination только для HTTP.`,
                        en: 'Per-protocol override of the global override-destination, for HTTP only.'
                      }
                    }
                  }
                }
              ],
              description: { ru: `Sniff заголовка HTTP Host.`, en: 'Sniff the HTTP Host header.' }
            },
            TLS: {
              oneOf: [
                { type: 'null' },
                {
                  type: 'object',
                  properties: {
                    ports: {
                      type: 'array',
                      items: { oneOf: [{ type: 'integer' }, { type: 'string' }] },
                      description: {
                        ru: `Порты для sniff TLS. Типичный порт по умолчанию — 443.`,
                        en: 'Ports to sniff TLS on. Typical default: 443.'
                      }
                    }
                  }
                }
              ],
              description: {
                ru: `Sniff SNI из TLS ClientHello.`,
                en: 'Sniff SNI from the TLS ClientHello.'
              }
            },
            QUIC: {
              oneOf: [
                { type: 'null' },
                {
                  type: 'object',
                  properties: {
                    ports: {
                      type: 'array',
                      items: { oneOf: [{ type: 'integer' }, { type: 'string' }] },
                      description: { ru: `Порты для sniff QUIC.`, en: 'Ports to sniff QUIC on.' }
                    }
                  }
                }
              ],
              description: {
                ru: `Sniff SNI из Initial handshake пакета QUIC.`,
                en: 'Sniff SNI out of the QUIC Initial handshake packet.'
              }
            }
          }
        },
        'force-domain': {
          type: 'array',
          items: { type: 'string' },
          description: {
            ru: `Всегда sniff'ить эти домены, даже если глобальные условия sniffing иначе бы их пропустили.`,
            en: 'Always sniff these domains even if global sniffing conditions would otherwise skip them.'
          }
        },
        'skip-domain': {
          type: 'array',
          items: { type: 'string' },
          description: {
            ru: `Никогда не sniff'ить эти домены — например, для которых известно, что подмена назначения их ломает.`,
            en: 'Never sniff these domains — e.g. ones known to break when their destination is rewritten.'
          }
        },
        'port-whitelist': {
          type: 'array',
          items: { type: 'integer' },
          description: {
            ru: `Устаревший белый список портов для sniffing — предпочтительнее использовать per-protocol sniff.*.ports.`,
            en: 'Legacy port allow-list for sniffing — prefer the per-protocol sniff.*.ports instead.'
          }
        }
      }
    },
    tun: {
      type: 'object',
      description: {
        ru: `Собственный виртуальный сетевой интерфейс Mihomo для прозрачного проксирования — альтернатива связке \`redir-port\`/\`tproxy-port\` + iptables.\n\n> На этой панели XKeen уже сам управляет прозрачным перехватом через собственные iptables/TPROXY правила на Keenetic. Включение \`tun\` поверх этого избыточно, и оба механизма могут конфликтовать за один и тот же трафик — оставляйте выключенным, если осознанно не переключили режим перехвата XKeen на \`tun\`.`,
        en: "Mihomo's own virtual network interface for transparent proxying — an alternative to the `redir-port`/`tproxy-port` + iptables approach.\n\n> On this panel XKeen already owns transparent interception via its own iptables/TPROXY rules on Keenetic. Enabling `tun` on top of that is redundant and the two can fight over the same traffic — leave it disabled unless you specifically switched XKeen's interception mode to rely on `tun`."
      },
      properties: {
        enable: {
          type: 'boolean',
          description: {
            ru: `Включить TUN-устройство. Обязательно для прозрачного режима вообще без iptables/nftables REDIRECT/TPROXY.`,
            en: 'Turn the TUN device on. Required for transparent proxying without iptables/nftables REDIRECT/TPROXY at all.'
          }
        },
        device: {
          type: 'string',
          description: {
            ru: `Имя создаваемого TUN-интерфейса (например "Mihomo" или "utun").`,
            en: 'Name of the TUN interface Mihomo creates (e.g. "Mihomo" or "utun").'
          }
        },
        stack: {
          type: 'string',
          enum: ['system', 'gvisor', 'mixed'],
          description: {
            ru: `Пользовательский сетевой стек TUN-устройства. "gvisor" — самый безопасный портируемый вариант по умолчанию, рекомендуется на Keenetic; "system" использует стек ядра (быстрее, только Linux/Android, чувствительнее к особенностям ядра); "mixed" — system для TCP + gvisor для UDP.`,
            en: 'Userspace network stack backing the TUN device. "gvisor" is the safest portable default and recommended on Keenetic; "system" uses the kernel network stack (faster, Linux/Android only, more sensitive to kernel quirks); "mixed" is system for TCP + gvisor for UDP.'
          }
        },
        'dns-hijack': {
          type: 'array',
          items: { type: 'string' },
          description: {
            ru: `Адреса DNS-серверов, чьи запросы перехватываются и обслуживаются собственным DNS Mihomo. "any:53" перехватывает вообще весь DNS-трафик независимо от назначения.`,
            en: 'DNS server addresses whose queries get intercepted and answered by Mihomo\'s own DNS instead. "any:53" hijacks all DNS traffic regardless of destination.'
          }
        },
        'auto-route': {
          type: 'boolean',
          description: {
            ru: `Разрешить Mihomo самостоятельно добавлять default-маршрут через TUN-устройство. На Keenetic это конфликтует с собственной настройкой маршрутизации/iptables в XKeen и обычно не работает как ожидается — маршруты должны управляться скриптами XKeen, поэтому оставляйте false.`,
            en: "Let Mihomo add its own default route through the TUN device automatically. On Keenetic this conflicts with XKeen's own routing/iptables setup and typically does not work as expected — routes must be managed by XKeen's scripts instead, so leave this false."
          }
        },
        'auto-detect-interface': {
          type: 'boolean',
          description: {
            ru: `Автоматически определять upstream (WAN) интерфейс. Без этого interface-name нужно задавать вручную, чтобы TUN-маршрутизация работала.`,
            en: 'Auto-detect the upstream (WAN) interface. Without it, interface-name must be set manually for TUN routing to work.'
          }
        },
        'strict-route': {
          type: 'boolean',
          description: {
            ru: `Жёстко принуждать маршрутизацию через TUN, блокируя обход трафика через альтернативные маршруты. Может полностью сломать связность, если настройка маршрутов не идеально точна — включайте только при отладке конкретной утечки.`,
            en: 'Enforce strict routing through TUN, blocking traffic from slipping out via any alternate route. Can break connectivity entirely if the route setup is not exactly right — leave off unless troubleshooting a specific leak.'
          }
        },
        mtu: {
          type: 'integer',
          description: {
            ru: `MTU TUN-интерфейса. Типичные значения: 1500 (обычный Ethernet), 1492 (PPPoE WAN), 9000 (jumbo frames, только для внутренних нужд).`,
            en: 'MTU of the TUN interface. Typical values: 1500 (plain Ethernet), 1492 (PPPoE WAN), 9000 (jumbo frames, internal use only).'
          }
        },
        'inet4-address': {
          type: 'array',
          items: { type: 'string' },
          description: {
            ru: `IPv4 CIDR-адрес(а), назначенные TUN-интерфейсу (по умолчанию 198.18.0.1/30).`,
            en: 'IPv4 CIDR address(es) assigned to the TUN interface (default 198.18.0.1/30).'
          }
        },
        'inet6-address': {
          type: 'array',
          items: { type: 'string' },
          description: {
            ru: `IPv6 CIDR-адрес(а), назначенные TUN-интерфейсу (по умолчанию fdfe:dcba:9876::1/126).`,
            en: 'IPv6 CIDR address(es) assigned to the TUN interface (default fdfe:dcba:9876::1/126).'
          }
        },
        'inet4-route-address': {
          type: 'array',
          items: { type: 'string' },
          description: {
            ru: `IPv4 CIDR, маршрутизируемые через TUN (по умолчанию 0.0.0.0/0 — всё — если оставить пустым).`,
            en: 'IPv4 CIDRs routed through TUN (defaults to 0.0.0.0/0 — everything — if left empty).'
          }
        },
        'inet6-route-address': {
          type: 'array',
          items: { type: 'string' },
          description: {
            ru: `IPv6 CIDR, маршрутизируемые через TUN.`,
            en: 'IPv6 CIDRs routed through TUN.'
          }
        },
        'inet4-route-exclude-address': {
          type: 'array',
          items: { type: 'string' },
          description: {
            ru: `IPv4 CIDR, исключённые из маршрутизации через TUN (идут в обход туннеля) — как правило LAN-диапазоны и CGNAT-пространство, которые никогда не должны идти через прокси.`,
            en: 'IPv4 CIDRs excluded from TUN routing (sent outside the tunnel) — typically LAN ranges and CGNAT space that must never go through the proxy.'
          }
        },
        'inet6-route-exclude-address': {
          type: 'array',
          items: { type: 'string' },
          description: {
            ru: `IPv6 CIDR, исключённые из маршрутизации через TUN.`,
            en: 'IPv6 CIDRs excluded from TUN routing.'
          }
        },
        'endpoint-independent-nat': {
          type: 'boolean',
          description: {
            ru: `Включить поведение Full-Cone (EIM/EIF) NAT для UDP через TUN. Улучшает связность P2P/голосовых звонков, но увеличивает потребление памяти.`,
            en: 'Enable Full-Cone (EIM/EIF) NAT behaviour for UDP through TUN. Improves P2P/voice-call connectivity but increases memory use.'
          }
        },
        'include-uid': {
          type: 'array',
          items: { type: 'integer' },
          description: {
            ru: `Перехватывать трафик только от этих Linux UID. На роутере используется редко.`,
            en: 'Only capture traffic from these Linux UIDs. Rarely used on a router.'
          }
        },
        'exclude-uid': {
          type: 'array',
          items: { type: 'integer' },
          description: {
            ru: `Исключить трафик этих Linux UID из захвата TUN — например, собственный UID Mihomo, чтобы предотвратить петлю маршрутизации в саму себя.`,
            en: "Exclude traffic from these Linux UIDs from TUN capture — e.g. Mihomo's own UID, to prevent a routing loop back into itself."
          }
        },
        'include-package': {
          type: 'array',
          items: { type: 'string' },
          description: {
            ru: `Перехватывать трафик только этих Android-пакетов (только сборки для Android).`,
            en: 'Only capture traffic from these Android app package names (Android builds only).'
          }
        },
        'exclude-package': {
          type: 'array',
          items: { type: 'string' },
          description: {
            ru: `Исключить трафик этих Android-пакетов (только сборки для Android).`,
            en: 'Exclude traffic from these Android app package names (Android builds only).'
          }
        }
      }
    },
    dns: {
      type: 'object',
      description: {
        ru: `Встроенный DNS-резолвер Mihomo. Без TUN резолвинг обычно берёт на себя сам роутер (Keenetic Cloud/AdGuard Home/и т.п.), и весь этот блок можно оставить выключенным; с TUN + enhanced-mode: fake-ip dns.enable: true обязателен, чтобы Mihomo сам мог перехватывать и отвечать на доменные запросы.`,
        en: "Mihomo's built-in DNS resolver. Without TUN, the router's own DNS (Keenetic Cloud/AdGuard Home/etc.) usually handles resolution and this whole block can stay off; with TUN + enhanced-mode: fake-ip, dns.enable: true is required so Mihomo can intercept and answer domain lookups itself."
      },
      properties: {
        enable: {
          type: 'boolean',
          description: {
            ru: `Включить встроенный DNS-сервер. Обязательно для режима TUN; для конфигураций без TUN часто оставляют false, чтобы DNS обслуживал сам роутер.`,
            en: 'Turn on the built-in DNS server. Mandatory for TUN mode; for non-TUN setups this is often left false so the router handles DNS itself.'
          }
        },
        'prefer-h3': {
          type: 'boolean',
          description: {
            ru: `Предпочитать DoH3 (DNS-over-HTTPS поверх HTTP/3-over-QUIC) для DoH-серверов. Быстрее там, где работает, но может падать на линках, где провайдер фильтрует/режет UDP/QUIC.`,
            en: 'Prefer DoH3 (DNS-over-HTTPS via HTTP/3-over-QUIC) for DoH servers. Faster where it works, but can fail on links where the ISP filters/throttles UDP/QUIC.'
          }
        },
        listen: {
          type: 'string',
          description: {
            ru: `Адрес:порт, на котором слушает внутренний DNS-сервер. Пустая строка означает, что он используется только внутренне (обычно случай TUN).`,
            en: 'Address:port the internal DNS server listens on. Empty string means it is only used internally (typically the case for TUN).'
          }
        },
        ipv6: {
          type: 'boolean',
          description: {
            ru: `Разрешать AAAA-ответы от этого резолвера. Оставляйте false без рабочего нативного IPv6, чтобы избежать медленных/неудачных IPv6-запросов.`,
            en: 'Allow AAAA answers from this resolver. Leave false without working native IPv6, to avoid slow/failed IPv6 lookups.'
          }
        },
        'ipv6-timeout': {
          type: 'integer',
          description: {
            ru: `Сколько миллисекунд ждать AAAA-ответ, прежде чем сдаться. Короткий таймаут ускоряет резолвинг в IPv4-only сетях.`,
            en: 'Milliseconds to wait for an AAAA answer before giving up. A short timeout speeds up resolution on IPv4-only networks.'
          }
        },
        'default-nameserver': {
          type: 'array',
          items: { type: 'string' },
          description: {
            ru: `Bootstrap DNS-серверы, используемые только для резолва хостнеймов DoH/DoT/DoQ-серверов, перечисленных в других полях — обязательно должны быть обычными IP, не доменами, чтобы не возникало циклической зависимости при резолвинге.`,
            en: 'Bootstrap DNS servers used only to resolve the hostnames of DoH/DoT/DoQ servers listed elsewhere — must be plain IPs, not domains, so there is no chicken-and-egg lookup problem.'
          }
        },
        'enhanced-mode': {
          type: 'string',
          enum: ['fake-ip', 'redir-host', 'normal'],
          description: {
            ru: `"fake-ip": Mihomo отвечает на DNS-запросы адресами из fake-ip-range и восстанавливает реальный домен внутри себя при подключении — самый надёжный вариант для доменных правил, хорошо работает с redir-port/tproxy-port, но ломает приложения, которые жёстко сверяют/сравнивают IP-адреса. "redir-host": подменяет DNS-ответ на собственный listen-адрес Mihomo — проще, но переносит домен только через HTTP Host-заголовок. "normal": без подмены, доменные правила под прозрачным проксированием вообще требуют sniffer.`,
            en: '"fake-ip": Mihomo answers DNS queries with addresses from fake-ip-range and maps them back to the real domain internally — most reliable for DOMAIN-based rules and works well with redir-port/tproxy-port, but breaks apps that hardcode/compare IP addresses. "redir-host": rewrites DNS answers to Mihomo\'s own listen address — simpler but only carries the domain through HTTP Host headers. "normal": no rewriting, DOMAIN rules need sniffer to work at all under transparent proxying.'
          }
        },
        'fake-ip-range': {
          type: 'string',
          pattern: '^([0-9]{1,3}\\.){3}[0-9]{1,3}/\\d{1,2}$',
          description: {
            ru: `CIDR-пул адресов-заглушек, выдаваемых в режиме fake-ip. Не должен пересекаться ни с одной реальной подсетью, используемой в LAN/WAN (диапазон по умолчанию 198.18.0.0/16 зарезервирован IANA под бенчмарки — безопасен, пока что-то ещё в сети его уже не занимает).`,
            en: 'CIDR pool of placeholder IPs handed out in fake-ip mode. Must not overlap any real subnet in use on the LAN/WAN (default 198.18.0.0/16 is an IANA-reserved benchmarking range, safe to keep unless something else on the network already uses it).'
          }
        },
        'fake-ip-filter': {
          type: 'array',
          items: { type: 'string' },
          description: {
            ru: `Домены, которые всегда получают реальный IP вместо fake-ip — нужно для всего, что ломается под fake-ip: LAN-хостнеймы, NTP, сервисы с проверкой обратного IP.`,
            en: 'Domains that always get their real IP instead of a fake one — needed for anything that breaks under fake-ip, e.g. LAN hostnames, NTP, or services that do reverse-IP checks.'
          }
        },
        'fake-ip-filter-mode': {
          type: 'string',
          enum: ['blacklist', 'whitelist'],
          description: {
            ru: `"blacklist": fake-ip применяется ко всем доменам, кроме перечисленных в fake-ip-filter. "whitelist": fake-ip применяется только к доменам из fake-ip-filter, остальные резолвятся обычным образом.`,
            en: '"blacklist": fake-ip applies to everything except domains in fake-ip-filter. "whitelist": fake-ip applies only to domains listed in fake-ip-filter, everything else resolves normally.'
          }
        },
        nameserver: {
          type: 'array',
          items: { type: 'string' },
          description: {
            ru: `Основные DNS-серверы для резолвинга клиентского трафика. Поддерживаются формы: plain (ip:port), DoH (https://), DoT (tls://), DoQ (quic://), DHCP (dhcp://eth0).`,
            en: 'Primary DNS servers for resolving client traffic. Supports plain (ip:port), DoH (https://), DoT (tls://), DoQ (quic://), and DHCP (dhcp://eth0) forms.'
          }
        },
        fallback: {
          type: 'array',
          items: { type: 'string' },
          description: {
            ru: `Резервные DNS-серверы. Если ответ основного сервера подпадает под fallback-filter, запрос повторяется через них — классический приём против отравления/подмены DNS.`,
            en: 'Backup DNS servers. If a nameserver answer matches fallback-filter, the query is retried against these instead — the classic technique for working around DNS poisoning/injection.'
          }
        },
        'fallback-filter': {
          type: 'object',
          description: {
            ru: `Условия, при которых срабатывает повторный запрос через fallback: если ответ основного сервера подходит под любое из них, Mihomo отбрасывает его и спрашивает fallback.`,
            en: 'Conditions that trigger a fallback re-query: if the primary nameserver answer matches any of these, Mihomo discards it and asks fallback instead.'
          },
          properties: {
            geoip: {
              type: 'boolean',
              description: {
                ru: `Включить срабатывание fallback по GeoIP.`,
                en: 'Enable GeoIP-based fallback triggering.'
              }
            },
            'geoip-code': {
              type: 'string',
              description: {
                ru: `Код страны, при попадании ответа в которую срабатывает fallback (например "CN").`,
                en: 'Country code that triggers fallback when the answer resolves inside it (e.g. "CN").'
              }
            },
            geosite: {
              type: 'array',
              items: { type: 'string' },
              description: {
                ru: `Домены из этих geosite-категорий всегда резолвятся через fallback.`,
                en: 'Domains in these geosite categories always resolve via fallback.'
              }
            },
            ipcidr: {
              type: 'array',
              items: { type: 'string' },
              description: {
                ru: `Диапазоны CIDR в ответе, вызывающие повторный запрос через fallback.`,
                en: 'Answer CIDR ranges that trigger a fallback re-query.'
              }
            },
            domain: {
              type: 'array',
              items: { type: 'string' },
              description: {
                ru: `Домены, которые всегда резолвятся через fallback.`,
                en: 'Domains that always resolve via fallback.'
              }
            }
          }
        },
        'nameserver-policy': {
          type: 'object',
          additionalProperties: {
            oneOf: [{ type: 'string' }, { type: 'array', items: { type: 'string' } }]
          },
          description: {
            ru: `Маршрутизация DNS по домену (или по rule-set): какой сервер(ы) использовать для конкретных доменов, минуя nameserver/fallback целиком. Ключ — домен/rule-set матчер, значение — один сервер или список.`,
            en: 'Per-domain (or per-rule-set) DNS routing: which server(s) to use for specific domains, bypassing nameserver/fallback entirely. Key is a domain/rule-set matcher, value is one server or a list.'
          }
        },
        'proxy-server-nameserver': {
          type: 'array',
          items: { type: 'string' },
          description: {
            ru: `DNS-серверы, используемые специально для резолва доменов из ваших же proxies[*].server — обычно "чистый" DNS, доступный без прохода через сам прокси, чтобы избежать проблемы курицы и яйца.`,
            en: 'DNS servers used specifically to resolve the domains of your own proxies[*].server entries — normally a "clean" DNS that is reachable without going through the proxy itself, to avoid a chicken-and-egg problem.'
          }
        },
        'direct-nameserver': {
          type: 'array',
          items: { type: 'string' },
          description: {
            ru: `DNS-серверы для доменов, попавших под DIRECT/policy, минуя nameserver.`,
            en: 'DNS servers used for domains matched by DIRECT/policy, bypassing nameserver.'
          }
        },
        'use-hosts': {
          type: 'boolean',
          description: {
            ru: `Учитывать собственный блок hosts этого файла при резолвинге.`,
            en: "Consult this file's own hosts block when resolving."
          }
        },
        'use-system-hosts': {
          type: 'boolean',
          description: {
            ru: `Учитывать системный /etc/hosts роутера при резолвинге.`,
            en: "Consult the router's system /etc/hosts when resolving."
          }
        },
        'respect-rules': {
          type: 'boolean',
          description: {
            ru: `Пропускать собственные DNS-запросы Mihomo через обычные правила маршрутизации (чтобы они тоже могли идти через прокси). Полезно, когда единственный доступный DNS находится по ту сторону VPN/прокси.`,
            en: "Route Mihomo's own DNS queries through the normal routing rules (so they can go through a proxy too). Useful when the only reachable DNS is on the other side of a VPN/proxy."
          }
        },
        'cache-algorithm': {
          type: 'string',
          enum: ['lru', 'arc'],
          description: {
            ru: `Алгоритм вытеснения DNS-кэша. "lru" (по умолчанию) — простой Least-Recently-Used; "arc" (Adaptive Replacement Cache) — чуть лучше hit rate ценой большего расхода RAM.`,
            en: 'DNS cache eviction algorithm. "lru" (default) is simple Least-Recently-Used; "arc" (Adaptive Replacement Cache) has a slightly better hit rate at the cost of more RAM.'
          }
        }
      }
    },
    hosts: {
      type: 'object',
      description: {
        ru: `Статические соответствия домен → IP, применяемые раньше любого другого DNS-запроса — встроенный аналог /etc/hosts. Полезно, чтобы закрепить домен прокси-сервера за конкретным IP, или для локальных имён, которые никогда не должны идти через fake-ip/апстрим DNS.`,
        en: 'Static domain → IP overrides resolved before any other DNS lookup — the built-in equivalent of /etc/hosts. Useful for pinning a proxy server domain to a known-good IP, or for local service names that should never go through fake-ip/upstream DNS.'
      },
      additionalProperties: {
        oneOf: [{ type: 'string' }, { type: 'array', items: { type: 'string' } }]
      }
    },
    'proxy-providers': {
      type: 'object',
      description: {
        ru: `Альтернатива ручному перечислению серверов в proxies через подписку: каждая запись автоматически скачивает/обновляет список прокси по URL (или следит за локальным файлом) и может подключаться в proxy-groups через use вместо статичного массива proxies.`,
        en: 'Subscription-based alternative to hand-listing servers in proxies: each entry auto-downloads/refreshes a proxy list from a URL (or watches a local file) and can be referenced from proxy-groups via use, instead of a static proxies array.'
      },
      additionalProperties: {
        type: 'object',
        properties: {
          type: {
            type: 'string',
            enum: ['http', 'file', 'inline'],
            description: { ru: `Тип источника списка узлов.`, en: 'Provider source type' }
          },
          url: {
            type: 'string',
            description: {
              ru: `URL подписки (для type: http).`,
              en: 'Subscription URL (type: http)'
            }
          },
          path: {
            type: 'string',
            description: { ru: `Локальный путь кэша/конфига.`, en: 'Local cache/config path' }
          },
          interval: {
            type: 'integer',
            description: {
              ru: `Интервал автообновления в секундах.`,
              en: 'Auto-update interval in seconds'
            }
          },
          proxy: {
            type: 'string',
            description: {
              ru: `Имя прокси или группы, через которые скачивать/обновлять подписку — только для загрузки провайдера, не выбирает маршрут для пользовательского трафика.`,
              en: "Proxy/group to route the provider's own download/refresh through — only affects fetching the subscription, not user traffic routing."
            }
          },
          'size-limit': {
            type: 'integer',
            description: {
              ru: `Максимальный размер скачиваемого файла провайдера в байтах. 0 — без ограничения.`,
              en: 'Max size of the downloaded provider file, in bytes. 0 means unlimited.'
            }
          },
          header: {
            type: 'object',
            description: {
              ru: `Дополнительные HTTP-заголовки при скачивании подписки: User-Agent, Authorization, HWID и прочие требования конкретного провайдера. Значения — строка или список строк.`,
              en: 'Extra HTTP headers sent when fetching the subscription: User-Agent, Authorization, HWID, or whatever the provider requires. Values are a string or a list of strings.'
            },
            properties: {
              'User-Agent': {
                type: 'array',
                items: { type: 'string' },
                description: {
                  ru: `User-Agent при скачивании подписки.`,
                  en: 'User-Agent sent when fetching the subscription.'
                }
              },
              'x-hwid': {
                type: 'array',
                items: { type: 'string' },
                description: {
                  ru: `HWID устройства для подписок, привязанных к устройству/роутеру.`,
                  en: 'Device HWID for subscriptions tied to a specific device/router.'
                }
              },
              'x-device-os': {
                type: 'array',
                items: { type: 'string' },
                description: {
                  ru: `ОС устройства, которую ожидает сервер подписки.`,
                  en: 'Device OS expected by the subscription server.'
                }
              },
              'x-ver-os': {
                type: 'array',
                items: { type: 'string' },
                description: { ru: `Версия ОС устройства.`, en: 'Device OS version.' }
              },
              'x-device-model': {
                type: 'array',
                items: { type: 'string' },
                description: {
                  ru: `Модель устройства для HWID-подписки.`,
                  en: 'Device model for an HWID-bound subscription.'
                }
              }
            }
          },
          'health-check': {
            type: 'object',
            description: {
              ru: `Проверка доступности и задержки уже загруженных узлов провайдера — отдельно от скачивания самой подписки (за него отвечает interval).`,
              en: "Availability/latency checks for the provider's already-loaded nodes — separate from re-downloading the subscription itself, which interval controls."
            },
            properties: {
              enable: {
                type: 'boolean',
                description: {
                  ru: `Включить health-check для узлов этого провайдера.`,
                  en: "Enable health-checking for this provider's nodes."
                }
              },
              url: {
                type: 'string',
                description: {
                  ru: `URL проверки доступности. Обычно короткий endpoint, быстро возвращающий 204 (например https://www.gstatic.com/generate_204).`,
                  en: 'Health-check URL — typically a short endpoint that quickly returns 204 (e.g. https://www.gstatic.com/generate_204).'
                }
              },
              interval: {
                type: 'integer',
                description: {
                  ru: `Интервал health-check в секундах — как часто проверять уже загруженные узлы (не путать с interval провайдера — тот про скачивание подписки).`,
                  en: "Health-check interval in seconds — how often already-loaded nodes are re-checked (not to be confused with the provider's own interval, which is about re-downloading the subscription)."
                }
              },
              timeout: {
                type: 'integer',
                description: {
                  ru: `Таймаут одной проверки в миллисодах.`,
                  en: 'Timeout for one check, in milliseconds.'
                }
              },
              lazy: {
                type: 'boolean',
                description: {
                  ru: `Ленивый режим — по умолчанию true: для узлов, которые сейчас не используются, плановая проверка не выполняется.`,
                  en: 'Lazy mode — defaults to true: nodes not currently in use skip their scheduled check.'
                }
              },
              'expected-status': {
                type: 'string',
                description: {
                  ru: `Ожидаемый HTTP-статус ответа. Поддерживает число, список через "/" и диапазон через "-".`,
                  en: 'Expected HTTP response status. Supports a number, a "/"-separated list, or a "-" range.'
                }
              }
            }
          },
          filter: {
            type: 'string',
            description: {
              ru: `Regex-фильтр по именам прокси.`,
              en: 'Regex filter applied to proxy names'
            }
          },
          'exclude-filter': {
            type: 'string',
            description: {
              ru: `Regex-фильтр исключения по именам прокси.`,
              en: 'Regex exclusion filter applied to proxy names'
            }
          },
          'exclude-type': {
            type: 'string',
            description: {
              ru: `Regex-фильтр исключения по типам прокси.`,
              en: 'Regex filter excluding proxy types'
            }
          },
          payload: {
            type: 'array',
            items: { type: 'object' },
            description: {
              ru: `Inline-список прокси (для type: inline), заданный прямо внутри этого конфига вместо отдельного файла.`,
              en: 'Inline proxy list (type: inline) defined directly in this config instead of a separate file.'
            }
          },
          override: {
            type: 'object',
            description: {
              ru: `Массовые переопределения полей, накладываемые поверх всех прокси из этого провайдера — например включить UDP всем узлам сразу или добавить префикс к именам.`,
              en: 'Bulk field overrides applied on top of every proxy from this provider — e.g. force UDP on for all nodes at once, or add a name prefix.'
            },
            properties: {
              tfo: {
                type: 'boolean',
                description: {
                  ru: `Массово включить/выключить TCP Fast Open.`,
                  en: 'Bulk override TCP Fast Open.'
                }
              },
              mptcp: {
                type: 'boolean',
                description: {
                  ru: `Массово переопределить MPTCP.`,
                  en: 'Bulk override Multipath TCP.'
                }
              },
              udp: {
                type: 'boolean',
                description: {
                  ru: `Массово переопределить поддержку UDP.`,
                  en: 'Bulk override UDP support.'
                }
              },
              'udp-over-tcp': {
                type: 'boolean',
                description: {
                  ru: `Массово переопределить UDP-over-TCP для Shadowsocks-подобных узлов, если сервер это поддерживает.`,
                  en: 'Bulk override UDP-over-TCP for Shadowsocks-like nodes, where the server supports it.'
                }
              },
              up: {
                type: 'string',
                description: {
                  ru: `Лимит/заявленная скорость uplink для Hysteria/Hysteria2/TUIC-подобных узлов.`,
                  en: 'Declared uplink speed for Hysteria/Hysteria2/TUIC-style nodes.'
                }
              },
              down: {
                type: 'string',
                description: {
                  ru: `Лимит/заявленная скорость downlink для Hysteria/Hysteria2/TUIC-подобных узлов.`,
                  en: 'Declared downlink speed for Hysteria/Hysteria2/TUIC-style nodes.'
                }
              },
              'skip-cert-verify': {
                type: 'boolean',
                description: {
                  ru: `Массово переопределить проверку TLS-сертификата.`,
                  en: 'Bulk override TLS certificate verification.'
                }
              },
              'dialer-proxy': {
                type: 'string',
                description: {
                  ru: `Массово задать dialer-proxy для всех узлов провайдера.`,
                  en: 'Bulk-set dialer-proxy for every node from this provider.'
                }
              },
              'interface-name': {
                type: 'string',
                description: {
                  ru: `Массово задать исходящий интерфейс для узлов провайдера.`,
                  en: "Bulk-set the outbound interface for this provider's nodes."
                }
              },
              'routing-mark': {
                type: 'integer',
                description: {
                  ru: `Массово задать fwmark для узлов провайдера.`,
                  en: "Bulk-set fwmark for this provider's nodes."
                }
              },
              'ip-version': {
                type: 'string',
                enum: ['dual', 'ipv4', 'ipv6', 'ipv4-prefer', 'ipv6-prefer'],
                description: {
                  ru: `Массово переопределить предпочитаемое семейство IP при подключении к доменным серверам провайдера.`,
                  en: "Bulk override the preferred IP family when connecting to this provider's domain-based servers."
                }
              },
              'additional-prefix': {
                type: 'string',
                description: {
                  ru: `Добавить фиксированный префикс к имени каждого узла провайдера.`,
                  en: 'Prepend a fixed prefix to every node name from this provider.'
                }
              },
              'additional-suffix': {
                type: 'string',
                description: {
                  ru: `Добавить фиксированный суффикс к имени каждого узла провайдера.`,
                  en: 'Append a fixed suffix to every node name from this provider.'
                }
              },
              'proxy-name': {
                type: 'array',
                description: {
                  ru: `Правила переименования узлов по регулярным выражениям — pattern ищет часть имени, target задаёт замену (можно использовать группы regex вида $1).`,
                  en: 'Regex-based node rename rules — pattern matches part of the name, target is the replacement (regex groups like $1 are allowed).'
                },
                items: {
                  type: 'object',
                  properties: {
                    pattern: {
                      type: 'string',
                      description: {
                        ru: `Regex, ищущий часть имени узла.`,
                        en: 'Regex matching part of the node name.'
                      }
                    },
                    target: {
                      type: 'string',
                      description: {
                        ru: `Строка замены (можно использовать $1 и другие группы regex).`,
                        en: 'Replacement string ($1 and other regex groups allowed).'
                      }
                    }
                  }
                }
              }
            }
          }
        },
        required: ['type']
      }
    },
    proxies: {
      type: 'array',
      description: { ru: `Список определений прокси-серверов`, en: 'Proxy server definitions' },
      items: {
        type: 'object',
        properties: {
          name: {
            type: 'string',
            description: {
              ru: `Уникальное имя прокси. Используется в proxy-groups[].proxies и в правилах (MATCH,<имя>).`,
              en: 'Unique proxy name. Referenced from proxy-groups[].proxies and rules (MATCH,<name>).'
            }
          },
          type: {
            type: 'string',
            enum: [
              'ss',
              'ssr',
              'vmess',
              'vless',
              'trojan',
              'hysteria',
              'hysteria2',
              'wireguard',
              'openvpn',
              'tailscale',
              'tuic',
              'socks5',
              'http',
              'ssh',
              'snell',
              'anytls',
              'direct'
            ],
            description: {
              ru: `Протокол/политика этого узла. "direct" отправляет трафик напрямую вообще без server/port (именованный алиас для DIRECT). "vless"+reality-opts и "hysteria2" — текущие рекомендуемые варианты для обхода DPI.`,
              en: 'Protocol/policy for this node. "direct" sends traffic straight out with no server/port at all (a named alias for DIRECT). "vless"+reality-opts and "hysteria2" are the current recommended choices for evading DPI.'
            }
          },
          server: {
            type: 'string',
            description: {
              ru: `Адрес сервера — домен или IP.`,
              en: 'Server address — domain or IP.'
            }
          },
          port: {
            type: 'integer',
            minimum: 1,
            maximum: 65535,
            description: { ru: `Порт сервера.`, en: 'Server port.' }
          },
          username: {
            type: 'string',
            description: {
              ru: `Имя пользователя для socks5/http-аутентификации, либо имя пользователя OpenVPN auth-user-pass.`,
              en: 'Username for socks5/http auth, or the OpenVPN auth-user-pass username.'
            }
          },
          password: {
            type: 'string',
            description: {
              ru: `Пароль — для ss/trojan/hysteria2/snell/tuic/socks5/http, а также OpenVPN auth-user-pass.`,
              en: 'Password — for ss/trojan/hysteria2/snell/tuic/socks5/http, and OpenVPN auth-user-pass.'
            }
          },
          uuid: {
            type: 'string',
            pattern:
              '^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$',
            description: {
              ru: `UUID клиента для vmess/vless, обычно в формате 8-4-4-4-12.`,
              en: 'Client UUID for vmess/vless, usually in 8-4-4-4-12 form.'
            }
          },
          alterId: {
            type: 'integer',
            description: {
              ru: `Устаревший AlterID VMess. Современный VMess (AEAD) всегда использует 0 — большие значения больше не поддерживаются.`,
              en: 'Legacy VMess AlterID. Modern VMess (AEAD) always uses 0 — higher values are no longer supported.'
            }
          },
          cipher: {
            type: 'string',
            description: {
              ru: `Метод шифрования. Для ss: aes-256-gcm, chacha20-ietf-poly1305, 2022-blake3-aes-256-gcm и т.д. Для vmess рекомендуется "auto".`,
              en: 'Encryption method. For ss: aes-256-gcm, chacha20-ietf-poly1305, 2022-blake3-aes-256-gcm, etc. For vmess: "auto" is recommended.'
            }
          },
          udp: {
            type: 'boolean',
            description: {
              ru: `Разрешить UDP через этот прокси. По умолчанию выключено для обычных TCP-протоколов; UDP-нативные типы (TUIC) и direct/dns включают его автоматически. Включение здесь не заставит сервер поддерживать UDP, если он реально не умеет.`,
              en: 'Allow UDP through this proxy. Off by default for ordinary TCP protocols; UDP-native types (TUIC) and direct/dns enable it automatically. Turning it on does not make the server support UDP if it actually cannot.'
            }
          },
          tfo: {
            type: 'boolean',
            description: {
              ru: `TCP Fast Open — отправляет данные уже на этапе handshake, экономя один round-trip. Только для TCP, требует поддержки ядра/сети с обеих сторон; при странном поведении соединений сначала попробуйте выключить.`,
              en: 'TCP Fast Open — sends data during the handshake to shave off a round trip. TCP-only, and needs kernel/network support on both ends; if connections behave oddly, try turning it off first.'
            }
          },
          mptcp: {
            type: 'boolean',
            description: {
              ru: `Multipath TCP — позволяет одному TCP-соединению использовать несколько сетевых путей там, где это поддерживают и ядро, и сервер. Только для TCP, на обычном домашнем роутере редко актуально; как правило оставляют false.`,
              en: 'Multipath TCP — lets one TCP connection use several network paths where both the kernel and the server support it. TCP-only and rarely relevant on a typical home router; usually left false.'
            }
          },
          'skip-cert-verify': {
            type: 'boolean',
            description: {
              ru: `Принимать TLS-сертификат сервера без проверки. Только для self-signed сертификатов на сервере, который вы контролируете — включение обычно сводит на нет смысл TLS, допуская тривиальный MITM.`,
              en: 'Accept the server TLS certificate without validation. Only for self-signed certs on a server you control — leaving this on generally defeats the point of TLS by allowing a trivial man-in-the-middle.'
            }
          },
          tls: {
            type: 'boolean',
            description: {
              ru: `Включить TLS. Для vless+reality это тоже должно быть true, вместе с параметрами REALITY в reality-opts.`,
              en: 'Enable TLS. For vless+reality this must also be true, with the REALITY parameters in reality-opts.'
            }
          },
          sni: {
            type: 'string',
            description: {
              ru: `TLS Server Name Indication. В документации некоторых типов прокси это поле называется servername — оба варианта описывают одно и то же понятие TLS.`,
              en: 'TLS Server Name Indication. Some proxy types document this as servername instead — both are accepted as the same TLS concept.'
            }
          },
          servername: {
            type: 'string',
            description: {
              ru: `TLS Server Name Indication. Для REALITY это домен прикрытия, под который маскируется соединение — он должен быть в allow-list сервера.`,
              en: "TLS Server Name Indication. For REALITY, this is the cover domain the connection masquerades as — it must be on the server's allow-list."
            }
          },
          fingerprint: {
            type: 'string',
            description: {
              ru: `SHA-256 pinned-отпечаток TLS-сертификата сервера. Если задан, сертификат проверяется по этому отпечатку вместо обычной цепочки CA.`,
              en: "SHA-256 pinned fingerprint of the server's TLS certificate. When set, the certificate is checked against this fingerprint instead of the normal CA chain."
            }
          },
          alpn: {
            type: 'array',
            items: { type: 'string' },
            description: {
              ru: `Список ALPN-протоколов TLS в порядке приоритета. Для XHTTP по умолчанию используется h2; укажите ["h3"] или ["http/1.1"], чтобы изменить.`,
              en: 'TLS ALPN protocol list in priority order. XHTTP defaults to h2; specify ["h3"] or ["http/1.1"] to change it.'
            }
          },
          certificate: {
            type: 'string',
            description: {
              ru: `Клиентский сертификат для mTLS — содержимое PEM или путь к файлу. Используется вместе с private-key.`,
              en: 'Client certificate for mTLS — PEM content or a file path. Used together with private-key.'
            }
          },
          'client-fingerprint': {
            type: 'string',
            enum: [
              'none',
              'chrome',
              'firefox',
              'safari',
              'ios',
              'android',
              'edge',
              '360',
              'qq',
              'random',
              'randomized',
              'chrome120',
              'firefox120',
              'safari16',
              'chrome_psk',
              'chrome_psk_shuffle',
              'chrome_padding_psk_shuffle',
              'chrome_pq',
              'chrome_pq_psk'
            ],
            description: {
              ru: `uTLS ClientHello fingerprint — делает TLS-хендшейк похожим на реальный браузер вместо дефолтного Go, который многие системы DPI фингерпринтят и блокируют именно потому, что он не похож на обычный браузерный трафик. Для REALITY обычно используют chrome, firefox, ios или random.`,
              en: "uTLS ClientHello fingerprint — makes the TLS handshake look like a real browser's instead of Go's default, which many DPI systems fingerprint and block precisely because it does not look like normal browser traffic. For REALITY, chrome/firefox/ios/random are the common choices."
            }
          },
          flow: {
            type: 'string',
            enum: ['xtls-rprx-vision', ''],
            description: {
              ru: `XTLS flow только для VLESS. "xtls-rprx-vision" избегает двойного шифрования TLS-in-TLS ради реального прироста скорости, но применяется только с tls: true (обычно вместе с reality-opts) и несовместим с mux/grpc/xhttp — для этих транспортов оставляйте пустым.`,
              en: 'VLESS-only XTLS flow control. "xtls-rprx-vision" avoids double-encrypting TLS-in-TLS traffic for a real throughput gain, but only applies with tls: true (typically paired with reality-opts) and is incompatible with mux/grpc/xhttp — leave empty for those transports.'
            }
          },
          'packet-encoding': {
            type: 'string',
            enum: ['', 'packetaddr', 'xudp'],
            description: {
              ru: `UDP packet encoding для VLESS. Пустая строка означает raw/исходное кодирование; "packetaddr" поддерживается v2ray 5+, "xudp" — вариант, ориентированный на Xray/Mihomo.`,
              en: 'VLESS UDP packet encoding. Empty string means raw/original encoding; "packetaddr" is supported by v2ray 5+, "xudp" is the Xray/Mihomo-oriented option.'
            }
          },
          encryption: {
            type: 'string',
            description: {
              ru: `VLESS encryption. Обычный VLESS в Mihomo чаще всего использует пустую строку (encryption: ""); новые пост-квантовые варианты ML-KEM задаются длинной строкой вида "mlkem768x25519plus..." согласно документации Mihomo.`,
              en: 'VLESS encryption. Ordinary VLESS in Mihomo commonly uses an empty string (encryption: ""); newer post-quantum ML-KEM variants use a long "mlkem768x25519plus..." string per Mihomo\'s own documentation.'
            }
          },
          network: {
            type: 'string',
            enum: ['raw', 'tcp', 'ws', 'grpc', 'h2', 'http', 'xhttp'],
            description: {
              ru: `Как этот прокси оборачивает/маскирует трафик поверх базового TCP/TLS-соединения. "raw"/"tcp" — обычный TCP (также то, во что Mihomo превращает незаданное значение); "ws" — WebSocket; "xhttp" — современный HTTP-based транспорт; "grpc" — gRPC; "h2"/"http" — HTTP/2. Должно совпадать с настройкой сервера, а соответствующий блок *-opts ниже должен быть заполнен.`,
              en: 'How this proxy wraps/disguises traffic on top of the base TCP/TLS connection. "raw"/"tcp" is plain TCP (also what Mihomo falls back to for an unset value); "ws" is WebSocket; "xhttp" is the modern HTTP-based transport; "grpc" is gRPC; "h2"/"http" is HTTP/2. Must match how the server is configured, and the matching *-opts block below must be filled in.'
            }
          },
          'ws-opts': {
            type: 'object',
            description: {
              ru: `Параметры транспорта WebSocket (network: ws). path/headers должны совпадать с серверными.`,
              en: 'WebSocket transport options (network: ws). Path/headers must match the server.'
            },
            properties: {
              path: { type: 'string' },
              headers: { type: 'object', additionalProperties: { type: 'string' } },
              'max-early-data': {
                type: 'integer',
                description: {
                  ru: `Максимум байт early data, отправляемых до завершения WS-хендшейка.`,
                  en: 'Max bytes of early data sent before the WS handshake completes.'
                }
              },
              'early-data-header-name': {
                type: 'string',
                description: {
                  ru: `HTTP-заголовок, через который передаются early data WebSocket.`,
                  en: 'HTTP header used to carry WebSocket early data.'
                }
              }
            }
          },
          'grpc-opts': {
            type: 'object',
            description: {
              ru: `Параметры транспорта gRPC (network: grpc).`,
              en: 'gRPC transport options (network: grpc).'
            },
            properties: {
              'grpc-service-name': {
                type: 'string',
                description: {
                  ru: `Имя gRPC-сервиса. Должно совпадать с серверным.`,
                  en: 'gRPC service name. Must match the server.'
                }
              },
              'grpc-user-agent': {
                type: 'string',
                description: {
                  ru: `User-Agent, отправляемый для gRPC-транспорта.`,
                  en: 'User-Agent sent for the gRPC transport.'
                }
              },
              'ping-interval': {
                type: 'integer',
                description: {
                  ru: `Интервал ping gRPC в секундах.`,
                  en: 'gRPC ping interval in seconds.'
                }
              },
              'max-connections': {
                type: 'integer',
                description: {
                  ru: `Максимум gRPC-соединений для мультиплексирования.`,
                  en: 'Max gRPC connections for multiplexing.'
                }
              },
              'min-streams': {
                type: 'integer',
                description: {
                  ru: `Минимум потоков на соединение, прежде чем открывать новое.`,
                  en: 'Minimum streams per connection before opening a new one.'
                }
              },
              'max-streams': {
                type: 'integer',
                description: {
                  ru: `Максимум потоков на соединение.`,
                  en: 'Maximum streams per connection.'
                }
              }
            }
          },
          'h2-opts': {
            type: 'object',
            description: {
              ru: `Параметры транспорта HTTP/2 (network: h2).`,
              en: 'HTTP/2 transport options (network: h2).'
            },
            properties: {
              host: { type: 'array', items: { type: 'string' } },
              path: { type: 'string', description: { ru: `HTTP/2 путь.`, en: 'HTTP/2 path.' } }
            }
          },
          'http-opts': {
            type: 'object',
            description: {
              ru: `Параметры обычного HTTP-транспорта, при network: http.`,
              en: 'Plain HTTP transport options, used when network: http.'
            },
            properties: {
              method: {
                type: 'string',
                description: {
                  ru: `HTTP-метод для transport-запроса.`,
                  en: 'HTTP method for the transport request.'
                }
              },
              path: {
                oneOf: [{ type: 'string' }, { type: 'array', items: { type: 'string' } }],
                description: {
                  ru: `HTTP-путь (или список путей) для transport-запроса.`,
                  en: 'HTTP path (or list of paths) for the transport request.'
                }
              },
              headers: {
                type: 'object',
                additionalProperties: { type: 'string' },
                description: {
                  ru: `HTTP-заголовки для transport-запроса.`,
                  en: 'HTTP headers for the transport request.'
                }
              }
            }
          },
          'xhttp-opts': xhttpOptsSchema,
          'ech-opts': echOptsSchema,
          'reality-opts': {
            type: 'object',
            description: {
              ru: `REALITY: соединение использует TLS-сертификат реального, не связанного с вами сайта (не нужен собственный сертификат/домен), так что пассивный DPI видит обычный на вид HTTPS-хендшейк к этому сайту. Требует type: vless и tls: true, и обычно вместе с servername + client-fingerprint — без них REALITY-клиент часто не поднимается. Серверные public-key/short-id здесь должны точно совпадать с настройкой REALITY на сервере, иначе хендшейк просто провалится.`,
              en: "REALITY: connects with the TLS certificate of a real, unrelated website (no cert/domain of your own needed) so passive DPI sees what looks like a normal HTTPS handshake to that site. Requires type: vless and tls: true, and usually servername + client-fingerprint alongside it — without those a REALITY client often fails to come up. Server-side public-key/short-id here must match the server's REALITY setup exactly, or the handshake fails outright."
            },
            properties: {
              'public-key': {
                type: 'string',
                description: {
                  ru: `Публичный ключ REALITY-сервера, сгенерированный серверным инструментом генерации пары ключей.`,
                  en: "The server's REALITY public key, generated by the server's key-pair tool."
                }
              },
              'short-id': {
                type: 'string',
                description: {
                  ru: `Короткий hex-идентификатор (0–16 символов), должен совпадать с одним из shortIds, настроенных на сервере.`,
                  en: 'Short hex identifier (0–16 chars) that must match one of the shortIds configured on the server.'
                }
              },
              'spider-x': {
                type: 'string',
                description: {
                  ru: `REALITY spiderX/путь, из серверного параметра spx — обычно "/".`,
                  en: 'REALITY spiderX/path, from the server\'s spx parameter — commonly "/".'
                }
              },
              'support-x25519mlkem768': {
                type: 'boolean',
                description: {
                  ru: `Включить поддержку пост-квантового гибридного обмена ключами X25519-MLKEM768 для REALITY.`,
                  en: 'Enable hybrid X25519-MLKEM768 post-quantum key exchange support for REALITY.'
                }
              }
            },
            required: ['public-key']
          },
          plugin: {
            type: 'string',
            enum: ['obfs', 'v2ray-plugin', 'shadow-tls', 'restls'],
            description: {
              ru: `Оборачивает трафик Shadowsocks в другой протокол для маскировки — у обычного Shadowsocks детектируемый паттерн трафика, который некоторые DPI блокируют напрямую. shadow-tls/restls делают соединение похожим на настоящий TLS к домену прикрытия, v2ray-plugin добавляет WebSocket/TLS, obfs — более простой легаси-вариант.`,
              en: 'Wraps Shadowsocks traffic in another protocol to disguise it — plain Shadowsocks has a detectable traffic pattern that some DPI blocks outright. shadow-tls/restls make the connection look like real TLS to a cover domain, v2ray-plugin adds WebSocket/TLS, obfs is the simpler legacy option.'
            }
          },
          'plugin-opts': {
            type: 'object',
            description: {
              ru: `Настройки конкретного плагина Shadowsocks — форма зависит от выбранного плагина.`,
              en: 'Shadowsocks plugin-specific options — shape depends on which plugin is selected.'
            },
            properties: {
              mode: {
                type: 'string',
                description: {
                  ru: `Режим работы плагина (зависит от плагина).`,
                  en: 'Plugin operating mode (plugin-specific).'
                }
              },
              host: {
                type: 'string',
                description: {
                  ru: `Домен прикрытия, используемый плагином (например shadow-tls/restls).`,
                  en: 'Cover-domain host used by the plugin (e.g. shadow-tls/restls).'
                }
              },
              tls: {
                type: 'boolean',
                description: {
                  ru: `Включить TLS внутри плагина.`,
                  en: 'Enable TLS within the plugin.'
                }
              },
              'skip-cert-verify': { type: 'boolean' },
              password: {
                type: 'string',
                description: {
                  ru: `Пароль/PSK на уровне плагина, отдельный от пароля SS.`,
                  en: 'Plugin-level password/PSK, distinct from the SS password.'
                }
              },
              version: {
                type: 'string',
                description: {
                  ru: `Версия протокола плагина, если у плагина их несколько.`,
                  en: 'Plugin protocol version, where the plugin has more than one.'
                }
              }
            }
          },
          obfs: {
            type: 'string',
            description: {
              ru: `Режим обфускации Hysteria — дополнительный слой маскировки поверх самого протокола.`,
              en: 'Hysteria obfuscation mode — an extra disguise layer on top of the protocol itself.'
            }
          },
          'obfs-password': {
            type: 'string',
            description: {
              ru: `Пароль/ключ для слоя обфускации Hysteria.`,
              en: 'Password/key for the Hysteria obfuscation layer.'
            }
          },
          'auth-str': {
            type: 'string',
            description: { ru: `Auth-строка Hysteria (v1).`, en: 'Hysteria (v1) auth string.' }
          },
          up: {
            type: 'string',
            description: {
              ru: `Заявленная скорость отдачи в стиле Hysteria/Hysteria2/TUIC (например "100 Mbps") — используется congestion-контроллером для расчёта окна, а не как жёсткий лимит.`,
              en: 'Hysteria/Hysteria2/TUIC-style declared upload bandwidth (e.g. "100 Mbps") — used by the congestion controller to size its window, not a hard cap.'
            }
          },
          down: {
            type: 'string',
            description: {
              ru: `Заявленная скорость загрузки (например "100 Mbps"), то же назначение, что и up.`,
              en: 'Declared download bandwidth (e.g. "100 Mbps"), same purpose as up.'
            }
          },
          'congestion-controller': {
            type: 'string',
            enum: ['cubic', 'new_reno', 'bbr'],
            description: {
              ru: `Алгоритм congestion control для TUIC.`,
              en: 'TUIC congestion control algorithm.'
            }
          },
          'reduce-rtt': {
            type: 'boolean',
            description: {
              ru: `Включить 0-RTT хендшейк TUIC для более быстрых переподключений.`,
              en: 'Enable TUIC 0-RTT handshake for faster reconnects.'
            }
          },
          'dialer-proxy': {
            type: 'string',
            description: {
              ru: `Имя другого прокси или группы в этом файле, через который сначала пойдёт туннель (цепочка прокси) — собственное соединение этого узла устанавливается через указанный, а не напрямую. Не создавайте циклы (A → B → A).`,
              en: "Name of another proxy or group in this file to tunnel through first (proxy chaining) — this proxy's own connection is made through that one instead of directly. Do not create cycles (A → B → A)."
            }
          },
          'interface-name': {
            type: 'string',
            description: {
              ru: `Исходящий сетевой интерфейс для конкретно этого прокси, переопределяет глобальный interface-name. Полезно на роутерах с несколькими WAN/VPN/policy-routing интерфейсами.`,
              en: 'Outbound network interface for this specific proxy, overriding the global interface-name. Useful on routers with multiple WAN/VPN/policy-routing interfaces.'
            }
          },
          'routing-mark': {
            type: 'integer',
            description: {
              ru: `Linux fwmark, проставляемая на собственные исходящие соединения этого прокси, для policy routing / iptables / nftables правил. Полезна, только если уже есть системные правила, которые читают эту метку.`,
              en: "Linux fwmark stamped on this proxy's own outbound connections, for policy routing / iptables / nftables rules. Only useful if matching system rules already read this mark — a mark with nothing consuming it has no effect."
            }
          },
          'ip-version': {
            type: 'string',
            enum: ['dual', 'ipv4', 'ipv6', 'ipv4-prefer', 'ipv6-prefer'],
            description: {
              ru: `Какое семейство IP использовать, когда server — домен. На роутере без стабильного IPv6 ipv4 или ipv4-prefer избавляют от долгих IPv6-таймаутов; ipv6/ipv6-prefer — только там, где IPv6 реально работает end-to-end.`,
              en: 'Which IP family to use when server is a domain. On a router without stable IPv6, ipv4 or ipv4-prefer avoids long IPv6 connection timeouts; use ipv6/ipv6-prefer only where IPv6 genuinely works end to end.'
            }
          },
          ports: {
            type: 'string',
            description: {
              ru: `Диапазон port-hopping для Hysteria2/TUIC (например "20000-30000") — клиент меняет исходящий порт в этом диапазоне, что мешает простой блокировке по одному фиксированному UDP-порту.`,
              en: 'Hysteria2/TUIC port-hopping range (e.g. "20000-30000") — the client rotates source ports across this range, which helps evade simple UDP-flow-based blocking of a single fixed port.'
            }
          },
          smux: {
            type: 'object',
            description: {
              ru: `Мультиплексирует несколько логических потоков поверх одного базового соединения, сокращая число повторных TLS/QUIC-хендшейков для множества коротких запросов — в основном полезно на линках с высокой задержкой или дорогим хендшейком.`,
              en: 'Multiplexes several logical streams over one underlying connection, cutting down on repeated TLS/QUIC handshakes for many short-lived requests — mainly useful on high-latency or handshake-expensive links.'
            }
          },
          // WireGuard, AmneziaWG, OpenVPN, Tailscale
          'private-key': {
            type: 'string',
            description: {
              ru: `Для WireGuard/AmneziaWG: обязательный base64-приватный ключ интерфейса. Для mTLS: содержимое PEM или путь к файлу, используется вместе с certificate.`,
              en: 'For WireGuard/AmneziaWG: the required base64 interface private key. For mTLS: PEM content or a file path, used together with certificate.'
            }
          },
          'public-key': {
            type: 'string',
            description: {
              ru: `Base64 публичный ключ сервера (peer) WireGuard/AmneziaWG.`,
              en: 'Base64 public key of the WireGuard/AmneziaWG server (peer).'
            }
          },
          'pre-shared-key': {
            type: 'string',
            description: {
              ru: `Необязательный base64 pre-shared key peer WireGuard.`,
              en: 'Optional base64 WireGuard peer pre-shared key.'
            }
          },
          ip: {
            type: 'string',
            description: {
              ru: `Клиентский IPv4-адрес WireGuard-туннеля. Маску CIDR можно не указывать — Mihomo подставит /32.`,
              en: 'WireGuard tunnel client IPv4 address. A CIDR mask can be omitted — Mihomo assumes /32.'
            }
          },
          ipv6: {
            type: 'string',
            description: {
              ru: `Клиентский IPv6-адрес WireGuard-туннеля. Маску CIDR можно не указывать — Mihomo подставит /128.`,
              en: 'WireGuard tunnel client IPv6 address. A CIDR mask can be omitted — Mihomo assumes /128.'
            }
          },
          'allowed-ips': {
            type: 'array',
            items: { type: 'string' },
            description: {
              ru: `Сети, маршрутизируемые через peer WireGuard. Для полного туннеля обычно 0.0.0.0/0 и ::/0.`,
              en: 'Networks routed through the WireGuard peer. For a full tunnel, typically 0.0.0.0/0 and ::/0.'
            }
          },
          reserved: {
            type: 'array',
            items: { type: 'integer' },
            description: {
              ru: `Три reserved-байта WireGuard, которые требуют некоторые (например WARP-подобные) серверы. Принимается как массив или как строковое представление.`,
              en: 'Three reserved WireGuard bytes some (e.g. WARP-style) servers require. Accepted as an array or as a string representation.'
            }
          },
          'persistent-keepalive': {
            type: 'integer',
            description: {
              ru: `Интервал keepalive peer WireGuard в секундах; 0 отключает периодические keepalive.`,
              en: 'WireGuard peer keepalive interval in seconds; 0 disables periodic keepalives.'
            }
          },
          workers: {
            type: 'integer',
            description: {
              ru: `Число worker-потоков userspace-реализации WireGuard. Обычно не задаётся.`,
              en: 'Worker-thread count for the WireGuard userspace implementation. Usually left unset.'
            }
          },
          'refresh-server-ip-interval': {
            type: 'integer',
            description: {
              ru: `Интервал в секундах для повторного резолва домена WireGuard-сервера — для серверов за динамическим DNS.`,
              en: "Interval in seconds to re-resolve the WireGuard server's domain, for servers behind dynamic DNS."
            }
          },
          'remote-dns-resolve': {
            type: 'boolean',
            description: {
              ru: `Для OpenVPN/WireGuard: резолвить DNS через удалённый туннель, а не локально.`,
              en: 'For OpenVPN/WireGuard: resolve DNS through the remote tunnel instead of locally.'
            }
          },
          dns: {
            type: 'array',
            items: { type: 'string' },
            description: {
              ru: `DNS-серверы, связанные с этим туннелем прокси.`,
              en: 'DNS servers associated with this proxy tunnel.'
            }
          },
          'ip-stack': {
            type: 'object',
            description: {
              ru: `Выбор реализации IP-стека для WireGuard outbound.`,
              en: 'WireGuard outbound IP-stack implementation selection.'
            },
            properties: {
              mode: {
                type: 'string',
                enum: ['auto', 'gvisor', 'mips'],
                description: {
                  ru: `"auto" выбирает любую доступную реализацию в данной сборке Mihomo; "gvisor" требует сборку с соответствующим тегом.`,
                  en: '"auto" picks whatever implementation the Mihomo build has available; "gvisor" needs a build with that tag.'
                }
              },
              'congestion-controller': {
                type: 'string',
                enum: ['cubic', 'reno', 'bbr', 'bbr3'],
                description: {
                  ru: `TCP congestion control для стека "mips". На gVisor не действует.`,
                  en: 'TCP congestion control for the "mips" stack. Has no effect on gVisor.'
                }
              }
            }
          },
          peers: {
            type: 'array',
            items: wireGuardPeerSchema,
            description: {
              ru: `Полная multi-peer форма WireGuard. При использовании верхнеуровневые server/port/public-key игнорируются в пользу каждой записи peer.`,
              en: 'Full multi-peer WireGuard form. When used, the top-level server/port/public-key are ignored in favor of each peer entry.'
            }
          },
          // OpenVPN
          proto: {
            type: 'string',
            enum: ['udp', 'tcp'],
            description: {
              ru: `Транспортный протокол OpenVPN. По умолчанию udp.`,
              en: 'OpenVPN transport protocol. Defaults to udp.'
            }
          },
          dev: {
            type: 'string',
            enum: ['tun'],
            description: {
              ru: `Тип устройства OpenVPN. Сейчас поддерживается только "tun".`,
              en: 'OpenVPN device type. Currently only "tun" is supported.'
            }
          },
          auth: {
            type: 'string',
            enum: ['SHA256'],
            description: {
              ru: `Auth digest OpenVPN. Сейчас поддерживается только SHA256.`,
              en: 'OpenVPN auth digest. Currently only SHA256 is supported.'
            }
          },
          ca: {
            type: 'string',
            description: {
              ru: `CA-сертификат OpenVPN в PEM, из inline-блока <ca>.`,
              en: 'OpenVPN CA certificate PEM, from the inline <ca> block.'
            }
          },
          cert: {
            type: 'string',
            description: {
              ru: `Клиентский сертификат OpenVPN в PEM. Можно опустить при использовании auth-user-pass.`,
              en: 'OpenVPN client certificate PEM. Can be omitted when using auth-user-pass.'
            }
          },
          key: {
            type: 'string',
            description: {
              ru: `Приватный ключ клиента OpenVPN в PEM, используется вместе с cert.`,
              en: 'OpenVPN client private key PEM, used together with cert.'
            }
          },
          'tls-crypt': {
            type: 'string',
            description: {
              ru: `Статический ключ OpenVPN из inline-блока <tls-crypt>.`,
              en: 'OpenVPN static key from the inline <tls-crypt> block.'
            }
          },
          // Tailscale
          hostname: {
            type: 'string',
            description: {
              ru: `Имя устройства Tailscale для tsnet.`,
              en: 'Tailscale device name for tsnet.'
            }
          },
          'auth-key': {
            type: 'string',
            description: {
              ru: `Auth key Tailscale. Если не задан, Mihomo может вывести интерактивную ссылку для входа.`,
              en: 'Tailscale auth key. If omitted, Mihomo may print an interactive login URL instead.'
            }
          },
          'control-url': {
            type: 'string',
            description: {
              ru: `URL control-сервера Tailscale/Headscale.`,
              en: 'Tailscale/Headscale control server URL.'
            }
          },
          'state-dir': {
            type: 'string',
            description: {
              ru: `Каталог состояния Tailscale/tsnet.`,
              en: 'Tailscale/tsnet state directory.'
            }
          },
          ephemeral: {
            type: 'boolean',
            description: {
              ru: `Войти в tailnet как ephemeral (авто-удаляемый) узел.`,
              en: 'Join the tailnet as an ephemeral (auto-removed) node.'
            }
          },
          'accept-routes': {
            type: 'boolean',
            description: {
              ru: `Принимать subnet-маршруты, анонсируемые tailnet.`,
              en: 'Accept subnet routes advertised by the tailnet.'
            }
          },
          'exit-node': {
            type: 'string',
            description: {
              ru: `IP/имя exit node Tailscale, либо "auto:any".`,
              en: 'Tailscale exit node IP/name, or "auto:any".'
            }
          },
          'exit-node-allow-lan-access': {
            type: 'boolean',
            description: {
              ru: `Разрешить доступ к локальной LAN при маршрутизации через exit node Tailscale.`,
              en: 'Allow access to the local LAN while routing through a Tailscale exit node.'
            }
          },
          'amnezia-wg-option': {
            type: 'object',
            description: {
              ru: `Параметры обфускации пакетов AmneziaWG поверх WireGuard. У обычного WireGuard очень узнаваемый handshake, который DPI может фингерпринтить и блокировать даже не расшифровывая трафик; эти параметры junk-пакетов/magic-заголовков должны точно совпадать с настройкой AmneziaWG на сервере (несовпадение молча приводит к таймауту соединения, а не к понятной ошибке). AWG 3.0/3.1 требуют version: 3 и Mihomo v1.19.30+.`,
              en: "AmneziaWG packet-obfuscation options layered on top of WireGuard. Plain WireGuard has a very recognizable handshake that DPI can fingerprint and block even without decrypting it; these junk-packet/header-magic parameters must match the server's AmneziaWG config exactly (a mismatch fails silently as a connection timeout, not a clear error). AWG 3.0/3.1 need version: 3 and Mihomo v1.19.30+."
            },
            properties: {
              version: {
                type: 'string',
                description: {
                  ru: `Селектор реализации AmneziaWG. Только "3" включает поведение AWG 3.x — AWG 3.1 тоже выбирается значением "3", а не "3.1".`,
                  en: 'AmneziaWG implementation selector. Only "3" enables AWG 3.x behaviour — AWG 3.1 is still selected with "3", not "3.1".'
                }
              },
              jc: {
                type: 'integer',
                description: {
                  ru: `Количество junk-пакетов перед handshake (AWG 1.0+).`,
                  en: 'Junk packet count before the handshake (AWG 1.0+).'
                }
              },
              jmin: {
                type: 'integer',
                description: {
                  ru: `Минимальный размер junk-пакета (AWG 1.0+).`,
                  en: 'Minimum junk packet size (AWG 1.0+).'
                }
              },
              jmax: {
                type: 'integer',
                description: {
                  ru: `Максимальный размер junk-пакета (AWG 1.0+).`,
                  en: 'Maximum junk packet size (AWG 1.0+).'
                }
              },
              s1: {
                type: 'integer',
                description: {
                  ru: `Размер padding первого handshake-пакета.`,
                  en: 'Padding size of the initial handshake packet.'
                }
              },
              s2: {
                type: 'integer',
                description: {
                  ru: `Размер padding ответного handshake-пакета.`,
                  en: 'Padding size of the handshake response packet.'
                }
              },
              s3: {
                type: 'integer',
                description: {
                  ru: `Размер padding cookie-пакета (AWG 1.5+).`,
                  en: 'Padding size of the cookie packet (AWG 1.5+).'
                }
              },
              s4: {
                type: 'integer',
                description: {
                  ru: `Размер padding transport-пакета (AWG 1.5+).`,
                  en: 'Padding size of the transport packet (AWG 1.5+).'
                }
              },
              h1: {
                type: ['integer', 'string'],
                description: {
                  ru: `Magic-заголовок пакета инициации — неотрицательное число или диапазон "min-max".`,
                  en: 'Initiation packet magic header — a non-negative number or a "min-max" range.'
                }
              },
              h2: {
                type: ['integer', 'string'],
                description: {
                  ru: `Magic-заголовок пакета ответа — неотрицательное число или диапазон "min-max".`,
                  en: 'Response packet magic header — a non-negative number or a "min-max" range.'
                }
              },
              h3: {
                type: ['integer', 'string'],
                description: {
                  ru: `Magic-заголовок underload-пакета — неотрицательное число или диапазон "min-max".`,
                  en: 'Underload packet magic header — a non-negative number or a "min-max" range.'
                }
              },
              h4: {
                type: ['integer', 'string'],
                description: {
                  ru: `Magic-заголовок transport-пакета — неотрицательное число или диапазон "min-max".`,
                  en: 'Transport packet magic header — a non-negative number or a "min-max" range.'
                }
              },
              'header-protection-key': {
                type: 'string',
                description: {
                  ru: `Base64-ключ защиты заголовков для AWG 3+. Должен совпадать на клиенте и сервере, требует version: 3 и соответствующих s1–s4.`,
                  en: 'Base64 header-protection key for AWG 3+. Must match on both client and server, and needs version: 3 plus matching s1–s4.'
                }
              },
              i1: {
                type: 'string',
                description: {
                  ru: `Кастомный сигнатурный пакет 1.`,
                  en: 'Custom signature packet 1.'
                }
              },
              i2: {
                type: 'string',
                description: {
                  ru: `Кастомный сигнатурный пакет 2.`,
                  en: 'Custom signature packet 2.'
                }
              },
              i3: {
                type: 'string',
                description: {
                  ru: `Кастомный сигнатурный пакет 3.`,
                  en: 'Custom signature packet 3.'
                }
              },
              i4: {
                type: 'string',
                description: {
                  ru: `Кастомный сигнатурный пакет 4.`,
                  en: 'Custom signature packet 4.'
                }
              },
              i5: {
                type: 'string',
                description: {
                  ru: `Кастомный сигнатурный пакет 5.`,
                  en: 'Custom signature packet 5.'
                }
              },
              j1: {
                type: 'string',
                description: {
                  ru: `Легаси-поле junk-цепочки — только AWG 1.5, убрано в AWG 2+.`,
                  en: 'Legacy junk chain field — AWG 1.5 only, removed in AWG 2+.'
                }
              },
              j2: {
                type: 'string',
                description: {
                  ru: `Легаси-поле junk-цепочки — только AWG 1.5, убрано в AWG 2+.`,
                  en: 'Legacy junk chain field — AWG 1.5 only, removed in AWG 2+.'
                }
              },
              j3: {
                type: 'string',
                description: {
                  ru: `Легаси-поле junk-цепочки — только AWG 1.5, убрано в AWG 2+.`,
                  en: 'Legacy junk chain field — AWG 1.5 only, removed in AWG 2+.'
                }
              },
              itime: {
                type: 'integer',
                description: {
                  ru: `Легаси-поле таймингов — только AWG 1.5.`,
                  en: 'Legacy timing field — AWG 1.5 only.'
                }
              },
              'content-padding-addition': {
                oneOf: [{ type: 'integer' }, { type: 'string' }],
                description: {
                  ru: `Размер дополнительного content padding — неотрицательное число или диапазон "min-max".`,
                  en: 'Content padding addition size — a non-negative number or a "min-max" range.'
                }
              },
              'random-trailers': {
                type: 'boolean',
                description: {
                  ru: `AWG 3.1+: добавлять случайные trailer-данные. Требует version: 3.`,
                  en: 'AWG 3.1+: append random trailer data. Requires version: 3.'
                }
              },
              'disable-cookies': {
                type: 'boolean',
                description: {
                  ru: `AWG 3.1+: отключить механизм cookie WireGuard. Требует version: 3.`,
                  en: 'AWG 3.1+: disable the WireGuard cookie mechanism. Requires version: 3.'
                }
              },
              'rekey-after-time': {
                oneOf: [{ type: 'integer' }, { type: 'string' }],
                description: {
                  ru: `Rekey-after-time — неотрицательное число секунд или диапазон "min-max".`,
                  en: 'Rekey-after-time — a non-negative number of seconds or a "min-max" range.'
                }
              },
              'rekey-timeout': {
                oneOf: [{ type: 'integer' }, { type: 'string' }],
                description: {
                  ru: `Rekey timeout — неотрицательное число или диапазон "min-max".`,
                  en: 'Rekey timeout — a non-negative number or a "min-max" range.'
                }
              },
              'reject-after-time': {
                oneOf: [{ type: 'integer' }, { type: 'string' }],
                description: {
                  ru: `Reject-after-time — неотрицательное число или диапазон "min-max".`,
                  en: 'Reject-after-time — a non-negative number or a "min-max" range.'
                }
              },
              'keepalive-timeout': {
                oneOf: [{ type: 'integer' }, { type: 'string' }],
                description: {
                  ru: `Keepalive timeout — неотрицательное число или диапазон "min-max".`,
                  en: 'Keepalive timeout — a non-negative number or a "min-max" range.'
                }
              },
              'max-handshake-attempts': {
                oneOf: [{ type: 'integer' }, { type: 'string' }],
                description: {
                  ru: `Максимум попыток handshake — неотрицательное число или диапазон "min-max".`,
                  en: 'Max handshake attempts — a non-negative number or a "min-max" range.'
                }
              }
            }
          }
        },
        required: ['name', 'type'],
        allOf: [
          {
            if: {
              properties: { type: { not: { enum: ['tailscale', 'direct'] } } },
              required: ['type']
            },
            then: { required: ['server', 'port'] }
          },
          {
            if: { properties: { type: { const: 'openvpn' } }, required: ['type'] },
            then: { required: ['server', 'port', 'ca', 'tls-crypt'] }
          },
          {
            if: { properties: { type: { enum: ['vmess', 'vless'] } }, required: ['type'] },
            then: { required: ['uuid'] }
          },
          {
            if: {
              properties: { type: { enum: ['ss', 'ssr', 'trojan', 'snell', 'hysteria2'] } },
              required: ['type']
            },
            then: { required: ['password'] }
          },
          {
            if: { properties: { type: { const: 'tuic' } }, required: ['type'] },
            then: { required: ['uuid', 'password'] }
          },
          {
            if: { properties: { type: { const: 'wireguard' } }, required: ['type'] },
            then: { required: ['private-key', 'ip'] }
          }
        ]
      }
    },
    'proxy-groups': {
      type: 'array',
      description: { ru: `Определения групп прокси`, en: 'Proxy group definitions' },
      items: {
        type: 'object',
        properties: {
          name: {
            type: 'string',
            description: {
              ru: `Имя группы. Используется в rules и в proxies других групп.`,
              en: "Group name. Referenced from rules and from other groups' proxies lists."
            }
          },
          type: {
            type: 'string',
            enum: ['select', 'url-test', 'fallback', 'load-balance', 'relay'],
            description: {
              ru: `Тип группы. "select" — ручной выбор через API/UI; "url-test" — автовыбор по минимальной задержке; "fallback" — первый рабочий узел по порядку; "load-balance" — распределение по стратегии; "relay" — цепочка (трафик идёт последовательно через все прокси списка).`,
              en: '"select": manual choice via API/UI. "url-test": auto-pick the lowest-latency node. "fallback": first working node in list order. "load-balance": spread traffic per strategy. "relay": a chain — traffic passes through every proxy in the list in sequence.'
            }
          },
          proxies: {
            type: 'array',
            items: { type: 'string' },
            description: {
              ru: `Имена прокси/групп. Также допустимы DIRECT, REJECT, PASS.`,
              en: 'Proxy/group names. DIRECT, REJECT and PASS are also accepted here.'
            }
          },
          use: {
            type: 'array',
            items: { type: 'string' },
            description: {
              ru: `Имена proxy-providers, из которых берутся серверы — все узлы провайдера автоматически добавляются в группу.`,
              en: 'Names of proxy-providers to pull servers from — every node from that provider is automatically added to the group.'
            }
          },
          url: {
            type: 'string',
            description: {
              ru: `URL для проверки задержки/доступности (url-test, fallback, load-balance). Проверяет только узлы из proxies — узлы, подключённые через use, проверяются собственным health-check провайдера.`,
              en: "URL for latency/availability checks (url-test, fallback, load-balance). Only checks nodes listed in proxies — nodes pulled in via use are checked by the provider's own health-check instead."
            }
          },
          interval: {
            type: 'integer',
            minimum: 1,
            description: {
              ru: `Интервал проверки в секундах.`,
              en: 'Check interval in seconds.'
            }
          },
          timeout: {
            type: 'integer',
            description: {
              ru: `Таймаут одной проверки в миллисекундах. Слишком маленькое значение может отбраковывать рабочие, но дальние узлы.`,
              en: 'Timeout for one check, in milliseconds. Too small a value can wrongly mark working-but-distant nodes as dead.'
            }
          },
          tolerance: {
            type: 'integer',
            minimum: 0,
            description: {
              ru: `Допустимое отклонение задержки в мс (для url-test). Если текущий прокси быстрее ±tolerance — группа не переключается.`,
              en: 'Latency tolerance in ms (url-test). If the current proxy stays within ±tolerance, the group does not switch.'
            }
          },
          lazy: {
            type: 'boolean',
            description: {
              ru: `Ленивый режим проверки. По умолчанию true: пока группа не выбрана как активная, плановые проверки для неё не выполняются.`,
              en: 'Lazy checking. Defaults to true: while the group is not the active one, its scheduled checks do not run, saving router resources.'
            }
          },
          'max-failed-times': {
            type: 'integer',
            description: {
              ru: `Максимум подряд неудачных проверок, после которого принудительно запускается health-check (по умолчанию 5).`,
              en: 'Consecutive failed checks allowed before a forced health-check kicks in (defaults to 5).'
            }
          },
          'expected-status': {
            type: 'string',
            description: {
              ru: `Ожидаемый HTTP-статус ответа проверки. Поддерживаются число, список через "/" и диапазон через "-" (например "200/204" или "200-299").`,
              en: 'Expected HTTP status of the check response. Supports a single code, a "/"-separated list, or a "-" range (e.g. "200/204" or "200-299").'
            }
          },
          'exclude-filter': {
            type: 'string',
            description: {
              ru: `Regex-исключение по имени узла.`,
              en: 'Regex exclusion filter applied to node names.'
            }
          },
          'exclude-type': {
            type: 'string',
            description: {
              ru: `Исключить узлы по типу протокола (не regex, типы через "|", например "ss|http").`,
              en: 'Exclude nodes by protocol type — not a regex, types separated by "|" (e.g. "ss|http").'
            }
          },
          filter: {
            type: 'string',
            description: {
              ru: `Regex-фильтр по имени: оставить в группе только подходящие узлы (применяется к use и include-all наборам).`,
              en: 'Regex allow-filter on node names — keeps only matching nodes (applies to use and include-all sets).'
            }
          },
          'include-all': {
            type: 'boolean',
            description: {
              ru: `Включить все верхнеуровневые proxies и все proxy-providers (proxy-groups автоматически не включаются).`,
              en: 'Include all top-level proxies and all proxy-providers (other proxy-groups are not auto-included).'
            }
          },
          'include-all-proxies': {
            type: 'boolean',
            description: {
              ru: `Включить все одиночные узлы из proxies, без proxy-providers.`,
              en: 'Include every standalone node from proxies, without pulling in proxy-providers.'
            }
          },
          'include-all-providers': {
            type: 'boolean',
            description: {
              ru: `Включить все proxy-providers автоматически (делает ручное перечисление через use для них излишним).`,
              en: 'Auto-include every proxy-provider (makes listing them via use redundant).'
            }
          },
          'disable-udp': {
            type: 'boolean',
            description: {
              ru: `Отключить UDP через эту группу, даже если отдельные узлы его поддерживают.`,
              en: 'Disable UDP through this group even where individual nodes support it.'
            }
          },
          'interface-name': {
            type: 'string',
            description: {
              ru: `Переопределить исходящий интерфейс для группы. Считается устаревшим в документации — предпочтительнее задавать interface-name на конкретном узле.`,
              en: 'Override the outbound interface for the group. Documented as deprecated in favor of setting interface-name on individual proxy nodes.'
            }
          },
          'routing-mark': {
            type: 'integer',
            description: {
              ru: `Переопределить fwmark для группы. Считается устаревшим — предпочтительнее задавать routing-mark на конкретном узле.`,
              en: 'Override fwmark for the group. Documented as deprecated in favor of setting routing-mark on individual proxy nodes.'
            }
          },
          strategy: {
            type: 'string',
            enum: ['consistent-hashing', 'round-robin', 'sticky-sessions'],
            description: {
              ru: `Стратегия балансировки (только для load-balance). "consistent-hashing" — по хешу домена назначения (один домен → один прокси); "round-robin" — по кругу; "sticky-sessions" — по хешу src-IP+dst.`,
              en: 'Load-balance strategy (load-balance only). "consistent-hashing": hashed by destination domain (one domain → one proxy). "round-robin": rotates through nodes. "sticky-sessions": hashed by src-IP + dst.'
            }
          },
          icon: {
            type: 'string',
            description: {
              ru: `URL иконки группы для отображения во внешнем дашборде.`,
              en: 'URL of an icon for this group, shown in an external dashboard UI.'
            }
          },
          hidden: {
            type: 'boolean',
            description: {
              ru: `Скрыть группу из внешнего UI.`,
              en: 'Hide the group from an external dashboard UI.'
            }
          }
        },
        required: ['name', 'type']
      }
    },
    listeners: {
      type: 'array',
      description: {
        ru: `Дополнительные входящие серверы, которые открывает сама Mihomo (работая как socks/vmess/vless/... сервер) — отдельно и в дополнение к port/socks-port/mixed-port. Противоположное proxies по направлению (это апстрим-серверы, к которым Mihomo подключается наружу) — используйте, только если другие устройства/клиенты должны подключаться к этому роутеру как к прокси-серверу по конкретному протоколу.`,
        en: 'Extra inbound servers Mihomo itself exposes (running it as a socks/vmess/vless/... server), separate from and in addition to port/socks-port/mixed-port. Opposite direction from proxies (which are upstream servers Mihomo connects out to) — use this only if other devices/clients should connect to this router as a proxy server over a specific protocol.'
      },
      items: {
        type: 'object',
        properties: {
          name: {
            type: 'string',
            description: {
              ru: `Имя листенера (матчится через IN-NAME)`,
              en: 'Listener name (matchable with IN-NAME)'
            }
          },
          type: {
            type: 'string',
            enum: [
              'socks',
              'http',
              'tproxy',
              'redir',
              'mixed',
              'tunnel',
              'tun',
              'shadowsocks',
              'snell',
              'vmess',
              'vless',
              'trojan',
              'hysteria2',
              'hysteria2-realm',
              'tuic',
              'shadowquic',
              'anytls',
              'mieru',
              'sudoku',
              'trusttunnel'
            ],
            description: {
              ru: `Протокол входящего листенера`,
              en: 'Inbound listener protocol type'
            }
          },
          listen: {
            type: 'string',
            description: {
              ru: `Адрес привязки (по умолчанию 0.0.0.0)`,
              en: 'Binding IP address (defaults to 0.0.0.0)'
            }
          },
          port: {
            oneOf: [
              { type: 'integer', minimum: 1, maximum: 65535 },
              {
                type: 'string',
                description: {
                  ru: `Диапазон портов, например "20000-20100"`,
                  en: 'Port range, e.g. "20000-20100"'
                }
              }
            ],
            description: {
              ru: `Слушающий порт или диапазон портов`,
              en: 'Listening port or port range'
            }
          },
          proxy: {
            type: 'string',
            description: {
              ru: `Имя прокси или proxy-group, в который этот листенер жёстко отправляет трафик напрямую, минуя выбор через rules. Не задавайте одновременно с rule — это два разных способа выбрать маршрут.`,
              en: 'Name of a proxy or proxy-group this listener sends traffic to directly, bypassing normal rule matching. Do not set alongside rule — pick one routing method.'
            }
          },
          rule: {
            type: 'string',
            description: {
              ru: `Прогонять трафик этого листенера через обычные rules Mihomo (например "MATCH,PROXY"), вместо жёсткой привязки к одному proxy. Не задавайте одновременно с proxy.`,
              en: 'Route this listener\'s traffic through Mihomo\'s normal rules processing (e.g. "MATCH,PROXY"), instead of pinning it to one proxy. Do not set alongside proxy.'
            }
          },
          'routing-mark': {
            type: 'integer',
            minimum: 0,
            description: {
              ru: `Значение SO_MARK для Linux-сокета`,
              en: 'Linux socket SO_MARK value'
            }
          },
          udp: { type: 'boolean', description: { ru: `Разрешить UDP`, en: 'Enable UDP support' } },
          users: {
            type: 'array',
            description: {
              ru: `Учётные данные аутентификации листенера`,
              en: 'Inbound authentication credentials'
            },
            items: {
              type: 'object',
              properties: {
                username: {
                  type: 'string',
                  description: { ru: `Имя пользователя`, en: 'Username' }
                },
                password: { type: 'string', description: { ru: `Пароль`, en: 'Password' } }
              }
            }
          },
          cipher: {
            type: 'string',
            description: { ru: `Шифр Shadowsocks`, en: 'Shadowsocks cipher' }
          },
          password: {
            type: 'string',
            description: { ru: `Пароль Shadowsocks`, en: 'Shadowsocks password' }
          }
        },
        required: ['name', 'type']
      }
    },
    'rule-providers': {
      type: 'object',
      description: {
        ru: `Переиспользуемые, самообновляющиеся наборы правил (например список доменов для конкретного сервиса или региона), подключаемые в rules через RULE-SET,<имя>,<политика> вместо вставки сотен отдельных строк правил — держит rules компактным и позволяет набору обновляться самому без правки этого конфига.`,
        en: 'Reusable, auto-updating rule sets (e.g. a domain list for a specific service or region) referenced from rules via RULE-SET,<name>,<policy> instead of pasting hundreds of individual rule lines — keeps the rules list short and lets a set update itself without editing this config.'
      },
      additionalProperties: {
        type: 'object',
        properties: {
          type: {
            type: 'string',
            enum: ['http', 'file', 'inline'],
            description: {
              ru: `Тип источника набора правил. "http" — скачать по URL, "file" — читать локальный файл, "inline" — задать payload прямо в этом конфиге.`,
              en: '"http" downloads by URL, "file" reads a local file, "inline" defines payload directly in this config.'
            }
          },
          behavior: {
            type: 'string',
            enum: ['domain', 'ipcidr', 'classical'],
            description: {
              ru: `Тип правил в наборе. "domain" — только домены/суффиксы, "ipcidr" — только IP/CIDR, "classical" — смешанный формат (DOMAIN-SUFFIX, IP-CIDR и т.д., как в основных rules). Несовпадение с реальным содержимым файла — тихая ошибка: набор загрузится, но будет матчиться неправильно.`,
              en: '"domain": domains/suffixes only. "ipcidr": IP/CIDR only. "classical": mixed format (DOMAIN-SUFFIX, IP-CIDR, etc., like the main rules array). A mismatch with the actual file content fails silently — the set loads but matches incorrectly.'
            }
          },
          url: {
            type: 'string',
            description: { ru: `URL набора правил (type: http)`, en: 'Rule-set URL (type: http)' }
          },
          path: {
            type: 'string',
            description: {
              ru: `Локальный путь. Для type: file — источник, для type: http — файл кэша. Можно не указывать — Mihomo сгенерирует путь сама.`,
              en: 'Local path — the source file for type: file, or the cache file for type: http. Optional; Mihomo can generate one automatically.'
            }
          },
          format: {
            type: 'string',
            enum: ['yaml', 'text', 'mrs'],
            description: {
              ru: `Формат файла. "yaml" — YAML-список с payload:, "text" — простой текстовый список, "mrs" — бинарный формат Mihomo для больших наборов (поддерживает только behavior: domain и behavior: ipcidr, не classical).`,
              en: '"yaml": a YAML list under payload:. "text": a plain text list. "mrs": Mihomo\'s compact binary format for large sets — supports behavior: domain and behavior: ipcidr only, not classical.'
            }
          },
          interval: {
            type: 'integer',
            description: {
              ru: `Интервал автообновления в секундах (для type: http) — как часто перекачивается сам набор правил.`,
              en: 'Auto-update interval in seconds (type: http) — how often the rule set itself is re-downloaded.'
            }
          },
          proxy: {
            type: 'string',
            description: {
              ru: `Прокси или группа для загрузки rule-set, если источник недоступен напрямую — влияет только на скачивание, не на то, куда отправляется совпавший трафик (это задаётся в самой строке RULE-SET,... в rules).`,
              en: 'Proxy/group used to fetch this rule-set when the source is not directly reachable — affects only the download, not where matched traffic is sent (that is set in the RULE-SET,... rule line itself).'
            }
          },
          'size-limit': {
            type: 'integer',
            description: {
              ru: `Максимальный размер скачиваемого файла в байтах. 0 — без ограничения.`,
              en: 'Max size of the downloaded file, in bytes. 0 means unlimited.'
            }
          },
          header: {
            type: 'object',
            additionalProperties: { type: 'array', items: { type: 'string' } },
            description: {
              ru: `Дополнительные HTTP-заголовки при скачивании набора правил: User-Agent, Authorization и прочие требования источника.`,
              en: 'Extra HTTP headers sent when fetching the rule set: User-Agent, Authorization, or whatever the source requires.'
            }
          },
          payload: {
            type: 'array',
            items: { type: 'string' },
            description: {
              ru: `Inline-список правил (для type: inline) — короткие собственные наборы удобно держать прямо здесь вместо отдельного файла.`,
              en: 'Inline rule list (type: inline) — convenient for short custom sets instead of a separate provider file.'
            }
          }
        },
        required: ['type', 'behavior']
      }
    },
    'sub-rules': {
      type: 'object',
      description: {
        ru: `Именованные подмножества правил, на которые можно ссылаться из listeners[].rule или rules через SUB-RULE, для областей маршрутизации по подмножествам.`,
        en: 'Named rule subsets matchable from listeners[].rule or rules via SUB-RULE, for split routing scopes'
      },
      additionalProperties: {
        type: 'array',
        items: { type: 'string' }
      }
    },
    rules: {
      type: 'array',
      description: {
        ru: `Правила маршрутизации трафика, проверяются сверху вниз — побеждает первое совпавшее правило, все последующие уже не проверяются, поэтому более специфичные правила (конкретный домен) должны стоять раньше более широких (весь набор GEOSITE/GEOIP), которые иначе их перекроют. Обычно заканчивается правилом-заглушкой MATCH,<политика>.`,
        en: 'Traffic routing rules, evaluated top to bottom — the first matching rule wins and later rules are never checked, so more specific rules (a single domain) must come before broader ones (a whole GEOSITE/GEOIP set) that would otherwise shadow them. Usually ends with a catch-all MATCH,<policy> rule.'
      },
      items: {
        type: 'string',
        pattern:
          '^(DOMAIN|DOMAIN-SUFFIX|DOMAIN-KEYWORD|DOMAIN-REGEX|DOMAIN-WILDCARD|GEOSITE|GEOIP|SRC-GEOIP|IP-ASN|SRC-IP-ASN|IP-CIDR|IP-CIDR6|SRC-IP-CIDR|IP-SUFFIX|SRC-IP-SUFFIX|SRC-PORT|DST-PORT|IN-PORT|IN-TYPE|IN-USER|IN-NAME|PROCESS-NAME|PROCESS-PATH|PROCESS-NAME-REGEX|PROCESS-PATH-REGEX|NETWORK|UID|SUB-RULE|RULE-SET|AND|OR|NOT|MATCH),.+$',
        description: {
          ru: `Правило в формате: TYPE,ARG[,ARG2],POLICY[,no-resolve] или MATCH,POLICY`,
          en: 'Rule in format: TYPE,ARG[,ARG2],POLICY[,no-resolve] or MATCH,POLICY'
        }
      }
    },
    script: {
      type: 'object',
      description: { ru: `Конфигурация на основе скриптов`, en: 'Script-based configuration' }
    }
  }
};
