import { writable, get, type Writable } from 'svelte/store';

export type PingPreset = 'google' | 'cloudflare' | 'yandex' | 'custom';

export interface PingTargetConfig {
  preset: PingPreset;
  customUrl: string;
  timeoutMs: number;
}

export const PING_PRESET_URLS: Record<Exclude<PingPreset, 'custom'>, string> = {
  google: 'http://www.gstatic.com/generate_204',
  cloudflare: 'https://cp.cloudflare.com/generate_204',
  yandex: 'https://yandex.ru/generate_204'
};

export const DEFAULT_PING_TARGET_CONFIG: PingTargetConfig = {
  preset: 'google',
  customUrl: '',
  timeoutMs: 5000
};

export const ALLOWED_TIMEOUTS = [2000, 5000, 8000, 10000] as const;

const STORAGE_KEY = 'xcp_ping_target_config';

export function isValidUrl(urlStr: string): boolean {
  if (!urlStr || typeof urlStr !== 'string') return false;
  const trimmed = urlStr.trim();
  try {
    const parsed = new URL(trimmed);
    return parsed.protocol === 'http:' || parsed.protocol === 'https:';
  } catch {
    return false;
  }
}

export function getTargetUrl(config: PingTargetConfig): string {
  if (config.preset === 'custom') {
    const trimmed = (config.customUrl || '').trim();
    if (isValidUrl(trimmed)) {
      return trimmed;
    }
    return PING_PRESET_URLS.google;
  }
  return PING_PRESET_URLS[config.preset] || PING_PRESET_URLS.google;
}

function loadStoredConfig(): PingTargetConfig {
  if (typeof window === 'undefined' || !window.localStorage) {
    return { ...DEFAULT_PING_TARGET_CONFIG };
  }

  try {
    const raw = localStorage.getItem(STORAGE_KEY);
    if (!raw) return { ...DEFAULT_PING_TARGET_CONFIG };

    const parsed = JSON.parse(raw);
    if (typeof parsed !== 'object' || parsed === null) {
      return { ...DEFAULT_PING_TARGET_CONFIG };
    }

    const preset: PingPreset =
      parsed.preset === 'google' ||
      parsed.preset === 'cloudflare' ||
      parsed.preset === 'yandex' ||
      parsed.preset === 'custom'
        ? parsed.preset
        : DEFAULT_PING_TARGET_CONFIG.preset;

    const customUrl = typeof parsed.customUrl === 'string' ? parsed.customUrl : '';
    const timeoutMs =
      typeof parsed.timeoutMs === 'number' && [2000, 5000, 8000, 10000].includes(parsed.timeoutMs)
        ? parsed.timeoutMs
        : DEFAULT_PING_TARGET_CONFIG.timeoutMs;

    return {
      preset,
      customUrl,
      timeoutMs
    };
  } catch {
    return { ...DEFAULT_PING_TARGET_CONFIG };
  }
}

function saveConfig(config: PingTargetConfig): void {
  if (typeof window === 'undefined' || !window.localStorage) return;
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(config));
  } catch {
    // Storage quota or private browsing restriction
  }
}

export const pingTargetStore: Writable<PingTargetConfig> =
  writable<PingTargetConfig>(loadStoredConfig());

pingTargetStore.subscribe((config) => {
  saveConfig(config);
});

export function setPingTargetConfig(patch: Partial<PingTargetConfig>): void {
  pingTargetStore.update((current) => {
    return {
      ...current,
      ...patch
    };
  });
}

export function getCurrentPingConfig(): PingTargetConfig {
  return get(pingTargetStore);
}
