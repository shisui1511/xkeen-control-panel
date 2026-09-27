import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { get } from 'svelte/store';
import { panelUnreachable } from '../stores';
import {
  panelNewVersion,
  reportPanelUnreachable,
  rememberPanelVersion,
  __resetPanelHealthForTests
} from './panelHealth';

function jsonResponse(body: unknown, ok = true): Response {
  return {
    ok,
    json: () => Promise.resolve(body)
  } as unknown as Response;
}

let fetchMock: ReturnType<typeof vi.fn>;

beforeEach(() => {
  vi.useFakeTimers();
  __resetPanelHealthForTests();
  fetchMock = vi.fn();
  vi.stubGlobal('fetch', fetchMock);
});

afterEach(() => {
  __resetPanelHealthForTests();
  vi.useRealTimers();
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

describe('panelHealth', () => {
  it('reportPanelUnreachable() sets panelUnreachable and a repeat call does not start a second poll cycle', async () => {
    fetchMock.mockRejectedValue(new TypeError('Failed to fetch'));

    reportPanelUnreachable();
    expect(get(panelUnreachable)).toBe(true);

    reportPanelUnreachable(); // idempotent — must not schedule a second timer

    await vi.advanceTimersByTimeAsync(2000);
    expect(fetchMock).toHaveBeenCalledTimes(1);
  });

  it('retries with the 2s/4s/8s/16s/30s/30s backoff ladder while probes keep failing', async () => {
    fetchMock.mockRejectedValue(new TypeError('Failed to fetch'));

    reportPanelUnreachable();
    expect(fetchMock).toHaveBeenCalledTimes(0);

    await vi.advanceTimersByTimeAsync(2000);
    expect(fetchMock).toHaveBeenCalledTimes(1);

    await vi.advanceTimersByTimeAsync(4000);
    expect(fetchMock).toHaveBeenCalledTimes(2);

    await vi.advanceTimersByTimeAsync(8000);
    expect(fetchMock).toHaveBeenCalledTimes(3);

    await vi.advanceTimersByTimeAsync(16000);
    expect(fetchMock).toHaveBeenCalledTimes(4);

    await vi.advanceTimersByTimeAsync(30000);
    expect(fetchMock).toHaveBeenCalledTimes(5);

    // Ladder caps at 30s from here on.
    await vi.advanceTimersByTimeAsync(30000);
    expect(fetchMock).toHaveBeenCalledTimes(6);
  });

  it('a successful probe clears panelUnreachable without setting panelNewVersion', async () => {
    rememberPanelVersion('v1.0.0');
    fetchMock.mockResolvedValueOnce(jsonResponse({ panel_version: 'v1.0.0' }));

    reportPanelUnreachable();
    await vi.advanceTimersByTimeAsync(2000);

    expect(get(panelUnreachable)).toBe(false);
    expect(get(panelNewVersion)).toBeNull();
  });

  it('a successful probe with a different panel_version sets panelNewVersion', async () => {
    rememberPanelVersion('v1.0.0');
    fetchMock.mockResolvedValueOnce(jsonResponse({ panel_version: 'v1.0.1' }));

    reportPanelUnreachable();
    await vi.advanceTimersByTimeAsync(2000);

    expect(get(panelUnreachable)).toBe(false);
    expect(get(panelNewVersion)).toBe('v1.0.1');
  });

  it('rememberPanelVersion only remembers the first non-empty value', () => {
    rememberPanelVersion('v1.0.0');
    rememberPanelVersion('v2.0.0');
    // First-remembered baseline stays v1.0.0 — a later probe reporting
    // v2.0.0 must now read as "changed" relative to that original baseline.
    fetchMock.mockResolvedValueOnce(jsonResponse({ panel_version: 'v2.0.0' }));

    reportPanelUnreachable();
    return vi.advanceTimersByTimeAsync(2000).then(() => {
      expect(get(panelNewVersion)).toBe('v2.0.0');
    });
  });
});
