import { describe, expect, it } from 'vitest';
import { detectPanelUpdate, dismissUpdate, isUpdateDismissed } from './updateNotify';

function memoryStore() {
  const data = new Map<string, string>();
  return {
    getItem: (k: string) => data.get(k) ?? null,
    setItem: (k: string, v: string) => void data.set(k, v)
  };
}

describe('detectPanelUpdate', () => {
  it('reports nothing on the first visit, then the new version once', () => {
    const store = memoryStore();
    expect(detectPanelUpdate('v0.28.0', store)).toBeNull();
    expect(detectPanelUpdate('v0.28.0', store)).toBeNull();
    expect(detectPanelUpdate('v0.29.0-rc.5', store)).toBe('0.29.0-rc.5');
    expect(detectPanelUpdate('v0.29.0-rc.5', store)).toBeNull();
  });

  it('ignores unknown versions and broken storage', () => {
    expect(detectPanelUpdate('unknown', memoryStore())).toBeNull();
    const broken = {
      getItem: () => {
        throw new Error('denied');
      },
      setItem: () => {
        throw new Error('denied');
      }
    };
    expect(detectPanelUpdate('v0.29.0', broken)).toBeNull();
    expect(detectPanelUpdate('v0.29.0', null)).toBeNull();
  });
});

describe('dismissUpdate', () => {
  it('hides the banner only for the dismissed version', () => {
    const store = memoryStore();
    expect(isUpdateDismissed('0.29.0', store)).toBe(false);
    dismissUpdate('0.29.0', store);
    expect(isUpdateDismissed('0.29.0', store)).toBe(true);
    expect(isUpdateDismissed('0.29.1', store)).toBe(false);
    expect(isUpdateDismissed('', store)).toBe(false);
  });
});
