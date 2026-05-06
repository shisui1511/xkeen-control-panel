<script lang="ts">
  import { onMount } from 'svelte'
  import Editor from './Editor.svelte'
  import Logs from './Logs.svelte'
  import Services from './Services.svelte'
  import Settings from './Settings.svelte'
  import Proxies from './Proxies.svelte'
  import Connections from './Connections.svelte'

  let version = 'loading...'
  let loading = false
  let currentTab = 'dashboard'
  let theme = document.documentElement.getAttribute('data-theme') || 'light'

  function toggleTheme() {
    theme = theme === 'dark' ? 'light' : 'dark'
    document.documentElement.setAttribute('data-theme', theme)
    localStorage.setItem('theme', theme)
  }

  async function fetchVersion() {
    try {
      const res = await fetch('/api/version')
      const data = await res.json()
      version = data.version
    } catch (e) {
      version = 'error'
    }
  }

  async function handleLogout() {
    loading = true
    try {
      const csrfToken = localStorage.getItem('csrf_token')
      await fetch('/api/auth/logout', {
        method: 'POST',
        headers: {
          'X-CSRF-Token': csrfToken || ''
        }
      })
      localStorage.removeItem('csrf_token')
      window.location.href = '/'
    } catch (e) {
      console.error('Logout error:', e)
    } finally {
      loading = false
    }
  }

  function switchTab(tab: string) {
    currentTab = tab
  }

  onMount(() => {
    fetchVersion()
  })
</script>

<div class="dashboard-layout">
  <div class="sidebar" style="display: flex; flex-direction: column;">
    <div class="sidebar-logo">⚡ XKeen CP</div>
    <nav style="flex: 1; overflow-y: auto;">
      <button class="nav-item" class:active={currentTab === 'dashboard'} on:click={() => switchTab('dashboard')}>
        📊 Dashboard
      </button>
      <button class="nav-item" class:active={currentTab === 'editor'} on:click={() => switchTab('editor')}>
        📝 Editor
      </button>
      <button class="nav-item" class:active={currentTab === 'logs'} on:click={() => switchTab('logs')}>
        📋 Logs
      </button>
      <button class="nav-item" class:active={currentTab === 'proxies'} on:click={() => switchTab('proxies')}>
        🌐 Proxies
      </button>
      <button class="nav-item" class:active={currentTab === 'connections'} on:click={() => switchTab('connections')}>
        🔗 Connections
      </button>
      <button class="nav-item" class:active={currentTab === 'services'} on:click={() => switchTab('services')}>
        🚀 Services
      </button>
      <button class="nav-item" class:active={currentTab === 'settings'} on:click={() => switchTab('settings')}>
        ⚙️ Settings
      </button>
    </nav>
    <div style="border-top: 1px solid var(--border); padding: 0.5rem 0;">
      <button class="nav-item" on:click={toggleTheme}>
        {theme === 'dark' ? '☀️' : '🌙'} {theme === 'dark' ? 'Светлая тема' : 'Тёмная тема'}
      </button>
      <button class="nav-item" on:click={handleLogout} disabled={loading}>
        🚪 {loading ? 'Выход...' : 'Выйти'}
      </button>
    </div>
  </div>

  <div class="main-content">
    {#if currentTab === 'dashboard'}
      <div class="container">
        <h1>Dashboard</h1>
        <p class="text-secondary mb-3">Добро пожаловать в панель управления XKeen</p>

        <div class="card mb-2">
          <h2>Информация о системе</h2>
          <p><strong>Версия:</strong> {version}</p>
          <p><strong>Статус:</strong> <span class="status-dot success"></span> Работает</p>
          <p class="text-secondary">v0.2.0 — Config Editor + Unified Logs</p>
        </div>

        <div class="card mb-2">
          <h2>Быстрые действия</h2>
          <p class="text-secondary mb-2">Основные функции будут доступны в следующих версиях:</p>
          <ul style="list-style: none; padding-left: 0;">
            <li>✅ Авторизация (bcrypt + HMAC cookie + CSRF)</li>
            <li>✅ Минималистичный дизайн (light/dark темы)</li>
            <li>🔄 v0.2.0 — Config Editor + Unified Logs (в разработке)</li>
            <li>⏳ v0.3.0 — Mihomo Dashboard (proxies, connections, rules)</li>
            <li>⏳ v0.4.0 — Subscriptions + Smart Proxy Manager</li>
          </ul>
        </div>

      </div>
    {:else if currentTab === 'editor'}
      <Editor />
    {:else if currentTab === 'logs'}
      <Logs />
    {:else if currentTab === 'proxies'}
      <Proxies />
    {:else if currentTab === 'connections'}
      <Connections />
    {:else if currentTab === 'services'}
      <Services />
    {:else if currentTab === 'settings'}
      <Settings />
    {/if}
  </div>
</div>
