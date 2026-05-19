<script lang="ts">
  import { onMount } from 'svelte'
  import { t } from './i18n'
  import PageHeader from './PageHeader.svelte'

  export let onSwitchTab: (tab: string) => void = () => {}

  interface Quota {
    id: string
    name: string
    target_type: string
    target_id: string
    limit_bytes: number
    period: string
    enabled: boolean
    alert_threshold: number
    current_bytes: number
    last_reset: number
  }

  interface ProxyStat {
    proxy_name: string
    upload_bytes: number
    download_bytes: number
    total_bytes: number
  }

  interface Alert {
    quota_id: string
    quota_name: string
    severity: string
    message: string
    timestamp: number
  }

  let quotas: Quota[] = []
  let stats: { proxies: ProxyStat[], total_upload: number, total_download: number, total: number } | null = null
  let alerts: Alert[] = []
  let loading = false
  let error = ''

  // Form state
  let showForm = false
  let editingQuota: Quota | null = null
  let formName = ''
  let formTargetType = 'global'
  let formTargetID = ''
  let formLimitValue = 10
  let formLimitUnit = 'GB'
  let formPeriod = 'monthly'
  let formAlertThreshold = 80
  let formEnabled = true

  const units = [
    { value: 'MB', bytes: 1024 * 1024 },
    { value: 'GB', bytes: 1024 * 1024 * 1024 },
    { value: 'TB', bytes: 1024 * 1024 * 1024 * 1024 }
  ]

  $: periods = [
    { value: 'daily', label: $t('trafficquotas.period_daily') },
    { value: 'weekly', label: $t('trafficquotas.period_weekly') },
    { value: 'monthly', label: $t('trafficquotas.period_monthly') }
  ]

  async function fetchQuotas() {
    loading = true
    try {
      const res = await fetch('/api/traffic/quotas')
      if (res.ok) quotas = await res.json()
    } catch (e: any) {
      error = e.message
    } finally {
      loading = false
    }
  }

  async function fetchStats() {
    try {
      const res = await fetch('/api/traffic/stats')
      if (res.ok) stats = await res.json()
    } catch (e) {
      // ignore
    }
  }

  async function fetchAlerts() {
    try {
      const res = await fetch('/api/traffic/alerts')
      if (res.ok) alerts = await res.json()
    } catch (e) {
      // ignore
    }
  }

  function startCreate() {
    showForm = true
    editingQuota = null
    formName = ''
    formTargetType = 'global'
    formTargetID = ''
    formLimitValue = 10
    formLimitUnit = 'GB'
    formPeriod = 'monthly'
    formAlertThreshold = 80
    formEnabled = true
  }

  function startEdit(q: Quota) {
    editingQuota = q
    formName = q.name
    formTargetType = q.target_type
    formTargetID = q.target_id
    formPeriod = q.period
    formAlertThreshold = q.alert_threshold
    formEnabled = q.enabled
    // Restore limit value/unit
    let bytes = q.limit_bytes
    if (bytes >= 1024 * 1024 * 1024 * 1024) {
      formLimitValue = bytes / (1024 * 1024 * 1024 * 1024)
      formLimitUnit = 'TB'
    } else if (bytes >= 1024 * 1024 * 1024) {
      formLimitValue = bytes / (1024 * 1024 * 1024)
      formLimitUnit = 'GB'
    } else {
      formLimitValue = bytes / (1024 * 1024)
      formLimitUnit = 'MB'
    }
  }

  function cancelEdit() {
    showForm = false
    editingQuota = null
  }

  function getLimitBytes(): number {
    const unit = units.find(u => u.value === formLimitUnit)
    return formLimitValue * (unit?.bytes || 0)
  }

  async function saveQuota() {
    const csrfToken = localStorage.getItem('csrf_token')
    const payload = {
      name: formName,
      target_type: formTargetType,
      target_id: formTargetID,
      limit_bytes: getLimitBytes(),
      period: formPeriod,
      alert_threshold: formAlertThreshold,
      enabled: formEnabled
    }

    const url = editingQuota
      ? `/api/traffic/quotas/update?id=${editingQuota.id}`
      : '/api/traffic/quotas/add'

    try {
      const res = await fetch(url, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          'X-CSRF-Token': csrfToken || ''
        },
        body: JSON.stringify(payload)
      })
      if (!res.ok) throw new Error('Failed to save')
      showForm = false
      editingQuota = null
      await fetchQuotas()
    } catch (e: any) {
      error = e.message
    }
  }

  async function deleteQuota(id: string) {
    if (!confirm($t('app.delete') + '?')) return
    const csrfToken = localStorage.getItem('csrf_token')
    try {
      const res = await fetch(`/api/traffic/quotas/delete?id=${id}`, {
        method: 'POST',
        headers: { 'X-CSRF-Token': csrfToken || '' }
      })
      if (!res.ok) throw new Error('Failed to delete')
      await fetchQuotas()
    } catch (e: any) {
      error = e.message
    }
  }

  async function toggleEnabled(q: Quota) {
    const csrfToken = localStorage.getItem('csrf_token')
    try {
      const res = await fetch(`/api/traffic/quotas/enabled?id=${q.id}&enabled=${!q.enabled}`, {
        method: 'POST',
        headers: { 'X-CSRF-Token': csrfToken || '' }
      })
      if (!res.ok) throw new Error('Failed to toggle')
      await fetchQuotas()
    } catch (e: any) {
      error = e.message
    }
  }

  async function resetQuota(id: string) {
    const csrfToken = localStorage.getItem('csrf_token')
    try {
      const res = await fetch(`/api/traffic/quotas/reset?id=${id}`, {
        method: 'POST',
        headers: { 'X-CSRF-Token': csrfToken || '' }
      })
      if (!res.ok) throw new Error('Failed to reset')
      await fetchQuotas()
    } catch (e: any) {
      error = e.message
    }
  }

  async function clearAlerts() {
    const csrfToken = localStorage.getItem('csrf_token')
    try {
      const res = await fetch('/api/traffic/alerts/clear', {
        method: 'POST',
        headers: { 'X-CSRF-Token': csrfToken || '' }
      })
      if (!res.ok) throw new Error('Failed to clear')
      alerts = []
    } catch (e: any) {
      error = e.message
    }
  }

  function formatBytes(b: number): string {
    if (b >= 1024 * 1024 * 1024 * 1024) return (b / (1024 * 1024 * 1024 * 1024)).toFixed(2) + ' TB'
    if (b >= 1024 * 1024 * 1024) return (b / (1024 * 1024 * 1024)).toFixed(2) + ' GB'
    if (b >= 1024 * 1024) return (b / (1024 * 1024)).toFixed(2) + ' MB'
    if (b >= 1024) return (b / 1024).toFixed(2) + ' KB'
    return b + ' B'
  }

  function percent(q: Quota): number {
    if (q.limit_bytes <= 0) return 0
    return Math.min(100, (q.current_bytes / q.limit_bytes) * 100)
  }

  onMount(() => {
    fetchQuotas()
    fetchStats()
    fetchAlerts()
    const interval = setInterval(() => {
      fetchQuotas()
      fetchStats()
      fetchAlerts()
    }, 30000)
    return () => clearInterval(interval)
  })
