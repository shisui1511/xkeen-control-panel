import { describe, it, expect, beforeEach, vi, afterEach } from 'vitest';
import { get } from 'svelte/store';
import {
  isServiceRestarting,
  activateRestartGrace,
  clearRestartGrace
} from '../src/lib/serviceGrace';

describe('serviceGrace', () => {
  beforeEach(() => {
    vi.useFakeTimers();
    clearRestartGrace();
  });

  afterEach(() => {
    clearRestartGrace();
    vi.useRealTimers();
  });

  it('starts inactive', () => {
    expect(get(isServiceRestarting)).toBe(false);
  });

  it('activates grace period and resets automatically after timeout', () => {
    activateRestartGrace(3000);
    expect(get(isServiceRestarting)).toBe(true);

    vi.advanceTimersByTime(2999);
    expect(get(isServiceRestarting)).toBe(true);

    vi.advanceTimersByTime(2);
    expect(get(isServiceRestarting)).toBe(false);
  });

  it('resets timer when re-activated', () => {
    activateRestartGrace(3000);
    expect(get(isServiceRestarting)).toBe(true);

    vi.advanceTimersByTime(2000);
    expect(get(isServiceRestarting)).toBe(true);

    // Re-activate with 4000ms
    activateRestartGrace(4000);
    vi.advanceTimersByTime(2500); // 4500ms total, but 2500ms since re-activation
    expect(get(isServiceRestarting)).toBe(true);

    vi.advanceTimersByTime(1600);
    expect(get(isServiceRestarting)).toBe(false);
  });

  it('clears grace period immediately when clearRestartGrace is called', () => {
    activateRestartGrace(5000);
    expect(get(isServiceRestarting)).toBe(true);

    clearRestartGrace();
    expect(get(isServiceRestarting)).toBe(false);

    // Advancing timers should not cause state change or error
    vi.advanceTimersByTime(6000);
    expect(get(isServiceRestarting)).toBe(false);
  });
});
