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
}

export interface InlineSegment {
  text: string;
  code: boolean;
}

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
 * Возвращает разделы «### …» со списками «- …». Шапка (баннер RC, описание
 * панели) и всё после первого «---» или «## » (установка, скачивание) опускаются.
 */
export function parseReleaseNotes(body: string): NoteSection[] {
  const sections: NoteSection[] = [];
  let current: NoteSection | null = null;

  for (const raw of (body || '').split(/\r?\n/)) {
    const line = raw.trim();
    if (line.startsWith('### ')) {
      const title = line.slice(4).trim();
      current = { kind: sectionKind(title), title: cleanTitle(title), items: [] };
      sections.push(current);
      continue;
    }
    if (sections.length > 0 && (line === '---' || line.startsWith('## '))) break;
    if (!current || !line.startsWith('- ')) continue;

    const text = line.slice(2).trim();
    const item: NoteItem = { text };
    const m = text.match(COMMIT_LINK);
    if (m) {
      item.commit = { sha: m[1].slice(0, 7), url: m[2] };
      item.text = text.slice(0, m.index).trim();
    }
    if (item.text) current.items.push(item);
  }

  return sections.filter((s) => s.items.length > 0);
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

export type ReleaseKind = 'stable' | 'rc' | 'dev';

/** Тип сборки по версии: 0.29.0 — stable, 0.29.0-rc.2 — rc, 0.29.0-dev.5+g… — dev. */
export function releaseKind(version: string): ReleaseKind {
  const pre = version.replace(/^v/, '').split('+')[0].split('-')[1] ?? '';
  if (pre === '') return 'stable';
  if (pre.startsWith('rc')) return 'rc';
  return 'dev';
}
