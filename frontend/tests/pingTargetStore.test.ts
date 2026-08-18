import { describe, it, expect, beforeEach } from 'vitest';
import { get } from 'svelte/store';
import {
  pingTargetStore,
  setPingTargetConfig,
  getTargetUrl,
  isValidUrl,
  DEFAULT_PING_TARGET_CONFIG,
  PING_PRESET_URLS
} from '../src/lib/pingTargetStore';

describe('pingTargetStore', () => {
  let mockStorage: Record<string, string> = {};

  beforeEach(() => {
    mockStorage = {};
    (global as any).window = {
      localStorage: {
        getItem: (key: string) => mockStorage[key] || null,
        setItem: (key: string, value: string) => {
          mockStorage[key] = value;
        },
        removeItem: (key: string) => {
          delete mockStorage[key];
        },
        clear: () => {
          mockStorage = {};
        }
      }
    };
    (global as any).localStorage = (global as any).window.localStorage;
    setPingTargetConfig({ ...DEFAULT_PING_TARGET_CONFIG });
  });

  it('validates URLs correctly', () => {
    expect(isValidUrl('http://example.com/204')).toBe(true);
    expect(isValidUrl('https://cp.cloudflare.com/generate_204')).toBe(true);
    expect(isValidUrl('ftp://invalid.com')).toBe(false);
    expect(isValidUrl('not a url')).toBe(false);
    expect(isValidUrl('')).toBe(false);
  });

  it('returns appropriate target URL based on preset', () => {
    expect(getTargetUrl({ preset: 'google', customUrl: '', timeoutMs: 5000 })).toBe(
      PING_PRESET_URLS.google
    );
    expect(getTargetUrl({ preset: 'cloudflare', customUrl: '', timeoutMs: 5000 })).toBe(
      PING_PRESET_URLS.cloudflare
    );
    expect(getTargetUrl({ preset: 'yandex', customUrl: '', timeoutMs: 5000 })).toBe(
      PING_PRESET_URLS.yandex
    );
    expect(
      getTargetUrl({
        preset: 'custom',
        customUrl: 'https://my-ping-server.org/check',
        timeoutMs: 5000
      })
    ).toBe('https://my-ping-server.org/check');
    // Fallback to Google 204 if custom URL is invalid
    expect(
      getTargetUrl({
        preset: 'custom',
        customUrl: 'invalid-url',
        timeoutMs: 5000
      })
    ).toBe(PING_PRESET_URLS.google);
  });

  it('updates configuration and persists to localStorage', () => {
    setPingTargetConfig({ preset: 'cloudflare', timeoutMs: 2000 });
    const current = get(pingTargetStore);
    expect(current.preset).toBe('cloudflare');
    expect(current.timeoutMs).toBe(2000);

    expect(mockStorage['xcp_ping_target_config']).toBeDefined();
    const saved = JSON.parse(mockStorage['xcp_ping_target_config']);
    expect(saved.preset).toBe('cloudflare');
    expect(saved.timeoutMs).toBe(2000);
  });
});
