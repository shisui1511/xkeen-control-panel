import { writable, get } from 'svelte/store';
import { apiFetchJSON } from './lib/api';
import { isServiceRestarting, clearRestartGrace } from './lib/serviceGrace';

// --- Capabilities store ---

export interface KernelCapability {
  installed: boolean;
  version?: string;
  channel?: string;
}

export interface CapabilitiesData {
  kernels: Record<string, KernelCapability>;
  active_kernel: string;
  xkeen_dns?: boolean;
  mihomo: {
    reachable: boolean;
    process_running: boolean;
    api_reachable: boolean;
    api_authenticated: boolean;
    api_url?: string;
    discovered_secret?: string;
    controller_type?: string;
    controller_target?: string;
    is_insecure_lan?: boolean;
  };
  xray?: {
    conf_dir: string;
    conf_dir_exists: boolean;
  };
  global_hwid?: string;
}

export const capabilities = writable<CapabilitiesData | null>(null);
export const isKernelChecking = writable(false);

// --- Mihomo API availability store ---
// Updated by fetchCapabilities on every poll cycle (10 s interval).
// Sidebar reads this store reactively to show/hide the badge on Proxy/Rules/Connections nav items.
export const mihomoApiAvailable = writable<boolean>(false);

let lastValidActiveKernel = '';
let consecutiveCapabilitiesFailures = 0;

export async function fetchCapabilities(signal?: AbortSignal): Promise<void> {
  try {
    const data = await apiFetchJSON<CapabilitiesData>('/api/capabilities', { signal });

    consecutiveCapabilitiesFailures = 0;
    if (get(isServiceRestarting) && data.mihomo?.api_reachable) {
      clearRestartGrace();
    }

    if (data.active_kernel) {
      lastValidActiveKernel = data.active_kernel;
    } else if (lastValidActiveKernel) {
      data.active_kernel = lastValidActiveKernel;
    }

    if (get(isKernelChecking)) {
      capabilities.update((current) => {
        if (current) {
          return {
            ...data,
            active_kernel: current.active_kernel
          };
        }
        return data;
      });
    } else {
      capabilities.set(data);
    }

    // Update Mihomo API availability store unconditionally on every successful fetch.
    // Sidebar and Dashboard checklist both subscribe to this store reactively (D-12, D-13).
    mihomoApiAvailable.set(data.mihomo?.api_reachable ?? false);
  } catch (e: any) {
    // Cancelled poll: no state change.
    if (e?.name === 'AbortError') return;
    // Session expired: apiFetch already handled logout+toast+redirect centrally,
    // wiping capabilities state here would just flash stale UI before the
    // navigation completes.
    if (e?.status === 401) return;

    consecutiveCapabilitiesFailures++;
    // Debounce network blips: during active service restart or for a single intermittent failure,
    // do not instantly set mihomoApiAvailable to false to avoid UI flickering.
    if (!get(isServiceRestarting) && consecutiveCapabilitiesFailures >= 2) {
      mihomoApiAvailable.set(false);
    }
  }
}

// UI state: controls whether the off-canvas sidebar is visible on mobile
export const isSidebarOpen = writable(false);

// UI state: desktop icon-rail sidebar collapse (persistent, NOT the mobile
// off-canvas drawer above — isSidebarOpen and isSidebarCollapsed are separate
// mechanisms and must not be merged, D-12/D-13/D-14).
function readInitialCollapsed(): boolean {
  try {
    const saved = localStorage.getItem('sidebar_collapsed');
    return saved === 'true';
  } catch (e) {
    // localStorage unavailable or corrupted — fail-closed to expanded (false)
    return false;
  }
}

export const isSidebarCollapsed = writable<boolean>(readInitialCollapsed());

isSidebarCollapsed.subscribe((v) => {
  try {
    localStorage.setItem('sidebar_collapsed', String(v));
  } catch (e) {
    // localStorage may be unavailable
  }
});

// --- Toast store ---

export interface ToastAction {
  label: string;
  onClick: () => void;
}

export interface ToastItem {
  id: number;
  type: 'success' | 'error' | 'info' | 'warning';
  message: string;
  duration?: number;
  action?: ToastAction;
}

export const toastStore = writable<ToastItem[]>([]);

let _toastCounter = 0;

