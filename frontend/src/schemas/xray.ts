export const shadowsocksCiphers = [
  '2022-blake3-aes-128-gcm',
  '2022-blake3-aes-256-gcm',
  '2022-blake3-chacha20-poly1305',
  'aes-256-gcm',
  'aes-128-gcm',
  'chacha20-poly1305',
  'xchacha20-poly1305',
  'none'
] as const;

const sockoptSchema = {
  type: 'object',
  description: {
    ru: 'Параметры сокетов для тюнинга сетевых соединений и цепочек прокси.',
    en: 'Socket options for connection tuning and proxy chaining.'
  },
  properties: {
    mark: {
      type: 'integer',
      description: {
        ru: 'Метка `SO_MARK`, выставляемая на собственных сокетах этого outbound, чтобы правила iptables/ip-rule исключали собственный исходящий трафик Xray из прозрачного перехвата.\n\n> Без этой метки собственные соединения Xray могут быть повторно перехвачены и направлены внутрь себя (петля маршрутизации).',
        en: "SO_MARK stamped on this outbound's own sockets so iptables/ip-rule setup can recognize and exclude Xray's own upstream traffic from transparent-proxy interception.\n\n> Without it, Xray's own connections can get redirected back into itself (a routing loop)."
      }
    },
    tcpFastOpen: {
      oneOf: [{ type: 'boolean' }, { type: 'integer' }],
      description: {
        ru: 'TCP Fast Open (TFO) — ускоряет установление соединения за счёт отправки данных уже в пакете SYN.',
        en: 'TCP Fast Open (TFO) — speeds up connection handshake by sending payload in the SYN packet.'
      }
    },
    tcpMptcp: {
      type: 'boolean',
      description: {
        ru: 'Multipath TCP (MPTCP) — одновременная передача данных по нескольким сетевым интерфейсам (требует поддержки в ядре роутера).',
        en: 'Multipath TCP (MPTCP) — enables multiple network paths concurrently (requires router Linux kernel support).'
      }
    },
    tcpNoDelay: {
      type: 'boolean',
      description: {
        ru: 'Отключение алгоритма Нейгла (`TCP_NODELAY`) для минимизации задержки ценой незначительного роста количества пакетов.',
        en: 'Disable Nagle algorithm (`TCP_NODELAY`) to reduce latency at the cost of slight packet overhead.'
      }
    },
    tcpKeepAliveInterval: {
      type: 'integer',
      description: {
        ru: 'Интервал отправки контрольных пакетов TCP keepalive в секундах.',
        en: 'TCP keepalive probe interval in seconds.'
      }
    },
    dialerProxy: {
      type: 'string',
      description: {
        ru: 'Тег другого outbound для построения цепочки прокси (трафик направляется через указанный промежуточный узел).',
        en: 'Outbound tag for chained proxying (traffic is forwarded through the specified proxy).'
      }
    }
  }
} as const;

const tlsSettingsSchema = {
  type: 'object',
  description: {
    ru: 'Параметры безопасности TLS-транспорта (`security: tls`).',
    en: 'TLS transport security settings (`security: tls`).'
  },
  properties: {
    serverName: {
      type: 'string',
      description: {
        ru: 'Значение SNI (Server Name Indication), передаваемое в рукопожатии TLS.',
        en: 'SNI (Server Name Indication) sent during the TLS handshake.'
      }
    },
    alpn: {
      type: 'array',
      items: { type: 'string' },
      description: {
        ru: 'Список протоколов TLS ALPN в порядке предпочтения (например, `["h2", "http/1.1"]`).',
        en: 'TLS ALPN protocol preference list (e.g. `["h2", "http/1.1"]`).'
      }
    },
    minVersion: {
      type: 'string',
      enum: ['1.0', '1.1', '1.2', '1.3'],
      description: {
        ru: 'Минимально допустимая версия протокола TLS.',
        en: 'Minimum allowed TLS version.'
      }
    },
    maxVersion: {
      type: 'string',
      enum: ['1.0', '1.1', '1.2', '1.3'],
      description: {
        ru: 'Максимально допустимая версия протокола TLS.',
        en: 'Maximum allowed TLS version.'
      }
    },
    fingerprint: {
      type: 'string',
      enum: ['chrome', 'firefox', 'safari', 'ios', 'android', 'edge', '360', 'qq', 'random'],
      description: {
        ru: 'Имитация TLS-отпечатка клиента (uTLS fingerprint) для маскировки под стандартные браузеры и обхода DPI.',
        en: 'Client TLS hello fingerprint simulation (uTLS fingerprint) for DPI circumvention.'
      }
    },
    allowInsecure: {
      type: 'boolean',
      description: {
        ru: 'Принимать сертификат сервера без проверки цепочки доверия.\n\n> Включение открывает уязвимость для атаки Man-in-the-Middle (MITM). Используйте только для самоподписанных сертификатов на собственных доверенных серверах.',
        en: 'Accept the server TLS certificate without validation.\n\n> Enabling this defeats TLS security by allowing trivial man-in-the-middle attacks. Only use for self-signed certs on servers you control.'
      }
    },
    certificates: {
      type: 'array',
      description: {
        ru: 'Список сертификатов и ключей для входящих или исходящих соединений.',
        en: 'List of certificates and keys for inbound or outbound connections.'
      },
      items: {
        type: 'object',
        properties: {
          certificateFile: {
            type: 'string',
            description: {
              ru: 'Путь к файлу сертификата (CRT/PEM).',
              en: 'Path to certificate file (CRT/PEM).'
            }
          },
          keyFile: {
            type: 'string',
            description: {
              ru: 'Путь к файлу приватного ключа (KEY/PEM).',
              en: 'Path to private key file (KEY/PEM).'
            }
          }
        }
      }
    }
  }
} as const;

