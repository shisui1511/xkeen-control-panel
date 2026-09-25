import { describe, expect, it } from 'vitest';
import { parseReleaseNotes, releaseKind, splitInlineCode } from './releaseNotes';

const REPO = 'https://github.com/shisui1511/xkeen-control-panel';

const RC_BODY = `> 🧪 **Release candidate v0.29.0** — доступен в канале обновлений beta.
> Если за 24 ч не выйдет новый RC, этот же коммит автоматически станет стабильным релизом **v0.29.0**.

> Веб-панель управления XKeen для роутеров Keenetic/Netcraze — единый бинарник без зависимостей.

### 🚀 Новые возможности

- автоматический выпуск release candidate [\`a4878ff\`](${REPO}/commit/a4878ff9aa)

### 🐛 Исправления

- самообновление проверяет SHA-256 бинарника [\`4c5f92e\`](${REPO}/commit/4c5f92ed11)
- сжатие \`.gz\` при скачивании вместо UPX

### ⚙️ Прочее

- pre-commit проверяет форматирование [\`e782315\`](${REPO}/commit/e782315f)

---

## 📥 Установка и обновление

- не должно попасть в разбор
`;

describe('parseReleaseNotes', () => {
  it('extracts sections, items and commit links, skipping header and install block', () => {
    const sections = parseReleaseNotes(RC_BODY);
    expect(sections.map((s) => [s.kind, s.title, s.items.length])).toEqual([
      ['feat', 'Новые возможности', 1],
      ['fix', 'Исправления', 2],
      ['other', 'Прочее', 1]
    ]);
    expect(sections[0].items[0]).toEqual({
      text: 'автоматический выпуск release candidate',
      commit: { sha: 'a4878ff', url: `${REPO}/commit/a4878ff9aa` }
    });
    expect(sections[1].items[1]).toEqual({ text: 'сжатие `.gz` при скачивании вместо UPX' });
  });

  it('ignores commit links outside github.com', () => {
    const [section] = parseReleaseNotes(
      '### 🐛 Исправления\n\n- фикс [`abcdef1`](https://evil.example/commit/abcdef1)'
    );
    expect(section.items[0].commit).toBeUndefined();
  });

  it('returns nothing for bodies without sections', () => {
    expect(parseReleaseNotes('')).toEqual([]);
    expect(parseReleaseNotes('_Список изменений недоступен._')).toEqual([]);
    expect(parseReleaseNotes('### Пусто\n\n---')).toEqual([]);
  });
});

describe('splitInlineCode', () => {
  it('splits backtick code spans', () => {
    expect(splitInlineCode('качает `.gz` и `.sha256`')).toEqual([
      { text: 'качает ', code: false },
      { text: '.gz', code: true },
      { text: ' и ', code: false },
      { text: '.sha256', code: true }
    ]);
  });

  it('keeps text with an unpaired backtick as is', () => {
    expect(splitInlineCode('один ` без пары')).toEqual([{ text: 'один ` без пары', code: false }]);
  });
});

describe('releaseKind', () => {
  it.each([
    ['0.29.0', 'stable'],
    ['v0.29.0', 'stable'],
    ['0.29.0-rc.2', 'rc'],
    ['0.29.0-dev', 'dev'],
    ['0.29.0-dev.5+g1a2b3c4', 'dev']
  ])('%s → %s', (version, kind) => {
    expect(releaseKind(version)).toBe(kind);
  });
});
