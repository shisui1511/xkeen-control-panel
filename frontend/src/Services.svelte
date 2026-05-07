<script lang="ts">
  import { onMount } from 'svelte'
  import { t } from './i18n'

  let xkeenStatus = ''
  let mihomoStatus = ''
  let loading = false
  let actionLoading: Record<string, boolean> = {}

  async function fetchStatus() {
    try {
      const [xkeenRes, mihomoRes] = await Promise.all([
        fetch('/api/service/status'),
        fetch('/api/mihomo/status')
      ])
      xkeenStatus = xkeenRes.ok ? await xkeenRes.text() : $t('app.error')
      mihomoStatus = mihomoRes.ok ? await mihomoRes.text() : $t('app.error')
    } catch (e) {
      xkeenStatus = $t('app.unavailable')
      mihomoStatus = $t('app.unavailable')
    }
  }

  async function controlService(service: 'xkeen' | 'mihomo', action: string) {
    const key = `${service}-${action}`
    actionLoading[key] = true
    
    try {
      const endpoint = service === 'xkeen' ? '/api/service/control' : '/api/mihomo/control'
      const csrfToken = localStorage.getItem('csrf_token')
      const res = await fetch(`${endpoint}?action=${action}`, {
        method: 'POST',
        headers: {
          'X-CSRF-Token': csrfToken || ''
        }
      })
      
      const text = await res.text()
      if (!res.ok) throw new Error(text)
      
      // Обновляем статус после действия
      await fetchStatus()
    } catch (e: any) {
      alert(`${$t('svc.action_error')}: ${e.message}`)
    } finally {
      actionLoading[key] = false
    }
  }

  onMount(() => {
    fetchStatus()
  })
</script>

<div class="container">
  <h1>{$t('svc.title')}</h1>
  <p class="text-secondary mb-3">{$t('svc.subtitle')}</p>

  <div class="services-grid">
    <!-- XKeen Card -->
    <div class="card">
      <div class="service-header">
        <h2>{$t('svc.xkeen')}</h2>
        <span class="status-badge" class:running={xkeenStatus.includes('running') || xkeenStatus.includes('работает') || xkeenStatus.includes('активен')}>
          {xkeenStatus || $t('app.loading')}
        </span>
      </div>
      <p class="text-secondary mb-2">{$t('svc.xkeen_desc')}</p>
      <div class="actions">
        <button 
          class="btn btn-primary" 
          on:click={() => controlService('xkeen', 'start')}
          disabled={actionLoading['xkeen-start']}
        >
          {actionLoading['xkeen-start'] ? $t('svc.starting') : '▶ ' + $t('app.start')}
        </button>
        <button 
          class="btn btn-secondary" 
          on:click={() => controlService('xkeen', 'stop')}
          disabled={actionLoading['xkeen-stop']}
        >
          {actionLoading['xkeen-stop'] ? $t('svc.stopping') : '⏹ ' + $t('app.stop')}
        </button>
        <button 
          class="btn btn-secondary" 
          on:click={() => controlService('xkeen', 'restart')}
          disabled={actionLoading['xkeen-restart']}
        >
          {actionLoading['xkeen-restart'] ? $t('svc.restarting') : '🔄 ' + $t('app.restart')}
        </button>
      </div>
    </div>

    <!-- Mihomo Card -->
    <div class="card">
      <div class="service-header">
        <h2>{$t('svc.mihomo')}</h2>
        <span class="status-badge" class:running={mihomoStatus.includes('running') || mihomoStatus.includes('pid')}>
          {mihomoStatus || $t('app.loading')}
        </span>
      </div>
      <p class="text-secondary mb-2">{$t('svc.mihomo_desc')}</p>
      <div class="actions">
        <button 
          class="btn btn-primary" 
          on:click={() => controlService('mihomo', 'start')}
          disabled={actionLoading['mihomo-start']}
        >
          {actionLoading['mihomo-start'] ? $t('svc.starting') : '▶ ' + $t('app.start')}
        </button>
        <button 
          class="btn btn-secondary" 
          on:click={() => controlService('mihomo', 'stop')}
          disabled={actionLoading['mihomo-stop']}
        >
          {actionLoading['mihomo-stop'] ? $t('svc.stopping') : '⏹ ' + $t('app.stop')}
        </button>
        <button 
          class="btn btn-secondary" 
          on:click={() => controlService('mihomo', 'restart')}
          disabled={actionLoading['mihomo-restart']}
        >
          {actionLoading['mihomo-restart'] ? $t('svc.restarting') : '🔄 ' + $t('app.restart')}
        </button>
      </div>
    </div>
  </div>

  <div class="card mt-2">
    <h3>{$t('svc.refresh_status')}</h3>
    <button class="btn btn-secondary" on:click={fetchStatus} disabled={loading}>
      {loading ? $t('app.loading') : '🔄 ' + $t('app.refresh')}
    </button>
  </div>
</div>

<style>
  .services-grid {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(300px, 1fr));
    gap: 1.5rem;
    margin-bottom: 1.5rem;
  }

  .service-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    margin-bottom: 0.75rem;
  }

  .service-header h2 {
    margin: 0;
  }

  .status-badge {
    padding: 0.25rem 0.75rem;
    border-radius: 999px;
    font-size: 0.75rem;
    font-weight: 500;
    background: var(--bg-page);
    color: var(--fg-secondary);
    border: 1px solid var(--border);
  }

  .status-badge.running {
    background: rgba(16, 185, 129, 0.1);
    color: var(--success);
    border-color: var(--success);
  }

  .actions {
    display: flex;
    gap: 0.5rem;
    flex-wrap: wrap;
  }

  .mt-2 {
    margin-top: 1rem;
  }
</style>