const realitySettingsSchema = {
  type: 'object',
  description: {
    ru: 'REALITY: подключение с использованием TLS-сертификата реального стороннего сайта без необходимости регистрировать домен или выпускать свой сертификат. Пассивный DPI видит обычное HTTPS-рукопожатие к выбранному ресурсу.\n\n> На клиенте `publicKey` и `shortId` обязаны в точности совпадать с параметрами сервера REALITY, иначе соединение завершится ошибкой.',
    en: "REALITY: connects using the TLS certificate of a real, unrelated website (no domain or certificate of your own needed) so passive DPI sees a normal HTTPS handshake to that site.\n\n> Client-side `publicKey` and `shortId` must match the server's REALITY config exactly, or the handshake fails."
  },
  properties: {
    show: {
      type: 'boolean',
      description: {
        ru: 'Выводить отладочную информацию REALITY в журнал (на стороне сервера).',
        en: 'Print REALITY debug info to log (server-side).'
      }
    },
    dest: {
      type: 'string',
      description: {
        ru: 'Адрес и порт маскировочного ресурса (на стороне сервера, например `example.com:443`).',
        en: 'Camouflage target address:port (server-side, e.g. `example.com:443`).'
      }
    },
    xver: {
      type: 'integer',
      description: {
        ru: 'Версия протокола PROXY при пересылке запроса на dest (на стороне сервера: 0, 1 или 2).',
        en: 'PROXY protocol version toward dest (server-side: 0, 1, or 2).'
      }
    },
    serverNames: {
      type: 'array',
      items: { type: 'string' },
      description: {
        ru: 'Список разрешённых значений SNI (на стороне сервера).',
        en: 'Allowed SNI values list (server-side).'
      }
    },
    privateKey: {
      type: 'string',
      description: {
        ru: 'Приватный ключ REALITY (на стороне сервера).',
        en: 'REALITY private key (server-side).'
      }
    },
    shortIds: {
      type: 'array',
      items: { type: 'string' },
      description: {
        ru: 'Список разрешённых идентификаторов short ID (на стороне сервера).',
        en: 'Allowed short IDs list (server-side).'
      }
    },
    publicKey: {
      type: 'string',
      description: {
        ru: 'Публичный ключ REALITY (на стороне клиента).',
        en: 'REALITY public key (client-side).'
      }
    },
    shortId: {
      type: 'string',
      description: {
        ru: 'Короткий идентификатор REALITY (на стороне клиента).',
        en: 'REALITY short ID (client-side).'
      }
    },
    spiderX: {
      type: 'string',
      description: {
        ru: 'Путь краулера spiderX для сканирования маскировочного сайта (на стороне клиента, например `/`).',
        en: 'Initial crawler spiderX path for probing the camouflage site (client-side, e.g. `/`).'
      }
    },
    fingerprint: {
      type: 'string',
      enum: ['chrome', 'firefox', 'safari', 'ios', 'android', 'edge', '360', 'qq', 'random'],
      description: {
        ru: 'TLS-отпечаток uTLS клиента для REALITY (рекомендуется `chrome` или `firefox`).',
        en: 'uTLS client hello fingerprint for REALITY (recommended `chrome` or `firefox`).'
      }
    }
  }
} as const;

const wsSettingsSchema = {
  type: 'object',
  description: {
    ru: 'Параметры транспорта WebSocket (`network: ws`).',
    en: 'WebSocket transport options (`network: ws`).'
  },
  properties: {
    path: {
      type: 'string',
      description: {
        ru: 'HTTP-путь для WebSocket-соединения (например, `/ws`).',
        en: 'HTTP path for WebSocket connection (e.g. `/ws`).'
      }
    },
    headers: {
      type: 'object',
      additionalProperties: { type: 'string' },
      description: {
        ru: 'Пользовательские HTTP-заголовки (например, заголовок `Host`).',
        en: 'Custom HTTP headers (e.g. `Host` header).'
      }
    }
  }
} as const;

const grpcSettingsSchema = {
  type: 'object',
  description: {
    ru: 'Параметры транспорта gRPC (`network: grpc`).',
    en: 'gRPC transport options (`network: grpc`).'
  },
  properties: {
    serviceName: {
      type: 'string',
      description: {
        ru: 'Имя gRPC-сервиса (Service Name). Обязано совпадать с настройкой на сервере.',
        en: 'gRPC service name. Must match the server configuration.'
      }
    },
    multiMode: {
      type: 'boolean',
      description: {
        ru: 'Включение многопоточного режима (MultiMode) для объединения нескольких соединений в один поток gRPC.',
        en: 'Enable multiMode to multiplex multiple connections into a single gRPC stream.'
      }
    }
  }
} as const;

const httpupgradeSettingsSchema = {
  type: 'object',
  description: {
    ru: 'Параметры транспорта HTTP-Upgrade (`network: httpupgrade`).',
    en: 'HTTP-Upgrade transport options (`network: httpupgrade`).'
  },
  properties: {
    path: {
      type: 'string',
      description: {
        ru: 'HTTP-путь для апгрейда соединения (например, `/upgrade`).',
        en: 'HTTP path for connection upgrade (e.g. `/upgrade`).'
      }
    },
    host: {
      type: 'string',
      description: {
        ru: 'Значение HTTP-заголовка Host при апгрейде соединения.',
        en: 'Host header value during HTTP upgrade.'
      }
    }
  }
} as const;

const streamSettingsSchema = {
  type: 'object',
  description: {
    ru: 'Настройки транспортного уровня (протокол передачи, TLS/Reality шифрование и параметры сокетов).',
    en: 'Transport settings (wire protocol, TLS/Reality security, and socket-specific options).'
  },
  properties: {
    network: {
      type: 'string',
      enum: ['tcp', 'kcp', 'ws', 'http', 'domainsocket', 'quic', 'grpc', 'httpupgrade', 'xhttp'],
      description: {
        ru: 'Транспортный протокол для передачи данных:\n- **tcp** — прямой TCP-поток (наименьшие накладные расходы).\n- **ws** / **grpc** / **httpupgrade** / **xhttp** — инкапсуляция в HTTP(S)-подобный протокол для маскировки под веб-трафик или прохождения через CDN.\n\n> Значение обязано в точности совпадать с настройкой на стороне сервера.',
        en: 'Wire protocol for data delivery:\n- **tcp** — raw TCP stream (lowest overhead).\n- **ws** / **grpc** / **httpupgrade** / **xhttp** — encapsulation in HTTP(S)-like layers to disguise traffic or pass through CDNs.\n\n> Must match the server configuration exactly.'
      }
    },
    security: {
      type: 'string',
      enum: ['none', 'tls', 'reality'],
      description: {
        ru: 'Уровень шифрования транспорта:\n- **none** — без дополнительного шифрования (подходит, если сам протокол уже зашифрован, например Shadowsocks).\n- **tls** — стандартный TLS с использованием собственного домена и сертификата.\n- **reality** — маскировка под TLS-сертификат стороннего доверенного сайта без своего домена.',
        en: "Transport security layer:\n- **none** — no encryption at this layer (suitable when inner protocol is already encrypted, e.g. Shadowsocks).\n- **tls** — standard TLS to your own domain/certificate.\n- **reality** — borrows a real site's certificate identity without needing your own domain."
      }
    },
    tlsSettings: tlsSettingsSchema,
    realitySettings: realitySettingsSchema,
    wsSettings: wsSettingsSchema,
    grpcSettings: grpcSettingsSchema,
    httpupgradeSettings: httpupgradeSettingsSchema,
    sockopt: sockoptSchema
  }
} as const;

