<script lang="ts">
  import { onMount } from 'svelte'

  let version = '...'

  async function fetchVersion() {
    try {
      const res = await fetch('/api/version')
      const data = await res.json()
      version = data.version
    } catch (e) {
      version = 'недоступно'
    }
  }

  onMount(() => {
    fetchVersion()
  })
</script>

<div class="container">
  <h1>Настройки</h1>
  <p class="text-secondary mb-3">Информация о панели управления</p>

  <div class="card mb-2">
    <h2>О системе</h2>
    <div class="setting-row">
      <span class="setting-label">Версия</span>
      <span class="setting-value">{version}</span>
    </div>
    <div class="setting-row">
      <span class="setting-label">Frontend</span>
      <span class="setting-value">Svelte 4 + TypeScript + Vite</span>
    </div>
    <div class="setting-row">
      <span class="setting-label">Backend</span>
      <span class="setting-value">Go + net/http</span>
    </div>
  </div>

  <div class="card mb-2">
    <h2>Безопасность</h2>
    <ul style="list-style: none; padding-left: 0;">
      <li class="mb-1">✅ Авторизация с bcrypt и HMAC-сессиями</li>
      <li class="mb-1">✅ CSRF-защита для изменяющих запросов</li>
      <li class="mb-1">✅ Rate limiting (5 попыток входа)</li>
      <li class="mb-1">✅ Security headers (CSP, XSS, Clickjacking)</li>
    </ul>
  </div>

  <div class="card">
    <h2>Roadmap</h2>
    <ul style="list-style: none; padding-left: 0;">
      <li class="mb-1">✅ v0.1.0 — Auth + Design Foundation</li>
      <li class="mb-1">🔄 v0.2.0 — Config Editor + Unified Logs</li>
      <li class="mb-1">⏳ v0.3.0 — Mihomo Dashboard</li>
      <li class="mb-1">⏳ v0.4.0 — Subscriptions + Smart Proxy</li>
      <li class="mb-1">⏳ v0.5.0 — Network Tools + Notifications</li>
    </ul>
  </div>
</div>

<style>
  .setting-row {
    display: flex;
    justify-content: space-between;
    padding: 0.75rem 0;
    border-bottom: 1px solid var(--border-light);
  }

  .setting-row:last-child {
    border-bottom: none;
  }

  .setting-label {
    color: var(--fg-secondary);
  }

  .setting-value {
    font-weight: 500;
  }
</style>