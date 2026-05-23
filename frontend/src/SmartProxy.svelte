<script lang="ts">
  import { onMount } from 'svelte';
  import { t, currentLang } from './i18n';
  import { showConfirm } from './stores';

  export let onSwitchTab: (tab: string) => void = () => {};

  interface Profile {
    id: string;
    name: string;
    enabled: boolean;
    mode: string;
    days_of_week: number[];
    start_time: string;
    end_time: string;
    group_name: string;
    proxy_name: string;
    latency_threshold?: number;
    consecutive_failures?: number;
    fallback_proxy?: string;
    round_robin_proxies?: string[];
    current_proxy?: string;
    current_failures?: number;
    last_applied: number;
    apply_count: number;
  }

  interface Status {
    active: Profile[];
    next: Profile[];
    time: string;
    day: number;
  }

  let profiles: Profile[] = [];
  let status: Status | null = null;
  let loading = false;
  let error = '';
  let activeDropdownId: string | null = null;

  // Form state
  let showForm = false;
  let editingProfile: Profile | null = null;
  let formName = '';
  let formEnabled = true;
  let formMode = 'time-based';
  let formDays: number[] = [1, 2, 3, 4, 5];
  let formStartTime = '09:00';
  let formEndTime = '18:00';
  let formGroupName = '';
  let formProxyName = '';
  let formLatencyThreshold = 500;
  let formConsecutiveFailures = 3;
  let formFallbackProxy = '';
  let formRoundRobinProxies = '';

  $: dayNames = $t('smartproxy.days').split(',');
  const allDays = [0, 1, 2, 3, 4, 5, 6];

  $: modes = [
    { value: 'time-based', label: $t('smartproxy.mode_time') },
    { value: 'auto-failover', label: $t('smartproxy.mode_failover') },
    { value: 'round-robin', label: $t('smartproxy.mode_roundrobin') }
  ];

  async function fetchProfiles() {
    loading = true;
    try {
      const res = await fetch('/api/smart-proxy/profiles');
      if (res.ok) profiles = await res.json();
    } catch (e: any) {
      error = e.message;
    } finally {
      loading = false;
    }
  }

  async function fetchStatus() {
    try {
      const res = await fetch('/api/smart-proxy/status');
      if (res.ok) status = await res.json();
    } catch (e) {
      // ignore
    }
  }

  function startCreate() {
    showForm = true;
    editingProfile = null;
    formName = '';
    formEnabled = true;
    formMode = 'time-based';
    formDays = [1, 2, 3, 4, 5];
    formStartTime = '09:00';
    formEndTime = '18:00';
    formGroupName = '';
    formProxyName = '';
    formLatencyThreshold = 500;
    formConsecutiveFailures = 3;
    formFallbackProxy = '';
    formRoundRobinProxies = '';
  }

  function startEdit(p: Profile) {
    editingProfile = p;
    formName = p.name;
    formEnabled = p.enabled;
    formMode = p.mode || 'time-based';
    formDays = [...(p.days_of_week || [])];
    formStartTime = p.start_time || '09:00';
    formEndTime = p.end_time || '18:00';
    formGroupName = p.group_name;
    formProxyName = p.proxy_name;
    formLatencyThreshold = p.latency_threshold || 500;
    formConsecutiveFailures = p.consecutive_failures || 3;
    formFallbackProxy = p.fallback_proxy || '';
    formRoundRobinProxies = p.round_robin_proxies ? p.round_robin_proxies.join('\n') : '';
    showForm = true;
  }

  function cancelEdit() {
    showForm = false;
    editingProfile = null;
  }

  function toggleDay(day: number) {
    if (formDays.includes(day)) {
      formDays = formDays.filter((d) => d !== day);
    } else {
      formDays = [...formDays, day].sort();
    }
  }

  async function saveProfile() {
    const csrfToken = localStorage.getItem('csrf_token');
    const payload: any = {
      name: formName,
      enabled: formEnabled,
      mode: formMode,
      group_name: formGroupName,
      proxy_name: formProxyName
    };

    if (formMode === 'time-based') {
      payload.days_of_week = formDays;
      payload.start_time = formStartTime;
      payload.end_time = formEndTime;
    } else if (formMode === 'auto-failover') {
      payload.latency_threshold = formLatencyThreshold;
      payload.consecutive_failures = formConsecutiveFailures;
      payload.fallback_proxy = formFallbackProxy;
    } else if (formMode === 'round-robin') {
      payload.round_robin_proxies = formRoundRobinProxies.split('\n').filter((s) => s.trim());
    }

    const url = editingProfile
      ? `/api/smart-proxy/profiles/update?id=${editingProfile.id}`
      : '/api/smart-proxy/profiles/add';

    try {
      const res = await fetch(url, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          'X-CSRF-Token': csrfToken || ''
        },
        body: JSON.stringify(payload)
      });

      if (!res.ok) throw new Error('Failed to save');

      showForm = false;
      editingProfile = null;
      await fetchProfiles();
      await fetchStatus();
    } catch (e: any) {
      error = e.message;
    }
  }

  async function deleteProfile(id: string) {
    if (!(await showConfirm($t('app.confirm'), $t('app.delete') + '?'))) return;
    const csrfToken = localStorage.getItem('csrf_token');
    try {
      const res = await fetch(`/api/smart-proxy/profiles/delete?id=${id}`, {
        method: 'POST',
        headers: { 'X-CSRF-Token': csrfToken || '' }
      });
      if (!res.ok) throw new Error('Failed to delete');
      await fetchProfiles();
      await fetchStatus();
    } catch (e: any) {
      error = e.message;
    }
  }

  async function toggleEnabled(p: Profile) {
    const csrfToken = localStorage.getItem('csrf_token');
    try {
      const res = await fetch(
        `/api/smart-proxy/profiles/enabled?id=${p.id}&enabled=${!p.enabled}`,
        {
          method: 'POST',
          headers: { 'X-CSRF-Token': csrfToken || '' }
        }
      );
      if (!res.ok) throw new Error('Failed to toggle');
      await fetchProfiles();
      await fetchStatus();
    } catch (e: any) {
      error = e.message;
    }
  }

  function toggleDropdown(id: string) {
    activeDropdownId = activeDropdownId === id ? null : id;
  }

  function handleKeydown(e: KeyboardEvent) {
    if (e.key === 'Escape') {
      cancelEdit();
      activeDropdownId = null;
    }
  }

  function handleClickOutside(e: MouseEvent) {
    const target = e.target as HTMLElement;
    if (!target.closest('.dropdown-container')) {
      activeDropdownId = null;
    }
  }

  onMount(() => {
    fetchProfiles();
    fetchStatus();
    const interval = setInterval(fetchStatus, 30000);
    window.addEventListener('click', handleClickOutside);
    window.addEventListener('keydown', handleKeydown);
    return () => {
      clearInterval(interval);
      window.removeEventListener('click', handleClickOutside);
      window.removeEventListener('keydown', handleKeydown);
    };
  });
