<script lang="ts">
  import { onMount } from 'svelte'
  import { t } from './i18n'
  import PageHeader from './PageHeader.svelte'

  export let onSwitchTab: (tab: string) => void = () => {}

  interface Profile {
    id: string
    name: string
    enabled: boolean
    days_of_week: number[]
    start_time: string
    end_time: string
    group_name: string
    proxy_name: string
    last_applied: number
    apply_count: number
  }

  interface Status {
    active: Profile[]
    next: Profile[]
    time: string
    day: number
  }

  let profiles: Profile[] = []
  let status: Status | null = null
  let loading = false
  let error = ''

  // Form state
  let editingProfile: Profile | null = null
  let formName = ''
  let formEnabled = true
  let formDays: number[] = [1, 2, 3, 4, 5]
  let formStartTime = '09:00'
  let formEndTime = '18:00'
  let formGroupName = ''
  let formProxyName = ''

  const dayNames = ['Вс', 'Пн', 'Вт', 'Ср', 'Чт', 'Пт', 'Сб']
  const allDays = [0, 1, 2, 3, 4, 5, 6]

  async function fetchProfiles() {
    loading = true
    try {
      const res = await fetch('/api/smart-proxy/profiles')
      if (res.ok) profiles = await res.json()
    } catch (e: any) {
      error = e.message
    } finally {
      loading = false
    }
  }

  async function fetchStatus() {
    try {
      const res = await fetch('/api/smart-proxy/status')
      if (res.ok) status = await res.json()
    } catch (e) {
      // ignore
    }
  }

  function startCreate() {
    editingProfile = null
    formName = ''
    formEnabled = true
    formDays = [1, 2, 3, 4, 5]
    formStartTime = '09:00'
    formEndTime = '18:00'
    formGroupName = ''
    formProxyName = ''
  }

  function startEdit(p: Profile) {
    editingProfile = p
    formName = p.name
    formEnabled = p.enabled
    formDays = [...p.days_of_week]
    formStartTime = p.start_time
    formEndTime = p.end_time
    formGroupName = p.group_name
    formProxyName = p.proxy_name
  }

  function cancelEdit() {
    editingProfile = null
  }

  function toggleDay(day: number) {
    if (formDays.includes(day)) {
      formDays = formDays.filter(d => d !== day)
    } else {
      formDays = [...formDays, day].sort()
    }
  }

  async function saveProfile() {
    const csrfToken = localStorage.getItem('csrf_token')
    const payload = {
      name: formName,
      enabled: formEnabled,
      days_of_week: formDays,
      start_time: formStartTime,
      end_time: formEndTime,
      group_name: formGroupName,
      proxy_name: formProxyName
    }

    const url = editingProfile
      ? `/api/smart-proxy/profiles/update?id=${editingProfile.id}`
      : '/api/smart-proxy/profiles/add'

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

      editingProfile = null
      await fetchProfiles()
      await fetchStatus()
    } catch (e: any) {
      error = e.message
    }
  }

  async function deleteProfile(id: string) {
    if (!confirm($t('app.delete') + '?')) return
    const csrfToken = localStorage.getItem('csrf_token')
    try {
      const res = await fetch(`/api/smart-proxy/profiles/delete?id=${id}`, {
        method: 'POST',
        headers: { 'X-CSRF-Token': csrfToken || '' }
      })
      if (!res.ok) throw new Error('Failed to delete')
      await fetchProfiles()
      await fetchStatus()
    } catch (e: any) {
      error = e.message
    }
  }

  async function toggleEnabled(p: Profile) {
    const csrfToken = localStorage.getItem('csrf_token')
    try {
      const res = await fetch(`/api/smart-proxy/profiles/enabled?id=${p.id}&enabled=${!p.enabled}`, {
        method: 'POST',
        headers: { 'X-CSRF-Token': csrfToken || '' }
      })
      if (!res.ok) throw new Error('Failed to toggle')
      await fetchProfiles()
      await fetchStatus()
    } catch (e: any) {
      error = e.message
    }
  }

  onMount(() => {
    fetchProfiles()
    fetchStatus()
    const interval = setInterval(fetchStatus, 30000)
    return () => clearInterval(interval)
  })
</script>

