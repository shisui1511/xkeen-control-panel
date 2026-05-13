<script lang="ts">
  import { onMount, onDestroy } from 'svelte'
  import { t, setLang, currentLang, getAvailableLangs, type Lang } from './i18n'

  let version = '...'
  let langs = getAvailableLangs()

  // Update state
  let updateInfo: { current_version: string; latest_version: string; has_update: boolean; channel: string; changelog?: string } | null = null
  let updateStatus: { status: string; message: string; progress: number } | null = null
  let updateChecking = false
  let updateInstalling = false
  let statusInterval: ReturnType<typeof setInterval>

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

  async function checkUpdate(channel: string = 'stable') {
    updateChecking = true
    try {
      const res = await fetch(`/api/update/check?channel=${channel}`)
      if (res.ok) {
        updateInfo = await res.json()
        if (updateInfo?.has_update && updateInfo?.latest_version) {
          await fetchChangelog(updateInfo.latest_version)
        }
      }
    } catch (e) {
      // ignore
    } finally {
      updateChecking = false
    }
  }

  async function fetchChangelog(version: string) {
    try {
      const res = await fetch(`/api/update/changelog?version=${version}`)
      if (res.ok && updateInfo) {
        updateInfo.changelog = await res.text()
      }
    } catch (e) {
      // ignore
    }
  }

  async function installUpdate(channel: string = 'stable') {
    updateInstalling = true
    try {
      const res = await fetch(`/api/update/install?channel=${channel}`, { method: 'POST' })
      if (res.ok) {
        startStatusPolling()
      }
    } catch (e) {
      updateInstalling = false
    }
  }

  async function rollbackUpdate() {
    try {
      const res = await fetch('/api/update/rollback')
      if (res.ok) {
        startStatusPolling()
      }
    } catch (e) {
      // ignore
    }
  }

  async function fetchStatus() {
    try {
      const res = await fetch('/api/update/status')
      if (res.ok) {
        updateStatus = await res.json()
        if (updateStatus?.status === 'done' || updateStatus?.status === 'failed') {
          updateInstalling = false
          clearInterval(statusInterval)
        }
      }
    } catch (e) {
      clearInterval(statusInterval)
      updateInstalling = false
    }
  }

  function startStatusPolling() {
    fetchStatus()
    statusInterval = setInterval(fetchStatus, 2000)
  }

  onMount(() => {
    fetchVersion()
  })

  onDestroy(() => {
    if (statusInterval) clearInterval(statusInterval)
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
      <span class="setting-value">Svelte 5 + TypeScript + Vite</span>
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
    <h2>{$t('settings.update')}</h2>
    <div class="setting-row">
      <span class="setting-label">{$t('settings.current_version')}</span>
      <span class="setting-value">{version}</span>
    </div>

    {#if updateInfo?.has_update}
      <div class="setting-row">
        <span class="setting-label">{$t('settings.available_version')}</span>
        <span class="setting-value" style="color: var(--primary)">{updateInfo.latest_version}</span>
      </div>
      {#if updateInfo.changelog}
        <div class="changelog-box">
          <pre>{updateInfo.changelog}</pre>
        </div>
      {/if}
    {/if}

    {#if updateStatus && updateStatus.status !== 'idle'}
      <div class="update-progress">
        <div class="progress-bar">
          <div class="progress-fill" style="width: {updateStatus.progress}%"></div>
        </div>
        <span class="progress-text">{updateStatus.message}</span>
      </div>
    {/if}

    <div class="update-actions">
      <button class="btn btn-secondary" on:click={() => checkUpdate('stable')} disabled={updateChecking || updateInstalling}>
        {updateChecking ? $t('settings.checking') : $t('settings.check_update')}
      </button>
      {#if updateInfo?.has_update}
        <button class="btn btn-primary" on:click={() => installUpdate('stable')} disabled={updateInstalling}>
          {updateInstalling ? $t('settings.installing') : $t('settings.install_update')}
        </button>
      {/if}
      {#if updateStatus?.status === 'failed'}
        <button class="btn btn-danger" on:click={rollbackUpdate}>
          {$t('settings.rollback')}
        </button>
      {/if}
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

  .changelog-box {
    margin: 0.75rem 0;
    padding: 0.75rem;
    background: var(--bg);
    border: 1px solid var(--border);
    border-radius: var(--radius);
    max-height: 200px;
    overflow-y: auto;
  }

  .changelog-box pre {
    margin: 0;
    white-space: pre-wrap;
    word-wrap: break-word;
    font-size: 0.8rem;
    color: var(--fg-secondary);
  }

  .update-actions {
    display: flex;
    gap: 0.5rem;
    margin-top: 0.75rem;
    flex-wrap: wrap;
  }

  .update-progress {
    margin: 0.75rem 0;
  }

  .progress-bar {
    height: 8px;
    background: var(--border);
    border-radius: 4px;
    overflow: hidden;
    margin-bottom: 0.5rem;
  }

  .progress-fill {
    height: 100%;
    background: var(--primary);
    border-radius: 4px;
    transition: width 0.3s ease;
  }

  .progress-text {
    font-size: 0.8rem;
    color: var(--fg-secondary);
  }
</style>