</script>

<div class="container">
  <div class="page-head">
    <div>
      <div class="crumbs">{$t('nav.group_proxy')} <span style="color:var(--fg-faint);margin:0 6px;">/</span> {$t('nav.smartproxy')}</div>
      <h1>{$t('smartproxy.title')}</h1>
      <p class="sub">{$t('smartproxy.subtitle')}</p>
    </div>
    <div class="ph-actions">
      <button class="btn btn-primary" on:click={startCreate}>
        <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" style="margin-right: 6px;"><path d="M12 5v14M5 12h14"/></svg>
        {$t('smartproxy.add')}
      </button>
    </div>
  </div>

  {#if error}
    <div class="alert alert-error mb-2">{error}</div>
  {/if}

  <!-- Current Status -->
  {#if status}
    <div class="card mb-2" style="padding:18px 22px;">
      <div style="display:flex;align-items:center;gap:14px;flex-wrap:wrap;">
        <span class="status-dot success" style="margin:0;"></span>
        <div style="font-weight:700;color:var(--fg-primary);">{$t('smartproxy.current_status')}</div>
        <div style="color:var(--fg-dim);font-size:12px;font-family:var(--font-family-mono);">
          ({status.time}, {dayNames[status.day]})
        </div>
        {#if status.active.length > 0}
          <div style="display:flex;gap:8px;margin-left:auto;flex-wrap:wrap;">
            {#each status.active as p}
              <span class="status-badge active">{p.name} → {p.current_proxy || p.proxy_name}</span>
            {/each}
          </div>
        {:else}
          <div style="margin-left:auto; color:var(--fg-dim); font-size: 13px;">
            {$t('smartproxy.no_active')}
          </div>
        {/if}
      </div>
    </div>
  {/if}

  <!-- Profile List -->
  {#if profiles.length === 0}
    <div class="card text-center" style="padding: 3rem; display: flex; flex-direction: column; align-items: center; justify-content: center; gap: 1rem;">
      <p style="color: var(--fg-secondary); margin: 0;">{$t('smartproxy.no_profiles')}</p>
      <button class="btn btn-primary" on:click={startCreate}>
        <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" style="margin-right: 6px;"><path d="M12 5v14M5 12h14"/></svg>
        {$t('smartproxy.add')}
      </button>
    </div>
  {:else}
    <div class="profile-grid">
      {#each profiles as p}
        {@const isActive = p.enabled && status?.active.some((a) => a.id === p.id)}
        <div class="card profile-card" class:active={isActive}>
          <div class="profile-card-header">
            <span class="profile-card-name">{p.name}</span>
            
            <span class="badge" class:badge-info={p.mode !== 'auto-failover'} class:badge-warning={p.mode === 'auto-failover'}>
              {p.mode === 'time-based'
                ? $t('smartproxy.mode_time')
                : p.mode === 'auto-failover'
                  ? $t('smartproxy.mode_failover')
                  : p.mode === 'round-robin'
                    ? $t('smartproxy.mode_roundrobin')
                    : p.mode || $t('smartproxy.mode_time')}
            </span>

            <div style="margin-left:auto; display:flex; align-items:center; gap:12px;">
              <label class="toggle-switch">
                <input type="checkbox" checked={p.enabled} on:change={() => toggleEnabled(p)} />
                <span class="toggle-slider"></span>
              </label>

              <div class="dropdown-container">
                <button class="btn btn-secondary action-btn-dots" on:click={() => toggleDropdown(p.id)}>⋯</button>
                {#if activeDropdownId === p.id}
                  <div class="dropdown-menu">
                    <button on:click={() => { startEdit(p); activeDropdownId = null; }}>{$t('app.edit')}</button>
                    <button on:click={() => { deleteProfile(p.id); activeDropdownId = null; }} class="delete-action">{$t('app.delete')}</button>
                  </div>
                {/if}
              </div>
            </div>
          </div>

          <div class="field-row">
            <div>
              <div class="lbl">{$currentLang === 'ru' ? 'Целевая группа' : 'Target Group'}</div>
            </div>
            <div class="ctrl">
              <span class="status-badge" class:active={p.enabled}>
                {p.group_name} → {p.current_proxy || p.proxy_name}
              </span>
            </div>
          </div>

          {#if p.mode === 'time-based'}
            <div class="field-row">
              <div>
                <div class="lbl">{$currentLang === 'ru' ? 'Расписание' : 'Schedule'}</div>
                <div class="desc">{p.days_of_week?.map((d) => dayNames[d]).join(', ')}</div>
              </div>
              <div class="ctrl mono" style="font-size:12px; color:var(--fg-primary);">
                {p.start_time} – {p.end_time}
              </div>
            </div>
          {:else if p.mode === 'auto-failover'}
            <div class="field-row">
              <div>
                <div class="lbl">{$t('smartproxy.latency_threshold')} / {$t('smartproxy.consecutive_failures')}</div>
                <div class="desc">
                  {$t('smartproxy.fallback_label')}: {p.fallback_proxy || 'DIRECT'}
                </div>
              </div>
              <div class="ctrl mono" style="font-size:12px; color:var(--fg-primary);">
                {p.latency_threshold}ms / {p.consecutive_failures} {$currentLang === 'ru' ? 'раз' : 'times'}
              </div>
            </div>

            {#if p.current_failures && p.current_failures > 0}
              <div class="field-row alert-row">
                <div>
                  <div class="lbl" style="color: var(--warning);">{$t('smartproxy.failures_label')}</div>
                </div>
                <div class="ctrl mono" style="color: var(--warning); font-weight: bold; font-size:12px;">
                  {p.current_failures} / {p.consecutive_failures}
                </div>
              </div>
            {/if}
          {:else if p.mode === 'round-robin'}
            <div class="field-row">
              <div>
                <div class="lbl">{$t('smartproxy.round_robin_proxies')}</div>
                <div class="desc">{(p.round_robin_proxies || []).length} {$t('smartproxy.proxies_label')}</div>
              </div>
              <div class="ctrl mono" style="font-size:11px; max-width: 200px; text-align: right; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; color:var(--fg-primary);">
                {(p.round_robin_proxies || []).join(', ')}
              </div>
            </div>
          {/if}

          {#if p.apply_count > 0}
            <div class="field-row">
              <div>
                <div class="lbl">{$currentLang === 'ru' ? 'Статистика срабатываний' : 'Execution stats'}</div>
              </div>
              <div class="ctrl mono" style="font-size:12px; color:var(--fg-secondary);">
                {$t('smartproxy.applied_count', { count: p.apply_count })}
              </div>
            </div>
          {/if}
        </div>
      {/each}
    </div>
  {/if}
</div>

{#if showForm}
  <div class="modal-overlay" role="button" tabindex="0" on:click={cancelEdit} on:keydown={handleKeydown}>
    <div class="modal-card" role="presentation" on:click|stopPropagation>
      <div class="modal-card-header">
        <h2>{editingProfile ? $t('smartproxy.edit_profile') : $t('smartproxy.new_profile')}</h2>
        <button class="modal-close-btn" on:click={cancelEdit}>&times;</button>
      </div>
      <div class="modal-card-body">
        <div class="form-group">
          <label for="sp-name" class="form-label">{$t('smartproxy.name')}</label>
          <input
            id="sp-name"
            type="text"
            class="input"
            bind:value={formName}
            placeholder={$t('smartproxy.name_placeholder')}
          />
        </div>

        <div class="form-group">
          <label for="sp-mode" class="form-label">{$t('smartproxy.mode')}</label>
          <select id="sp-mode" class="input" bind:value={formMode}>
            {#each modes as m}
              <option value={m.value}>{m.label}</option>
            {/each}
          </select>
        </div>

        {#if formMode === 'time-based'}
          <div class="form-group">
            <label for="sp-days" class="form-label">{$t('smartproxy.days_of_week')}</label>
            <div class="day-selector" id="sp-days">
              {#each allDays as day}
                <button
                  type="button"
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
              <label for="sp-start" class="form-label">{$t('smartproxy.from')}</label>
              <input id="sp-start" type="time" class="input" bind:value={formStartTime} />
            </div>
            <div class="form-group">
              <label for="sp-end" class="form-label">{$t('smartproxy.to')}</label>
              <input id="sp-end" type="time" class="input" bind:value={formEndTime} />
            </div>
          </div>
        {:else if formMode === 'auto-failover'}
          <div class="form-row">
            <div class="form-group">
              <label for="sp-threshold" class="form-label">{$t('smartproxy.latency_threshold')} (ms)</label>
              <input
                id="sp-threshold"
                type="number"
                class="input"
                bind:value={formLatencyThreshold}
                min="50"
                max="5000"
              />
            </div>
            <div class="form-group">
              <label for="sp-failures" class="form-label">{$t('smartproxy.consecutive_failures')}</label>
              <input
                id="sp-failures"
                type="number"
                class="input"
                bind:value={formConsecutiveFailures}
                min="1"
                max="10"
              />
            </div>
          </div>
          <div class="form-group">
            <label for="sp-fallback" class="form-label">{$t('smartproxy.fallback_proxy')}</label>
            <input
              id="sp-fallback"
              type="text"
              class="input"
              bind:value={formFallbackProxy}
              placeholder="DIRECT"
            />
          </div>
        {:else if formMode === 'round-robin'}
          <div class="form-group">
            <label for="sp-rr" class="form-label">{$t('smartproxy.round_robin_proxies')}</label>
            <textarea
              id="sp-rr"
              class="input"
              bind:value={formRoundRobinProxies}
              rows="4"
              placeholder="proxy1&#10;proxy2&#10;proxy3"
              style="resize: vertical; font-family: var(--font-family-mono);"
            ></textarea>
            <p class="hint">{$t('smartproxy.round_robin_hint')}</p>
          </div>
        {/if}

        <div class="form-group">
          <label for="sp-group" class="form-label">{$t('smartproxy.proxy_group')}</label>
          <input
            id="sp-group"
            type="text"
            class="input"
            bind:value={formGroupName}
            placeholder={$t('smartproxy.proxy_group_placeholder')}
          />
        </div>

        <div class="form-group">
          <label for="sp-proxy" class="form-label">{$t('smartproxy.proxy')}</label>
          <input
            id="sp-proxy"
            type="text"
            class="input"
            bind:value={formProxyName}
            placeholder={$t('smartproxy.proxy_placeholder')}
          />
        </div>

        <div class="form-group-checkbox">
          <label class="toggle-switch">
            <input type="checkbox" id="sp-enabled" bind:checked={formEnabled} />
            <span class="toggle-slider"></span>
          </label>
          <label for="sp-enabled" class="checkbox-label">{$currentLang === 'ru' ? 'Активен' : 'Enabled'}</label>
        </div>
      </div>
      <div class="modal-card-footer">
        <button class="btn btn-secondary" on:click={cancelEdit}>{$t('app.cancel')}</button>
        <button class="btn btn-primary" on:click={saveProfile}>{$t('app.save')}</button>
      </div>
    </div>
  </div>
{/if}

<style>
  .profile-grid {
    display: grid;
    grid-template-columns: repeat(2, 1fr);
    gap: 14px;
  }

  @media (max-width: 768px) {
    .profile-grid {
      grid-template-columns: 1fr;
    }
  }

  .profile-card {
    padding: 0;
  }

  .profile-card.active {
    border-color: var(--success);
    box-shadow: 0 4px 12px rgba(70, 209, 138, 0.05);
  }

  .profile-card-header {
    padding: 16px 20px;
    border-bottom: 1px solid var(--border);
    display: flex;
    align-items: center;
    gap: 10px;
  }

  .profile-card-name {
    font-weight: 700;
    color: var(--fg-primary);
    font-size: 14px;
  }

  .alert-row {
    background: rgba(240, 180, 80, 0.05);
  }

  .day-selector {
    display: flex;
    gap: 4px;
    flex-wrap: wrap;
    margin-top: 4px;
  }

  .day-btn {
    padding: 8px 12px;
    background: var(--bg-card);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    cursor: pointer;
    font-size: 12px;
    color: var(--fg-secondary);
    transition: all var(--transition-fast);
  }

  .day-btn:hover {
    background: var(--hover);
  }

  .day-btn.selected {
    background: var(--accent-soft);
    color: var(--accent);
    border-color: var(--accent);
  }

  .form-row {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: 12px;
  }

  .hint {
    font-size: 11px;
    color: var(--fg-dim);
    margin-top: 4px;
  }

  /* Dropdown Styles */
  .dropdown-container {
    position: relative;
    display: inline-block;
  }

  .action-btn-dots {
    padding: 6px 10px;
    font-size: 14px;
    line-height: 1;
  }

  .dropdown-menu {
    position: absolute;
    right: 0;
    top: 100%;
    margin-top: 6px;
    background: var(--bg-card);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    box-shadow: 0 10px 25px rgba(0, 0, 0, 0.3);
    z-index: 100;
    min-width: 140px;
    overflow: hidden;
    display: flex;
    flex-direction: column;
  }

  .dropdown-menu button {
    background: none;
    border: none;
    padding: 10px 14px;
    text-align: left;
    font-size: 13px;
    color: var(--fg-primary);
    cursor: pointer;
    width: 100%;
    transition: background var(--transition-fast);
  }

  .dropdown-menu button:hover {
    background: var(--hover);
  }

  .dropdown-menu button.delete-action {
    color: var(--danger);
  }

  .dropdown-menu button.delete-action:hover {
    background: rgba(235, 94, 85, 0.1);
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

  .form-group-checkbox {
    display: flex;
    align-items: center;
    gap: 12px;
    margin-top: 4px;
  }

  .checkbox-label {
    font-size: 13px;
    color: var(--fg-primary);
    cursor: pointer;
    user-select: none;
  }

  .modal-card-footer {
    padding: 16px 24px;
    border-top: 1px solid var(--border);
    display: flex;
    justify-content: flex-end;
    gap: 12px;
  }
</style>
