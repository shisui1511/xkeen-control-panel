import { describe, it, expect, beforeEach, vi } from 'vitest';
import { get } from 'svelte/store';
import {
  registerDirtySource,
  unregisterDirtySource,
  isAnySourceDirty,
  getDirtySources,
  saveAllDirtySources,
  saveDraftsToSessionStorage,
  getDraft,
  clearDraft,
  getAllDrafts,
  clearAllDrafts,
  hasUnsavedChanges,
  DRAFT_PREFIX,
  type DirtySource
} from '../src/lib/dirtyRegistry';

describe('dirtyRegistry', () => {
  // Simple in-memory sessionStorage mock for testing
  let store: Record<string, string> = {};

  beforeEach(() => {
    store = {};
    const sessionStorageMock = {
      getItem: vi.fn((key: string) => store[key] || null),
      setItem: vi.fn((key: string, value: string) => {
        store[key] = value.toString();
      }),
      removeItem: vi.fn((key: string) => {
        delete store[key];
      }),
      clear: vi.fn(() => {
        store = {};
      }),
      get length() {
        return Object.keys(store).length;
      },
      key: vi.fn((index: number) => {
        const keys = Object.keys(store);
        return keys[index] || null;
      })
    };
    vi.stubGlobal('sessionStorage', sessionStorageMock);

    // Clear any registered sources from previous tests by checking all active IDs
    for (const item of getDirtySources()) {
      unregisterDirtySource(item.id);
    }
  });

  describe('Source Registration & Tracking', () => {
    it('registers and unregisters sources, updating store correctly', () => {
      let isDirtyState = false;
      const source: DirtySource = {
        name: 'Editor',
        isDirty: () => isDirtyState
      };

      const unregister = registerDirtySource('editor-1', source);
      expect(isAnySourceDirty()).toBe(false);
      expect(get(hasUnsavedChanges)).toBe(false);

      isDirtyState = true;
      expect(isAnySourceDirty()).toBe(true);
      expect(get(hasUnsavedChanges)).toBe(true);

      unregister();
      expect(isAnySourceDirty()).toBe(false);
      expect(get(hasUnsavedChanges)).toBe(false);
    });

    it('handles multiple sources and identifies dirty ones', () => {
      const source1: DirtySource = {
        name: 'Editor 1',
        isDirty: () => false
      };
      const source2: DirtySource = {
        name: 'Generator',
        isDirty: () => true
      };

      const unreg1 = registerDirtySource('s1', source1);
      const unreg2 = registerDirtySource('s2', source2);

      expect(isAnySourceDirty()).toBe(true);
      const dirtyList = getDirtySources();
      expect(dirtyList.length).toBe(1);
      expect(dirtyList[0].id).toBe('s2');
      expect(dirtyList[0].source.name).toBe('Generator');

      unreg1();
      unreg2();
    });
  });

  describe('saveAllDirtySources', () => {
    it('executes onSave on all dirty sources successfully', async () => {
      let saved1 = false;
      let saved2 = false;

      const unreg1 = registerDirtySource('src-1', {
        name: 'S1',
        isDirty: () => true,
        onSave: async () => {
          saved1 = true;
          return true;
        }
      });
      const unreg2 = registerDirtySource('src-2', {
        name: 'S2',
        isDirty: () => true,
        onSave: async () => {
          saved2 = true;
          return true;
        }
      });

      const res = await saveAllDirtySources();
      expect(res).toBe(true);
      expect(saved1).toBe(true);
      expect(saved2).toBe(true);

      unreg1();
      unreg2();
    });

    it('returns false and stops if an onSave fails', async () => {
      let saved2 = false;

      const unreg1 = registerDirtySource('fail-src', {
        name: 'Fail',
        isDirty: () => true,
        onSave: async () => false
      });
      const unreg2 = registerDirtySource('next-src', {
        name: 'Next',
        isDirty: () => true,
        onSave: async () => {
          saved2 = true;
          return true;
        }
      });

      const res = await saveAllDirtySources();
      expect(res).toBe(false);
      expect(saved2).toBe(false);

      unreg1();
      unreg2();
    });

    it('handles thrown errors in onSave gracefully', async () => {
      const unreg = registerDirtySource('err-src', {
        name: 'Err',
        isDirty: () => true,
        onSave: async () => {
          throw new Error('Save network error');
        }
      });

      const res = await saveAllDirtySources();
      expect(res).toBe(false);

      unreg();
    });

    it('returns false if a dirty source lacks an onSave handler', async () => {
      const unreg = registerDirtySource('no-save-src', {
        name: 'NoSave',
        isDirty: () => true
      });

      const res = await saveAllDirtySources();
      expect(res).toBe(false);

      unreg();
    });
  });

  describe('Draft Serialization & sessionStorage', () => {
    it('saves drafts to sessionStorage only for dirty sources with getDraft', () => {
      const draftData = { text: 'draft content', cursor: 12 };
      const unreg1 = registerDirtySource('form-1', {
        name: 'Config Form',
        isDirty: () => true,
        getDraft: () => draftData
      });
      const unreg2 = registerDirtySource('form-2', {
        name: 'Clean Form',
        isDirty: () => false,
        getDraft: () => ({ foo: 'bar' })
      });

      const count = saveDraftsToSessionStorage();
      expect(count).toBe(1);

      const raw = sessionStorage.getItem(DRAFT_PREFIX + 'form-1');
      expect(raw).toBeTruthy();
      const parsed = JSON.parse(raw!);
      expect(parsed.sourceId).toBe('form-1');
      expect(parsed.name).toBe('Config Form');
      expect(parsed.data).toEqual(draftData);
      expect(typeof parsed.timestamp).toBe('number');

      expect(sessionStorage.getItem(DRAFT_PREFIX + 'form-2')).toBeNull();

      unreg1();
      unreg2();
    });

    it('retrieves draft correctly via getDraft', () => {
      const record = {
        sourceId: 'editor-x',
        name: 'Editor X',
        timestamp: Date.now(),
        data: { content: 'sample yaml' }
      };
      sessionStorage.setItem(DRAFT_PREFIX + 'editor-x', JSON.stringify(record));

      const retrieved = getDraft<{ content: string }>('editor-x');
      expect(retrieved).not.toBeNull();
      expect(retrieved?.data.content).toBe('sample yaml');
      expect(retrieved?.sourceId).toBe('editor-x');
    });

    it('returns null for non-existent or corrupted drafts', () => {
      expect(getDraft('non-existent')).toBeNull();

      sessionStorage.setItem(DRAFT_PREFIX + 'corrupted', '{ invalid JSON');
      expect(getDraft('corrupted')).toBeNull();

      sessionStorage.setItem(
        DRAFT_PREFIX + 'bad-structure',
        JSON.stringify({ wrong: 'structure' })
      );
      expect(getDraft('bad-structure')).toBeNull();
    });

    it('clears specific draft and lists all drafts', () => {
      sessionStorage.setItem(
        DRAFT_PREFIX + 'd1',
        JSON.stringify({ sourceId: 'd1', name: 'Draft 1', timestamp: 1, data: '1' })
      );
      sessionStorage.setItem(
        DRAFT_PREFIX + 'd2',
        JSON.stringify({ sourceId: 'd2', name: 'Draft 2', timestamp: 2, data: '2' })
      );
      sessionStorage.setItem('other_key', 'unrelated');

      const all = getAllDrafts();
      expect(all.length).toBe(2);

      clearDraft('d1');
      expect(getDraft('d1')).toBeNull();
      expect(getDraft('d2')).not.toBeNull();
      expect(sessionStorage.getItem('other_key')).toBe('unrelated');

      clearAllDrafts();
      expect(getAllDrafts().length).toBe(0);
      expect(sessionStorage.getItem('other_key')).toBe('unrelated');
    });
  });
});
