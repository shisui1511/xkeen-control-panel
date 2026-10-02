/// <reference types="node" />
import { describe, it, expect } from 'vitest';
import { existsSync, readdirSync, readFileSync, statSync } from 'node:fs';
import { dirname, join, relative, sep } from 'node:path';
import { fileURLToPath } from 'node:url';

/**
 * Охранный тест единого источника состояния ядра (ISOL-04).
 *
 * Состояние «какое ядро активно» вычисляется в одном месте (lib/kernelState.ts)
 * и раздаётся сторами (stores.ts). Если компонент читает поле активного ядра
 * из ответа capabilities напрямую, в конфликте ядер он выберет ядро сам и
 * обойдёт гейт. Тест не даёт вернуть такую проверку.
 */

const SRC_DIR = join(dirname(fileURLToPath(import.meta.url)), '..');

/** Обращение к полю через точку: `x.active_kernel`, `x.activeKernel`. */
export const FORBIDDEN_PATTERNS: RegExp[] = [/\.active_kernel\b/, /\.activeKernel\b/];

/** Пути относительно `src`, где обращение разрешено. */
export const ALLOWLIST: Record<string, string> = {
  // стор capabilities: единственное место, где поле читается из ответа сервера
  'stores.ts': 'стор capabilities и производные isXray/isMihomo/isConflict',
  // чистая модель: превращает ответ capabilities в состояние ядра
  'lib/kernelState.ts': 'чистая модель состояния ядра без Svelte',
  // кэш меню: нормализация сохранённого ответа capabilities для навигации
  'lib/navCaps.ts': 'кэш capabilities для меню',
  // исход применения с сервера apply, а не capabilities
  'lib/serviceApply.ts': 'ApplyResult с сервера apply',
  // исход применения конструктора Mihomo, а не capabilities
  'lib/constructors/mihomoApply.ts': 'result.active_kernel исхода применения'
};

const HINT =
  'Используйте isXray/isMihomo/isConflict/activeKernelName из stores.ts ' +
  'или kernelStateOf из lib/kernelState.ts вместо прямого чтения поля активного ядра.';

/** Убирает комментарии, сохраняя переводы строк (номера строк остаются верными). */
export function stripComments(text: string): string {
  const keepNewlines = (m: string) => m.replace(/[^\n]/g, '');
  return (
    text
      .replace(/\/\*[\s\S]*?\*\//g, keepNewlines)
      .replace(/<!--[\s\S]*?-->/g, keepNewlines)
      // однострочный комментарий; `://` в адресах не считается началом комментария
      .replace(/(^|[^:\\])\/\/.*$/gm, '$1')
  );
}

export interface GuardFinding {
  line: number;
  text: string;
}

/** Ищет запрещённые обращения в тексте файла (комментарии не учитываются). */
export function findForbidden(content: string): GuardFinding[] {
  const findings: GuardFinding[] = [];
  const original = content.split('\n');
  const lines = stripComments(content).split('\n');
  lines.forEach((line, i) => {
    if (FORBIDDEN_PATTERNS.some((re) => re.test(line))) {
      findings.push({ line: i + 1, text: (original[i] ?? line).trim() });
    }
  });
  return findings;
}

function collectSources(dir: string, out: string[] = []): string[] {
  for (const entry of readdirSync(dir)) {
    if (entry === 'node_modules') continue;
    const full = join(dir, entry);
    if (statSync(full).isDirectory()) {
      collectSources(full, out);
      continue;
    }
    if (!/\.(ts|svelte)$/.test(entry)) continue;
    if (/\.(test|spec)\.ts$/.test(entry)) continue;
    out.push(full);
  }
  return out;
}

function toRel(file: string): string {
  return relative(SRC_DIR, file).split(sep).join('/');
}

describe('kernelGuard — единый источник состояния ядра (ISOL-04)', () => {
  it('нет прямых обращений к полю активного ядра вне allowlist', () => {
    const files = collectSources(SRC_DIR);
    expect(files.length).toBeGreaterThan(50);

    const violations: string[] = [];
    for (const file of files) {
      const rel = toRel(file);
      if (rel in ALLOWLIST) continue;
      for (const f of findForbidden(readFileSync(file, 'utf8'))) {
        violations.push(`${rel}:${f.line}: ${f.text}`);
      }
    }
    expect(violations, `${HINT}\n${violations.join('\n')}`).toEqual([]);
  });

  it('сканер ловит образец: чтение поля через $capabilities', () => {
    expect(findForbidden("const k = $capabilities.active_kernel === 'xray';")).toHaveLength(1);
    expect(findForbidden('{#if $capabilities?.activeKernel}x{/if}')).toHaveLength(1);
    expect(
      findForbidden('const a = caps.active_kernel;\nconst b = 1;\nlet c = r.activeKernel;')
    ).toMatchObject([{ line: 1 }, { line: 3 }]);
  });

  it('сканер не трогает объектный ключ, пропс без точки и комментарии', () => {
    expect(findForbidden("const p = { active_kernel: '' };")).toHaveLength(0);
    expect(findForbidden('let { activeKernel }: Props = $props();')).toHaveLength(0);
    expect(findForbidden('<Card activeKernel={kernel} />')).toHaveLength(0);
    expect(findForbidden('// прежде читали $capabilities.active_kernel')).toHaveLength(0);
    expect(findForbidden('/* $capabilities.active_kernel */\nconst x = 1;')).toHaveLength(0);
    expect(findForbidden('<!-- $capabilities.activeKernel -->')).toHaveLength(0);
  });

  it('allowlist не устарел: каждый файл существует', () => {
    for (const rel of Object.keys(ALLOWLIST)) {
      expect(existsSync(join(SRC_DIR, rel)), `в allowlist нет файла src/${rel}`).toBe(true);
    }
  });
});
