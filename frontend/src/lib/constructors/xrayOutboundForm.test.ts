import { describe, it, expect } from 'vitest';
import {
  emptyOutboundForm,
  formToOutbound,
  outboundToForm,
  splitEndpoint
} from './xrayOutboundForm';

function build(patch: Record<string, unknown>) {
  const res = formToOutbound({ ...emptyOutboundForm(), tag: 'node', ...patch });
  if (!res.ok) throw new Error(res.error);
  return res.outbound;
}

// Запись B29 этапа 10: значения полей формы должны попадать в сохранённый узел.
describe('formToOutbound: поля формы попадают в узел (B29)', () => {
  it('VLESS + REALITY: публичный ключ, shortId, fingerprint, SNI', () => {
    const o = build({
      address: 'srv.example',
      port: 8443,
      uuid: '11111111-2222-3333-4444-555555555555',
      flow: 'xtls-rprx-vision',
      security: 'reality',
      sni: 'yahoo.com',
      publicKey: 'PUBKEY',
      shortId: 'ab12',
      fingerprint: 'firefox'
    });
    expect(o.settings.vnext[0]).toMatchObject({
      address: 'srv.example',
      port: 8443,
      users: [{ id: '11111111-2222-3333-4444-555555555555', flow: 'xtls-rprx-vision' }]
    });
    expect(o.streamSettings.security).toBe('reality');
    expect(o.streamSettings.realitySettings).toMatchObject({
      serverName: 'yahoo.com',
      publicKey: 'PUBKEY',
      shortId: 'ab12',
      fingerprint: 'firefox'
    });
  });

  it('REALITY без выбора отпечатка получает chrome', () => {
    const o = build({ security: 'reality', fingerprint: '', publicKey: 'K' });
    expect(o.streamSettings.realitySettings.fingerprint).toBe('chrome');
  });

  it('WebSocket: путь и Host', () => {
    const o = build({ network: 'ws', path: '/ws-path', wsHost: 'cdn.example' });
    expect(o.streamSettings.network).toBe('ws');
    expect(o.streamSettings.wsSettings).toEqual({
      path: '/ws-path',
      headers: { Host: 'cdn.example' }
    });
  });

  it('gRPC: имя сервиса', () => {
    const o = build({ network: 'grpc', serviceName: 'grpc-svc' });
    expect(o.streamSettings.grpcSettings).toEqual({ serviceName: 'grpc-svc' });
  });

  it('VMess: alterId и способ шифрования пользователя', () => {
    const o = build({ protocol: 'vmess', uuid: 'id', cipher: 'chacha20-poly1305', alterId: 3 });
    expect(o.settings.vnext[0].users[0]).toMatchObject({
      id: 'id',
      alterId: 3,
      security: 'chacha20-poly1305'
    });
    expect(o.settings.vnext[0].users[0].flow).toBeUndefined();
  });

  it('Shadowsocks: метод и пароль из полей формы', () => {
    const o = build({
      protocol: 'shadowsocks',
      address: 'ss.example',
      port: 8388,
      cipher: '2022-blake3-aes-128-gcm',
      shadowsocksPassword: 'S3CRET=='
    });
    expect(o.settings.servers[0]).toEqual({
      address: 'ss.example',
      port: 8388,
      password: 'S3CRET==',
      method: '2022-blake3-aes-128-gcm'
    });
    expect(o.streamSettings).toBeDefined();
  });

  it('WireGuard: пир, PSK, allowedIPs, keepAlive, endpoint с портом', () => {
    const o = build({
      protocol: 'wireguard',
      endpoint: 'wg.example:51821',
      wireguardAddress: '10.0.0.2/32, fd00::2/128',
      wireguardSecretKey: 'SECRET',
      wireguardPublicKey: 'PEERPUB',
      wireguardPsk: 'PSK',
      wireguardAllowedIPs: '0.0.0.0/0, ::/0',
      wireguardKeepAlive: 25,
      wireguardMtu: 1280,
      wireguardReserved: '1, 2, 3'
    });
    expect(o.settings).toEqual({
      secretKey: 'SECRET',
      address: ['10.0.0.2/32', 'fd00::2/128'],
      mtu: 1280,
      reserved: [1, 2, 3],
      peers: [
        {
          publicKey: 'PEERPUB',
          preSharedKey: 'PSK',
          endpoint: 'wg.example:51821',
          allowedIPs: ['0.0.0.0/0', '::/0'],
          keepAlive: 25
        }
      ]
    });
    expect(o.streamSettings).toBeUndefined();
  });

  it('WireGuard: endpoint без порта получает 51820, пустые PSK и allowedIPs не пишутся', () => {
    const o = build({ protocol: 'wireguard', endpoint: 'wg.example', wireguardPublicKey: 'P' });
    const peer = o.settings.peers[0];
    expect(peer.endpoint).toBe('wg.example:51820');
    expect(peer).not.toHaveProperty('preSharedKey');
    expect(peer).not.toHaveProperty('allowedIPs');
    expect(peer).not.toHaveProperty('keepAlive');
  });

  it('WireGuard: reserved вне 0..255 отклоняется', () => {
    const res = formToOutbound({
      ...emptyOutboundForm(),
      tag: 'wg',
      protocol: 'wireguard',
      endpoint: 'h:1',
      wireguardReserved: '300, 400, 500'
    });
    expect(res).toEqual({ ok: false, error: 'reserved_invalid' });
  });

  it('параметры сокета: метка, интервал keepalive, флаги и dialerProxy', () => {
    const o = build({
      sockoptMark: 255,
      sockoptTcpKeepAliveInterval: 30,
      sockoptTcpFastOpen: true,
      sockoptTcpMptcp: true,
      sockoptTcpNoDelay: true,
      dialerProxy: 'parent'
    });
    expect(o.streamSettings.sockopt).toEqual({
      dialerProxy: 'parent',
      mark: 255,
      tcpKeepAliveInterval: 30,
      tcpFastOpen: true,
      tcpMptcp: true,
      tcpNoDelay: true
    });
  });

  it('параметры сокета WireGuard сохраняются без транспорта', () => {
    const o = build({ protocol: 'wireguard', endpoint: 'h:1', sockoptMark: 7 });
    expect(o.streamSettings).toEqual({ sockopt: { mark: 7 } });
  });

  it('без параметров сокета ключ sockopt не пишется', () => {
    const o = build({});
    expect(o.streamSettings).not.toHaveProperty('sockopt');
  });
});

