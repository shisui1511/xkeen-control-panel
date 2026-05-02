<script lang="ts">
  import { onMount } from 'svelte'
  import Editor from './Editor.svelte'
  import Logs from './Logs.svelte'

  let version = 'loading...'
  let loading = false
  let currentTab = 'dashboard'

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
  <div class="sidebar">
    <div class="sidebar-logo">⚡ XKeen CP</div>
    <nav>
      <button class="nav-item" class:active={currentTab === 'dashboard'} on:click={() => switchTab('dashboard')}>
        📊 Dashboard
      </button>
      <button class="nav-item" class:active={currentTab === 'editor'} on:click={() => switchTab('editor')}>
        📝 Editor
      </button>
      <button class="nav-item" class:active={currentTab === 'logs'} on:click={() => switchTab('logs')}>
        📋 Logs
      </button>
      <button class="nav-item" class:active={currentTab === 'services'} on:click={() => switchTab('services')}>
        🚀 Services
      </button>
      <button class="nav-item" class:active={currentTab === 'settings'} on:click={() => switchTab('settings')}>
        ⚙️ Settings
      </button>
    </nav>
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

        <div class="card">
          <h3>Выход</h3>
          <button
            class="btn btn-danger"
            on:click={handleLogout}
            disabled={loading}
          >
            {loading ? 'Выход...' : 'Выйти из системы'}
          </button>
        </div>
      </div>
    {:else if currentTab === 'editor'}
      <Editor />
    {:else if currentTab === 'logs'}
      <Logs />
    {:else if currentTab === 'services'}
      <div class="container">
        <h1>Services</h1>
        <p class="text-secondary">Управление сервисами (в разработке)</p>
      </div>
    {:else if currentTab === 'settings'}
      <div class="container">
        <h1>Settings</h1>
        <p class="text-secondary">Настройки (в разработке)</p>
      </div>
    {/if}
  </div>
</div>