const vmessVlessUserSchema = {
  type: 'object',
  properties: {
    id: {
      type: 'string',
      pattern: '^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$',
      description: {
        ru: 'UUID идентификатор пользователя (36-символьный формат UUID v4).',
        en: 'Client UUID (36-character UUID v4 format).'
      }
    },
    alterId: {
      type: 'integer',
      description: {
        ru: 'Устаревший параметр AlterID для VMess. В современных версиях Xray используйте 0 (AEAD-режим).',
        en: 'Legacy VMess AlterID. In modern Xray versions, use 0 (AEAD mode).'
      }
    },
    security: {
      type: 'string',
      enum: ['auto', 'aes-128-gcm', 'chacha20-poly1305', 'none', 'zero'],
      description: {
        ru: 'Метод шифрования полезной нагрузки VMess (рекомендуется "auto" или "aes-128-gcm").',
        en: 'VMess payload encryption method (recommended "auto" or "aes-128-gcm").'
      }
    },
    encryption: {
      type: 'string',
      description: {
        ru: 'Шифрование протокола VLESS (на стороне сервера обычно "none").',
        en: 'VLESS protocol encryption (typically "none" on server side).'
      }
    },
    flow: {
      type: 'string',
      enum: ['xtls-rprx-vision', ''],
      description: {
        ru: 'Управление потоком VLESS. Значение "xtls-rprx-vision" обеспечивает прямое чтение TLS без повторного шифрования и скрывает внутренние паттерны трафика.',
        en: 'VLESS flow control. "xtls-rprx-vision" provides zero-copy inner TLS and obfuscates handshake patterns.'
      }
    },
    level: {
      type: 'integer',
      description: {
        ru: 'Уровень пользователя для применения политик в policy.',
        en: 'User level for matching rules in policy.'
      }
    },
    email: {
      type: 'string',
      description: {
        ru: 'Идентификатор или email пользователя для логирования и статистики.',
        en: 'User email or identifier for logging and statistics.'
      }
    }
  },
  required: ['id']
} as const;

const trojanClientSchema = {
  type: 'object',
  description: {
    ru: 'Авторизованный клиент для входящего прокси Trojan.',
    en: 'Authorized client for Trojan inbound proxy.'
  },
  properties: {
    password: {
      type: 'string',
      description: {
        ru: 'Пароль доступа клиента.',
        en: 'Client access password.'
      }
    },
    email: {
      type: 'string',
      description: {
        ru: 'Идентификатор или email клиента.',
        en: 'Client identifier or email.'
      }
    },
    level: {
      type: 'integer',
      description: {
        ru: 'Уровень пользователя для применения политик policy.',
        en: 'User level for policy enforcement.'
      }
    }
  },
  required: ['password']
} as const;

const inboundClientsSchema = {
  type: 'array',
  description: {
    ru: 'Список авторизованных клиентов (по UUID для VMess/VLESS или по паролю для Trojan).',
    en: 'Authorized client list (UUID-based for VMess/VLESS, password-based for Trojan).'
  },
  items: { oneOf: [vmessVlessUserSchema, trojanClientSchema] }
} as const;

const vmessVlessOutboundSettingsSchema = {
  type: 'object',
  description: {
    ru: 'Настройки исходящих серверов VMess/VLESS.',
    en: 'VMess/VLESS outbound server settings.'
  },
  properties: {
    vnext: {
      type: 'array',
      description: {
        ru: 'Список удалённых серверов назначения VMess/VLESS.',
        en: 'List of remote destination servers for VMess/VLESS.'
      },
      items: {
        type: 'object',
        properties: {
          address: {
            type: 'string',
            description: {
              ru: 'Доменное имя или IP-адрес сервера.',
              en: 'Server domain name or IP address.'
            }
          },
          port: {
            type: 'integer',
            minimum: 1,
            maximum: 65535,
            description: {
              ru: 'Порт удалённого сервера (1-65535).',
              en: 'Remote server port (1-65535).'
            }
          },
          users: {
            type: 'array',
            items: vmessVlessUserSchema,
            description: {
              ru: 'Список пользователей с UUID и параметрами шифрования.',
              en: 'List of users with UUID and encryption parameters.'
            }
          }
        },
        required: ['address', 'port', 'users']
      }
    }
  }
} as const;

const trojanServerEntrySchema = {
  type: 'object',
  properties: {
    address: {
      type: 'string',
      description: {
        ru: 'Доменное имя или IP-адрес сервера Trojan.',
        en: 'Trojan server domain name or IP address.'
      }
    },
    port: {
      type: 'integer',
      minimum: 1,
      maximum: 65535,
      description: {
        ru: 'Порт сервера Trojan (1-65535).',
        en: 'Trojan server port (1-65535).'
      }
    },
    password: {
      type: 'string',
      description: {
        ru: 'Пароль подключения к серверу Trojan.',
        en: 'Password for connecting to the Trojan server.'
      }
    },
    email: {
      type: 'string',
      description: {
        ru: 'Email или метка клиента.',
        en: 'Client email or label.'
      }
    },
    level: {
      type: 'integer',
      description: {
        ru: 'Уровень пользователя для применения политик.',
        en: 'User level for applying policies.'
      }
    }
  },
  required: ['address', 'port', 'password']
} as const;

const inboundFallbackSchema = {
  type: 'object',
  description: {
    ru: 'Резервные назначения (fallbacks) VLESS для нераспознанного или не-прокси трафика.',
    en: 'VLESS fallbacks for unrecognized or non-proxy traffic.'
  },
  properties: {
    name: {
      type: 'string',
      description: {
        ru: 'Сопоставление по значению SNI (оставьте пустым для любого SNI).',
        en: 'SNI matching value (leave empty for any SNI).'
      }
    },
    alpn: {
      type: 'string',
      description: {
        ru: 'Сопоставление по протоколу ALPN (например, `h2` или `http/1.1`).',
        en: 'ALPN matching protocol (e.g. `h2` or `http/1.1`).'
      }
    },
    path: {
      type: 'string',
      description: {
        ru: 'Сопоставление по HTTP-пути (префиксу).',
        en: 'HTTP path (prefix) matching.'
      }
    },
    dest: {
      oneOf: [{ type: 'string' }, { type: 'integer' }],
      description: {
        ru: 'Целевой адрес:порт для перенаправления нераспознанного трафика (например, `127.0.0.1:80` или локальный веб-сервер).',
        en: 'Target address:port to forward unrecognized traffic to (e.g. `127.0.0.1:80` or a local web server).'
      }
    },
    xver: {
      type: 'integer',
      description: {
        ru: 'Версия протокола PROXY при пересылке на dest (0, 1 или 2).',
        en: 'PROXY protocol version toward dest (0, 1, or 2).'
      }
    }
  }
} as const;

const socksHttpAccountSchema = {
  type: 'object',
  properties: {
    user: {
      type: 'string',
      description: {
        ru: 'Имя пользователя (логин).',
        en: 'Username.'
      }
    },
    pass: {
      type: 'string',
      description: {
        ru: 'Пароль учётной записи.',
        en: 'Account password.'
      }
    }
  },
  required: ['user', 'pass']
} as const;

const shadowsocksServerEntrySchema = {
  type: 'object',
  properties: {
    address: {
      type: 'string',
      description: {
        ru: 'Адрес сервера Shadowsocks.',
        en: 'Shadowsocks server address.'
      }
    },
    port: {
      type: 'integer',
      minimum: 1,
      maximum: 65535,
      description: {
        ru: 'Порт сервера Shadowsocks (1-65535).',
        en: 'Shadowsocks server port (1-65535).'
      }
    },
    method: {
      type: 'string',
      enum: [...shadowsocksCiphers],
      description: {
        ru: 'Метод шифрования Shadowsocks (рекомендуются современные шифры 2022-blake3).',
        en: 'Shadowsocks encryption method (modern 2022-blake3 ciphers recommended).'
      }
    },
    password: {
      type: 'string',
      description: {
        ru: 'Ключ или пароль Shadowsocks.',
        en: 'Shadowsocks password or key.'
      }
    },
    uot: {
      type: 'boolean',
      description: {
        ru: 'UDP-over-TCP (UOT) — туннелирование UDP через TCP-соединение.',
        en: 'UDP-over-TCP (UOT) — tunnels UDP packets inside TCP connection.'
      }
    },
    level: {
      type: 'integer',
      description: {
        ru: 'Уровень пользователя для применения политик.',
        en: 'User level for applying policies.'
      }
    }
  },
  required: ['address', 'port', 'method', 'password']
} as const;