</script>

<div class="container">
  <PageHeader
    title={$t('trafficquotas.title')}
    subtitle={$t('trafficquotas.subtitle')}
    breadcrumbs={[{ label: $t('trafficquotas.title') }]}
    {onSwitchTab}
  />

  {#if error}
    <div class="alert alert-error mb-2">{error}</div>
  {/if}

  <!-- Alerts -->
  {#if alerts.length > 0}
    <div class="card mb-2">
      <div class="flex-between mb-2">
        <h2>{$t('trafficquotas.alerts')}</h2>
        <button class="btn btn-secondary" on:click={clearAlerts}>{$t('trafficquotas.clear_alerts')}</button>
      </div>
      {#each alerts as a}
        <div class="alert mb-1" class:alert-warning={a.severity === 'warning'} class:alert-error={a.severity === 'critical'}>
          {a.message}
        </div>
      {/each}
    </div>
  {/if}

  <!-- Stats -->
  {#if stats}
    <div class="card mb-2">
      <h2>{$t('trafficquotas.stats')}</h2>
      <div class="stats-grid">
        <div class="stat-box">
          <div class="stat-label">{$t('trafficquotas.total_upload')}</div>
          <div class="stat-value">{formatBytes(stats.total_upload)}</div>
        </div>
        <div class="stat-box">
          <div class="stat-label">{$t('trafficquotas.total_download')}</div>
          <div class="stat-value">{formatBytes(stats.total_download)}</div>
        </div>
        <div class="stat-box">
          <div class="stat-label">{$t('trafficquotas.total')}</div>
          <div class="stat-value">{formatBytes(stats.total)}</div>
        </div>
      </div>
      {#if stats.proxies.length > 0}
        <div class="proxy-stats mt-2">
          <h3>{$t('trafficquotas.per_proxy')}</h3>
          <div class="proxy-list">
            {#each stats.proxies as p}
              <div class="proxy-row">
                <span class="proxy-name">{p.proxy_name}</span>
                <span class="proxy-traffic">↑ {formatBytes(p.upload_bytes)} ↓ {formatBytes(p.download_bytes)}</span>
              </div>
            {/each}
          </div>
        </div>
      {/if}
    </div>
  {/if}

  <!-- Quotas -->
  <div class="card mb-2">
    <div class="flex-between mb-2">
      <h2>{$t('trafficquotas.quotas')}</h2>
      <button class="btn btn-primary" on:click={startCreate}>+ {$t('trafficquotas.add_quota')}</button>
    </div>

    {#if quotas.length === 0}
      <p class="text-secondary">{$t('trafficquotas.no_quotas')}</p>
    {:else}
      <div class="quota-list">
        {#each quotas as q}
          <div class="quota-item">
            <div class="quota-main">
              <div class="quota-header">
                <span class="quota-name">{q.name}</span>
                <label class="toggle-switch">
                  <input type="checkbox" checked={q.enabled} on:change={() => toggleEnabled(q)} />
                  <span class="toggle-slider"></span>
                </label>
              </div>
              <div class="quota-details">
                <span class="detail">{q.target_type === 'global' ? $t('trafficquotas.target_global') : q.target_id}</span>
                <span class="detail">{q.period === 'daily' ? $t('trafficquotas.period_daily') : q.period === 'weekly' ? $t('trafficquotas.period_weekly') : q.period === 'monthly' ? $t('trafficquotas.period_monthly') : q.period}</span>
                <span class="detail">{formatBytes(q.current_bytes)} / {formatBytes(q.limit_bytes)}</span>
              </div>
              <div class="progress-bar">
                <div class="progress-fill" class:warning={percent(q) >= q.alert_threshold && percent(q) < 100} class:critical={percent(q) >= 100} style="width: {percent(q)}%"></div>
              </div>
              <div class="progress-text">{percent(q).toFixed(1)}%</div>
            </div>
            <div class="quota-actions">
              <button class="btn-icon" on:click={() => resetQuota(q.id)} title={$t('trafficquotas.reset')}>↺</button>
              <button class="btn-icon" on:click={() => startEdit(q)} title={$t('app.edit')}>✏️</button>
              <button class="btn-icon" on:click={() => deleteQuota(q.id)} title={$t('app.delete')}>🗑️</button>
            </div>
          </div>
        {/each}
      </div>
    {/if}
  </div>

  <!-- Edit/Create Form -->
  {#if showForm || editingQuota !== null}
      <div class="card">
        <h2>{editingQuota ? $t('trafficquotas.edit_quota') : $t('trafficquotas.new_quota')}</h2>

        <div class="form-group">
          <label for="tq-name">{$t('trafficquotas.name')}</label>
          <input id="tq-name" type="text" class="input" bind:value={formName} placeholder={$t('trafficquotas.name_placeholder')} />
        </div>

        <div class="form-group">
          <label for="tq-type">{$t('trafficquotas.target_type')}</label>
          <select id="tq-type" class="input" bind:value={formTargetType}>
            <option value="global">{$t('trafficquotas.target_global')}</option>
            <option value="proxy">{$t('trafficquotas.target_proxy')}</option>
          </select>
        </div>

        {#if formTargetType === 'proxy'}
          <div class="form-group">
            <label for="tq-target">{$t('trafficquotas.proxy_name')}</label>
            <input id="tq-target" type="text" class="input" bind:value={formTargetID} placeholder="HK-1" />
          </div>
        {/if}

        <div class="form-row">
          <div class="form-group">
            <label for="tq-limit">{$t('trafficquotas.limit')}</label>
            <input id="tq-limit" type="number" class="input" bind:value={formLimitValue} min="1" step="0.1" />
          </div>
          <div class="form-group">
            <label for="tq-unit">{$t('trafficquotas.unit')}</label>
            <select id="tq-unit" class="input" bind:value={formLimitUnit}>
              {#each units as u}
                <option value={u.value}>{u.value}</option>
              {/each}
            </select>
          </div>
        </div>

        <div class="form-group">
          <label for="tq-period">{$t('trafficquotas.period')}</label>
          <select id="tq-period" class="input" bind:value={formPeriod}>
            {#each periods as p}
              <option value={p.value}>{p.label}</option>
            {/each}
          </select>
        </div>

        <div class="form-group">
          <label for="tq-threshold">{$t('trafficquotas.alert_threshold')} (%)</label>
          <input id="tq-threshold" type="number" class="input" bind:value={formAlertThreshold} min="0" max="100" />
        </div>

        <div class="form-actions">
          <button class="btn btn-secondary" on:click={cancelEdit}>{$t('app.cancel')}</button>
          <button class="btn btn-primary" on:click={saveQuota}>{$t('app.save')}</button>
        </div>
      </div>
  {/if}
</div>

<style>
  .flex-between {
    display: flex;
    justify-content: space-between;
    align-items: center;
  }

  .stats-grid {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(120px, 1fr));
    gap: 1rem;
    margin-top: 0.5rem;
  }

  .stat-box {
    padding: 0.75rem;
    border: 1px solid var(--border);
    border-radius: var(--radius);
    text-align: center;
  }

  .stat-label {
    font-size: 0.8rem;
    color: var(--text-secondary);
    margin-bottom: 0.25rem;
  }

  .stat-value {
    font-weight: 600;
    font-size: 1.1rem;
  }

  .proxy-stats {
    margin-top: 1rem;
  }

  .proxy-list {
    display: flex;
    flex-direction: column;
    gap: 0.5rem;
  }

  .proxy-row {
    display: flex;
    justify-content: space-between;
    padding: 0.5rem;
    border: 1px solid var(--border-light);
    border-radius: 4px;
    font-size: 0.85rem;
  }

  .proxy-name {
    font-weight: 500;
  }

  .proxy-traffic {
    color: var(--text-secondary);
  }

  .quota-list {
    display: flex;
    flex-direction: column;
    gap: 0.75rem;
  }

  .quota-item {
    display: flex;
    justify-content: space-between;
    align-items: flex-start;
    padding: 0.75rem;
    border: 1px solid var(--border);
    border-radius: var(--radius);
    background: var(--bg);
  }

  .quota-main {
    flex: 1;
  }

  .quota-header {
    display: flex;
    justify-content: space-between;
    align-items: center;
    margin-bottom: 0.25rem;
  }

  .quota-name {
    font-weight: 600;
  }

  .quota-details {
    display: flex;
    gap: 0.5rem;
    flex-wrap: wrap;
    font-size: 0.8rem;
    color: var(--text-secondary);
    margin-bottom: 0.5rem;
  }

  .progress-bar {
    height: 8px;
    background: var(--border);
    border-radius: 4px;
    overflow: hidden;
  }

  .progress-fill {
    height: 100%;
    background: var(--primary);
    transition: width 0.3s;
  }

  .progress-fill.warning {
    background: var(--warning, #ffc107);
  }

  .progress-fill.critical {
    background: var(--danger, #dc3545);
  }

  .progress-text {
    font-size: 0.75rem;
    color: var(--text-secondary);
    margin-top: 0.25rem;
  }

  .quota-actions {
    display: flex;
    gap: 0.25rem;
  }

  .btn-icon {
    padding: 0.25rem 0.5rem;
    background: transparent;
    border: 1px solid var(--border);
    border-radius: 4px;
    cursor: pointer;
  }

  .form-row {
    display: flex;
    gap: 1rem;
  }

  .form-row .form-group {
    flex: 1;
  }

  .form-actions {
    display: flex;
    gap: 0.5rem;
    justify-content: flex-end;
    margin-top: 1rem;
  }

  .toggle-switch {
    position: relative;
    display: inline-block;
    width: 40px;
    height: 20px;
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
    background-color: var(--border);
    transition: .3s;
    border-radius: 20px;
  }

  .toggle-slider:before {
    position: absolute;
    content: "";
    height: 14px;
    width: 14px;
    left: 3px;
    bottom: 3px;
    background-color: white;
    transition: .3s;
    border-radius: 50%;
  }

  input:checked + .toggle-slider {
    background-color: var(--primary);
  }

  input:checked + .toggle-slider:before {
    transform: translateX(20px);
  }

  .mt-1 { margin-top: 0.5rem; }
  .mt-2 { margin-top: 1rem; }
  .mb-1 { margin-bottom: 0.5rem; }
  .mb-2 { margin-bottom: 1rem; }
</style>
