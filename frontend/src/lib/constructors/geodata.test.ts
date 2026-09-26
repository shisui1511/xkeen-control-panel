import { describe, it, expect } from 'vitest';
import {
  PRIVATE_CIDRS,
  adaptDnsServers,
  adaptGeoValues,
  adaptPresetRules,
  geoAvailability
} from './geodata';

describe('geodata: ссылки пресетов под установленные базы', () => {
  const none = geoAvailability([]);
  const v2fly = geoAvailability(['geosite_v2fly.dat', 'geoip_v2fly.dat', 'geosite_zkeen.dat']);
  const std = geoAvailability(['geosite.dat', 'geoip.dat', 'geosite_v2fly.dat']);

  it('выбирает стандартный .dat, иначе v2fly, иначе ничего', () => {
    expect(std).toEqual({ geosite: '', geoip: '' });
    expect(v2fly).toEqual({ geosite: 'geosite_v2fly.dat', geoip: 'geoip_v2fly.dat' });
    expect(none).toEqual({ geosite: null, geoip: null });
  });

  it('geoip:private — всегда частные сети, без geoip.dat', () => {
    for (const avail of [none, v2fly, std]) {
      expect(adaptGeoValues(['geoip:private'], avail)).toEqual(PRIVATE_CIDRS);
    }
  });

  it('geosite:/geoip: — как есть, через ext: или отбрасываются', () => {
    const vals = ['geosite:category-ads-all', 'geoip:ru', 'example.com', 'ext:zkeen.dat:x'];
    expect(adaptGeoValues(vals, std)).toEqual(vals);
    expect(adaptGeoValues(vals, v2fly)).toEqual([
      'ext:geosite_v2fly.dat:category-ads-all',
      'ext:geoip_v2fly.dat:ru',
      'example.com',
      'ext:zkeen.dat:x'
    ]);
    expect(adaptGeoValues(vals, none)).toEqual(['example.com', 'ext:zkeen.dat:x']);
  });

  it('правило без условий после адаптации убирается', () => {
    const rules = [
      { type: 'field', outboundTag: 'direct', ip: ['geoip:private'] },
      { type: 'field', outboundTag: 'block', domain: ['geosite:category-ads-all'] },
      { type: 'field', outboundTag: 'PROXY', network: 'tcp,udp' }
    ];
    const out = adaptPresetRules(rules, none);
    expect(out.map((r) => r.outboundTag)).toEqual(['direct', 'PROXY']);
    expect(out[0].ip).toEqual(PRIVATE_CIDRS);
    expect(adaptPresetRules(rules, v2fly)[1].domain).toEqual([
      'ext:geosite_v2fly.dat:category-ads-all'
    ]);
  });

  it('DNS-сервер для доменов без баз убирается, общий остаётся', () => {
    const servers = [
      '1.1.1.1',
      { address: '77.88.8.8', port: 53, domains: ['geosite:tld-ru'] },
      { address: '8.8.8.8', expectIPs: ['geoip:ru'] }
    ];
    expect(adaptDnsServers(servers, none)).toEqual(['1.1.1.1', { address: '8.8.8.8' }]);
    expect(adaptDnsServers(servers, v2fly)[1]).toEqual({
      address: '77.88.8.8',
      port: 53,
      domains: ['ext:geosite_v2fly.dat:tld-ru']
    });
  });
});
