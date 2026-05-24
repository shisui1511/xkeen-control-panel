import { snippetCompletion } from '@codemirror/autocomplete';
import type { Completion, CompletionContext, CompletionResult } from '@codemirror/autocomplete';

// ── Xray JSON outbound snippets ─────────────────────────────────────────────

const xrayOutboundSnippets: Completion[] = [
  snippetCompletion(
    `{
  "tag": "\${1:vless-reality-out}",
  "protocol": "vless",
  "settings": {
    "vnext": [{
      "address": "\${2:example.com}",
      "port": \${3:443},
      "users": [{
        "id": "\${4:your-uuid}",
        "flow": "xtls-rprx-vision",
        "encryption": "none"
      }]
    }]
  },
  "streamSettings": {
    "network": "tcp",
    "security": "reality",
    "realitySettings": {
      "serverName": "\${5:www.apple.com}",
      "fingerprint": "chrome",
      "publicKey": "\${6:public-key}",
      "shortId": "\${7:short-id}"
    }
  }
}`,
    { label: 'vless-reality', detail: 'VLESS + XTLS Reality outbound', type: 'keyword', boost: 10 }
  ),

  snippetCompletion(
    `{
  "tag": "\${1:vless-ws-out}",
  "protocol": "vless",
  "settings": {
    "vnext": [{
      "address": "\${2:example.com}",
      "port": \${3:443},
      "users": [{
        "id": "\${4:your-uuid}",
        "encryption": "none"
      }]
    }]
  },
  "streamSettings": {
    "network": "ws",
    "security": "tls",
    "tlsSettings": {
      "serverName": "\${5:example.com}"
    },
    "wsSettings": {
      "path": "\${6:/}"
    }
  }
}`,
    { label: 'vless-ws-tls', detail: 'VLESS + WebSocket + TLS outbound', type: 'keyword', boost: 9 }
  ),

  snippetCompletion(
    `{
  "tag": "\${1:vmess-ws-out}",
  "protocol": "vmess",
  "settings": {
    "vnext": [{
      "address": "\${2:example.com}",
      "port": \${3:443},
      "users": [{
        "id": "\${4:your-uuid}",
        "alterId": 0,
        "security": "auto"
      }]
    }]
  },
  "streamSettings": {
    "network": "ws",
    "security": "tls",
    "tlsSettings": {
      "serverName": "\${5:example.com}"
    },
    "wsSettings": {
      "path": "\${6:/}"
    }
  }
}`,
    { label: 'vmess-ws-tls', detail: 'VMess + WebSocket + TLS outbound', type: 'keyword', boost: 8 }
  ),

  snippetCompletion(
    `{
  "tag": "\${1:hy2-out}",
  "protocol": "hysteria2",
  "settings": {
    "servers": [{
      "address": "\${2:example.com}",
      "port": \${3:443},
      "password": "\${4:your-password}"
    }]
  },
  "streamSettings": {
    "network": "hysteria2",
    "security": "tls",
    "tlsSettings": {
      "serverName": "\${5:example.com}"
    }
  }
}`,
    { label: 'hysteria2', detail: 'Hysteria2 outbound', type: 'keyword', boost: 9 }
  ),

  snippetCompletion(
    `{
  "tag": "\${1:tuic-out}",
  "protocol": "tuic",
  "settings": {
    "servers": [{
      "address": "\${2:example.com}",
      "port": \${3:443},
      "uuid": "\${4:your-uuid}",
      "password": "\${5:your-password}",
      "congestionControl": "bbr"
    }]
  },
  "streamSettings": {
    "network": "tuic",
    "security": "tls",
    "tlsSettings": {
      "serverName": "\${6:example.com}"
    }
  }
}`,
    { label: 'tuic', detail: 'TUIC outbound', type: 'keyword', boost: 8 }
  ),

  snippetCompletion(
    `{
  "tag": "\${1:ss-out}",
  "protocol": "shadowsocks",
  "settings": {
    "servers": [{
      "address": "\${2:example.com}",
      "port": \${3:443},
      "method": "\${4:aes-256-gcm}",
      "password": "\${5:your-password}"
    }]
  }
}`,
    { label: 'shadowsocks', detail: 'Shadowsocks outbound', type: 'keyword', boost: 7 }
  ),

  snippetCompletion(
    `{
  "tag": "\${1:trojan-out}",
  "protocol": "trojan",
  "settings": {
    "servers": [{
      "address": "\${2:example.com}",
      "port": \${3:443},
      "password": "\${4:your-password}"
    }]
  },
  "streamSettings": {
    "network": "tcp",
    "security": "tls",
    "tlsSettings": {
      "serverName": "\${5:example.com}"
    }
  }
}`,
    { label: 'trojan', detail: 'Trojan outbound', type: 'keyword', boost: 7 }
  ),

  snippetCompletion(
    `{
  "tag": "\${1:rule}",
  "type": "field",
  "domain": ["\${2:example.com}"],
  "outboundTag": "\${3:proxy}"
}`,
    {
      label: 'routing-rule-domain',
      detail: 'Xray routing rule (domain)',
      type: 'keyword',
      boost: 6
    }
  ),

  snippetCompletion(
    `{
  "tag": "\${1:rule}",
  "type": "field",
  "ip": ["\${2:0.0.0.0/0}"],
  "outboundTag": "\${3:proxy}"
}`,
    { label: 'routing-rule-ip', detail: 'Xray routing rule (IP/CIDR)', type: 'keyword', boost: 6 }
  ),

  snippetCompletion(
    `{
  "tag": "\${1:rule}",
  "type": "field",
  "domain": ["geosite:\${2:google}"],
  "ip": ["geoip:\${3:google}"],
  "outboundTag": "\${4:proxy}"
}`,
    {
      label: 'routing-rule-geo',
      detail: 'Xray routing rule (geosite + geoip)',
      type: 'keyword',
      boost: 6
    }
  )
];

