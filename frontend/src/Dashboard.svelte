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
  import PageHeader from './PageHeader.svelte';
  import ServiceStatusGroup from './components/dashboard/ServiceStatusGroup.svelte';
  import SystemResourcesWidget from './components/dashboard/SystemResourcesWidget.svelte';
  import TrafficTelemetryWidget from './components/dashboard/TrafficTelemetryWidget.svelte';
  import QuickActionsWidget from './components/dashboard/QuickActionsWidget.svelte';
  import SystemInfoWidget from './components/dashboard/SystemInfoWidget.svelte';
  import SystemAboutModal from './components/dashboard/SystemAboutModal.svelte';
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
  let showAboutModal = $state(false);
  let showUnsavedModal = $state(false);
  let pendingTargetTab = $state<string | null>(null);
  let pendingTargetHash = $state<string | null>(null);
  let isSavingAndNavigating = $state(false);
  let dirtySourceNames = $state<string[]>([]);
  let currentTab = $state('dashboard');
  let currentHash = $state(typeof window !== 'undefined' ? window.location.hash : '');

  function checkIsConstructorHash(hash: string): boolean {
    if (!hash) return false;
    const cleanHash = hash.replace(/^#\/?/, '');
    const [path, query] = cleanHash.split('?');
    if (path === 'constructor' || path === 'mihomo-gen') return true;
    if (path === 'editor' && query) {
      const params = new URLSearchParams(query);
      if (params.get('tab') === 'constructor') return true;
    }
    return false;
  }

  const isConstructorMode = $derived(checkIsConstructorHash(currentHash));
  const isEditorFullscreen = $derived(currentTab === 'editor' && !isConstructorMode);
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

  interface WatchdogStatus {
    state: string;
    consecutive_failures: number;
    disarm_attempts: number;
    last_disarm_error: string;
    interception_active: boolean;
    interception_family: string;
    next_attempt_at: number;
    degraded_at: number;
  }

  let watchdogStatus = $state<WatchdogStatus | null>(null);
  let isResettingWatchdog = $state(false);

  const isWatchdogIncident = $derived(
    watchdogStatus?.state === 'degraded' || watchdogStatus?.state === 'disarmed'
  );

  const watchdogBadge = $derived.by(() => {
    if (!watchdogStatus?.state) return null;
    switch (watchdogStatus.state) {
      case 'armed':
        return {
          cssClass: 'badge badge-success',
          labelKey: 'watchdog.state_armed',
          hintKey: 'watchdog.state_armed_hint'
        };
      case 'idle':
        return {
          cssClass: 'badge',
          labelKey: 'watchdog.state_idle',
          hintKey: 'watchdog.state_idle_hint'
        };
      case 'degraded':
        return {
          cssClass: 'badge badge-danger',
          labelKey: 'watchdog.state_degraded',
          hintKey: 'watchdog.state_degraded_hint'
        };
      case 'disarmed':
        return {
          cssClass: 'badge badge-warning',
          labelKey: 'watchdog.state_disarmed',
          hintKey: 'watchdog.state_disarmed_hint'
        };
      default:
        return null;
    }
  });

  async function handleResetWatchdog() {
    if (isResettingWatchdog) return;
    isResettingWatchdog = true;
    try {
      const res = await apiFetch('/api/service/watchdog/reset', { method: 'POST' });
      if (!res.ok) {
        let errMessage = '';
        try {
          const errData = await res.json();
          errMessage = errData?.error || errData?.message || '';
        } catch (_) {
          errMessage = await res.text().catch(() => '');
        }
        showToast('error', $t('watchdog.reset_failed', { error: errMessage || res.statusText }));
        return;
      }
      await fetchLiveStatus();
    } catch (e: any) {
      if (e?.status === 401) return;
      showToast('error', $t('watchdog.reset_failed', { error: e?.message || $t('app.error') }));
    } finally {
      isResettingWatchdog = false;
    }
  }

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
  let subsSummaryLoaded = $state(false);
  let totalProxiesCount = $state(0);
  let activeProxiesCount = $state(0);
  let subscriptionProxiesCount = $state(0);
  let statsLastFetched = $state('');

  // XKeen не установлен: без него ядра не запустить, а ставятся они тем же
  // установщиком — пункт про ядра в этом случае не показывается
  const isXKeenMissing = $derived($capabilities?.xkeen_installed === false);
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

  /**
   * A subscription may be declared directly in config.yaml as an HTTP
   * proxy-provider (not managed by the panel); it counts for the quick start.
   */
  async function hasCoreProxyProvider(signal?: AbortSignal): Promise<boolean> {
    try {
      const res = await apiFetch('/api/mihomo/proxy/providers/proxies', { signal });
      if (!res.ok) return false;
      const data = await res.json();
      return Object.values(data?.providers ?? {}).some(
        (p: any) =>
          String(p?.vehicleType).toUpperCase() === 'HTTP' &&
          Array.isArray(p?.proxies) &&
          p.proxies.length > 0
      );
    } catch {
      return false;
    }
  }

  async function fetchSubscriptionSummary(signal?: AbortSignal) {
    try {
      const res = await apiFetch('/api/subscriptions', { signal });
      if (res.ok) {
        const envelope = await res.json();
        const rawList = Array.isArray(envelope) ? envelope : (envelope?.data ?? []);
        const subs = Array.isArray(rawList) ? rawList : [];
        totalSubsCount = subs.length;
        hasSubscription = subs.length > 0 || (await hasCoreProxyProvider(signal));
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
    } finally {
      subsSummaryLoaded = true;
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
            if (parsed.data.watchdog) {
              watchdogStatus = parsed.data.watchdog;
            }
          } else {
            xkeenRaw = text;
            isXkeenRunning = guessXkeenRunning(text);
          }
        } catch (_) {
          xkeenRaw = text;
          isXkeenRunning = guessXkeenRunning(text);
        }
      } else {
        statusError = true;
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
    // Headroom above the peak so the highest sample never glues to the top edge.
    const max = Math.max(...values, 0.01) * 1.15;
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

  // Unknown until the first status poll: keep the capsule LED neutral
  // instead of reporting XKeen as stopped.
  const xkeenRunningForCapsule = $derived(
    serviceStatus.xkeen === 'loading' ? undefined : serviceStatus.xkeen === 'running'
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
      return basePath || 'dashboard';
    }
    return 'dashboard';
  }

  function handleHashChange() {
    currentHash = window.location.hash;
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
    if (targetTab !== currentTab) {
      resetScrollForNewTab();
    }
    currentTab = targetTab;
  }

  // Новая страница открывается сверху, а не с прокруткой предыдущей.
  // lockedScrollY обнуляется, иначе закрытие мобильного меню вернёт
  // позицию старой страницы.
  function resetScrollForNewTab() {
    lockedScrollY = 0;
    window.scrollTo(0, 0);
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
          resetScrollForNewTab();
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
      resetScrollForNewTab();
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

    currentHash = window.location.hash;
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

<div class="dashboard-layout" class:editor-active={isEditorFullscreen}>
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
        isXkeenRunning={xkeenRunningForCapsule}
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
      isXkeenRunning={xkeenRunningForCapsule}
    />
  </div>

  <!-- Main content area -->
  <div
    class="main-content"
    class:editor-active={isEditorFullscreen}
    class:rail={$isSidebarCollapsed}
    inert={drawerIsModal}
  >
    <!-- Mihomo offline warning banner / Restarting notice -->
    {#if mihomoDependentTabs.includes(currentTab) && $capabilities !== null && !$capabilities?.mihomo?.reachable}
      {#if $isServiceRestarting}
        <div
          class="service-restarting-banner"
          style="margin: 12px 16px 0; padding: 12px 18px; background: var(--accent-soft); border: 1px solid var(--accent); border-radius: var(--radius-md); display: flex; align-items: center; gap: 12px; font-size: 13.5px; color: var(--fg-primary);"
        >
          <span class="spinner"></span>
          <span>{$t('service.restarting_wait')}</span>
        </div>
      {:else}
        <div style="margin: 12px 16px 0;">
          <ApiOffline
            endpoint={$capabilities?.mihomo?.discovered_secret ? 'Mihomo API' : '127.0.0.1:9090'}
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
          <PageHeader
            title={$t('dash.title')}
            subtitle={$t('dash.welcome')}
            breadcrumbs={[{ label: $t('nav.group_overview') }, { label: $t('nav.dashboard') }]}
            onSwitchTab={switchTab}
            hideHome={true}
          >
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
          </PageHeader>

          <!-- Quickstart Checklist (Mihomo only, auto-hides when all steps complete).
               Gated on statusLoading/subsSummaryLoaded so it doesn't flash "incomplete"
               using each store's not-yet-fetched default before the first poll round
               of fetchLiveStatus/fetchSubscriptionSummary actually lands. -->
          {#if $capabilities?.active_kernel === 'mihomo' && !statusLoading && subsSummaryLoaded && !allQuickstartComplete}
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
                    <span class="qs-icon" role="img" aria-label={$t('dash.quickstart.step_done')}>
                      <Icon name="check" size={16} color="var(--success)" />
                    </span>
                    <span class="qs-text">{$t('dash.quickstart.step1_label')}</span>
                  </li>
                  <!-- Step 2: subscription added -->
                  <li class="qs-step" class:qs-step--done={hasSubscription}>
                    <span
                      class="qs-icon"
                      role="img"
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
                      role="img"
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
                      role="img"
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
          {#if (systemStats && systemStats.invalid_config) || isXKeenMissing || ($capabilities !== null && !$capabilities?.mihomo?.api_reachable && $capabilities?.mihomo?.process_running) || ($capabilities !== null && !$capabilities?.kernels?.xray?.installed && !$capabilities?.kernels?.mihomo?.installed) || ($capabilities !== null && $capabilities?.mihomo?.is_insecure_lan) || isKernelCrashed || isDiskLow || isSSLExpiring || isWatchdogIncident}
            <div style="margin-bottom: 18px;">
              <Card title={$t('dash.problems_panel')}>
                <div class="problems-list">
                  {#if isWatchdogIncident && watchdogStatus}
                    <div
                      class="problem-item {watchdogStatus.state === 'degraded'
                        ? 'alert-error'
                        : 'alert-warning'}"
                    >
                      <div class="problem-content">
                        <span class="problem-icon"><Icon name="warning" size={16} /></span>
                        <div>
                          <strong class="problem-title">
                            {$t(
                              watchdogStatus.state === 'degraded'
                                ? 'watchdog.banner_degraded_title'
                                : 'watchdog.banner_disarmed_title'
                            )}
                          </strong>
                          <div class="problem-desc">
                            {$t(
                              watchdogStatus.state === 'degraded'
                                ? 'watchdog.banner_degraded_desc'
                                : 'watchdog.banner_disarmed_desc'
                            )}
                            {#if statusError}
                              <span class="watchdog-stale-desc">({$t('watchdog.stale_note')})</span>
                            {/if}
                          </div>
                          {#if watchdogStatus.last_disarm_error}
                            <div class="watchdog-error-detail">
                              {watchdogStatus.last_disarm_error}
                            </div>
                          {/if}
                        </div>
                      </div>
                      <Button
                        variant="secondary"
                        loading={isResettingWatchdog}
                        onclick={handleResetWatchdog}
                      >
                        {$t(
                          watchdogStatus.state === 'degraded'
                            ? 'watchdog.cta_retry'
                            : 'watchdog.cta_reset'
                        )}
                      </Button>
                    </div>
                  {/if}
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
                  {#if $capabilities !== null && !$capabilities?.mihomo?.api_reachable && $capabilities?.mihomo?.process_running}
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
                  {#if isXKeenMissing}
                    <div class="problem-item alert-error" data-testid="problem-xkeen-missing">
                      <div class="problem-content">
                        <span class="problem-icon"><Icon name="warning" size={16} /></span>
                        <div>
                          <strong class="problem-title"
                            >{$t('dash.problems.xkeen_missing_title')}</strong
                          >
                          <div class="problem-desc">{$t('dash.problems.xkeen_missing_desc')}</div>
                        </div>
                      </div>
                      <Button variant="secondary" onclick={() => switchTab('services')}>
                        {$t('dash.problems.xkeen_missing_cta')}
                      </Button>
                    </div>
                  {:else if $capabilities !== null && !$capabilities?.kernels?.xray?.installed && !$capabilities?.kernels?.mihomo?.installed}
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
                  {#if $capabilities !== null && $capabilities?.mihomo?.is_insecure_lan}
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

          <!-- Live Service Status cards (DASH-03) -->
          <!-- Dashboard 60/40 Layout Grid (DASH-01) -->
          <div class="dashboard-grid-scope">
            <div class="dashboard-layout-grid">
              <!-- Left Column (60%): Service Status, System Resources, Traffic Telemetry -->
              <div class="dash-col-left">
                <!-- Service Status Group (DASH-03) -->
                <div class="dash-section">
                  <Card title={$t('dash.service_status')}>
                    {#snippet actions()}
                      {#if watchdogBadge}
                        <div class="dash-watchdog-badge-row" title={$t(watchdogBadge.hintKey)}>
                          <span class="dash-watchdog-label">{$t('watchdog.section_title')}</span>
                          <span class={watchdogBadge.cssClass}>{$t(watchdogBadge.labelKey)}</span>
                        </div>
                      {/if}
                    {/snippet}
                    <ServiceStatusGroup
                      {serviceStatus}
                      capabilities={$capabilities}
                      xkeenVersion={version !== $t('app.loading') && version !== $t('app.error')
                        ? version
                        : ''}
                      {statusLoading}
                      {statusError}
                      onRefresh={fetchLiveStatus}
                      onShowMihomoMigrateModal={() => (showMihomoMigrateModal = true)}
                    />
                  </Card>
                </div>

                <!-- System Resources (DASH-01, DASH-04) -->
                <div class="dash-section">
                  <SystemResourcesWidget {systemStats} {loadHistory} {sparklineData} />
                </div>

                <!-- Traffic & Network Telemetry (DASH-01) -->
                <div class="dash-section">
                  <TrafficTelemetryWidget onSwitchTab={switchTab} />
                </div>
              </div>

              <!-- Right Column (40%): Quick Actions, System Info -->
              <div class="dash-col-right">
                <!-- Quick Actions (DASH-02) -->
                <div class="dash-section">
                  <QuickActionsWidget onSwitchTab={switchTab} />
                </div>

                <!-- System Info (DASH-04, D-08) -->
                <div class="dash-section">
                  <SystemInfoWidget
                    {systemStats}
                    {version}
                    {panelVersion}
                    {statsLastFetched}
                    onOpenAbout={() => (showAboutModal = true)}
                  />
                </div>
              </div>
            </div>
          </div>
        </div>
      {:else if currentTab === 'editor'}
        {#await import('./Editor.svelte')}
          <Skeleton type="card" height="100%" />
        {:then { default: Editor }}
          <div
            style={isEditorFullscreen
              ? 'flex: 1; display: flex; flex-direction: column; min-height: 0; height: 100%;'
              : 'flex: 1; display: flex; flex-direction: column; min-height: 100%;'}
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
            <Rules onSwitchTab={switchTab} />
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
<SystemAboutModal
  isOpen={showAboutModal}
  onClose={() => (showAboutModal = false)}
  {systemStats}
  {panelVersion}
/>

<style>
  /* Dashboard 60/40 Layout Grid (DASH-01) */
  /* Раскладка переключается по фактической ширине области контента
     (container query), а не по ширине окна браузера. Порог 940px:
     левой колонке нужно ~520px (виджеты используют minmax(180px,1fr)),
     правой — ~360px, плюс gap 24px. */
  .dashboard-grid-scope {
    container: dashgrid / inline-size;
  }

  .dashboard-layout-grid {
    display: grid;
    grid-template-columns: minmax(0, 1.4fr) minmax(0, 1fr);
    gap: var(--space-lg, 24px);
    align-items: start;
  }

  @container dashgrid (max-width: 940px) {
    .dashboard-layout-grid {
      grid-template-columns: 1fr;
      gap: 18px;
    }
  }

  /* Fallback для движков без поддержки container queries: при окне
     <=1024px область контента заведомо уже 940px, оба правила совпадают. */
  @media (max-width: 1024px) {
    .dashboard-layout-grid {
      grid-template-columns: 1fr;
      gap: 18px;
    }
  }

  /* D-13: Адаптивная раскладка для сверхшироких экранов (>2400px / 4K/8K) */
  @container dashgrid (min-width: 2400px) {
    .dashboard-layout-grid {
      grid-template-columns: repeat(auto-fit, minmax(min(100%, 340px), 1fr));
    }
  }

  @media (min-width: 2401px) {
    .dashboard-layout-grid {
      grid-template-columns: repeat(auto-fit, minmax(min(100%, 340px), 1fr));
    }
  }

  .dash-col-left,
  .dash-col-right {
    display: flex;
    flex-direction: column;
    gap: 18px;
    min-width: 0;
  }

  .dash-section {
    width: 100%;
    min-width: 0;
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

  .dash-watchdog-badge-row {
    display: flex;
    align-items: center;
    gap: 6px;
  }

  .dash-watchdog-label {
    font-size: var(--font-size-xs);
    font-weight: 600;
    color: var(--fg-dim);
  }

  .watchdog-stale-desc {
    display: inline-block;
    margin-left: var(--spacing-1, 4px);
    color: var(--fg-dim);
    font-size: var(--font-size-xs, 12px);
  }

  .watchdog-error-detail {
    font-family: var(--font-family-mono, monospace);
    font-size: var(--font-size-xs, 12px);
    color: var(--fg-secondary);
    margin-top: var(--spacing-2, 8px);
    word-break: break-word;
    white-space: pre-wrap;
  }

  /* Fullscreen editor layout geometry (.dashboard-layout.editor-active,
     .main-content.editor-active) lives solely in global.css to avoid
     maintaining two out-of-sync copies of the same !important rules. */
</style>
