import { describe, it, expect } from 'vitest';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import checkBundleSize from '../scripts/check-bundle-size.cjs';

const {
  BUDGET_BYTES,
  findEntryChunk,
  gzipSize,
  formatKb,
  checkNoExternalResources,
  checkNoInlineScripts,
  checkThemeInitPresent,
  checkWoff2Only
} = checkBundleSize;

describe('Bundle Size Gate', () => {
  it('findEntryChunk returns the record with isEntry: true, ignoring async chunks', () => {
    const manifest = {
      'src/main.ts': { file: 'assets/index-abc123.js', isEntry: true },
      'src/Logs.svelte': { file: 'assets/Logs-def456.js' },
      'src/Proxies.svelte': { file: 'assets/Proxies-ghi789.js', isEntry: false }
    };
    const entry = findEntryChunk(manifest);
    expect(entry).not.toBeNull();
    expect(entry.file).toBe('assets/index-abc123.js');
  });

  it('findEntryChunk returns null when no record has isEntry: true', () => {
    const manifest = {
      'src/Logs.svelte': { file: 'assets/Logs-def456.js' },
      'src/Proxies.svelte': { file: 'assets/Proxies-ghi789.js', isEntry: false }
    };
    expect(findEntryChunk(manifest)).toBeNull();
  });

  it('gzipSize returns a positive number smaller than the input for compressible data', () => {
    const buffer = Buffer.alloc(10000, 'a');
    const size = gzipSize(buffer);
    expect(size).toBeGreaterThan(0);
    expect(size).toBeLessThan(buffer.length);
  });

  it('gzipSize is deterministic across repeated calls on the same buffer', () => {
    const buffer = Buffer.alloc(10000, 'a');
    expect(gzipSize(buffer)).toBe(gzipSize(buffer));
  });

  it('formatKb formats bytes as KB with two decimal places', () => {
    expect(formatKb(204800)).toBe('200.00');
  });

  it('BUDGET_BYTES equals 204800 (200 KB)', () => {
    expect(BUDGET_BYTES).toBe(204800);
  });

  it('checkNoExternalResources finds a violation for an absolute https:// link href', () => {
    const violations = checkNoExternalResources('<link href="https://example.com/x.css" />');
    expect(violations).toEqual(['https://example.com/x.css']);
  });

  it('checkNoExternalResources finds a violation for a scheme-relative // script src', () => {
    const violations = checkNoExternalResources('<script src="//example.com/x.js"></script>');
    expect(violations).toEqual(['//example.com/x.js']);
  });

  it('checkNoExternalResources ignores relative and data: URIs', () => {
    const html = `
      <link rel="manifest" href="./manifest.json" />
      <link rel="stylesheet" href="/assets/index.css" />
      <link rel="icon" href="data:image/svg+xml,%3Csvg%3E%3C/svg%3E" />
    `;
    expect(checkNoExternalResources(html)).toEqual([]);
  });

  it('checkWoff2Only finds a violation for a truetype src inside @font-face', () => {
    const css = `
      @font-face {
        font-family: 'Manrope';
        src: url('/fonts/a.ttf') format('truetype');
      }
    `;
    expect(checkWoff2Only(css)).toEqual(['/fonts/a.ttf']);
  });

  it('checkWoff2Only returns an empty array for a woff2-only src', () => {
    const css = `
      @font-face {
        font-family: 'Manrope';
        src: url('/fonts/a.woff2') format('woff2');
      }
    `;
    expect(checkWoff2Only(css)).toEqual([]);
  });

  it('checkNoInlineScripts accepts only scripts with src and an empty body', () => {
    const ok =
      '<script src="./theme-init.js"></script><script type="module" crossorigin src="./assets/index-x.js"></script>';
    expect(checkNoInlineScripts(ok)).toEqual([]);
  });

  it('checkNoInlineScripts flags an inline script without src', () => {
    expect(
      checkNoInlineScripts('<script>document.documentElement.dataset.theme="dark"</script>')
    ).toHaveLength(1);
  });

  it('checkNoInlineScripts flags an inline script whose end tag has a space or attributes', () => {
    expect(checkNoInlineScripts('<script>alert(1)</script >')).toHaveLength(1);
    expect(checkNoInlineScripts('<script>alert(1)</script foo="bar">')).toHaveLength(1);
  });

  it('checkNoInlineScripts flags a module script with an inline body', () => {
    expect(checkNoInlineScripts('<script type="module">import "x"</script>')).toHaveLength(1);
  });

  it('checkThemeInitPresent requires theme-init.js and a reference to it in index.html', () => {
    const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'theme-init-'));
    try {
      expect(checkThemeInitPresent(dir)).toBe(false);
      fs.writeFileSync(path.join(dir, 'index.html'), '<script src="/theme-init.js"></script>');
      expect(checkThemeInitPresent(dir)).toBe(false);
      fs.writeFileSync(path.join(dir, 'theme-init.js'), '');
      expect(checkThemeInitPresent(dir)).toBe(true);
    } finally {
      fs.rmSync(dir, { recursive: true, force: true });
    }
  });
});
