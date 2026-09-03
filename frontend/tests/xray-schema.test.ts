/**
 * xray-schema.test.ts — Unit-тесты для JSON-схемы Xray.
 */

import { describe, expect, test } from 'vitest';
import { shadowsocksCiphers, xraySchema } from '../src/schemas/xray';

describe('xraySchema Shadowsocks ciphers (D-07)', () => {
  test('длина списка шифров ровно 8', () => {
    expect(shadowsocksCiphers).toHaveLength(8);
  });

  test('присутствие каждого из трёх значений семейства 2022-blake3', () => {
    expect(shadowsocksCiphers).toContain('2022-blake3-aes-128-gcm');
    expect(shadowsocksCiphers).toContain('2022-blake3-aes-256-gcm');
    expect(shadowsocksCiphers).toContain('2022-blake3-chacha20-poly1305');
  });

  test('порядок шифров: 3 семейства 2022-blake3, затем 5 устаревших AEAD', () => {
    expect(shadowsocksCiphers).toEqual([
      '2022-blake3-aes-128-gcm',
      '2022-blake3-aes-256-gcm',
      '2022-blake3-chacha20-poly1305',
      'aes-256-gcm',
      'aes-128-gcm',
      'chacha20-poly1305',
      'xchacha20-poly1305',
      'none'
    ]);
  });

  test('схема настроек outbound и inbound ссылается на актуальные шифры Shadowsocks', () => {
    const outboundProps = (xraySchema.properties.outbounds.items as any).properties;
    expect(outboundProps.settings.properties.method.enum).toEqual([...shadowsocksCiphers]);

    const inboundProps = (xraySchema.properties.inbounds.items as any).properties;
    expect(inboundProps.settings.properties.method.enum).toEqual([...shadowsocksCiphers]);
  });
});

describe('xraySchema outbound protocols & WireGuard settings (D-08)', () => {
  test('перечисление протоколов outbound содержит wireguard и сохраняет все прежние 8 значений', () => {
    const outboundProps = (xraySchema.properties.outbounds.items as any).properties;
    const protocols = outboundProps.protocol.enum;

    expect(protocols).toHaveLength(9);
    expect(protocols).toContain('wireguard');
    expect(protocols).toContain('vmess');
    expect(protocols).toContain('vless');
    expect(protocols).toContain('trojan');
    expect(protocols).toContain('shadowsocks');
    expect(protocols).toContain('freedom');
    expect(protocols).toContain('blackhole');
    expect(protocols).toContain('dns');
    expect(protocols).toContain('loopback');
  });

  test('структура настроек WireGuard в outbound описывает все актуальные поля', () => {
    const outboundProps = (xraySchema.properties.outbounds.items as any).properties;
    const settingsProps = outboundProps.settings.properties;

    expect(settingsProps.secretKey).toBeDefined();
    expect(settingsProps.secretKey.type).toBe('string');

    expect(settingsProps.address).toBeDefined();
    expect(settingsProps.address.type).toBe('array');

    expect(settingsProps.mtu).toBeDefined();
    expect(settingsProps.mtu.type).toBe('integer');

    expect(settingsProps.reserved).toBeDefined();
    expect(settingsProps.reserved.type).toBe('array');

    expect(settingsProps.peers).toBeDefined();
    expect(settingsProps.peers.type).toBe('array');

    const peerProps = settingsProps.peers.items.properties;
    expect(peerProps.endpoint).toBeDefined();
    expect(peerProps.publicKey).toBeDefined();
    expect(peerProps.preSharedKey).toBeDefined();
    expect(peerProps.keepAlive).toBeDefined();
    expect(peerProps.allowedIPs).toBeDefined();
  });
});

describe('xraySchema sockopt & dialerProxy (D-11, D-12)', () => {
  test('множество ключей sockopt состоит ровно из 4 ожидаемых полей', () => {
    const outboundProps = (xraySchema.properties.outbounds.items as any).properties;
    const sockoptProps = outboundProps.streamSettings.properties.sockopt.properties;

    const keys = Object.keys(sockoptProps).sort();
    expect(keys).toEqual(['dialerProxy', 'mark', 'tcpFastOpen', 'tcpMptcp']);

    expect(sockoptProps.mark.type).toBe('integer');
    expect(sockoptProps.tcpFastOpen.oneOf).toBeDefined();
    expect(sockoptProps.tcpMptcp.type).toBe('boolean');
    expect(sockoptProps.dialerProxy.type).toBe('string');
  });

  test('снятое из ядра поле tcpNoDelay отсутствует в схеме sockopt', () => {
    const outboundProps = (xraySchema.properties.outbounds.items as any).properties;
    const sockoptProps = outboundProps.streamSettings.properties.sockopt.properties;
    expect((sockoptProps as any).tcpNoDelay).toBeUndefined();
  });
});

describe('xraySchema routing domainMatcher (D-13 отменено)', () => {
  test('блок стратегии сопоставления доменов присутствует и неизменен', () => {
    const routingProps = (xraySchema.properties.routing as any).properties;
    expect(routingProps.domainMatcher).toBeDefined();
    expect(routingProps.domainMatcher.enum).toEqual(['hybrid', 'linear']);
  });
});
