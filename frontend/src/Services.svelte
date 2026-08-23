<script lang="ts">
  import { onMount } from 'svelte';
  import { t, currentLang, pluralize } from './i18n';
  import {
    showToast,
    fetchCapabilities,
    showConfirm,
    isKernelChecking,
    capabilities
  } from './stores';
  import { usePoller } from './lib/poller';
  import Skeleton from './components/Skeleton.svelte';
  import { apiFetch } from './lib/api';
  import { activateRestartGrace } from './lib/serviceGrace';
  import MihomoSocketMigrateModal from './components/mihomo/MihomoSocketMigrateModal.svelte';

  let { onSwitchTab = () => {} }: { onSwitchTab?: (tab: string) => void } = $props();

  let showMihomoMigrateModal = $state(false);

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

  let xkeenInfo = $state<XKeenStatusInfo>({
    isRunning: false,
    activeKernel: '',
    pid: 0,
    uptime: '',
    binaryPath: '',
    raw: ''
  });

  let xkeenStatus = $state('');
  let actionLoading = $state<Record<string, boolean>>({});

  let kernels = $state<Kernel[]>([]);
  let kernelsLoaded = $state(false);
  const statusIntervals: Record<string, ReturnType<typeof setInterval>> = {};

  // Restart log
  interface RestartLogEntry {
    timestamp: number;
    action: string;
    success: boolean;
    exit_code: number;
    output: string;
  }
  let restartLog = $state<RestartLogEntry[]>([]);
  let restartLogExpanded = $state(false);

  async function fetchRestartLog() {
    try {
      const res = await apiFetch('/api/service/restart-log');
      if (res.ok) {
        const json = await res.json();
        restartLog = Array.isArray(json) ? json : Array.isArray(json?.data) ? json.data : [];
      }
    } catch (e: any) {
      if (e?.status === 401) return;
    }
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

  let refreshingStatus = $state(false);

  async function handleRefreshStatus() {
    if (refreshingStatus) return;
    refreshingStatus = true;
    try {
      await fetchStatus();
      await fetchKernels();
      await fetchRestartLog();
    } finally {
      refreshingStatus = false;
    }
  }

  async function fetchStatus(signal?: AbortSignal) {
    try {
      const res = await apiFetch('/api/service/status', { signal });
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
            if (
              /[\u043D][\u0435]\s*[\u0437][\u0430][\u043F][\u0443][\u0449][\u0435][\u043D]/.test(
                lower
              ) ||
              lower.includes('not running')
            ) {
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
    } catch (e: any) {
      if (e?.name === 'AbortError') return;
      if (e?.status === 401) return;
      xkeenStatus = $t('app.unavailable');
      xkeenInfo = {
        isRunning: false,
        activeKernel: '',
        pid: 0,
        uptime: '',
        binaryPath: '',
        raw: $t('app.unavailable')
      };
      throw e;
    }
  }

  function parseRawText(text: string) {
    const lower = text.toLowerCase();
    const isRunning =
      lower.includes('running') ||
      /[\u0437][\u0430][\u043F][\u0443][\u0449][\u0435][\u043D]/.test(lower);
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
    if (
      /[\u043D][\u0435]\s*[\u0437][\u0430][\u043F][\u0443][\u0449][\u0435][\u043D]/.test(lower) ||
      lower.includes('not running')
    ) {
      xkeenStatus = $t('svc.kernel_not_selected');
    } else {
      xkeenStatus = text;
    }
  }

  async function fetchKernels(signal?: AbortSignal) {
    try {
      const res = await apiFetch('/api/kernels', { signal });
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
    } catch (e: any) {
      if (e?.name === 'AbortError') return;
      if (e?.status === 401) return;
      throw e;
    } finally {
      kernelsLoaded = true;
    }
  }

  async function controlService(action: string) {
    isKernelChecking.set(false);
    const key = `xkeen-${action}`;
    actionLoading[key] = true;
    try {
      if (action === 'start') {
        // Pre-flight check: determine kernel to validate
        const kernel =
          activeKernel === 'xray' || activeKernel === 'mihomo'
            ? activeKernel
            : xray?.process_status === 'running'
              ? 'xray'
              : mihomo?.process_status === 'running'
                ? 'mihomo'
                : 'xray';
        try {
          const pfRes = await apiFetch(`/api/config/preflight?kernel=${kernel}`, {
            signal: AbortSignal.timeout(3000)
          });
          if (pfRes.ok) {
            const pfBody = await pfRes.json();
            const data = pfBody.data ?? pfBody;
            if (Array.isArray(data.errors) && data.errors.length > 0) {
              const errList = data.errors
                .map((e: { message?: string; code?: string }) => e.message || e.code || '')
                .join('\n• ');
              const message = `${$t('svc.preflight_error_body')}\n• ${errList}`;
              const proceed = await showConfirm(
                $t('svc.preflight_error_title'),
                message,
                $t('svc.preflight_start_anyway'),
                $t('svc.preflight_fix')
              );
              if (!proceed) {
                window.location.hash = '/editor';
                actionLoading[key] = false;
                return;
              }
            }
          }
        } catch (_) {
          // Network/timeout error — fall through to silent start
        }
      }
      if (action === 'restart' || action === 'switch_kernel' || action === 'start') {
        activateRestartGrace(6000);
      }
      const res = await apiFetch(`/api/service/control?action=${action}`, {
        method: 'POST'
      });
      const text = await res.text();
      if (!res.ok) throw new Error(text);
      await fetchStatus();
      await fetchKernels();
      await fetchCapabilities();
      fetchRestartLog();
    } catch (e: any) {
      if (e?.status === 401) return;
      showToast('error', `${$t('svc.action_error')}: ${e.message}`);
      fetchRestartLog();
    } finally {
      actionLoading[key] = false;
    }
  }

  async function handleSwitchKernel(target: string) {
    if (target === activeKernel || switchingKernelTo !== null) return;
    const confirmed = await showConfirm(
      $t('svc.switch_confirm_title'),
      $t('svc.switch_confirm_msg', { from: activeKernel.toUpperCase(), to: target.toUpperCase() }),
      $t('svc.make_active'),
      $t('app.cancel')
    );
    if (!confirmed) return;
    await switchKernel(target);
  }

  async function switchKernel(kernel: string) {
    isKernelChecking.set(false);
    switchingKernelTo = kernel;
    actionLoading[`switch-${kernel}`] = true;
    activateRestartGrace(6000);
    try {
      const res = await apiFetch(`/api/service/control?action=switch_kernel&kernel=${kernel}`, {
        method: 'POST'
      });
      const text = await res.text();
      if (!res.ok) throw new Error(text);
      await fetchStatus();
      await fetchKernels();
      await fetchCapabilities();
      fetchRestartLog();
    } catch (e: any) {
      if (e?.status === 401) return;
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
      const res = await apiFetch(`/api/kernels/${name}/check`, {
        method: 'POST'
      });
      if (!res.ok) {
        throw new Error(await res.text());
      }
      startPolling(name);
    } catch (e: any) {
      if (e?.status === 401) return;
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
      const res = await apiFetch(`/api/kernels/${name}/install`, {
        method: 'POST'
      });
      if (!res.ok) {
        throw new Error(await res.text());
      }
      startPolling(name);
    } catch (e: any) {
      if (e?.status === 401) return;
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
      await apiFetch(`/api/kernels/${name}/channel`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ channel })
      });
      await fetchKernels();
    } catch (e: any) {
      if (e?.status === 401) return;
    }
  }

  function checkIfFinishedChecking() {
    const isAnyChecking =
      Object.keys(statusIntervals).length > 0 || kernels.some((k) => k.status === 'checking');
    if (!isAnyChecking) {
      isKernelChecking.set(false);
    }
  }

  async function fetchKernelStatus(name: string) {
    try {
      const res = await apiFetch(`/api/kernels/${name}/status`);
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
        if (
          idx >= 0 &&
          kernels[idx].status !== 'idle' &&
          kernels[idx].status !== 'done' &&
          kernels[idx].status !== 'failed'
        ) {
          kernels[idx] = { ...kernels[idx], status: 'failed' };
          kernels = [...kernels];
        }
        checkIfFinishedChecking();
      }
    } catch (e: any) {
      if (e?.status === 401) return;
      clearInterval(statusIntervals[name]);
      delete statusIntervals[name];
      const idx = kernels.findIndex((k) => k.name === name);
      if (idx >= 0) {
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

  let xray = $derived(Array.isArray(kernels) ? kernels.find((k) => k.name === 'xray') : undefined);
  let mihomo = $derived(
    Array.isArray(kernels) ? kernels.find((k) => k.name === 'mihomo') : undefined
  );
  let isAnyKernelChecking = $derived(
    Array.isArray(kernels) ? kernels.some((k) => k.status === 'checking') : false
  );
  let activeKernel = $derived.by(() => {
    if (xray?.process_status === 'running') return 'xray';
    if (mihomo?.process_status === 'running') return 'mihomo';
    const lastSwitch = Array.isArray(restartLog)
      ? restartLog.find((entry) => entry.action.startsWith('switch_kernel:') && entry.success)
      : undefined;
    if (lastSwitch) {
      return lastSwitch.action.split(':')[1];
    }
    return xkeenInfo.activeKernel || 'none';
  });

  let activeKernelObj = $derived(
    activeKernel === 'xray' ? xray : activeKernel === 'mihomo' ? mihomo : undefined
  );

  let isRunning = $derived(
    xray?.process_status === 'running' ||
      mihomo?.process_status === 'running' ||
      xkeenInfo.isRunning
  );

  let switchingKernelTo = $state<string | null>(null);

  onMount(() => {
    fetchRestartLog();
    const kernelPoller = usePoller((signal) => fetchKernels(signal), 5000);
    const statusPoller = usePoller((signal) => fetchStatus(signal), 15000);
    return () => {
      kernelPoller.stop();
      statusPoller.stop();
      Object.values(statusIntervals).forEach(clearInterval);
    };
  });
</script>

<div class="container">
  <!-- page-head -->
  <div class="page-head">
    <div>
      <div class="crumbs">
        {$t('nav.group_system')} <span class="crumb-sep">›</span>
        {$t('nav.services')}
      </div>
      <h1>{$t('svc.h1')}</h1>
      <p class="sub">{$t('svc.h1_sub')}</p>
    </div>
    <div class="ph-actions">
      <button
        class="btn btn-secondary"
        onclick={handleRefreshStatus}
        disabled={$isKernelChecking || refreshingStatus}
        class:btn-loading={refreshingStatus}
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
        onclick={() => {
          checkKernelUpdate('xray');
          checkKernelUpdate('mihomo');
        }}
        disabled={$isKernelChecking || isAnyKernelChecking}
        class:btn-loading={$isKernelChecking || isAnyKernelChecking}
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

  <!-- Top 2-Section Grid (Hero 65% / Updates 35%) -->
  <div class="services-top-grid">
    <!-- HERO: Active Core & XKeen Process Card (SRV-01, SRV-02, SRV-05) -->
    <div class="card hero-card">
      <div class="hero-header">
        <div class="hero-title-group">
          <div class="hero-badge-wrap">
            <span class="hero-icon">
              <svg width="22" height="22" viewBox="0 0 24 24" fill="currentColor"
                ><path d="M13 2 L4 14 L11 14 L10 22 L20 9 L13 9 Z" /></svg
              >
            </span>
            <div>
              <h2 class="hero-title">{$t('svc.hero_title')}</h2>
              <p class="hero-subtitle">{$t('svc.hero_desc')}</p>
            </div>
          </div>
        </div>
        <div class="hero-status">
          {#if isRunning}
            <span class="status-badge running">
              <span class="status-dot success"></span>{$t('svc.running')}
            </span>
          {:else}
            <span class="status-badge stopped">
              <span class="status-dot error"></span>{$t('svc.stopped')}
            </span>
          {/if}
        </div>
      </div>

      <!-- Mutual Exclusive Radio Selector (SRV-02) -->
      <div class="core-radio-grid" role="radiogroup" aria-label={$t('svc.active_kernel_label')}>
        <!-- Mihomo Option -->
        <div
          role="radio"
          aria-checked={activeKernel === 'mihomo'}
          tabindex="0"
          class="core-radio-card kernel-card"
          class:active={activeKernel === 'mihomo'}
          class:switching={switchingKernelTo === 'mihomo'}
          onclick={() => handleSwitchKernel('mihomo')}
          onkeydown={(e) => {
            if (e.key === 'Enter' || e.key === ' ') handleSwitchKernel('mihomo');
          }}
        >
          <div class="radio-indicator">
            <span class="radio-dot" class:checked={activeKernel === 'mihomo'}></span>
          </div>
          <div class="radio-body k-body">
            <div class="radio-name">
              <span>Mihomo</span>
              {#if mihomo?.current_version}
                <span class="k-ver text-secondary" style="font-size:12px; font-weight:normal;"
                  >v{mihomo.current_version}</span
                >
              {/if}
              {#if activeKernel === 'mihomo'}
                <span class="active-pill">{$t('svc.active_label')}</span>
              {/if}
            </div>
            <div class="radio-desc k-meta">
              {#if !kernelsLoaded}
                <Skeleton type="text-line" width="90px" />
              {:else}
                {mihomo?.process_status === 'running'
                  ? `${$t('svc.running')} · PID ${mihomo?.pid || xkeenInfo.pid || '—'}`
                  : $t('svc.stopped')}
              {/if}
            </div>
            {#if ($capabilities?.mihomo?.process_running || mihomo?.process_status === 'running') && $capabilities?.mihomo?.reachable && !$capabilities?.mihomo?.api_reachable}
              <a
                href="#/editor"
                class="badge badge-warning"
                style="margin-top: 6px; display: inline-flex;"
                title={$t('svc.mihomo_api_unavailable_title')}
                onclick={(e) => e.stopPropagation()}
              >
                {$t('svc.mihomo_api_unavailable')}
              </a>
            {/if}
          </div>
          {#if !isRunning}
            <button
              type="button"
              class="btn btn-primary btn-sm"
              onclick={(e) => {
                e.stopPropagation();
                controlService('start');
              }}
              title={$t('svc.action_start')}
            >
              <svg width="12" height="12" viewBox="0 0 24 24" fill="currentColor">
                <polygon points="5 3 19 12 5 21 5 3" />
              </svg>
              {$t('svc.action_start')}
            </button>
          {/if}
        </div>

        <!-- Xray Option -->
        <div
          role="radio"
          aria-checked={activeKernel === 'xray'}
          tabindex="0"
          class="core-radio-card kernel-card"
          class:active={activeKernel === 'xray'}
          class:switching={switchingKernelTo === 'xray'}
          onclick={() => handleSwitchKernel('xray')}
          onkeydown={(e) => {
            if (e.key === 'Enter' || e.key === ' ') handleSwitchKernel('xray');
          }}
        >
          <div class="radio-indicator">
            <span class="radio-dot" class:checked={activeKernel === 'xray'}></span>
          </div>
          <div class="radio-body k-body">
            <div class="radio-name">
              <span>Xray</span>
              {#if xray?.current_version}
                <span class="k-ver text-secondary" style="font-size:12px; font-weight:normal;"
                  >v{xray.current_version}</span
                >
              {/if}
              {#if activeKernel === 'xray'}
                <span class="active-pill">{$t('svc.active_label')}</span>
              {/if}
            </div>
            <div class="radio-desc k-meta">
              {#if !kernelsLoaded}
                <Skeleton type="text-line" width="90px" />
              {:else}
                {xray?.process_status === 'running'
                  ? `${$t('svc.running')} · PID ${xray?.pid || xkeenInfo.pid || '—'}`
                  : $t('svc.stopped')}
              {/if}
            </div>
          </div>
          {#if !isRunning}
            <button
              type="button"
              class="btn btn-primary btn-sm"
              onclick={(e) => {
                e.stopPropagation();
                controlService('start');
              }}
              title={$t('svc.action_start')}
            >
              <svg width="12" height="12" viewBox="0 0 24 24" fill="currentColor">
                <polygon points="5 3 19 12 5 21 5 3" />
              </svg>
              {$t('svc.action_start')}
            </button>
          {/if}
        </div>
      </div>

      <!-- Process Metadata Panel -->
      <div class="process-meta-box">
        <div class="meta-item">
          <span class="meta-lbl">{$t('svc.service_label')}:</span>
          <span class="meta-val">XKeen Supervisor</span>
        </div>
        <div class="meta-item">
          <span class="meta-lbl">PID:</span>
          <span class="meta-val monospace">{xkeenInfo.pid || activeKernelObj?.pid || '—'}</span>
        </div>
        <div class="meta-item">
          <span class="meta-lbl">{$t('svc.uptime_label', { time: '' }).replace(':', '')}:</span>
          <span class="meta-val monospace"
            >{activeKernelObj?.uptime || xkeenInfo.uptime || '—'}</span
          >
        </div>
        <div class="meta-item">
          <span class="meta-lbl">API / Socket:</span>
          <span class="meta-val monospace">
            {#if activeKernel === 'mihomo'}
              {mihomo?.api_addr || '/opt/etc/mihomo/mihomo-api.sock'}
            {:else if activeKernel === 'xray'}
              {xray?.binary_path || '/opt/bin/xray'}
            {:else}
              —
            {/if}
          </span>
        </div>
      </div>

      <!-- Actions Toolbar -->
      <div class="hero-actions">
        {#if isRunning}
          <button
            class="btn btn-danger-soft"
            onclick={() => controlService('stop')}
            disabled={actionLoading['xkeen-stop']}
            title={$t('svc.action_stop')}
          >
            <svg
              width="14"
              height="14"
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
            {$t('svc.action_stop')}
          </button>
          <button
            class="btn btn-secondary"
            onclick={() => controlService('restart')}
            disabled={actionLoading['xkeen-restart']}
            class:btn-loading={actionLoading['xkeen-restart']}
            title={$t('svc.action_restart')}
          >
            <svg
              width="14"
              height="14"
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              stroke-width="2"><path d="M21 12a9 9 0 1 1-3-6.7L21 8M21 3v5h-5" /></svg
            >
            {$t('svc.action_restart')}
          </button>
        {:else}
          <button
            class="btn btn-primary"
            onclick={() => controlService('start')}
            disabled={actionLoading['xkeen-start']}
            class:btn-loading={actionLoading['xkeen-start']}
            title={$t('svc.action_start')}
          >
            <svg width="14" height="14" viewBox="0 0 24 24" fill="currentColor"
              ><polygon points="5 3 19 12 5 21 5 3" /></svg
            >
            {$t('svc.action_start')}
          </button>
        {/if}

        {#if activeKernel === 'mihomo'}
          <button
            class="btn btn-secondary"
            onclick={() => onSwitchTab('proxies')}
            title={$t('svc.api_test')}
          >
            <svg
              width="14"
              height="14"
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              stroke-width="2"
              ><circle cx="12" cy="12" r="9" /><path d="M12 3a14 14 0 0 1 0 18" /></svg
            >
            {$t('svc.api_test')}
          </button>
        {/if}

        <button
          class="btn btn-secondary"
          onclick={() => onSwitchTab('logs')}
          title={$t('svc.logs')}
        >
          <svg
            width="14"
            height="14"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            stroke-width="2"
            ><path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z" /><polyline
              points="14 2 14 8 20 8"
            /><line x1="16" y1="13" x2="8" y2="13" /><line
              x1="16"
              y1="17"
              x2="8"
              y2="17"
            /><polyline points="10 9 9 9 8 9" /></svg
          >
          {$t('svc.logs')}
        </button>

        {#if activeKernel === 'mihomo' && $capabilities?.mihomo?.is_insecure_lan}
          <button
            class="btn btn-warning-soft"
            onclick={() => (showMihomoMigrateModal = true)}
            title={$t('mihomo.migrate_banner_body')}
          >
            ⚠ {$t('mihomo.controller_mode_insecure')}
          </button>
        {/if}
      </div>
    </div>

    <!-- Updates & Channels Card (35%) -->
    <div class="card updates-card">
      <div class="updates-header">
        <div>
          <h2 class="card-title">{$t('svc.updates_title')}</h2>
          <p class="card-subtitle">{$t('svc.updates_desc')}</p>
        </div>
      </div>

      <!-- Channel Selector -->
      <div class="channel-row">
        <span class="channel-lbl">{$t('svc.channel_label')}</span>
        <div class="channel-pills">
          <button
            type="button"
            class="channel-pill"
            class:active={(xray?.channel || mihomo?.channel || 'stable') === 'stable'}
            onclick={() => {
              setKernelChannel('xray', 'stable');
              setKernelChannel('mihomo', 'stable');
            }}
          >
            {$t('svc.channel_stable')}
          </button>
          <button
            type="button"
            class="channel-pill"
            class:active={(xray?.channel || mihomo?.channel || 'stable') === 'preview'}
            onclick={() => {
              setKernelChannel('xray', 'preview');
              setKernelChannel('mihomo', 'preview');
            }}
          >
            {$t('svc.channel_preview')}
          </button>
        </div>
      </div>

      <!-- Kernel Updates List -->
      <div class="kernel-updates-list">
        <!-- Mihomo Item -->
        <div class="update-item">
          <div class="update-info">
            <div class="update-name">Mihomo</div>
            <div class="update-version">
              {#if !kernelsLoaded}
                <Skeleton type="text-line" width="70px" />
              {:else}
                <span>v{mihomo?.current_version || '—'}</span>
                {#if mihomo?.has_update}
                  <span class="badge badge-warning">→ v{mihomo.latest_version}</span>
                {:else}
                  <span class="badge badge-neutral">{$t('svc.actual_badge')}</span>
                {/if}
              {/if}
            </div>
          </div>
          <div class="update-actions">
            {#if mihomo?.has_update}
              <button
                class="btn btn-sm btn-primary"
                onclick={() => installKernel('mihomo')}
                disabled={mihomo.status !== 'idle'}
                title={$t('svc.install_update')}
              >
                {mihomo.status === 'downloading' || mihomo.status === 'installing'
                  ? $t('kernels.installing')
                  : $t('svc.install_update')}
              </button>
            {/if}
            {#if mihomo?.current_version && mihomo.current_version !== 'not installed'}
              <button
                class="btn btn-sm btn-secondary btn-icon"
                onclick={() => downloadKernelBinary('mihomo')}
                title={$t('svc.download')}
                aria-label={$t('svc.download')}
              >
                <svg
                  width="13"
                  height="13"
                  viewBox="0 0 24 24"
                  fill="none"
                  stroke="currentColor"
                  stroke-width="2"
                  ><path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4" /><polyline
                    points="7 10 12 15 17 10"
                  /><line x1="12" y1="15" x2="12" y2="3" /></svg
                >
              </button>
            {/if}
          </div>
        </div>

        <!-- Xray Item -->
        <div class="update-item">
          <div class="update-info">
            <div class="update-name">Xray</div>
            <div class="update-version">
              {#if !kernelsLoaded}
                <Skeleton type="text-line" width="70px" />
              {:else}
                <span>v{xray?.current_version || '—'}</span>
                {#if xray?.has_update}
                  <span class="badge badge-warning">→ v{xray.latest_version}</span>
                {:else}
                  <span class="badge badge-neutral">{$t('svc.actual_badge')}</span>
                {/if}
              {/if}
            </div>
          </div>
          <div class="update-actions">
            {#if xray?.has_update}
              <button
                class="btn btn-sm btn-primary"
                onclick={() => installKernel('xray')}
                disabled={xray.status !== 'idle'}
                title={$t('svc.install_update')}
              >
                {xray.status === 'downloading' || xray.status === 'installing'
                  ? $t('kernels.installing')
                  : $t('svc.install_update')}
              </button>
            {/if}
            {#if xray?.current_version && xray.current_version !== 'not installed'}
              <button
                class="btn btn-sm btn-secondary btn-icon"
                onclick={() => downloadKernelBinary('xray')}
                title={$t('svc.download')}
                aria-label={$t('svc.download')}
              >
                <svg
                  width="13"
                  height="13"
                  viewBox="0 0 24 24"
                  fill="none"
                  stroke="currentColor"
                  stroke-width="2"
                  ><path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4" /><polyline
                    points="7 10 12 15 17 10"
                  /><line x1="12" y1="15" x2="12" y2="3" /></svg
                >
              </button>
            {/if}
          </div>
        </div>
      </div>
    </div>
  </div>

  <!-- Bottom Grid: Restart History & Entware System Status (SRV-03, SRV-04) -->
  <div class="services-bottom-grid">
    <!-- Restart History Card (SRV-04) -->
    <div class="card restart-card">
      <div class="card-head-row">
        <div>
          <h2 class="card-title">{$t('svc.restart_log_title')}</h2>
          <p class="card-subtitle">
            {pluralize(
              restartLog.length,
              $t('svc.entries_count_one', { count: String(restartLog.length) }),
              $t('svc.entries_count_few', { count: String(restartLog.length) }),
              $t('svc.entries_count_many', { count: String(restartLog.length) }),
              $currentLang
            )}
          </p>
        </div>
        {#if restartLog.length > 0}
          <div class="ct-actions">
            <button
              class="btn btn-sm btn-secondary"
              onclick={() => (restartLogExpanded = !restartLogExpanded)}
            >
              {restartLogExpanded ? $t('svc.log_collapse') : $t('svc.log_expand')}
            </button>
          </div>
        {/if}
      </div>

      {#if restartLog.length === 0}
        <div class="empty-state">
          <svg
            width="32"
            height="32"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            stroke-width="1.5"
            ><circle cx="12" cy="12" r="10" /><polyline points="12 6 12 12 16 14" /></svg
          >
          <p>{$t('svc.restart_log_empty')}</p>
        </div>
      {:else}
        <div class="restart-log">
          {#each Array.isArray(restartLog) ? (restartLogExpanded ? restartLog : restartLog.slice(0, 5)) : [] as entry}
            <div
              class="log-entry"
              class:log-success={entry.success}
              class:log-fail={!entry.success}
            >
              <div class="log-meta">
                <span class="log-action">{formatAction(entry.action)}</span>
                <span
                  class="log-badge"
                  class:badge-ok={entry.success}
                  class:badge-err={!entry.success}
                >
                  {entry.success ? $t('svc.log_ok') : $t('svc.log_fail')}
                </span>
                <span class="log-ts monospace">{formatTs(entry.timestamp)}</span>
              </div>
              {#if entry.output}
                <pre class="log-output">{entry.output}</pre>
              {/if}
            </div>
          {/each}
        </div>
      {/if}
    </div>

    <!-- Entware System Services Status (SRV-03) -->
    <div class="card entware-card">
      <div class="card-head-row">
        <div>
          <h2 class="card-title">{$t('svc.entware_services')}</h2>
          <p class="card-subtitle">{$t('svc.entware_desc')}</p>
        </div>
      </div>

      <div class="entware-list">
        <div class="entware-item">
          <div class="entware-info">
            <div class="entware-name monospace">/opt/etc/init.d/S99xcp</div>
            <div class="entware-desc">XKeen Control Panel Daemon (Active)</div>
          </div>
          <span class="status-badge running">
            <span class="status-dot success"></span>Active
          </span>
        </div>

        <div class="entware-item">
          <div class="entware-info">
            <div class="entware-name monospace">/opt/etc/init.d/S24xkeen</div>
            <div class="entware-desc">
              XKeen Router Core Supervisor ({isRunning ? 'Running' : 'Stopped'})
            </div>
          </div>
          {#if isRunning}
            <span class="status-badge running">
              <span class="status-dot success"></span>Active
            </span>
          {:else}
            <span class="status-badge stopped">
              <span class="status-dot error"></span>Stopped
            </span>
          {/if}
        </div>
      </div>
    </div>
  </div>
</div>

<MihomoSocketMigrateModal
  bind:open={showMihomoMigrateModal}
  onclose={() => (showMihomoMigrateModal = false)}
  onsuccess={() => {
    fetchStatus();
    fetchKernels();
    fetchCapabilities();
  }}
/>

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

  /* 2-Section Grid Layout (SRV-01) */
  .services-top-grid {
    display: grid;
    grid-template-columns: minmax(0, 1.7fr) minmax(0, 1fr);
    gap: 20px;
    margin-bottom: 24px;
  }

  .services-bottom-grid {
    display: grid;
    grid-template-columns: minmax(0, 1.4fr) minmax(0, 1fr);
    gap: 20px;
    margin-bottom: 32px;
  }

  /* Hero Card */
  .hero-card {
    padding: 24px;
    background: var(--bg-card);
    border: 1px solid var(--border);
    border-radius: var(--radius-lg);
    display: flex;
    flex-direction: column;
    gap: 18px;
  }

  .hero-header {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    gap: 12px;
  }

  .hero-badge-wrap {
    display: flex;
    align-items: center;
    gap: 12px;
  }

  .hero-icon {
    width: 44px;
    height: 44px;
    border-radius: var(--radius-md);
    background: rgba(41, 194, 240, 0.12);
    color: var(--accent);
    display: grid;
    place-items: center;
    flex-shrink: 0;
  }

  .hero-title {
    margin: 0;
    font-size: 18px;
    font-weight: 700;
    color: var(--fg-primary);
  }

  .hero-subtitle {
    margin: 2px 0 0;
    font-size: 12px;
    color: var(--fg-dim);
  }

  /* Radio Grid for Kernel Selection (SRV-02) */
  .core-radio-grid {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: 12px;
  }

  .core-radio-card {
    display: flex;
    align-items: center;
    gap: 12px;
    padding: 12px 16px;
    background: var(--bg-secondary);
    border: 1.5px solid var(--border);
    border-radius: var(--radius-md);
    cursor: pointer;
    text-align: left;
    transition: all 0.18s ease;
    outline: none;
    font-family: inherit;
  }

  .core-radio-card:hover:not(:disabled) {
    border-color: var(--border-hover, var(--accent));
    background: var(--bg-hover);
  }

  .core-radio-card.active {
    border-color: var(--accent);
    background: rgba(41, 194, 240, 0.08);
    box-shadow: 0 0 0 1px rgba(41, 194, 240, 0.25);
  }

  .core-radio-card.switching {
    opacity: 0.7;
    cursor: wait;
  }

  .radio-indicator {
    display: flex;
    align-items: center;
    justify-content: center;
    flex-shrink: 0;
  }

  .radio-dot {
    width: 16px;
    height: 16px;
    border-radius: 50%;
    border: 2px solid var(--border-hover, var(--fg-dim));
    position: relative;
    transition: all 0.15s ease;
  }

  .radio-dot.checked {
    border-color: var(--accent);
  }

  .radio-dot.checked::after {
    content: '';
    position: absolute;
    width: 8px;
    height: 8px;
    top: 2px;
    left: 2px;
    border-radius: 50%;
    background: var(--accent);
  }

  .radio-body {
    flex: 1;
    min-width: 0;
  }

  .radio-name {
    display: flex;
    align-items: center;
    gap: 8px;
    font-size: 14px;
    font-weight: 700;
    color: var(--fg-primary);
  }

  .active-pill {
    font-size: 10px;
    font-weight: 700;
    text-transform: uppercase;
    letter-spacing: 0.04em;
    padding: 1px 6px;
    border-radius: 10px;
    background: var(--accent);
    color: #fff;
  }

  .radio-desc {
    font-size: 12px;
    color: var(--fg-dim);
    margin-top: 2px;
    font-family: var(--font-family-mono);
  }

  /* Process Meta Box */
  .process-meta-box {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(180px, 1fr));
    gap: 10px;
    padding: 12px 16px;
    background: var(--bg-surface-elevated, rgba(20, 51, 79, 0.4));
    border: 1px solid var(--border-light, rgba(255, 255, 255, 0.06));
    border-radius: var(--radius-md);
  }

  .meta-item {
    display: flex;
    flex-direction: column;
    gap: 2px;
  }

  .meta-lbl {
    font-size: 11px;
    font-weight: 500;
    color: var(--fg-dim);
  }

  .meta-val {
    font-size: 13px;
    color: var(--fg-primary);
    font-weight: 600;
  }

  .monospace {
    font-family: var(--font-family-mono);
  }

  /* Hero Actions */
  .hero-actions {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 10px;
    padding-top: 4px;
  }

  .btn-danger-soft {
    background: rgba(244, 112, 127, 0.15);
    color: var(--danger, #f4707f);
    border: 1px solid rgba(244, 112, 127, 0.3);
  }

  .btn-danger-soft:hover {
    background: rgba(244, 112, 127, 0.25);
  }

  .btn-warning-soft {
    background: rgba(245, 166, 35, 0.15);
    color: var(--warning, #f5a623);
    border: 1px solid rgba(245, 166, 35, 0.3);
  }

  /* Updates Card */
  .updates-card {
    min-width: 0;
    padding: 24px;
    display: flex;
    flex-direction: column;
    gap: 16px;
  }

  .card-title {
    margin: 0;
    font-size: 16px;
    font-weight: 700;
    color: var(--fg-primary);
  }

  .card-subtitle {
    margin: 2px 0 0;
    font-size: 12px;
    color: var(--fg-dim);
  }

  .channel-row {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    justify-content: space-between;
    gap: 8px;
    padding: 10px 14px;
    background: var(--bg-secondary);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
  }

  .channel-lbl {
    font-size: 13px;
    font-weight: 500;
    color: var(--fg-primary);
  }

  .channel-pills {
    display: flex;
    border: 1px solid var(--border);
    border-radius: 20px;
    overflow: hidden;
    background: var(--bg-card);
  }

  .channel-pill {
    padding: 4px 10px;
    font-size: 11px;
    font-weight: 600;
    border: none;
    background: transparent;
    color: var(--fg-secondary);
    cursor: pointer;
    transition: all 0.15s ease;
  }

  .channel-pill.active {
    background: var(--accent);
    color: #fff;
  }

  .kernel-updates-list {
    display: flex;
    flex-direction: column;
    gap: 10px;
  }

  .update-item {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 12px 14px;
    background: var(--bg-secondary);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
  }

  .update-name {
    font-size: 14px;
    font-weight: 700;
    color: var(--fg-primary);
  }

  .update-version {
    font-size: 12px;
    color: var(--fg-dim);
    margin-top: 2px;
    display: flex;
    align-items: center;
    gap: 6px;
    font-family: var(--font-family-mono);
  }

  .update-actions {
    display: flex;
    align-items: center;
    gap: 6px;
  }

  .badge-neutral {
    background: rgba(255, 255, 255, 0.08);
    color: var(--fg-dim);
  }

  /* Restart Log & Entware Section */
  .restart-card,
  .entware-card {
    padding: 20px 24px;
  }

  .card-head-row {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    margin-bottom: 14px;
  }

  .empty-state {
    padding: 28px 16px;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    color: var(--fg-dim);
    gap: 8px;
    font-size: 13px;
  }

  .restart-log {
    display: flex;
    flex-direction: column;
    gap: 8px;
  }

  .log-entry {
    border-radius: var(--radius-sm);
    border-left: 3px solid var(--border);
    padding: 8px 12px;
    background: var(--bg-secondary);
  }

  .log-entry.log-success {
    border-left-color: var(--success, #46d18a);
  }

  .log-entry.log-fail {
    border-left-color: var(--danger, #f4707f);
  }

  .log-meta {
    display: flex;
    align-items: center;
    gap: 8px;
  }

  .log-action {
    font-weight: 600;
    font-size: 13px;
    color: var(--fg-primary);
  }

  .log-badge {
    font-size: 10px;
    padding: 1px 6px;
    border-radius: 4px;
    font-weight: 700;
  }

  .badge-ok {
    background: rgba(70, 209, 138, 0.15);
    color: var(--success, #46d18a);
  }

  .badge-err {
    background: rgba(244, 112, 127, 0.15);
    color: var(--danger, #f4707f);
  }

  .log-ts {
    font-size: 11px;
    color: var(--fg-dim);
    margin-left: auto;
  }

  .log-output {
    margin: 6px 0 0;
    font-size: 11px;
    color: var(--fg-dim);
    white-space: pre-wrap;
    word-break: break-all;
    max-height: 120px;
    overflow-y: auto;
    background: rgba(0, 0, 0, 0.2);
    padding: 6px 8px;
    border-radius: 4px;
  }

  .entware-list {
    display: flex;
    flex-direction: column;
    gap: 10px;
  }

  .entware-item {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 12px 14px;
    background: var(--bg-secondary);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
  }

  .entware-name {
    font-size: 13px;
    font-weight: 700;
    color: var(--fg-primary);
  }

  .entware-desc {
    font-size: 11px;
    color: var(--fg-dim);
    margin-top: 2px;
  }

  @media (max-width: 900px) {
    .services-top-grid,
    .services-bottom-grid {
      grid-template-columns: 1fr;
    }

    .core-radio-grid {
      grid-template-columns: 1fr;
    }
  }
</style>
