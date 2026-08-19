import { describe, it, expect, beforeEach, afterEach } from 'vitest';
import { get } from 'svelte/store';
import {
  capsuleConfigStore,
  updateCapsuleConfig,
  resetCapsuleConfig,
  DEFAULT_CAPSULE_CONFIG
} from '../src/lib/capsuleSettings';

describe('capsuleSettings', () => {
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
    resetCapsuleConfig();
  });

  afterEach(() => {
    resetCapsuleConfig();
  });

  it('initializes with default configuration', () => {
    const config = get(capsuleConfigStore);
    expect(config).toEqual(DEFAULT_CAPSULE_CONFIG);
  });

  it('updates configuration and persists to localStorage', () => {
    updateCapsuleConfig({
      showTraffic: false
    });

    const config = get(capsuleConfigStore);
    expect(config.showTraffic).toBe(false);
    expect(config.visible).toBe(true);
    expect(config.showResources).toBe(true);

    expect(mockStorage['xcp_capsule_config']).toBeDefined();
    const stored = JSON.parse(mockStorage['xcp_capsule_config']);
    expect(stored.showTraffic).toBe(false);
  });

  it('resets configuration back to defaults', () => {
    updateCapsuleConfig({
      visible: false,
      showTraffic: false,
      showResources: false
    });

    expect(get(capsuleConfigStore).visible).toBe(false);

    resetCapsuleConfig();

    expect(get(capsuleConfigStore)).toEqual(DEFAULT_CAPSULE_CONFIG);
    const stored = JSON.parse(mockStorage['xcp_capsule_config']);
    expect(stored).toEqual(DEFAULT_CAPSULE_CONFIG);
  });
});
