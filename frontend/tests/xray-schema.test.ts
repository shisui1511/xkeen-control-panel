/**
 * xray-schema.test.ts — Unit-тесты для JSON-схемы Xray.
 */

import { describe, expect, test } from 'vitest';
import { shadowsocksCiphers, xraySchema } from '../src/schemas/xray';
import { localizeSchema } from '../src/schemas/localize';

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
    const addressTypes = settingsProps.address.oneOf.map((s: any) => s.type);
    expect(addressTypes).toContain('array');
    expect(addressTypes).toContain('string');

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

describe('xraySchema sockopt & dialerProxy (XRAY-02, XRAY-03, XRAY-04)', () => {
  test('множество ключей sockopt состоит ровно из 6 ожидаемых полей', () => {
    const outboundProps = (xraySchema.properties.outbounds.items as any).properties;
    const sockoptProps = outboundProps.streamSettings.properties.sockopt.properties;

    const keys = Object.keys(sockoptProps).sort();
    expect(keys).toEqual([
      'dialerProxy',
      'mark',
      'tcpFastOpen',
      'tcpKeepAliveInterval',
      'tcpMptcp',
      'tcpNoDelay'
    ]);

    expect(sockoptProps.mark.type).toBe('integer');
    expect(sockoptProps.tcpFastOpen.oneOf).toBeDefined();
    expect(sockoptProps.tcpMptcp.type).toBe('boolean');
    expect(sockoptProps.tcpNoDelay.type).toBe('boolean');
    expect(sockoptProps.tcpKeepAliveInterval.type).toBe('integer');
    expect(sockoptProps.dialerProxy.type).toBe('string');
  });
});

describe('xraySchema routing domainMatcher (D-13 отменено)', () => {
  test('блок стратегии сопоставления доменов присутствует и неизменен', () => {
    const routingProps = (xraySchema.properties.routing as any).properties;
    expect(routingProps.domainMatcher).toBeDefined();
    expect(routingProps.domainMatcher.enum).toEqual(['hybrid', 'linear']);
  });
});

describe('xraySchema vmess/vless/trojan authentication settings', () => {
  const outboundSettings = (xraySchema.properties.outbounds.items as any).properties.settings
    .properties;
  const inboundSettings = (xraySchema.properties.inbounds.items as any).properties.settings
    .properties;

  test('outbound settings.vnext описывает address/port/users с id-паттерном UUID', () => {
    const vnext = outboundSettings.vnext;
    expect(vnext.type).toBe('array');
    const userSchema = vnext.items.properties.users.items;
    expect(userSchema.properties.id.pattern).toBeDefined();
    expect('123e4567-e89b-12d3-a456-426614174000').toMatch(
      new RegExp(userSchema.properties.id.pattern)
    );
  });

  test('outbound settings.servers допускает и trojan (password), и shadowsocks (method) форму', () => {
    const serversOneOf = outboundSettings.servers.oneOf;
    const itemShapes = serversOneOf.map((branch: any) =>
      Object.keys(branch.items.properties).sort()
    );
    expect(itemShapes).toContainEqual(['address', 'email', 'level', 'password', 'port'].sort());
    expect(itemShapes).toContainEqual(
      ['address', 'level', 'method', 'password', 'port', 'uot'].sort()
    );
  });

  test('inbound settings.clients поддерживает id-клиентов (vmess/vless) и password-клиентов (trojan)', () => {
    const clientOneOf = inboundSettings.clients.items.oneOf;
    const keys = clientOneOf.map((branch: any) => Object.keys(branch.properties).sort());
    expect(keys.some((k: string[]) => k.includes('id'))).toBe(true);
    expect(keys.some((k: string[]) => k.includes('password') && !k.includes('id'))).toBe(true);
  });
});

describe('xraySchema streamSettings TLS/Reality/transport coverage', () => {
  const outboundStream = (xraySchema.properties.outbounds.items as any).properties.streamSettings;
  const inboundStream = (xraySchema.properties.inbounds.items as any).properties.streamSettings;

  test('streamSettings переиспользуется дословно между inbound и outbound', () => {
    expect(outboundStream).toBe(inboundStream);
  });

  test('network и security описаны как закрытые enum, а не свободная строка', () => {
    expect(outboundStream.properties.network.enum).toEqual(
      expect.arrayContaining(['tcp', 'ws', 'grpc', 'xhttp'])
    );
    expect(outboundStream.properties.security.enum).toEqual(['none', 'tls', 'reality']);
  });

  test('tlsSettings и realitySettings присутствуют с ключевыми полями', () => {
    expect(outboundStream.properties.tlsSettings.properties.serverName).toBeDefined();
    expect(outboundStream.properties.tlsSettings.properties.alpn).toBeDefined();
    expect(outboundStream.properties.realitySettings.properties.publicKey).toBeDefined();
    expect(outboundStream.properties.realitySettings.properties.shortId).toBeDefined();
  });

  test('wsSettings и grpcSettings присутствуют', () => {
    expect(outboundStream.properties.wsSettings.properties.path).toBeDefined();
    expect(outboundStream.properties.grpcSettings.properties.serviceName).toBeDefined();
  });
});

