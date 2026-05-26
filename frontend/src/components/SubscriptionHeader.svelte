<script lang="ts">
  import { t, currentLang } from '../i18n';

  export let sub: {
    id: string;
    name: string;
    enabled: boolean;
    last_error?: string;
    upload?: number;
    download?: number;
    total?: number;
    expire?: number;
    last_update?: string;
    interval: number;
    proxy_count?: number;
    rule_count?: number;
    type?: string;
  };
  export let onEdit: (() => void) | undefined = undefined;
  export let isDetail = false;

  function formatTraffic(bytes: number): string {
    const gb = bytes / (1024 * 1024 * 1024);
    if (gb >= 1) return `${gb.toFixed(1)} GB`;
    const mb = bytes / (1024 * 1024);
    return `${mb.toFixed(1)} MB`;
  }

  $: used = (sub.upload || 0) + (sub.download || 0);
  $: percent = sub.total && sub.total > 0 ? Math.min(100, Math.round((used / sub.total) * 100)) : 0;

  $: expireDays = (() => {
    if (!sub.expire || sub.expire <= 0) return null;
    const diff = sub.expire * 1000 - Date.now();
    if (diff <= 0) return { expired: true, text: $t('subscr.expired') };
    const days = Math.ceil(diff / (24 * 3600 * 1000));
    return {
      expired: false,
      days,
      text: $t('subscr.expires_in').replace('{days}', String(days))
    };
  })();

  $: nextUpdate = (() => {
    if (!sub.enabled || !sub.last_update || sub.last_update.startsWith('0001')) return null;
    const nextTime = new Date(sub.last_update).getTime() + sub.interval * 3600 * 1000;
    const diff = nextTime - Date.now();
    if (diff <= 0) return null;
    const hours = Math.floor(diff / (3600 * 1000));
    const mins = Math.floor((diff % (3600 * 1000)) / (60 * 1000));
    return $currentLang === 'ru' ? `${hours}ч ${mins}м` : `${hours}h ${mins}m`;
  })();
</script>

