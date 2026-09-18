import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { get } from 'svelte/store';
import {
  clientExitIpStore,
  fetchClientExitIP,
  parseAsnAndIsp,
  getLocalizedCountryName,
  resetClientExitIpStore
} from './clientIp';

describe('clientIp module', () => {
  beforeEach(() => {
    resetClientExitIpStore();
    vi.restoreAllMocks();
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  describe('parseAsnAndIsp', () => {
    it('parses standard AS org strings', () => {
      const res = parseAsnAndIsp('AS16276 OVH SAS');
      expect(res.asn).toBe('AS16276');
      expect(res.isp).toBe('OVH SAS');
    });

    it('handles org string without ASN', () => {
      const res = parseAsnAndIsp('Google LLC');
      expect(res.asn).toBeUndefined();
      expect(res.isp).toBe('Google LLC');
    });

    it('handles undefined or empty string', () => {
      expect(parseAsnAndIsp(undefined)).toEqual({});
      expect(parseAsnAndIsp('')).toEqual({});
    });
  });

  describe('getLocalizedCountryName', () => {
    it('returns country name in Russian', () => {
      const de = getLocalizedCountryName('DE', 'ru');
      expect(de).toBe('Германия');
      const nl = getLocalizedCountryName('NL', 'ru');
      expect(nl).toBe('Нидерланды');
    });

    it('returns country name in English', () => {
      const de = getLocalizedCountryName('DE', 'en');
      expect(de).toBe('Germany');
    });

    it('handles empty code gracefully', () => {
      expect(getLocalizedCountryName('')).toBe('');
    });
  });

  describe('fetchClientExitIP', () => {
    it('successfully detects client exit IP from ipinfo and determines proxy routing', async () => {
      const globalFetch = vi.fn().mockImplementation((url: string) => {
        if (url.includes('/api/network/ip')) {
          return Promise.resolve({
            ok: true,
            status: 200,
            json: () => Promise.resolve({ success: true, ip: '95.100.100.1' })
          });
        }
        if (url.includes('ipinfo.io/json')) {
          return Promise.resolve({
            ok: true,
            status: 200,
            json: () =>
              Promise.resolve({
                ip: '185.220.101.5',
                country: 'NL',
                city: 'Amsterdam',
                org: 'AS60729 TorExit'
              })
          });
        }
        return Promise.reject(new Error('Unexpected URL: ' + url));
      });

      vi.stubGlobal('fetch', globalFetch);

      const result = await fetchClientExitIP(true, 'ru');

      expect(result.status).toBe('success');
      expect(result.ip).toBe('185.220.101.5');
      expect(result.countryCode).toBe('NL');
      expect(result.countryName).toBe('Нидерланды');
      expect(result.flag).toBe('🇳🇱');
      expect(result.city).toBe('Amsterdam');
      expect(result.asn).toBe('AS60729');
      expect(result.isp).toBe('TorExit');
      expect(result.routerWanIp).toBe('95.100.100.1');
      expect(result.isProxied).toBe(true);

      const storeVal = get(clientExitIpStore);
      expect(storeVal.ip).toBe('185.220.101.5');
      expect(storeVal.isProxied).toBe(true);
    });

    it('detects direct connection when client IP matches router WAN IP', async () => {
      const globalFetch = vi.fn().mockImplementation((url: string) => {
        if (url.includes('/api/network/ip')) {
          return Promise.resolve({
            ok: true,
            status: 200,
            json: () => Promise.resolve({ success: true, ip: '95.100.100.1' })
          });
        }
        if (url.includes('ipinfo.io/json')) {
          return Promise.resolve({
            ok: true,
            status: 200,
            json: () =>
              Promise.resolve({
                ip: '95.100.100.1',
                country: 'RU',
                city: 'Moscow',
                org: 'AS12389 Rostelecom'
              })
          });
        }
        return Promise.reject(new Error('Unexpected URL: ' + url));
      });

      vi.stubGlobal('fetch', globalFetch);

      const result = await fetchClientExitIP(true, 'ru');

      expect(result.status).toBe('success');
      expect(result.ip).toBe('95.100.100.1');
      expect(result.routerWanIp).toBe('95.100.100.1');
      expect(result.isProxied).toBe(false);
    });

    it('falls back to api.ipify.org when ipinfo fails', async () => {
      const globalFetch = vi.fn().mockImplementation((url: string) => {
        const parsed = new URL(url, 'http://localhost');
        if (parsed.pathname === '/api/network/ip') {
          return Promise.resolve({
            ok: true,
            status: 200,
            json: () => Promise.resolve({ success: true, ip: '95.100.100.1' })
          });
        }
        if (parsed.hostname === 'ipinfo.io') {
          return Promise.reject(new Error('Network error on ipinfo'));
        }
        if (parsed.hostname === 'api.ipify.org') {
          return Promise.resolve({
            ok: true,
            status: 200,
            json: () => Promise.resolve({ ip: '1.2.3.4' })
          });
        }
        return Promise.reject(new Error('Unexpected URL: ' + url));
      });

      vi.stubGlobal('fetch', globalFetch);

      const result = await fetchClientExitIP(true, 'ru');

      expect(result.status).toBe('success');
      expect(result.ip).toBe('1.2.3.4');
      expect(result.isProxied).toBe(true);
    });

    it('caches successful result within TTL unless force is true', async () => {
      let callCount = 0;
      const globalFetch = vi.fn().mockImplementation((url: string) => {
        callCount++;
        if (url.includes('/api/network/ip')) {
          return Promise.resolve({
            ok: true,
            status: 200,
            json: () => Promise.resolve({ success: true, ip: '95.100.100.1' })
          });
        }
        return Promise.resolve({
          ok: true,
          status: 200,
          json: () =>
            Promise.resolve({
              ip: '185.220.101.5',
              country: 'NL'
            })
        });
      });

      vi.stubGlobal('fetch', globalFetch);

      await fetchClientExitIP(false);
      const firstCalls = callCount;

      // Second call without force should return cached data without extra fetches
      const cached = await fetchClientExitIP(false);
      expect(callCount).toBe(firstCalls);
      expect(cached.ip).toBe('185.220.101.5');

      // Third call with force=true should trigger new fetch
      await fetchClientExitIP(true);
      expect(callCount).toBeGreaterThan(firstCalls);
    });

    it('sets error status when all echo services fail', async () => {
      const globalFetch = vi.fn().mockRejectedValue(new Error('Offline'));
      vi.stubGlobal('fetch', globalFetch);

      const result = await fetchClientExitIP(true);

      expect(result.status).toBe('error');
      expect(result.error).toBeDefined();
    });
  });
});
