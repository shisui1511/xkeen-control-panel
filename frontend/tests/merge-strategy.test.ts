/**
 * merge-strategy.test.ts — Unit-тесты merge-логики для Xray конфигурационных файлов.
 *
 * Инварианты (RED до реализации в Plan 15.2-02/04):
 *   D-04  — merge сохраняет неуправляемые ключи (e.g. proxies / кастомный outbound)
 *   D-08  — объектный DNS-сервер сериализуется с полями address/port/tag/domains/skipFallback
 *   D-09  — добавление тегированного DNS порождает dokodemo-door inbound
 *   D-11  — PROXY_TAG заменяется на выбранный outbound тег
 *   идемпотентность — третий merge равен второму
 */

import { test, describe, expect } from 'vitest';
import { mergeXrayFile, generateDnsOverVlessRules } from '../src/lib/xrayMerge';

// ---------------------------------------------------------------------------
// D-04: merge сохраняет неуправляемый ключ proxies / кастомный outbound
// ---------------------------------------------------------------------------
describe('mergeXrayFile (D-04) — сохранение неуправляемых ключей', () => {
  test('merge 05_routing.json сохраняет кастомный outbound тег в proxies', () => {
    const existing = {
      routing: {
        domainStrategy: 'IPIfNonMatch',
        rules: [{ type: 'field', domain: ['geosite:private'], outboundTag: 'custom-outbound' }]
      },
      // Неуправляемый ключ — конструктор не должен его удалять
      _custom_meta: { createdBy: 'manual', version: 2 }
    };

    const managed = {
      rules: [{ type: 'field', port: '53', outboundTag: 'dns-out' }]
    };

    const result = mergeXrayFile('05_routing.json', existing, managed);

    // Управляемые правила должны быть записаны
    expect((result.routing as Record<string, unknown>)?.rules).toBeDefined();
    // Неуправляемый ключ должен сохраниться
    expect(result._custom_meta).toEqual({ createdBy: 'manual', version: 2 });
    // domainStrategy не является управляемым — должен сохраниться
    expect((result.routing as Record<string, unknown>)?.domainStrategy).toBe('IPIfNonMatch');
  });

  test('merge 01_log.json сохраняет пути логов (access/error)', () => {
    const existing = {
      log: {
        loglevel: 'info',
        access: '/opt/var/log/xray/access.log',
        error: '/opt/var/log/xray/error.log',
        dnsLog: false
      }
    };

    const managed = {
      loglevel: 'warning',
      dnsLog: true
    };

    const result = mergeXrayFile('01_log.json', existing, managed);
    const log = result.log as Record<string, unknown>;

    // Управляемые ключи обновляются
    expect(log?.loglevel).toBe('warning');
    expect(log?.dnsLog).toBe(true);
    // Неуправляемые пути не трогаются
    expect(log?.access).toBe('/opt/var/log/xray/access.log');
    expect(log?.error).toBe('/opt/var/log/xray/error.log');
  });
});

// ---------------------------------------------------------------------------
// D-08: объектный DNS-сервер сериализуется с полями address/port/tag/domains/skipFallback
// ---------------------------------------------------------------------------
describe('mergeXrayFile (D-08) — объектные DNS-серверы', () => {
  test('объектный DNS-сервер сохраняет все поля: address, port, tag, domains, skipFallback', () => {
    const existing = {
      dns: {
        tag: 'dns-in',
        queryStrategy: 'UseIP',
        servers: ['8.8.8.8'],
        hosts: {}
      }
    };

    const dnsServer = {
      address: '8.8.8.8',
      port: 53,
      tag: 'dns-in-ytb',
      domains: ['geosite:youtube', 'geosite:google'],
      skipFallback: true
    };

    const managed = {
      servers: [dnsServer],
      queryStrategy: 'UseIP',
      hosts: {}
    };

    const result = mergeXrayFile('02_dns.json', existing, managed);
    const dns = result.dns as Record<string, unknown>;
    const servers = dns?.servers as unknown[];

    expect(servers).toHaveLength(1);
    const server = servers[0] as Record<string, unknown>;
    expect(server.address).toBe('8.8.8.8');
    expect(server.port).toBe(53);
    expect(server.tag).toBe('dns-in-ytb');
    expect(server.domains).toEqual(['geosite:youtube', 'geosite:google']);
    expect(server.skipFallback).toBe(true);
  });
});

