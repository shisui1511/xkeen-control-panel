<script lang="ts">
  import { onMount } from 'svelte';
  import { t, currentLang, pluralize } from './i18n';
  import { showToast, fetchCapabilities, showConfirm, isKernelChecking } from './stores';
  import Skeleton from './components/Skeleton.svelte';

  export let onSwitchTab: (tab: string) => void = () => {};

  interface Kernel {
    name: string;
    display_name: string;
    binary_path: string;
    current_version: string;
    latest_version: string;
    has_update: boolean;
    channel: string;
    status: string;
    process_status: string;
    message: string;
    pid?: number;
    uptime?: string;
    api_addr?: string;
  }

  interface XKeenStatusInfo {
    isRunning: boolean;
    activeKernel: string;
    pid: number;
    uptime: string;
    binaryPath: string;
    raw: string;
  }

  let xkeenInfo: XKeenStatusInfo = {
    isRunning: false,
    activeKernel: '',
    pid: 0,
    uptime: '',
    binaryPath: '',
    raw: ''
  };

  let xkeenStatus = '';
  let loading = false;
  let actionLoading: Record<string, boolean> = {};

  let kernels: Kernel[] = [];
  let kernelsLoaded = false;
  let statusIntervals: Record<string, ReturnType<typeof setInterval>> = {};

  // Restart log
  interface RestartLogEntry {
    timestamp: number;
    action: string;
    success: boolean;
    exit_code: number;
    output: string;
  }
  let restartLog: RestartLogEntry[] = [];
  let restartLogExpanded = false;

  async function fetchRestartLog() {
    try {
      const res = await fetch('/api/service/restart-log');
      if (res.ok) restartLog = await res.json();
    } catch (_) {}
  }

  function formatAction(action: string): string {
    const map: Record<string, string> = {
      start: $t('svc.log_action_start'),
      stop: $t('svc.log_action_stop'),
      restart: $t('svc.log_action_restart')
    };
    if (action.startsWith('switch_kernel:')) {
      return $t('svc.log_action_switch') + ' ' + action.split(':')[1];
    }
    return map[action] ?? action;
  }

  function formatTs(ts: number): string {
    return new Date(ts * 1000).toLocaleString();
  }

  // Auto-start toggles (localStorage-persisted until backend API exists)
  let autostartKeenetic = localStorage.getItem('autostart_keenetic') !== 'false';
  let watchdogEnabled = localStorage.getItem('watchdog_enabled') !== 'false';
  let datUpdateDaily = localStorage.getItem('dat_update_daily') === 'true';

  function toggleAutostart(key: string, value: boolean) {
    localStorage.setItem(key, String(value));
  }

  async function fetchStatus() {
    try {
      const res = await fetch('/api/service/status');
      if (res.ok) {
        const text = await res.text();
        try {
          const parsed = JSON.parse(text);
          if (parsed && parsed.success && parsed.data) {
            xkeenInfo = {
              isRunning: parsed.data.is_running,
              activeKernel: parsed.data.active_kernel || '',
              pid: parsed.data.pid || 0,
              uptime: parsed.data.uptime || '',
              binaryPath: parsed.data.binary_path || '',
              raw: parsed.data.raw || ''
            };

            const lower = xkeenInfo.raw.toLowerCase();
            if (lower.includes('не запущен') || lower.includes('not running')) {
              xkeenStatus = $t('svc.kernel_not_selected');
            } else {
              xkeenStatus = xkeenInfo.raw;
            }
          } else {
            parseRawText(text);
          }
        } catch (_) {
          parseRawText(text);
        }
      } else {
        xkeenStatus = $t('app.error');
        xkeenInfo = {
          isRunning: false,
          activeKernel: '',
          pid: 0,
          uptime: '',
          binaryPath: '',
          raw: $t('app.error')
        };
      }
    } catch (e) {
      xkeenStatus = $t('app.unavailable');
      xkeenInfo = {
        isRunning: false,
        activeKernel: '',
        pid: 0,
        uptime: '',
        binaryPath: '',
        raw: $t('app.unavailable')
      };
    }
  }

  function parseRawText(text: string) {
    const lower = text.toLowerCase();
    const isRunning = lower.includes('running') || lower.includes('запущен');
    xkeenInfo = {
      isRunning: isRunning,
      activeKernel: isRunning
        ? lower.includes('xray')
          ? 'xray'
          : lower.includes('mihomo')
            ? 'mihomo'
            : ''
        : '',
      pid: 0,
      uptime: '',
      binaryPath: '',
      raw: text
    };
    if (lower.includes('не запущен') || lower.includes('not running')) {
      xkeenStatus = $t('svc.kernel_not_selected');
    } else {
      xkeenStatus = text;
    }
  }

  async function fetchKernels() {
    try {
      const res = await fetch('/api/kernels');
      if (res.ok) {
        const envelope = await res.json();
        const list = Array.isArray(envelope) ? envelope : (envelope.data ?? []);
        kernels = list;
        kernels.forEach((k: (typeof kernels)[0]) => {
          if (k.status !== 'idle' && !statusIntervals[k.name]) {
            startPolling(k.name);
          }
        });
      }
    } catch (e) {
    } finally {
      kernelsLoaded = true;
    }
  }

  async function controlService(action: string) {
    isKernelChecking.set(false);
    const key = `xkeen-${action}`;
    actionLoading[key] = true;
    try {
      const csrfToken = localStorage.getItem('csrf_token');
      const res = await fetch(`/api/service/control?action=${action}`, {
        method: 'POST',
        headers: { 'X-CSRF-Token': csrfToken || '' }
      });
      const text = await res.text();
      if (!res.ok) throw new Error(text);
      await fetchStatus();
      await fetchCapabilities();
      fetchRestartLog();
    } catch (e: any) {
      showToast('error', `${$t('svc.action_error')}: ${e.message}`);
      fetchRestartLog();
    } finally {
      actionLoading[key] = false;
    }
  }

  async function switchKernel(kernel: string) {
    isKernelChecking.set(false);
    switchingKernelTo = kernel;
    actionLoading[`switch-${kernel}`] = true;
    try {
      const csrfToken = localStorage.getItem('csrf_token');
      const res = await fetch(`/api/service/control?action=switch_kernel&kernel=${kernel}`, {
        method: 'POST',
        headers: { 'X-CSRF-Token': csrfToken || '' }
      });
      const text = await res.text();
      if (!res.ok) throw new Error(text);
      await fetchStatus();
      await fetchKernels();
      await fetchCapabilities();
    } catch (e: any) {
      showToast('error', `${$t('svc.action_error')}: ${e.message}`);
    } finally {
      actionLoading[`switch-${kernel}`] = false;
      switchingKernelTo = null;
    }
  }

  async function checkKernelUpdate(name: string) {
    isKernelChecking.set(true);
    const idx = kernels.findIndex((k) => k.name === name);
    if (idx >= 0) {
      kernels[idx] = { ...kernels[idx], status: 'checking' };
      kernels = [...kernels];
    }
    try {
      const csrfToken = localStorage.getItem('csrf_token');
      const res = await fetch(`/api/kernels/${name}/check`, {
        method: 'POST',
        headers: { 'X-CSRF-Token': csrfToken || '' }
      });
      if (!res.ok) {
        throw new Error(await res.text());
      }
      startPolling(name);
    } catch (e: any) {
      showToast('error', `${$t('svc.action_error')}: ${e.message || e}`);
      const idx = kernels.findIndex((k) => k.name === name);
      if (idx >= 0) {
        kernels[idx] = { ...kernels[idx], status: 'idle' };
        kernels = [...kernels];
      }
    }
  }

  async function installKernel(name: string) {
    try {
      const csrfToken = localStorage.getItem('csrf_token');
      const res = await fetch(`/api/kernels/${name}/install`, {
        method: 'POST',
        headers: { 'X-CSRF-Token': csrfToken || '' }
      });
      if (!res.ok) {
        throw new Error(await res.text());
      }
      startPolling(name);
    } catch (e: any) {
      showToast('error', `${$t('svc.action_error')}: ${e.message || e}`);
    }
  }

  function downloadKernelBinary(name: string) {
    const a = document.createElement('a');
    a.href = `/api/kernels/${name}/download`;
    a.download = name;
    document.body.appendChild(a);
    a.click();
    document.body.removeChild(a);
  }

  async function setKernelChannel(name: string, channel: string) {
    try {
      const csrfToken = localStorage.getItem('csrf_token');
      await fetch(`/api/kernels/${name}/channel`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrfToken || '' },
        body: JSON.stringify({ channel })
      });
      await fetchKernels();
    } catch (e) {}
  }

  function checkIfFinishedChecking() {
    const isAnyChecking = Object.keys(statusIntervals).length > 0 || kernels.some((k) => k.status === 'checking');
    if (!isAnyChecking) {
      isKernelChecking.set(false);
    }
  }

  async function fetchKernelStatus(name: string) {
    try {
      const res = await fetch(`/api/kernels/${name}/status`);
      if (res.ok) {
        const envelope = await res.json();
        const data = envelope.data ?? envelope;
        const idx = kernels.findIndex((k) => k.name === name);
        if (idx >= 0) {
          kernels[idx] = { ...kernels[idx], ...data };
          kernels = [...kernels];
        }
        if (data.status === 'idle' || data.status === 'done' || data.status === 'failed') {
          clearInterval(statusIntervals[name]);
          delete statusIntervals[name];
          fetchKernels();
          checkIfFinishedChecking();
        }
      } else {
        clearInterval(statusIntervals[name]);
        delete statusIntervals[name];
        const idx = kernels.findIndex((k) => k.name === name);
        if (idx >= 0 && (kernels[idx].status === 'checking' || kernels[idx].status === 'downloading' || kernels[idx].status === 'installing')) {
          kernels[idx] = { ...kernels[idx], status: 'failed' };
          kernels = [...kernels];
        }
        checkIfFinishedChecking();
      }
    } catch (e) {
      clearInterval(statusIntervals[name]);
      delete statusIntervals[name];
      const idx = kernels.findIndex((k) => k.name === name);
      if (idx >= 0 && (kernels[idx].status === 'checking' || kernels[idx].status === 'downloading' || kernels[idx].status === 'installing')) {
        kernels[idx] = { ...kernels[idx], status: 'failed' };
        kernels = [...kernels];
      }
      checkIfFinishedChecking();
    }
  }

  function startPolling(name: string) {
    if (statusIntervals[name]) clearInterval(statusIntervals[name]);
    fetchKernelStatus(name);
    statusIntervals[name] = setInterval(() => fetchKernelStatus(name), 2000);
  }

  function getKernel(name: string) {
    return kernels.find((k) => k.name === name);
  }

  $: xray = kernels.find((k) => k.name === 'xray');
  $: mihomo = kernels.find((k) => k.name === 'mihomo');
  $: isAnyKernelChecking = kernels.some((k) => k.status === 'checking');
  $: activeKernel = (() => {
    if (xray?.process_status === 'running') return 'xray';
    if (mihomo?.process_status === 'running') return 'mihomo';
    const lastSwitch = restartLog.find(
      (entry) => entry.action.startsWith('switch_kernel:') && entry.success
    );
    if (lastSwitch) {
      return lastSwitch.action.split(':')[1];
    }
    return xkeenInfo.activeKernel || 'none';
  })();
  $: isRunning = xray?.process_status === 'running' || mihomo?.process_status === 'running';

  // Optimistic UI: при переключении/смене ядра подсвечиваем спиннером целевую кнопку
  let switchingKernelTo: string | null = null;

  onMount(() => {
    // Единый тикер: fetchKernels — источник правды для статуса процесса.
    // fetchStatus нужен только для XKeen pid/uptime/binaryPath.
    fetchKernels();
    fetchStatus();
    fetchRestartLog();
    const kernelInterval = setInterval(fetchKernels, 5000);
    const statusInterval = setInterval(fetchStatus, 15000);
    return () => {
      clearInterval(kernelInterval);
      clearInterval(statusInterval);
      Object.values(statusIntervals).forEach(clearInterval);
    };
  });