<div class="sub-header" class:detail-mode={isDetail}>
  <div class="title-row">
    <div class="name-container">
      <h2 class="sub-name">{sub.name}</h2>
      {#if onEdit}
        <button class="edit-btn" on:click={onEdit} title={$t('app.edit')}>
          <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5">
            <path d="M11 4H4a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h14a2 2 0 0 0 2-2v-7M18.5 2.5a2.121 2.121 0 1 1 3 3L12 15l-4 1 1-4 9.5-9.5z"/>
          </svg>
        </button>
      {/if}
      <span class="nodes-badge">
        {$t('subscr.nodes_count').replace('{count}', String(sub.proxy_count || 0))}
      </span>
      {#if sub.type === 'mihomo' && sub.rule_count && sub.rule_count > 0}
        <span class="rules-badge">
          {$t('subscr.rules_count').replace('{count}', String(sub.rule_count))}
        </span>
      {/if}
    </div>

    <div class="status-container">
      {#if sub.last_error}
        <span class="chip chip-danger chip--dot" title={sub.last_error}>
          {$currentLang === 'ru' ? 'ошибка' : 'error'}
        </span>
      {:else if sub.enabled}
        <span class="chip chip-success chip--dot">
          {$currentLang === 'ru' ? 'активна' : 'active'}
        </span>
      {:else}
        <span class="chip chip-danger chip--dot">
          {$currentLang === 'ru' ? 'выключена' : 'disabled'}
        </span>
      {/if}

      {#if expireDays}
        {#if expireDays.expired}
          <span class="chip chip-danger">
            {expireDays.text}
          </span>
        {:else if expireDays.days !== null && expireDays.days <= 5}
          <span class="chip chip-warning">
            {expireDays.text}
          </span>
        {:else}
          <span class="chip chip-default">
            {expireDays.text}
          </span>
        {/if}
      {/if}
    </div>
  </div>

  <!-- Панель трафика -->
  {#if sub.total && sub.total > 0}
    <div class="traffic-section">
      <div class="traffic-labels">
        <span class="traffic-label">{$t('trafficquotas.limit')}</span>
        <span class="traffic-value">
          {formatTraffic(used)} / {formatTraffic(sub.total)} ({percent}%)
        </span>
      </div>
      <div class="progress-container">
        <div class="progress-bar" style="width: {percent}%" class:warning={percent >= 80} class:danger={percent >= 95}></div>
      </div>
    </div>
  {:else if used > 0}
    <div class="traffic-section">
      <div class="traffic-labels">
        <span class="traffic-label">{$t('trafficquotas.limit')}</span>
        <span class="traffic-value">
          {formatTraffic(used)} / ∞ ({$t('subscr.infinite_traffic')})
        </span>
      </div>
      <div class="progress-container">
        <div class="progress-bar infinite" style="width: 100%"></div>
      </div>
    </div>
  {/if}

  {#if nextUpdate}
    <div class="refresh-timer">
      <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" class="timer-icon">
        <circle cx="12" cy="12" r="10"/><polyline points="12 6 12 12 16 14"/>
      </svg>
      <span>
        {$currentLang === 'ru' ? 'след. обновление через' : 'next update in'} <b>{nextUpdate}</b>
      </span>
    </div>
  {/if}
</div>

<style>
  .sub-header {
    display: flex;
    flex-direction: column;
    gap: 12px;
    width: 100%;
  }

  .title-row {
    display: flex;
    justify-content: space-between;
    align-items: center;
    flex-wrap: wrap;
    gap: 10px;
  }

  .name-container {
    display: flex;
    align-items: center;
    gap: 8px;
    flex-wrap: wrap;
  }

  .sub-name {
    margin: 0;
    font-size: 15px;
    font-weight: 600;
    color: var(--fg-primary);
  }

  .edit-btn {
    background: none;
    border: none;
    padding: 4px;
    color: var(--fg-dim);
    cursor: pointer;
    border-radius: 4px;
    display: flex;
    align-items: center;
    justify-content: center;
    opacity: 0;
    transition: opacity var(--transition-fast), color var(--transition-fast), background var(--transition-fast);
  }

  .title-row:hover .edit-btn,
  .edit-btn:focus {
    opacity: 1;
  }

  .edit-btn:hover {
    color: var(--accent);
    background: rgba(255, 255, 255, 0.04);
  }

  .nodes-badge,
  .rules-badge {
    font-size: 10px;
    background: rgba(255, 255, 255, 0.04);
    border: 1px solid var(--border);
    border-radius: 4px;
    padding: 1px 5px;
    font-family: var(--font-family-mono);
    color: var(--fg-secondary);
  }

  .status-container {
    display: flex;
    align-items: center;
    gap: 6px;
  }

  /* Секция трафика */
  .traffic-section {
    display: flex;
    flex-direction: column;
    gap: 6px;
    margin-top: 2px;
  }

  .traffic-labels {
    display: flex;
    justify-content: space-between;
    font-size: 11px;
    color: var(--fg-secondary);
  }

  .traffic-label {
    text-transform: uppercase;
    letter-spacing: 0.05em;
    font-weight: 600;
  }

  .traffic-value {
    font-family: var(--font-family-mono);
  }

  .progress-container {
    width: 100%;
    height: 6px;
    background: rgba(255, 255, 255, 0.03);
    border: 1px solid var(--border-light, rgba(255, 255, 255, 0.04));
    border-radius: 3px;
    overflow: hidden;
  }

  .progress-bar {
    height: 100%;
    background: linear-gradient(90deg, var(--accent-2), var(--accent));
    border-radius: 3px;
    transition: width var(--transition-normal);
  }

  .progress-bar.warning {
    background: var(--warning);
  }

  .progress-bar.danger {
    background: var(--danger);
  }

  .progress-bar.infinite {
    background: rgba(41, 194, 240, 0.15);
  }

  /* Таймер */
  .refresh-timer {
    display: flex;
    align-items: center;
    gap: 6px;
    font-size: 11.5px;
    color: var(--fg-dim);
    margin-top: -2px;
  }

  .timer-icon {
    color: var(--fg-dim);
  }

  .timer-icon :global(polyline) {
    animation: clockRotate 4s linear infinite;
    transform-origin: 12px 12px;
  }

  @keyframes clockRotate {
    from {
      transform: rotate(0deg);
    }
    to {
      transform: rotate(360deg);
    }
  }

  /* Детальный режим */
  .detail-mode .sub-name {
    font-size: 18px;
  }
</style>
