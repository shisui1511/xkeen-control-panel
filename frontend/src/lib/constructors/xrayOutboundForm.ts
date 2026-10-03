// Преобразование между формой исходящего Xray (поля `XrayOutboundForm.svelte`)
// и узлом `outbounds[]` конфигурации. Имена полей формы заданы шаблоном формы:
// `publicKey`, `shortId`, `fingerprint`, `path`, `serviceName`, `cipher`,
// `shadowsocksPassword`, `endpoint`, `wireguard*`, `sockopt*`. Раньше сохранение
// читало другие имена (`realityPublicKey`, `wsPath`, `method`, ...), и введённые
// значения в узел не попадали.

export type OutboundForm = Record<string, any>;

export type OutboundFormResult =
  { ok: true; outbound: any } | { ok: false; error: 'reserved_invalid' };

const DEFAULT_PORT = 443;
const DEFAULT_WG_PORT = 51820;
const DEFAULT_WG_MTU = 1420;

/** Пустая форма нового исходящего. */
export function emptyOutboundForm(): OutboundForm {
  return {
    tag: '',
    protocol: 'vless',
    address: '',
    port: DEFAULT_PORT,
    uuid: '',
    flow: '',
    cipher: 'aes-128-gcm',
    alterId: 0,
    network: 'tcp',
    security: 'none',
    sni: '',
    publicKey: '',
    shortId: '',
    fingerprint: 'chrome',
    spiderX: '',
    path: '',
    wsHost: '',
    serviceName: '',
    password: '',
    shadowsocksPassword: '',
    endpoint: '',
    wireguardAddress: '',
    wireguardSecretKey: '',
    wireguardPublicKey: '',
    wireguardPsk: '',
    wireguardAllowedIPs: '',
    wireguardKeepAlive: '',
    wireguardMtu: DEFAULT_WG_MTU,
    wireguardReserved: '',
    sockoptMark: '',
    sockoptTcpKeepAliveInterval: '',
    sockoptTcpFastOpen: false,
    sockoptTcpMptcp: false,
    sockoptTcpNoDelay: false,
    dialerProxy: ''
  };
}

const str = (v: unknown): string => (typeof v === 'string' ? v.trim() : '');

function splitList(v: unknown): string[] {
  return String(v ?? '')
    .split(',')
    .map((s) => s.trim())
    .filter(Boolean);
}

function positiveInt(v: unknown): number | undefined {
  if (v === '' || v === null || v === undefined) return undefined;
  const n = Number(v);
  return Number.isFinite(n) && n > 0 ? Math.trunc(n) : undefined;
}

/** `host:port` и `[v6]:port`; без порта возвращает только адрес. */
export function splitEndpoint(endpoint: string): { host: string; port: number | undefined } {
  const value = endpoint.trim();
  if (value.startsWith('[')) {
    const end = value.indexOf(']');
    if (end > 0) {
      const port = Number(value.slice(end + 2));
      return {
        host: value.slice(1, end),
        port: Number.isFinite(port) && port > 0 ? port : undefined
      };
    }
  }
  const idx = value.lastIndexOf(':');
  if (idx > 0 && value.indexOf(':') === idx) {
    const port = Number(value.slice(idx + 1));
    return {
      host: value.slice(0, idx),
      port: Number.isFinite(port) && port > 0 ? port : undefined
    };
  }
  return { host: value, port: undefined };
}

function joinEndpoint(host: string, port: number | undefined): string {
  if (!host) return '';
  const h = host.includes(':') && !host.startsWith('[') ? `[${host}]` : host;
  return port ? `${h}:${port}` : h;
}

/** Разбирает reserved: ровно три числа 0..255; пустая строка допустима. */
function parseReserved(raw: unknown): number[] | undefined | 'invalid' {
  const text = String(raw ?? '').trim();
  if (!text) return undefined;
  const parts = text
    .split(',')
    .map((s) => parseInt(s.trim(), 10))
    .filter((n) => !isNaN(n) && n >= 0 && n <= 255);
  return parts.length === 3 ? parts : 'invalid';
}

