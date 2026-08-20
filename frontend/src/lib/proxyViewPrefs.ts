// Персистентность пользовательских предпочтений страницы Прокси в localStorage.
// Конвенция try/catch + whitelist-валидация распарсенного JSON — по образцу
// frontend/src/lib/pingTargetStore.ts (readPinnedCoreGroups/readProxiesViewMode
// никогда не бросают исключение и всегда возвращают безопасное значение по умолчанию).

export const PINNED_CORE_GROUPS_KEY = 'xcp_pinned_core_groups';
export const PROXIES_VIEW_MODE_KEY = 'proxies_view_mode';
export const MAX_PINNED_CORE_GROUPS = 50;

export type ProxiesViewMode = 'grid' | 'list';

function safeGetItem(key: string): string | null {
  if (typeof window === 'undefined' || !window.localStorage) return null;
  try {
    return localStorage.getItem(key);
  } catch {
    return null;
  }
}

function safeSetItem(key: string, value: string): void {
  if (typeof window === 'undefined' || !window.localStorage) return;
  try {
    localStorage.setItem(key, value);
  } catch {
    // Storage quota or private browsing restriction — ignore
  }
}

export function readPinnedCoreGroups(): string[] {
  const raw = safeGetItem(PINNED_CORE_GROUPS_KEY);
  if (!raw) return [];
  try {
    const parsed = JSON.parse(raw);
    if (!Array.isArray(parsed)) return [];
    const cleaned = parsed.filter(
      (v): v is string => typeof v === 'string' && v.length > 0 && v.length <= 200
    );
    const deduped = Array.from(new Set(cleaned));
    return deduped.slice(0, MAX_PINNED_CORE_GROUPS);
  } catch {
    return [];
  }
}

export function writePinnedCoreGroups(names: string[]): void {
  safeSetItem(PINNED_CORE_GROUPS_KEY, JSON.stringify(names));
}

export function togglePinnedCoreGroup(name: string): string[] {
  const current = readPinnedCoreGroups();
  const idx = current.indexOf(name);
  const next = idx >= 0 ? current.filter((n) => n !== name) : [...current, name];
  writePinnedCoreGroups(next);
  return next;
}

export function readProxiesViewMode(): ProxiesViewMode {
  const raw = safeGetItem(PROXIES_VIEW_MODE_KEY);
  return raw === 'list' ? 'list' : 'grid';
}

export function writeProxiesViewMode(mode: ProxiesViewMode): void {
  safeSetItem(PROXIES_VIEW_MODE_KEY, mode);
}