describe('xraySchema freedom/blackhole/dns/loopback outbound settings', () => {
  const settingsProps = (xraySchema.properties.outbounds.items as any).properties.settings
    .properties;

  test('freedom-специфичные поля присутствуют', () => {
    expect(settingsProps.domainStrategy).toBeDefined();
    expect(settingsProps.redirect).toBeDefined();
  });

  test('blackhole response.type присутствует', () => {
    expect(settingsProps.response.properties.type.enum).toEqual(['none', 'http']);
  });

  test('loopback inboundTag присутствует', () => {
    expect(settingsProps.inboundTag).toBeDefined();
  });
});

describe('xraySchema port bounds and required fields', () => {
  test('inbound port — oneOf integer(1-65535)/string, как у listeners Mihomo', () => {
    const portProp = (xraySchema.properties.inbounds.items as any).properties.port;
    const intBranch = portProp.oneOf.find((b: any) => b.type === 'integer');
    expect(intBranch.minimum).toBe(1);
    expect(intBranch.maximum).toBe(65535);
    expect(portProp.oneOf.some((b: any) => b.type === 'string')).toBe(true);
  });

  test('inbounds и outbounds требуют обязательное поле protocol', () => {
    expect((xraySchema.properties.inbounds.items as any).required).toContain('protocol');
    expect((xraySchema.properties.outbounds.items as any).required).toContain('protocol');
  });
});

describe('xraySchema bilingual descriptions and localization', () => {
  test('все description в xraySchema содержат непустые поля ru и en', () => {
    let count = 0;
    function walk(node: any, path = ''): void {
      if (!node || typeof node !== 'object') return;
      if (Array.isArray(node)) {
        node.forEach((child, i) => walk(child, `${path}[${i}]`));
        return;
      }
      if ('description' in node) {
        count++;
        const desc = node.description;
        expect(typeof desc, `description at ${path} must be an object`).toBe('object');
        expect(desc, `description at ${path} must not be null`).not.toBeNull();
        expect(typeof desc.ru, `desc.ru at ${path} must be string`).toBe('string');
        expect(desc.ru.length, `desc.ru at ${path} must not be empty`).toBeGreaterThan(0);
        expect(typeof desc.en, `desc.en at ${path} must be string`).toBe('string');
        expect(desc.en.length, `desc.en at ${path} must not be empty`).toBeGreaterThan(0);
      }
      for (const [key, val] of Object.entries(node)) {
        if (key !== 'description') {
          walk(val, path ? `${path}.${key}` : key);
        }
      }
    }
    walk(xraySchema);
    expect(count).toBeGreaterThan(100);
  });

  test('localizeSchema корректно преобразует xraySchema для ru и en в строковые описания', () => {
    const ruSchema: any = localizeSchema(xraySchema, 'ru');
    const enSchema: any = localizeSchema(xraySchema, 'en');

    expect(ruSchema.description).toBe('Конфигурационный файл Xray-core');
    expect(enSchema.description).toBe('Xray-core configuration file');

    function assertAllStringDescriptions(node: any): void {
      if (!node || typeof node !== 'object') return;
      if (Array.isArray(node)) {
        node.forEach(assertAllStringDescriptions);
        return;
      }
      if ('description' in node) {
        expect(typeof node.description).toBe('string');
      }
      for (const val of Object.values(node)) {
        assertAllStringDescriptions(val);
      }
    }

    assertAllStringDescriptions(ruSchema);
    assertAllStringDescriptions(enSchema);
  });
});

describe('xraySchema top-level coverage parity', () => {
  test('содержит env/transport/metrics/geodata/version помимо уже описанных ключей', () => {
    const keys = Object.keys((xraySchema as any).properties);
    expect(keys).toEqual(
      expect.arrayContaining([
        'log',
        'api',
        'dns',
        'routing',
        'inbounds',
        'outbounds',
        'policy',
        'stats',
        'reverse',
        'fakedns',
        'burstObservatory',
        'observatory',
        'env',
        'transport',
        'metrics',
        'geodata',
        'version'
      ])
    );
  });

  test('metrics.tag и policy.system.overrideAccessLogDest описаны', () => {
    const props = (xraySchema as any).properties;
    expect(props.metrics.properties.tag.description).toBeTruthy();
    expect(
      props.policy.properties.system.properties.overrideAccessLogDest.description
    ).toBeTruthy();
  });
});
