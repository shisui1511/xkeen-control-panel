<script lang="ts">
  import { onMount, onDestroy } from 'svelte';
  import { t } from '../../i18n';
  import { formatBytes } from '../format';

  let diskStats = $state<{ total: number; used: number; free: number } | null>(null);

  async function fetchDiskStats() {
    try {
      const res = await fetch('/api/system/stats');
      if (res.ok) {
        const data = await res.json();
        if (data && data.disk) {
          diskStats = data.disk;
        }
      }
    } catch (e) {
      console.error('Failed to fetch disk stats:', e);
    }
  }

  let diskInterval: any;

  onMount(() => {
    fetchDiskStats();
    diskInterval = setInterval(fetchDiskStats, 10000);
  });

  onDestroy(() => {
    if (diskInterval) {
      clearInterval(diskInterval);
    }
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
      ? 'var(--color-danger, #e74c3c)'
      : usedPercent >= 80
        ? 'var(--color-warning, #f39c12)'
        : 'var(--color-success, var(--color-primary, #2ecc71))'}

  <div class="card mb-2">
    <div class="card-label">{$t('settings.section_storage')}</div>
    <div class="field-group">
      <div class="field-row" style="flex-direction: column; align-items: stretch; gap: 8px;">
        <div
          style="display: flex; justify-content: space-between; font-size: 14px; font-weight: 500;"
        >
          <span style="color: var(--fg-secondary);">{$t('settings.section_storage')}</span>
          <span style="color: var(--fg-primary);">
            {$t('settings.storage_free_of')
              .replace('{free}', formatBytes(diskStats.free))
              .replace('{total}', formatBytes(diskStats.total))}
          </span>
        </div>

        <div
          class="progress-container"
          style="background-color: var(--bg-tertiary, #2c2c2e); height: 8px; border-radius: var(--radius-sm, 4px); overflow: hidden; width: 100%;"
        >
          <div
            class="progress-bar"
            style="width: {usedPercent}%; height: 100%; background-color: {barColor}; transition: width 0.3s ease; border-radius: var(--radius-sm, 4px);"
            title="{usedPercent}%"
            role="progressbar"
            aria-valuenow={usedPercent}
            aria-valuemin="0"
            aria-valuemax="100"
          ></div>
        </div>

        <div
          style="display: flex; justify-content: space-between; font-size: 12px; color: var(--fg-muted);"
        >
          <span>
            {$t('settings.storage_used').replace('{used}', formatBytes(diskStats.used))}
          </span>
          <span>{usedPercent}%</span>
        </div>
      </div>
    </div>
  </div>
{/if}
