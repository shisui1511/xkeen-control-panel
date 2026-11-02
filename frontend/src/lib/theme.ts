import { writable } from 'svelte/store';

// Темы оформления. `id` пишется в localStorage('theme') и в data-theme на <html>;
// `scheme` — в data-scheme: по нему CSS выбирает светлые/тёмные варианты акцентов
// и прочие правила «для светлой темы», не перечисляя каждую палитру.
// Список дублируется в public/theme-init.js (CSP запрещает inline-скрипты) —
// при добавлении темы правьте оба места и блок токенов в styles/global.css.
export type ThemeScheme = 'light' | 'dark';
export type ThemeId = 'dark' | 'charcoal' | 'dusk' | 'amber' | 'light';
export type ThemeChoice = ThemeId | 'auto';

export interface ThemeInfo {
  id: ThemeId;
  scheme: ThemeScheme;
  /** Цвет строки браузера на телефоне — фон мобильной шапки (--bg-deep). */
  themeColor: string;
  labelKey: string;
  /** Цвета мини-превью в настройках: фон, карточка, акцент, текст. */
  preview: { bg: string; card: string; accent: string; text: string };
}

export const THEMES: readonly ThemeInfo[] = [
  {
    id: 'dark',
    scheme: 'dark',
    themeColor: '#07182a',
    labelKey: 'settings.theme_dark_btn',
    preview: { bg: '#0c2237', card: '#102a44', accent: '#29c2f0', text: '#d9e7f4' }
  },
  {
    id: 'charcoal',
    scheme: 'dark',
    themeColor: '#000000',
    labelKey: 'settings.theme_charcoal_btn',
    preview: { bg: '#121416', card: '#181b1e', accent: '#29c2f0', text: '#e3e6e9' }
  },
  {
    id: 'dusk',
    scheme: 'dark',
    themeColor: '#171b21',
    labelKey: 'settings.theme_dusk_btn',
    preview: { bg: '#1f242c', card: '#272d36', accent: '#6db3d6', text: '#c6d0dc' }
  },
  {
    id: 'amber',
    scheme: 'dark',
    themeColor: '#0a0704',
    labelKey: 'settings.theme_amber_btn',
    preview: { bg: '#110d07', card: '#18120a', accent: '#ffbe55', text: '#f1c47e' }
  },
  // «Туман». id остаётся 'light': так его уже сохранили пользователи прежней
  // «Светлой» и на него опираются роутерные тесты.
  {
    id: 'light',
    scheme: 'light',
    themeColor: '#22303d',
    labelKey: 'settings.theme_fog_btn',
    preview: { bg: '#e2e7ec', card: '#eef1f4', accent: '#1b6a8e', text: '#1c2834' }
  }
];

/** Тёмная и светлая темы, которые включает «Авто» по настройке системы. */
export const AUTO_THEMES: Record<ThemeScheme, ThemeId> = { dark: 'charcoal', light: 'light' };

/** Устаревшие id: 'fog' был отдельной темой, пока «Светлая» не стала «Туманом». */
const LEGACY_IDS: Record<string, ThemeId> = { fog: 'light' };

const PREV_KEY = 'theme_prev';

export function isThemeId(v: unknown): v is ThemeId {
  return typeof v === 'string' && THEMES.some((th) => th.id === v);
}

export function themeInfo(id: ThemeId): ThemeInfo {
  return THEMES.find((th) => th.id === id) ?? THEMES[0];
}

export function schemeOf(id: ThemeId): ThemeScheme {
  return themeInfo(id).scheme;
}

function systemPrefersDark(): boolean {
  return (
    typeof window !== 'undefined' &&
    typeof window.matchMedia === 'function' &&
    window.matchMedia('(prefers-color-scheme: dark)').matches
  );
}

function storageGet(key: string): string {
  try {
    return localStorage.getItem(key) || '';
  } catch {
    // localStorage недоступен (приватный режим, заблокированные cookies)
    return '';
  }
}

function storageSet(key: string, value: string | null) {
  try {
    if (value === null) localStorage.removeItem(key);
    else localStorage.setItem(key, value);
  } catch {
    // см. storageGet
  }
}

/** Сохранённый выбор пользователя; всё, чего нет в списке, — «авто». */
export function readThemeChoice(): ThemeChoice {
  const raw = storageGet('theme');
  const saved = LEGACY_IDS[raw] ?? raw;
  return isThemeId(saved) ? saved : 'auto';
}

export function resolveTheme(choice: ThemeChoice): ThemeId {
  if (choice !== 'auto') return choice;
  return AUTO_THEMES[systemPrefersDark() ? 'dark' : 'light'];
}

function readAppliedTheme(): ThemeId {
  if (typeof document === 'undefined') return 'dark';
  const attr = document.documentElement.getAttribute('data-theme');
  return isThemeId(attr) ? attr : resolveTheme(readThemeChoice());
}

/** Тема, применённая к документу сейчас. */
export const currentTheme = writable<ThemeId>(readAppliedTheme());

let switchFrame = 0;

/**
 * На время смены темы (и акцента, плотности) снимает CSS-переходы: иначе каждый элемент с
 * `transition: background/color` перекрашивается со своей скоростью и экран
 * на долю секунды пестрит смесью двух палитр. Атрибут уходит через два кадра,
 * когда новые цвета уже отрисованы.
 */
export function suppressTransitions(root: HTMLElement = document.documentElement) {
  if (typeof requestAnimationFrame !== 'function') return;
  root.setAttribute('data-theme-switching', '');
  cancelAnimationFrame(switchFrame);
  switchFrame = requestAnimationFrame(() => {
    switchFrame = requestAnimationFrame(() => root.removeAttribute('data-theme-switching'));
  });
}

export function applyTheme(id: ThemeId) {
  const info = themeInfo(id);
  const root = document.documentElement;
  if (root.getAttribute('data-theme') !== info.id) suppressTransitions(root);
  root.setAttribute('data-theme', info.id);
  root.setAttribute('data-scheme', info.scheme);
  document.querySelector('meta[name="theme-color"]')?.setAttribute('content', info.themeColor);
  currentTheme.set(info.id);
}

/** Сохраняет выбор и применяет его. */
export function setThemeChoice(choice: ThemeChoice) {
  const prev = readAppliedTheme();
  const next = resolveTheme(choice);
  storageSet('theme', choice === 'auto' ? null : choice);
  if (next !== prev) storageSet(PREV_KEY, prev);
  applyTheme(next);
}

/**
 * Быстрый переключатель в меню: светлая ↔ тёмная. Возвращается к последней теме
 * другой схемы (например, «Туман» ↔ «Сумерки»), иначе — к темам режима «Авто».
 */
export function toggleThemeScheme() {
  const cur = readAppliedTheme();
  const prev = storageGet(PREV_KEY);
  const target: ThemeId =
    isThemeId(prev) && schemeOf(prev) !== schemeOf(cur)
      ? prev
      : AUTO_THEMES[schemeOf(cur) === 'dark' ? 'light' : 'dark'];
  setThemeChoice(target);
}

export function initTheme() {
  applyTheme(resolveTheme(readThemeChoice()));
}
