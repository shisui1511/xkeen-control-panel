<script lang="ts">
  import { onMount } from 'svelte'

  let version = 'loading...'
  let loading = false

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

  onMount(() => {
    fetchVersion()
  })
</script>

<div class="sidebar">
  <div class="sidebar-logo">⚡ XKeen CP</div>
  <nav>
    <a href="#dashboard" class="nav-item active">📊 Dashboard</a>
    <a href="#editor" class="nav-item">📝 Editor</a>
    <a href="#logs" class="nav-item">📋 Logs</a>
    <a href="#services" class="nav-item">🚀 Services</a>
    <a href="#settings" class="nav-item">⚙️ Settings</a>
  </nav>
</div>

<div class="main-content">
  <div class="container">
    <h1>Dashboard</h1>
    <p class="text-secondary mb-3">Добро пожаловать в панель управления XKeen</p>

    <div class="card mb-2">
      <h2>Информация о системе</h2>
      <p><strong>Версия:</strong> {version}</p>
      <p><strong>Статус:</strong> <span class="status-dot success"></span> Работает</p>
      <p class="text-secondary">v0.1.0 — Auth + Design Foundation</p>
    </div>

    <div class="card mb-2">
      <h2>Быстрые действия</h2>
      <p class="text-secondary mb-2">Основные функции будут доступны в следующих версиях:</p>
      <ul style="list-style: none; padding-left: 0;">
        <li>✅ Авторизация (bcrypt + HMAC cookie + CSRF)</li>
        <li>✅ Минималистичный дизайн (light/dark темы)</li>
        <li>⏳ v0.2.0 — Config Editor + Unified Logs</li>
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
</div>
