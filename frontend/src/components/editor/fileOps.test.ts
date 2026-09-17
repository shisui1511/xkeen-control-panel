import { describe, it, expect } from 'vitest';
import { formatBytes } from './fileOps';

describe('formatBytes', () => {
  it('formats zero and negative bytes as 0 B', () => {
    expect(formatBytes(0)).toBe('0 B');
    expect(formatBytes(-100)).toBe('0 B');
    expect(formatBytes(NaN)).toBe('0 B');
  });

  it('formats bytes, KB, MB correctly', () => {
    expect(formatBytes(500)).toBe('500 B');
    expect(formatBytes(1024)).toBe('1 KB');
    expect(formatBytes(1024 * 1024 * 2.5)).toBe('2.5 MB');
  });

  it('formats GB and TB correctly without undefined', () => {
    expect(formatBytes(1024 * 1024 * 1024 * 1.5)).toBe('1.5 GB');
    expect(formatBytes(1024 * 1024 * 1024 * 1024 * 3)).toBe('3 TB');
  });
});