export function showToast(
  type: ToastItem['type'],
  message: string,
  duration = 4000,
  action?: ToastAction
): void {
  const id = ++_toastCounter;
  toastStore.update((items) => [...items, { id, type, message, duration, action }]);
  if (duration > 0) {
    setTimeout(() => {
      toastStore.update((items) => items.filter((t) => t.id !== id));
    }, duration);
  }
}

// --- ConfirmDialog store ---

export interface ConfirmRequest {
  title: string;
  message?: string;
  objectName?: string;
  consequence?: string;
  confirmLabel?: string;
  cancelLabel?: string;
  variant?: 'danger' | 'warning' | 'primary';
  resolve: (value: boolean) => void;
}

export const confirmStore = writable<ConfirmRequest | null>(null);

export function showConfirm(
  titleOrOptions: string | Omit<ConfirmRequest, 'resolve'>,
  message?: string,
  confirmLabel?: string,
  cancelLabel?: string
): Promise<boolean> {
  return new Promise((resolve) => {
    if (typeof titleOrOptions === 'object' && titleOrOptions !== null) {
      confirmStore.set({
        variant: 'danger',
        ...titleOrOptions,
        resolve
      });
    } else {
      confirmStore.set({
        title: String(titleOrOptions),
        message,
        confirmLabel,
        cancelLabel,
        variant: 'danger',
        resolve
      });
    }
  });
}

// --- Dev mode store ---

export const devMode = writable(false);

export async function fetchDevMode(): Promise<void> {
  try {
    const data = await apiFetchJSON<{ dev_mode: boolean }>('/api/settings');
    devMode.set(data.dev_mode ?? false);
  } catch (_) {
    // ignore
  }
}

export async function setDevMode(enabled: boolean): Promise<void> {
  try {
    await apiFetchJSON('/api/settings/dev-mode', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ enabled })
    });
    devMode.set(enabled);
  } catch (e) {
    devMode.set(!enabled);
    showToast('error', e instanceof Error ? e.message : String(e));
  }
}

// --- Theme density store (DENS-01) ---

export type ThemeDensity = 'auto' | 'comfortable' | 'compact';

export function resolveDensityAttr(mode?: ThemeDensity): 'comfortable' | 'compact' {
  let densityMode = mode;
  if (!densityMode) {
    try {
      const saved = localStorage.getItem('theme_density');
      if (saved === 'comfortable' || saved === 'compact') {
        densityMode = saved;
      }
    } catch (_) {}
  }
  if (densityMode === 'compact' || densityMode === 'comfortable') {
    return densityMode;
  }
  const isMobile =
    typeof window !== 'undefined' &&
    typeof window.matchMedia === 'function' &&
    window.matchMedia('(max-width: 1024px)').matches;
  return isMobile ? 'compact' : 'comfortable';
}

function readInitialDensity(): ThemeDensity {
  try {
    const saved = localStorage.getItem('theme_density');
    if (saved === 'comfortable' || saved === 'compact') {
      return saved;
    }
  } catch (e) {
    // localStorage unavailable
  }
  return 'auto';
}

export const themeDensity = writable<ThemeDensity>(readInitialDensity());

export function applyDensity(mode: ThemeDensity): void {
  try {
    if (mode === 'auto') {
      localStorage.removeItem('theme_density');
    } else {
      localStorage.setItem('theme_density', mode);
    }
    if (typeof document !== 'undefined') {
      document.documentElement.setAttribute('data-density', resolveDensityAttr(mode));
    }
  } catch (e) {
    // localStorage or DOM unavailable
  }
  themeDensity.set(mode);
}

let densityMql: MediaQueryList | null = null;
let handleDensityMediaChange: ((e: MediaQueryListEvent) => void) | null = null;

export function initDensity(): () => void {
  if (typeof document !== 'undefined') {
    document.documentElement.setAttribute('data-density', resolveDensityAttr());
  }
  if (typeof window !== 'undefined' && typeof window.matchMedia === 'function' && !densityMql) {
    densityMql = window.matchMedia('(max-width: 1024px)');
    handleDensityMediaChange = (e: MediaQueryListEvent) => {
      if (get(themeDensity) === 'auto' && typeof document !== 'undefined') {
        document.documentElement.setAttribute(
          'data-density',
          e.matches ? 'compact' : 'comfortable'
        );
      }
    };
    densityMql.addEventListener('change', handleDensityMediaChange);
  }
  return () => {
    if (densityMql && handleDensityMediaChange) {
      densityMql.removeEventListener('change', handleDensityMediaChange);
      densityMql = null;
      handleDensityMediaChange = null;
    }
  };
}
