import { describe, it, expect, vi, beforeEach } from 'vitest';
import { formatTimeAgo, BatchLatencyTester } from '../src/lib/batchLatencyTester';

describe('formatTimeAgo', () => {
  const mockT = (key: string, params?: any) => {
    if (key === 'time.just_now') return 'только что';
    if (key === 'time.minutes_ago') return `${params?.count} мин назад`;
    if (key === 'time.hours_ago') return `${params?.count} ч назад`;
    if (key === 'time.days_ago') return `${params?.count} д назад`;
    return key;
  };

  it('formats recent timestamps as just now', () => {
    const now = Date.now();
    expect(formatTimeAgo(now - 10000, mockT)).toBe('только что');
    expect(formatTimeAgo(now - 55000, mockT)).toBe('только что');
  });

  it('formats minute intervals correctly', () => {
    const now = Date.now();
    expect(formatTimeAgo(now - 2 * 60 * 1000, mockT)).toBe('2 мин назад');
    expect(formatTimeAgo(now - 15 * 60 * 1000, mockT)).toBe('15 мин назад');
  });

  it('formats hours intervals correctly', () => {
    const now = Date.now();
    expect(formatTimeAgo(now - 3 * 3600 * 1000, mockT)).toBe('3 ч назад');
  });

  it('formats days intervals correctly', () => {
    const now = Date.now();
    expect(formatTimeAgo(now - 2 * 86400 * 1000, mockT)).toBe('2 д назад');
  });
});

describe('BatchLatencyTester', () => {
  let tester: BatchLatencyTester;

  beforeEach(() => {
    tester = new BatchLatencyTester();
    vi.restoreAllMocks();
  });

  it('initializes in inactive state', () => {
    expect(tester.isActive()).toBe(false);
  });

  it('cancels gracefully during execution', () => {
    tester.cancel();
    expect(tester.isActive()).toBe(false);
  });
});