</script>

<div class="container">
  <!-- page-head -->
  <div class="page-head">
    <div>
      <div class="crumbs">
        {$t('nav.group_core')} <span class="crumb-sep">/</span>
        {$t('nav.services')}
      </div>
      <h1>{$t('svc.h1')}</h1>
      <p class="sub">{$t('svc.h1_sub')}</p>
    </div>
    <div class="ph-actions">
      <button
        class="btn btn-secondary"
        on:click={() => {
          fetchStatus();
          fetchKernels();
        }}
        title={$t('svc.refresh_status')}
      >
        <svg
          width="14"
          height="14"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          stroke-width="2"><path d="M21 12a9 9 0 1 1-3-6.7L21 8M21 3v5h-5" /></svg
        >
        {$t('svc.refresh_status')}
      </button>
      <button
        class="btn btn-primary"
        on:click={() => {
          checkKernelUpdate('xray');
          checkKernelUpdate('mihomo');
        }}
        disabled={$isKernelChecking}
        class:btn-loading={$isKernelChecking}
        title={$t('svc.check_updates')}
      >
        <svg
          width="14"
          height="14"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          stroke-width="2"><polyline points="5 12 10 17 20 7" /></svg
        >
        {$t('svc.check_updates')}
      </button>
    </div>
  </div>

  <!-- XKeen main module card -->
  <div class="card" style="margin-bottom:18px;padding:0;overflow:hidden;">
    <h2 class="card-title" style="margin:0;padding:14px 20px;">{$t('svc.section_xkeen')}</h2>
    <div class="kernel-card" style="border:0;border-radius:0;">
      <div class="k-ico">
        <svg width="22" height="22" viewBox="0 0 24 24" fill="currentColor"
          ><path d="M13 2 L4 14 L11 14 L10 22 L20 9 L13 9 Z" /></svg
        >
      </div>
      <div class="k-body">
        <div class="k-name">
          XKeen
          {#if isRunning}
            <span class="status-badge running"
              ><span class="status-dot success" style="margin:0;"></span>{$t(
                'kernel.status.running'
              )}</span
            >
          {:else}
            <span class="status-badge stopped"
              ><span class="status-dot error" style="margin:0;"></span>{$t(
                'kernel.status.stopped'
              )}</span
            >
          {/if}
        </div>
        <div class="k-meta">
          {#if isRunning}
            {#if xkeenInfo.pid}
              PID {xkeenInfo.pid} · uptime {xkeenInfo.uptime || '—'} · {xkeenInfo.binaryPath ||
                '/opt/sbin/xkeen'}
            {:else if xkeenStatus}
              {xkeenStatus}
            {:else}
              {$t('svc.xkeen_module')}{#if activeKernel !== 'none'}
                · {$t('svc.active_kernel')}: {activeKernel}{/if}
            {/if}
          {:else}
            {$t('svc.xkeen_module')}{#if xkeenInfo.binaryPath}
              · {xkeenInfo.binaryPath}{/if}
          {/if}
        </div>
      </div>
      <div class="k-actions">
        {#if isRunning}
          <button
            class="btn btn-secondary"
            on:click={() => controlService('stop')}
            disabled={actionLoading['xkeen-stop']}
            title={$t('app.stop')}
          >
            <svg
              width="13"
              height="13"
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              stroke-width="2"
              ><rect x="6" y="5" width="4" height="14" rx="1" /><rect
                x="14"
                y="5"
                width="4"
                height="14"
                rx="1"
              /></svg
            >
            {$t('app.stop')}
          </button>
          <button
            class="btn btn-secondary"
            on:click={() => controlService('restart')}
            disabled={actionLoading['xkeen-restart']}
            title={$t('svc.restart')}
          >
            <svg
              width="13"
              height="13"
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              stroke-width="2"><path d="M21 12a9 9 0 1 1-3-6.7L21 8M21 3v5h-5" /></svg
            >
            {$t('svc.restart')}
          </button>
        {:else}
          <button
            class="btn btn-primary"
            on:click={() => controlService('start')}
            disabled={actionLoading['xkeen-start']}
            title={$t('app.start')}
          >
            <svg width="13" height="13" viewBox="0 0 24 24" fill="currentColor"
              ><polygon points="5 3 19 12 5 21 5 3" /></svg
            >
            {$t('app.start')}
          </button>
        {/if}
      </div>
    </div>
  </div>

  <!-- Proxy kernels card -->
  <div class="card" style="margin-bottom:18px;padding:0;overflow:hidden;">
    <h2
      class="card-title"
      style="margin:0;padding:14px 20px;display:flex;align-items:center;justify-content:space-between;"
    >
      <span>{$t('svc.section_kernels')}</span>
      <div style="display:flex;align-items:center;gap:12px;">
        <!-- Pill-переключатель активного ядра -->
        {#if kernelsLoaded && (xray || mihomo)}
          <div class="kernel-switcher" aria-label={$t('svc.active_kernel_label')} role="group">
            {#if xray}
              <button
                class="ks-btn"
                class:ks-active={activeKernel === 'xray'}
                class:ks-switching={switchingKernelTo === 'xray'}
                disabled={switchingKernelTo !== null || activeKernel === 'xray'}
                on:click={() => switchKernel('xray')}
                title={activeKernel === 'xray'
                  ? $t('svc.active_label')
                  : $t('svc.make_active') + ' Xray'}
              >
                {#if switchingKernelTo === 'xray'}
                  <span class="ks-dot ks-dot-spin"></span>
                {:else if activeKernel === 'xray'}
                  <span class="ks-dot ks-dot-running"></span>
                {:else}
                  <span class="ks-dot ks-dot-idle"></span>
                {/if}
                Xray
              </button>
            {/if}
            {#if mihomo}
              <button
                class="ks-btn"
                class:ks-active={activeKernel === 'mihomo'}
                class:ks-switching={switchingKernelTo === 'mihomo'}
                disabled={switchingKernelTo !== null || activeKernel === 'mihomo'}
                on:click={() => switchKernel('mihomo')}
                title={activeKernel === 'mihomo'
                  ? $t('svc.active_label')
                  : $t('svc.make_active') + ' Mihomo'}
              >
                {#if switchingKernelTo === 'mihomo'}
                  <span class="ks-dot ks-dot-spin"></span>
                {:else if activeKernel === 'mihomo'}
                  <span class="ks-dot ks-dot-running"></span>
                {:else}
                  <span class="ks-dot ks-dot-idle"></span>
                {/if}
                Mihomo
              </button>
            {/if}
          </div>
        {/if}
        <span
          style="font-size:11px;color:var(--fg-dim);letter-spacing:.04em;font-weight:500;text-transform:none;display:flex;align-items:center;gap:6px;"
        >
          {$t('svc.channel_prefix')} ·
          <select
            value={xray?.channel || mihomo?.channel || 'stable'}
            on:change={(e) => {
              const ch = e.currentTarget.value;
              setKernelChannel('xray', ch);
              setKernelChannel('mihomo', ch);
            }}
            style="background:transparent;border:0;color:var(--accent);font-size:11px;font-weight:600;padding:0;cursor:pointer;outline:none;"
          >
            <option value="stable" style="background:var(--bg-card);color:var(--fg-primary);"
              >{$t('svc.channel_stable').toLowerCase()}</option
            >
            <option value="preview" style="background:var(--bg-card);color:var(--fg-primary);"
              >{$t('svc.channel_preview').toLowerCase()}</option
            >
          </select>
        </span>
      </div>
    </h2>

    <!-- Xray row -->
    <div
      class="kernel-card"
      style="border-radius:0;border-top:0;border-left:0;border-right:0;border-bottom:1px solid var(--border);"
    >
      <div class="k-ico">
        <svg
          width="22"
          height="22"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          stroke-width="2"><circle cx="12" cy="12" r="9" /><path d="M3 12h18" /></svg
        >
      </div>
      <div class="k-body">
        <div class="k-name">
          Xray
          {#if !kernelsLoaded}
            <Skeleton type="text-line" width="60px" />
          {:else if xray}
            {#if xray.process_status === 'running'}
              <span class="status-badge running"
                ><span class="status-dot success" style="margin:0;"></span>{$t(
                  'kernel.status.running'
                )}</span
              >
            {:else}
              <span class="status-badge stopped"
                ><span class="status-dot error" style="margin:0;"></span>{$t(
                  'kernel.status.stopped'
                )}</span
              >
            {/if}
            {#if xray.status === 'checking'}
              <span class="badge badge-info">{$t('kernels.checking')}</span>
            {:else}
              {#if xray.has_update}
                <span class="badge badge-warning">{$t('svc.update_badge')} {xray.latest_version}</span
                >
              {:else if xray.current_version && xray.current_version !== 'not installed'}
                <span class="badge">v{xray.current_version} · {$t('svc.actual_badge')}</span>
              {/if}
            {/if}
          {:else}
            <span class="status-badge stopped"
              ><span class="status-dot error" style="margin:0;"></span>{$t(
                'kernel.status.not_installed'
              )}</span
            >
          {/if}
        </div>
        <div class="k-meta">
          {#if !kernelsLoaded}
            <Skeleton type="text-line" width="120px" />
          {:else if xray}
            {#if xray.process_status === 'running'}
              PID {xray.pid || '—'} · uptime {xray.uptime || '—'} · {xray.binary_path}
            {:else}
              {xray.binary_path}
              {#if xray.message}
                · {xray.message}{/if}
            {/if}
          {:else}
            {$t('kernel.status.not_installed')}
          {/if}
        </div>
      </div>
      <div class="k-actions">
        {#if !kernelsLoaded}
          <Skeleton type="text-line" width="80px" />
        {:else if xray}
          {#if xray.process_status === 'running'}
            <button
              class="btn btn-secondary"
              on:click={() => controlService('stop')}
              disabled={actionLoading['xkeen-stop']}
              title={$t('app.stop')}
            >
              <svg
                width="13"
                height="13"
                viewBox="0 0 24 24"
                fill="none"
                stroke="currentColor"
                stroke-width="2"
                ><rect x="6" y="5" width="4" height="14" rx="1" /><rect
                  x="14"
                  y="5"
                  width="4"
                  height="14"
                  rx="1"
                /></svg
              >
              {$t('app.stop')}
            </button>
            <button
              class="btn btn-secondary"
              on:click={() => controlService('restart')}
              disabled={actionLoading['xkeen-restart']}
              title={$t('svc.restart')}
            >
              <svg
                width="13"
                height="13"
                viewBox="0 0 24 24"
                fill="none"
                stroke="currentColor"
                stroke-width="2"><path d="M21 12a9 9 0 1 1-3-6.7L21 8M21 3v5h-5" /></svg
              >
              {$t('svc.restart')}
            </button>
          {/if}
          {#if xray.has_update}
            <button
              class="btn btn-secondary"
              on:click={() => installKernel('xray')}
              disabled={xray.status !== 'idle'}
              title={$t('svc.install_update')}
            >
              {xray.status === 'downloading' || xray.status === 'installing'
                ? $t('kernels.installing')
                : $t('svc.install_update')}
            </button>
            <button
              class="btn btn-secondary"
              on:click={() => downloadKernelBinary('xray')}
              disabled={xray.status === 'downloading' || xray.status === 'installing'}
              title={$t('svc.download')}
            >
              {$t('svc.download')}
            </button>
          {/if}
          <button
            class="btn btn-secondary"
            on:click={() => onSwitchTab('logs')}
            title={$t('svc.logs')}
          >
            {$t('svc.logs')}
          </button>
        {:else}
          <button
            class="btn btn-primary"
            on:click={() => installKernel('xray')}
            title={$t('svc.install_update')}
          >
            {$t('svc.install_update')}
          </button>
        {/if}
      </div>
    </div>

    <!-- Mihomo row -->
    <div class="kernel-card" style="border-radius:0;border:0;">
      <div class="k-ico">
        <svg
          width="22"
          height="22"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          stroke-width="2"
          ><circle cx="12" cy="12" r="9" /><path
            d="M12 3a14 14 0 0 1 0 18M12 3a14 14 0 0 0 0 18"
          /></svg
        >
      </div>
      <div class="k-body">
        <div class="k-name">
          Mihomo
          {#if !kernelsLoaded}
            <Skeleton type="text-line" width="60px" />
          {:else if mihomo}
            {#if mihomo.process_status === 'running'}
              <span class="status-badge running"
                ><span class="status-dot success" style="margin:0;"></span>{$t(
                  'kernel.status.running'
                )}</span
              >
            {:else}
              <span class="status-badge stopped"
                ><span class="status-dot error" style="margin:0;"></span>{$t(
                  'kernel.status.stopped'
                )}</span
              >
            {/if}
            {#if mihomo.status === 'checking'}
              <span class="badge badge-info">{$t('kernels.checking')}</span>
            {:else}
              {#if mihomo.has_update}
                <span class="badge badge-warning"
                  >{$t('svc.update_badge')} {mihomo.latest_version}</span
                >
              {:else if mihomo.current_version && mihomo.current_version !== 'not installed'}
                <span class="badge">v{mihomo.current_version} · {$t('svc.actual_badge')}</span>
              {/if}
            {/if}
          {:else}
            <span class="status-badge stopped"
              ><span class="status-dot error" style="margin:0;"></span>{$t(
                'kernel.status.not_installed'
              )}</span
            >
          {/if}
        </div>
        <div class="k-meta">
          {#if !kernelsLoaded}
            <Skeleton type="text-line" width="120px" />
          {:else if mihomo}
            {#if mihomo.process_status === 'running'}
              API {mihomo.api_addr || '127.0.0.1:9090'} · uptime {mihomo.uptime || '—'} · {mihomo.binary_path}
            {:else}
              {mihomo.binary_path}
              {#if mihomo.message}
                · {mihomo.message}{/if}
            {/if}
          {:else}
            {$t('kernel.status.not_installed')}
          {/if}
        </div>
      </div>
      <div class="k-actions">
        {#if !kernelsLoaded}
          <Skeleton type="text-line" width="80px" />
        {:else if mihomo}
          {#if mihomo.process_status === 'running'}
            <button
              class="btn btn-secondary"
              on:click={() => controlService('stop')}
              disabled={actionLoading['xkeen-stop']}
              title={$t('app.stop')}
            >
              <svg
                width="13"
                height="13"
                viewBox="0 0 24 24"
                fill="none"
                stroke="currentColor"
                stroke-width="2"
                ><rect x="6" y="5" width="4" height="14" rx="1" /><rect
                  x="14"
                  y="5"
                  width="4"
                  height="14"
                  rx="1"
                /></svg
              >
              {$t('app.stop')}
            </button>
            <button
              class="btn btn-secondary"
              on:click={() => controlService('restart')}
              disabled={actionLoading['xkeen-restart']}
              title={$t('svc.restart')}
            >
              <svg
                width="13"
                height="13"
                viewBox="0 0 24 24"
                fill="none"
                stroke="currentColor"
                stroke-width="2"><path d="M21 12a9 9 0 1 1-3-6.7L21 8M21 3v5h-5" /></svg
              >
              {$t('svc.restart')}
            </button>
            <button
              class="btn btn-secondary"
              on:click={() => onSwitchTab('proxies')}
              title={$t('svc.api_test')}
            >
              {$t('svc.api_test')}
            </button>
          {/if}
          {#if mihomo.has_update}
            <button
              class="btn btn-secondary"
              on:click={() => installKernel('mihomo')}
              disabled={mihomo.status !== 'idle'}
              title={$t('svc.install_update')}
            >
              {mihomo.status === 'downloading' || mihomo.status === 'installing'
                ? $t('kernels.installing')
                : $t('svc.install_update')}
            </button>
            <button
              class="btn btn-secondary"
              on:click={() => downloadKernelBinary('mihomo')}
              disabled={mihomo.status === 'downloading' || mihomo.status === 'installing'}
              title={$t('svc.download')}
            >
              {$t('svc.download')}
            </button>
          {/if}
        {:else}
          <button
            class="btn btn-primary"
            on:click={() => installKernel('mihomo')}
            title={$t('svc.install_update')}
          >
            {$t('svc.install_update')}
          </button>
        {/if}
      </div>
    </div>
  </div>

  <!-- Auto-start card -->
  <div class="card" style="margin-bottom:18px;">
    <h2 class="card-title">{$t('svc.section_autostart')}</h2>
    <div class="field-row" style="border-bottom:1px solid var(--border-light);">
      <div>
        <div class="lbl">{$t('svc.autostart_keenetic_label')}</div>
        <div class="desc">{$t('svc.autostart_keenetic_desc')}</div>
      </div>
      <div class="ctrl">
        <label class="toggle-switch" title={$t('svc.autostart_keenetic_label')}>
          <input
            type="checkbox"
            bind:checked={autostartKeenetic}
            on:change={() => toggleAutostart('autostart_keenetic', autostartKeenetic)}
          />
          <span class="toggle-slider"></span>
        </label>
      </div>
    </div>
    <div class="field-row" style="border-bottom:1px solid var(--border-light);">
      <div>
        <div class="lbl">{$t('svc.watchdog_label')}</div>
        <div class="desc">{$t('svc.watchdog_desc')}</div>
      </div>
      <div class="ctrl">
        <label class="toggle-switch" title={$t('svc.watchdog_label')}>
          <input
            type="checkbox"
            bind:checked={watchdogEnabled}
            on:change={() => toggleAutostart('watchdog_enabled', watchdogEnabled)}
          />
          <span class="toggle-slider"></span>
        </label>
      </div>
    </div>
    <div class="field-row">
      <div>
        <div class="lbl">{$t('svc.dat_update_label')}</div>
        <div class="desc">{$t('svc.dat_update_desc')}</div>
      </div>
      <div class="ctrl">
        <label class="toggle-switch" title={$t('svc.dat_update_label')}>
          <input
            type="checkbox"
            bind:checked={datUpdateDaily}
            on:change={() => toggleAutostart('dat_update_daily', datUpdateDaily)}
          />
          <span class="toggle-slider"></span>
        </label>
      </div>
    </div>
  </div>

  <!-- Restart log card -->
  {#if restartLog.length > 0}
    <div class="card">
      <h2 class="card-title">
        {$t('svc.restart_log_title')}
        <span class="ct-actions">
          <button
            on:click={() => (restartLogExpanded = !restartLogExpanded)}
          >
            {restartLogExpanded ? $t('svc.log_collapse') : $t('svc.log_expand')}
          </button>
        </span>
      </h2>
      <div class="restart-log">
        {#each restartLogExpanded ? restartLog : restartLog.slice(0, 5) as entry}
          <div class="log-entry" class:log-success={entry.success} class:log-fail={!entry.success}>
            <div class="log-meta">
              <span class="log-action">{formatAction(entry.action)}</span>
              <span
                class="log-badge"
                class:badge-ok={entry.success}
                class:badge-err={!entry.success}
              >
                {entry.success ? $t('svc.log_ok') : $t('svc.log_fail')}
              </span>
              <span class="log-ts">{formatTs(entry.timestamp)}</span>
            </div>
            {#if entry.output}
              <pre class="log-output">{entry.output}</pre>
            {/if}
          </div>
        {/each}
        {#if !restartLogExpanded && restartLog.length > 5}
          <div class="log-more" on:click={() => (restartLogExpanded = true)}>
            {pluralize(
              restartLog.length - 5,
              $t('svc.log_show_more_one', { count: String(restartLog.length - 5) }),
              $t('svc.log_show_more_few', { count: String(restartLog.length - 5) }),
              $t('svc.log_show_more_many', { count: String(restartLog.length - 5) }),
              $currentLang
            )}
          </div>
        {/if}
      </div>
    </div>
  {/if}
</div>

<style>
  .page-head {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    margin-bottom: 24px;
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

  .ph-actions {
    display: flex;
    gap: 10px;
    align-items: center;
    flex-shrink: 0;
    padding-top: 6px;
  }

  /* kernel card layout from preview.html */
  .kernel-card {
    background: var(--bg-card);
    border: 1px solid var(--border);
    border-radius: var(--radius-lg);
    overflow: hidden;
    display: grid;
    grid-template-columns: 60px 1fr auto;
    align-items: center;
  }

  /* Pill-переключатель активного ядра */
  .kernel-switcher {
    display: flex;
    border: 1px solid var(--border);
    border-radius: 20px;
    overflow: hidden;
    background: var(--bg-secondary);
    flex-shrink: 0;
  }
  .ks-btn {
    display: flex;
    align-items: center;
    gap: 5px;
    padding: 4px 12px;
    font-size: 12px;
    font-weight: 500;
    color: var(--fg-secondary);
    background: transparent;
    border: none;
    cursor: pointer;
    transition:
      background 0.15s,
      color 0.15s;
    line-height: 1.5;
  }
  .ks-btn:hover:not(:disabled):not(.ks-active) {
    background: var(--bg-hover);
    color: var(--fg-primary);
  }
  .ks-btn.ks-active {
    background: var(--accent);
    color: #fff;
    cursor: default;
  }
  .ks-btn:disabled:not(.ks-active) {
    opacity: 0.5;
    cursor: not-allowed;
  }
  .ks-btn.ks-switching {
    background: rgba(41, 194, 240, 0.2);
    color: var(--accent);
    cursor: wait;
  }
  .ks-dot {
    width: 6px;
    height: 6px;
    border-radius: 50%;
    flex-shrink: 0;
  }
  .ks-dot-running {
    background: #4ade80;
    box-shadow: 0 0 0 2px rgba(74, 222, 128, 0.25);
  }
  .ks-dot-idle {
    background: var(--fg-faint, rgba(255, 255, 255, 0.15));
  }
  .ks-dot-spin {
    background: var(--accent);
    animation: ks-pulse 1s ease-in-out infinite;
  }
  @keyframes ks-pulse {
    0%,
    100% {
      opacity: 1;
    }
    50% {
      opacity: 0.4;
    }
  }
  .kernel-card .k-ico {
    width: 60px;
    height: 100%;
    display: grid;
    place-items: center;
    background: linear-gradient(180deg, rgba(41, 194, 240, 0.06), transparent);
    border-right: 1px solid var(--border);
    color: var(--accent);
    align-self: stretch; /* ensure full height to keep border/gradient running top-to-bottom */
  }
  .kernel-card .k-body {
    padding: 16px 20px;
  }
  .kernel-card .k-name {
    font-weight: 700;
    color: var(--fg-primary);
    font-size: 14px;
    display: flex;
    align-items: center;
    gap: 10px;
  }
  .kernel-card .k-meta {
    color: var(--fg-dim);
    font-size: 12px;
    margin-top: 4px;
    font-family: var(--font-family-mono);
  }
  .kernel-card .k-actions {
    padding: 0 18px;
    display: flex;
    gap: 8px;
  }

  /* field-row for autostart */
  .field-row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 14px 20px;
  }
  .field-row .lbl {
    font-size: 14px;
    font-weight: 500;
    color: var(--fg-primary);
  }
  .field-row .desc {
    font-size: 12px;
    color: var(--fg-dim);
    margin-top: 2px;
  }
  .field-row .ctrl {
    display: flex;
    align-items: center;
  }
  .card-title-row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    margin-bottom: 12px;
  }
  .card-title-row .card-title {
    margin-bottom: 0;
  }
  .btn-sm {
    padding: 4px 10px;
    font-size: 12px;
  }
  .btn-ghost {
    background: transparent;
    border: 1px solid var(--border);
    color: var(--fg-secondary);
  }
  .btn-ghost:hover {
    background: var(--bg-hover);
  }
  .restart-log {
    display: flex;
    flex-direction: column;
    gap: 6px;
  }
  .log-entry {
    border-radius: 6px;
    border-left: 3px solid var(--border);
    padding: 8px 10px;
    background: var(--bg-secondary);
  }
  .log-entry.log-success {
    border-left-color: var(--success, #22c55e);
  }
  .log-entry.log-fail {
    border-left-color: var(--danger, #ef4444);
  }
  .log-meta {
    display: flex;
    align-items: center;
    gap: 8px;
    flex-wrap: wrap;
  }
  .log-action {
    font-weight: 600;
    font-size: 13px;
  }
  .log-ts {
    color: var(--fg-secondary);
    font-size: 12px;
    margin-left: auto;
  }
  .log-badge {
    font-size: 11px;
    padding: 1px 6px;
    border-radius: 4px;
    font-weight: 600;
  }
  .badge-ok {
    background: rgba(34, 197, 94, 0.15);
    color: #22c55e;
  }
  .badge-err {
    background: rgba(239, 68, 68, 0.15);
    color: #ef4444;
  }
  .log-output {
    margin: 6px 0 0;
    font-size: 11px;
    color: var(--fg-secondary);
    white-space: pre-wrap;
    word-break: break-all;
    max-height: 120px;
    overflow-y: auto;
  }
  .log-more {
    text-align: center;
    font-size: 12px;
    color: var(--accent);
    cursor: pointer;
    padding: 4px;
  }
  .log-more:hover {
    text-decoration: underline;
  }

  @media (max-width: 768px) {
    .kernel-card {
      grid-template-columns: 60px 1fr;
      grid-template-rows: auto auto;
    }

    .kernel-card .k-actions {
      grid-column: 1 / span 2;
      border-top: 1px solid var(--border);
      background: var(--bg-secondary);
      padding: 12px 18px;
      display: flex;
      flex-wrap: wrap;
      gap: 8px;
    }

    .kernel-card .k-name {
      flex-wrap: wrap;
    }
  }
</style>
