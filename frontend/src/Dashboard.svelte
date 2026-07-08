<script lang="ts">
  import { onMount } from 'svelte';
  import { fade } from 'svelte/transition';
  import { t, currentLang, setLang, pluralize } from './i18n';
  import {
    isSidebarOpen,
    capabilities,
    fetchCapabilities,
    showToast,
    mihomoApiAvailable
  } from './stores';
  import Sidebar from './components/Sidebar.svelte';
  import Toast from './components/Toast.svelte';
  import ConfirmDialog from './components/ConfirmDialog.svelte';
  import Card from './components/Card.svelte';
  import Button from './components/Button.svelte';
  import Icon from './lib/components/Icon.svelte';
  import Skeleton from './components/Skeleton.svelte';
  import Editor from './Editor.svelte';
  import Logs from './Logs.svelte';
  import Services from './Services.svelte';
  import Settings from './Settings.svelte';
  import Proxies from './Proxies.svelte';
  import Connections from './Connections.svelte';
  import Rules from './Rules.svelte';
  import Traffic from './Traffic.svelte';
  import NetworkTools from './NetworkTools.svelte';
  import SmartProxy from './SmartProxy.svelte';
  import TrafficQuotas from './TrafficQuotas.svelte';
  import DATManager from './DATManager.svelte';
  import MihomoGenerator from './MihomoGenerator.svelte';
  import Console from './Console.svelte';
  import ApiOffline from './components/ApiOffline.svelte';

  let version = $state($t('app.loading'));
  let panelVersion = $state($t('app.loading'));
  let loading = $state(false);
  let currentTab = $state('dashboard');
  const mihomoDependentTabs = [
    'proxies',
    'connections',
    'rules',
    'traffic',
    'smartproxy',
    'trafficquotas'
  ];
  let theme = $state(document.documentElement.getAttribute('data-theme') || 'light');
  let pwaInstallPrompt = $state<any>(null);

  // Dashboard live monitoring state
  interface ServiceStatus {
    xkeen: string;
    xray: string;
    mihomo: string;
    connections: number;
    xrayVersion: string;
    mihomoVersion: string;
  }

  let serviceStatus = $state<ServiceStatus>({
    xkeen: 'loading',
    xray: 'loading',
    mihomo: 'loading',
    connections: 0,
    xrayVersion: '',
    mihomoVersion: ''
  });
  let statusError = $state(false);
  let statusLoading = $state(true);

  interface SystemStats {
    memory: { total: number; used: number; free: number };
    disk: { total: number; used: number; free: number };
    ssl_cert_days: number;
    load: [number, number, number];
    uptime: { seconds: number; days: number; hours: number; minutes: number };
    go_runtime: {
      goroutines: number;
      heap_alloc: number;
      heap_sys: number;
      num_gc: number;
      go_version: string;
      gomaxprocs: number;
      goarch: string;
    };
    router_model: string;
    hostname: string;
    wan_status: string;
    default_gateway: string;
    dns_servers: string[];
    dns_resolving: boolean;
    invalid_config: boolean;
    platform: string;
    kernel_version: string;
    ip_interface: string;
    timezone: string;
    config_path: string;
    config_lines: number;
    boot_time: string;
  }

  let systemStats = $state<SystemStats | null>(null);
  let loadHistory = $state<number[]>([]);
  let activeSubscriptionsCount = $state(0);
  let totalSubsCount = $state(0);
  let hasSubscription = $state(false);
  let subsLastUpdated = $state('');
  let totalProxiesCount = $state(0);
  let activeProxiesCount = $state(0);
  let subscriptionProxiesCount = $state(0);
  let statsLastFetched = $state('');

  const isKernelCrashed = $derived(
    serviceStatus.xkeen === 'running' &&
      $capabilities?.active_kernel &&
      $capabilities.active_kernel !== 'none' &&
      (($capabilities.active_kernel === 'mihomo' && serviceStatus.mihomo === 'stopped') ||
        ($capabilities.active_kernel === 'xray' && serviceStatus.xray === 'stopped'))
  );

  const isDiskLow = $derived(
    systemStats !== null && systemStats.disk && systemStats.disk.free < 10 * 1024 * 1024
  );

  const isSSLExpiring = $derived(
    systemStats !== null && systemStats.ssl_cert_days >= 0 && systemStats.ssl_cert_days < 7
  );

  function getDiskBarColor(stats: SystemStats): string {
    if (!stats.disk) {
      return 'var(--color-success, var(--color-primary, #2ecc71))';
    }
    const pct = (stats.disk.used / stats.disk.total) * 100;
    const freeMB = stats.disk.free / 1024 / 1024;
    if (pct > 90 || freeMB < 10) {
      return 'var(--color-danger, #e74c3c)';
    }
    if (pct > 80) {
      return 'var(--color-warning, #f39c12)';
    }
    return 'var(--color-success, var(--color-primary, #2ecc71))';
  }

  async function fetchSubscriptionSummary() {
    try {
      const res = await fetch('/api/subscriptions');
      if (res.ok) {
        const envelope = await res.json();
        const subs = Array.isArray(envelope) ? envelope : (envelope.data ?? []);
        activeSubscriptionsCount = subs.filter((s: any) => s.enabled).length;
        totalSubsCount = subs.length;
        hasSubscription = subs.length > 0;
        subscriptionProxiesCount = subs.reduce(
          (acc: number, s: any) => acc + (s.proxy_count || 0),
          0
        );
        // Find most recent update
        const dates = subs.map((s: any) => s.last_updated || s.updated_at || '').filter(Boolean);
        if (dates.length > 0) {
          const latest = dates.sort().reverse()[0];
          const d = new Date(latest);
          const today = new Date();
          if (d.toDateString() === today.toDateString()) {
            subsLastUpdated = $t('dash.updated_today');
          } else {
            subsLastUpdated = d.toLocaleDateString($currentLang === 'ru' ? 'ru-RU' : 'en-US', {
              day: '2-digit',
              month: '2-digit'
            });
          }
        }
      }
    } catch (_) {}
  }

  async function fetchProxySummary() {
    try {
      const res = await fetch('/api/mihomo/proxy/proxies');
      if (res.ok) {
        const data = await res.json();
        const proxies = data.proxies || {};
        const keys = Object.keys(proxies);
        totalProxiesCount = keys.length;
        activeProxiesCount = keys.filter(
          (k) =>
            proxies[k].alive !== false &&
            proxies[k].type !== 'Selector' &&
            proxies[k].type !== 'URLTest'
        ).length;
      }
    } catch (_) {}
  }

  async function fetchLiveStatus() {
    statusError = false;
    try {
      const [svcRes, mihomoRes] = await Promise.allSettled([
        fetch('/api/service/status'),
        fetch('/api/mihomo/status')
      ]);

      let isXkeenRunning = false;
      let xkeenRaw = '';
      if (svcRes.status === 'fulfilled' && svcRes.value.ok) {
        const text = await svcRes.value.text();
        try {
          const parsed = JSON.parse(text);
          if (parsed && parsed.success && parsed.data) {
            isXkeenRunning = parsed.data.is_running;
            xkeenRaw = parsed.data.raw || '';
          } else {
            xkeenRaw = text;
            isXkeenRunning =
              text.toLowerCase().includes('running') || text.toLowerCase().includes('запущен');
          }
        } catch (_) {
          xkeenRaw = text;
          isXkeenRunning =
            text.toLowerCase().includes('running') || text.toLowerCase().includes('запущен');
        }
      }

      const mihomoText =
        mihomoRes.status === 'fulfilled' && mihomoRes.value.ok ? await mihomoRes.value.text() : '';

      // Try to get connection count from mihomo
      let connCount = 0;
      try {
        const connRes = await fetch('/api/mihomo/proxy/connections?limit=1');
        if (connRes.ok) {
          const connData = await connRes.json();
          connCount = connData?.connections?.length ?? 0;
        }
      } catch (_) {}

      // Get kernel versions and process_status from /api/kernels
      let xrayVer = '';
      let mihomoVer = '';
      let xrayProcessStatus = 'unknown';
      let mihomoProcessStatus = 'unknown';
      try {
        const kernelsRes = await fetch('/api/kernels');
        if (kernelsRes.ok) {
          const kernelsEnvelope = await kernelsRes.json();
          // KernelList uses JSONSuccess envelope: {success, data: [...]}
          const kernels = Array.isArray(kernelsEnvelope)
            ? kernelsEnvelope
            : (kernelsEnvelope.data ?? kernelsEnvelope);
          for (const k of kernels) {
            if (k.name === 'xray') {
              xrayVer = k.current_version || '';
              xrayProcessStatus = k.process_status || 'unknown';
            }
            if (k.name === 'mihomo') {
              mihomoVer = k.current_version || '';
              mihomoProcessStatus = k.process_status || 'unknown';
            }
          }
        } else {
          xrayProcessStatus = 'error';
          mihomoProcessStatus = 'error';
        }
      } catch (_) {
        xrayProcessStatus = 'error';
        mihomoProcessStatus = 'error';
      }

      serviceStatus = {
        xkeen: isXkeenRunning ? 'running' : xkeenRaw || 'unknown',
        xray: xrayProcessStatus,
        mihomo: mihomoProcessStatus,
        connections: connCount,
        xrayVersion: xrayVer,
        mihomoVersion: mihomoVer
      };
    } catch (_) {
      statusError = true;
      serviceStatus = { ...serviceStatus, xray: 'error', mihomo: 'error' };
    } finally {
      statusLoading = false;
    }
  }

  async function fetchSystemStats() {
    try {
      const res = await fetch('/api/system/stats');
      if (res.ok) {
        systemStats = await res.json();
        if (systemStats) {
          loadHistory = [...loadHistory, systemStats.load[0]].slice(-16);
          const d = new Date();
          const p = (n: number) => n.toString().padStart(2, '0');
          statsLastFetched = `${p(d.getDate())}.${p(d.getMonth() + 1)}.${String(d.getFullYear()).slice(2)} ${p(d.getHours())}:${p(d.getMinutes())}`;
        }
      }
    } catch (e) {
      // ignore
    }
  }

  function buildSparklinePath(values: number[]): string {
    if (values.length < 2) return '';
    const w = 200,
      h = 42;
    const max = Math.max(...values, 0.01);
    const pts = values.map((v, i) => {
      const x = (i / (values.length - 1)) * w;
      const y = h - 4 - (v / max) * (h - 10);
      return `${x.toFixed(1)},${y.toFixed(1)}`;
    });
    const line = `M${pts.join(' L')}`;
    const fill = `${line} L${w},${h} L0,${h} Z`;
    return JSON.stringify({ line, fill });
  }

  const sparklineData = $derived(
    loadHistory.length >= 2 ? JSON.parse(buildSparklinePath(loadHistory)) : null
  );

  // Quickstart checklist reactive state
  const quickstartDoneCount = $derived(
    [
      true, // step 1 always done when card is visible (active_kernel === 'mihomo')
      hasSubscription,
      $mihomoApiAvailable,
      serviceStatus.mihomo === 'running'
    ].filter(Boolean).length
  );
  const allQuickstartComplete = $derived(quickstartDoneCount === 4);

  function formatBytes(bytes: number): string {
    if (bytes === 0) return '0 B';
    const k = 1024;
    const sizes = ['B', 'KB', 'MB', 'GB'];
    const i = Math.floor(Math.log(bytes) / Math.log(k));
    return parseFloat((bytes / Math.pow(k, i)).toFixed(2)) + ' ' + sizes[i];
  }

  function toggleTheme() {
    theme = theme === 'dark' ? 'light' : 'dark';
    document.documentElement.setAttribute('data-theme', theme);
    localStorage.setItem('theme', theme);
  }

  async function fetchVersion() {
    try {
      const res = await fetch('/api/version');
      const data = await res.json();
      version = data.version;
      panelVersion = data.panel_version;
    } catch (e) {
      version = $t('app.error');
      panelVersion = $t('app.error');
    }
  }

  let isRefreshing = $state(false);

  async function handleRefresh() {
    if (isRefreshing) return;
    isRefreshing = true;
    try {
      await Promise.all([fetchLiveStatus(), fetchSystemStats(), fetchVersion()]);
    } finally {
      isRefreshing = false;
    }
  }

  async function handleLogout() {
    loading = true;
    try {
      const csrfToken = localStorage.getItem('csrf_token');
      await fetch('/api/auth/logout', {
        method: 'POST',
        headers: { 'X-CSRF-Token': csrfToken || '' }
      });
      localStorage.removeItem('csrf_token');
      window.location.href = '/';
    } catch (e) {
      console.error('Logout error:', e);
    } finally {
      loading = false;
    }
  }

  function getTabFromHash(): string {
    const hash = window.location.hash;
    if (hash && hash.startsWith('#/')) {
      const path = hash.slice(2);
      const queryIdx = path.indexOf('?');
      const basePath = queryIdx !== -1 ? path.slice(0, queryIdx) : path;

      if (basePath.startsWith('subscriptions/')) {
        const id = basePath.slice('subscriptions/'.length);
        window.location.hash = `#/proxies?tab=providers&expand=${id}`;
        return 'proxies';
      }
      if (basePath === 'subscriptions') {
        window.location.hash = '#/proxies?tab=providers';
        return 'proxies';
      }
      if (basePath === 'mihomo-gen' || basePath === 'constructor') {
        return 'editor';
      }
      return basePath;
    }
    return 'dashboard';
  }

  function handleHashChange() {
    currentTab = getTabFromHash();
  }

  let isSidebarCollapsed = $state(localStorage.getItem('sidebar.collapsed') === 'true');

  function toggleSidebarCollapse() {
    isSidebarCollapsed = !isSidebarCollapsed;
    localStorage.setItem('sidebar.collapsed', String(isSidebarCollapsed));
  }

  function handleKeydown(e: KeyboardEvent) {
    if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === 'b') {
      e.preventDefault();
      toggleSidebarCollapse();
    }
  }

  function switchTab(tab: string) {
    window.location.hash = '#/' + tab;
  }

  function toggleSidebar() {
    isSidebarOpen.update((v) => !v);
  }

  function closeSidebar() {
    isSidebarOpen.set(false);
  }

  async function installPWA() {
    if (!pwaInstallPrompt) return;
    pwaInstallPrompt.prompt();
    const { outcome } = await pwaInstallPrompt.userChoice;
    if (outcome === 'accepted') {
      pwaInstallPrompt = null;
    }
  }

  async function restartXkeen() {
    try {
      const csrfToken = localStorage.getItem('csrf_token');
      const res = await fetch('/api/service/control?action=restart', {
        method: 'POST',
        headers: { 'X-CSRF-Token': csrfToken || '' }
      });
      if (res.ok) {
        showToast('success', $t('app.restart') + ' XKeen...');
        setTimeout(fetchLiveStatus, 3000);
      } else {
        showToast('error', $t('app.error'));
      }
    } catch (_) {
      showToast('error', $t('app.error'));
    }
  }

  function statusColor(status: string): string {
    if (status === 'running') return 'success';
    if (status === 'stopped' || status === 'not_installed') return 'error';
    if (status === 'error') return 'error';
    if (status === 'loading') return 'warning';
    return 'warning'; // unknown
  }

  onMount(() => {
    fetchVersion();
    fetchLiveStatus();
    fetchSystemStats();
    fetchCapabilities();
    fetchSubscriptionSummary();
    fetchProxySummary();

    currentTab = getTabFromHash();
    window.addEventListener('hashchange', handleHashChange);
    if (!window.location.hash) {
      window.location.hash = '#/' + currentTab;
    }

    window.addEventListener('keydown', handleKeydown);

    const statusInterval = setInterval(fetchLiveStatus, 10000);
    const statsInterval = setInterval(fetchSystemStats, 5000);
    const capInterval = setInterval(fetchCapabilities, 10000);
    const subsInterval = setInterval(fetchSubscriptionSummary, 30000);
    window.addEventListener('beforeinstallprompt', (e: Event) => {
      e.preventDefault();
      pwaInstallPrompt = e;
    });
    return () => {
      clearInterval(statusInterval);
      clearInterval(statsInterval);
      clearInterval(capInterval);
      clearInterval(subsInterval);
      window.removeEventListener('hashchange', handleHashChange);
      window.removeEventListener('keydown', handleKeydown);
    };
  });