// ── Mihomo YAML proxy snippets ───────────────────────────────────────────────

const mihomoProxySnippets: Completion[] = [
  snippetCompletion(
    `name: "\${1:vless-reality}"
  type: vless
  server: \${2:example.com}
  port: \${3:443}
  uuid: \${4:your-uuid}
  flow: xtls-rprx-vision
  tls: true
  reality-opts:
    public-key: \${5:public-key}
    short-id: \${6:short-id}
  client-fingerprint: chrome
  servername: \${7:www.apple.com}`,
    { label: 'vless-reality', detail: 'Mihomo VLESS Reality proxy', type: 'keyword', boost: 10 }
  ),

  snippetCompletion(
    `name: "\${1:hy2-proxy}"
  type: hysteria2
  server: \${2:example.com}
  port: \${3:443}
  password: \${4:your-password}
  sni: \${5:example.com}`,
    { label: 'hysteria2', detail: 'Mihomo Hysteria2 proxy', type: 'keyword', boost: 9 }
  ),

  snippetCompletion(
    `name: "\${1:tuic-proxy}"
  type: tuic
  server: \${2:example.com}
  port: \${3:443}
  uuid: \${4:your-uuid}
  password: \${5:your-password}
  congestion-controller: bbr
  sni: \${6:example.com}`,
    { label: 'tuic', detail: 'Mihomo TUIC proxy', type: 'keyword', boost: 9 }
  ),

  snippetCompletion(
    `name: "\${1:ss-proxy}"
  type: ss
  server: \${2:example.com}
  port: \${3:443}
  cipher: aes-256-gcm
  password: \${4:your-password}`,
    { label: 'shadowsocks', detail: 'Mihomo Shadowsocks proxy', type: 'keyword', boost: 7 }
  ),

  snippetCompletion(
    `name: "\${1:vmess-ws}"
  type: vmess
  server: \${2:example.com}
  port: \${3:443}
  uuid: \${4:your-uuid}
  alterId: 0
  cipher: auto
  tls: true
  network: ws
  ws-opts:
    path: \${5:/}`,
    { label: 'vmess-ws', detail: 'Mihomo VMess WebSocket proxy', type: 'keyword', boost: 8 }
  ),

  snippetCompletion(
    `name: "\${1:proxy-group}"
  type: select
  proxies:
    - \${2:DIRECT}`,
    { label: 'proxy-group-select', detail: 'Mihomo select proxy group', type: 'keyword', boost: 6 }
  ),

  snippetCompletion(
    `name: "\${1:auto-group}"
  type: url-test
  proxies:
    - \${2:proxy-name}
  url: https://www.gstatic.com/generate_204
  interval: 300`,
    {
      label: 'proxy-group-urltest',
      detail: 'Mihomo url-test proxy group',
      type: 'keyword',
      boost: 6
    }
  )
];

// ── Completion sources ───────────────────────────────────────────────────────

/**
 * Returns a completion source for Xray JSON snippets.
 * Triggers when the user types a known keyword prefix.
 */
export function xraySnippetSource(context: CompletionContext): CompletionResult | null {
  const word = context.matchBefore(/[\w-]*/);
  if (!word || (word.from === word.to && !context.explicit)) return null;
  return {
    from: word.from,
    options: xrayOutboundSnippets,
    validFor: /^[\w-]*$/
  };
}

/**
 * Returns a completion source for Mihomo YAML snippets.
 */
export function mihomoSnippetSource(context: CompletionContext): CompletionResult | null {
  const word = context.matchBefore(/[\w-]*/);
  if (!word || (word.from === word.to && !context.explicit)) return null;
  return {
    from: word.from,
    options: mihomoProxySnippets,
    validFor: /^[\w-]*$/
  };
}
