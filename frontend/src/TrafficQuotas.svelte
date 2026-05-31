<script lang="ts">
  import { onMount } from 'svelte';
  import { t, currentLang } from './i18n';
  import PageHeader from './PageHeader.svelte';
  import Icon from './lib/components/Icon.svelte';

  export let onSwitchTab: (tab: string) => void = () => {};

  interface Quota {
    id: string;
    name: string;
    target_type: string;
    target_id: string;
    limit_bytes: number;
    period: string;
    enabled: boolean;
    alert_threshold: number;
    action?: string;
    current_bytes: number;
    last_reset: number;
  }

  interface ProxyStat {
    proxy_name: string;
    upload_bytes: number;
    download_bytes: number;
    total_bytes: number;
  }

  interface Alert {
    quota_id: string;
    quota_name: string;
    severity: string;
    message: string;
    timestamp: number;
  }

  let quotas: Quota[] = [];
  let stats: {
    proxies: ProxyStat[];
    total_upload: number;
    total_download: number;
    total: number;
    reset_time?: number;
  } | null = null;
  let alerts: Alert[] = [];
  let loading = false;
  let error = '';

  // Form state
  let showForm = false;
  let editingQuota: Quota | null = null;
  let formName = '';
  let formTargetType = 'global';
  let formTargetID = '';
  let formLimitValue = 10;
  let formLimitUnit = 'GB';
  let formPeriod = 'monthly';
  let formAlertThreshold = 80;
  let formAction = 'notify';
  let formEnabled = true;

  let activeDropdownId: string | null = null;

  const units = [
    { value: 'MB', bytes: 1024 * 1024 },
    { value: 'GB', bytes: 1024 * 1024 * 1024 },
    { value: 'TB', bytes: 1024 * 1024 * 1024 * 1024 }
  ];

  $: periods = [
    { value: 'daily', label: $t('trafficquotas.period_daily') },
    { value: 'weekly', label: $t('trafficquotas.period_weekly') },
    { value: 'monthly', label: $t('trafficquotas.period_monthly') }
  ];

  $: activeQuotas = quotas.filter((q) => q.enabled);
  $: sumQuotaLimit = activeQuotas.reduce((s, q) => s + q.limit_bytes, 0);
  $: totalPct = sumQuotaLimit > 0 ? Math.min(100, ((stats?.total || 0) / sumQuotaLimit) * 100) : 0;

  async function fetchQuotas() {
    loading = true;
    try {
      const res = await fetch('/api/traffic/quotas');
      if (res.ok) quotas = await res.json();
    } catch (e: any) {
      error = e.message;
    } finally {
      loading = false;
    }
  }

  async function fetchStats() {
    try {
      const res = await fetch('/api/traffic/stats');
      if (res.ok) stats = await res.json();
    } catch (e) {
      // ignore
    }
  }

  async function fetchAlerts() {
    try {
      const res = await fetch('/api/traffic/alerts');
      if (res.ok) alerts = await res.json();
    } catch (e) {
      // ignore
    }
  }

  function startCreate() {
    showForm = true;
    editingQuota = null;
    formName = '';
    formTargetType = 'global';
    formTargetID = '';
    formLimitValue = 10;
    formLimitUnit = 'GB';
    formPeriod = 'monthly';
    formAlertThreshold = 80;
    formAction = 'notify';
    formEnabled = true;
    activeDropdownId = null;
  }

  function startEdit(q: Quota) {
    editingQuota = q;
    formName = q.name;
    formTargetType = q.target_type;
    formTargetID = q.target_id;
    formPeriod = q.period;
    formAlertThreshold = q.alert_threshold;
    formAction = q.action || 'notify';
    formEnabled = q.enabled;
    // Restore limit value/unit
    let bytes = q.limit_bytes;
    if (bytes >= 1024 * 1024 * 1024 * 1024) {
      formLimitValue = bytes / (1024 * 1024 * 1024 * 1024);
      formLimitUnit = 'TB';
    } else if (bytes >= 1024 * 1024 * 1024) {
      formLimitValue = bytes / (1024 * 1024 * 1024);
      formLimitUnit = 'GB';
    } else {
      formLimitValue = bytes / (1024 * 1024);
      formLimitUnit = 'MB';
    }
    showForm = true;
    activeDropdownId = null;
  }

  function cancelEdit() {
    showForm = false;
    editingQuota = null;
  }

  function getLimitBytes(): number {
    const unit = units.find((u) => u.value === formLimitUnit);
    return formLimitValue * (unit?.bytes || 0);
  }

  async function saveQuota() {
    const csrfToken = localStorage.getItem('csrf_token');
    const payload = {
      name: formName,
      target_type: formTargetType,
      target_id: formTargetID,
      limit_bytes: getLimitBytes(),
      period: formPeriod,
      alert_threshold: formAlertThreshold,
      action: formAction,
      enabled: formEnabled
    };

    const url = editingQuota
      ? `/api/traffic/quotas/update?id=${editingQuota.id}`
      : '/api/traffic/quotas/add';

    try {
      const res = await fetch(url, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          'X-CSRF-Token': csrfToken || ''
        },
        body: JSON.stringify(payload)
      });
      if (!res.ok) throw new Error('Failed to save');
      showForm = false;
      editingQuota = null;
      await fetchQuotas();
    } catch (e: any) {
      error = e.message;
    }
  }

  async function deleteQuota(id: string) {
    if (!confirm($t('app.delete') + '?')) return;
    const csrfToken = localStorage.getItem('csrf_token');
    try {
      const res = await fetch(`/api/traffic/quotas/delete?id=${id}`, {
        method: 'POST',
        headers: { 'X-CSRF-Token': csrfToken || '' }
      });
      if (!res.ok) throw new Error('Failed to delete');
      await fetchQuotas();
    } catch (e: any) {
      error = e.message;
    }
  }

  async function toggleEnabled(q: Quota) {
    const csrfToken = localStorage.getItem('csrf_token');
    try {
      const res = await fetch(`/api/traffic/quotas/enabled?id=${q.id}&enabled=${!q.enabled}`, {
        method: 'POST',
        headers: { 'X-CSRF-Token': csrfToken || '' }
      });
      if (!res.ok) throw new Error('Failed to toggle');
      await fetchQuotas();
    } catch (e: any) {
      error = e.message;
    }
  }

  async function resetQuota(id: string) {
    const csrfToken = localStorage.getItem('csrf_token');
    try {
      const res = await fetch(`/api/traffic/quotas/reset?id=${id}`, {
        method: 'POST',
        headers: { 'X-CSRF-Token': csrfToken || '' }
      });
      if (!res.ok) throw new Error('Failed to reset');
      await fetchQuotas();
    } catch (e: any) {
      error = e.message;
    }
  }

  async function clearAlerts() {
    const csrfToken = localStorage.getItem('csrf_token');
    try {
      const res = await fetch('/api/traffic/alerts/clear', {
        method: 'POST',
        headers: { 'X-CSRF-Token': csrfToken || '' }
      });
      if (!res.ok) throw new Error('Failed to clear');
      alerts = [];
    } catch (e: any) {
      error = e.message;
    }
  }

  function formatBytes(b: number): string {
    if (b >= 1024 * 1024 * 1024 * 1024) return (b / (1024 * 1024 * 1024 * 1024)).toFixed(2) + ' TB';
    if (b >= 1024 * 1024 * 1024) return (b / (1024 * 1024 * 1024)).toFixed(2) + ' GB';
    if (b >= 1024 * 1024) return (b / (1024 * 1024)).toFixed(2) + ' MB';
    if (b >= 1024) return (b / 1024).toFixed(2) + ' KB';
    return b + ' B';
  }

  function percent(q: Quota): number {
    if (q.limit_bytes <= 0) return 0;
    return Math.min(100, (q.current_bytes / q.limit_bytes) * 100);
  }

  function getActionBadgeClass(action?: string): string {
    switch (action) {
      case 'throttle':
        return 'badge tq-action-throttle';
      case 'log_only':
        return 'badge tq-action-log';
      case 'block':
        return 'badge tq-action-block';
      case 'redirect_direct':
        return 'badge tq-action-redirect';
      default:
        return 'badge tq-action-notify';
    }
  }

  function getActionLabel(action?: string): string {
    switch (action) {
      case 'throttle':
        return $t('trafficquotas.action_throttle');
      case 'log_only':
        return $t('trafficquotas.action_log_only');
      case 'block':
        return $t('trafficquotas.action_block');
      case 'redirect_direct':
        return $t('trafficquotas.action_redirect_direct');
      default:
        return $t('trafficquotas.action_notify');
    }
  }

  function toggleDropdown(id: string, event: MouseEvent) {
    event.stopPropagation();
    if (activeDropdownId === id) {
      activeDropdownId = null;
    } else {
      activeDropdownId = id;
    }
  }

  function handleDocumentClick() {
    activeDropdownId = null;
  }

  function handleKeydown(event: KeyboardEvent) {
    if (event.key === 'Escape') {
      cancelEdit();
    }
  }

  let dismissedBanner = false;

  function dismissBanner() {
    dismissedBanner = true;
    localStorage.setItem('tq_banner_dismissed', 'true');
  }

  let forecastValue: number | null = null;
  let showForecastCalculating = false;

  $: {
    if (stats && stats.reset_time) {
      const nowSec = Math.floor(Date.now() / 1000);
      const duration = nowSec - stats.reset_time;
      if (duration > 0) {
        if (duration < 600) {
          showForecastCalculating = true;
          forecastValue = null;
        } else {
          showForecastCalculating = false;
          forecastValue = (stats.total / duration) * 30 * 24 * 3600;
        }
      } else {
        showForecastCalculating = true;
        forecastValue = null;
      }
    } else {
      showForecastCalculating = false;
      forecastValue = null;
    }
  }

  onMount(() => {
    dismissedBanner = localStorage.getItem('tq_banner_dismissed') === 'true';
    fetchQuotas();
    fetchStats();
    fetchAlerts();
    document.addEventListener('click', handleDocumentClick);
    const interval = setInterval(() => {
      fetchQuotas();
      fetchStats();
      fetchAlerts();
    }, 30000);
    return () => {
      clearInterval(interval);
      document.removeEventListener('click', handleDocumentClick);
    };
  });