</script>

<div class="dashboard-layout" class:sb-collapsed={isSidebarCollapsed}>
  <!-- Mobile header bar -->
  <header class="mobile-header">
    <button
      class="burger-btn"
      onclick={toggleSidebar}
      aria-label={$t('nav.open_menu')}
      title={$t('nav.open_menu')}
    >
      <svg width="22" height="22" viewBox="0 0 22 22" fill="none" aria-hidden="true">
        <rect y="3" width="22" height="2.5" rx="1.25" fill="currentColor" />
        <rect y="9.75" width="22" height="2.5" rx="1.25" fill="currentColor" />
        <rect y="16.5" width="22" height="2.5" rx="1.25" fill="currentColor" />
      </svg>
    </button>
    <span style="font-weight: 600; font-size: 16px;">XKeen CP</span>
    <span style="width: 34px;"></span>
  </header>

  <!-- Off-canvas overlay (mobile only) -->
  <!-- svelte-ignore a11y-click-events-have-key-events -->
  <!-- svelte-ignore a11y-no-static-element-interactions -->
  <div
    class="sidebar-overlay"
    class:hidden={!$isSidebarOpen}
    onclick={closeSidebar}
    role="button"
    tabindex="0"
    aria-label={$t('nav.close_menu')}
    title={$t('nav.close_menu')}
  ></div>

  <!-- Sidebar -->
  <div
    class="sidebar"
    class:sidebar-open={$isSidebarOpen}
    style="display: flex; flex-direction: column;"
  >
    <Sidebar
      {currentTab}
      onSwitchTab={switchTab}
      {theme}
      onToggleTheme={toggleTheme}
      onLogout={handleLogout}
      {loading}
      {pwaInstallPrompt}
      onInstallPWA={installPWA}
      isCollapsed={isSidebarCollapsed}
      onToggleCollapse={toggleSidebarCollapse}
    />
  </div>

  <!-- Main content area -->
  <div class="main-content">
    <!-- Mihomo offline warning banner -->
    {#if mihomoDependentTabs.includes(currentTab) && $capabilities !== null && !$capabilities.mihomo.reachable}
      <div style="margin: 12px 16px 0;">
        <ApiOffline
          endpoint={$capabilities.mihomo.discovered_secret ? 'Mihomo API' : '127.0.0.1:9090'}
          lastSeenSeconds={0}
          onRetry={fetchCapabilities}
        />
      </div>
    {/if}

    {#if currentTab === 'dashboard'}
      <div class="container" transition:fade={{ duration: 150 }}>
        <!-- Page header -->
        <div class="page-head">
          <div>
            <div class="crumbs">
              {$t('nav.group_core')} <span style="color:var(--fg-faint);margin:0 6px;">/</span>
              {$t('nav.dashboard')}
            </div>
            <h1>{$t('dash.title')}</h1>
            <p class="sub">{$t('dash.welcome')}</p>
          </div>
          <div class="ph-actions">
            <Button
              variant="secondary"
              onclick={handleRefresh}
              loading={isRefreshing}
              disabled={isRefreshing}
              title={$t('app.refresh')}
            >
              <Icon name="refresh" size={14} />
              {$t('app.refresh')}
            </Button>
            <Button variant="primary" onclick={restartXkeen} title={$t('dash.restart_xkeen')}>
              <svg width="14" height="14" viewBox="0 0 24 24" fill="currentColor" aria-hidden="true"
                ><path d="M13 2 4 14h7l-1 8 10-13h-7z" /></svg
              >
              {$t('dash.restart_xkeen')}
            </Button>
          </div>
        </div>

        <!-- Quickstart Checklist (Mihomo only, auto-hides when all steps complete) -->
        {#if $capabilities?.active_kernel === 'mihomo' && !allQuickstartComplete}
          <div style="margin-bottom: 18px;">
            <Card title={$t('dash.quickstart.title')}>
              {#snippet actions()}
                <span
                  style="font-size: 12px; font-weight: 400; color: var(--fg-dim); font-family: var(--font-family-mono);"
                >
                  {$t('dash.quickstart.progress', {
                    done: String(quickstartDoneCount),
                    total: '4'
                  })}
                </span>
              {/snippet}
              <ul class="quickstart-list" role="list">
                <!-- Step 1: kernel selected (always done when card is visible) -->
                <li class="qs-step qs-step--done">
                  <span class="qs-icon" aria-label="Выполнено">
                    <Icon name="check" size={16} color="var(--success)" />
                  </span>
                  <span class="qs-text">{$t('dash.quickstart.step1_label')}</span>
                </li>
                <!-- Step 2: subscription added -->
                <li class="qs-step" class:qs-step--done={hasSubscription}>
                  <span class="qs-icon" aria-label={hasSubscription ? 'Выполнено' : 'Не выполнено'}>
                    {#if hasSubscription}
                      <Icon name="check" size={16} color="var(--success)" />
                    {:else}
                      <svg
                        width="16"
                        height="16"
                        viewBox="0 0 16 16"
                        fill="none"
                        aria-hidden="true"
                      >
                        <circle cx="8" cy="8" r="6.5" stroke="var(--fg-dim)" stroke-width="1.5" />
                      </svg>
                    {/if}
                  </span>
                  <span class="qs-text">
                    {hasSubscription
                      ? $t('dash.quickstart.step2_done')
                      : $t('dash.quickstart.step2_label')}
                  </span>
                  {#if !hasSubscription}
                    <a
                      class="btn btn-secondary qs-cta"
                      href="#/proxies?tab=providers"
                      onclick={() => switchTab('proxies')}
                    >
                      {$t('dash.quickstart.step2_cta')}
                    </a>
                  {/if}
                </li>
                <!-- Step 3: config applied (Mihomo API reachable) -->
                <li class="qs-step" class:qs-step--done={$mihomoApiAvailable}>
                  <span
                    class="qs-icon"
                    aria-label={$mihomoApiAvailable ? 'Выполнено' : 'Не выполнено'}
                  >
                    {#if $mihomoApiAvailable}
                      <Icon name="check" size={16} color="var(--success)" />
                    {:else}
                      <svg
                        width="16"
                        height="16"
                        viewBox="0 0 16 16"
                        fill="none"
                        aria-hidden="true"
                      >
                        <circle cx="8" cy="8" r="6.5" stroke="var(--fg-dim)" stroke-width="1.5" />
                      </svg>
                    {/if}
                  </span>
                  <span class="qs-text">
                    {$mihomoApiAvailable
                      ? $t('dash.quickstart.step3_done')
                      : $t('dash.quickstart.step3_label')}
                  </span>
                  {#if !$mihomoApiAvailable}
                    <a
                      class="btn btn-secondary qs-cta"
                      href="#/constructor"
                      onclick={() => {
                        window.location.hash = '#/constructor';
                      }}
                    >
                      {$t('dash.quickstart.step3_cta')}
                    </a>
                  {/if}
                </li>
                <!-- Step 4: Mihomo running -->
                <li class="qs-step" class:qs-step--done={serviceStatus.mihomo === 'running'}>
                  <span
                    class="qs-icon"
                    aria-label={serviceStatus.mihomo === 'running' ? 'Выполнено' : 'Не выполнено'}
                  >
                    {#if serviceStatus.mihomo === 'running'}
                      <Icon name="check" size={16} color="var(--success)" />
                    {:else}
                      <svg
                        width="16"
                        height="16"
                        viewBox="0 0 16 16"
                        fill="none"
                        aria-hidden="true"
                      >
                        <circle cx="8" cy="8" r="6.5" stroke="var(--fg-dim)" stroke-width="1.5" />
                      </svg>
                    {/if}
                  </span>
                  <span class="qs-text">
                    {serviceStatus.mihomo === 'running'
                      ? $t('dash.quickstart.step4_done')
                      : $t('dash.quickstart.step4_label')}
                  </span>
                  {#if serviceStatus.mihomo !== 'running'}
                    <a
                      class="btn btn-secondary qs-cta"
                      href="#/services"
                      onclick={() => switchTab('services')}
                    >
                      {$t('dash.quickstart.step4_cta')}
                    </a>
                  {/if}
                </li>
              </ul>
            </Card>
          </div>
        {/if}

        <!-- Problems Panel (conditional) -->
        {#if (systemStats && systemStats.invalid_config) || ($capabilities !== null && !$capabilities.mihomo.api_reachable && $capabilities.mihomo.process_running) || ($capabilities !== null && !$capabilities.kernels.xray.installed && !$capabilities.kernels.mihomo.installed) || isKernelCrashed || isDiskLow || isSSLExpiring}
          <div style="margin-bottom: 18px;">
            <Card title={$t('dash.problems_panel')}>
              <div class="problems-list">
                {#if isKernelCrashed}
                  <div class="problem-item alert-error">
                    <div class="problem-content">
                      <span class="problem-icon"><Icon name="warning" size={16} /></span>
                      <div>
                        <strong class="problem-title"
                          >{$t('dash.problems.kernel_crash_title')}</strong
                        >
                        <div class="problem-desc">
                          {$t('dash.problems.kernel_crash_desc').replace(
                            '{kernel}',
                            $capabilities?.active_kernel || ''
                          )}
                        </div>
                      </div>
                    </div>
                    <Button variant="secondary" onclick={restartXkeen}>
                      {$t('dash.problems.kernel_crash_cta')}
                    </Button>
                  </div>
                {/if}

                {#if isDiskLow && systemStats && systemStats.disk}
                  <div class="problem-item alert-error">
                    <div class="problem-content">
                      <span class="problem-icon"><Icon name="warning" size={16} /></span>
                      <div>
                        <strong class="problem-title">{$t('dash.problems.disk_low_title')}</strong>
                        <div class="problem-desc">
                          {$t('dash.problems.disk_low_desc').replace(
                            '{free}',
                            formatBytes(systemStats.disk.free)
                          )}
                        </div>
                      </div>
                    </div>
                    <Button variant="secondary" onclick={() => switchTab('settings')}>
                      {$t('dash.problems.disk_low_cta')}
                    </Button>
                  </div>
                {/if}

                {#if isSSLExpiring && systemStats}
                  <div class="problem-item alert-warning">
                    <div class="problem-content">
                      <span class="problem-icon"><Icon name="warning" size={16} /></span>
                      <div>
                        <strong class="problem-title">{$t('dash.problems.ssl_expire_title')}</strong
                        >
                        <div class="problem-desc">
                          {$t('dash.problems.ssl_expire_desc').replace(
                            '{days}',
                            String(systemStats.ssl_cert_days)
                          )}
                        </div>
                      </div>
                    </div>
                  </div>
                {/if}

                {#if systemStats && systemStats.invalid_config}
                  <div class="problem-item alert-error">
                    <div class="problem-content">
                      <span class="problem-icon"><Icon name="warning" size={16} /></span>
                      <div>
                        <strong class="problem-title"
                          >{$t('dash.problems.invalid_config_title')}</strong
                        >
                        <div class="problem-desc">{$t('dash.problems.invalid_config_desc')}</div>
                      </div>
                    </div>
                    <Button variant="secondary" onclick={() => switchTab('editor')}>
                      {$t('dash.problems.invalid_config_cta')}
                    </Button>
                  </div>
                {/if}
                {#if $capabilities !== null && !$capabilities.mihomo.api_reachable && $capabilities.mihomo.process_running}
                  <div class="problem-item alert-warning">
                    <div class="problem-content">
                      <span class="problem-icon"><Icon name="warning" size={16} /></span>
                      <div>
                        <strong class="problem-title">{$t('dash.problems.mihomo_api_title')}</strong
                        >
                        <div class="problem-desc">{$t('dash.problems.mihomo_api_desc')}</div>
                      </div>
                    </div>
                    <Button
                      variant="secondary"
                      onclick={() => {
                        window.location.hash = '#/constructor';
                      }}
                    >
                      {$t('dash.problems.mihomo_api_cta')}
                    </Button>
                  </div>
                {/if}
                {#if $capabilities !== null && !$capabilities.kernels.xray.installed && !$capabilities.kernels.mihomo.installed}
                  <div class="problem-item alert-error">
                    <div class="problem-content">
                      <span class="problem-icon"><Icon name="warning" size={16} /></span>
                      <div>
                        <strong class="problem-title"
                          >{$t('dash.problems.kernel_missing_title')}</strong
                        >
                        <div class="problem-desc">{$t('dash.problems.kernel_missing_desc')}</div>
                      </div>
                    </div>
                    <Button variant="secondary" onclick={() => switchTab('services')}>
                      {$t('dash.problems.kernel_missing_cta')}
                    </Button>
                  </div>
                {/if}
              </div>
            </Card>
          </div>
        {/if}

        <!-- Live Service Status card -->
        <div style="margin-bottom: 18px;">
          <Card title={$t('dash.service_status')}>
            {#if statusLoading}
              <div class="status-badges-row">
                <div class="status-badge-item">
                  <Skeleton type="rect" width="140px" height="34px" />
                </div>
                <div class="status-badge-item">
                  <Skeleton type="rect" width="140px" height="34px" />
                </div>
                <div class="status-badge-item">
                  <Skeleton type="rect" width="140px" height="34px" />
                </div>
                <div class="status-badge-item">
                  <Skeleton type="rect" width="80px" height="34px" />
                </div>
              </div>
            {:else if statusError}
              <div class="status-error-row">
                <span><Icon name="warning" size={14} /> {$t('dash.status_error')}</span>
                <Button
                  variant="secondary"
                  onclick={handleRefresh}
                  loading={isRefreshing}
                  disabled={isRefreshing}
                  title={$t('app.refresh')}
                >
                  ↺ {$t('app.refresh')}
                </Button>
              </div>
            {:else}
              <div class="status-badges-row">
                <div class="status-badge-item">
                  <span class="status-dot {statusColor(serviceStatus.xkeen)}"></span>
                  <span class="svc-cell-stack">
                    <span class="status-badge-label">XKeen</span>
                    <span class="lbl">{$t('dash.xkeen_sub')}</span>
                  </span>
                  <span class="status-badge-value">
                    <span class="status-{statusColor(serviceStatus.xkeen)}">
                      {serviceStatus.xkeen === 'running'
                        ? $t('app.running')
                        : $t('kernel.status.stopped')}
                    </span>
                  </span>
                </div>
                <div class="status-badge-item">
                  <span class="status-dot {statusColor(serviceStatus.xray)}"></span>
                  <span class="svc-cell-stack">
                    <span class="status-badge-label">Xray</span>
                    <span class="lbl">{$t('dash.xray_sub')}</span>
                  </span>
                  <span class="status-badge-value">
                    <span class="status-{statusColor(serviceStatus.xray)}">
                      {$t('kernel.status.' + (serviceStatus.xray || 'unknown'))}
                    </span>
                    {#if serviceStatus.xrayVersion && serviceStatus.xray !== 'not_installed'}
                      <span class="version-badge">{serviceStatus.xrayVersion}</span>
                    {/if}
                  </span>
                </div>
                <div class="status-badge-item">
                  <span class="status-dot {statusColor(serviceStatus.mihomo)}"></span>
                  <span class="svc-cell-stack">
                    <span class="status-badge-label">Mihomo</span>
                    <span class="lbl">{$t('dash.mihomo_sub')}</span>
                  </span>
                  <span class="status-badge-value">
                    <span class="status-{statusColor(serviceStatus.mihomo)}">
                      {$t('kernel.status.' + (serviceStatus.mihomo || 'unknown'))}
                    </span>
                    {#if serviceStatus.mihomoVersion && serviceStatus.mihomo !== 'not_installed'}
                      <span class="version-badge">{serviceStatus.mihomoVersion}</span>
                    {/if}
                  </span>
                </div>
                <div class="status-badge-item">
                  <span class="status-dot {serviceStatus.connections > 0 ? 'success' : 'warning'}"
                  ></span>
                  <span class="svc-cell-stack">
                    <span class="status-badge-label">{$t('dash.connections')}</span>
                    <span class="lbl">{$t('dash.connections_sub')}</span>
                  </span>
                  <span class="status-badge-value mono" style="color:var(--fg-primary);">
                    {serviceStatus.connections}
                  </span>
                </div>
              </div>
            {/if}
          </Card>
        </div>

        <!-- System Resources -->
        {#if systemStats}
          <div style="margin-bottom: 18px;">
            <Card title={$t('dash.system_stats')}>
              <div class="stats-grid">
                {#if systemStats.disk}
                  <div class="stat-box">
                    <div class="stat-label">{$t('dash.disk')}</div>
                    <div class="stat-value">
                      {formatBytes(systemStats.disk.free)}
                    </div>
                    <div class="res-sub">
                      {$t('dash.disk_free', { free: formatBytes(systemStats.disk.free) })} из {formatBytes(
                        systemStats.disk.total
                      )} · {((systemStats.disk.used / systemStats.disk.total) * 100).toFixed(1)}%
                    </div>
                    <div class="stat-bar">
                      <div
                        class="stat-bar-fill"
                        style="width: {(
                          (systemStats.disk.used / systemStats.disk.total) *
                          100
                        ).toFixed(1)}%; background: {getDiskBarColor(
                          systemStats
                        )}; box-shadow: 0 0 8px {getDiskBarColor(systemStats)};"
                      ></div>
                    </div>
                  </div>
                {/if}
                <div class="stat-box">
                  <div class="stat-label">{$t('dash.ram')}</div>
                  <div class="stat-value">
                    {(systemStats.memory.used / 1024 / 1024).toFixed(2)}<span
                      style="color:var(--fg-secondary);font-size:14px;font-weight:500;margin-left:6px;"
                      >МБ</span
                    >
                  </div>
                  <div class="res-sub">
                    из {(systemStats.memory.total / 1024 / 1024).toFixed(2)} МБ · {(
                      (systemStats.memory.used / systemStats.memory.total) *
                      100
                    ).toFixed(1)}%
                  </div>
                  <div class="stat-bar">
                    <div
                      class="stat-bar-fill"
                      style="width: {(
                        (systemStats.memory.used / systemStats.memory.total) *
                        100
                      ).toFixed(1)}%"
                    ></div>
                  </div>
                </div>
                <div class="stat-box">
                  <div class="stat-label">{$t('dash.load')}</div>
                  <div class="stat-value">{systemStats.load[0].toFixed(2)}</div>
                  <div class="res-sub">
                    1м {systemStats.load[0].toFixed(2)} · 5м {systemStats.load[1].toFixed(2)} · 15м {systemStats.load[2].toFixed(
                      2
                    )}
                  </div>
                  {#if sparklineData}
                    <svg class="sparkline" viewBox="0 0 200 42" preserveAspectRatio="none">
                      <defs>
                        <linearGradient id="sg1" x1="0" y1="0" x2="0" y2="1">
                          <stop offset="0%" stop-color="#29c2f0" stop-opacity=".5" />
                          <stop offset="100%" stop-color="#29c2f0" stop-opacity="0" />
                        </linearGradient>
                      </defs>
                      <path d={sparklineData.fill} fill="url(#sg1)" />
                      <path
                        d={sparklineData.line}
                        fill="none"
                        stroke="#29c2f0"
                        stroke-width="1.5"
                      />
                    </svg>
                  {/if}
                </div>
                <div class="stat-box">
                  <div class="stat-label">{$t('dash.uptime')}</div>
                  <div class="stat-value">
                    {systemStats.uptime.days}д {systemStats.uptime.hours}ч {systemStats.uptime
                      .minutes}м
                  </div>
                  {#if systemStats.boot_time}
                    <div class="res-sub">
                      {$t('dash.uptime_since', { time: systemStats.boot_time })}
                    </div>
                  {/if}
                  <div class="stats" style="margin-top:10px;">
                    <span class="stat">{$t('dash.uptime_stable')}</span>
                    <span class="stat">{$t('dash.uptime_restarts')}</span>
                  </div>
                </div>
                <div class="stat-box">
                  <div class="stat-label">{$t('dash.goroutines')}</div>
                  <div class="stat-value">{systemStats.go_runtime.goroutines}</div>
                  <div class="res-sub">
                    heap {(systemStats.go_runtime.heap_alloc / 1024 / 1024).toFixed(1)} МБ · gc {systemStats
                      .go_runtime.num_gc} мс
                  </div>
                  {#if systemStats.go_runtime.go_version || systemStats.go_runtime.goarch}
                    <div class="stats" style="margin-top:10px;">
                      {#if systemStats.go_runtime.gomaxprocs}
                        <span class="stat"
                          >{systemStats.go_runtime.gomaxprocs}
                          {systemStats.go_runtime.go_version}</span
                        >
                      {/if}
                      {#if systemStats.go_runtime.goarch}
                        <span class="stat">{systemStats.go_runtime.goarch}</span>
                      {/if}
                    </div>
                  {/if}
                </div>
              </div>
            </Card>
          </div>
        {/if}

        <!-- System Info -->
        <div style="margin-bottom: 18px;">
          <Card title={$t('dash.system_info')}>
            <div class="info-rows">
              <div class="info-row">
                <div class="lbl">{$t('dash.info_version')}</div>
                <div class="val">{version}</div>
              </div>
              <div class="info-row">
                <div class="lbl">{$t('dash.info_version_panel')}</div>
                <div class="val">{panelVersion}</div>
              </div>
              <div class="info-row">
                <div class="lbl">{$t('dash.info_platform')}</div>
                <div class="val">{systemStats?.platform || '—'}</div>
              </div>
              <div class="info-row">
                <div class="lbl">{$t('dash.info_kernel')}</div>
                <div class="val">{systemStats?.kernel_version || '—'}</div>
              </div>
              <div class="info-row">
                <div class="lbl">{$t('dash.info_host')}</div>
                <div class="val">{systemStats?.hostname || '—'}</div>
              </div>
              <div class="info-row">
                <div class="lbl">{$t('dash.info_ip')}</div>
                <div class="val">{systemStats?.ip_interface || '—'}</div>
              </div>
              <div class="info-row">
                <div class="lbl">{$t('dash.info_timezone')}</div>
                <div class="val">{systemStats?.timezone || '—'}</div>
              </div>
              <div class="info-row">
                <div class="lbl">{$t('dash.info_config')}</div>
                <div class="val">
                  {systemStats?.config_path || '/opt/etc/xkeen/'}
                  {#if systemStats?.config_lines}
                    <span class="info-badge info-badge-orange"
                      >{pluralize(
                        systemStats.config_lines,
                        $t('dash.info_lines_one', { count: String(systemStats.config_lines) }),
                        $t('dash.info_lines_few', { count: String(systemStats.config_lines) }),
                        $t('dash.info_lines_many', { count: String(systemStats.config_lines) })
                      )}</span
                    >
                  {/if}
                </div>
              </div>
              <div class="info-row">
                <div class="lbl">{$t('dash.info_updated')}</div>
                <div class="val">{statsLastFetched || '—'}</div>
              </div>
            </div>
          </Card>
        </div>

        <!-- Quick Actions -->
        <div style="margin-bottom: 8px;">
          <Card title={$t('dash.quick_actions')}>
            <div class="qa-grid-mini">
              <!-- svelte-ignore a11y-click-events-have-key-events -->
              <!-- svelte-ignore a11y-no-static-element-interactions -->
              <div
                class="qa-mini"
                onclick={() => switchTab('proxies')}
                role="button"
                tabindex="0"
                onkeydown={(e) => e.key === 'Enter' && switchTab('proxies')}
              >
                <span class="qa-mini-ico"><Icon name="proxies" size={18} /></span>
                <span
                  ><b>{$t('nav.proxies')}</b><span class="s"
                    >{totalProxiesCount > 0
                      ? `${totalProxiesCount} узлов · ${activeProxiesCount} активных`
                      : subscriptionProxiesCount > 0
                        ? `${subscriptionProxiesCount} из подписок`
                        : 'Mihomo узлы и группы'}</span
                  ></span
                >
              </div>
              <!-- svelte-ignore a11y-click-events-have-key-events -->
              <!-- svelte-ignore a11y-no-static-element-interactions -->
              <div
                class="qa-mini"
                onclick={() => {
                  switchTab('proxies');
                  window.location.hash = '#/proxies?tab=providers';
                }}
                role="button"
                tabindex="0"
                onkeydown={(e) =>
                  e.key === 'Enter' &&
                  (switchTab('proxies'), (window.location.hash = '#/proxies?tab=providers'))}
              >
                <span class="qa-mini-ico"><Icon name="subscriptions" size={18} /></span>
                <span
                  ><b>{$t('nav.subscriptions')}</b><span class="s"
                    >{totalSubsCount > 0
                      ? `${totalSubsCount} источника${subsLastUpdated ? ' · ' + subsLastUpdated : ''}`
                      : $t('dash.subs_empty')}</span
                  ></span
                >
              </div>
              <!-- svelte-ignore a11y-click-events-have-key-events -->
              <!-- svelte-ignore a11y-no-static-element-interactions -->
              <div
                class="qa-mini"
                onclick={() => switchTab('editor')}
                role="button"
                tabindex="0"
                onkeydown={(e) => e.key === 'Enter' && switchTab('editor')}
              >
                <span class="qa-mini-ico"><Icon name="editor" size={18} /></span>
                <span
                  ><b>{$t('nav.editor')}</b><span class="s">{$t('dash.editor_subtitle')}</span
                  ></span
                >
              </div>
              <!-- svelte-ignore a11y-click-events-have-key-events -->
              <!-- svelte-ignore a11y-no-static-element-interactions -->
              <div
                class="qa-mini"
                onclick={() => switchTab('logs')}
                role="button"
                tabindex="0"
                onkeydown={(e) => e.key === 'Enter' && switchTab('logs')}
              >
                <span class="qa-mini-ico"><Icon name="logs" size={18} /></span>
                <span><b>{$t('nav.logs')}</b><span class="s">хвост последних 500 строк</span></span>
              </div>
              <!-- svelte-ignore a11y-click-events-have-key-events -->
              <!-- svelte-ignore a11y-no-static-element-interactions -->
              <div
                class="qa-mini"
                onclick={() => switchTab('dat')}
                role="button"
                tabindex="0"
                onkeydown={(e) => e.key === 'Enter' && switchTab('dat')}
              >
                <span class="qa-mini-ico"><Icon name="dat" size={18} /></span>
                <span><b>{$t('nav.dat')}</b><span class="s">geoip · geosite · правила</span></span>
              </div>
              <!-- svelte-ignore a11y-click-events-have-key-events -->
              <!-- svelte-ignore a11y-no-static-element-interactions -->
              <div
                class="qa-mini"
                onclick={() => switchTab('console')}
                role="button"
                tabindex="0"
                onkeydown={(e) => e.key === 'Enter' && switchTab('console')}
              >
                <span class="qa-mini-ico"><Icon name="console" size={18} /></span>
                <span><b>{$t('nav.console')}</b><span class="s">shell в окружении XKeen</span></span
                >
              </div>
            </div>
          </Card>
        </div>
      </div>
    {:else if currentTab === 'editor'}
      <div transition:fade={{ duration: 150 }}>
        <Editor onSwitchTab={switchTab} />
      </div>
    {:else if currentTab === 'logs'}
      <div transition:fade={{ duration: 150 }}>
        <Logs />
      </div>
    {:else if currentTab === 'proxies'}
      <div transition:fade={{ duration: 150 }}>
        <Proxies />
      </div>
    {:else if currentTab === 'connections'}
      <div transition:fade={{ duration: 150 }}>
        <Connections />
      </div>
    {:else if currentTab === 'rules'}
      <div transition:fade={{ duration: 150 }}>
        <Rules />
      </div>
    {:else if currentTab === 'traffic'}
      <div transition:fade={{ duration: 150 }}>
        <Traffic />
      </div>
    {:else if currentTab === 'services'}
      <div transition:fade={{ duration: 150 }}>
        <Services onSwitchTab={switchTab} />
      </div>
    {:else if currentTab === 'smartproxy'}
      <div transition:fade={{ duration: 150 }}>
        <SmartProxy onSwitchTab={switchTab} />
      </div>
    {:else if currentTab === 'trafficquotas'}
      <div transition:fade={{ duration: 150 }}>
        <TrafficQuotas onSwitchTab={switchTab} />
      </div>
    {:else if currentTab === 'dat'}
      <div transition:fade={{ duration: 150 }}>
        <DATManager onSwitchTab={switchTab} />
      </div>
    {:else if currentTab === 'mihomo-gen'}
      <div transition:fade={{ duration: 150 }}>
        <MihomoGenerator onSwitchTab={switchTab} />
      </div>
    {:else if currentTab === 'console'}
      <div transition:fade={{ duration: 150 }}>
        <Console onSwitchTab={switchTab} />
      </div>
    {:else if currentTab === 'network'}
      <div transition:fade={{ duration: 150 }}>
        <NetworkTools onSwitchTab={switchTab} />
      </div>
    {:else if currentTab === 'settings'}
      <div transition:fade={{ duration: 150 }}>
        <Settings onSwitchTab={switchTab} />
      </div>
    {/if}
  </div>
</div>

<Toast />
<ConfirmDialog />

<style>
  /* Status badges — matches reference: flush grid inside card with dividers */
  .status-badges-row {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(200px, 1fr));
    gap: 0;
    margin: -18px -24px -24px;
    border-top: 1px solid var(--border);
  }

  .status-badge-item {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 14px 20px;
    border-right: 1px solid var(--border);
    border-bottom: 1px solid var(--border);
    font-size: 13px;
  }

  .status-badge-item:last-child {
    border-right: 0;
  }
  .status-badge-item:nth-last-child(-n + 4) {
    border-bottom: 0;
  }

  .svc-cell-stack {
    display: flex;
    flex-direction: column;
    line-height: 1.25;
  }

  .svc-cell-stack .lbl {
    font-size: 11.5px;
    color: var(--fg-dim);
    margin-top: 2px;
  }

  .status-badge-label {
    font-weight: 700;
    color: var(--fg-primary);
    font-size: 13px;
  }

  .status-badge-value {
    display: flex;
    flex-direction: column;
    align-items: flex-end;
    gap: 4px;
    margin-left: auto;
    flex-shrink: 0;
  }

  .status-success {
    color: var(--success);
  }
  .status-error {
    color: var(--danger);
  }
  .status-warning {
    color: var(--warning);
  }

  .version-badge {
    font-family: var(--font-family-mono);
    font-size: 10px;
    color: var(--fg-dim);
    letter-spacing: 0.03em;
    background: rgba(255, 255, 255, 0.04);
    border: 1px solid var(--border);
    border-radius: 3px;
    padding: 1px 6px;
  }

  .status-error-row {
    display: flex;
    align-items: center;
    gap: 12px;
    color: var(--danger);
    padding: 14px 20px;
  }

  /* Quick actions grid */
  .qa-grid-mini {
    display: grid;
    grid-template-columns: repeat(3, 1fr);
    gap: 12px;
  }

  .qa-mini {
    display: flex;
    gap: 12px;
    align-items: center;
    padding: 14px;
    border: 1px solid var(--border);
    background: var(--bg-elevated);
    border-radius: var(--radius-md);
    cursor: pointer;
    transition: all 0.15s;
    text-decoration: none;
    color: inherit;
  }

  .qa-mini:hover {
    border-color: var(--accent-line);
    transform: translateY(-1px);
    box-shadow: 0 14px 28px -18px rgba(41, 194, 240, 0.45);
  }

  .qa-mini-ico {
    width: 36px;
    height: 36px;
    border-radius: 8px;
    display: grid;
    place-items: center;
    background: var(--accent-soft);
    color: var(--accent);
    border: 1px solid var(--accent-line);
    flex: 0 0 36px;
  }

  .qa-mini b {
    color: var(--fg-primary);
    font-weight: 700;
    font-size: 13.5px;
    display: block;
  }

  .qa-mini .s {
    color: var(--fg-dim);
    font-size: 11.5px;
    display: block;
    margin-top: 2px;
  }

  /* Info rows */
  .info-rows {
    display: grid;
    grid-template-columns: repeat(2, 1fr);
    margin: -18px -24px -24px;
    border-top: 1px solid var(--border);
  }

  .info-row {
    display: flex;
    gap: 14px;
    align-items: center;
    padding: 13px 20px;
    border-bottom: 1px solid var(--border);
    border-right: 1px solid var(--border);
  }

  .info-row:nth-child(2n) {
    border-right: 0;
  }
  .info-row:nth-last-child(-n + 2) {
    border-bottom: 0;
  }

  .info-row .lbl {
    color: var(--fg-secondary);
    font-size: 12.5px;
    min-width: 130px;
  }

  .info-row .val {
    color: var(--fg-primary);
    font-family: var(--font-family-mono);
    font-size: 13px;
  }

  /* Page header — title left, buttons top-right */
  .page-head {
    display: flex;
    justify-content: space-between;
    align-items: flex-start;
    gap: 16px;
    margin-bottom: 20px;
    flex-wrap: wrap;
  }

  .crumbs {
    font-size: 11px;
    font-weight: 700;
    letter-spacing: 0.18em;
    text-transform: uppercase;
    color: var(--fg-dim);
    margin-bottom: 6px;
  }

  .sub {
    color: var(--fg-secondary);
    font-size: 13px;
    margin: 4px 0 0;
  }

  /* Sub-text under stat values */
  .res-sub {
    font-size: 11.5px;
    color: var(--fg-dim);
    margin-top: 6px;
    font-family: var(--font-family-mono);
  }

  /* ph-actions */
  .ph-actions {
    display: flex;
    gap: 10px;
    align-items: center;
    flex-shrink: 0;
    padding-top: 4px;
  }

  /* sparkline */
  .sparkline {
    display: block;
    width: 100%;
    height: 42px;
    margin-top: 10px;
    overflow: visible;
  }

  /* Info badges inside info-row — match reference .pill */
  .info-badge {
    display: inline-block;
    font-size: 10.5px;
    font-weight: 600;
    padding: 1px 7px;
    border-radius: 3px;
    margin-left: 6px;
    vertical-align: middle;
    font-family: var(--font-family-mono);
    letter-spacing: 0.02em;
  }

  /* "latest" badge — uses accent color vars from design system */
  .info-badge-teal {
    background: var(--accent-soft);
    color: var(--accent);
    border: 1px solid var(--accent-line);
  }

  /* config lines badge — warning/orange */
  .info-badge-orange {
    background: rgba(255, 138, 0, 0.1);
    color: var(--warning, #f59e0b);
    border: 1px solid rgba(255, 138, 0, 0.2);
  }

  /* Quickstart checklist card */
  .quickstart-list {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: var(--spacing-2, 8px);
  }

  .qs-step {
    display: flex;
    align-items: center;
    gap: var(--spacing-2, 8px);
    padding: var(--spacing-1, 4px) 0;
  }

  .qs-icon {
    display: inline-flex;
    align-items: center;
    flex-shrink: 0;
  }

  .qs-text {
    font-size: 13px;
    color: var(--fg-primary);
    flex: 1;
  }

  .qs-step--done .qs-text {
    color: var(--fg-secondary);
  }

  .qs-cta {
    font-size: 12px;
    padding: 4px 8px;
    margin-left: auto;
    flex-shrink: 0;
  }
</style>
