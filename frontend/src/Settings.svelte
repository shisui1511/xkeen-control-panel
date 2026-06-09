<script lang="ts">
  import { onMount, onDestroy } from 'svelte';
  import { t, setLang, currentLang, getAvailableLangs, type Lang } from './i18n';
  import Icon from './lib/components/Icon.svelte';
  import {
    capabilities,
    fetchCapabilities,
    showToast,
    devMode,
    fetchDevMode,
    setDevMode
  } from './stores';

  export let onSwitchTab: (tab: string) => void = () => {};

  let checkingConnection = false;
  let secretVisible = false;

  async function recheckConnection() {
    checkingConnection = true;
    try {
      await fetchCapabilities();
    } finally {
      checkingConnection = false;
    }
  }

  let version = '...';
  let langs = getAvailableLangs();
  let activeTab: 'general' | 'updates' | 'security' | 'connection' | 'backups' | 'about' =
    'general';

  // Backups state variables
  let configFiles: string[] = [];
  let selectedFile = '';
  let backups: string[] = [];
  let loadingBackups = false;
  let backupsLoaded = false;

  // Snapshots state
  interface SnapshotMeta {
    id: string;
    label: string;
    created_at: number;
    size_bytes: number;
  }
  let snapshots: SnapshotMeta[] = [];
  let snapshotLabel = '';
  let creatingSnapshot = false;
  let restoringSnapshot = '';

  async function fetchSnapshots() {
    try {
      const res = await fetch('/api/snapshots/list');
      if (res.ok) snapshots = (await res.json()) ?? [];
    } catch (_) {}
  }

  async function createSnapshot() {
    creatingSnapshot = true;
    try {
      const csrfToken = localStorage.getItem('csrf_token');
      const res = await fetch('/api/snapshots/create', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrfToken || '' },
        body: JSON.stringify({ label: snapshotLabel })
      });
      if (res.ok) {
        snapshotLabel = '';
        showToast('success', $t('settings.snapshot_created'));
        fetchSnapshots();
      } else {
        showToast('error', await res.text());
      }
    } catch (e: any) {
      showToast('error', e.message);
    } finally {
      creatingSnapshot = false;
    }
  }

  async function restoreSnapshot(id: string) {
    if (!confirm($t('settings.snapshot_restore_confirm'))) return;
    restoringSnapshot = id;
    try {
      const csrfToken = localStorage.getItem('csrf_token');
      const res = await fetch(`/api/snapshots/${id}/restore`, {
        method: 'POST',
        headers: { 'X-CSRF-Token': csrfToken || '' }
      });
      if (res.ok) {
        showToast('success', $t('settings.snapshot_restored'));
      } else {
        showToast('error', await res.text());
      }
    } catch (e: any) {
      showToast('error', e.message);
    } finally {
      restoringSnapshot = '';
    }
  }

  async function deleteSnapshot(id: string) {
    if (!confirm($t('settings.snapshot_delete_confirm'))) return;
    try {
      const csrfToken = localStorage.getItem('csrf_token');
      const res = await fetch(`/api/snapshots/${id}/delete`, {
        method: 'POST',
        headers: { 'X-CSRF-Token': csrfToken || '' }
      });
      if (res.ok) {
        showToast('success', $t('settings.snapshot_deleted'));
        fetchSnapshots();
      } else {
        showToast('error', await res.text());
      }
    } catch (e: any) {
      showToast('error', e.message);
    }
  }

  function formatSnapshotSize(bytes: number): string {
    if (bytes < 1024) return bytes + ' B';
    if (bytes < 1024 * 1024) return (bytes / 1024).toFixed(1) + ' KB';
    return (bytes / (1024 * 1024)).toFixed(1) + ' MB';
  }

  async function loadConfigFiles() {
    try {
      const xrayRes = await fetch('/api/config/list?dir=/opt/etc/xray/configs');
      const mihomoRes = await fetch('/api/config/list?dir=/opt/etc/mihomo');

      let files: string[] = [];
      if (xrayRes.ok) {
        const data = await xrayRes.json();
        files = [...files, ...data.map((f: any) => f.path)];
      }
      if (mihomoRes.ok) {
        const data = await mihomoRes.json();
        files = [...files, ...data.map((f: any) => f.path)];
      }

      files.push('/opt/etc/xcp/config.json');

      configFiles = Array.from(new Set(files)).sort();
      if (configFiles.length > 0 && !selectedFile) {
        selectedFile = configFiles[0];
        fetchBackups();
      }
    } catch (e) {
      console.error(e);
    }
  }

  async function fetchBackups() {
    if (!selectedFile) return;
    loadingBackups = true;
    try {
      const res = await fetch(`/api/config/backups?path=${encodeURIComponent(selectedFile)}`);
      if (res.ok) {
        backups = (await res.json()) ?? [];
      } else {
        backups = [];
      }
    } catch (e) {
      backups = [];
    } finally {
      loadingBackups = false;
    }
  }

  async function restoreBackup(backupPath: string) {
    if (!confirm($t('settings.backup_restore_confirm'))) return;
    const targetFile = selectedFile; // capture before any await to prevent TOCTOU race
    if (!targetFile) return;
    try {
      const readRes = await fetch(`/api/config/read?path=${encodeURIComponent(backupPath)}`);
      if (!readRes.ok) {
        const txt = await readRes.text();
        showToast('error', `${$t('settings.backup_read_error')}: ${txt}`);
        return;
      }
      const data = await readRes.text();

      const csrfToken = localStorage.getItem('csrf_token');
      const saveRes = await fetch(`/api/config/save?path=${encodeURIComponent(targetFile)}`, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          'X-CSRF-Token': csrfToken || ''
        },
        body: data
      });
      if (saveRes.ok) {
        showToast('success', $t('settings.backup_restore_success'));
        fetchBackups();
      } else {
        const txt = await saveRes.text();
        showToast('error', `${$t('settings.backup_restore_error')}: ${txt}`);
      }
    } catch (e: any) {
      showToast('error', e.message);
    }
  }

  async function deleteBackup(backupPath: string) {
    if (!confirm($t('settings.backup_delete_confirm'))) return;
    try {
      const csrfToken = localStorage.getItem('csrf_token');
      const res = await fetch(`/api/config/delete?path=${encodeURIComponent(backupPath)}`, {
        method: 'POST',
        headers: {
          'X-CSRF-Token': csrfToken || ''
        }
      });
      if (res.ok) {
        showToast('success', $t('settings.backup_delete_success'));
        fetchBackups();
      } else {
        const txt = await res.text();
        showToast('error', `Ошибка удаления: ${txt}`);
      }
    } catch (e: any) {
      showToast('error', e.message);
    }
  }

  async function createBackup() {
    if (!selectedFile) return;
    // Backup is created as a side-effect of ConfigSave (internal/handlers/config.go):
    // the handler always writes a timestamped .backup-* file before overwriting.
    // There is no dedicated POST /api/config/backup endpoint.
    try {
      const readRes = await fetch(`/api/config/read?path=${encodeURIComponent(selectedFile)}`);
      if (!readRes.ok) {
        showToast('error', await readRes.text());
        return;
      }
      const data = await readRes.text();
      const csrfToken = localStorage.getItem('csrf_token');
      const saveRes = await fetch(`/api/config/save?path=${encodeURIComponent(selectedFile)}`, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          'X-CSRF-Token': csrfToken || ''
        },
        body: data
      });
      if (saveRes.ok) {
        showToast('success', $t('settings.backup_created'));
        await fetchBackups();
      } else {
        showToast('error', await saveRes.text());
      }
    } catch (e: any) {
      showToast('error', e.message);
    }
  }

  $: if (activeTab === 'backups' && !backupsLoaded) {
    backupsLoaded = true;
    loadConfigFiles();
    fetchSnapshots();
  }
  $: if (activeTab !== 'backups') {
    backupsLoaded = false;
  }

  // Appearance & Behavior settings (persisted in localStorage)
  let selectedTheme: 'light' | 'dark' | 'auto' = 'auto';
  let timezone = 'UTC';
  let animationsEnabled = true;
  let autoRefresh = true;
  let confirmDangerous = true;
  let notificationSound = false;

  function loadAppearanceSettings() {
    try {
      const saved = localStorage.getItem('theme') || '';
      selectedTheme = saved === 'light' || saved === 'dark' ? saved : 'auto';
      timezone = localStorage.getItem('timezone') || 'UTC';
      animationsEnabled = localStorage.getItem('animations') !== 'false';
      autoRefresh = localStorage.getItem('autoRefresh') !== 'false';
      confirmDangerous = localStorage.getItem('confirmDangerous') !== 'false';
      notificationSound = localStorage.getItem('notificationSound') === 'true';
    } catch {}
  }

  function setTheme(t: 'light' | 'dark' | 'auto') {
    selectedTheme = t;
    try {
      if (t === 'auto') {
        localStorage.removeItem('theme');
        const prefersDark = window.matchMedia('(prefers-color-scheme: dark)').matches;
        document.documentElement.setAttribute('data-theme', prefersDark ? 'dark' : 'light');
      } else {
        localStorage.setItem('theme', t);
        document.documentElement.setAttribute('data-theme', t);
      }
    } catch {}
  }

  function saveSetting(key: string, value: string) {
    try {
      localStorage.setItem(key, value);
    } catch {}
  }

  // Change password
  let currentPassword = '';
  let newPassword = '';
  let confirmPassword = '';
  let passwordChanging = false;
  let passwordError = '';
  let passwordSuccess = false;

  async function changePassword() {
    passwordError = '';
    passwordSuccess = false;
    if (newPassword !== confirmPassword) {
      passwordError = $t('settings.password_mismatch');
      return;
    }
    passwordChanging = true;
    try {
      const csrfToken = localStorage.getItem('csrf_token');
      const res = await fetch('/api/auth/change-password', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrfToken || '' },
        body: JSON.stringify({ current_password: currentPassword, new_password: newPassword })
      });
      if (res.ok) {
        passwordSuccess = true;
        currentPassword = '';
        newPassword = '';
        confirmPassword = '';
      } else {
        const text = await res.text();
        passwordError = text || $t('settings.password_error');
      }
    } catch (e: any) {
      passwordError = e.message;
    } finally {
      passwordChanging = false;
    }
  }

  // Update state
  let updateInfo: {
    current_version: string;
    latest_version: string;
    has_update: boolean;
    channel: string;
    changelog?: string;
  } | null = null;
  let updateStatus: { status: string; message: string; progress: number } | null = null;
  let updateChecking = false;
  let updateInstalling = false;
  let updateChannel: 'stable' | 'beta' = 'stable';

  async function fetchUpdateChannel() {
    try {
      const res = await fetch('/api/update/channel');
      if (res.ok) {
        const data = await res.json();
        updateChannel = data.channel ?? 'stable';
      }
    } catch (_) {}
  }

  async function saveUpdateChannel(ch: 'stable' | 'beta') {
    const prev = updateChannel;
    try {
      const csrfToken = localStorage.getItem('csrf_token');
      const res = await fetch('/api/update/channel', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrfToken || '' },
        body: JSON.stringify({ channel: ch })
      });
      if (res.ok) {
        updateChannel = ch;
        showToast('success', $t('settings.channel_saved'));
      } else {
        updateChannel = prev;
        showToast('error', await res.text());
      }
    } catch (e) {
      updateChannel = prev;
      showToast('error', e instanceof Error ? e.message : String(e));
    }
  }

  async function fetchVersion() {
    try {
      const res = await fetch('/api/version');
      if (!res.ok) { version = $t('app.unavailable'); return; }
      const data = await res.json();
      version = data.panel_version || data.version || $t('app.unavailable');
    } catch (e) {
      version = $t('app.unavailable');
    }
  }

  function handleLangChange(e: Event) {
    const select = e.target as HTMLSelectElement;
    setLang(select.value as Lang);
  }

  async function checkUpdate(channel: string = updateChannel) {
    updateChecking = true;
    try {
      const res = await fetch(`/api/update/check?channel=${channel}`);
      if (res.ok) {
        const envelope = await res.json();
        // UpdateCheck uses JSONSuccess envelope: {success, data: {...}}
        updateInfo = envelope.data ?? envelope;
        if (updateInfo?.has_update && updateInfo?.latest_version) {
          await fetchChangelog(updateInfo.latest_version);
        }
      } else {
        showToast('error', await res.text());
      }
    } catch (e) {
      showToast('error', e instanceof Error ? e.message : String(e));
    } finally {
      updateChecking = false;
    }
  }

  async function fetchChangelog(version: string) {
    try {
      const res = await fetch(`/api/update/changelog?version=${version}`);
      if (res.ok && updateInfo) {
        updateInfo.changelog = await res.text();
      }
    } catch (e) {
      // ignore
    }
  }

  let sseSource: EventSource | null = null;
  let showConfirmUpdateModal = false;

  // Reconnect/polling state after update restart
  let reconnecting = false;
  let reconnectAttempt = 0;
  let reconnectTimer: ReturnType<typeof setTimeout> | null = null;
  const RECONNECT_INTERVAL_MS = 1500;
  const RECONNECT_MAX_ATTEMPTS = 40; // 40 × 1.5s = 60s max

  function stopReconnectPolling() {
    if (reconnectTimer !== null) {
      clearTimeout(reconnectTimer);
      reconnectTimer = null;
    }
    reconnecting = false;
    reconnectAttempt = 0;
  }

  function startReconnectPolling() {
    if (reconnecting) return;
    reconnecting = true;
    reconnectAttempt = 0;
    pollVersion();
  }

  function pollVersion() {
    reconnectTimer = setTimeout(async () => {
      reconnectAttempt++;
      try {
        const res = await fetch('/api/version', { cache: 'no-store' });
        if (res.ok) {
          // New server is up — reload the page
          stopReconnectPolling();
          window.location.reload();
          return;
        }
      } catch (_) {
        // Server not yet up — continue polling
      }
      if (reconnectAttempt < RECONNECT_MAX_ATTEMPTS) {
        pollVersion();
      } else {
        // Give up — let user know
        stopReconnectPolling();
        updateInstalling = false;
        updateStatus = {
          status: 'failed',
          message: $t('settings.update_reconnect_timeout'),
          progress: 0
        };
      }
    }, RECONNECT_INTERVAL_MS);
  }

  function startStatusSSE() {
    if (sseSource) {
      sseSource.close();
    }
    updateInstalling = true;
    sseSource = new EventSource('/api/update/events');
    sseSource.onmessage = (event) => {
      try {
        const state = JSON.parse(event.data);
        updateStatus = state;
        if (state.status === 'restarting') {
          // Server is about to shut down — switch to polling mode
          sseSource?.close();
          sseSource = null;
          startReconnectPolling();
        } else if (state.status === 'done') {
          updateInstalling = false;
          sseSource?.close();
          sseSource = null;
          // Reload so the page reflects the new version
          window.location.reload();
        } else if (state.status === 'failed') {
          updateInstalling = false;
          sseSource?.close();
          sseSource = null;
        }
      } catch (e) {
        // ignore
      }
    };
    sseSource.onerror = () => {
      sseSource?.close();
      sseSource = null;
      // If we were in the middle of an install (not done/failed), start polling
      if (updateInstalling && !reconnecting) {
        startReconnectPolling();
      } else {
        updateInstalling = false;
      }
    };
  }

  async function installUpdate(channel: string = updateChannel) {
    updateInstalling = true;
    try {
      const csrfToken = localStorage.getItem('csrf_token');
      const res = await fetch(`/api/update/install?channel=${channel}`, {
        method: 'POST',
        headers: { 'X-CSRF-Token': csrfToken || '' }
      });
      if (res.ok) {
        startStatusSSE();
      } else {
        updateInstalling = false;
        showToast('error', await res.text());
      }
    } catch (e) {
      updateInstalling = false;
      showToast('error', e instanceof Error ? e.message : String(e));
    }
  }

  async function rollbackUpdate() {
    updateInstalling = true;
    try {
      const csrfToken = localStorage.getItem('csrf_token');
      const res = await fetch('/api/update/rollback', {
        method: 'POST',
        headers: { 'X-CSRF-Token': csrfToken || '' }
      });
      if (res.ok) {
        startStatusSSE();
      } else {
        updateInstalling = false;
        showToast('error', await res.text());
      }
    } catch (e) {
      updateInstalling = false;
      showToast('error', e instanceof Error ? e.message : String(e));
    }
  }

  async function fetchStatus() {
    try {
      const res = await fetch('/api/update/status');
      if (res.ok) {
        const envelope = await res.json();
        updateStatus = envelope.data ?? envelope;
        if (updateStatus?.status === 'done' || updateStatus?.status === 'failed') {
          updateInstalling = false;
        }
      }
    } catch (e) {
      updateInstalling = false;
    }
  }

  onMount(async () => {
    fetchVersion();
    fetchCapabilities();
    fetchDevMode();
    loadAppearanceSettings();
    fetchUpdateChannel();

    await fetchStatus();
    if (updateStatus && !['idle', 'done', 'failed'].includes(updateStatus.status)) {
      startStatusSSE();
    }
  });

  onDestroy(() => {
    if (sseSource) {
      sseSource.close();
      sseSource = null;
    }
    stopReconnectPolling();
  });
