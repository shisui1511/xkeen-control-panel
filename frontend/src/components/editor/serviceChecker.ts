import { apiFetch } from '../../lib/api';
import { showToast } from '../../stores';
import { t } from '../../i18n';
import { get } from 'svelte/store';

export interface ServiceCheckCallbacks {
  onStatusChange: (text: string) => void;
  onSuccess: () => void;
  onError: () => void;
}

export function startServiceStatusPolling(callbacks: ServiceCheckCallbacks): () => void {
  let attempts = 0;
  let cancelled = false;
  let isFetching = false;
  const maxAttempts = 12;
  const intervalTime = 1500;
  const $t = get(t);

  callbacks.onStatusChange(`${$t('editor.checking_status')} (1/${maxAttempts})`);

  const interval = setInterval(async () => {
    if (cancelled || isFetching) return;
    isFetching = true;
    attempts++;
    callbacks.onStatusChange(`${$t('editor.checking_status')} (${attempts}/${maxAttempts})`);

    try {
      const res = await apiFetch('/api/service/status');
      if (cancelled) return;
      if (res.ok) {
        const parsed = await res.json().catch(() => null);
        if (cancelled) return;
        if (parsed && parsed.success && parsed.data && parsed.data.is_running === true) {
          clearInterval(interval);
          if (!cancelled) {
            showToast('success', $t('editor.apply_success'));
            callbacks.onSuccess();
          }
          return;
        }
      }
    } catch (err: any) {
      if (err?.status === 401 || cancelled) {
        clearInterval(interval);
        return;
      }
    } finally {
      isFetching = false;
    }

    if (attempts >= maxAttempts && !cancelled) {
      clearInterval(interval);
      showToast('error', $t('editor.apply_timeout'));
      callbacks.onError();
    }
  }, intervalTime);

  return () => {
    cancelled = true;
    clearInterval(interval);
  };
}