describe('outboundToForm: узел → форма и обратно', () => {
  it('REALITY, ws, сокет проходят круг без потерь', () => {
    const form = {
      ...emptyOutboundForm(),
      tag: 'rt',
      address: 'srv.example',
      port: 443,
      uuid: 'uid',
      flow: 'xtls-rprx-vision',
      security: 'reality',
      sni: 'yahoo.com',
      publicKey: 'PK',
      shortId: 'sid',
      fingerprint: 'edge',
      spiderX: '/x',
      network: 'ws',
      path: '/p',
      wsHost: 'h.example',
      sockoptMark: 9,
      dialerProxy: 'up'
    };
    const first = formToOutbound(form);
    if (!first.ok) throw new Error(first.error);
    const again = formToOutbound(outboundToForm(first.outbound));
    expect(again).toEqual(first);
  });

  it('Shadowsocks и WireGuard проходят круг без потерь', () => {
    for (const patch of [
      {
        protocol: 'shadowsocks',
        address: 'a',
        port: 1,
        cipher: 'aes-256-gcm',
        shadowsocksPassword: 'pw'
      },
      {
        protocol: 'wireguard',
        endpoint: 'h:2',
        wireguardAddress: '10.0.0.2/32',
        wireguardSecretKey: 's',
        wireguardPublicKey: 'p',
        wireguardPsk: 'k',
        wireguardAllowedIPs: '0.0.0.0/0',
        wireguardKeepAlive: 20,
        wireguardReserved: '1,2,3'
      }
    ]) {
      const first = formToOutbound({ ...emptyOutboundForm(), tag: 't', ...patch });
      if (!first.ok) throw new Error(first.error);
      expect(formToOutbound(outboundToForm(first.outbound))).toEqual(first);
    }
  });

  it('узел с publicKey в поле password (новое имя Xray) читается в форму', () => {
    const f = outboundToForm({
      tag: 'r',
      protocol: 'vless',
      settings: { vnext: [{ address: 'a', port: 1, users: [{ id: 'i' }] }] },
      streamSettings: {
        network: 'tcp',
        security: 'reality',
        realitySettings: { serverName: 's', password: 'NEWKEY', shortId: 'x' }
      }
    });
    expect(f.publicKey).toBe('NEWKEY');
  });
});

describe('splitEndpoint', () => {
  it('разбирает host:port, [v6]:port и голый адрес', () => {
    expect(splitEndpoint('wg.example:51820')).toEqual({ host: 'wg.example', port: 51820 });
    expect(splitEndpoint('[2001:db8::1]:51820')).toEqual({ host: '2001:db8::1', port: 51820 });
    expect(splitEndpoint('wg.example')).toEqual({ host: 'wg.example', port: undefined });
  });
});