/** Узел из `outbounds[]` → значения формы. Незнакомые поля узла формой не отображаются. */
export function outboundToForm(item: any): OutboundForm {
  const f = emptyOutboundForm();
  f.tag = item?.tag || '';
  f.protocol = item?.protocol || 'vless';
  const settings = item?.settings ?? {};
  const stream = item?.streamSettings;

  if (f.protocol === 'vless' || f.protocol === 'vmess') {
    const vnext = settings.vnext?.[0];
    if (vnext) {
      f.address = vnext.address || '';
      f.port = vnext.port || DEFAULT_PORT;
      const user = vnext.users?.[0];
      if (user) {
        f.uuid = user.id || '';
        f.flow = user.flow && user.flow !== 'none' ? user.flow : '';
        if (f.protocol === 'vmess') {
          f.cipher = user.security || 'auto';
          f.alterId = user.alterId ?? 0;
        }
      }
    }
  } else if (f.protocol === 'trojan') {
    const srv = settings.servers?.[0];
    if (srv) {
      f.address = srv.address || '';
      f.port = srv.port || DEFAULT_PORT;
      f.password = srv.password || '';
    }
  } else if (f.protocol === 'shadowsocks') {
    const srv = settings.servers?.[0];
    if (srv) {
      f.address = srv.address || '';
      f.port = srv.port || DEFAULT_PORT;
      f.shadowsocksPassword = srv.password || '';
      f.cipher = srv.method || 'aes-128-gcm';
    }
  } else if (f.protocol === 'wireguard') {
    f.wireguardSecretKey = settings.secretKey || '';
    f.wireguardAddress = Array.isArray(settings.address)
      ? settings.address.join(', ')
      : settings.address || '';
    f.wireguardMtu = settings.mtu || DEFAULT_WG_MTU;
    if (Array.isArray(settings.reserved)) f.wireguardReserved = settings.reserved.join(', ');
    const peer = settings.peers?.[0];
    if (peer) {
      f.endpoint = peer.endpoint || '';
      f.wireguardPublicKey = peer.publicKey || '';
      f.wireguardPsk = peer.preSharedKey || '';
      f.wireguardAllowedIPs = Array.isArray(peer.allowedIPs) ? peer.allowedIPs.join(', ') : '';
      f.wireguardKeepAlive = peer.keepAlive ?? '';
    }
  }

  if (stream) {
    f.network = stream.network || 'tcp';
    f.security = stream.security || 'none';
    if (stream.tlsSettings) {
      f.sni = stream.tlsSettings.serverName || '';
    }
    if (stream.realitySettings) {
      const r = stream.realitySettings;
      f.sni = r.serverName || '';
      f.publicKey = r.publicKey || r.password || '';
      f.shortId = r.shortId || '';
      f.fingerprint = r.fingerprint || 'chrome';
      f.spiderX = r.spiderX || '';
    }
    if (stream.wsSettings) {
      f.path = stream.wsSettings.path || '';
      f.wsHost = stream.wsSettings.headers?.Host || '';
    }
    if (stream.grpcSettings) {
      f.serviceName = stream.grpcSettings.serviceName || '';
    }
    const sock = stream.sockopt;
    if (sock) {
      f.dialerProxy = sock.dialerProxy || '';
      f.sockoptMark = sock.mark ?? '';
      f.sockoptTcpKeepAliveInterval = sock.tcpKeepAliveInterval ?? '';
      f.sockoptTcpFastOpen = sock.tcpFastOpen === true;
      f.sockoptTcpMptcp = sock.tcpMptcp === true;
      f.sockoptTcpNoDelay = sock.tcpNoDelay === true;
    }
  }
  return f;
}

