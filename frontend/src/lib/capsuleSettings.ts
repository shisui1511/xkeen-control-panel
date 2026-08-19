import { writable, type Writable } from 'svelte/store';

export interface CapsuleConfig {
  visible: boolean;
  showTraffic: boolean;
  showResources: boolean;
}

export const DEFAULT_CAPSULE_CONFIG: CapsuleConfig = {
  visible: true,
  showTraffic: true,
  showResources: true
};

const STORAGE_KEY = 'xcp_capsule_config';

function loadStoredConfig(): CapsuleConfig {
  if (typeof window === 'undefined' || !window.localStorage) {
    return { ...DEFAULT_CAPSULE_CONFIG };
  }

  try {
    const raw = localStorage.getItem(STORAGE_KEY);
    if (!raw) return { ...DEFAULT_CAPSULE_CONFIG };

    const parsed = JSON.parse(raw);
    if (typeof parsed !== 'object' || parsed === null) {
      return { ...DEFAULT_CAPSULE_CONFIG };
    }

    return {
      visible:
        typeof parsed.visible === 'boolean' ? parsed.visible : DEFAULT_CAPSULE_CONFIG.visible,
      showTraffic:
        typeof parsed.showTraffic === 'boolean'
          ? parsed.showTraffic
          : DEFAULT_CAPSULE_CONFIG.showTraffic,
      showResources:
        typeof parsed.showResources === 'boolean'
          ? parsed.showResources
          : DEFAULT_CAPSULE_CONFIG.showResources
    };
  } catch {
    return { ...DEFAULT_CAPSULE_CONFIG };
  }
}

function saveConfig(config: CapsuleConfig): void {
  if (typeof window === 'undefined' || !window.localStorage) return;
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(config));
  } catch {
    // Storage quota or private browsing restriction
  }
}

export const capsuleConfigStore: Writable<CapsuleConfig> =
  writable<CapsuleConfig>(loadStoredConfig());

// Sync changes to localStorage automatically
capsuleConfigStore.subscribe((config) => {
  saveConfig(config);
});

export function updateCapsuleConfig(patch: Partial<CapsuleConfig>): void {
  capsuleConfigStore.update((current) => {
    const updated = {
      ...current,
      ...patch
    };
    return updated;
  });
}

export function resetCapsuleConfig(): void {
  capsuleConfigStore.set({ ...DEFAULT_CAPSULE_CONFIG });
}
