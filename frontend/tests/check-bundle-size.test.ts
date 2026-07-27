import { describe, it, expect } from 'vitest';
import checkBundleSize from '../scripts/check-bundle-size.cjs';

const { BUDGET_BYTES, findEntryChunk, gzipSize, formatKb } = checkBundleSize;

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
});