// ---------------------------------------------------------------------------
// D-09: добавление тегированного DNS порождает dokodemo-door inbound
// ---------------------------------------------------------------------------
describe('mergeXrayFile (D-09) — автогенерация dokodemo-door inbound', () => {
  test('merge 03_inbounds.json с dnsInbounds создаёт dokodemo-door запись', () => {
    const existing = {
      inbounds: [
        {
          port: 1080,
          protocol: 'socks',
          tag: 'socks-in'
        }
      ]
    };

    const dnsInbound = {
      port: 1082,
      protocol: 'dokodemo-door',
      settings: { network: 'tcp,udp', followRedirect: true },
      sniffing: { enabled: true, routeOnly: true, destOverride: ['http', 'tls', 'quic'] },
      streamSettings: { sockopt: { tproxy: 'tproxy' } },
      tag: 'dns-in-ytb'
    };

    const managed = {
      dnsInbounds: [dnsInbound]
    };

    const result = mergeXrayFile('03_inbounds.json', existing, managed);
    const inbounds = result.inbounds as unknown[];

    // socks-in сохраняется (неуправляемый)
    const socksInbound = (inbounds as Record<string, unknown>[]).find(
      (ib) => ib.tag === 'socks-in'
    );
    expect(socksInbound).toBeDefined();

    // dns-in-ytb добавляется
    const dnsIn = (inbounds as Record<string, unknown>[]).find((ib) => ib.tag === 'dns-in-ytb');
    expect(dnsIn).toBeDefined();
    expect(dnsIn?.protocol).toBe('dokodemo-door');
    expect(dnsIn?.port).toBe(1082);
  });

  test('повторный merge 03_inbounds.json не дублирует dns-in-* inbound (идемпотентность)', () => {
    const existing = {
      inbounds: [
        {
          port: 1082,
          protocol: 'dokodemo-door',
          tag: 'dns-in-ytb'
        }
      ]
    };

    const managed = {
      dnsInbounds: [
        {
          port: 1082,
          protocol: 'dokodemo-door',
          settings: { network: 'tcp,udp', followRedirect: true },
          tag: 'dns-in-ytb'
        }
      ]
    };

    const firstResult = mergeXrayFile('03_inbounds.json', existing, managed);
    const secondResult = mergeXrayFile('03_inbounds.json', firstResult, managed);

    const countDnsIn = (secondResult.inbounds as Record<string, unknown>[]).filter((ib) =>
      String(ib.tag || '').startsWith('dns-in-')
    ).length;

    expect(countDnsIn).toBe(1);
  });
});

// ---------------------------------------------------------------------------
// D-11: PROXY_TAG заменяется на выбранный outbound тег
// ---------------------------------------------------------------------------
describe('mergeXrayFile (D-11) — замена PROXY_TAG', () => {
  test('правило с outboundTag PROXY_TAG заменяется на реальный тег при merge', () => {
    const existing = {
      routing: {
        domainStrategy: 'IPIfNonMatch',
        rules: [
          { type: 'field', network: 'tcp,udp', outboundTag: 'PROXY_TAG' },
          { type: 'field', ip: ['geoip:private'], outboundTag: 'direct' }
        ]
      }
    };

    const managed = {
      rules: [
        { type: 'field', network: 'tcp,udp', outboundTag: 'PROXY_TAG' },
        { type: 'field', ip: ['geoip:private'], outboundTag: 'direct' }
      ],
      proxyTag: 'my-vless-proxy'
    };

    const result = mergeXrayFile('05_routing.json', existing, managed);
    const rules = (result.routing as Record<string, unknown>)?.rules as Record<string, unknown>[];

    // Ни одно правило не должно содержать PROXY_TAG после merge
    const proxyTagRule = rules.find((r) => r.outboundTag === 'PROXY_TAG');
    expect(proxyTagRule).toBeUndefined();

    // Правило с network tcp,udp должно использовать реальный тег
    const networkRule = rules.find((r) => r.network === 'tcp,udp');
    expect(networkRule?.outboundTag).toBe('my-vless-proxy');
  });
});

