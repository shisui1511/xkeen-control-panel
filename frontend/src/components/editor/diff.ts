import { t } from '../../i18n';
import { get } from 'svelte/store';

export interface DiffChange {
  type: 'added' | 'removed' | 'unchanged';
  value: string;
}

export interface DiffGroup {
  type: 'added' | 'removed' | 'unchanged' | 'collapsed';
  lines: string[];
}

export function getDiff(oldStr: string, newStr: string): DiffChange[] {
  const oldLines = oldStr.split('\n');
  const newLines = newStr.split('\n');

  const m = oldLines.length;
  const n = newLines.length;

  if (m + n > 2000) {
    const $t = get(t);
    return [
      {
        type: 'removed',
        value: $t('editor.diff_large_old')
      },
      {
        type: 'added',
        value: $t('editor.diff_large_new')
      }
    ];
  }

  const dp: number[][] = Array.from({ length: m + 1 }, () => new Array(n + 1).fill(0));

  for (let i = 1; i <= m; i++) {
    for (let j = 1; j <= n; j++) {
      if (oldLines[i - 1] === newLines[j - 1]) {
        dp[i][j] = dp[i - 1][j - 1] + 1;
      } else {
        dp[i][j] = Math.max(dp[i - 1][j], dp[i][j - 1]);
      }
    }
  }

  const diff: DiffChange[] = [];
  let i = m,
    j = n;
  while (i > 0 || j > 0) {
    if (i > 0 && j > 0 && oldLines[i - 1] === newLines[j - 1]) {
      diff.unshift({ type: 'unchanged', value: oldLines[i - 1] });
      i--;
      j--;
    } else if (j > 0 && (i === 0 || dp[i][j - 1] >= dp[i - 1][j])) {
      diff.unshift({ type: 'added', value: newLines[j - 1] });
      j--;
    } else if (i > 0 && (j === 0 || dp[i - 1][j] > dp[i][j - 1])) {
      diff.unshift({ type: 'removed', value: oldLines[i - 1] });
      i--;
    }
  }
  return diff;
}

function getHiddenLinesText(count: number): string {
  try {
    const $t = get(t);
    if (typeof $t === 'function') {
      const text = $t('editor.diff_lines_hidden', { count });
      if (text && text !== 'editor.diff_lines_hidden') {
        return text;
      }
    }
  } catch {
    // fallback if store is unavailable
  }
  return `... ${count} lines hidden ...`;
}

export function getDiffGroups(oldStr: string, newStr: string): DiffGroup[] {
  const changes = getDiff(oldStr, newStr);
  if (changes.length === 0) return [];
  const groups: DiffGroup[] = [];

  let currentType: DiffGroup['type'] = changes[0].type;
  let currentLines: string[] = [];

  for (const change of changes) {
    if (change.type === currentType) {
      currentLines.push(change.value);
    } else {
      if (currentType === 'unchanged' && currentLines.length > 6) {
        groups.push({
          type: 'unchanged',
          lines: currentLines.slice(0, 3)
        });
        groups.push({
          type: 'collapsed',
          lines: [getHiddenLinesText(currentLines.length - 6)]
        });
        groups.push({
          type: 'unchanged',
          lines: currentLines.slice(-3)
        });
      } else {
        groups.push({
          type: currentType,
          lines: currentLines
        });
      }
      currentType = change.type;
      currentLines = [change.value];
    }
  }

  if (currentLines.length > 0) {
    if (currentType === 'unchanged' && currentLines.length > 6) {
      groups.push({
        type: 'unchanged',
        lines: currentLines.slice(0, 3)
      });
      groups.push({
        type: 'collapsed',
        lines: [getHiddenLinesText(currentLines.length - 6)]
      });
      groups.push({
        type: 'unchanged',
        lines: currentLines.slice(-3)
      });
    } else {
      groups.push({
        type: currentType,
        lines: currentLines
      });
    }
  }

  return groups;
}
