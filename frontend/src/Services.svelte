<script lang="ts">
  import { onMount } from 'svelte'

  let xkeenStatus = 'Загрузка...'
  let mihomoStatus = 'Загрузка...'
  let loading = false
  let actionLoading: Record<string, boolean> = {}

  async function fetchStatus() {
    try {
      const [xkeenRes, mihomoRes] = await Promise.all([
        fetch('/api/service/status'),
        fetch('/api/mihomo/status')
      ])
      xkeenStatus = xkeenRes.ok ? await xkeenRes.text() : 'Ошибка'
      mihomoStatus = mihomoRes.ok ? await mihomoRes.text() : 'Ошибка'
    } catch (e) {
      xkeenStatus = 'Недоступно'
      mihomoStatus = 'Недоступно'
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
      alert(`Ошибка: ${e.message}`)
    } finally {
      actionLoading[key] = false
    }
  }

  onMount(() => {
    fetchStatus()
  })
</script>

<div class="container">
  <h1>Управление сервисами</h1>
  <p class="text-secondary mb-3">Запуск, остановка и перезапуск XKeen и Mihomo</p>

  <div class="services-grid">
    <!-- XKeen Card -->
    <div class="card">
      <div class="service-header">
        <h2>XKeen (Xray)</h2>
        <span class="status-badge" class:running={xkeenStatus.includes('running') || xkeenStatus.includes('работает') || xkeenStatus.includes('активен')}>
          {xkeenStatus}
        </span>
      </div>
      <p class="text-secondary mb-2">Основной прокси-сервис на базе Xray-core</p>
      <div class="actions">
        <button 
          class="btn btn-primary" 
          on:click={() => controlService('xkeen', 'start')}
          disabled={actionLoading['xkeen-start']}
        >
          {actionLoading['xkeen-start'] ? 'Запуск...' : '▶ Запустить'}
        </button>
        <button 
          class="btn btn-secondary" 
          on:click={() => controlService('xkeen', 'stop')}
          disabled={actionLoading['xkeen-stop']}
        >
          {actionLoading['xkeen-stop'] ? 'Остановка...' : '⏹ Остановить'}
        </button>
        <button 
          class="btn btn-secondary" 
          on:click={() => controlService('xkeen', 'restart')}
          disabled={actionLoading['xkeen-restart']}
        >
          {actionLoading['xkeen-restart'] ? 'Перезапуск...' : '🔄 Перезапустить'}
        </button>
      </div>
    </div>

    <!-- Mihomo Card -->
    <div class="card">
      <div class="service-header">
        <h2>Mihomo (Clash)</h2>
        <span class="status-badge" class:running={mihomoStatus.includes('running') || mihomoStatus.includes('pid')}>
          {mihomoStatus}
        </span>
      </div>
      <p class="text-secondary mb-2">Альтернативный прокси на базе Mihomo (Clash.Meta)</p>
      <div class="actions">
        <button 
          class="btn btn-primary" 
          on:click={() => controlService('mihomo', 'start')}
          disabled={actionLoading['mihomo-start']}
        >
          {actionLoading['mihomo-start'] ? 'Запуск...' : '▶ Запустить'}
        </button>
        <button 
          class="btn btn-secondary" 
          on:click={() => controlService('mihomo', 'stop')}
          disabled={actionLoading['mihomo-stop']}
        >
          {actionLoading['mihomo-stop'] ? 'Остановка...' : '⏹ Остановить'}
        </button>
        <button 
          class="btn btn-secondary" 
          on:click={() => controlService('mihomo', 'restart')}
          disabled={actionLoading['mihomo-restart']}
        >
          {actionLoading['mihomo-restart'] ? 'Перезапуск...' : '🔄 Перезапустить'}
        </button>
      </div>
    </div>
  </div>

  <div class="card mt-2">
    <h3>Обновить статус</h3>
    <button class="btn btn-secondary" on:click={fetchStatus} disabled={loading}>
      {loading ? 'Обновление...' : '🔄 Обновить'}
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