</script>

<div class="container">
  <!-- page-head -->
  <div class="page-head">
    <div>
      <div class="crumbs">
        {$t('nav.group_core')} <span class="crumb-sep">/</span>
        {$t('settings.h1')}
      </div>
      <h1>{$t('settings.h1')}</h1>
      <p class="sub">{$t('settings.h1_sub')}</p>
    </div>
  </div>

  <!-- tab nav -->
  <div class="settings-tabs">
    <button
      class="stab"
      class:active={activeTab === 'general'}
      on:click={() => (activeTab = 'general')}>{$t('settings.tab_general')}</button
    >
    <button
      class="stab"
      class:active={activeTab === 'updates'}
      on:click={() => (activeTab = 'updates')}>{$t('settings.tab_updates')}</button
    >
    <button
      class="stab"
      class:active={activeTab === 'security'}
      on:click={() => (activeTab = 'security')}>{$t('settings.tab_security')}</button
    >
    <button
      class="stab"
      class:active={activeTab === 'connection'}
      on:click={() => (activeTab = 'connection')}>{$t('settings.tab_connection')}</button
    >
    <button
      class="stab"
      class:active={activeTab === 'backups'}
      on:click={() => (activeTab = 'backups')}>{$t('settings.tab_backups')}</button
    >
    <button class="stab" class:active={activeTab === 'about'} on:click={() => (activeTab = 'about')}
      >{$t('settings.tab_about')}</button
    >
  </div>

  <!-- General tab -->
  {#if activeTab === 'general'}
    <div class="card mb-2">
      <div class="card-label">{$t('settings.section_locale')}</div>
      <div class="field-group">
        <div class="field-row">
          <span class="field-row-name">{$t('settings.language')}</span>
          <select
            class="field-select"
            value={$currentLang}
            on:change={handleLangChange}
            title={$t('settings.language')}
          >
            {#each langs as lang}
              <option value={lang.code}>{lang.name}</option>
            {/each}
          </select>
        </div>
        <div class="field-row">
          <div>
            <span class="field-row-name">{$t('settings.timezone')}</span>
            <div class="field-row-desc">{$t('settings.timezone_desc')}</div>
          </div>
          <select
            class="field-select"
            bind:value={timezone}
            on:change={() => saveSetting('timezone', timezone)}
            title={$t('settings.timezone')}
          >
            <option value="UTC">UTC</option>
            <option value="Europe/Moscow">Europe/Moscow (UTC+3)</option>
            <option value="Europe/London">Europe/London</option>
            <option value="Europe/Berlin">Europe/Berlin</option>
            <option value="America/New_York">America/New_York</option>
            <option value="America/Los_Angeles">America/Los_Angeles</option>
            <option value="Asia/Tokyo">Asia/Tokyo</option>
            <option value="Asia/Shanghai">Asia/Shanghai</option>
          </select>
        </div>
      </div>
    </div>

    <div class="card mb-2">
      <div class="card-label">{$t('settings.section_appearance')}</div>
      <div class="field-group">
        <div class="field-row">
          <div>
            <span class="field-row-name">{$t('settings.theme')}</span>
            <div class="field-row-desc">{$t('settings.theme_desc')}</div>
          </div>
          <div class="seg-btn">
            <button
              class="seg-opt"
              class:seg-active={selectedTheme === 'light'}
              on:click={() => setTheme('light')}>{$t('settings.theme_light_btn')}</button
            >
            <button
              class="seg-opt"
              class:seg-active={selectedTheme === 'dark'}
              on:click={() => setTheme('dark')}>{$t('settings.theme_dark_btn')}</button
            >
            <button
              class="seg-opt"
              class:seg-active={selectedTheme === 'auto'}
              on:click={() => setTheme('auto')}>{$t('settings.theme_auto_btn')}</button
            >
          </div>
        </div>
        <div class="field-row">
          <div>
            <span class="field-row-name">{$t('settings.animations')}</span>
            <div class="field-row-desc">{$t('settings.animations_desc')}</div>
          </div>
          <label class="toggle">
            <input
              type="checkbox"
              bind:checked={animationsEnabled}
              on:change={() => saveSetting('animations', String(animationsEnabled))}
            />
            <span class="toggle-track"><span class="toggle-thumb"></span></span>
          </label>
        </div>
      </div>
    </div>

    <div class="card mb-2">
      <div class="card-label">{$t('settings.section_behavior')}</div>
      <div class="field-group">
        <div class="field-row">
          <div>
            <span class="field-row-name">{$t('settings.auto_refresh')}</span>
            <div class="field-row-desc">{$t('settings.auto_refresh_desc')}</div>
          </div>
          <label class="toggle">
            <input
              type="checkbox"
              bind:checked={autoRefresh}
              on:change={() => saveSetting('autoRefresh', String(autoRefresh))}
            />
            <span class="toggle-track"><span class="toggle-thumb"></span></span>
          </label>
        </div>
        <div class="field-row">
          <div>
            <span class="field-row-name">{$t('settings.confirm_dangerous')}</span>
            <div class="field-row-desc">{$t('settings.confirm_dangerous_desc')}</div>
          </div>
          <label class="toggle">
            <input
              type="checkbox"
              bind:checked={confirmDangerous}
              on:change={() => saveSetting('confirmDangerous', String(confirmDangerous))}
            />
            <span class="toggle-track"><span class="toggle-thumb"></span></span>
          </label>
        </div>
        <div class="field-row">
          <div>
            <span class="field-row-name">{$t('settings.notification_sound')}</span>
            <div class="field-row-desc">{$t('settings.notification_sound_desc')}</div>
          </div>
          <label class="toggle">
            <input
              type="checkbox"
              bind:checked={notificationSound}
              on:change={() => saveSetting('notificationSound', String(notificationSound))}
            />
            <span class="toggle-track"><span class="toggle-thumb"></span></span>
          </label>
        </div>
        <div class="field-row">
          <div>
            <span class="field-row-name">{$t('settings.dev_mode')}</span>
            <div class="field-row-desc">{$t('settings.dev_mode_desc')}</div>
          </div>
          <label class="toggle">
            <input
              type="checkbox"
              checked={$devMode}
              on:change={(e) => setDevMode((e.target as HTMLInputElement).checked)}
            />
            <span class="toggle-track"><span class="toggle-thumb"></span></span>
          </label>
        </div>
      </div>
    </div>
  {/if}

  <!-- Updates tab -->
  {#if activeTab === 'updates'}
    <div class="card mb-2">
      <div class="card-label">{$t('settings.update')}</div>
      <div class="field-group">
        <div class="field-row">
          <span class="field-row-name">{$t('settings.current_version')}</span>
          <span class="field-row-val mono">{version}</span>
        </div>
        {#if updateInfo?.has_update}
          <div class="field-row">
            <span class="field-row-name">{$t('settings.available_version')}</span>
            <span class="field-row-val" style="color: var(--accent)"
              >{updateInfo.latest_version}</span
            >
          </div>
        {/if}
        <div class="field-row">
          <span class="field-row-name">{$t('settings.update_channel')}</span>
          <div class="field-row-val">
            <div class="channel-switcher">
              {#each ['stable', 'beta'] as const as ch}
                <button
                  class="channel-btn"
                  class:active={updateChannel === ch}
                  on:click={() => saveUpdateChannel(ch)}
                  disabled={updateInstalling}>{$t(`settings.channel_${ch}`)}</button
                >
              {/each}
            </div>
          </div>
        </div>
      </div>

      {#if updateInfo?.changelog}
        <div class="changelog-box">
          <pre>{updateInfo.changelog}</pre>
        </div>
      {/if}

      {#if updateStatus && updateStatus.status !== 'idle'}
        <div class="update-progress">
          <div class="progress-bar">
            <div
              class="progress-fill"
              class:progress-pulse={reconnecting}
              style="width: {reconnecting ? 100 : updateStatus.progress}%"
            ></div>
          </div>
          <span class="progress-text">{updateStatus.message}</span>
        </div>
      {/if}

      {#if reconnecting}
        <div class="reconnect-overlay">
          <div class="reconnect-spinner"></div>
          <div class="reconnect-text">
            <span>{$t('settings.update_reconnecting')}</span>
            <span class="reconnect-dots"></span>
          </div>
          <div class="reconnect-sub">{$t('settings.update_reconnecting_sub')}</div>
        </div>
      {/if}

      <div class="card-actions">
        <button
          class="btn btn-secondary"
          on:click={() => checkUpdate()}
          disabled={updateChecking || updateInstalling}
          title={$t('settings.check_update')}
        >
          {updateChecking ? $t('settings.checking') : $t('settings.check_update')}
        </button>
        {#if updateInfo?.has_update}
          <button
            class="btn btn-primary"
            on:click={() => (showConfirmUpdateModal = true)}
            disabled={updateInstalling}
            title={$t('settings.install_update')}
          >
            {updateInstalling ? $t('settings.installing') : $t('settings.install_update')}
          </button>
        {/if}
        {#if updateStatus?.status === 'failed'}
          <button class="btn btn-danger" on:click={rollbackUpdate} title={$t('settings.rollback')}>
            {$t('settings.rollback')}
          </button>
        {/if}
      </div>
    </div>
  {/if}

  {#if showConfirmUpdateModal}
    <div
      class="modal-overlay"
      role="button"
      tabindex="0"
      on:click={() => (showConfirmUpdateModal = false)}
      on:keydown={(e) => e.key === 'Escape' && (showConfirmUpdateModal = false)}
    >
      <div class="modal-card" role="presentation" on:click|stopPropagation>
        <div class="modal-card-header">
          <h2>{$t('settings.update_confirm_title')}</h2>
          <button class="modal-close-btn" on:click={() => (showConfirmUpdateModal = false)}
            >&times;</button
          >
        </div>
        <div class="modal-card-body">
          <p>{$t('settings.update_confirm_text')}</p>
          {#if updateInfo?.changelog}
            <div class="changelog-box" style="max-height: 300px; margin-top: 10px;">
              <pre>{updateInfo.changelog}</pre>
            </div>
          {/if}
        </div>
        <div class="modal-card-footer">
          <button class="btn btn-secondary" on:click={() => (showConfirmUpdateModal = false)}
            >{$t('app.cancel')}</button
          >
          <button
            class="btn btn-primary"
            on:click={() => {
              showConfirmUpdateModal = false;
              installUpdate();
            }}>{$t('settings.update_install_btn')}</button
          >
        </div>
      </div>
    </div>
  {/if}

  <!-- Backups tab -->
  {#if activeTab === 'backups'}
    <div class="card settings-card" style="margin-bottom:18px;padding:0;">
      <div class="field-group">
        <div class="field-group-head">{$t('settings.section_file_backups')}</div>
        <div class="field-row">
          <div>
            <div class="lbl">{$t('settings.backup_file')}</div>
            <div class="desc">{$t('settings.backups_desc')}</div>
          </div>
          <div class="ctrl">
            <select
              class="input"
              style="min-width: 250px;"
              bind:value={selectedFile}
              on:change={fetchBackups}
              title={$t('settings.backup_file')}
            >
              {#each configFiles as file}
                <option value={file}>{file}</option>
              {:else}
                <option value="">{$t('settings.no_files')}</option>
              {/each}
            </select>
            <button class="btn btn-primary btn-sm" on:click={createBackup} disabled={!selectedFile}>
              {$t('settings.backup_create_btn')}
            </button>
          </div>
        </div>
      </div>

      <!-- Backups table -->
      <div class="field-group" style="border:0;">
        {#if !selectedFile}
          <div style="padding:20px;text-align:center;color:var(--fg-dim);font-style:italic;">
            {$t('settings.backup_select_file_hint')}
          </div>
        {:else if loadingBackups}
          <div style="padding:20px;text-align:center;color:var(--fg-dim);">{$t('app.loading')}</div>
        {:else if backups.length === 0}
          <div style="padding:20px;text-align:center;color:var(--fg-dim);font-style:italic;">
            {$t('settings.backups_empty')}
          </div>
        {:else}
          {#each backups as backup}
            <div class="field-row">
              <div>
                <div class="lbl mono">{backup.split('/').pop()}</div>
                <div class="desc mono" style="font-size: 11px; color: var(--fg-dim);">{backup}</div>
              </div>
              <div class="ctrl">
                <button
                  class="btn btn-secondary btn-sm"
                  on:click={() => restoreBackup(backup)}
                  title={$t('settings.backup_restore_btn')}
                >
                  {$t('settings.backup_restore_btn')}
                </button>
                <button
                  class="btn btn-danger btn-sm"
                  on:click={() => deleteBackup(backup)}
                  title={$t('app.delete')}
                >
                  {$t('app.delete')}
                </button>
              </div>
            </div>
          {/each}
        {/if}
      </div>
    </div>

    <!-- Divider -->
    <div style="border-top: 1px solid var(--border); margin: 24px 0;"></div>

    <!-- Section 2: Snapshots -->
    <div class="card" style="margin-top:0;">
      <div
        class="card-title-row"
        style="display:flex;align-items:center;justify-content:space-between;margin-bottom:14px;"
      >
        <h2 class="card-title" style="margin:0;">{$t('settings.section_snapshots')}</h2>
      </div>
      <div
        class="field-row"
        style="border-bottom:1px solid var(--border-light);padding-bottom:12px;margin-bottom:12px;"
      >
        <input
          class="input"
          style="flex:1;margin-right:8px;"
          type="text"
          placeholder={$t('settings.snapshot_label_placeholder')}
          bind:value={snapshotLabel}
        />
        <button class="btn btn-primary" on:click={createSnapshot} disabled={creatingSnapshot}>
          {creatingSnapshot ? $t('app.loading') : $t('settings.snapshot_create_btn')}
        </button>
      </div>
      {#if snapshots.length === 0}
        <div style="color:var(--fg-secondary);font-size:13px;text-align:center;padding:12px 0;">
          {$t('settings.snapshots_empty')}
        </div>
      {:else}
        {#each snapshots as snap}
          <div class="field-row snapshot-row">
            <div>
              <div class="lbl mono">{snap.label || snap.id}</div>
              <div class="desc" style="font-size:11px;">
                {new Date(snap.created_at * 1000).toLocaleString()} · {formatSnapshotSize(
                  snap.size_bytes
                )}
              </div>
            </div>
            <div class="ctrl" style="gap:6px;">
              <a class="btn btn-secondary" href="/api/snapshots/{snap.id}/download" download>
                {$t('settings.snapshot_download_btn')}
              </a>
              <button
                class="btn btn-secondary"
                on:click={() => restoreSnapshot(snap.id)}
                disabled={restoringSnapshot === snap.id}
              >
                {restoringSnapshot === snap.id
                  ? $t('app.loading')
                  : $t('settings.snapshot_restore_btn')}
              </button>
              <button class="btn btn-danger" on:click={() => deleteSnapshot(snap.id)}>
                {$t('settings.snapshot_delete_btn')}
              </button>
            </div>
          </div>
        {/each}
      {/if}
    </div>
  {/if}

  <!-- Connection tab -->
  {#if activeTab === 'connection'}
    <div class="card mb-2">
      <div class="card-label">{$t('settings.mihomo_api')}</div>
      <div class="field-group">
        {#if $capabilities?.mihomo.api_url}
          <div class="field-row">
            <span class="field-row-name">{$t('settings.mihomo_api_url')}</span>
            <span class="field-row-val mono">{$capabilities.mihomo.api_url}</span>
          </div>
        {/if}
        <div class="field-row">
          <span class="field-row-name">{$t('settings.mihomo_status')}</span>
          <span class="field-row-val">
            {#if $capabilities?.mihomo.process_running}
              <span class="status-ok">● {$t('settings.mihomo_running')}</span>
            {:else}
              <span class="status-err">○ {$t('settings.mihomo_stopped')}</span>
            {/if}
          </span>
        </div>
        <div class="field-row">
          <span class="field-row-name">{$t('settings.mihomo_api_reachable')}</span>
          <span class="field-row-val">
            {#if $capabilities?.mihomo.api_reachable}
              <span class="status-ok">{$t('settings.mihomo_yes')}</span>
            {:else}
              <span class="status-err">{$t('settings.mihomo_no')}</span>
            {/if}
          </span>
        </div>
        <div class="field-row">
          <span class="field-row-name">{$t('settings.mihomo_api_auth')}</span>
          <span class="field-row-val">
            {#if $capabilities?.mihomo.api_authenticated}
              <span class="status-ok">{$t('settings.mihomo_yes')}</span>
            {:else if $capabilities?.mihomo.api_reachable}
              <span class="status-err">{$t('settings.mihomo_auth_error')}</span>
            {:else}
              <span style="color: var(--fg-secondary)">—</span>
            {/if}
          </span>
        </div>
        {#if $capabilities?.mihomo.discovered_secret}
          <div class="field-row">
            <span class="field-row-name">{$t('settings.mihomo_secret_discovered')}</span>
            <span class="field-row-val mono" style="display:flex;align-items:center;gap:6px;">
              {secretVisible ? $capabilities.mihomo.discovered_secret : '••••••••'}
              <button class="btn btn-secondary btn-sm" on:click={() => (secretVisible = !secretVisible)}>
                {secretVisible ? $t('app.hide') : $t('app.show')}
              </button>
            </span>
          </div>
        {/if}
      </div>
      <div class="card-actions">
        <button
          class="btn btn-secondary"
          on:click={recheckConnection}
          disabled={checkingConnection}
          title={$t('settings.recheck_title')}
        >
          {checkingConnection ? $t('settings.checking') : $t('settings.recheck_btn')}
        </button>
      </div>
    </div>
  {/if}

  <!-- Security tab -->
  {#if activeTab === 'security'}
    <div class="card mb-2">
      <div class="card-label">{$t('settings.change_password')}</div>
      <div class="field-group">
        <div class="field-row">
          <label class="field-row-name" for="curr-pwd">{$t('settings.current_password')}</label>
          <input
            id="curr-pwd"
            type="password"
            class="field-input"
            bind:value={currentPassword}
            placeholder="••••••••"
          />
        </div>
        <div class="field-row">
          <label class="field-row-name" for="new-pwd">{$t('settings.new_password')}</label>
          <input
            id="new-pwd"
            type="password"
            class="field-input"
            bind:value={newPassword}
            placeholder="••••••••"
          />
        </div>
        <div class="field-row">
          <label class="field-row-name" for="conf-pwd">{$t('settings.confirm_password')}</label>
          <input
            id="conf-pwd"
            type="password"
            class="field-input"
            bind:value={confirmPassword}
            placeholder="••••••••"
          />
        </div>
      </div>
      {#if passwordError}
        <div class="field-error">{passwordError}</div>
      {/if}
      {#if passwordSuccess}
        <div class="field-success">{$t('settings.password_changed')}</div>
      {/if}
      <div class="card-actions">
        <button
          class="btn btn-primary"
          on:click={changePassword}
          disabled={passwordChanging || !currentPassword || !newPassword || !confirmPassword}
          title={$t('settings.save_password')}
        >
          {passwordChanging ? $t('app.loading') : $t('settings.save_password')}
        </button>
      </div>
    </div>

    <div class="card mb-2">
      <div class="card-label">{$t('settings.security')}</div>
      <div class="field-group">
        <div class="field-row-info">
          <Icon name="check" size={14} /><span>{$t('settings.auth_bcrypt')}</span>
        </div>
        <div class="field-row-info">
          <Icon name="check" size={14} /><span>{$t('settings.csrf')}</span>
        </div>
        <div class="field-row-info">
          <Icon name="check" size={14} /><span>{$t('settings.rate_limit')}</span>
        </div>
        <div class="field-row-info">
          <Icon name="check" size={14} /><span>{$t('settings.security_headers')}</span>
        </div>
      </div>
    </div>
  {/if}

  <!-- About tab -->
  {#if activeTab === 'about'}
    <div class="card mb-2">
      <div class="card-label">{$t('settings.about')}</div>
      <div class="field-group">
        <div class="field-row">
          <span class="field-row-name">{$t('settings.version')}</span>
          <span class="field-row-val mono">{version}</span>
        </div>
        <div class="field-row">
          <span class="field-row-name">{$t('settings.frontend')}</span>
          <span class="field-row-val mono">Svelte 5 + TypeScript + Vite</span>
        </div>
        <div class="field-row">
          <span class="field-row-name">{$t('settings.backend')}</span>
          <span class="field-row-val mono">Go + net/http</span>
        </div>
      </div>
    </div>
  {/if}
</div>

<style>
  .page-head {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    margin-bottom: 20px;
    gap: 16px;
  }

  .page-head h1 {
    margin: 4px 0 6px;
    font-size: 22px;
    font-weight: 700;
  }

  .page-head .sub {
    margin: 0;
    color: var(--fg-secondary);
    font-size: 13px;
  }

  .crumbs {
    font-size: 12px;
    color: var(--fg-dim);
    margin-bottom: 2px;
  }

  .crumb-sep {
    color: var(--fg-faint);
    margin: 0 6px;
  }

  /* tab nav */
  .settings-tabs {
    display: flex;
    gap: 2px;
    margin-bottom: 20px;
    border-bottom: 1px solid var(--border);
    padding-bottom: 0;
  }

  .stab {
    padding: 8px 16px;
    background: transparent;
    border: none;
    border-bottom: 2px solid transparent;
    margin-bottom: -1px;
    font-size: 13px;
    font-weight: 500;
    color: var(--fg-secondary);
    cursor: pointer;
    border-radius: 4px 4px 0 0;
    transition:
      color 0.15s,
      border-color 0.15s;
  }

  .stab:hover {
    color: var(--fg-primary);
  }

  .stab.active {
    color: var(--accent);
    border-bottom-color: var(--accent);
  }

  /* card label */
  .card-label {
    font-size: 11px;
    font-weight: 700;
    letter-spacing: 0.08em;
    text-transform: uppercase;
    color: var(--fg-dim);
    margin-bottom: 14px;
  }

  /* field-group / field-row */
  .field-group {
    display: flex;
    flex-direction: column;
  }

  .field-row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 10px 0;
    border-bottom: 1px solid var(--border);
    gap: 16px;
  }

  .field-row:last-child {
    border-bottom: none;
    padding-bottom: 0;
  }

  .field-row:first-child {
    padding-top: 0;
  }

  .field-row-name {
    font-size: 14px;
    font-weight: 500;
    color: var(--fg-primary);
    flex-shrink: 0;
  }

  .field-row-val {
    font-size: 13px;
    color: var(--fg-secondary);
    text-align: right;
  }

  .field-row-val.mono {
    font-family: var(--font-mono, monospace);
    font-size: 12px;
  }

  .mono {
    font-family: var(--font-mono, monospace);
    font-size: 12px;
  }

  .field-select {
    font-size: 13px;
    padding: 5px 8px;
    border: 1px solid var(--border);
    border-radius: 6px;
    background: var(--bg-card);
    color: var(--fg-primary);
    cursor: pointer;
    min-width: 120px;
  }

  .field-input {
    font-size: 13px;
    padding: 6px 10px;
    border: 1px solid var(--border);
    border-radius: 6px;
    background: var(--bg-deep, var(--bg));
    color: var(--fg-primary);
    min-width: 180px;
  }

  .field-input:focus {
    outline: none;
    border-color: var(--accent);
  }

  .field-row-info {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 8px 0;
    font-size: 13px;
    color: var(--fg-secondary);
    border-bottom: 1px solid var(--border);
  }

  .field-row-info:last-child {
    border-bottom: none;
  }

  .card-actions {
    display: flex;
    gap: 8px;
    margin-top: 14px;
    flex-wrap: wrap;
  }

  .status-ok {
    color: #10b981;
  }

  .status-err {
    color: #ef4444;
  }

  .field-error {
    margin-top: 8px;
    font-size: 13px;
    color: #ef4444;
    padding: 6px 10px;
    background: rgba(239, 68, 68, 0.08);
    border-radius: 6px;
    border: 1px solid rgba(239, 68, 68, 0.25);
  }

  .field-success {
    margin-top: 8px;
    font-size: 13px;
    color: #10b981;
    padding: 6px 10px;
    background: rgba(16, 185, 129, 0.08);
    border-radius: 6px;
    border: 1px solid rgba(16, 185, 129, 0.25);
  }

  .channel-switcher {
    display: flex;
    gap: 4px;
  }

  .channel-btn {
    padding: 4px 12px;
    border-radius: var(--radius-sm, 4px);
    border: 1px solid var(--border);
    background: var(--bg-deep, var(--bg));
    color: var(--fg-secondary);
    font-size: 13px;
    cursor: pointer;
    transition:
      background 0.15s,
      color 0.15s,
      border-color 0.15s;
  }

  .channel-btn:hover {
    border-color: var(--accent);
    color: var(--fg);
  }

  .channel-btn.active {
    background: var(--accent);
    border-color: var(--accent);
    color: #fff;
  }

  .channel-btn:disabled {
    opacity: 0.5;
    cursor: not-allowed;
  }

  .changelog-box {
    margin: 12px 0;
    padding: 10px;
    background: var(--bg-deep, var(--bg));
    border: 1px solid var(--border);
    border-radius: 6px;
    max-height: 200px;
    overflow-y: auto;
  }

  .changelog-box pre {
    margin: 0;
    white-space: pre-wrap;
    word-wrap: break-word;
    font-size: 12px;
    color: var(--fg-secondary);
  }

  .update-progress {
    margin: 10px 0;
  }

  .progress-bar {
    height: 6px;
    background: var(--border);
    border-radius: 4px;
    overflow: hidden;
    margin-bottom: 6px;
  }

  .progress-fill {
    height: 100%;
    background: var(--accent);
    border-radius: 4px;
    transition: width 0.3s ease;
  }

  .progress-fill.progress-pulse {
    animation: progress-shimmer 1.5s ease-in-out infinite;
    background: linear-gradient(
      90deg,
      var(--accent) 0%,
      color-mix(in srgb, var(--accent) 60%, white) 50%,
      var(--accent) 100%
    );
    background-size: 200% 100%;
  }

  @keyframes progress-shimmer {
    0% { background-position: 200% 0; }
    100% { background-position: -200% 0; }
  }

  /* Reconnect overlay */
  .reconnect-overlay {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 10px;
    padding: 18px 16px;
    margin: 6px 0 10px;
    background: color-mix(in srgb, var(--accent) 8%, var(--bg-card));
    border: 1px solid color-mix(in srgb, var(--accent) 30%, transparent);
    border-radius: var(--radius-md, 8px);
  }

  .reconnect-spinner {
    width: 28px;
    height: 28px;
    border: 3px solid color-mix(in srgb, var(--accent) 25%, transparent);
    border-top-color: var(--accent);
    border-radius: 50%;
    animation: spin 0.8s linear infinite;
    flex-shrink: 0;
  }

  @keyframes spin {
    to { transform: rotate(360deg); }
  }

  .reconnect-text {
    display: flex;
    align-items: center;
    gap: 4px;
    font-size: 14px;
    font-weight: 600;
    color: var(--fg-primary);
  }

  .reconnect-dots::after {
    content: '';
    animation: dots 1.5s steps(4, end) infinite;
  }

  @keyframes dots {
    0%   { content: ''; }
    25%  { content: '.'; }
    50%  { content: '..'; }
    75%  { content: '...'; }
    100% { content: ''; }
  }

  .reconnect-sub {
    font-size: 12px;
    color: var(--fg-secondary);
    text-align: center;
  }

  .progress-text {
    font-size: 12px;
    color: var(--fg-secondary);
  }

  .mb-2 {
    margin-bottom: 12px;
  }

  .field-row-desc {
    font-size: 12px;
    color: var(--fg-dim);
    margin-top: 2px;
  }

  /* Segmented button */
  .seg-btn {
    display: flex;
    border: 1px solid var(--border);
    border-radius: 6px;
    overflow: hidden;
    flex-shrink: 0;
  }

  .seg-opt {
    padding: 5px 12px;
    font-size: 13px;
    background: transparent;
    border: none;
    border-right: 1px solid var(--border);
    color: var(--fg-secondary);
    cursor: pointer;
    transition:
      background 0.15s,
      color 0.15s;
  }

  .seg-opt:last-child {
    border-right: none;
  }

  .seg-opt:hover {
    background: var(--bg-hover, rgba(0, 0, 0, 0.04));
  }

  .seg-opt.seg-active {
    background: var(--accent);
    color: #fff;
  }

  /* Toggle switch */
  .toggle {
    position: relative;
    display: inline-flex;
    align-items: center;
    cursor: pointer;
    flex-shrink: 0;
  }

  .toggle input {
    position: absolute;
    opacity: 0;
    width: 0;
    height: 0;
  }

  .toggle-track {
    width: 36px;
    height: 20px;
    background: var(--border);
    border-radius: 10px;
    transition: background 0.2s;
    position: relative;
    display: block;
  }

  .toggle input:checked ~ .toggle-track {
    background: var(--accent);
  }

  .toggle-thumb {
    position: absolute;
    top: 2px;
    left: 2px;
    width: 16px;
    height: 16px;
    background: #fff;
    border-radius: 50%;
    transition: transform 0.2s;
    box-shadow: 0 1px 3px rgba(0, 0, 0, 0.2);
  }

  .toggle input:checked ~ .toggle-track .toggle-thumb {
    transform: translateX(16px);
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

  .btn-sm {
    padding: 6px 12px;
    font-size: 12px;
  }
</style>
