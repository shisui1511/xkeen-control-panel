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
    schedule?: boolean[][];
    days_of_week?: number[];
    start_time?: string;
    end_time?: string;
    group_name: string;
    proxy_name: string;
    last_applied: number;
    apply_count: number;
    current_proxy?: string;
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

  // Clash proxies
  let mihomoGroups: string[] = [];
  let mihomoProxies: string[] = [];

  // Form & Wizard state
  let showForm = false;
  let currentStep = 1;
  let editingProfile: Profile | null = null;

  let formName = '';
  let formEnabled = true;
  let formMode = 'time-based';
  let formGroupName = '';
  let formProxyName = '';
  let formSchedule: boolean[][] = Array.from({ length: 7 }, () => Array(24).fill(false));

  // Click-and-drag drawing state
  let isDrawing = false;
  let drawMode = true; // true to draw, false to erase

  $: dayNames = $t('smartproxy.days').split(',');
  const allDays = [0, 1, 2, 3, 4, 5, 6];

  async function fetchProfiles() {
    loading = true;
    error = '';
    try {
      const res = await fetch('/api/smart-proxy/profiles');
      if (!res.ok) throw new Error(`HTTP ${res.status}`);
      profiles = await res.json();
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

  async function fetchClashProxies() {
    try {
      const res = await fetch('/api/mihomo/proxy/proxies');
      if (res.ok) {
        const data = await res.json();
        const groups: string[] = [];
        const proxies: string[] = [];
        for (const [name, p] of Object.entries(data.proxies || {})) {
          const type = (p as any).type;
          if (type === 'Selector' || type === 'Fallback' || type === 'URLTest' || type === 'LoadBalance') {
            groups.push(name);
          } else if (type !== 'Direct' && type !== 'Reject') {
            proxies.push(name);
          }
        }
        mihomoGroups = groups.sort();
        mihomoProxies = proxies.sort();
      }
    } catch (e) {
      // ignore
    }
  }

  function startCreate() {
    error = '';
    fetchClashProxies();
    currentStep = 1;
    showForm = true;
    editingProfile = null;
    formName = '';
    formEnabled = true;
    formMode = 'time-based';
    formSchedule = Array.from({ length: 7 }, () => Array(24).fill(false));
    formGroupName = '';
    formProxyName = '';
  }

  function startEdit(p: Profile) {
    error = '';
    fetchClashProxies();
    currentStep = 1;
    editingProfile = p;
    formName = p.name;
    formEnabled = p.enabled;
    formMode = p.mode || 'time-based';
    formGroupName = p.group_name;
    formProxyName = p.proxy_name;

    // Load or convert schedule
    if (p.schedule && p.schedule.length === 7) {
      formSchedule = p.schedule.map(row => [...row]);
    } else {
      formSchedule = Array.from({ length: 7 }, () => Array(24).fill(false));
      if (p.days_of_week && p.start_time && p.end_time) {
        const startHour = parseInt(p.start_time.split(':')[0], 10);
        const endHour = parseInt(p.end_time.split(':')[0], 10);
        for (const day of p.days_of_week) {
          for (let h = startHour; h <= endHour; h++) {
            if (h >= 0 && h < 24) {
              formSchedule[day][h] = true;
            }
          }
        }
      }
    }

    showForm = true;
  }

  function createFromTemplate(templateName: string) {
    startCreate();
    formName = templateName === 'night'
      ? $t('smartproxy.preset_night_title')
      : templateName === 'workday'
        ? $t('smartproxy.preset_workdays')
        : $t('smartproxy.preset_always_title');

    formSchedule = Array.from({ length: 7 }, () => Array(24).fill(false));

    if (templateName === 'night') {
      const nightHours = [23, 0, 1, 2, 3, 4, 5, 6, 7];
      for (let d = 0; d < 7; d++) {
        for (const h of nightHours) {
          formSchedule[d][h] = true;
        }
      }
    } else if (templateName === 'workday') {
      for (let d = 1; d <= 5; d++) {
        for (let h = 9; h <= 17; h++) {
          formSchedule[d][h] = true;
        }
      }
    } else if (templateName === 'always') {
      for (let d = 0; d < 7; d++) {
        for (let h = 0; h < 24; h++) {
          formSchedule[d][h] = true;
        }
      }
    }

    // Directly open target select step
    currentStep = 2;
  }

  function cancelEdit() {
    showForm = false;
    editingProfile = null;
    error = '';
    formName = '';
    formEnabled = true;
    formMode = 'time-based';
    formGroupName = '';
    formProxyName = '';
    formSchedule = Array.from({ length: 7 }, () => Array(24).fill(false));
  }

  function nextStep() {
    if (currentStep < 3) currentStep++;
  }

  function prevStep() {
    if (currentStep > 1) currentStep--;
  }

  // Preset functions
  function presetFillAll() {
    for (let d = 0; d < 7; d++) {
      formSchedule[d].fill(true);
    }
    formSchedule = [...formSchedule];
  }

  function presetClearAll() {
    for (let d = 0; d < 7; d++) {
      formSchedule[d].fill(false);
    }
    formSchedule = [...formSchedule];
  }

  function presetWorkdays() {
    presetClearAll();
    for (let d = 1; d <= 5; d++) {
      for (let h = 9; h <= 17; h++) {
        formSchedule[d][h] = true;
      }
    }
    formSchedule = [...formSchedule];
  }

  // Click-and-drag handlers
  function handleCellMouseDown(day: number, hour: number) {
    isDrawing = true;
    drawMode = !formSchedule[day][hour];
    formSchedule[day][hour] = drawMode;
    formSchedule = [...formSchedule];
  }

  function handleCellMouseEnter(day: number, hour: number) {
    if (isDrawing) {
      formSchedule[day][hour] = drawMode;
      formSchedule = [...formSchedule];
    }
  }

  function handleMouseUp() {
    isDrawing = false;
  }

  async function saveProfile() {
    error = '';
    if (!formName || !formGroupName || !formProxyName) {
      error = $t('smartproxy.save_error', { message: $t('smartproxy.fill_required') });
      return;
    }

    const csrfToken = localStorage.getItem('csrf_token');
    const payload: any = {
      name: formName,
      enabled: formEnabled,
      mode: formMode,
      group_name: formGroupName,
      proxy_name: formProxyName,
      schedule: formSchedule
    };

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
    error = '';
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
    error = '';
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

  // Count active slots in schedule
  function countActiveSlots(schedule?: boolean[][]): number {
    if (!schedule) return 0;
    return schedule.reduce((sum, row) => sum + row.filter(Boolean).length, 0);
  }

  onMount(() => {
    fetchProfiles();
    fetchStatus();
    const interval = setInterval(fetchStatus, 30000);
    window.addEventListener('click', handleClickOutside);
    window.addEventListener('keydown', handleKeydown);
    window.addEventListener('mouseup', handleMouseUp);
    return () => {
      clearInterval(interval);
      window.removeEventListener('click', handleClickOutside);
      window.removeEventListener('keydown', handleKeydown);
      window.removeEventListener('mouseup', handleMouseUp);
    };
  });
</script>

<div class="container">
  <div class="page-head">
    <div>
      <div class="crumbs">
        {$t('nav.group_proxy')} <span style="color:var(--fg-faint);margin:0 6px;">/</span>
        {$t('nav.smartproxy')}
      </div>
      <h1>{$t('smartproxy.title')}</h1>
      <p class="sub">{$t('smartproxy.subtitle')}</p>
    </div>
    <div class="ph-actions" style="display:flex; gap:10px;">
      <button class="btn btn-secondary" on:click={() => createFromTemplate('always')}>
        {$t('smartproxy.from_template')}
      </button>
      <button class="btn btn-primary" on:click={startCreate}>
        <svg
          width="14"
          height="14"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          stroke-width="2"
          style="margin-right: 6px;"><path d="M12 5v14M5 12h14" /></svg
        >
        {$t('smartproxy.add')}
      </button>
    </div>
  </div>

  {#if error && !showForm}
    <div class="alert alert-error mb-2">{error}</div>
  {/if}

  <!-- Current Status -->
  {#if status}
    <div class="card mb-2" style="padding:18px 22px;">
      <div style="display:flex;align-items:center;gap:14px;flex-wrap:wrap;">
        <span class="status-dot success" style="margin:0;"></span>
        <div style="font-weight:700;color:var(--fg-primary);">
          {$t('smartproxy.current_status')}
        </div>
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
    <!-- Templates Empty State -->
    <div class="empty-state-container">
      <div class="empty-state-head">
        <h2>{$t('smartproxy.no_profiles_title')}</h2>
        <p>{$t('smartproxy.no_profiles_desc')}</p>
      </div>

      <div class="template-cards-grid">
        <!-- Card 1: Night VPN -->
        <div class="card template-card" on:click={() => createFromTemplate('night')} role="button" tabindex="0">
          <div class="template-icon text-accent">
            <svg width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
              <path d="M21 12.79A9 9 0 1 1 11.21 3 7 7 0 0 0 21 12.79z"/>
            </svg>
          </div>
          <h3>{$t('smartproxy.preset_night_title')}</h3>
          <p>{$t('smartproxy.preset_night_desc')}</p>
          <button class="btn btn-secondary btn-sm" style="margin-top:auto;">
            {$t('smartproxy.select')}
          </button>
        </div>

        <!-- Card 2: Workdays -->
        <div class="card template-card" on:click={() => createFromTemplate('workday')} role="button" tabindex="0">
          <div class="template-icon text-success">
            <svg width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
              <rect x="3" y="4" width="18" height="18" rx="2" ry="2"/>
              <line x1="16" y1="2" x2="16" y2="6"/>
              <line x1="8" y1="2" x2="8" y2="6"/>
              <line x1="3" y1="10" x2="21" y2="10"/>
            </svg>
          </div>
          <h3>{$t('smartproxy.preset_workdays')}</h3>
          <p>{$t('smartproxy.preset_workday_desc')}</p>
          <button class="btn btn-secondary btn-sm" style="margin-top:auto;">
            {$t('smartproxy.select')}
          </button>
        </div>

        <!-- Card 3: 24/7 -->
        <div class="card template-card" on:click={() => createFromTemplate('always')} role="button" tabindex="0">
          <div class="template-icon text-warning">
            <svg width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
              <circle cx="12" cy="12" r="10"/>
              <polyline points="12 6 12 12 16 14"/>
            </svg>
          </div>
          <h3>{$t('smartproxy.preset_always_title')}</h3>
          <p>{$t('smartproxy.preset_always_desc')}</p>
          <button class="btn btn-secondary btn-sm" style="margin-top:auto;">
            {$t('smartproxy.select')}
          </button>
        </div>
      </div>

      <div style="margin-top:24px; text-align:center;">
        <button class="btn btn-primary" on:click={startCreate}>
          {$t('smartproxy.create_manually')}
        </button>
      </div>
    </div>
  {:else}
    <div class="profile-grid">
      {#each profiles as p}
        {@const isActive = p.enabled && status?.active.some((a) => a.id === p.id)}
        <div class="card profile-card" class:active={isActive}>
          <div class="profile-card-header">
            <span class="profile-card-name">{p.name}</span>

            {#if !p.enabled}
              <span class="badge sp-mode-badge sp-mode-disabled">
                {$t('smartproxy.disabled')}
              </span>
            {:else}
              <span class="badge sp-mode-badge sp-mode-scheduled">
                {$t('smartproxy.mode_time')}
              </span>
            {/if}

            <div style="margin-left:auto; display:flex; align-items:center; gap:12px;">
              <label class="toggle-switch">
                <input type="checkbox" checked={p.enabled} on:change={() => toggleEnabled(p)} />
                <span class="toggle-slider"></span>
              </label>

              <div class="dropdown-container">
                <button
                  class="btn btn-secondary action-btn-dots"
                  on:click={() => toggleDropdown(p.id)}>⋯</button
                >
                {#if activeDropdownId === p.id}
                  <div class="dropdown-menu">
                    <button
                      on:click={() => {
                        startEdit(p);
                        activeDropdownId = null;
                      }}>{$t('app.edit')}</button
                    >
                    <button
                      on:click={() => {
                        deleteProfile(p.id);
                        activeDropdownId = null;
                      }}
                      class="delete-action">{$t('app.delete')}</button
                    >
                  </div>
                {/if}
              </div>
            </div>
          </div>

          <div class="field-row">
            <div>
              <div class="lbl">{$t('smartproxy.target_group')}</div>
            </div>
            <div class="ctrl">
              <span class="status-badge" class:active={p.enabled}>
                {p.group_name} → {p.current_proxy || p.proxy_name}
              </span>
            </div>
          </div>

          <div class="field-row">
            <div>
              <div class="lbl">{$t('smartproxy.schedule_slots')}</div>
              <div class="desc">
                {$t('smartproxy.active_hours_weekly', { count: countActiveSlots(p.schedule) })}
              </div>
            </div>
            <div class="ctrl mono" style="font-size:12px; color:var(--fg-primary);">
              {countActiveSlots(p.schedule)} / 168 {$t('smartproxy.hours_short')}
            </div>
          </div>

          {#if p.apply_count > 0}
            <div class="field-row">
              <div>
                <div class="lbl">
                  {$t('smartproxy.execution_stats')}
                </div>
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

<!-- Add/Edit Modal (3-Step Wizard) -->
{#if showForm}
  <div
    class="modal-overlay"
    role="button"
    tabindex="0"
    on:click={cancelEdit}
    on:keydown={handleKeydown}
  >
    <div class="modal-card" role="presentation" on:click|stopPropagation>
      <div class="modal-card-header">
        <h2>{editingProfile ? $t('smartproxy.edit_profile') : $t('smartproxy.new_profile')}</h2>
        <button class="modal-close-btn" on:click={cancelEdit}>&times;</button>
      </div>

      <!-- Step Indicators -->
      <div class="wizard-steps-bar">
        <div class="wizard-step-indicator" class:active={currentStep >= 1}>
          <span class="step-num">1</span>
          <span class="step-lbl">{$t('smartproxy.step_1')}</span>
        </div>
        <div class="wizard-step-line" class:active={currentStep >= 2}></div>
        <div class="wizard-step-indicator" class:active={currentStep >= 2}>
          <span class="step-num">2</span>
          <span class="step-lbl">{$t('smartproxy.step_2')}</span>
        </div>
        <div class="wizard-step-line" class:active={currentStep >= 3}></div>
        <div class="wizard-step-indicator" class:active={currentStep >= 3}>
          <span class="step-num">3</span>
          <span class="step-lbl">{$t('smartproxy.step_3')}</span>
        </div>
      </div>

      <div class="modal-card-body">
        {#if error}
          <div class="alert alert-error mb-2">{error}</div>
        {/if}
        <!-- STEP 1: Basic Info -->
        {#if currentStep === 1}
          <div class="form-group">
            <label for="sp-name" class="form-label">{$t('smartproxy.name')} *</label>
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
            <select id="sp-mode" class="input" bind:value={formMode} disabled>
              <option value="time-based">{$t('smartproxy.mode_time')}</option>
            </select>
            <p class="hint" style="margin-top:6px;">
              {$t('smartproxy.schedule_mode_hint')}
            </p>
          </div>

          <div class="form-group-checkbox" style="margin-top: 10px;">
            <label class="toggle-switch">
              <input type="checkbox" id="sp-enabled" bind:checked={formEnabled} />
              <span class="toggle-slider"></span>
            </label>
            <label for="sp-enabled" class="checkbox-label">
              {$t('smartproxy.profile_active')}
            </label>
          </div>
        {/if}

        <!-- STEP 2: Targets Selection -->
        {#if currentStep === 2}
          <div class="form-group">
            <label for="sp-group" class="form-label">{$t('smartproxy.proxy_group')} *</label>
            {#if mihomoGroups.length > 0}
              <select id="sp-group" class="input" bind:value={formGroupName}>
                <option value="">-- {$t('smartproxy.select_group')} --</option>
                {#each mihomoGroups as g}
                  <option value={g}>{g}</option>
                {/each}
              </select>
            {:else}
              <input
                id="sp-group"
                type="text"
                class="input"
                bind:value={formGroupName}
                placeholder={$t('smartproxy.proxy_group_placeholder')}
              />
            {/if}
          </div>

          <div class="form-group">
            <label for="sp-proxy" class="form-label">{$t('smartproxy.proxy')} *</label>
            {#if mihomoProxies.length > 0}
              <select id="sp-proxy" class="input" bind:value={formProxyName}>
                <option value="">-- {$t('smartproxy.select_proxy')} --</option>
                <option value="DIRECT">DIRECT</option>
                {#each mihomoProxies as p}
                  <option value={p}>{p}</option>
                {/each}
              </select>
            {:else}
              <input
                id="sp-proxy"
                type="text"
                class="input"
                bind:value={formProxyName}
                placeholder={$t('smartproxy.proxy_placeholder')}
              />
            {/if}
          </div>
        {/if}

        <!-- STEP 3: Grid Scheduler -->
        {#if currentStep === 3}
          <div class="grid-presets-toolbar">
            <button type="button" class="btn btn-secondary btn-sm" on:click={presetFillAll}>
              {$t('smartproxy.preset_fill')}
            </button>
            <button type="button" class="btn btn-secondary btn-sm" on:click={presetClearAll}>
              {$t('smartproxy.preset_clear')}
            </button>
            <button type="button" class="btn btn-secondary btn-sm" on:click={presetWorkdays}>
              {$t('smartproxy.preset_workdays')}
            </button>
          </div>

          <p class="hint" style="margin-bottom:8px;">
            {$t('smartproxy.grid_paint_hint')}
          </p>

          <!-- 7x24 Grid Container with thin scrollbar -->
          <div class="grid-scrollbar-container">
            <div class="schedule-grid-table">
              <!-- Top Hour Headers -->
              <div class="grid-row-header">
                <div class="day-label-sticky header-cell"></div>
                {#each Array(24) as _, h}
                  <div class="hour-header-cell">{h.toString().padStart(2, '0')}</div>
                {/each}
              </div>

              <!-- Grid Rows per Day -->
              {#each allDays as d}
                <div class="grid-row-day">
                  <div class="day-label-sticky">{dayNames[d]}</div>
                  {#each Array(24) as _, h}
                    {@const isCellActive = formSchedule[d][h]}
                    <div
                      class="grid-cell"
                      class:active={isCellActive}
                      on:mousedown|preventDefault={() => handleCellMouseDown(d, h)}
                      on:mouseenter={() => handleCellMouseEnter(d, h)}
                      role="presentation"
                    ></div>
                  {/each}
                </div>
              {/each}
            </div>
          </div>
        {/if}
      </div>

      <div class="modal-card-footer">
        {#if currentStep > 1}
          <button class="btn btn-secondary" on:click={prevStep} style="margin-right:auto;">
            {$t('app.back')}
          </button>
        {/if}
        <button class="btn btn-secondary" on:click={cancelEdit}>{$t('app.cancel')}</button>
        {#if currentStep < 3}
          <button class="btn btn-primary" on:click={nextStep} disabled={currentStep === 2 && (!formGroupName || !formProxyName)}>
            {$t('app.continue')}
          </button>
        {:else}
          <button class="btn btn-primary" on:click={saveProfile}>
            {$t('app.save')}
          </button>
        {/if}
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
    max-width: 680px; /* Slightly wider modal for beautiful grid scroll */
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

  /* Wizard Step indicators styling */
  .wizard-steps-bar {
    display: flex;
    align-items: center;
    background: var(--bg-card);
    border-bottom: 1px solid var(--border);
    padding: 12px 24px;
    gap: 10px;
  }

  .wizard-step-indicator {
    display: flex;
    align-items: center;
    gap: 8px;
    opacity: 0.45;
    transition: opacity 0.25s ease;
  }

  .wizard-step-indicator.active {
    opacity: 1;
  }

  .wizard-step-indicator .step-num {
    display: flex;
    align-items: center;
    justify-content: center;
    width: 20px;
    height: 20px;
    border-radius: 50%;
    background: var(--accent);
    color: #fff;
    font-size: 11px;
    font-weight: 700;
  }

  .wizard-step-indicator .step-lbl {
    font-size: 12px;
    font-weight: 700;
    color: var(--fg-primary);
  }

  .wizard-step-line {
    flex-grow: 1;
    height: 2px;
    background: var(--border);
    opacity: 0.5;
  }

  .wizard-step-line.active {
    background: var(--accent);
    opacity: 0.8;
  }

  /* 7x24 grid schedule styling */
  .grid-presets-toolbar {
    display: flex;
    gap: 8px;
    margin-bottom: 8px;
  }

  .grid-scrollbar-container {
    overflow-x: auto;
    max-width: 100%;
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    background: var(--bg-card);
    scrollbar-width: thin;
  }

  .schedule-grid-table {
    display: flex;
    flex-direction: column;
    min-width: 600px;
  }

  .grid-row-header, .grid-row-day {
    display: grid;
    grid-template-columns: 80px repeat(24, 1fr);
  }

  .day-label-sticky {
    position: sticky;
    left: 0;
    background: var(--bg-card);
    padding: 8px;
    font-size: 11px;
    font-weight: 700;
    color: var(--fg-secondary);
    border-right: 1px solid var(--border);
    display: flex;
    align-items: center;
    z-index: 2;
  }

  .day-label-sticky.header-cell {
    background: var(--bg-card);
    border-bottom: 1px solid var(--border);
  }

  .hour-header-cell {
    padding: 6px 4px;
    font-size: 10px;
    font-weight: 700;
    text-align: center;
    color: var(--fg-faint);
    border-bottom: 1px solid var(--border);
    border-right: 1px solid rgba(255, 255, 255, 0.05);
  }

  .grid-cell {
    height: 32px;
    border-right: 1px solid var(--border);
    border-bottom: 1px solid var(--border);
    cursor: crosshair;
    background: rgba(255, 255, 255, 0.02);
    transition: background var(--transition-fast);
  }

  .grid-cell:hover {
    background: rgba(41, 194, 240, 0.15);
  }

  .grid-cell.active {
    background: var(--accent);
    box-shadow: inset 0 0 0 1px rgba(255, 255, 255, 0.15);
  }

  .grid-cell.active:hover {
    background: var(--accent-hover);
  }

  .grid-row-day:last-child .grid-cell {
    border-bottom: none;
  }

  .grid-row-day .grid-cell:last-child {
    border-right: none;
  }

  /* Empty state template cards design */
  .empty-state-container {
    display: flex;
    flex-direction: column;
    align-items: center;
    padding: 24px 0;
  }

  .empty-state-head {
    text-align: center;
    margin-bottom: 32px;
    max-width: 480px;
  }

  .empty-state-head h2 {
    font-size: 18px;
    font-weight: 700;
    color: var(--fg-primary);
    margin-bottom: 8px;
  }

  .empty-state-head p {
    font-size: 13px;
    color: var(--fg-dim);
    line-height: 1.5;
  }

  .template-cards-grid {
    display: grid;
    grid-template-columns: repeat(3, 1fr);
    gap: 16px;
    width: 100%;
    max-width: 820px;
  }

  @media (max-width: 768px) {
    .template-cards-grid {
      grid-template-columns: 1fr;
    }
  }

  .template-card {
    padding: 20px;
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    gap: 12px;
    text-align: left;
    cursor: pointer;
    transition: border-color var(--transition-fast), transform var(--transition-fast);
  }

  .template-card:hover {
    border-color: var(--accent);
    transform: translateY(-2px);
  }

  .template-icon {
    display: flex;
    align-items: center;
    justify-content: center;
    width: 42px;
    height: 42px;
    border-radius: 8px;
    background: rgba(255, 255, 255, 0.03);
  }

  .template-card h3 {
    font-size: 14px;
    font-weight: 700;
    color: var(--fg-primary);
    margin: 0;
  }

  .template-card p {
    font-size: 12px;
    color: var(--fg-dim);
    line-height: 1.4;
    margin: 0;
  }

  .text-accent { color: var(--accent); }
  .text-success { color: #10b981; }
  .text-warning { color: #f59e0b; }

  /* Mode color badges */
  :global(.sp-mode-badge) {
    font-size: 10.5px;
    font-weight: 700;
    letter-spacing: 0.04em;
    text-transform: uppercase;
    padding: 2px 7px;
    border-radius: 4px;
  }

  :global(.sp-mode-scheduled) {
    background: rgba(41, 194, 240, 0.1);
    color: var(--accent);
    border: 1px solid rgba(41, 194, 240, 0.25);
  }

  :global(.sp-mode-disabled) {
    background: rgba(239, 68, 68, 0.1);
    color: #ef4444;
    border: 1px solid rgba(239, 68, 68, 0.25);
  }
</style>
