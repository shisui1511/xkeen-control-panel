import { writable, get } from 'svelte/store';
import { countryCodeToFlag } from './countryFlags';
import { apiFetchJSON } from './api';

export interface ClientExitIPInfo {
  ip: string;
  countryCode?: string;
  countryName?: string;
  flag?: string;
  city?: string;
  isp?: string;
  asn?: string;
  org?: string;
  routerWanIp?: string;
  isProxied?: boolean;
  status: 'idle' | 'loading' | 'success' | 'error';
  error?: string;
  lastChecked?: number;
}

const INITIAL_STATE: ClientExitIPInfo = {
  ip: '',
  status: 'idle'
};

export const clientExitIpStore = writable<ClientExitIPInfo>(INITIAL_STATE);

const CACHE_TTL_MS = 60000; // 1 minute
let currentFetchPromise: Promise<ClientExitIPInfo> | null = null;

export function getLocalizedCountryName(countryCode: string, lang = 'ru'): string {
  if (!countryCode) return '';
  const code = countryCode.toUpperCase();
  try {
    const dn = new Intl.DisplayNames([lang, 'en'], { type: 'region' });
    return dn.of(code) || code;
  } catch {
    return code;
  }
}

interface RawGeoResult {
  ip: string;
  countryCode?: string;
  city?: string;
  org?: string;
}

async function fetchWithTimeout(url: string, timeoutMs = 4000): Promise<Response> {
  const controller = new AbortController();
  const timer = setTimeout(() => controller.abort(), timeoutMs);
  try {
    // eslint-disable-next-line no-restricted-syntax -- external public echo service request from client browser
    const res = await fetch(url, {
      signal: controller.signal,
      headers: { Accept: 'application/json' }
    });
    return res;
  } finally {
    clearTimeout(timer);
  }
}

async function queryPublicEcho(): Promise<RawGeoResult> {
  // Service 1: ipinfo.io
  try {
    const res = await fetchWithTimeout('https://ipinfo.io/json');
    if (res.ok) {
      const data = await res.json();
      if (data && typeof data.ip === 'string' && data.ip.trim()) {
        return {
          ip: data.ip.trim(),
          countryCode: typeof data.country === 'string' ? data.country.trim() : undefined,
          city: typeof data.city === 'string' ? data.city.trim() : undefined,
          org: typeof data.org === 'string' ? data.org.trim() : undefined
        };
      }
    }
  } catch {
    // try next service
  }

  // Service 2: api.ipify.org
  try {
    const res = await fetchWithTimeout('https://api.ipify.org?format=json');
    if (res.ok) {
      const data = await res.json();
      if (data && typeof data.ip === 'string' && data.ip.trim()) {
        const ip = data.ip.trim();
        // Try secondary geo enrichment for this IP
        const enriched = await tryEnrichGeo(ip);
        return { ip, ...enriched };
      }
    }
  } catch {
    // try next service
  }

  // Service 3: ipapi.co
  try {
    const res = await fetchWithTimeout('https://ipapi.co/json/');
    if (res.ok) {
      const data = await res.json();
      if (data && typeof data.ip === 'string' && data.ip.trim()) {
        return {
          ip: data.ip.trim(),
          countryCode: typeof data.country_code === 'string' ? data.country_code.trim() : undefined,
          city: typeof data.city === 'string' ? data.city.trim() : undefined,
          org: typeof data.org === 'string' ? data.org.trim() : undefined
        };
      }
    }
  } catch {
    // try next service
  }

  // Service 4: icanhazip.com (fallback plain text)
  try {
    const res = await fetchWithTimeout('https://icanhazip.com');
    if (res.ok) {
      const text = await res.text();
      const ip = text.trim();
      if (ip && /^[\d.:a-fA-F]+$/.test(ip)) {
        const enriched = await tryEnrichGeo(ip);
        return { ip, ...enriched };
      }
    }
  } catch {
    // all failed
  }

  throw new Error('Unable to detect public IP address');
}

async function tryEnrichGeo(
  ip: string
): Promise<{ countryCode?: string; city?: string; org?: string }> {
  try {
    const res = await fetchWithTimeout(`https://ipinfo.io/${encodeURIComponent(ip)}/json`, 3000);
    if (res.ok) {
      const data = await res.json();
      return {
        countryCode: typeof data.country === 'string' ? data.country.trim() : undefined,
        city: typeof data.city === 'string' ? data.city.trim() : undefined,
        org: typeof data.org === 'string' ? data.org.trim() : undefined
      };
    }
  } catch {
    // enrichment is non-fatal
  }
  return {};
}

async function fetchRouterWanIp(): Promise<string | undefined> {
  try {
    const data = await apiFetchJSON<{ success: boolean; ip?: string }>('/api/network/ip');
    if (data && data.success && typeof data.ip === 'string' && data.ip.trim()) {
      return data.ip.trim();
    }
  } catch {
    // Router WAN IP fetch failure is non-fatal
  }
  return undefined;
}

export function parseAsnAndIsp(org?: string): { asn?: string; isp?: string } {
  if (!org) return {};
  const trimmed = org.trim();
  const m = trimmed.match(/^(AS\d+)\s*(.*)$/i);
  if (m) {
    return {
      asn: m[1].toUpperCase(),
      isp: m[2] ? m[2].trim() : m[1].toUpperCase()
    };
  }
  return { isp: trimmed };
}

export async function fetchClientExitIP(force = false, lang = 'ru'): Promise<ClientExitIPInfo> {
  const current = get(clientExitIpStore);
  const now = Date.now();

  if (
    !force &&
    current.status === 'success' &&
    current.lastChecked &&
    now - current.lastChecked < CACHE_TTL_MS
  ) {
    return current;
  }

  if (currentFetchPromise) {
    return currentFetchPromise;
  }

  clientExitIpStore.update((s) => ({
    ...s,
    status: 'loading',
    error: undefined
  }));

  currentFetchPromise = (async () => {
    try {
      // Parallel fetch: Client public IP (browser) + Router WAN IP (backend)
      const [clientResult, routerWanIp] = await Promise.all([
        queryPublicEcho(),
        fetchRouterWanIp()
      ]);

      const countryCode = clientResult.countryCode
        ? clientResult.countryCode.toUpperCase()
        : undefined;
      const flag = countryCode ? countryCodeToFlag(countryCode) : undefined;
      const countryName = countryCode ? getLocalizedCountryName(countryCode, lang) : undefined;
      const { asn, isp } = parseAsnAndIsp(clientResult.org);

      let isProxied: boolean | undefined = undefined;
      if (clientResult.ip && routerWanIp) {
        isProxied = clientResult.ip.trim() !== routerWanIp.trim();
      }

      const updatedInfo: ClientExitIPInfo = {
        ip: clientResult.ip,
        countryCode,
        countryName,
        flag,
        city: clientResult.city,
        isp,
        asn,
        org: clientResult.org,
        routerWanIp,
        isProxied,
        status: 'success',
        lastChecked: Date.now()
      };

      clientExitIpStore.set(updatedInfo);
      return updatedInfo;
    } catch (err: any) {
      const errorMsg = err?.message || 'Failed to detect IP';
      const errorInfo: ClientExitIPInfo = {
        ...get(clientExitIpStore),
        status: 'error',
        error: errorMsg
      };
      clientExitIpStore.set(errorInfo);
      return errorInfo;
    } finally {
      currentFetchPromise = null;
    }
  })();

  return currentFetchPromise;
}

export function resetClientExitIpStore(): void {
  currentFetchPromise = null;
  clientExitIpStore.set(INITIAL_STATE);
}