function buildSockopt(form: OutboundForm): Record<string, unknown> | undefined {
  const sock: Record<string, unknown> = {};
  const dialer = str(form.dialerProxy);
  if (dialer) sock.dialerProxy = dialer;
  const mark = positiveInt(form.sockoptMark);
  if (mark !== undefined) sock.mark = mark;
  const keepAlive = positiveInt(form.sockoptTcpKeepAliveInterval);
  if (keepAlive !== undefined) sock.tcpKeepAliveInterval = keepAlive;
  if (form.sockoptTcpFastOpen) sock.tcpFastOpen = true;
  if (form.sockoptTcpMptcp) sock.tcpMptcp = true;
  if (form.sockoptTcpNoDelay) sock.tcpNoDelay = true;
  return Object.keys(sock).length > 0 ? sock : undefined;
}

/** Значения формы → узел для `outbounds[]`. Вызывающий проверяет тег заранее. */
export function formToOutbound(form: OutboundForm): OutboundFormResult {
  const outbound: any = { tag: str(form.tag), protocol: form.protocol };
  const address = str(form.address);
  const port = Number(form.port) || DEFAULT_PORT;

  if (form.protocol === 'vless' || form.protocol === 'vmess') {
    const user: any = { id: str(form.uuid) };
    if (form.protocol === 'vless') {
      if (form.flow && form.flow !== 'none') user.flow = form.flow;
      user.encryption = 'none';
    } else {
      user.alterId = Number(form.alterId) || 0;
      user.security = form.cipher || 'auto';
    }
    outbound.settings = { vnext: [{ address, port, users: [user] }] };
  } else if (form.protocol === 'trojan') {
    outbound.settings = { servers: [{ address, port, password: str(form.password) }] };
  } else if (form.protocol === 'shadowsocks') {
    outbound.settings = {
      servers: [
        {
          address,
          port,
          password: str(form.shadowsocksPassword),
          method: form.cipher || 'aes-128-gcm'
        }
      ]
    };
  } else if (form.protocol === 'wireguard') {
    const reserved = parseReserved(form.wireguardReserved);
    if (reserved === 'invalid') return { ok: false, error: 'reserved_invalid' };
    const ep = splitEndpoint(str(form.endpoint));
    const peer: any = {
      publicKey: str(form.wireguardPublicKey),
      endpoint: joinEndpoint(ep.host, ep.port ?? DEFAULT_WG_PORT)
    };
    const psk = str(form.wireguardPsk);
    if (psk) peer.preSharedKey = psk;
    const allowed = splitList(form.wireguardAllowedIPs);
    if (allowed.length > 0) peer.allowedIPs = allowed;
    const keepAlive = positiveInt(form.wireguardKeepAlive);
    if (keepAlive !== undefined) peer.keepAlive = keepAlive;
    outbound.settings = {
      secretKey: str(form.wireguardSecretKey),
      address: splitList(form.wireguardAddress),
      mtu: Number(form.wireguardMtu) || DEFAULT_WG_MTU,
      peers: [peer]
    };
    if (reserved) outbound.settings.reserved = reserved;
  }

  const sockopt = buildSockopt(form);
  if (form.protocol === 'wireguard') {
    // У WireGuard своего транспорта нет: в streamSettings остаются только параметры сокета.
    if (sockopt) outbound.streamSettings = { sockopt };
  } else if (!['freedom', 'blackhole'].includes(form.protocol)) {
    const stream: any = {
      network: form.network || 'tcp',
      security: form.security || 'none'
    };
    const sni = str(form.sni);
    if (form.security === 'tls') {
      stream.tlsSettings = { serverName: sni || undefined };
    } else if (form.security === 'reality') {
      stream.realitySettings = {
        serverName: sni || undefined,
        fingerprint: str(form.fingerprint) || 'chrome',
        publicKey: str(form.publicKey) || undefined,
        shortId: str(form.shortId) || undefined,
        spiderX: str(form.spiderX) || undefined
      };
    }
    if (form.network === 'ws') {
      const host = str(form.wsHost);
      stream.wsSettings = {
        path: str(form.path) || undefined,
        headers: host ? { Host: host } : undefined
      };
    } else if (form.network === 'grpc') {
      stream.grpcSettings = { serviceName: str(form.serviceName) || undefined };
    }
    if (sockopt) stream.sockopt = sockopt;
    outbound.streamSettings = stream;
  }

  return { ok: true, outbound };
}