export const xraySchema = {
  $schema: 'http://json-schema.org/draft-07/schema#',
  type: 'object',
  title: 'Xray Configuration',
  description: {
    ru: 'Конфигурационный файл Xray-core',
    en: 'Xray-core configuration file'
  },
  properties: {
    log: {
      type: 'object',
      description: {
        ru: 'Настройки ведения журналов событий и доступа.',
        en: 'Log configuration.'
      },
      properties: {
        access: {
          type: 'string',
          description: {
            ru: 'Путь к файлу журнала доступа (access log).',
            en: 'Access log file path.'
          }
        },
        error: {
          type: 'string',
          description: {
            ru: 'Путь к файлу журнала ошибок (error log).',
            en: 'Error log file path.'
          }
        },
        loglevel: {
          type: 'string',
          enum: ['debug', 'info', 'warning', 'error', 'none'],
          description: {
            ru: '- **debug** — максимально подробный вывод (только для отладки).\n- **info** / **warning** — стандартный режим работы роутера.\n- **error** — только ошибки.\n- **none** — полное отключение логирования.\n\n> `debug` быстро забивает хранилище и системный журнал роутера — не оставляйте его включённым надолго.',
            en: '- **debug** — very verbose, for active troubleshooting only.\n- **info** / **warning** — standard operational mode for router.\n- **error** — log errors only.\n- **none** — disable logging entirely.\n\n> `debug` quickly fills router storage and logs — do not leave it enabled indefinitely.'
          }
        }
      }
    },
    api: {
      type: 'object',
      description: {
        ru: 'Настройки встроенного gRPC API Xray для мониторинга статистики и управления.',
        en: 'Xray internal gRPC API configuration for stats and control.'
      },
      properties: {
        tag: {
          type: 'string',
          description: {
            ru: 'Тег входящего соединения (inbound tag), выделенного под API.',
            en: 'Inbound tag assigned to the API.'
          }
        },
        services: {
          type: 'array',
          items: {
            type: 'string',
            enum: ['HandlerService', 'LoggerService', 'StatsService', 'RoutingService']
          },
          description: {
            ru: 'Список активных gRPC-сервисов API.',
            en: 'List of enabled gRPC API services.'
          }
        }
      }
    },
    dns: {
      type: 'object',
      description: {
        ru: 'Встроенный DNS-клиент Xray для разрешения доменных имён и сопоставления правил.',
        en: 'Built-in Xray DNS client configuration for domain resolution.'
      },
      properties: {
        servers: {
          type: 'array',
          description: {
            ru: 'Список DNS-серверов (IP-адреса или объекты с привязкой доменов).',
            en: 'List of DNS servers (IP addresses or domain-mapped objects).'
          },
          items: {
            oneOf: [
              {
                type: 'string',
                description: {
                  ru: 'IP-адрес DNS-сервера (например, `8.8.8.8` или `tcp+local://1.1.1.1:53`).',
                  en: 'DNS server IP address (e.g. `8.8.8.8` or `tcp+local://1.1.1.1:53`).'
                }
              },
              {
                type: 'object',
                properties: {
                  address: {
                    type: 'string',
                    description: {
                      ru: 'Адрес DNS-сервера.',
                      en: 'DNS server address.'
                    }
                  },
                  port: {
                    type: 'integer',
                    description: {
                      ru: 'Порт DNS-сервера (по умолчанию 53).',
                      en: 'DNS server port (default 53).'
                    }
                  },
                  domains: {
                    type: 'array',
                    items: { type: 'string' },
                    description: {
                      ru: 'Список доменов, направляемых на этот DNS-сервер.',
                      en: 'List of domains routed to this DNS server.'
                    }
                  }
                }
              }
            ]
          }
        }
      }
    },
    routing: {
      type: 'object',
      description: {
        ru: 'Правила маршрутизации трафика между входящими и исходящими узлами.',
        en: 'Traffic routing rules between inbounds and outbounds.'
      },
      properties: {
        domainStrategy: {
          type: 'string',
          enum: ['AsIs', 'IPIfNonMatch', 'IPOnDemand'],
          description: {
            ru: 'Стратегия разрешения доменов при сопоставлении правил маршрутизации:\n- **AsIs** — сопоставление только по домену без DNS-запроса (наименьшая задержка).\n- **IPIfNonMatch** — отправлять DNS-запрос, только если ни одно правило по домену не совпало.\n- **IPOnDemand** — разрешать DNS сразу при проверке первого правила с условием по IP.',
            en: 'Domain resolution strategy when matching rules:\n- **AsIs** — match by domain without DNS lookup (lowest latency).\n- **IPIfNonMatch** — resolve IP only if no domain-based rule matches.\n- **IPOnDemand** — resolve IP as soon as any IP-based rule is evaluated.'
          }
        },
        domainMatcher: {
          type: 'string',
          enum: ['hybrid', 'linear'],
          description: {
            ru: 'Алгоритм сопоставления доменов:\n- **hybrid** — быстрый индексированный алгоритм (по умолчанию).\n- **linear** — последовательный перебор правил по порядку (для отладки).',
            en: 'Domain matching algorithm:\n- **hybrid** — fast indexed matcher (default, recommended).\n- **linear** — sequential rule evaluation (for debugging).'
          }
        },
        rules: {
          type: 'array',
          description: {
            ru: 'Массив правил маршрутизации. В отличие от Mihomo, порядок не всегда строго определяет приоритет — срабатывает наиболее специфичное совпадение.\n\n> `outboundTag` и `balancerTag` обязаны указывать на существующие теги из `outbounds` или `balancers`.',
            en: 'Traffic routing rules. Unlike Mihomo, order does not strictly decide priority — the most specific matching rule wins.\n\n> `outboundTag` or `balancerTag` must name a tag that actually exists in `outbounds` or `balancers`.'
          },
          items: {
            type: 'object',
            properties: {
              type: {
                type: 'string',
                enum: ['field'],
                description: {
                  ru: 'Тип правила (по умолчанию "field").',
                  en: 'Rule type (default "field").'
                }
              },
              domain: {
                type: 'array',
                items: { type: 'string' },
                description: {
                  ru: 'Список сопоставления доменов: `geosite:...`, `regexp:...`, `domain:...`, `full:...`',
                  en: 'Domain matching list: `geosite:...`, `regexp:...`, `domain:...`, `full:...`'
                }
              },
              ip: {
                type: 'array',
                items: { type: 'string' },
                description: {
                  ru: 'Список сопоставления IP: `geoip:...`, IP-адреса и CIDR-подсети.',
                  en: 'IP matching list: `geoip:...`, IP addresses, and CIDR subnets.'
                }
              },
              port: {
                type: 'string',
                description: {
                  ru: 'Диапазон целевых портов (например, `"80,443"` или `"1000-2000"`).',
                  en: 'Target port range (e.g. `"80,443"` or `"1000-2000"`).'
                }
              },
              network: {
                type: 'string',
                enum: ['tcp', 'udp'],
                description: {
                  ru: 'Сетевой протокол транспортного уровня (tcp или udp).',
                  en: 'Transport network protocol (tcp or udp).'
                }
              },
              source: {
                type: 'array',
                items: { type: 'string' },
                description: {
                  ru: 'Список исходных IP-адресов или CIDR-подсетей клиентов LAN.',
                  en: 'Source client IP addresses or CIDR subnets.'
                }
              },
              user: {
                type: 'array',
                items: { type: 'string' },
                description: {
                  ru: 'Список email авторизованных пользователей.',
                  en: 'Authorized user email list.'
                }
              },
              inboundTag: {
                type: 'array',
                items: { type: 'string' },
                description: {
                  ru: 'Список тегов входящих подключений (inbounds), к которым применяется правило.',
                  en: 'Inbound tags to which this rule applies.'
                }
              },
              protocol: {
                type: 'array',
                items: { type: 'string' },
                description: {
                  ru: 'Список распознанных протоколов прикладного уровня (http, tls, bittorrent).',
                  en: 'Sniffed application protocol list (http, tls, bittorrent).'
                }
              },
              outboundTag: {
                type: 'string',
                description: {
                  ru: 'Целевой тег исходящего узла (outbound) для совпавшего трафика.',
                  en: 'Target outbound tag for matched traffic.'
                }
              },
              balancerTag: {
                type: 'string',
                description: {
                  ru: 'Целевой тег балансировщика для совпавшего трафика.',
                  en: 'Target balancer tag for matched traffic.'
                }
              }
            }
          }
        },
        balancers: {
          type: 'array',
          description: {
            ru: 'Конфигурация балансировщиков нагрузки между группой исходящих узлов.',
            en: 'Load balancing configurations across outbound node pools.'
          },
          items: {
            type: 'object',
            properties: {
              tag: {
                type: 'string',
                description: {
                  ru: 'Уникальное имя тега балансировщика.',
                  en: 'Balancer tag name.'
                }
              },
              selector: {
                type: 'array',
                items: { type: 'string' },
                description: {
                  ru: 'Шаблоны/префиксы тегов outbound для включения в пул балансировки.',
                  en: 'Selector patterns/prefixes for outbound tags.'
                }
              },
              strategy: {
                type: 'object',
                description: {
                  ru: 'Стратегия распределения нагрузки между узлами.',
                  en: 'Load distribution strategy.'
                },
                properties: {
                  type: {
                    type: 'string',
                    enum: ['random', 'leastPing', 'roundRobin', 'leastLoad'],
                    description: {
                      ru: 'Тип алгоритма балансировки: random (случайно), leastPing (наименьший пинг), roundRobin (по очереди), leastLoad (наименьшая нагрузка).',
                      en: 'Balancer algorithm type: random, leastPing, roundRobin, leastLoad.'
                    }
                  }
                }
              }
            }
          }
        }
      }
    },
    inbounds: {
      type: 'array',
      description: {
        ru: 'Список входящих интерфейсов и прокси (порты прослушивания роутера).',
        en: 'Inbound proxy configurations (router listening ports).'
      },
      items: {
        type: 'object',
        properties: {
          tag: {
            type: 'string',
            description: {
              ru: 'Уникальный идентификатор входящего интерфейса (inbound tag).',
              en: 'Inbound tag identifier.'
            }
          },
          port: {
            oneOf: [
              { type: 'integer', minimum: 1, maximum: 65535 },
              {
                type: 'string',
                description: {
                  ru: 'Диапазон портов, например "1000-2000".',
                  en: 'Port range, e.g. "1000-2000".'
                }
              }
            ],
            description: {
              ru: 'Порт прослушивания или диапазон портов.',
              en: 'Listening port or port range.'
            }
          },
          protocol: {
            type: 'string',
            enum: [
              'vmess',
              'vless',
              'trojan',
              'shadowsocks',
              'socks',
              'http',
              'dokodemo-door',
              'mtproto'
            ],
            description: {
              ru: 'Протокол входящего прокси (например, `dokodemo-door` для прозрачного перехвата, `socks` или `http`).',
              en: 'Inbound protocol (e.g. `dokodemo-door` for transparent proxy, `socks` or `http`).'
            }
          },
          listen: {
            type: 'string',
            description: {
              ru: 'IP-адрес привязки сокета (`0.0.0.0` — все интерфейсы, `127.0.0.1` — только локально).',
              en: 'Socket bind address (`0.0.0.0` for all interfaces, `127.0.0.1` for localhost only).'
            }
          },
          sniffing: {
            type: 'object',
            description: {
              ru: 'Перехват (сниффинг) SNI из TLS-рукопожатий и заголовка Host из HTTP для извлечения реального домена.\n\n> Критически важно для прозрачного проксирования (`dokodemo-door` / TPROXY): без сниффинга правила маршрутизации по доменам не смогут обработать трафик, поступающий по IP.',
              en: 'Peeks at TLS SNI and HTTP Host to recover real domains for connections with bare destination IPs.\n\n> Essential under transparent interception (dokodemo-door / TPROXY); without it, domain-based routing rules cannot match incoming traffic.'
            },
            properties: {
              enabled: {
                type: 'boolean',
                description: {
                  ru: 'Включение механизма сниффинга доменов.',
                  en: 'Enable domain sniffing.'
                }
              },
              destOverride: {
                type: 'array',
                items: { type: 'string' },
                description: {
                  ru: 'Список протоколов для сниффинга (например, `["tls", "http", "quic"]`).',
                  en: 'Which protocols to sniff for (e.g. `["tls", "http", "quic"]`).'
                }
              },
              routeOnly: {
                type: 'boolean',
                description: {
                  ru: 'Использовать перехваченный домен только для маршрутизации, не подменяя целевой IP-адрес соединения.\n\n> Рекомендуется включить (`true`), если целевой IP доступен напрямую и требуется только маршрутизация по доменным спискам.',
                  en: 'Use the sniffed domain only for routing decisions while retaining original destination IP.\n\n> Recommended (`true`) when the IP is directly reachable and you only need domain rule matching.'
                }
              }
            }
          },
          settings: {
            type: 'object',
            description: {
              ru: 'Специфичные настройки выбранного протокола входящего соединения.',
              en: 'Protocol-specific inbound settings.'
            },
            properties: {
              clients: inboundClientsSchema,
              decryption: {
                type: 'string',
                description: {
                  ru: 'Шифрование VLESS (на стороне сервера обычно "none").',
                  en: 'VLESS decryption ("none" on server side).'
                }
              },
              fallbacks: {
                type: 'array',
                items: inboundFallbackSchema,
                description: {
                  ru: 'Резервные назначения для нераспознанного трафика.',
                  en: 'Fallback destinations for unrecognized traffic.'
                }
              },
              method: {
                type: 'string',
                enum: [...shadowsocksCiphers],
                description: {
                  ru: 'Метод шифрования Shadowsocks.',
                  en: 'Shadowsocks encryption method.'
                }
              },
              password: {
                type: 'string',
                description: {
                  ru: 'Пароль подключения Shadowsocks или Trojan.',
                  en: 'Shadowsocks or Trojan shared password.'
                }
              },
              network: {
                type: 'string',
                enum: ['tcp', 'udp', 'tcp,udp'],
                description: {
                  ru: 'Разрешённые протоколы транспортного уровня (`tcp`, `udp` или `tcp,udp`).',
                  en: 'Allowed L4 transport network (`tcp`, `udp`, or `tcp,udp`).'
                }
              },
              auth: {
                type: 'string',
                enum: ['noauth', 'password'],
                description: {
                  ru: 'Режим аутентификации Socks (`noauth` — без пароля, `password` — по логину и паролю).',
                  en: 'Socks authentication mode (`noauth` or `password`).'
                }
              },
              accounts: {
                type: 'array',
                items: socksHttpAccountSchema,
                description: {
                  ru: 'Список учётных данных пользователей (логин/пароль) для Socks/HTTP.',
                  en: 'Socks/HTTP user credentials list.'
                }
              },
              udp: {
                type: 'boolean',
                description: {
                  ru: 'Включение ретрансляции UDP-пакетов.',
                  en: 'Enable UDP relay.'
                }
              },
              ip: {
                type: 'string',
                description: {
                  ru: 'IP-адрес, возвращаемый клиентам при UDP-ассоциации (Socks).',
                  en: 'IP address returned to UDP clients (Socks).'
                }
              },
              address: {
                type: 'string',
                description: {
                  ru: 'Целевой адрес перенаправления (для dokodemo-door).',
                  en: 'Forward target address (for dokodemo-door).'
                }
              },
              followRedirect: {
                type: 'boolean',
                description: {
                  ru: 'Использовать исходный адрес назначения из перенаправления iptables (прозрачный прокси REDIRECT).',
                  en: 'Use original destination address from iptables REDIRECT.'
                }
              }
            }
          },
          streamSettings: streamSettingsSchema
        },
        required: ['protocol']
      }
    },
    outbounds: {
      type: 'array',
      description: {
        ru: 'Список исходящих узлов (прокси-серверы, прямое подключение freedom, блокировка blackhole).',
        en: 'Outbound proxy configurations (upstream proxies, direct freedom, blackhole).'
      },
      items: {
        type: 'object',
        properties: {
          tag: {
            type: 'string',
            description: {
              ru: 'Уникальный тег исходящего узла (используется в правилах routing `outboundTag`).',
              en: 'Outbound tag identifier (referenced in routing rules).'
            }
          },
          protocol: {
            type: 'string',
            enum: [
              'vmess',
              'vless',
              'trojan',
              'shadowsocks',
              'freedom',
              'blackhole',
              'dns',
              'loopback',
              'wireguard'
            ],
            description: {
              ru: 'Протокол исходящего узла.',
              en: 'Outbound protocol.'
            }
          },
          settings: {
            type: 'object',
            description: {
              ru: 'Специфичные настройки выбранного протокола исходящего узла.',
              en: 'Protocol-specific outbound settings.'
            },
            properties: {
              // vmess/vless
              vnext: vmessVlessOutboundSettingsSchema.properties.vnext,
              // trojan
              servers: {
                oneOf: [
                  { type: 'array', items: trojanServerEntrySchema },
                  { type: 'array', items: shadowsocksServerEntrySchema }
                ],
                description: {
                  ru: 'Список удалённых серверов (в формате Trojan или Shadowsocks).',
                  en: 'Server list (Trojan or Shadowsocks form).'
                }
              },
              // shadowsocks legacy single-server form (flattened, no servers[])
              address: {
                oneOf: [
                  {
                    type: 'string',
                    description: {
                      ru: 'Адрес сервера Shadowsocks.',
                      en: 'Shadowsocks server address.'
                    }
                  },
                  {
                    type: 'array',
                    items: { type: 'string' },
                    description: {
                      ru: 'Локальные IP-адреса виртуального интерфейса WireGuard с CIDR-маской.',
                      en: 'WireGuard local tunnel IP addresses with CIDR mask.'
                    }
                  }
                ]
              },
              port: {
                type: 'integer',
                minimum: 1,
                maximum: 65535,
                description: {
                  ru: 'Порт сервера Shadowsocks.',
                  en: 'Shadowsocks server port.'
                }
              },
              method: {
                type: 'string',
                enum: [...shadowsocksCiphers],
                description: {
                  ru: 'Метод шифрования Shadowsocks.',
                  en: 'Shadowsocks encryption method.'
                }
              },
              password: {
                type: 'string',
                description: {
                  ru: 'Пароль подключения Shadowsocks.',
                  en: 'Shadowsocks password.'
                }
              },
              // freedom
              domainStrategy: {
                type: 'string',
                enum: ['AsIs', 'UseIP', 'UseIPv4', 'UseIPv6'],
                description: {
                  ru: 'Стратегия обработки доменов в outbound freedom:\n- **AsIs** — прямое подключение по имени домена (DNS разрешает удалённая сторона).\n- **UseIP** / **UseIPv4** / **UseIPv6** — предварительное разрешение через DNS Xray с фиксацией семейства IP.',
                  en: 'How freedom outbound handles domain destinations:\n- **AsIs** — connect by domain (upstream does DNS lookup).\n- **UseIP** / **UseIPv4** / **UseIPv6** — resolve via Xray DNS first to enforce specific IP family.'
                }
              },
              redirect: {
                type: 'string',
                description: {
                  ru: 'Принудительное перенаправление всего трафика через этот outbound на фиксированный `адрес:порт` вместо исходного назначения.',
                  en: 'Force every connection through this outbound to a fixed address:port instead of original destination.'
                }
              },
              userLevel: {
                type: 'integer',
                description: {
                  ru: 'Уровень пользователя для применения политик.',
                  en: 'User level for policy enforcement.'
                }
              },
              // blackhole
              response: {
                type: 'object',
                description: {
                  ru: 'Действие blackhole перед закрытием соединения:\n- **none** — мгновенный разрыв без передачи данных (тихий сброс).\n- **http** — возврат шаблонного ответа HTTP 403 Forbidden перед закрытием.',
                  en: 'What blackhole outbound sends before closing connection:\n- **none** — close immediately with no data.\n- **http** — send a canned HTTP 403 Forbidden response first.'
                },
                properties: {
                  type: {
                    type: 'string',
                    enum: ['none', 'http'],
                    description: {
                      ru: 'Тип ответа blackhole.',
                      en: 'Blackhole response type.'
                    }
                  }
                }
              },
              // dns (forward outbound)
              network: {
                type: 'string',
                enum: ['tcp', 'udp'],
                description: {
                  ru: 'Транспортный протокол для пересылки DNS-запросов (tcp или udp).',
                  en: 'DNS outbound forwarding transport protocol (tcp or udp).'
                }
              },
              nonIPQuery: {
                type: 'string',
                enum: ['drop', 'skip'],
                description: {
                  ru: 'Поведение DNS outbound для запросов не-IP типов (drop — сбрасывать, skip — пропускать).',
                  en: 'DNS outbound behavior for non-IP queries (drop or skip).'
                }
              },
              // loopback
              inboundTag: {
                type: 'string',
                description: {
                  ru: 'Целевой тег inbound для повторного ввода трафика в стек маршрутизации (loopback).',
                  en: 'Target inbound tag for re-entering the routing pipeline (loopback).'
                }
              },
              // wireguard
              secretKey: {
                type: 'string',
                description: {
                  ru: 'Приватный ключ WireGuard интерфейса (в формате Base64).',
                  en: 'WireGuard private key (Base64).'
                }
              },
              peers: {
                type: 'array',
                description: {
                  ru: 'Список удалённых пиров WireGuard.',
                  en: 'WireGuard peer list.'
                },
                items: {
                  type: 'object',
                  properties: {
                    endpoint: {
                      type: 'string',
                      description: {
                        ru: 'Адрес и порт удалённого WireGuard сервера (`host:port`).',
                        en: 'Remote WireGuard server address:port.'
                      }
                    },
                    publicKey: {
                      type: 'string',
                      description: {
                        ru: 'Публичный ключ удалённого сервера WireGuard.',
                        en: 'Remote server public key.'
                      }
                    },
                    preSharedKey: {
                      type: 'string',
                      description: {
                        ru: 'Предварительный общий ключ (PSK, опционально).',
                        en: 'Pre-shared key (PSK, optional).'
                      }
                    },
                    keepAlive: {
                      type: 'integer',
                      description: {
                        ru: 'Интервал отправки пакетов keepalive в секундах (рекомендуется 25).',
                        en: 'Keepalive interval in seconds (25 recommended).'
                      }
                    },
                    allowedIPs: {
                      type: 'array',
                      items: { type: 'string' },
                      description: {
                        ru: 'Список разрешённых IP/CIDR для маршрутизации через этот пир (обычно `["0.0.0.0/0", "::/0"]`).',
                        en: 'Allowed IPs routing CIDR list (typically `["0.0.0.0/0", "::/0"]`).'
                      }
                    }
                  }
                }
              },
              mtu: {
                type: 'integer',
                description: {
                  ru: 'Размер MTU виртуального интерфейса WireGuard (обычно 1420 или 1280).',
                  en: 'WireGuard interface MTU (typically 1420 or 1280).'
                }
              },
              reserved: {
                type: 'array',
                items: { type: 'integer' },
                description: {
                  ru: 'Зарезервированные байты рукопожатия WireGuard `[B1, B2, B3]` для обхода DPI.\n\n> Оставляйте `[0, 0, 0]`, если сервер не требует специальных значений — при несовпадении соединение зависает по таймауту.',
                  en: 'Reserved handshake bytes `[B1, B2, B3]` used for DPI circumvention.\n\n> Keep `[0, 0, 0]` unless server specifically requires custom bytes — mismatch silently times out.'
                }
              }
            }
          },
          streamSettings: streamSettingsSchema,
          proxySettings: {
            type: 'object',
            description: {
              ru: 'Параметры пересылки через цепочку прокси (proxy chaining).',
              en: 'Proxy forwarding and chaining settings.'
            }
          },
          mux: {
            type: 'object',
            description: {
              ru: 'Мультиплексирование TCP/TLS соединений (Mux.Cool) для объединения нескольких логических запросов в один TCP-поток.\n\n> Снижает задержку на рукопожатиях при множестве мелких запросов, но может снижать пропускную способность при передаче больших файлов.',
              en: 'TCP/TLS connection multiplexing (Mux.Cool) combining multiple connections into a single stream.\n\n> Cuts handshake overhead for many short requests, but can hurt single large file transfers if concurrency is set too high.'
            },
            properties: {
              enabled: {
                type: 'boolean',
                description: {
                  ru: 'Включение мультиплексирования Mux.',
                  en: 'Enable Mux multiplexing.'
                }
              },
              concurrency: {
                type: 'integer',
                description: {
                  ru: 'Максимальное число мультиплексируемых TCP-соединений в одном потоке (обычно 8).',
                  en: 'Maximum concurrent multiplexed TCP connections per stream (typically 8).'
                }
              },
              xudpConcurrency: {
                type: 'integer',
                description: {
                  ru: 'Максимальное число мультиплексируемых UDP-соединений.',
                  en: 'Maximum concurrent multiplexed UDP connections.'
                }
              },
              xudpProxyUDP: {
                type: 'boolean',
                description: {
                  ru: 'Проксирование UDP пакетов через xudp.',
                  en: 'Proxy UDP packets over xudp.'
                }
              }
            }
          }
        },
        required: ['protocol']
      }
    },
    policy: {
      type: 'object',
      description: {
        ru: 'Системные политики уровней пользователей, таймаутов и буферов соединений.',
        en: 'Connection policies, timeouts, and user levels.'
      },
      properties: {
        levels: {
          type: 'object',
          description: {
            ru: 'Пользовательские уровни политик (числовые ключи "0", "1", ...).',
            en: 'Per-user level policy maps.'
          },
          additionalProperties: {
            type: 'object',
            properties: {
              handshake: {
                type: 'integer',
                description: {
                  ru: 'Таймаут рукопожатия в секундах.',
                  en: 'Handshake timeout in seconds.'
                }
              },
              connIdle: {
                type: 'integer',
                description: {
                  ru: 'Таймаут простоя соединения в секундах.',
                  en: 'Connection idle timeout in seconds.'
                }
              },
              uplinkOnly: {
                type: 'integer',
                description: {
                  ru: 'Таймаут закрытия соединения после завершения передачи клиентом.',
                  en: 'Timeout for closing connection after client finishes sending.'
                }
              },
              downlinkOnly: {
                type: 'integer',
                description: {
                  ru: 'Таймаут закрытия соединения после завершения передачи сервером.',
                  en: 'Timeout for closing connection after server finishes sending.'
                }
              },
              statsUserUplink: {
                type: 'boolean',
                description: {
                  ru: 'Включение сбора статистики исходящего трафика пользователя.',
                  en: 'Enable user uplink traffic statistics.'
                }
              },
              statsUserDownlink: {
                type: 'boolean',
                description: {
                  ru: 'Включение сбора статистики входящего трафика пользователя.',
                  en: 'Enable user downlink traffic statistics.'
                }
              },
              bufferSize: {
                type: 'integer',
                description: {
                  ru: 'Размер внутреннего буфера канала в килобайтах.',
                  en: 'Channel buffer size in kilobytes.'
                }
              }
            }
          }
        },
        system: {
          type: 'object',
          description: {
            ru: 'Системные настройки сбора статистики для входящих и исходящих соединений.',
            en: 'System-wide statistics collection settings.'
          },
          properties: {
            statsInboundUplink: {
              type: 'boolean',
              description: {
                ru: 'Считать исходящий трафик по всем inbound.',
                en: 'Count uplink traffic across all inbounds.'
              }
            },
            statsInboundDownlink: {
              type: 'boolean',
              description: {
                ru: 'Считать входящий трафик по всем inbound.',
                en: 'Count downlink traffic across all inbounds.'
              }
            },
            statsOutboundUplink: {
              type: 'boolean',
              description: {
                ru: 'Считать исходящий трафик по всем outbound.',
                en: 'Count uplink traffic across all outbounds.'
              }
            },
            statsOutboundDownlink: {
              type: 'boolean',
              description: {
                ru: 'Считать входящий трафик по всем outbound.',
                en: 'Count downlink traffic across all outbounds.'
              }
            },
            overrideAccessLogDest: {
              type: 'boolean',
              description: {
                ru: 'Переопределить путь access-лога значением из `log.access`, когда он меняется через runtime API.',
                en: "Override the access log destination with `log.access`'s value when changed via the runtime API."
              }
            }
          }
        }
      }
    },
    stats: {
      type: 'object',
      description: {
        ru: 'Включение встроенного модуля сбора статистики трафика Xray.',
        en: 'Enable Xray built-in traffic statistics module.'
      }
    },
    reverse: {
      type: 'object',
      description: {
        ru: 'Конфигурация обратного прокси (Reverse proxy / bridges / portals) для пробива NAT.',
        en: 'Reverse proxy configuration (bridges and portals) for NAT traversal.'
      },
      properties: {
        bridges: {
          type: 'array',
          items: { type: 'object' },
          description: {
            ru: 'Список мостов (bridges) на стороне клиента за NAT.',
            en: 'Bridge list on the client side behind NAT.'
          }
        },
        portals: {
          type: 'array',
          items: { type: 'object' },
          description: {
            ru: 'Список порталов (portals) на стороне сервера с белым IP.',
            en: 'Portal list on the server side with public IP.'
          }
        }
      }
    },
    fakedns: {
      type: 'object',
      description: {
        ru: 'Встроенный пул фиктивных IP-адресов (Fake-IP) Xray для сопоставления доменов при прозрачном проксировании.\n\n> Пул Fake-IP Xray полностью независим от `dns.fake-ip-range` в Mihomo. Если оба ядра активны на роутере, используйте непересекающиеся подсети во избежание конфликтов адресов.',
        en: "Xray's own fake-ip pool for domain-based routing.\n\n> Independent from Mihomo's `dns.fake-ip-range`. If both cores run concurrently, configure disjoint subnets to prevent collisions."
      },
      properties: {
        ipPool: {
          type: 'string',
          description: {
            ru: 'CIDR-диапазон пула Fake-IP (например, `198.18.0.0/15`).',
            en: 'Fake-IP address pool CIDR (e.g. `198.18.0.0/15`).'
          }
        },
        poolSize: {
          type: 'integer',
          description: {
            ru: 'Максимальное количество сопоставлений доменов в пуле Fake-IP.',
            en: 'Maximum number of domain mappings in Fake-IP pool.'
          }
        }
      }
    },
    burstObservatory: {
      type: 'object',
      description: {
        ru: 'Пакетный мониторинг доступности и состояния исходящих узлов.',
        en: 'Burst health monitoring for outbound nodes.'
      },
      properties: {
        subjectSelector: {
          type: 'array',
          items: { type: 'string' },
          description: {
            ru: 'Селекторы тегов исходящих узлов для пакетной проверки.',
            en: 'Outbound tag selectors for burst probing.'
          }
        },
        probeURL: {
          type: 'string',
          description: {
            ru: 'URL-адрес для контрольных проверок доступности.',
            en: 'URL for burst health probes.'
          }
        },
        probeInterval: {
          type: 'string',
          description: {
            ru: 'Интервал между проверками (например, `1m`).',
            en: 'Probe interval (e.g. `1m`).'
          }
        }
      }
    },
    observatory: {
      type: 'object',
      description: {
        ru: 'Фоновый мониторинг задержки и доступности исходящих узлов (результаты используются стратегиями балансировки, такими как `leastPing`).',
        en: 'Background health and latency prober feeding balancer strategies like `leastPing`.'
      },
      properties: {
        subjectSelector: {
          type: 'array',
          items: { type: 'string' },
          description: {
            ru: 'Селекторы тегов исходящих узлов для наблюдения.',
            en: 'Outbound tag selectors to observe.'
          }
        },
        probeURL: {
          type: 'string',
          description: {
            ru: 'URL-адрес для замера задержки (health probe).',
            en: 'URL for latency and health probe.'
          }
        },
        probeInterval: {
          type: 'string',
          description: {
            ru: 'Интервал между замерами (например, `10s`).',
            en: 'Probe interval (e.g. `10s`).'
          }
        },
        enableConcurrency: {
          type: 'boolean',
          description: {
            ru: 'Разрешить одновременный параллельный опрос нескольких узлов.',
            en: 'Enable concurrent probing across multiple outbounds.'
          }
        }
      }
    },
    env: {
      type: 'object',
      additionalProperties: { type: 'string' },
      description: {
        ru: 'Переменные окружения Xray, заданные прямо в конфигурации, а не через окружение процесса на роутере.',
        en: 'Xray environment variables set directly in the config, instead of via the router process environment.'
      }
    },
    transport: {
      type: 'object',
      description: {
        ru: '> Устаревший способ задать транспорт глобально — используйте `streamSettings` внутри конкретного `inbound`/`outbound` вместо этого блока.',
        en: '> Deprecated global transport block — use `streamSettings` inside a specific `inbound`/`outbound` instead.'
      }
    },
    metrics: {
      type: 'object',
      description: {
        ru: 'Встроенный HTTP-сервер метрик в формате Prometheus. Активируется через inbound с тем же тегом, что указан здесь — доступ к `/debug/vars` и `/metrics` идёт через этот inbound.',
        en: 'Built-in Prometheus-compatible metrics HTTP server. Activated via an inbound sharing the same tag as configured here — `/debug/vars` and `/metrics` are served through that inbound.'
      },
      properties: {
        tag: {
          type: 'string',
          description: {
            ru: 'Тег inbound, обслуживающего метрики. Нужно объявить `inbound` с этим тегом и `protocol: dokodemo-door`, а правило маршрутизации — вести на `outboundTag: metrics`.',
            en: 'Tag of the inbound serving metrics. Requires declaring an `inbound` with this tag and `protocol: dokodemo-door`, with a routing rule pointing to `outboundTag: metrics`.'
          }
        }
      }
    },
    geodata: {
      type: 'object',
      description: {
        ru: 'Автообновление и горячая перезагрузка файлов geodata (geosite/geoip) без перезапуска ядра. Набор доступных полей зависит от версии Xray-core.',
        en: 'Auto-update and hot-reload of geodata (geosite/geoip) files without restarting the core. Available fields depend on the Xray-core version.'
      }
    },
    version: {
      type: 'object',
      description: {
        ru: 'Ограничение версий Xray-core, с которыми разрешено запускать этот конфиг — ядро откажется стартовать при несовпадении.',
        en: 'Restricts which Xray-core versions may run this config — the core refuses to start on a mismatch.'
      }
    }
  }
};
