<script lang="ts">
  import { onMount } from 'svelte';
  import { fade } from 'svelte/transition';
  import { t, currentLang, pluralize } from './i18n';
  import {
    isSidebarOpen,
    isSidebarCollapsed,
    capabilities,
    fetchCapabilities,
    showToast,
    mihomoApiAvailable
  } from './stores';
  import { usePoller } from './lib/poller';
  import { apiFetch, apiFetchJSON } from './lib/api';
  import { isServiceRestarting, activateRestartGrace } from './lib/serviceGrace';
  import Sidebar from './components/Sidebar.svelte';
  import Toast from './components/Toast.svelte';
  import ConfirmDialog from './components/ConfirmDialog.svelte';
  import Card from './components/Card.svelte';
  import Button from './components/Button.svelte';
  import Icon from './lib/components/Icon.svelte';
  import Skeleton from './components/Skeleton.svelte';
  import ApiOffline from './components/ApiOffline.svelte';
  import EmptyState from './components/EmptyState.svelte';
  import MihomoSocketMigrateModal from './components/mihomo/MihomoSocketMigrateModal.svelte';
  import UnsavedChangesModal from './components/UnsavedChangesModal.svelte';
  import SystemStatusCapsule from './components/status/SystemStatusCapsule.svelte';
  import { capsuleConfigStore } from './lib/capsuleSettings';
  import {
    isAnySourceDirty,
    getDirtySources,
    saveAllDirtySources,
    hasUnsavedChanges
  } from './lib/dirtyRegistry';

  let version = $state($t('app.loading'));
  let panelVersion = $state($t('app.loading'));
  let loading = $state(false);
  let showMihomoMigrateModal = $state(false);
  let showUnsavedModal = $state(false);
  let pendingTargetTab = $state<string | null>(null);
  let pendingTargetHash = $state<string | null>(null);
  let isSavingAndNavigating = $state(false);
  let dirtySourceNames = $state<string[]>([]);
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

  // Mobile drawer modal gate (D-10): drawer behaves as an accessible modal dialog
  // only when isMobile (≤768px) AND the drawer is open. Desktop keeps the sidebar
  // as a plain, non-modal neighbor of the content.
  let isMobile = $state(window.matchMedia('(max-width: 768px)').matches);
  const drawerIsModal = $derived(isMobile && $isSidebarOpen);

  let sidebarEl: HTMLElement | null = $state(null);
  let previouslyFocusedElement: HTMLElement | null = null;
  let lockedScrollY = 0;

  // Dashboard live monitoring state
  interface Kernel {
    name: string;
    current_version?: string;
    process_status?: string;
  }

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
      (($capabilities.active_kernel === 'mihomo' &&
        (serviceStatus.mihomo === 'stopped' || serviceStatus.mihomo === 'error')) ||
        ($capabilities.active_kernel === 'xray' &&
          (serviceStatus.xray === 'stopped' || serviceStatus.xray === 'error')))
  );

  const isDiskLow = $derived(
    systemStats !== null && systemStats.disk && systemStats.disk.free < 10 * 1024 * 1024
  );

  const isSSLExpiring = $derived(
    systemStats !== null && systemStats.ssl_cert_days >= 0 && systemStats.ssl_cert_days < 7
  );

  function getDiskBarColor(stats: SystemStats): string {
    if (!stats.disk) {
      return 'var(--success)';
    }
    const pct = (stats.disk.used / stats.disk.total) * 100;
    const freeMB = stats.disk.free / 1024 / 1024;
    if (pct > 90 || freeMB < 10) {
      return 'var(--danger)';
    }
    if (pct > 80) {
      return 'var(--warning)';
    }
    return 'var(--success)';
  }

  async function fetchSubscriptionSummary(signal?: AbortSignal) {
    try {
      const res = await apiFetch('/api/subscriptions', { signal });
      if (res.ok) {
        const envelope = await res.json();
        const rawList = Array.isArray(envelope) ? envelope : (envelope?.data ?? []);
        const subs = Array.isArray(rawList) ? rawList : [];
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
    } catch (e: any) {
      if (e?.name === 'AbortError') return;
      if (e?.status === 401) return;
      console.error('fetchSubscriptionSummary failed:', e);
    }
  }

  async function fetchProxySummary(signal?: AbortSignal) {
    try {
      const res = await apiFetch('/api/mihomo/proxy/proxies', { signal });
      if (res.ok) {
        const data = await res.json();
        const proxies = data.proxies || {};
        const keys = Object.keys(proxies);
        const nodeKeys = keys.filter(
          (k) => proxies[k].type !== 'Selector' && proxies[k].type !== 'URLTest'
        );
        totalProxiesCount = nodeKeys.length;
        activeProxiesCount = nodeKeys.filter((k) => proxies[k].alive !== false).length;
      }
    } catch (e: any) {
      if (e?.name === 'AbortError') return;
      if (e?.status === 401) return;
      console.error('fetchProxySummary failed:', e);
    }
  }

  // WR-04: a plain "running"/"запущен" substring match also matches its own
  // negation ("not running" / "не запущен"), misreporting a stopped service
  // as running. Require the positive word AND the absence of a "not"/"не"
  // immediately preceding it.
  function guessXkeenRunning(text: string): boolean {
    const lower = text.toLowerCase();
    const hasRunningWord =
      /\brunning\b/.test(lower) ||
      /[\u0437][\u0430][\u043F][\u0443][\u0449][\u0435][\u043D]/.test(lower);
    const isNegated =
      /\bnot\s+running\b/.test(lower) ||
      /[\u043D][\u0435]\s*[\u0437][\u0430][\u043F][\u0443][\u0449][\u0435][\u043D]/.test(lower);
    return hasRunningWord && !isNegated;
  }

  async function fetchLiveStatus(signal?: AbortSignal) {
    statusError = false;
    try {
      const [svcRes, mihomoRes] = await Promise.allSettled([
        apiFetch('/api/service/status', { signal }),
        apiFetch('/api/mihomo/status', { signal })
      ]);
      // WR-03: allSettled never rejects, so a total outage must be derived
      // explicitly here rather than relying on the outer catch below (which
      // every individually-shielded fetch in this function makes unreachable).
      statusError = svcRes.status === 'rejected' && mihomoRes.status === 'rejected';

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
            isXkeenRunning = guessXkeenRunning(text);
          }
        } catch (_) {
          xkeenRaw = text;
          isXkeenRunning = guessXkeenRunning(text);
        }
      }

      const mihomoText =
        mihomoRes.status === 'fulfilled' && mihomoRes.value.ok ? await mihomoRes.value.text() : '';

      // Try to get connection count from mihomo
      let connCount = 0;
      try {
        const connRes = await apiFetch('/api/mihomo/proxy/connections?limit=1', { signal });
        if (connRes.ok) {
          const connData = await connRes.json();
          connCount = connData?.connections?.length ?? 0;
        }
      } catch (e: any) {
        if (e?.status === 401) return;
      }

      // Get kernel versions and process_status from /api/kernels
      let xrayVer = '';
      let mihomoVer = '';
      let xrayProcessStatus = 'unknown';
      let mihomoProcessStatus = 'unknown';
      try {
        const kernels = await apiFetchJSON<Kernel[]>('/api/kernels', { signal });
        if (Array.isArray(kernels)) {
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
      } catch (e: any) {
        if (e?.status === 401) return;
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
    } catch (e: any) {
      if (e?.name === 'AbortError') return;
      if (e?.status === 401) return;
      statusError = true;
      serviceStatus = { ...serviceStatus, xray: 'error', mihomo: 'error' };
    } finally {
      statusLoading = false;
    }
  }

  async function fetchSystemStats(signal?: AbortSignal) {
    try {
      const res = await apiFetch('/api/system/stats', { signal });
      if (res.ok) {
        systemStats = await res.json();
        if (systemStats) {
          loadHistory = [...loadHistory, systemStats.load[0]].slice(-16);
          const d = new Date();
          const p = (n: number) => n.toString().padStart(2, '0');
          statsLastFetched = `${p(d.getDate())}.${p(d.getMonth() + 1)}.${String(d.getFullYear()).slice(2)} ${p(d.getHours())}:${p(d.getMinutes())}`;
        }
      }
    } catch (e: any) {
      if (e?.name === 'AbortError') return;
      if (e?.status === 401) return;
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
      const data = await apiFetchJSON<{ version: string; panel_version: string }>('/api/version');
      version = data.version;
      panelVersion = data.panel_version;
    } catch (e: any) {
      if (e?.status === 401) return;
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
      await apiFetch('/api/auth/logout', {
        method: 'POST'
      });
      localStorage.removeItem('csrf_token');
      window.location.href = '/';
    } catch (e: any) {
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
    const targetTab = getTabFromHash();
    if (targetTab !== currentTab && isAnySourceDirty()) {
      pendingTargetTab = targetTab;
      pendingTargetHash = window.location.hash;
      dirtySourceNames = getDirtySources().map((s) => s.source.name || s.id);

      // Revert hash in URL to currentTab without re-triggering hashchange handling
      history.replaceState(null, '', `/#/${currentTab}`);
      showUnsavedModal = true;
      return;
    }
    currentTab = targetTab;
  }

  async function handleSaveAndLeave() {
    isSavingAndNavigating = true;
    try {
      const ok = await saveAllDirtySources();
      if (ok) {
        showUnsavedModal = false;
        const target = pendingTargetTab;
        const hash = pendingTargetHash;
        pendingTargetTab = null;
        pendingTargetHash = null;
        if (target) {
          currentTab = target;
          window.location.hash = hash || '#/' + target;
        }
      } else {
        showToast('error', $t('nav.save_error'));
      }
    } catch (e: any) {
      console.error('Save error during navigation:', e);
      showToast('error', $t('nav.save_error'));
    } finally {
      isSavingAndNavigating = false;
    }
  }

  function handleLeaveWithoutSaving() {
    showUnsavedModal = false;
    const target = pendingTargetTab;
    const hash = pendingTargetHash;
    pendingTargetTab = null;
    pendingTargetHash = null;
    if (target) {
      currentTab = target;
      window.location.hash = hash || '#/' + target;
    }
  }

  function handleStay() {
    showUnsavedModal = false;
    pendingTargetTab = null;
    pendingTargetHash = null;
  }

  function handleBeforeUnload(e: BeforeUnloadEvent) {
    if (isAnySourceDirty()) {
      e.preventDefault();
      e.returnValue = '';
      return '';
    }
  }

  function getDrawerFocusables(): HTMLElement[] {
    if (!sidebarEl) return [];
    const selectors =
      'summary, button, [href], input, select, textarea, [tabindex]:not([tabindex="-1"])';
    return Array.from(sidebarEl.querySelectorAll(selectors)).filter(
      (el) => !(el as HTMLElement).closest('details:not([open])')
    ) as HTMLElement[];
  }

  function handleDrawerKeydown(e: KeyboardEvent) {
    // Gate on drawerIsModal (single gate, D-10 prohibition #2): on desktop the
    // sidebar is a persistent, non-modal panel — Escape-close and Tab-trap must
    // stay inert there so desktop keyboard navigation never regresses.
    if (!drawerIsModal) return;
    if (e.key === 'Escape') {
      closeSidebar();
      return;
    }
    if (e.key === 'Tab') {
      const focusables = getDrawerFocusables();
      if (focusables.length === 0) {
        e.preventDefault();
        return;
      }
      const first = focusables[0];
      const last = focusables[focusables.length - 1];
      const active = document.activeElement;
      if (e.shiftKey) {
        if (active === first) {
          last.focus();
          e.preventDefault();
        }
      } else {
        if (active === last) {
          first.focus();
          e.preventDefault();
        }
      }
    }
  }

  // Focus save/restore for the mobile drawer-as-modal (mirrors Modal.svelte).
  $effect(() => {
    if (drawerIsModal) {
      previouslyFocusedElement = document.activeElement as HTMLElement;
      const timerId = setTimeout(() => {
        if (sidebarEl) {
          const focusables = getDrawerFocusables();
          if (focusables.length > 0) focusables[0].focus();
          else sidebarEl.focus();
        }
      }, 0);
      return () => clearTimeout(timerId);
    } else if (previouslyFocusedElement) {
      previouslyFocusedElement.focus();
      previouslyFocusedElement = null;
    }
  });

  // scrollY-preserving body scroll-lock (RESEARCH §2), reactive on drawerIsModal so
  // it auto-releases on close and on nav-item auto-close (pitfall 6 — no stuck state).
  $effect(() => {
    if (drawerIsModal) {
      lockedScrollY = window.scrollY;
      document.body.style.top = `-${lockedScrollY}px`;
      document.body.classList.add('drawer-locked');
    } else {
      document.body.classList.remove('drawer-locked');
      document.body.style.top = '';
      window.scrollTo(0, lockedScrollY);
    }
    return () => {
      document.body.classList.remove('drawer-locked');
      document.body.style.top = '';
    };
  });

  let chunkReloadKey = $state(0);
  let lastChunkErrorTab: string | null = null;

  // D-04 retry: browsers permanently cache a failed dynamic import() for a
  // given module specifier for the lifetime of the document (confirmed:
  // re-invoking import() on the same URL after a failure resolves to the
  // same rejected module-map entry, with no new network request). A soft
  // in-place retry can therefore never actually recover — only a full
  // reload re-attempts the fetch against a fresh module graph. `chunkReloadKey`
  // is still bumped (harmless, keeps the {#await} block's identity fresh for
  // any tab switched away/back to), but the real recovery mechanism is reload.
  function retryChunkLoad() {
    lastChunkErrorTab = null;
    chunkReloadKey++;
    window.location.reload();
  }

  function reportChunkError(err: unknown): void {
    console.error('Failed to load lazy chunk for tab', currentTab, err);
    if (lastChunkErrorTab === currentTab) return;
    lastChunkErrorTab = currentTab;
    showToast('error', $t('app.chunk_load_failed'), 0, {
      label: $t('app.retry'),
      onClick: retryChunkLoad
    });
  }

  function reportChunkErrorAction(_node: HTMLElement, err: unknown): void {
    reportChunkError(err);
  }

  function switchTab(tab: string) {
    lastChunkErrorTab = null;
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
    activateRestartGrace(6000);
    try {
      const res = await apiFetch('/api/service/control?action=restart', {
        method: 'POST'
      });
      if (res.ok) {
        showToast('success', $t('app.restart') + ' XKeen...');
        setTimeout(fetchLiveStatus, 3000);
      } else {
        showToast('error', $t('app.error'));
      }
    } catch (e: any) {
      if (e?.status === 401) return;
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
    fetchProxySummary();

    currentTab = getTabFromHash();
    window.addEventListener('hashchange', handleHashChange);
    if (!window.location.hash) {
      window.location.hash = '#/' + currentTab;
    }

    const mobileMql = window.matchMedia('(max-width: 768px)');
    const handleMobileMqlChange = (e: MediaQueryListEvent) => {
      isMobile = e.matches;
      if (!isMobile) {
        isSidebarOpen.set(false);
      }
    };
    mobileMql.addEventListener('change', handleMobileMqlChange);

    usePoller((signal) => fetchLiveStatus(signal), 10000);
    usePoller((signal) => fetchSystemStats(signal), 5000);
    usePoller((signal) => fetchCapabilities(signal), 10000);
    usePoller((signal) => fetchSubscriptionSummary(signal), 30000);
    usePoller((signal) => fetchProxySummary(signal), 30000);
    const handleBeforeInstallPrompt = (e: Event) => {
      e.preventDefault();
      pwaInstallPrompt = e;
    };
    window.addEventListener('beforeinstallprompt', handleBeforeInstallPrompt);

    // Production builds wrap every `import()` in Vite's own preload helper,
    // which does not reliably surface the failure to the `{:catch}` branch
    // of a lazy-chunk `{#await}` block (the promise rejection observed by
    // Svelte's await block can be swallowed by the preload machinery). Vite's own
    // documented mechanism for lazy-chunk load failures is this global event;
    // `preventDefault()` stops it from becoming an uncaught window error, and
    // we route it through the same `reportChunkError` toast+retry pipeline.
    const handlePreloadError = (event: Event) => {
      event.preventDefault();
      reportChunkError((event as Event & { payload?: unknown }).payload);
    };
    window.addEventListener('vite:preloadError', handlePreloadError);
    window.addEventListener('beforeunload', handleBeforeUnload);

    return () => {
      window.removeEventListener('hashchange', handleHashChange);
      mobileMql.removeEventListener('change', handleMobileMqlChange);
      window.removeEventListener('beforeinstallprompt', handleBeforeInstallPrompt);
      window.removeEventListener('vite:preloadError', handlePreloadError);
      window.removeEventListener('beforeunload', handleBeforeUnload);
    };
  });
</script>

<div class="dashboard-layout" class:editor-active={currentTab === 'editor'}>
  <!-- Mobile header bar -->
  <header class="mobile-header" inert={drawerIsModal}>
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
    <span id="mobile-header-title" style="font-weight: 600; font-size: 16px;">XKeen CP</span>
    {#if $capsuleConfigStore.visible}
      <SystemStatusCapsule
        variant="mobile"
        {systemStats}
        activeKernel={$capabilities?.active_kernel}
        isXkeenRunning={serviceStatus.xkeen === 'running'}
        onSwitchTab={switchTab}
      />
    {:else}
      <span style="width: 34px;"></span>
    {/if}
  </header>

  <!-- Off-canvas overlay (mobile only) -->
  <div
    class="sidebar-overlay"
    class:hidden={!$isSidebarOpen}
    onclick={closeSidebar}
    role="presentation"
  ></div>

  <!-- Sidebar -->
  <div
    class="sidebar"
    class:sidebar-open={$isSidebarOpen}
    class:rail={$isSidebarCollapsed}
    style="display: flex; flex-direction: column;"
    bind:this={sidebarEl}
    onkeydown={handleDrawerKeydown}
    role={drawerIsModal ? 'dialog' : undefined}
    aria-modal={drawerIsModal ? 'true' : undefined}
    aria-labelledby={drawerIsModal ? 'mobile-header-title' : undefined}
    tabindex="-1"
    inert={isMobile && !$isSidebarOpen}
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
      {systemStats}
      isXkeenRunning={serviceStatus.xkeen === 'running'}
    />
  </div>

  <!-- Main content area -->
  <div
    class="main-content"
    class:editor-active={currentTab === 'editor'}
    class:rail={$isSidebarCollapsed}
    inert={drawerIsModal}
  >
    <!-- Mihomo offline warning banner / Restarting notice -->
    {#if mihomoDependentTabs.includes(currentTab) && $capabilities !== null && !$capabilities.mihomo.reachable}
      {#if $isServiceRestarting}
        <div
          class="service-restarting-banner"
          style="margin: 12px 16px 0; padding: 12px 18px; background: rgba(56, 189, 248, 0.1); border: 1px solid var(--accent); border-radius: var(--radius-md); display: flex; align-items: center; gap: 12px; font-size: 13.5px; color: var(--fg-primary);"
        >
          <span
            class="spinner"
            style="width: 16px; height: 16px; border: 2px solid var(--accent); border-top-color: transparent; border-radius: 50%; animation: spin 1s linear infinite; flex-shrink: 0;"
          ></span>
          <span>{$t('service.restarting_wait')}</span>
        </div>
      {:else}
        <div style="margin: 12px 16px 0;">
          <ApiOffline
            endpoint={$capabilities.mihomo.discovered_secret ? 'Mihomo API' : '127.0.0.1:9090'}
            lastSeenSeconds={0}
            onRetry={fetchCapabilities}
          />
        </div>
      {/if}
    {/if}

    {#key chunkReloadKey}
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
                <svg
                  width="14"
                  height="14"
                  viewBox="0 0 24 24"
                  fill="currentColor"
                  aria-hidden="true"><path d="M13 2 4 14h7l-1 8 10-13h-7z" /></svg
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
                    <span class="qs-icon" aria-label={$t('dash.quickstart.step_done')}>
                      <Icon name="check" size={16} color="var(--success)" />
                    </span>
                    <span class="qs-text">{$t('dash.quickstart.step1_label')}</span>
                  </li>
                  <!-- Step 2: subscription added -->
                  <li class="qs-step" class:qs-step--done={hasSubscription}>
                    <span
                      class="qs-icon"
                      aria-label={hasSubscription
                        ? $t('dash.quickstart.step_done')
                        : $t('dash.quickstart.step_pending')}
                    >
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
                      <a class="btn btn-secondary qs-cta" href="#/proxies?tab=providers">
                        {$t('dash.quickstart.step2_cta')}
                      </a>
                    {/if}
                  </li>
                  <!-- Step 3: config applied (Mihomo API reachable) -->
                  <li class="qs-step" class:qs-step--done={$mihomoApiAvailable}>
                    <span
                      class="qs-icon"
                      aria-label={$mihomoApiAvailable
                        ? $t('dash.quickstart.step_done')
                        : $t('dash.quickstart.step_pending')}
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
                      aria-label={serviceStatus.mihomo === 'running'
                        ? $t('dash.quickstart.step_done')
                        : $t('dash.quickstart.step_pending')}
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
          {#if (systemStats && systemStats.invalid_config) || ($capabilities !== null && !$capabilities.mihomo.api_reachable && $capabilities.mihomo.process_running) || ($capabilities !== null && !$capabilities.kernels?.xray?.installed && !$capabilities.kernels?.mihomo?.installed) || ($capabilities !== null && $capabilities.mihomo?.is_insecure_lan) || isKernelCrashed || isDiskLow || isSSLExpiring}
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
                            {$t('dash.problems.kernel_crash_desc', {
                              kernel: $capabilities?.active_kernel || ''
                            })}
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
                          <strong class="problem-title">{$t('dash.problems.disk_low_title')}</strong
                          >
                          <div class="problem-desc">
                            {$t('dash.problems.disk_low_desc', {
                              free: formatBytes(systemStats.disk.free)
                            })}
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
                          <strong class="problem-title"
                            >{$t('dash.problems.ssl_expire_title')}</strong
                          >
                          <div class="problem-desc">
                            {$t('dash.problems.ssl_expire_desc', {
                              days: systemStats.ssl_cert_days
                            })}
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
                          <strong class="problem-title"
                            >{$t('dash.problems.mihomo_api_title')}</strong
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
                  {#if $capabilities !== null && $capabilities.mihomo?.is_insecure_lan}
                    <div class="problem-item alert-warning">
                      <div class="problem-content">
                        <span class="problem-icon"><Icon name="warning" size={16} /></span>
                        <div>
                          <strong class="problem-title"
                            >{$t('dash.problems.mihomo_insecure_title')}</strong
                          >
                          <div class="problem-desc">{$t('dash.problems.mihomo_insecure_desc')}</div>
                        </div>
                      </div>
                      <Button variant="secondary" onclick={() => (showMihomoMigrateModal = true)}>
                        {$t('mihomo.migrate_btn')}
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
                      {#if $capabilities?.mihomo?.is_insecure_lan}
                        <button
                          class="badge badge-warning"
                          style="margin-left: 6px; cursor: pointer; border: none; font-size: 11px; padding: 2px 6px;"
                          onclick={() => (showMihomoMigrateModal = true)}
                          title={$t('mihomo.migrate_banner_body')}
                        >
                          {$t('mihomo.controller_mode_insecure')}
                        </button>
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
                        {$t('dash.disk_free', { free: formatBytes(systemStats.disk.free) })}
                        {$t('dash.disk_of_total_pct', {
                          total: formatBytes(systemStats.disk.total),
                          pct: ((systemStats.disk.used / systemStats.disk.total) * 100).toFixed(1)
                        })}
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
                        >{$t('dash.unit_mb')}</span
                      >
                    </div>
                    <div class="res-sub">
                      {$t('dash.ram_of_total_pct', {
                        total: (systemStats.memory.total / 1024 / 1024).toFixed(2),
                        pct: ((systemStats.memory.used / systemStats.memory.total) * 100).toFixed(1)
                      })}
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
                      {$t('dash.load_avg_line', {
                        v1: systemStats.load[0].toFixed(2),
                        v2: systemStats.load[1].toFixed(2),
                        v3: systemStats.load[2].toFixed(2)
                      })}
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
                      {$t('dash.uptime_dhm', {
                        days: systemStats.uptime.days,
                        hours: systemStats.uptime.hours,
                        minutes: systemStats.uptime.minutes
                      })}
                    </div>
                    {#if systemStats.boot_time}
                      <div class="res-sub">
                        {$t('dash.uptime_since', { time: systemStats.boot_time })}
                      </div>
                    {/if}
                    <div class="stats" style="margin-top:10px;">
                      <span class="stat">{$t('dash.uptime_stable')}</span>
                    </div>
                  </div>
                  <div class="stat-box">
                    <div class="stat-label">{$t('dash.goroutines')}</div>
                    <div class="stat-value">{systemStats.go_runtime.goroutines}</div>
                    <div class="res-sub">
                      {$t('dash.goroutines_heap_gc', {
                        heap: (systemStats.go_runtime.heap_alloc / 1024 / 1024).toFixed(1),
                        gc: systemStats.go_runtime.num_gc
                      })}
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
                          $t('dash.info_lines_many', { count: String(systemStats.config_lines) }),
                          $currentLang
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
                <button type="button" class="qa-mini" onclick={() => switchTab('proxies')}>
                  <span class="qa-mini-ico"><Icon name="proxies" size={18} /></span>
                  <span
                    ><b>{$t('nav.proxies')}</b><span class="s"
                      >{totalProxiesCount > 0
                        ? $t('dash.proxies_summary', {
                            total: totalProxiesCount,
                            active: activeProxiesCount
                          })
                        : subscriptionProxiesCount > 0
                          ? $t('dash.proxies_from_subs', { count: subscriptionProxiesCount })
                          : $t('dash.proxies_placeholder')}</span
                    ></span
                  >
                </button>
                <button
                  type="button"
                  class="qa-mini"
                  onclick={() => {
                    switchTab('proxies');
                    window.location.hash = '#/proxies?tab=providers';
                  }}
                >
                  <span class="qa-mini-ico"><Icon name="subscriptions" size={18} /></span>
                  <span
                    ><b>{$t('nav.subscriptions')}</b><span class="s"
                      >{totalSubsCount > 0
                        ? `${$t('dash.subs_count', { count: totalSubsCount })}${subsLastUpdated ? ' · ' + subsLastUpdated : ''}`
                        : $t('dash.subs_empty')}</span
                    ></span
                  >
                </button>
                <button type="button" class="qa-mini" onclick={() => switchTab('editor')}>
                  <span class="qa-mini-ico"><Icon name="editor" size={18} /></span>
                  <span
                    ><b>{$t('nav.editor')}</b><span class="s">{$t('dash.editor_subtitle')}</span
                    ></span
                  >
                </button>
                <button type="button" class="qa-mini" onclick={() => switchTab('logs')}>
                  <span class="qa-mini-ico"><Icon name="logs" size={18} /></span>
                  <span
                    ><b>{$t('nav.logs')}</b><span class="s">{$t('dash.logs_subtitle')}</span></span
                  >
                </button>
                <button type="button" class="qa-mini" onclick={() => switchTab('dat')}>
                  <span class="qa-mini-ico"><Icon name="dat" size={18} /></span>
                  <span><b>{$t('nav.dat')}</b><span class="s">{$t('dash.dat_subtitle')}</span></span
                  >
                </button>
                <button type="button" class="qa-mini" onclick={() => switchTab('console')}>
                  <span class="qa-mini-ico"><Icon name="console" size={18} /></span>
                  <span
                    ><b>{$t('nav.console')}</b><span class="s">{$t('dash.console_subtitle')}</span
                    ></span
                  >
                </button>
              </div>
            </Card>
          </div>
        </div>
      {:else if currentTab === 'editor'}
        {#await import('./Editor.svelte')}
          <Skeleton type="card" height="100%" />
        {:then { default: Editor }}
          <div
            style="flex: 1; display: flex; flex-direction: column; min-height: 0; height: 100%;"
            transition:fade={{ duration: 150 }}
          >
            <Editor onSwitchTab={switchTab} />
          </div>
        {:catch err}
          <div use:reportChunkErrorAction={err}>
            <EmptyState
              title={$t('app.chunk_load_failed')}
              description=""
              ctaText={$t('app.retry')}
              oncta={retryChunkLoad}
            />
          </div>
        {/await}
      {:else if currentTab === 'logs'}
        {#await import('./Logs.svelte')}
          <Skeleton type="card" height="60vh" />
        {:then { default: Logs }}
          <div transition:fade={{ duration: 150 }}>
            <Logs />
          </div>
        {:catch err}
          <div use:reportChunkErrorAction={err}>
            <EmptyState
              title={$t('app.chunk_load_failed')}
              description=""
              ctaText={$t('app.retry')}
              oncta={retryChunkLoad}
            />
          </div>
        {/await}
      {:else if currentTab === 'proxies'}
        {#await import('./Proxies.svelte')}
          <Skeleton type="card" height="60vh" />
        {:then { default: Proxies }}
          <div transition:fade={{ duration: 150 }}>
            <Proxies />
          </div>
        {:catch err}
          <div use:reportChunkErrorAction={err}>
            <EmptyState
              title={$t('app.chunk_load_failed')}
              description=""
              ctaText={$t('app.retry')}
              oncta={retryChunkLoad}
            />
          </div>
        {/await}
      {:else if currentTab === 'connections'}
        {#await import('./Connections.svelte')}
          <Skeleton type="card" height="60vh" />
        {:then { default: Connections }}
          <div transition:fade={{ duration: 150 }}>
            <Connections />
          </div>
        {:catch err}
          <div use:reportChunkErrorAction={err}>
            <EmptyState
              title={$t('app.chunk_load_failed')}
              description=""
              ctaText={$t('app.retry')}
              oncta={retryChunkLoad}
            />
          </div>
        {/await}
      {:else if currentTab === 'rules'}
        {#await import('./Rules.svelte')}
          <Skeleton type="card" height="60vh" />
        {:then { default: Rules }}
          <div transition:fade={{ duration: 150 }}>
            <Rules />
          </div>
        {:catch err}
          <div use:reportChunkErrorAction={err}>
            <EmptyState
              title={$t('app.chunk_load_failed')}
              description=""
              ctaText={$t('app.retry')}
              oncta={retryChunkLoad}
            />
          </div>
        {/await}
      {:else if currentTab === 'traffic'}
        {#await import('./Traffic.svelte')}
          <Skeleton type="card" height="60vh" />
        {:then { default: Traffic }}
          <div transition:fade={{ duration: 150 }}>
            <Traffic />
          </div>
        {:catch err}
          <div use:reportChunkErrorAction={err}>
            <EmptyState
              title={$t('app.chunk_load_failed')}
              description=""
              ctaText={$t('app.retry')}
              oncta={retryChunkLoad}
            />
          </div>
        {/await}
      {:else if currentTab === 'services'}
        {#await import('./Services.svelte')}
          <Skeleton type="card" height="60vh" />
        {:then { default: Services }}
          <div transition:fade={{ duration: 150 }}>
            <Services onSwitchTab={switchTab} />
          </div>
        {:catch err}
          <div use:reportChunkErrorAction={err}>
            <EmptyState
              title={$t('app.chunk_load_failed')}
              description=""
              ctaText={$t('app.retry')}
              oncta={retryChunkLoad}
            />
          </div>
        {/await}
      {:else if currentTab === 'smartproxy'}
        {#await import('./SmartProxy.svelte')}
          <Skeleton type="card" height="60vh" />
        {:then { default: SmartProxy }}
          <div transition:fade={{ duration: 150 }}>
            <SmartProxy onSwitchTab={switchTab} />
          </div>
        {:catch err}
          <div use:reportChunkErrorAction={err}>
            <EmptyState
              title={$t('app.chunk_load_failed')}
              description=""
              ctaText={$t('app.retry')}
              oncta={retryChunkLoad}
            />
          </div>
        {/await}
      {:else if currentTab === 'trafficquotas'}
        {#await import('./TrafficQuotas.svelte')}
          <Skeleton type="card" height="60vh" />
        {:then { default: TrafficQuotas }}
          <div transition:fade={{ duration: 150 }}>
            <TrafficQuotas onSwitchTab={switchTab} />
          </div>
        {:catch err}
          <div use:reportChunkErrorAction={err}>
            <EmptyState
              title={$t('app.chunk_load_failed')}
              description=""
              ctaText={$t('app.retry')}
              oncta={retryChunkLoad}
            />
          </div>
        {/await}
      {:else if currentTab === 'dat'}
        {#await import('./DATManager.svelte')}
          <Skeleton type="card" height="60vh" />
        {:then { default: DATManager }}
          <div transition:fade={{ duration: 150 }}>
            <DATManager onSwitchTab={switchTab} />
          </div>
        {:catch err}
          <div use:reportChunkErrorAction={err}>
            <EmptyState
              title={$t('app.chunk_load_failed')}
              description=""
              ctaText={$t('app.retry')}
              oncta={retryChunkLoad}
            />
          </div>
        {/await}
      {:else if currentTab === 'mihomo-gen'}
        {#await import('./MihomoGenerator.svelte')}
          <Skeleton type="card" height="60vh" />
        {:then { default: MihomoGenerator }}
          <div transition:fade={{ duration: 150 }}>
            <MihomoGenerator onSwitchTab={switchTab} />
          </div>
        {:catch err}
          <div use:reportChunkErrorAction={err}>
            <EmptyState
              title={$t('app.chunk_load_failed')}
              description=""
              ctaText={$t('app.retry')}
              oncta={retryChunkLoad}
            />
          </div>
        {/await}
      {:else if currentTab === 'console'}
        {#await import('./Console.svelte')}
          <Skeleton type="card" height="60vh" />
        {:then { default: Console }}
          <div transition:fade={{ duration: 150 }}>
            <Console onSwitchTab={switchTab} />
          </div>
        {:catch err}
          <div use:reportChunkErrorAction={err}>
            <EmptyState
              title={$t('app.chunk_load_failed')}
              description=""
              ctaText={$t('app.retry')}
              oncta={retryChunkLoad}
            />
          </div>
        {/await}
      {:else if currentTab === 'network'}
        {#await import('./NetworkTools.svelte')}
          <Skeleton type="card" height="60vh" />
        {:then { default: NetworkTools }}
          <div transition:fade={{ duration: 150 }}>
            <NetworkTools onSwitchTab={switchTab} />
          </div>
        {:catch err}
          <div use:reportChunkErrorAction={err}>
            <EmptyState
              title={$t('app.chunk_load_failed')}
              description=""
              ctaText={$t('app.retry')}
              oncta={retryChunkLoad}
            />
          </div>
        {/await}
      {:else if currentTab === 'settings'}
        {#await import('./Settings.svelte')}
          <Skeleton type="card" height="60vh" />
        {:then { default: Settings }}
          <div transition:fade={{ duration: 150 }}>
            <Settings onSwitchTab={switchTab} />
          </div>
        {:catch err}
          <div use:reportChunkErrorAction={err}>
            <EmptyState
              title={$t('app.chunk_load_failed')}
              description=""
              ctaText={$t('app.retry')}
              oncta={retryChunkLoad}
            />
          </div>
        {/await}
      {/if}
    {/key}
  </div>
</div>

<Toast />
<ConfirmDialog />
<MihomoSocketMigrateModal
  bind:open={showMihomoMigrateModal}
  onclose={() => (showMihomoMigrateModal = false)}
  onsuccess={handleRefresh}
/>
<UnsavedChangesModal
  isOpen={showUnsavedModal}
  dirtySources={dirtySourceNames}
  isSaving={isSavingAndNavigating}
  onSaveAndLeave={handleSaveAndLeave}
  onLeaveWithoutSaving={handleLeaveWithoutSaving}
  onStay={handleStay}
/>

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

  @media (max-width: 768px) {
    .status-badge-item {
      border-bottom: 1px solid var(--border);
      border-right: 1px solid var(--border);
    }
    .status-badge-item:nth-child(2n) {
      border-right: 0;
    }
    .status-badge-item:nth-last-child(-n + 2) {
      border-bottom: 0;
    }
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
    width: 100%;
    font: inherit;
    text-align: left;
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
    word-break: break-word;
    overflow-wrap: anywhere;
    min-width: 0;
  }

  @media (max-width: 768px) {
    .info-rows {
      grid-template-columns: 1fr;
    }

    .info-row {
      border-right: 0 !important;
      padding: 10px 14px;
      gap: 10px;
    }

    .info-row:nth-last-child(-n + 2) {
      border-bottom: 1px solid var(--border);
    }

    .info-row:last-child {
      border-bottom: 0;
    }

    .info-row .lbl {
      min-width: 110px;
      font-size: 12px;
    }

    .info-row .val {
      font-size: 12px;
    }
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

  /* Fullscreen editor layout geometry (.dashboard-layout.editor-active,
     .main-content.editor-active) lives solely in global.css to avoid
     maintaining two out-of-sync copies of the same !important rules. */
</style>
