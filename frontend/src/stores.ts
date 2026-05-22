import { writable } from 'svelte/store';

// --- Capabilities store ---

export interface KernelCapability {
  installed: boolean;
  version?: string;
  channel?: string;
}

export interface CapabilitiesData {
  kernels: Record<string, KernelCapability>;
  mihomo: {
    reachable: boolean;
    process_running: boolean;
    api_reachable: boolean;
    api_authenticated: boolean;
    discovered_secret?: string;
  };
}

export const capabilities = writable<CapabilitiesData | null>(null);

export async function fetchCapabilities(): Promise<void> {
  try {
    const res = await fetch('/api/capabilities');
    if (res.ok) {
      const envelope = await res.json();
      // Capabilities uses JSONSuccess envelope: {success, data: {...}}
      const data: CapabilitiesData = envelope.data ?? envelope;
      capabilities.set(data);
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
  type: 'success' | 'error' | 'info';
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
