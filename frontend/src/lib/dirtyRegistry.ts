import { writable } from 'svelte/store';

export interface DirtySource<T = any> {
  name: string;
  isDirty: () => boolean;
  onSave?: () => Promise<boolean>;
  getDraft?: () => T;
  restoreDraft?: (draft: T) => void;
}

export interface DraftRecord<T = any> {
  sourceId: string;
  name: string;
  timestamp: number;
  data: T;
}

export const DRAFT_PREFIX = 'xcp_draft_';

const sources = new Map<string, DirtySource>();

export const hasUnsavedChanges = writable<boolean>(false);

function updateDirtyStore(): boolean {
  let dirty = false;
  for (const [, source] of sources) {
    try {
      if (source.isDirty()) {
        dirty = true;
        break;
      }
    } catch (e) {
      console.error('[dirtyRegistry] Error checking isDirty():', e);
    }
  }
  hasUnsavedChanges.set(dirty);
  return dirty;
}

/**
 * Registers a dirty source and returns an unregister function.
 */
export function registerDirtySource(id: string, source: DirtySource): () => void {
  sources.set(id, source);
  updateDirtyStore();
  return () => unregisterDirtySource(id);
}

/**
 * Unregisters a dirty source by ID.
 */
export function unregisterDirtySource(id: string): void {
  sources.delete(id);
  updateDirtyStore();
}

/**
 * Checks whether any registered source has unsaved changes.
 */
export function isAnySourceDirty(): boolean {
  return updateDirtyStore();
}

/**
 * Returns all currently dirty sources with their IDs.
 */
export function getDirtySources(): Array<{ id: string; source: DirtySource }> {
  const result: Array<{ id: string; source: DirtySource }> = [];
  for (const [id, source] of sources) {
    try {
      if (source.isDirty()) {
        result.push({ id, source });
      }
    } catch (e) {
      console.error(`[dirtyRegistry] Error checking isDirty() for ${id}:`, e);
    }
  }
  return result;
}

/**
 * Sequentially calls onSave() for all dirty sources.
 * Returns true if all saved successfully, or false if any failed or threw.
 */
export async function saveAllDirtySources(): Promise<boolean> {
  const dirty = getDirtySources();
  for (const item of dirty) {
    if (typeof item.source.onSave === 'function') {
      try {
        const ok = await item.source.onSave();
        if (!ok) {
          return false;
        }
      } catch (e) {
        console.error(`[dirtyRegistry] Error saving dirty source ${item.id}:`, e);
        return false;
      }
    }
  }
  updateDirtyStore();
  return true;
}

/**
 * Saves draft records for all dirty sources into sessionStorage with xcp_draft_ prefix.
 * Returns the number of drafts successfully saved.
 */
export function saveDraftsToSessionStorage(): number {
  if (typeof sessionStorage === 'undefined') return 0;

  let savedCount = 0;
  for (const [id, source] of sources) {
    try {
      if (source.isDirty() && typeof source.getDraft === 'function') {
        const data = source.getDraft();
        if (data !== undefined && data !== null) {
          const record: DraftRecord = {
            sourceId: id,
            name: source.name || id,
            timestamp: Date.now(),
            data
          };
          sessionStorage.setItem(DRAFT_PREFIX + id, JSON.stringify(record));
          savedCount++;
        }
      }
    } catch (e) {
      console.error(`[dirtyRegistry] Error creating draft for ${id}:`, e);
    }
  }
  return savedCount;
}

/**
 * Retrieves a draft record by source ID from sessionStorage.
 */
export function getDraft<T = any>(id: string): DraftRecord<T> | null {
  if (typeof sessionStorage === 'undefined') return null;

  try {
    const raw = sessionStorage.getItem(DRAFT_PREFIX + id);
    if (!raw) return null;
    const parsed = JSON.parse(raw) as DraftRecord<T>;
    if (parsed && typeof parsed === 'object' && parsed.sourceId && parsed.timestamp !== undefined) {
      return parsed;
    }
    return null;
  } catch (e) {
    console.error(`[dirtyRegistry] Failed to parse draft for ${id}:`, e);
    return null;
  }
}

/**
 * Clears a draft record from sessionStorage.
 */
export function clearDraft(id: string): void {
  if (typeof sessionStorage === 'undefined') return;
  sessionStorage.removeItem(DRAFT_PREFIX + id);
}

/**
 * Retrieves all active draft records from sessionStorage.
 */
export function getAllDrafts(): DraftRecord[] {
  if (typeof sessionStorage === 'undefined') return [];

  const drafts: DraftRecord[] = [];
  try {
    for (let i = 0; i < sessionStorage.length; i++) {
      const key = sessionStorage.key(i);
      if (key && key.startsWith(DRAFT_PREFIX)) {
        const raw = sessionStorage.getItem(key);
        if (raw) {
          try {
            const parsed = JSON.parse(raw) as DraftRecord;
            if (parsed && parsed.sourceId) {
              drafts.push(parsed);
            }
          } catch {
            // Ignore corrupted individual draft entry
          }
        }
      }
    }
  } catch (e) {
    console.error('[dirtyRegistry] Error scanning sessionStorage for drafts:', e);
  }
  return drafts;
}

/**
 * Clears all draft records (xcp_draft_*) from sessionStorage.
 */
export function clearAllDrafts(): void {
  if (typeof sessionStorage === 'undefined') return;

  try {
    const keysToRemove: string[] = [];
    for (let i = 0; i < sessionStorage.length; i++) {
      const key = sessionStorage.key(i);
      if (key && key.startsWith(DRAFT_PREFIX)) {
        keysToRemove.push(key);
      }
    }
    for (const key of keysToRemove) {
      sessionStorage.removeItem(key);
    }
  } catch (e) {
    console.error('[dirtyRegistry] Error clearing all drafts:', e);
  }
}
