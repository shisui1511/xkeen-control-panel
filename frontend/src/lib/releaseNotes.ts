// Разбор описания релиза (.github/scripts/gen_notes.py) в структуру для страницы
// обновлений. Рендерится обычной разметкой Svelte, без {@html}: текст релиза
// приходит с GitHub и не должен попадать в DOM как HTML.

export type NoteKind = 'feat' | 'fix' | 'other';

export interface NoteCommit {
  sha: string;
  url: string;
}

export interface NoteItem {
  text: string;
  commit?: NoteCommit;
}

export interface NoteSection {
  kind: NoteKind;
  title: string;
  items: NoteItem[];
  /** Сколько строк не вошло в раздел: хвост «…и ещё N» из gen_notes.py (capped). */
  more: number;
}

export interface ParsedRelease {
  /** Строки «## Что вошло»: о чём релиз, словами человека. */
  summary: string[];
  sections: NoteSection[];
}

export interface InlineSegment {
  text: string;
  code: boolean;
}

// Хвост длинного раздела: «…и ещё 4 — полный список по ссылке внизу» (ссылка — на странице GitHub)
const MORE_TAIL = /^(?:…|\.\.\.)\s*(?:и ещё|and)\s+(\d+)/i;

const COMMIT_LINK = /\s*\[`([0-9a-f]{7,40})`\]\((https:\/\/github\.com\/[^\s)]+)\)\s*$/;

function sectionKind(title: string): NoteKind {
  const t = title.toLowerCase();
  if (title.includes('🚀') || t.includes('возможност') || t.includes('feature')) return 'feat';
  if (title.includes('🐛') || t.includes('исправлен') || t.includes('fix')) return 'fix';
  return 'other';
}

// Заголовок без эмодзи в начале: «🚀 Новые возможности» → «Новые возможности»
function cleanTitle(title: string): string {
  return title.replace(/^[^\p{L}\p{N}]+/u, '').trim();
}

/**
 * Возвращает строки «## Что вошло» и разделы «### …» со списками «- …». Прочая
 * шапка (баннер пре-релиза, описание панели) и всё после первого «---» или «## »
 * за разделами (установка, скачивание) опускаются.
 */
export function parseRelease(body: string): ParsedRelease {
  const summary: string[] = [];
  const sections: NoteSection[] = [];
  let current: NoteSection | null = null;
  let inSummary = false;

  for (const raw of (body || '').split(/\r?\n/)) {
    const line = raw.trim();
    if (line.startsWith('### ')) {
      const title = line.slice(4).trim();
      current = { kind: sectionKind(title), title: cleanTitle(title), items: [], more: 0 };
      sections.push(current);
      inSummary = false;
      continue;
    }
    if (sections.length > 0 && (line === '---' || line.startsWith('## '))) break;
    if (line.startsWith('## ')) {
      inSummary = true;
      continue;
    }
    if (line === '---') inSummary = false;
    if (!line.startsWith('- ')) continue;

    const text = line.slice(2).trim();
    if (!current) {
      if (inSummary && text) summary.push(text);
      continue;
    }
    const tail = text.match(MORE_TAIL);
    if (tail) {
      current.more += Number(tail[1]);
      continue;
    }
    const item: NoteItem = { text };
    const m = text.match(COMMIT_LINK);
    if (m) {
      item.commit = { sha: m[1].slice(0, 7), url: m[2] };
      item.text = text.slice(0, m.index).trim();
    }
    if (item.text) current.items.push(item);
  }

  return { summary, sections: sections.filter((s) => s.items.length > 0) };
}

/** Только разделы: см. parseRelease. */
export function parseReleaseNotes(body: string): NoteSection[] {
  return parseRelease(body).sections;
}

/** Делит текст по `обратным кавычкам` на обычные фрагменты и код. */
export function splitInlineCode(text: string): InlineSegment[] {
  const parts = text.split('`');
  // Непарная кавычка — показываем как есть
  if (parts.length % 2 === 0) return [{ text, code: false }];
  return parts
    .map((part, i) => ({ text: part, code: i % 2 === 1 }))
    .filter((seg) => seg.text !== '');
}

export type ReleaseKind = 'stable' | 'beta' | 'alpha';

/**
 * Тип версии по суффиксу: X.Y.Z — stable, X.Y.Z-beta.2 — beta, X.Y.Z-alpha.3+g1a2b3c4 — alpha.
 * Старые теги -rc.N остаются в «Что нового» как beta; прежние сборки разработки и неизвестное — alpha.
 * Порядок версий здесь не считается: сравнение живёт в Go.
 */
export function releaseKind(version: string): ReleaseKind {
  const pre = version.replace(/^v/, '').split('+')[0].split('-')[1] ?? '';
  if (pre === '') return 'stable';
  if (pre.startsWith('beta') || pre.startsWith('rc')) return 'beta';
  return 'alpha';
}
