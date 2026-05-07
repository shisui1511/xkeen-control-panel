<script lang="ts">
  import { onMount } from 'svelte'
  import { t, setLang, currentLang, getAvailableLangs, type Lang } from './i18n'

  let version = '...'
  let langs = getAvailableLangs()

  async function fetchVersion() {
    try {
      const res = await fetch('/api/version')
      const data = await res.json()
      version = data.version
    } catch (e) {
      version = $t('app.unavailable')
    }
  }

  function handleLangChange(e: Event) {
    const select = e.target as HTMLSelectElement
    setLang(select.value as Lang)
  }

  onMount(() => {
    fetchVersion()
  })
</script>

<div class="container">
  <h1>{$t('settings.title')}</h1>
  <p class="text-secondary mb-3">{$t('settings.subtitle')}</p>

  <div class="card mb-2">
    <h2>{$t('settings.about')}</h2>
    <div class="setting-row">
      <span class="setting-label">{$t('settings.version')}</span>
      <span class="setting-value">{version}</span>
    </div>
    <div class="setting-row">
      <span class="setting-label">{$t('settings.frontend')}</span>
      <span class="setting-value">Svelte 4 + TypeScript + Vite</span>
    </div>
    <div class="setting-row">
      <span class="setting-label">{$t('settings.backend')}</span>
      <span class="setting-value">Go + net/http</span>
    </div>
  </div>

  <div class="card mb-2">
    <h2>{$t('settings.language')}</h2>
    <div class="setting-row">
      <span class="setting-label">{$t('settings.language')}</span>
      <select class="input" value={$currentLang} on:change={handleLangChange}>
        {#each langs as lang}
          <option value={lang.code}>{lang.name}</option>
        {/each}
      </select>
    </div>
  </div>

  <div class="card mb-2">
    <h2>{$t('settings.security')}</h2>
    <ul style="list-style: none; padding-left: 0;">
      <li class="mb-1">✅ {$t('settings.auth_bcrypt')}</li>
      <li class="mb-1">✅ {$t('settings.csrf')}</li>
      <li class="mb-1">✅ {$t('settings.rate_limit')}</li>
      <li class="mb-1">✅ {$t('settings.security_headers')}</li>
    </ul>
  </div>

  <div class="card">
    <h2>{$t('settings.roadmap')}</h2>
    <ul style="list-style: none; padding-left: 0;">
      <li class="mb-1">✅ v0.1.0 — Auth + Design Foundation</li>
      <li class="mb-1">✅ v0.2.0 — Config Editor + Unified Logs</li>
      <li class="mb-1">✅ v0.3.0 — Mihomo Dashboard</li>
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