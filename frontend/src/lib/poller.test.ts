import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { usePoller } from './poller';

describe('usePoller', () => {
  beforeEach(() => {
    vi.useFakeTimers();
  });

  afterEach(() => {
    vi.useRealTimers();
    vi.restoreAllMocks();
  });

  it('triggers immediate poll execution by default', async () => {
    const pollFn = vi.fn().mockResolvedValue(undefined);
    const poller = usePoller(pollFn, 1000);

    expect(pollFn).toHaveBeenCalledTimes(1);
    poller.stop();
  });

  it('respects immediate: false option', () => {
    const pollFn = vi.fn().mockResolvedValue(undefined);
    const poller = usePoller(pollFn, 1000, { immediate: false });

    expect(pollFn).not.toHaveBeenCalled();
    poller.stop();
  });

  it('schedules subsequent polls according to activeIntervalMs', async () => {
    const pollFn = vi.fn().mockResolvedValue(undefined);
    const poller = usePoller(pollFn, 1000);

    expect(pollFn).toHaveBeenCalledTimes(1);

    await vi.advanceTimersByTimeAsync(1000);
    expect(pollFn).toHaveBeenCalledTimes(2);

    await vi.advanceTimersByTimeAsync(1000);
    expect(pollFn).toHaveBeenCalledTimes(3);

    poller.stop();
  });

  it('applies exponential backoff up to 60000ms on errors', async () => {
    const pollFn = vi.fn().mockRejectedValue(new Error('Network error'));
    const poller = usePoller(pollFn, 1000);

    expect(pollFn).toHaveBeenCalledTimes(1); // initial execution

    // 1st error -> backoff becomes min(1000 * 2, 60000) = 2000ms
    await vi.advanceTimersByTimeAsync(1000);
    expect(pollFn).toHaveBeenCalledTimes(1); // not called yet at 1000ms

    await vi.advanceTimersByTimeAsync(1000);
    expect(pollFn).toHaveBeenCalledTimes(2); // called at 2000ms

    // 2nd error -> backoff becomes 4000ms
    await vi.advanceTimersByTimeAsync(4000);
    expect(pollFn).toHaveBeenCalledTimes(3);

    poller.stop();
  });

  it('resets backoff to activeIntervalMs on successful poll', async () => {
    let callCount = 0;
    const pollFn = vi.fn().mockImplementation(async () => {
      callCount++;
      if (callCount === 1) throw new Error('Network error');
      return undefined;
    });

    const poller = usePoller(pollFn, 1000);

    // Call 1 fails -> backoff 2000ms
    await vi.advanceTimersByTimeAsync(2000);
    expect(pollFn).toHaveBeenCalledTimes(2); // Call 2 succeeds

    // Next schedule should be 1000ms (reset backoff)
    await vi.advanceTimersByTimeAsync(1000);
    expect(pollFn).toHaveBeenCalledTimes(3);

    poller.stop();
  });

  it('passes AbortSignal to pollFn and aborts on stop()', async () => {
    let capturedSignal: AbortSignal | null = null;
    const pollFn = vi.fn().mockImplementation(async (signal: AbortSignal) => {
      capturedSignal = signal;
    });

    const poller = usePoller(pollFn, 1000);
    expect(capturedSignal).not.toBeNull();
    expect(capturedSignal?.aborted).toBe(false);

    poller.stop();
    expect(capturedSignal?.aborted).toBe(true);
  });

  it('stops polling on 401 error', async () => {
    const pollFn = vi.fn().mockRejectedValue({ status: 401, message: 'Unauthorized' });
    const poller = usePoller(pollFn, 1000);

    expect(pollFn).toHaveBeenCalledTimes(1);

    await vi.advanceTimersByTimeAsync(5000);
    expect(pollFn).toHaveBeenCalledTimes(1); // No more calls

    poller.stop();
  });
});