<div class="container">
  <PageHeader
    title={$t('smartproxy.title')}
    subtitle={$t('smartproxy.subtitle')}
    breadcrumbs={[{ label: $t('smartproxy.title') }]}
    {onSwitchTab}
  />

  {#if error}
    <div class="alert alert-error mb-2">{error}</div>
  {/if}

  <!-- Current Status -->
  {#if status}
    <div class="card mb-2">
      <h2>{$t('smartproxy.current_status')}</h2>
      <p class="text-secondary">{$t('smartproxy.time')}: {status.time}, {$t('smartproxy.day')}: {dayNames[status.day]}</p>
      {#if status.active.length > 0}
        <div class="active-profiles">
          {#each status.active as p}
            <span class="status-badge active">{p.name} → {p.proxy_name}</span>
          {/each}
        </div>
      {:else}
        <p class="text-secondary">{$t('smartproxy.no_active')}</p>
      {/if}
    </div>
  {/if}

  <!-- Profile List -->
  <div class="card mb-2">
    <div class="flex-between mb-2">
      <h2>{$t('smartproxy.profiles')}</h2>
      <button class="btn btn-primary" on:click={startCreate}>+ {$t('smartproxy.add')}</button>
    </div>

    {#if profiles.length === 0}
      <p class="text-secondary">{$t('smartproxy.no_profiles')}</p>
    {:else}
      <div class="profile-list">
        {#each profiles as p}
          <div class="profile-item" class:active={p.enabled && status?.active.some(a => a.id === p.id)}>
            <div class="profile-main">
              <div class="profile-header">
                <span class="profile-name">{p.name}</span>
                <label class="toggle-switch">
                  <input type="checkbox" checked={p.enabled} on:change={() => toggleEnabled(p)} />
                  <span class="toggle-slider"></span>
                </label>
              </div>
              <div class="profile-details">
                <span class="detail">
                  {p.days_of_week.map(d => dayNames[d]).join(', ')}
                </span>
                <span class="detail">{p.start_time} – {p.end_time}</span>
                <span class="detail">{p.group_name} → {p.proxy_name}</span>
                {#if p.apply_count > 0}
                  <span class="detail">{$t('smartproxy.applied_count', { count: p.apply_count })}</span>
                {/if}
              </div>
            </div>
            <div class="profile-actions">
              <button class="btn-icon" on:click={() => startEdit(p)} title={$t('app.edit')}>✏️</button>
              <button class="btn-icon" on:click={() => deleteProfile(p.id)} title={$t('app.delete')}>🗑️</button>
            </div>
          </div>
        {/each}
      </div>
    {/if}
  </div>

  <!-- Edit/Create Form -->
  {#if editingProfile !== null || formName !== '' || editingProfile === null && formName === '' && profiles.length === 0}
    {#if editingProfile !== null || (editingProfile === null && formName === '')}
      <div class="card">
        <h2>{editingProfile ? $t('smartproxy.edit_profile') : $t('smartproxy.new_profile')}</h2>

        <div class="form-group">
          <label for="sp-name">{$t('smartproxy.name')}</label>
          <input id="sp-name" type="text" class="input" bind:value={formName} placeholder={$t('smartproxy.name_placeholder')} />
        </div>

        <div class="form-group">
          <label for="sp-days">{$t('smartproxy.days_of_week')}</label>
          <div class="day-selector" id="sp-days">
            {#each allDays as day}
              <button
                class="day-btn"
                class:selected={formDays.includes(day)}
                on:click={() => toggleDay(day)}
              >
                {dayNames[day]}
              </button>
            {/each}
          </div>
        </div>

        <div class="form-row">
          <div class="form-group">
            <label for="sp-start">{$t('smartproxy.from')}</label>
            <input id="sp-start" type="time" class="input" bind:value={formStartTime} />
          </div>
          <div class="form-group">
            <label for="sp-end">{$t('smartproxy.to')}</label>
            <input id="sp-end" type="time" class="input" bind:value={formEndTime} />
          </div>
        </div>

        <div class="form-group">
          <label for="sp-group">{$t('smartproxy.proxy_group')}</label>
          <input id="sp-group" type="text" class="input" bind:value={formGroupName} placeholder={$t('smartproxy.proxy_group_placeholder')} />
        </div>

        <div class="form-group">
          <label for="sp-proxy">{$t('smartproxy.proxy')}</label>
          <input id="sp-proxy" type="text" class="input" bind:value={formProxyName} placeholder={$t('smartproxy.proxy_placeholder')} />
        </div>

        <div class="form-actions">
          <button class="btn btn-secondary" on:click={cancelEdit}>{$t('app.cancel')}</button>
          <button class="btn btn-primary" on:click={saveProfile}>{$t('app.save')}</button>
        </div>
      </div>
    {/if}
  {/if}
</div>

<style>
  .flex-between {
    display: flex;
    justify-content: space-between;
    align-items: center;
  }

  .active-profiles {
    display: flex;
    gap: 0.5rem;
    flex-wrap: wrap;
    margin-top: 0.5rem;
  }

  .status-badge {
    padding: 0.25rem 0.75rem;
    border-radius: 4px;
    font-size: 0.85rem;
    background: var(--bg);
    border: 1px solid var(--border);
  }

  .status-badge.active {
    background: var(--success-bg, #d4edda);
    color: var(--success-text, #155724);
    border-color: var(--success, #28a745);
  }

  .profile-list {
    display: flex;
    flex-direction: column;
    gap: 0.75rem;
  }

  .profile-item {
    display: flex;
    justify-content: space-between;
    align-items: flex-start;
    padding: 0.75rem;
    border: 1px solid var(--border);
    border-radius: var(--radius);
    background: var(--bg);
  }

  .profile-item.active {
    border-color: var(--success, #28a745);
    background: var(--success-bg, rgba(40, 167, 69, 0.05));
  }

  .profile-main {
    flex: 1;
  }

  .profile-header {
    display: flex;
    justify-content: space-between;
    align-items: center;
    margin-bottom: 0.25rem;
  }

  .profile-name {
    font-weight: 600;
  }

  .profile-details {
    display: flex;
    gap: 0.5rem;
    flex-wrap: wrap;
    font-size: 0.8rem;
    color: var(--text-secondary);
  }

  .profile-actions {
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

  .day-selector {
    display: flex;
    gap: 0.25rem;
  }

  .day-btn {
    padding: 0.5rem;
    min-width: 36px;
    background: var(--bg);
    border: 1px solid var(--border);
    border-radius: 4px;
    cursor: pointer;
    font-size: 0.85rem;
  }

  .day-btn.selected {
    background: var(--primary);
    color: white;
    border-color: var(--primary);
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
</style>
