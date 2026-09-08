<script lang="ts">
  import { onMount } from 'svelte';
  import { usePoller } from './lib/poller';
  import Modal from './components/Modal.svelte';
  import { t, currentLang } from './i18n';
  import { showConfirm, showToast } from './stores';
  import { apiFetch, apiFetchJSON } from './lib/api';

  interface Props {
    onSwitchTab?: (tab: string) => void;
  }

  let { onSwitchTab = () => {} }: Props = $props();

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
    timezone?: string;
  }

  let profiles: Profile[] = $state([]);
  let status: Status | null = $state(null);
  let loading = $state(false);
  let error = $state('');
  let activeDropdownId: string | null = $state(null);

  // Clash proxies
  let mihomoGroups: string[] = $state([]);
  let mihomoProxies: string[] = $state([]);

  // Form & Wizard state
  let showForm = $state(false);
  let currentStep = $state(1);
  let editingProfile: Profile | null = $state(null);

  let formName = $state('');
  let formEnabled = $state(true);
  let formMode = $state('time-based');
  let formGroupName = $state('');
  let formProxyName = $state('');
  let formSchedule: boolean[][] = $state(Array.from({ length: 7 }, () => Array(24).fill(false)));

  // Click-and-drag drawing state
  let isDrawing = $state(false);
  let drawMode = $state(true); // true to draw, false to erase

  let dayNames = $derived($t('smartproxy.days').split(','));
  const displayDayIndices = [1, 2, 3, 4, 5, 6, 0];

  async function fetchProfiles() {
    loading = true;
    error = '';
    try {
      const res = await apiFetch('/api/smart-proxy/profiles');
      if (!res.ok) throw new Error(`HTTP ${res.status}`);
      profiles = await res.json();
    } catch (e: any) {
      if (e?.status === 401) return;
      error = e.message;
      showToast('error', e.message);
    } finally {
      loading = false;
    }
  }

  async function fetchStatus(signal?: AbortSignal) {
    try {
      const res = await apiFetch('/api/smart-proxy/status', { signal });
      if (res.ok) status = await res.json();
    } catch (e: any) {
      if (e?.name === 'AbortError') return;
      if (e?.status === 401) return;
      throw e;
    }
  }

  async function fetchClashProxies() {
    try {
      const res = await apiFetch('/api/mihomo/proxy/proxies');
      if (res.ok) {
        const data = await res.json();
        const groups: string[] = [];
        const proxies: string[] = [];
        for (const [name, p] of Object.entries(data.proxies || {})) {
          const type = (p as any).type;
          if (
            type === 'Selector' ||
            type === 'Fallback' ||
            type === 'URLTest' ||
            type === 'LoadBalance'
          ) {
            groups.push(name);
          } else if (type !== 'Direct' && type !== 'Reject') {
            proxies.push(name);
          }
        }
        mihomoGroups = groups.sort();
        mihomoProxies = proxies.sort();
      }
    } catch (e: any) {
      if (e?.status === 401) return;
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
      formSchedule = p.schedule.map((row) => [...row]);
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

  function createFromTemplate(templateName: 'workdays' | 'night' | 'weekend') {
    startCreate();
    formSchedule = Array.from({ length: 7 }, () => Array(24).fill(false));

    if (templateName === 'workdays') {
      formName = $t('smartproxy.preset_workdays_title');
      for (let d = 1; d <= 5; d++) {
        for (let h = 9; h <= 17; h++) {
          formSchedule[d][h] = true;
        }
      }
    } else if (templateName === 'night') {
      formName = $t('smartproxy.preset_night_title');
      for (let d = 0; d < 7; d++) {
        for (let h = 0; h < 8; h++) {
          formSchedule[d][h] = true;
        }
      }
    } else if (templateName === 'weekend') {
      formName = $t('smartproxy.preset_weekend_title');
      for (const d of [0, 6]) {
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

  // Click-and-drag handlers & Touch handlers
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

  function handleTouchStart(day: number, hour: number, e: TouchEvent) {
    isDrawing = true;
    drawMode = !formSchedule[day][hour];
    formSchedule[day][hour] = drawMode;
    formSchedule = [...formSchedule];
  }

  function handleTouchMove(e: TouchEvent) {
    if (!isDrawing || !e.touches || e.touches.length === 0) return;
    const touch = e.touches[0];
    const target = document
      .elementFromPoint(touch.clientX, touch.clientY)
      ?.closest('.grid-cell') as HTMLElement | null;
    if (target && target.dataset.day !== undefined && target.dataset.hour !== undefined) {
      const day = parseInt(target.dataset.day, 10);
      const hour = parseInt(target.dataset.hour, 10);
      if (
        !isNaN(day) &&
        !isNaN(hour) &&
        formSchedule[day] &&
        formSchedule[day][hour] !== drawMode
      ) {
        formSchedule[day][hour] = drawMode;
        formSchedule = [...formSchedule];
      }
    }
  }

  function handleTouchEnd() {
    isDrawing = false;
  }

  async function saveProfile() {
    error = '';
    if (!formName || !formGroupName || !formProxyName) {
      error = $t('smartproxy.save_error', { message: $t('smartproxy.fill_required') });
      return;
    }

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
      const res = await apiFetch(url, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json'
        },
        body: JSON.stringify(payload)
      });

      if (!res.ok) throw new Error('Failed to save');

      showForm = false;
      editingProfile = null;
      await fetchProfiles();
      await fetchStatus();
    } catch (e: any) {
      if (e?.status === 401) return;
      error = e.message;
      showToast('error', e.message);
    }
  }

  async function deleteProfile(id: string) {
    error = '';
    if (!(await showConfirm($t('app.confirm'), $t('app.delete') + '?'))) return;
    try {
      await apiFetchJSON(`/api/smart-proxy/profiles/delete?id=${id}`, {
        method: 'POST'
      });
      await fetchProfiles();
      await fetchStatus();
    } catch (e: any) {
      if (e?.status === 401) return;
      error = e.message;
      showToast('error', e.message);
    }
  }

  async function toggleEnabled(p: Profile) {
    error = '';
    try {
      await apiFetchJSON(`/api/smart-proxy/profiles/enabled?id=${p.id}&enabled=${!p.enabled}`, {
        method: 'POST'
      });
      await fetchProfiles();
      await fetchStatus();
    } catch (e: any) {
      if (e?.status === 401) return;
      error = e.message;
      showToast('error', e.message);
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
    const statusPoller = usePoller((signal) => fetchStatus(signal), 30000);
    window.addEventListener('click', handleClickOutside);
    window.addEventListener('keydown', handleKeydown);
    window.addEventListener('mouseup', handleMouseUp);
    window.addEventListener('touchend', handleTouchEnd);
    window.addEventListener('touchcancel', handleTouchEnd);
    return () => {
      statusPoller.stop();
      window.removeEventListener('click', handleClickOutside);
      window.removeEventListener('keydown', handleKeydown);
      window.removeEventListener('mouseup', handleMouseUp);
      window.removeEventListener('touchend', handleTouchEnd);
      window.removeEventListener('touchcancel', handleTouchEnd);
    };
  });
</script>

<div class="container">
  <div class="page-head">
    <div>
      <div class="crumbs">
        {$t('nav.group_proxy_subs')} <span class="crumb-sep">›</span>
        {$t('smartproxy.title')}
      </div>
      <h1>{$t('smartproxy.title')}</h1>
      <p class="sub">{$t('smartproxy.subtitle')}</p>
    </div>
    <div class="ph-actions">
      <button class="btn btn-primary" onclick={startCreate}>
        <svg
          width="14"
          height="14"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          stroke-width="2"
          style="margin-right: 6px;"
        >
          <path d="M12 5v14M5 12h14" />
        </svg>
        {$t('smartproxy.create_profile')}
      </button>
    </div>
  </div>

  {#if error && !showForm}
    <div class="alert alert-error mb-2">{error}</div>
  {/if}

  <!-- Current Status -->
  {#if status}
    {@const hasActive = status.active && status.active.length > 0}
    <div class="card status-card mb-2">
      <div class="status-card-inner">
        <div class="status-indicator-group">
          <span class="status-dot {hasActive ? 'active' : 'inactive'}" aria-hidden="true"></span>
          <div class="status-label">
            {$t('smartproxy.current_status')}
          </div>
          {#if hasActive}
            <div class="status-active-badges">
              {#each status.active as p}
                <span class="status-badge active">{p.name} → {p.current_proxy || p.proxy_name}</span
                >
              {/each}
            </div>
          {:else}
            <div class="status-no-active-text">
              {$t('smartproxy.status_no_active')}
            </div>
          {/if}
        </div>

        <div class="status-time-group">
          <svg
            width="14"
            height="14"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            stroke-width="2"
            class="time-icon"
          >
            <circle cx="12" cy="12" r="10"></circle>
            <polyline points="12 6 12 12 16 14"></polyline>
          </svg>
          <span class="status-time-text">
            {$t('smartproxy.router_time', {
              time: status.time,
              tz: status.timezone || dayNames[status.day]
            })}
          </span>
        </div>
      </div>
    </div>
  {/if}

  <!-- Profile List -->
  {#if profiles.length === 0}
    <!-- Templates Empty State & Balanced Onboarding -->
    <div class="sp-onboarding-hero">
      <div class="empty-state-head">
        <h2>{$t('smartproxy.no_profiles_title')}</h2>
        <p>{$t('smartproxy.no_profiles_desc')}</p>
      </div>

      <div class="template-cards-grid">
        <!-- Card 1: Work Hours -->
        <button
          type="button"
          class="card template-card"
          onclick={() => createFromTemplate('workdays')}
        >
          <div class="template-card-top">
            <div class="template-icon text-accent">
              <svg
                width="22"
                height="22"
                viewBox="0 0 24 24"
                fill="none"
                stroke="currentColor"
                stroke-width="2"
              >
                <rect x="2" y="7" width="20" height="14" rx="2" ry="2"></rect>
                <path d="M16 21V5a2 2 0 0 0-2-2h-4a2 2 0 0 0-2 2v16"></path>
              </svg>
            </div>
            <span class="template-badge">{$t('smartproxy.preset_workdays_badge')}</span>
          </div>
          <h3>{$t('smartproxy.preset_workdays_title')}</h3>
          <p>{$t('smartproxy.preset_workdays_desc')}</p>
          <div class="template-timeline" aria-hidden="true">
            <div class="timeline-track">
              <div class="timeline-segment" style="left: 37.5%; width: 37.5%;"></div>
            </div>
            <div class="timeline-labels">
              <span>00:00</span>
              <span class="lbl-highlight" style="left: 37.5%;">09:00</span>
              <span class="lbl-highlight" style="left: 75%;">18:00</span>
              <span>24:00</span>
            </div>
          </div>
        </button>

        <!-- Card 2: Night Mode -->
        <button
          type="button"
          class="card template-card"
          onclick={() => createFromTemplate('night')}
        >
          <div class="template-card-top">
            <div class="template-icon text-warning">
              <svg
                width="22"
                height="22"
                viewBox="0 0 24 24"
                fill="none"
                stroke="currentColor"
                stroke-width="2"
              >
                <path d="M21 12.79A9 9 0 1 1 11.21 3 7 7 0 0 0 21 12.79z" />
              </svg>
            </div>
            <span class="template-badge">{$t('smartproxy.preset_night_badge')}</span>
          </div>
          <h3>{$t('smartproxy.preset_night_title')}</h3>
          <p>{$t('smartproxy.preset_night_desc')}</p>
          <div class="template-timeline" aria-hidden="true">
            <div class="timeline-track">
              <div class="timeline-segment" style="left: 0%; width: 33.3%;"></div>
            </div>
            <div class="timeline-labels">
              <span>00:00</span>
              <span class="lbl-highlight" style="left: 33.3%;">08:00</span>
              <span>24:00</span>
            </div>
          </div>
        </button>

        <!-- Card 3: Weekend -->
        <button
          type="button"
          class="card template-card"
          onclick={() => createFromTemplate('weekend')}
        >
          <div class="template-card-top">
            <div class="template-icon text-success">
              <svg
                width="22"
                height="22"
                viewBox="0 0 24 24"
                fill="none"
                stroke="currentColor"
                stroke-width="2"
              >
                <rect x="3" y="4" width="18" height="18" rx="2" ry="2" />
                <line x1="16" y1="2" x2="16" y2="6" />
                <line x1="8" y1="2" x2="8" y2="6" />
                <line x1="3" y1="10" x2="21" y2="10" />
              </svg>
            </div>
            <span class="template-badge">{$t('smartproxy.preset_weekend_badge')}</span>
          </div>
          <h3>{$t('smartproxy.preset_weekend_title')}</h3>
          <p>{$t('smartproxy.preset_weekend_desc')}</p>
          <div class="template-timeline" aria-hidden="true">
            <div class="timeline-track">
              <div class="timeline-segment" style="left: 0%; width: 100%;"></div>
            </div>
            <div class="timeline-labels">
              <span>00:00</span>
              <span class="lbl-highlight" style="left: 50%;">12:00</span>
              <span>24:00</span>
            </div>
          </div>
        </button>
      </div>

      <div class="manual-create-wrap">
        <button type="button" class="btn-link manual-create-btn" onclick={startCreate}>
          {$t('smartproxy.or_create_manual')}
        </button>
      </div>
    </div>
  {:else}
    <div class="profile-grid">
      {#each profiles as p}
        {@const isActive = p.enabled && status?.active?.some((a) => a.id === p.id)}
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
                <input type="checkbox" checked={p.enabled} onchange={() => toggleEnabled(p)} />
                <span class="toggle-slider"></span>
              </label>

              <div class="dropdown-container">
                <button
                  class="btn btn-secondary action-btn-dots"
                  onclick={() => toggleDropdown(p.id)}>⋯</button
                >
                {#if activeDropdownId === p.id}
                  <div class="dropdown-menu">
                    <button
                      onclick={() => {
                        startEdit(p);
                        activeDropdownId = null;
                      }}>{$t('app.edit')}</button
                    >
                    <button
                      onclick={() => {
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
<Modal
  isOpen={showForm}
  title={editingProfile ? $t('smartproxy.edit_profile') : $t('smartproxy.new_profile')}
  onclose={cancelEdit}
  maxWidth="680px"
>
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

  <div style="display: flex; flex-direction: column; gap: 16px;">
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
        <label for="sp-group" class="form-label">{$t('smartproxy.form_proxy_group')} *</label>
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
        <label for="sp-proxy" class="form-label">{$t('smartproxy.form_target_proxy')} *</label>
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
        <button type="button" class="btn btn-secondary btn-sm" onclick={presetFillAll}>
          {$t('smartproxy.preset_fill')}
        </button>
        <button type="button" class="btn btn-secondary btn-sm" onclick={presetClearAll}>
          {$t('smartproxy.preset_clear')}
        </button>
        <button type="button" class="btn btn-secondary btn-sm" onclick={presetWorkdays}>
          {$t('smartproxy.preset_workdays')}
        </button>
      </div>

      <p class="hint" style="margin-bottom:8px;">
        {$t('smartproxy.grid_paint_hint')}
      </p>

      <!-- 7x24 Grid Container with thin scrollbar -->
      <div
        class="grid-scrollbar-container"
        ontouchmove={handleTouchMove}
        role="region"
        aria-label={$t('smartproxy.schedule_slots')}
      >
        <div class="schedule-grid-table">
          <!-- Top Hour Headers -->
          <div class="grid-row-header">
            <div class="day-label-sticky header-cell"></div>
            {#each Array(24) as _, h}
              <div class="hour-header-cell">{h.toString().padStart(2, '0')}</div>
            {/each}
          </div>

          <!-- Grid Rows per Day (Mon to Sun) -->
          {#each displayDayIndices as d}
            <div class="grid-row-day">
              <div class="day-label-sticky">{dayNames[d]}</div>
              {#each Array(24) as _, h}
                {@const isCellActive = formSchedule[d][h]}
                <div
                  class="grid-cell"
                  class:active={isCellActive}
                  data-day={d}
                  data-hour={h}
                  onmousedown={(e) => {
                    e.preventDefault();
                    handleCellMouseDown(d, h);
                  }}
                  onmouseenter={() => handleCellMouseEnter(d, h)}
                  ontouchstart={(e) => handleTouchStart(d, h, e)}
                  role="presentation"
                ></div>
              {/each}
            </div>
          {/each}
        </div>
      </div>
    {/if}
  </div>

  <div style="display: flex; justify-content: flex-end; gap: 12px; margin-top: 16px;">
    {#if currentStep > 1}
      <button class="btn btn-secondary" onclick={prevStep} style="margin-right:auto;">
        {$t('app.back')}
      </button>
    {/if}
    <button class="btn btn-secondary" onclick={cancelEdit}>{$t('app.cancel')}</button>
    {#if currentStep < 3}
      <button
        class="btn btn-primary"
        onclick={nextStep}
        disabled={currentStep === 2 && (!formGroupName || !formProxyName)}
      >
        {$t('app.continue')}
      </button>
    {:else}
      <button class="btn btn-primary" onclick={saveProfile}>
        {$t('app.save')}
      </button>
    {/if}
  </div>
</Modal>

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
    color: var(--btn-primary-text);
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
    touch-action: none;
    -webkit-overflow-scrolling: touch;
  }

  .schedule-grid-table {
    display: flex;
    flex-direction: column;
    min-width: 600px;
    user-select: none;
    touch-action: none;
  }

  .grid-row-header,
  .grid-row-day {
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
    touch-action: none;
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

  /* Status Banner */
  .status-card {
    padding: 14px 20px;
    background: var(--bg-card);
    border: 1px solid var(--border);
    border-radius: var(--radius-lg);
  }

  .status-card-inner {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 16px;
    flex-wrap: wrap;
  }

  .status-indicator-group {
    display: flex;
    align-items: center;
    gap: 12px;
    flex-wrap: wrap;
  }

  .status-label {
    font-weight: 700;
    font-size: 14px;
    color: var(--fg-primary);
  }

  .status-no-active-text {
    font-size: 13px;
    color: var(--fg-dim);
  }

  .status-active-badges {
    display: flex;
    gap: 8px;
    flex-wrap: wrap;
  }

  .status-time-group {
    display: flex;
    align-items: center;
    gap: 6px;
    font-size: 12px;
    font-family: var(--font-family-mono);
    color: var(--fg-dim);
    margin-left: auto;
  }

  :global(.status-dot.inactive) {
    background-color: var(--fg-dim, #64748b);
    box-shadow: none;
  }

  :global(.status-dot.active) {
    background-color: var(--success, #46d18a);
    box-shadow: 0 0 8px rgba(70, 209, 138, 0.4);
  }

  /* Empty state template cards design & Onboarding Hero */
  .sp-onboarding-hero {
    display: flex;
    flex-direction: column;
    align-items: center;
    padding: 32px 0 24px;
    width: 100%;
  }

  .empty-state-head {
    text-align: center;
    margin-bottom: 28px;
    max-width: 560px;
  }

  .empty-state-head h2 {
    font-size: 20px;
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
    max-width: 900px;
  }

  @media (max-width: 820px) {
    .template-cards-grid {
      grid-template-columns: 1fr;
    }
  }

  .template-card {
    padding: 20px;
    display: flex;
    flex-direction: column;
    align-items: stretch;
    gap: 12px;
    text-align: left;
    cursor: pointer;
    background: var(--bg-card);
    border: 1px solid var(--border);
    border-radius: var(--radius-lg);
    outline: none;
    transition:
      border-color var(--transition-fast),
      transform var(--transition-fast),
      box-shadow var(--transition-fast);
  }

  .template-card:hover {
    border-color: var(--accent);
    transform: translateY(-2px);
    box-shadow: 0 6px 20px rgba(41, 194, 240, 0.08);
  }

  .template-card:focus-visible {
    border-color: var(--accent);
    box-shadow: 0 0 0 2px var(--accent);
  }

  .template-card-top {
    display: flex;
    align-items: center;
    justify-content: space-between;
    width: 100%;
  }

  .template-icon {
    display: flex;
    align-items: center;
    justify-content: center;
    width: 38px;
    height: 38px;
    border-radius: var(--radius-md);
    background: rgba(255, 255, 255, 0.04);
  }

  .template-badge {
    font-size: 11px;
    font-weight: 600;
    color: var(--fg-secondary);
    background: rgba(255, 255, 255, 0.05);
    padding: 3px 8px;
    border-radius: var(--radius-sm);
    border: 1px solid var(--border);
  }

  .template-card h3 {
    font-size: 15px;
    font-weight: 700;
    color: var(--fg-primary);
    margin: 0;
  }

  .template-card p {
    font-size: 12.5px;
    color: var(--fg-dim);
    line-height: 1.45;
    margin: 0;
    flex-grow: 1;
  }

  /* 24-hour mini-timeline */
  .template-timeline {
    display: flex;
    flex-direction: column;
    gap: 6px;
    margin-top: 6px;
    width: 100%;
  }

  .timeline-track {
    position: relative;
    width: 100%;
    height: 6px;
    background: rgba(255, 255, 255, 0.08);
    border-radius: 3px;
    overflow: hidden;
  }

  .timeline-segment {
    position: absolute;
    top: 0;
    bottom: 0;
    background: var(--accent);
    border-radius: 3px;
  }

  .timeline-labels {
    position: relative;
    display: flex;
    justify-content: space-between;
    font-size: 10px;
    font-family: var(--font-family-mono);
    color: var(--fg-faint);
  }

  .lbl-highlight {
    color: var(--accent);
    font-weight: 600;
  }

  .manual-create-wrap {
    margin-top: 24px;
    text-align: center;
  }

  .manual-create-btn {
    background: none;
    border: none;
    color: var(--fg-dim);
    font-size: 13px;
    cursor: pointer;
    text-decoration: underline;
    text-underline-offset: 4px;
    transition: color var(--transition-fast);
  }

  .manual-create-btn:hover {
    color: var(--accent);
  }

  .text-accent {
    color: var(--accent);
  }
  .text-success {
    color: var(--success);
  }
  .text-warning {
    color: var(--warning);
  }

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
    background: color-mix(in srgb, var(--accent) 10%, transparent);
    color: var(--accent);
    border: 1px solid color-mix(in srgb, var(--accent) 25%, transparent);
  }

  :global(.sp-mode-disabled) {
    background: color-mix(in srgb, var(--danger) 10%, transparent);
    color: var(--danger);
    border: 1px solid color-mix(in srgb, var(--danger) 25%, transparent);
  }
</style>