</script>

<div class="container">
  <div class="page-head">
    <div>
      <div class="crumbs">
        {$t('nav.group_tools')} <span class="crumb-separator">/</span>
        {$t('nav.trafficquotas')}
      </div>
      <h1>{$t('trafficquotas.title')}</h1>
      <p class="sub">{$t('trafficquotas.subtitle')}</p>
    </div>
    <div class="ph-actions">
      {#if stats}
        <button class="btn btn-secondary" on:click={clearAlerts}>
          {$t('trafficquotas.clear_alerts')}
        </button>
      {/if}
      <button class="btn btn-primary" on:click={startCreate}>
        <svg
          width="14"
          height="14"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          stroke-width="2"
          style="margin-right: 6px;"
        >
          <path d="M12 5v14M5 12h14" />
        </svg>
        {$t('trafficquotas.add_quota')}
      </button>
    </div>
  </div>

  {#if error}
    <div class="alert alert-error mb-2">{error}</div>
  {/if}

  <!-- Banner -->
  {#if !dismissedBanner}
    <div class="info-banner mb-3">
      <div class="info-banner-content">
        <div class="info-banner-icon">
          <Icon name="warning" size={20} />
        </div>
        <div class="info-banner-text">
          <h3 class="info-banner-title">{$t('trafficquotas.banner_title')}</h3>
          <p class="info-banner-description">{$t('trafficquotas.banner_text')}</p>
        </div>
      </div>
      <button class="info-banner-close" on:click={dismissBanner} aria-label="Dismiss banner">
        &times;
      </button>
    </div>
  {/if}

  <!-- Alerts -->
  {#if alerts.length > 0}
    <div class="card mb-3">
      <div class="flex-between mb-2">
        <h2 class="card-title">{$t('trafficquotas.alerts')}</h2>
      </div>
      <div class="alerts-list">
        {#each alerts as a}
          <div
            class="alert"
            class:alert-warning={a.severity === 'warning'}
            class:alert-error={a.severity === 'critical'}
          >
            {a.message}
          </div>
        {/each}
      </div>
    </div>
  {/if}

  <!-- Stats Summaries -->
  {#if stats}
    <div class="card mb-3">
      <h2 class="card-title">{$currentLang === 'ru' ? 'СВОДКА' : 'SUMMARY'}</h2>
      <div class="stats-grid">
        <div class="stat-box">
          <div class="stat-label">{$t('trafficquotas.total_download')}</div>
          <div class="stat-value">{formatBytes(stats.total_download)}</div>
          <div class="stat-sub">{$currentLang === 'ru' ? 'загружено' : 'received'}</div>
        </div>
        <div class="stat-box">
          <div class="stat-label">{$t('trafficquotas.total_upload')}</div>
          <div class="stat-value">{formatBytes(stats.total_upload)}</div>
          <div class="stat-sub">{$currentLang === 'ru' ? 'отправлено' : 'sent'}</div>
        </div>
        <div class="stat-box">
          <div class="stat-label">Σ {$t('trafficquotas.total')}</div>
          <div class="stat-value">{formatBytes(stats.total)}</div>
          {#if sumQuotaLimit > 0}
            <div class="stat-bar" style="margin-top: 8px;">
              <div
                class="stat-bar-fill"
                class:warning={totalPct >= 80 && totalPct < 100}
                class:error={totalPct >= 100}
                style="width: {totalPct}%"
              ></div>
            </div>
            <div class="stat-sub">
              {totalPct.toFixed(1)}% {$currentLang === 'ru' ? 'от лимита' : 'of limit'}
            </div>
          {/if}
        </div>
        <div class="stat-box">
          <div class="stat-label">{$t('trafficquotas.forecast_label')}</div>
          <div class="stat-value">
            {#if showForecastCalculating}
              <span style="font-size: 16px; color: var(--fg-secondary);">{$t('trafficquotas.forecast_calculating')}</span>
            {:else if forecastValue !== null}
              {formatBytes(forecastValue)}
            {:else}
              —
            {/if}
          </div>
          <div class="stat-sub">{$t('trafficquotas.forecast_subtext')}</div>
        </div>
        <div class="stat-box">
          <div class="stat-label">{$currentLang === 'ru' ? 'ЛИМИТЫ' : 'QUOTAS'}</div>
          <div class="stat-value">
            {activeQuotas.length}<span class="stat-value-unit"> / {quotas.length}</span>
          </div>
          <div class="stat-sub">{$currentLang === 'ru' ? 'активных' : 'active'}</div>
        </div>
      </div>
    </div>
  {/if}

  <!-- Quotas Table -->
  <div class="card card-tight mb-3">
    <h2 class="card-title" style="padding: 20px 24px 8px 24px;">{$t('trafficquotas.quotas')}</h2>

    {#if quotas.length === 0}
      <div style="padding: 24px; text-align: center; color: var(--fg-faint);">
        {$t('trafficquotas.no_quotas')}
      </div>
    {:else}
      <div class="table-responsive">
        <table>
          <thead>
            <tr>
              <th>{$t('trafficquotas.name')}</th>
              <th>{$t('trafficquotas.target_type')}</th>
              <th>{$t('trafficquotas.period')}</th>
              <th>Использовано</th>
              <th>{$t('trafficquotas.limit')}</th>
              <th>{$t('trafficquotas.action')}</th>
              <th>Состояние</th>
              <th>Статус</th>
              <th style="width: 50px;"></th>
            </tr>
          </thead>
          <tbody>
            {#each quotas as q}
              <tr>
                <td><b>{q.name}</b></td>
                <td>
                  <span
                    class="status-badge"
                    style="background: rgba(255,255,255,0.05); color: var(--fg-secondary);"
                  >
                    {q.target_type === 'global' ? $t('trafficquotas.target_global') : q.target_id}
                  </span>
                </td>
                <td class="mono">
                  {q.period === 'daily'
                    ? $t('trafficquotas.period_daily')
                    : q.period === 'weekly'
                      ? $t('trafficquotas.period_weekly')
                      : q.period === 'monthly'
                        ? $t('trafficquotas.period_monthly')
                        : q.period}
                </td>
                <td class="mono">
                  {formatBytes(q.current_bytes)}
                  <div class="stat-bar" style="width: 100px; margin-top: 4px;">
                    <div
                      class="stat-bar-fill"
                      class:warning={percent(q) >= q.alert_threshold && percent(q) < 100}
                      class:error={percent(q) >= 100}
                      style="width: {percent(q)}%"
                    ></div>
                  </div>
                </td>
                <td class="mono">{formatBytes(q.limit_bytes)}</td>
                <td>
                  <span class={getActionBadgeClass(q.action)}>
                    {getActionLabel(q.action)}
                  </span>
                </td>
                <td>
                  {#if q.enabled}
                    <span class="status-badge active">
                      <span class="status-dot success" style="margin: 0 4px 0 0;"></span>
                      {$currentLang === 'ru' ? 'активен' : 'active'}
                    </span>
                  {:else}
                    <span class="status-badge stopped">
                      <span class="status-dot error" style="margin: 0 4px 0 0;"></span>
                      {$currentLang === 'ru' ? 'выключен' : 'disabled'}
                    </span>
                  {/if}
                </td>
                <td>
                  {#if percent(q) >= 100}
                    {#if q.action === 'block'}
                      <span class="badge badge-error">{$t('trafficquotas.badge_status_blocked')}</span>
                    {:else if q.action === 'redirect_direct'}
                      <span class="badge badge-redirected">{$t('trafficquotas.badge_status_redirected')}</span>
                    {:else}
                      <span class="badge badge-error">{$currentLang === 'ru' ? 'Превышен' : 'Exceeded'}</span>
                    {/if}
                  {:else if percent(q) >= q.alert_threshold}
                    <span class="badge badge-warning">{$currentLang === 'ru' ? 'Предупреждение' : 'Warning'}</span>
                  {:else}
                    <span class="badge badge-success">OK</span>
                  {/if}
                </td>
                <td>
                  <div class="actions-wrapper">
                    <label
                      class="toggle-switch"
                      style="margin-right: 12px;"
                      title="Включить/выключить лимит"
                    >
                      <input
                        type="checkbox"
                        checked={q.enabled}
                        on:change={() => toggleEnabled(q)}
                      />
                      <span class="toggle-slider"></span>
                    </label>

                    <div class="dropdown-container">
                      <button
                        class="btn btn-secondary action-btn-dots"
                        on:click={(e) => toggleDropdown(q.id, e)}>⋯</button
                      >
                      {#if activeDropdownId === q.id}
                        <div class="dropdown-menu">
                          <button
                            on:click={() => {
                              resetQuota(q.id);
                              activeDropdownId = null;
                            }}
                          >
                            {$t('trafficquotas.reset')}
                          </button>
                          <button on:click={() => startEdit(q)}>
                            {$t('app.edit')}
                          </button>
                          <button
                            on:click={() => {
                              deleteQuota(q.id);
                              activeDropdownId = null;
                            }}
                            class="delete-action"
                          >
                            {$t('app.delete')}
                          </button>
                        </div>
                      {/if}
                    </div>
                  </div>
                </td>
              </tr>
            {/each}
          </tbody>
        </table>
      </div>
    {/if}
  </div>

  <!-- Proxy Stats (Optional/Additional Info in Hopper Style) -->
  {#if stats && stats.proxies && stats.proxies.length > 0}
    <div class="card card-tight mb-3">
      <h2 class="card-title" style="padding: 20px 24px 8px 24px;">
        {$t('trafficquotas.per_proxy')}
      </h2>
      <div class="table-responsive">
        <table>
          <thead>
            <tr>
              <th>{$t('trafficquotas.proxy_name')}</th>
              <th>Upload</th>
              <th>Download</th>
              <th>Всего</th>
            </tr>
          </thead>
          <tbody>
            {#each stats.proxies as p}
              <tr>
                <td><b>{p.proxy_name}</b></td>
                <td class="mono">{formatBytes(p.upload_bytes)}</td>
                <td class="mono">{formatBytes(p.download_bytes)}</td>
                <td class="mono" style="color: var(--fg-primary); font-weight: 600;"
                  >{formatBytes(p.total_bytes)}</td
                >
              </tr>
            {/each}
          </tbody>
        </table>
      </div>
    </div>
  {/if}
</div>

<!-- Modal Form -->
{#if showForm}
  <div
    class="modal-overlay"
    role="button"
    tabindex="0"
    on:click={cancelEdit}
    on:keydown={handleKeydown}
  >
    <div class="modal-card" role="presentation" on:click|stopPropagation>
      <div class="modal-card-header">
        <h2>{editingQuota ? $t('trafficquotas.edit_quota') : $t('trafficquotas.new_quota')}</h2>
        <button class="modal-close-btn" on:click={cancelEdit}>&times;</button>
      </div>
      <div class="modal-card-body">
        <div class="form-group">
          <label for="form-name" class="form-label">{$t('trafficquotas.name')}</label>
          <input
            id="form-name"
            type="text"
            class="input"
            bind:value={formName}
            placeholder={$t('trafficquotas.name_placeholder')}
          />
        </div>

        <div class="form-group">
          <label for="form-type" class="form-label">{$t('trafficquotas.target_type')}</label>
          <select id="form-type" class="input" bind:value={formTargetType}>
            <option value="global">{$t('trafficquotas.target_global')}</option>
            <option value="proxy">{$t('trafficquotas.target_proxy')}</option>
          </select>
        </div>

        {#if formTargetType === 'proxy'}
          <div class="form-group">
            <label for="form-target" class="form-label">{$t('trafficquotas.proxy_name')}</label>
            <input
              id="form-target"
              type="text"
              class="input"
              bind:value={formTargetID}
              placeholder="HK-1"
            />
          </div>
        {/if}

        <div class="form-row-grid">
          <div class="form-group">
            <label for="form-limit" class="form-label">{$t('trafficquotas.limit')}</label>
            <input
              id="form-limit"
              type="number"
              class="input"
              bind:value={formLimitValue}
              min="0.1"
              step="0.1"
            />
          </div>
          <div class="form-group">
            <label for="form-unit" class="form-label">{$t('trafficquotas.unit')}</label>
            <select id="form-unit" class="input" bind:value={formLimitUnit}>
              {#each units as u}
                <option value={u.value}>{u.value}</option>
              {/each}
            </select>
          </div>
        </div>

        <div class="form-group">
          <label for="form-period" class="form-label">{$t('trafficquotas.period')}</label>
          <select id="form-period" class="input" bind:value={formPeriod}>
            {#each periods as p}
              <option value={p.value}>{p.label}</option>
            {/each}
          </select>
        </div>

        <div class="form-group">
          <label for="form-threshold" class="form-label"
            >{$t('trafficquotas.alert_threshold')} (%)</label
          >
          <input
            id="form-threshold"
            type="number"
            class="input"
            bind:value={formAlertThreshold}
            min="1"
            max="100"
          />
        </div>

        <div class="form-group">
          <label for="form-action" class="form-label">{$t('trafficquotas.action')}</label>
          <select id="form-action" class="input" bind:value={formAction}>
            <option value="notify">{$t('trafficquotas.action_notify')}</option>
            <option value="throttle">{$t('trafficquotas.action_throttle')}</option>
            <option value="log_only">{$t('trafficquotas.action_log_only')}</option>
            <option value="block">{$t('trafficquotas.action_block')}</option>
            <option value="redirect_direct">{$t('trafficquotas.action_redirect_direct')}</option>
          </select>
        </div>

        <div class="form-group-checkbox">
          <label class="toggle-switch">
            <input type="checkbox" id="form-enabled" bind:checked={formEnabled} />
            <span class="toggle-slider"></span>
          </label>
          <label for="form-enabled" class="checkbox-label">
            {$currentLang === 'ru' ? 'Активен' : 'Enabled'}
          </label>
        </div>
      </div>
      <div class="modal-card-footer">
        <button class="btn btn-secondary" on:click={cancelEdit}>{$t('app.cancel')}</button>
        <button class="btn btn-primary" on:click={saveQuota}>{$t('app.save')}</button>
      </div>
    </div>
  </div>
{/if}

<style>
  .crumb-separator {
    color: var(--fg-faint);
    margin: 0 6px;
  }

  .flex-between {
    display: flex;
    justify-content: space-between;
    align-items: center;
  }

  .alerts-list {
    display: flex;
    flex-direction: column;
    gap: 8px;
  }

  .stats-grid {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(200px, 1fr));
    gap: 16px;
    margin-top: 8px;
  }

  .stat-box {
    padding: 16px;
    background: rgba(255, 255, 255, 0.02);
    border: 1px solid var(--border);
    border-radius: var(--radius);
  }

  .stat-label {
    font-size: 11px;
    font-weight: 700;
    color: var(--fg-secondary);
    text-transform: uppercase;
    letter-spacing: 0.05em;
    margin-bottom: 6px;
  }

  .stat-value {
    font-weight: 600;
    font-size: 20px;
    color: var(--fg-primary);
  }

  .stat-value-unit {
    font-size: 13px;
    font-weight: 400;
    color: var(--fg-secondary);
  }

  .stat-sub {
    font-size: 11px;
    color: var(--fg-dim);
    margin-top: 4px;
  }

  .table-responsive {
    width: 100%;
    overflow-x: auto;
  }

  table {
    width: 100%;
    border-collapse: collapse;
    font-size: 13px;
    color: var(--fg-secondary);
  }

  th {
    text-align: left;
    padding: 12px 24px;
    font-weight: 600;
    color: var(--fg-secondary);
    border-bottom: 1px solid var(--border);
    font-size: 11px;
    text-transform: uppercase;
    letter-spacing: 0.05em;
  }

  td {
    padding: 16px 24px;
    border-bottom: 1px solid var(--border);
    vertical-align: middle;
  }

  tr:last-child td {
    border-bottom: none;
  }

  .mono {
    font-family: var(--font-mono, monospace);
  }

  .stat-bar {
    height: 6px;
    background: rgba(255, 255, 255, 0.05);
    border-radius: 3px;
    overflow: hidden;
  }

  .stat-bar-fill {
    height: 100%;
    background: var(--primary, #3b82f6);
    border-radius: 3px;
    transition: width 0.3s ease;
  }

  .stat-bar-fill.warning {
    background: var(--warning, #f59e0b);
  }

  .stat-bar-fill.error {
    background: var(--error, #ef4444);
  }

  .actions-wrapper {
    display: flex;
    align-items: center;
    justify-content: flex-end;
  }

  /* Dropdown Styles */
  .dropdown-container {
    position: relative;
    display: inline-block;
  }

  .action-btn-dots {
    padding: 4px 8px;
    font-size: 14px;
    line-height: 1;
    height: auto;
  }

  .dropdown-menu {
    position: absolute;
    right: 0;
    top: 100%;
    margin-top: 4px;
    background: var(--bg-card, #121212);
    border: 1px solid var(--border);
    border-radius: var(--radius);
    box-shadow: 0 10px 25px rgba(0, 0, 0, 0.5);
    z-index: 10;
    min-width: 150px;
    display: flex;
    flex-direction: column;
    padding: 4px;
    animation: dropdown-anim 0.15s ease-out;
  }

  @keyframes dropdown-anim {
    from {
      opacity: 0;
      transform: translateY(-5px);
    }
    to {
      opacity: 1;
      transform: translateY(0);
    }
  }

  .dropdown-menu button {
    background: none;
    border: none;
    color: var(--fg-secondary);
    padding: 8px 12px;
    text-align: left;
    font-size: 13px;
    cursor: pointer;
    border-radius: 4px;
    width: 100%;
    transition:
      background 0.2s,
      color 0.2s;
  }

  .dropdown-menu button:hover {
    background: rgba(255, 255, 255, 0.05);
    color: var(--fg-primary);
  }

  .dropdown-menu button.delete-action {
    color: var(--error);
  }

  .dropdown-menu button.delete-action:hover {
    background: rgba(239, 68, 68, 0.1);
  }

  /* Form row layout */
  .form-row-grid {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: 16px;
  }

  /* Modal Styles */
  .modal-overlay {
    position: fixed;
    top: 0;
    left: 0;
    right: 0;
    bottom: 0;
    background: rgba(0, 0, 0, 0.6);
    backdrop-filter: blur(4px);
    display: flex;
    align-items: center;
    justify-content: center;
    z-index: 1000;
    padding: 20px;
  }

  .modal-card {
    background: var(--bg-card);
    border: 1px solid var(--border);
    border-radius: var(--radius-lg);
    width: 100%;
    max-width: 520px;
    box-shadow: 0 20px 40px rgba(0, 0, 0, 0.5);
    overflow: hidden;
    display: flex;
    flex-direction: column;
    max-height: 90vh;
    animation: modal-anim 0.2s cubic-bezier(0.16, 1, 0.3, 1);
  }

  @keyframes modal-anim {
    from {
      transform: scale(0.95) translateY(10px);
      opacity: 0;
    }
    to {
      transform: scale(1) translateY(0);
      opacity: 1;
    }
  }

  .modal-card-header {
    padding: 16px 24px;
    border-bottom: 1px solid var(--border);
    display: flex;
    justify-content: space-between;
    align-items: center;
  }

  .modal-card-header h2 {
    margin: 0;
    font-size: 16px;
    font-weight: 700;
    color: var(--fg-primary);
  }

  .modal-close-btn {
    background: none;
    border: none;
    color: var(--fg-dim);
    font-size: 24px;
    cursor: pointer;
    line-height: 1;
    padding: 4px;
  }

  .modal-close-btn:hover {
    color: var(--fg-primary);
  }

  .modal-card-body {
    padding: 24px;
    overflow-y: auto;
    display: flex;
    flex-direction: column;
    gap: 16px;
  }

  .modal-card-footer {
    padding: 16px 24px;
    border-top: 1px solid var(--border);
    display: flex;
    justify-content: flex-end;
    gap: 12px;
  }

  .form-group-checkbox {
    display: flex;
    align-items: center;
    gap: 12px;
    margin-top: 4px;
  }

  .checkbox-label {
    font-size: 13px;
    color: var(--fg-primary);
    cursor: pointer;
    user-select: none;
  }

  /* Toggle Switch */
  .toggle-switch {
    position: relative;
    display: inline-block;
    width: 32px;
    height: 18px;
  }

  .toggle-switch input {
    opacity: 0;
    width: 0;
    height: 0;
  }

  .toggle-slider {
    position: absolute;
    cursor: pointer;
    top: 0;
    left: 0;
    right: 0;
    bottom: 0;
    background-color: rgba(255, 255, 255, 0.1);
    transition: 0.2s;
    border-radius: 9px;
    border: 1px solid var(--border);
  }

  .toggle-slider:before {
    position: absolute;
    content: '';
    height: 12px;
    width: 12px;
    left: 2px;
    bottom: 2px;
    background-color: var(--fg-secondary);
    transition: 0.2s;
    border-radius: 50%;
  }

  input:checked + .toggle-slider {
    background-color: var(--primary);
    border-color: var(--primary);
  }

  input:checked + .toggle-slider:before {
    transform: translateX(14px);
    background-color: #fff;
  }

  :global(.tq-action-notify) {
    background: rgba(251, 191, 36, 0.15);
    color: #fbbf24;
    border: 1px solid rgba(251, 191, 36, 0.3);
  }
  :global(.tq-action-throttle) {
    background: rgba(251, 146, 60, 0.15);
    color: #fb923c;
    border: 1px solid rgba(251, 146, 60, 0.3);
  }
  :global(.tq-action-log) {
    background: rgba(148, 163, 184, 0.12);
    color: var(--fg-secondary);
    border: 1px solid rgba(148, 163, 184, 0.2);
  }
  :global(.tq-action-block) {
    background: rgba(239, 91, 107, 0.15);
    color: var(--danger);
    border: 1px solid rgba(239, 91, 107, 0.3);
  }
  :global(.tq-action-redirect) {
    background: rgba(16, 185, 129, 0.15);
    color: #10b981;
    border: 1px solid rgba(16, 185, 129, 0.3);
  }
  .badge-redirected {
    background: rgba(16, 185, 129, 0.2);
    color: #10b981;
    border: 1px solid rgba(16, 185, 129, 0.4);
  }
  .info-banner {
    display: flex;
    justify-content: space-between;
    align-items: flex-start;
    padding: 16px 20px;
    background: rgba(59, 130, 246, 0.08);
    border: 1px solid rgba(59, 130, 246, 0.2);
    border-radius: var(--radius-lg);
    position: relative;
  }
  .info-banner-content {
    display: flex;
    gap: 12px;
  }
  .info-banner-icon {
    color: var(--primary, #3b82f6);
    display: flex;
    align-items: center;
  }
  .info-banner-text {
    display: flex;
    flex-direction: column;
    gap: 4px;
  }
  .info-banner-title {
    margin: 0;
    font-size: 14px;
    font-weight: 600;
    color: var(--fg-primary);
  }
  .info-banner-description {
    margin: 0;
    font-size: 13px;
    color: var(--fg-secondary);
    line-height: 1.4;
  }
  .info-banner-close {
    background: none;
    border: none;
    color: var(--fg-dim);
    font-size: 20px;
    cursor: pointer;
    line-height: 1;
    padding: 2px 6px;
    margin-top: -4px;
    margin-right: -8px;
    transition: color 0.2s;
  }
  .info-banner-close:hover {
    color: var(--fg-primary);
  }
</style>
