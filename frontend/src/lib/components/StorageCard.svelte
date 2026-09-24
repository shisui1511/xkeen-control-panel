<script lang="ts">
  import { onMount } from 'svelte';
  import { t } from '../../i18n';
  import { formatBytes } from '../format';
  import { usePoller } from '../poller';
  import { apiFetch } from '../api';

  let diskStats = $state<{ total: number; used: number; free: number } | null>(null);

  async function fetchDiskStats(signal?: AbortSignal) {
    try {
      const res = await apiFetch('/api/system/stats', { signal });
      if (res.ok) {
        const data = await res.json();
        if (data && data.disk) {
          diskStats = data.disk;
        }
      }
    } catch (e: any) {
      if (e?.name === 'AbortError' || e?.status === 401) {
        return;
      }
      console.error('Failed to fetch disk stats:', e);
    }
  }

  onMount(() => {
    usePoller((signal) => fetchDiskStats(signal), 10000);
  });
</script>

{#if diskStats}
  {@const usedPercent = Math.min(
    100,
    Math.max(0, Math.round((diskStats.used / diskStats.total) * 100))
  )}
  {@const isLowSpace = diskStats.free < 10 * 1024 * 1024}
  {@const barColor =
    usedPercent > 90 || isLowSpace
      ? 'var(--danger)'
      : usedPercent >= 80
        ? 'var(--warning)'
        : 'var(--success)'}

  <div class="card mb-2">
    <div class="storage-label">{$t('settings.section_storage')}</div>
    <div class="storage-body">
      <div class="storage-head">
        <span class="storage-free">
          {$t('settings.storage_free_of')
            .replace('{free}', formatBytes(diskStats.free))
            .replace('{total}', formatBytes(diskStats.total))}
        </span>
        <span class="storage-percent">{usedPercent}%</span>
      </div>
      <div
        class="storage-track"
        role="progressbar"
        aria-valuenow={usedPercent}
        aria-valuemin="0"
        aria-valuemax="100"
        aria-label="{$t('settings.storage')}: {usedPercent}%"
      >
        <div class="storage-bar" style:width="{usedPercent}%" style:background={barColor}></div>
      </div>
      <div class="storage-used">
        {$t('settings.storage_used').replace('{used}', formatBytes(diskStats.used))}
      </div>
    </div>
  </div>
{/if}

<style>
  .storage-label {
    font-size: 12px;
    font-weight: 600;
    letter-spacing: 0.04em;
    color: var(--fg-dim);
    margin-bottom: 14px;
  }

  .storage-body {
    display: flex;
    flex-direction: column;
    gap: 8px;
  }

  .storage-head {
    display: flex;
    justify-content: space-between;
    gap: 12px;
    font-size: 14px;
    font-weight: 500;
    color: var(--fg-primary);
  }

  .storage-percent {
    color: var(--fg-secondary);
    font-family: var(--font-family-mono);
  }

  .storage-track {
    height: 8px;
    width: 100%;
    overflow: hidden;
    border-radius: var(--radius-sm);
    background: var(--border);
  }

  .storage-bar {
    height: 100%;
    border-radius: var(--radius-sm);
    transition: width 0.3s ease;
  }

  .storage-used {
    font-size: 12px;
    color: var(--fg-secondary);
  }
</style>
