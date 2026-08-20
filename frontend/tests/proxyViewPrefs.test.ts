import { describe, it, expect, beforeEach } from 'vitest';
import {
  readPinnedCoreGroups,
  writePinnedCoreGroups,
  togglePinnedCoreGroup,
  readProxiesViewMode,
  writeProxiesViewMode,
  PINNED_CORE_GROUPS_KEY,
  PROXIES_VIEW_MODE_KEY
} from '../src/lib/proxyViewPrefs';

describe('proxyViewPrefs', () => {
  let mockStorage: Record<string, string> = {};

  beforeEach(() => {
    mockStorage = {};
    (global as any).window = {
      localStorage: {
        getItem: (key: string) => mockStorage[key] ?? null,
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
  });

  describe('readPinnedCoreGroups', () => {
    it('returns [] for corrupted or unexpected localStorage values', () => {
      mockStorage[PINNED_CORE_GROUPS_KEY] = 'not json';
      expect(readPinnedCoreGroups()).toEqual([]);

      mockStorage[PINNED_CORE_GROUPS_KEY] = '{"a":1}';
      expect(readPinnedCoreGroups()).toEqual([]);

      mockStorage[PINNED_CORE_GROUPS_KEY] = 'null';
      expect(readPinnedCoreGroups()).toEqual([]);
    });

    it('filters non-strings and deduplicates', () => {
      mockStorage[PINNED_CORE_GROUPS_KEY] = JSON.stringify(['A', 1, null, 'A']);
      expect(readPinnedCoreGroups()).toEqual(['A']);
    });

    it('caps result at MAX_PINNED_CORE_GROUPS (50)', () => {
      const names = Array.from({ length: 80 }, (_, i) => `group-${i}`);
      mockStorage[PINNED_CORE_GROUPS_KEY] = JSON.stringify(names);
      expect(readPinnedCoreGroups().length).toBe(50);
    });

    it('returns [] when localStorage is empty', () => {
      expect(readPinnedCoreGroups()).toEqual([]);
    });
  });

  describe('togglePinnedCoreGroup', () => {
    it('adds then removes a group name', () => {
      const afterAdd = togglePinnedCoreGroup('X');
      expect(afterAdd).toEqual(['X']);
      expect(readPinnedCoreGroups()).toEqual(['X']);

      const afterRemove = togglePinnedCoreGroup('X');
      expect(afterRemove).toEqual([]);
      expect(readPinnedCoreGroups()).toEqual([]);
    });
  });

  describe('writePinnedCoreGroups', () => {
    it('persists the given array as JSON', () => {
      writePinnedCoreGroups(['A', 'B']);
      expect(JSON.parse(mockStorage[PINNED_CORE_GROUPS_KEY])).toEqual(['A', 'B']);
    });
  });

  describe('readProxiesViewMode / writeProxiesViewMode', () => {
    it('defaults to grid for missing or unexpected values', () => {
      expect(readProxiesViewMode()).toBe('grid');
      mockStorage[PROXIES_VIEW_MODE_KEY] = 'weird';
      expect(readProxiesViewMode()).toBe('grid');
    });

    it('accepts list', () => {
      mockStorage[PROXIES_VIEW_MODE_KEY] = 'list';
      expect(readProxiesViewMode()).toBe('list');
    });

    it('persists writes', () => {
      writeProxiesViewMode('list');
      expect(mockStorage[PROXIES_VIEW_MODE_KEY]).toBe('list');
    });
  });
});
