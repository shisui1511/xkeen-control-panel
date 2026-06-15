import { writable, get } from 'svelte/store';

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
  };
  xray?: {
    conf_dir: string;
    conf_dir_exists: boolean;
  };
}

export const capabilities = writable<CapabilitiesData | null>(null);
export const isKernelChecking = writable(false);

// --- Mihomo API availability store ---
// Updated by fetchCapabilities on every poll cycle (10 s interval).
// Sidebar reads this store reactively to show/hide the badge on Proxy/Rules/Connections nav items.
export const mihomoApiAvailable = writable<boolean>(false);

let lastValidActiveKernel = '';

export async function fetchCapabilities(): Promise<void> {
  try {
    const res = await fetch('/api/capabilities');
    if (res.ok) {
      const envelope = await res.json();
      // Capabilities uses JSONSuccess envelope: {success, data: {...}}
      const data: CapabilitiesData = envelope.data ?? envelope;

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
    }
  } catch (_) {
    // Silently ignore — capabilities will remain null
  }
}

// UI state: controls whether the off-canvas sidebar is visible on mobile
export const isSidebarOpen = writable(false);

// --- Toast store ---

export interface ToastItem {
  id: number;
  type: 'success' | 'error' | 'info' | 'warning';
  message: string;
  duration?: number;
}

export const toastStore = writable<ToastItem[]>([]);

let _toastCounter = 0;

export function showToast(type: ToastItem['type'], message: string, duration = 4000): void {
  const id = ++_toastCounter;
  toastStore.update((items) => [...items, { id, type, message, duration }]);
  setTimeout(() => {
    toastStore.update((items) => items.filter((t) => t.id !== id));
  }, duration);
}

// --- ConfirmDialog store ---

export interface ConfirmRequest {
  title: string;
  message: string;
  confirmLabel: string;
  cancelLabel: string;
  resolve: (value: boolean) => void;
}

export const confirmStore = writable<ConfirmRequest | null>(null);

export function showConfirm(
  title: string,
  message: string,
  confirmLabel = 'OK',
  cancelLabel = 'Cancel'
): Promise<boolean> {
  return new Promise((resolve) => {
    confirmStore.set({ title, message, confirmLabel, cancelLabel, resolve });
  });
}

// --- Dev mode store ---

export const devMode = writable(false);

export async function fetchDevMode(): Promise<void> {
  try {
    const res = await fetch('/api/settings');
    if (res.ok) {
      const envelope = await res.json();
      const data = envelope.data ?? envelope;
      devMode.set(data.dev_mode ?? false);
    }
  } catch (_) {
    // ignore
  }
}

export async function setDevMode(enabled: boolean): Promise<void> {
  try {
    const csrfToken = localStorage.getItem('csrf_token');
    const res = await fetch('/api/settings/dev-mode', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrfToken || '' },
      body: JSON.stringify({ enabled })
    });
    if (res.ok) {
      devMode.set(enabled);
    } else {
      devMode.set(!enabled);
      showToast('error', await res.text());
    }
  } catch (e) {
    devMode.set(!enabled);
    showToast('error', e instanceof Error ? e.message : String(e));
  }
}
