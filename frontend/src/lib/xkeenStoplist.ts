// Зеркало internal/utils/xkeen_stoplist.go: правило check_xray_backups() из
// S05xkeen. Источник истины — сервер (409 xkeen_stoplist_name при создании,
// переименовании и сохранении нового файла); модуль нужен только для мгновенной
// подсказки под полем имени и значка в дереве файлов.

/** Слова стоп-списка в порядке проверки XKeen. */
export const XKEEN_STOPLIST_WORDS = [
  'bak',
  'old',
  'copy',
  'копия',
  'orig',
  'save',
  'temp',
  'tmp'
] as const;

/** Псевдослово для шаблона `*(*).json`. */
export const XKEEN_PAREN_WORD = '()';

const JSON_SUFFIX = '.json';

/**
 * Сворачивает регистр явной таблицей: ASCII A–Z, кириллица А–Я и Ё; остальные
 * символы без изменений (та же таблица, что в Go, вместо toLowerCase).
 */
export function foldXKeen(s: string): string {
  let out = '';
  for (const ch of s) {
    const cp = ch.codePointAt(0)!;
    if (cp >= 0x41 && cp <= 0x5a) {
      out += String.fromCodePoint(cp + 0x20);
    } else if (cp >= 0x0410 && cp <= 0x042f) {
      out += String.fromCodePoint(cp + 0x20);
    } else if (cp === 0x0401) {
      out += String.fromCodePoint(0x0451);
    } else {
      out += ch;
    }
  }
  return out;
}

/**
 * Возвращает первое совпавшее слово стоп-списка XKeen или null. Слова ищутся в
 * имени без учёта регистра (включая суффикс .JSON), шаблон скобок `*(*).json` —
 * с учётом регистра.
 */
export function matchXKeenStoplist(name: string): string | null {
  const folded = foldXKeen(name);
  if (folded.endsWith(JSON_SUFFIX)) {
    const stem = folded.slice(0, -JSON_SUFFIX.length);
    for (const word of XKEEN_STOPLIST_WORDS) {
      if (stem.includes(word)) return word;
    }
  }
  const parenSuffix = ')' + JSON_SUFFIX;
  if (name.endsWith(parenSuffix) && name.slice(0, -parenSuffix.length).includes('(')) {
    return XKEEN_PAREN_WORD;
  }
  return null;
}

/**
 * Лежит ли файл прямо в каталоге Xray: XKeen проверяет только корень каталога,
 * подкаталоги он не просматривает.
 */
export function isXrayRootPath(path: string, xrayDir: string): boolean {
  const slash = path.lastIndexOf('/');
  if (slash < 0) return false;
  const parent = path.slice(0, slash);
  const dir = xrayDir.endsWith('/') ? xrayDir.slice(0, -1) : xrayDir;
  return parent === dir;
}
