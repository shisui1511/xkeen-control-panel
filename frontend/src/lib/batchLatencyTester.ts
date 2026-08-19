import { apiFetch } from './api';
import { getCountryFlag } from './countryFlags';

export interface BatchProgressState {
  running: boolean;
  current: number;
  total: number;
  currentNode: string;
  currentNodeFlag: string;
  retrying: boolean;
  retrySeconds: number;
  targetUrl: string;
  timeoutMs: number;
}

export interface BatchRunOptions {
  nodes: string[];
  targetUrl: string;
  timeoutMs: number;
  onNodeStart?: (node: string) => void;
  onNodeComplete: (
    node: string,
    delay: number | null,
    rawHistoryItem?: { time: string; delay: number }
  ) => void;
  onProgressChange: (state: BatchProgressState) => void;
}

export function formatTimeAgo(
  timestampMs: number,
  t: (key: string, params?: any) => string
): string {
  if (!timestampMs || isNaN(timestampMs)) return t('time.just_now');
  const diffSec = Math.max(0, Math.floor((Date.now() - timestampMs) / 1000));
  if (diffSec < 60) return t('time.just_now');
  const diffMin = Math.floor(diffSec / 60);
  if (diffMin < 60) return t('time.minutes_ago', { count: diffMin });
  const diffHours = Math.floor(diffMin / 60);
  if (diffHours < 24) return t('time.hours_ago', { count: diffHours });
  const diffDays = Math.floor(diffHours / 24);
  return t('time.days_ago', { count: diffDays });
}

export class BatchLatencyTester {
  private abortController: AbortController | null = null;
  private isRunning = false;

  async run(options: BatchRunOptions): Promise<void> {
    if (this.isRunning) {
      this.cancel();
    }
    this.isRunning = true;
    this.abortController = new AbortController();
    const signal = this.abortController.signal;

    const { nodes, targetUrl, timeoutMs, onNodeStart, onNodeComplete, onProgressChange } = options;

    const progress: BatchProgressState = {
      running: true,
      current: 0,
      total: nodes.length,
      currentNode: '',
      currentNodeFlag: '',
      retrying: false,
      retrySeconds: 0,
      targetUrl,
      timeoutMs
    };

    onProgressChange({ ...progress });

    try {
      for (let i = 0; i < nodes.length; i++) {
        if (signal.aborted) break;

        const node = nodes[i];
        progress.current = i + 1;
        progress.currentNode = node;
        progress.currentNodeFlag = getCountryFlag(node);
        progress.retrying = false;
        progress.retrySeconds = 0;
        onProgressChange({ ...progress });

        if (onNodeStart) {
          onNodeStart(node);
        }

        await this.testSingleNodeWithRetry(
          node,
          targetUrl,
          timeoutMs,
          signal,
          progress,
          onProgressChange,
          onNodeComplete
        );
      }
    } finally {
      this.isRunning = false;
      this.abortController = null;
      progress.running = false;
      progress.retrying = false;
      onProgressChange({ ...progress });
    }
  }

  private async sleep(ms: number, signal: AbortSignal): Promise<void> {
    return new Promise<void>((resolve, reject) => {
      if (signal.aborted) {
        return reject(new DOMException('Aborted', 'AbortError'));
      }
      const timer = setTimeout(() => {
        signal.removeEventListener('abort', onAbort);
        resolve();
      }, ms);
      const onAbort = () => {
        clearTimeout(timer);
        signal.removeEventListener('abort', onAbort);
        reject(new DOMException('Aborted', 'AbortError'));
      };
      signal.addEventListener('abort', onAbort);
    });
  }

  private async testSingleNodeWithRetry(
    node: string,
    targetUrl: string,
    timeoutMs: number,
    signal: AbortSignal,
    progress: BatchProgressState,
    onProgressChange: (state: BatchProgressState) => void,
    onNodeComplete: (
      node: string,
      delay: number | null,
      rawHistoryItem?: { time: string; delay: number }
    ) => void
  ): Promise<void> {
    const maxRetries = 2;
    let attempt = 0;

    while (attempt <= maxRetries && !signal.aborted) {
      attempt++;
      const url = `/api/mihomo/proxy/proxies/${encodeURIComponent(node)}/delay?url=${encodeURIComponent(targetUrl)}&timeout=${timeoutMs}`;

      try {
        const res = await apiFetch(url, { signal, skip401Redirect: false });

        if (res.status === 429) {
          if (attempt > maxRetries) {
            onNodeComplete(node, 0, {
              time: new Date().toISOString(),
              delay: 0
            });
            return;
          }

          let retrySec = 2;
          const retryHeader = res.headers.get('Retry-After');
          if (retryHeader) {
            const parsed = parseInt(retryHeader, 10);
            if (!isNaN(parsed) && parsed > 0) {
              retrySec = parsed;
            }
          } else {
            try {
              const body = await res.clone().json();
              if (body && typeof body.retry_after === 'number') {
                retrySec = body.retry_after;
              }
            } catch {
              // fallback to 2s
            }
          }

          progress.retrying = true;
          progress.retrySeconds = retrySec;
          onProgressChange({ ...progress });

          await this.sleep(retrySec * 1000, signal);
          progress.retrying = false;
          onProgressChange({ ...progress });
          continue;
        }

        if (res.ok) {
          const data = await res.json();
          const delay = typeof data?.delay === 'number' ? data.delay : 0;
          const historyItem = {
            time: new Date().toISOString(),
            delay
          };
          onNodeComplete(node, delay, historyItem);
          return;
        } else {
          onNodeComplete(node, 0, {
            time: new Date().toISOString(),
            delay: 0
          });
          return;
        }
      } catch (err: any) {
        if (err?.name === 'AbortError' || signal.aborted) {
          return;
        }
        onNodeComplete(node, 0, {
          time: new Date().toISOString(),
          delay: 0
        });
        return;
      }
    }
  }

  cancel(): void {
    if (this.abortController) {
      this.abortController.abort();
      this.abortController = null;
    }
    this.isRunning = false;
  }

  isActive(): boolean {
    return this.isRunning;
  }
}
