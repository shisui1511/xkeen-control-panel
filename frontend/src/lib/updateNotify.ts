// Уведомления об обновлениях панели: общее состояние фоновой проверки
// (GET /api/update/state) для бейджа в меню, баннера на дашборде и вкладки
// «Обновления», а также «панель обновилась с прошлого визита».
import { writable } from 'svelte/store';
import { apiFetchJSON } from './api';

export interface AutoInstallRecord {
  version: string;
  from: string;
  started_at: number;
  ok: boolean;
  done_at?: number;
}

export interface UpdateCheckState {
  checked_at?: number;
  channel: string;
  current_version: string;
  latest_version?: string;
  has_update: boolean;
  prerelease: boolean;
  error?: string;
  auto_check: boolean;
  auto_install: boolean;
  install_window: string;
  auto_failed_version?: string;
  last_auto_install?: AutoInstallRecord;
}

export const updateState = writable<UpdateCheckState | null>(null);

export async function refreshUpdateState(signal?: AbortSignal): Promise<void> {
  try {
    updateState.set(await apiFetchJSON<UpdateCheckState>('/api/update/state', { signal }));
  } catch (_: any) {
    // Нет связи или сессии — уведомление просто не покажется
  }
}

const DISMISSED_KEY = 'xcp.update.dismissed';
const SEEN_VERSION_KEY = 'xcp.version.seen';

type KV = Pick<Storage, 'getItem' | 'setItem'>;

function storage(): KV | null {
  try {
    return window.localStorage;
  } catch {
    return null;
  }
}

function read(store: KV | null, key: string): string | null {
  try {
    return store?.getItem(key) ?? null;
  } catch {
    return null;
  }
}

function write(store: KV | null, key: string, value: string): void {
  try {
    store?.setItem(key, value);
  } catch {
    // приватный режим или запрет хранилища — не критично
  }
}

/** Баннер про версию скрыт пользователем; новая версия покажет его снова. */
export function isUpdateDismissed(version: string, store: KV | null = storage()): boolean {
  return !!version && read(store, DISMISSED_KEY) === version;
}

export function dismissUpdate(version: string, store: KV | null = storage()): void {
  write(store, DISMISSED_KEY, version);
}

/**
 * Запоминает версию панели и возвращает её, если с прошлого визита панель
 * обновилась (вручную или автоматически). Первый визит — null.
 */
export function detectPanelUpdate(current: string, store: KV | null = storage()): string | null {
  const version = current.replace(/^v/, '');
  if (!version || version === 'unknown') return null;
  const prev = read(store, SEEN_VERSION_KEY);
  write(store, SEEN_VERSION_KEY, version);
  return prev && prev !== version ? version : null;
}