// ---------------------------------------------------------------------------
// Идемпотентность: третий merge равен второму
// ---------------------------------------------------------------------------
describe('mergeXrayFile — идемпотентность', () => {
  test('двойное применение merge 02_dns.json даёт одинаковый результат', () => {
    const existing = {
      dns: {
        tag: 'dns-in',
        queryStrategy: 'UseIPv4',
        servers: ['1.1.1.1'],
        hosts: { 'dns.google': '8.8.8.8' }
      }
    };

    const managed = {
      servers: [
        {
          address: '8.8.8.8',
          port: 53,
          tag: 'dns-in-proxy',
          domains: ['geosite:geolocation-!cn'],
          skipFallback: true
        }
      ],
      queryStrategy: 'UseIP',
      hosts: { 'dns.google': '8.8.8.8' }
    };

    const second = mergeXrayFile('02_dns.json', existing, managed);
    const third = mergeXrayFile('02_dns.json', second, managed);

    expect(JSON.stringify(third)).toBe(JSON.stringify(second));
  });
});

// ---------------------------------------------------------------------------
// DNS-over-VLESS & tag preservation tests
// ---------------------------------------------------------------------------
describe('mergeXrayFile (DNS-over-VLESS) — DNS-over-VLESS & tag preservation', () => {
  test('merge 02_dns.json сохраняет поле tag', () => {
    const existing = {
      dns: {
        tag: 'dns-in-old',
        servers: ['8.8.8.8']
      }
    };
    const managed = {
      tag: 'dns-in',
      servers: ['1.1.1.1']
    };

    const result = mergeXrayFile('02_dns.json', existing, managed);
    expect((result.dns as Record<string, any>)?.tag).toBe('dns-in');
  });

  test('generateDnsOverVlessRules генерирует правильные правила', () => {
    const rules = generateDnsOverVlessRules('vless-proxy');
    expect(rules).toHaveLength(2);
    expect(rules[0]).toEqual({
      type: 'field',
      inboundTag: ['dns-in'],
      outboundTag: 'vless-proxy'
    });
    expect(rules[1]).toEqual({
      type: 'field',
      port: 53,
      outboundTag: 'dns-out'
    });
  });

  test('merge 04_outbounds.json сохраняет системные и перезаписывает кастомные outbounds', () => {
    const existing = {
      outbounds: [
        { tag: 'direct', protocol: 'freedom' },
        { tag: 'block', protocol: 'blackhole' },
        { tag: 'dns-out', protocol: 'dns' },
        { tag: 'old-custom', protocol: 'vless', settings: { vnext: [{ address: '1.1.1.1' }] } }
      ]
    };
    const managed = {
      outbounds: [
        { tag: 'new-custom', protocol: 'vless', settings: { vnext: [{ address: '2.2.2.2' }] } }
      ]
    };

    const result = mergeXrayFile('04_outbounds.json', existing, managed);
    const outbounds = result.outbounds as any[];

    expect(outbounds).toHaveLength(4);
    expect(outbounds.find(o => o.tag === 'direct')).toBeDefined();
    expect(outbounds.find(o => o.tag === 'block')).toBeDefined();
    expect(outbounds.find(o => o.tag === 'dns-out')).toBeDefined();
    expect(outbounds.find(o => o.tag === 'old-custom')).toBeUndefined();
    expect(outbounds.find(o => o.tag === 'new-custom')).toBeDefined();
  });
});

