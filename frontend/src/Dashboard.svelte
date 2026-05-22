<script lang="ts">
  import { onMount } from 'svelte';
  import { fade } from 'svelte/transition';
  import { t, setLang } from './i18n';
  import { isSidebarOpen, capabilities, fetchCapabilities } from './stores';
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
  import Subscriptions from './Subscriptions.svelte';
  import NetworkTools from './NetworkTools.svelte';
  import SmartProxy from './SmartProxy.svelte';
  import TrafficQuotas from './TrafficQuotas.svelte';
  import DATManager from './DATManager.svelte';
  import Console from './Console.svelte';

  let version = $t('app.loading');
  let loading = false;
  let currentTab = 'dashboard';
  const mihomoDependentTabs = [
    'proxies',
    'connections',
    'rules',
    'traffic',
    'smartproxy',
    'trafficquotas'
  ];
  let theme = document.documentElement.getAttribute('data-theme') || 'light';
  let pwaInstallPrompt: any = null;

  // Dashboard live monitoring state
  interface ServiceStatus {
    xkeen: string;
    xray: string;
    mihomo: string;
    connections: number;
    xrayVersion: string;
    mihomoVersion: string;
  }

  let serviceStatus: ServiceStatus = {
    xkeen: 'loading',
    xray: 'loading',
    mihomo: 'loading',
    connections: 0,
    xrayVersion: '',
    mihomoVersion: ''
  };
  let statusError = false;
  let statusLoading = true;

  interface SystemStats {
    memory: { total: number; used: number; free: number };
    load: [number, number, number];
    uptime: { seconds: number; days: number; hours: number; minutes: number };
    go_runtime: { goroutines: number; heap_alloc: number; heap_sys: number; num_gc: number };
    router_model: string;
    hostname: string;
    wan_status: string;
    default_gateway: string;
    dns_servers: string[];
    dns_resolving: boolean;
    invalid_config: boolean;
  }

  let systemStats: SystemStats | null = null;
  let activeSubscriptionsCount = 0;
  let totalProxiesCount = 0;

  async function fetchSubscriptionSummary() {
    try {
      const res = await fetch('/api/subscriptions');
      if (res.ok) {
        const envelope = await res.json();
        const subs = Array.isArray(envelope) ? envelope : (envelope.data ?? []);
        activeSubscriptionsCount = subs.filter((s: any) => s.enabled).length;
        totalProxiesCount = subs.reduce((acc: number, s: any) => acc + (s.proxy_count || 0), 0);
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

      const svcText =
        svcRes.status === 'fulfilled' && svcRes.value.ok ? await svcRes.value.text() : '';
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
        xkeen: svcText.toLowerCase().includes('running') ? 'running' : svcText || 'unknown',
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
      }
    } catch (e) {
      // ignore
    }
  }

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
    } catch (e) {
      version = $t('app.error');
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

  function switchTab(tab: string) {
    currentTab = tab;
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
    const statusInterval = setInterval(fetchLiveStatus, 10000);
    const statsInterval = setInterval(fetchSystemStats, 5000);
    const capInterval = setInterval(fetchCapabilities, 30000);
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
    };
  });
</script>

<div class="dashboard-layout">
  <!-- Mobile header bar -->
  <header class="mobile-header">
    <button
      class="burger-btn"
      on:click={toggleSidebar}
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
    on:click={closeSidebar}
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
    />
  </div>

  <!-- Main content area -->
  <div class="main-content">
    <!-- Mihomo offline warning banner -->
    {#if mihomoDependentTabs.includes(currentTab) && $capabilities !== null && !$capabilities.mihomo.reachable}
      <div
        class="alert alert-warning"
        style="margin: 12px 16px 0; padding: 10px 14px; border-radius: 8px; font-size: 13px;"
      >
        <Icon name="warning" size={14} /> <strong>{$t('capabilities.mihomo_offline')}</strong> — {$t(
          'capabilities.mihomo_offline_desc'
        )}
      </div>
    {/if}

    {#if currentTab === 'dashboard'}
      <div class="container" transition:fade={{ duration: 150 }}>
        <h1>{$t('nav.monitoring')}</h1>
        <p class="text-secondary mb-3">{$t('dash.welcome')}</p>

        <!-- Problems Panel -->
        {#if (systemStats && systemStats.invalid_config) || ($capabilities !== null && !$capabilities.mihomo.api_reachable && $capabilities.mihomo.process_running) || ($capabilities !== null && !$capabilities.kernels.xray.installed && !$capabilities.kernels.mihomo.installed)}
          <div style="margin-bottom: var(--spacing-4);">
            <Card title={$t('dash.problems_panel')}>
              <div style="display: flex; flex-direction: column; gap: var(--spacing-3);">
                {#if systemStats && systemStats.invalid_config}
                  <div
                    class="alert alert-error"
                    style="margin: 0; display: flex; justify-content: space-between; align-items: center; flex-wrap: wrap; gap: 12px;"
                  >
                    <div
                      style="display: flex; align-items: flex-start; gap: var(--spacing-2); flex: 1;"
                    >
                      <span style="margin-top: 2px; display: inline-flex;"
                        ><Icon name="warning" size={16} /></span
                      >
                      <div>
                        <strong>{$t('dash.problems.invalid_config_title')}</strong>
                        <div style="font-size: 13px; opacity: 0.9; margin-top: 2px;">
                          {$t('dash.problems.invalid_config_desc')}
                        </div>
                      </div>
                    </div>
                    <Button variant="secondary" onclick={() => switchTab('editor')}>
                      {$t('dash.problems.invalid_config_cta')}
                    </Button>
                  </div>
                {/if}

                {#if $capabilities !== null && !$capabilities.mihomo.api_reachable && $capabilities.mihomo.process_running}
                  <div
                    class="alert alert-warning"
                    style="margin: 0; display: flex; justify-content: space-between; align-items: center; flex-wrap: wrap; gap: 12px;"
                  >
                    <div
                      style="display: flex; align-items: flex-start; gap: var(--spacing-2); flex: 1;"
                    >
                      <span style="margin-top: 2px; display: inline-flex;"
                        ><Icon name="warning" size={16} /></span
                      >
                      <div>
                        <strong>{$t('dash.problems.mihomo_api_title')}</strong>
                        <div style="font-size: 13px; opacity: 0.9; margin-top: 2px;">
                          {$t('dash.problems.mihomo_api_desc')}
                        </div>
                      </div>
                    </div>
                    <Button variant="secondary" onclick={() => switchTab('settings')}>
                      {$t('dash.problems.mihomo_api_cta')}
                    </Button>
                  </div>
                {/if}

                {#if $capabilities !== null && !$capabilities.kernels.xray.installed && !$capabilities.kernels.mihomo.installed}
                  <div
                    class="alert alert-error"
                    style="margin: 0; display: flex; justify-content: space-between; align-items: center; flex-wrap: wrap; gap: 12px;"
                  >
                    <div
                      style="display: flex; align-items: flex-start; gap: var(--spacing-2); flex: 1;"
                    >
                      <span style="margin-top: 2px; display: inline-flex;"
                        ><Icon name="warning" size={16} /></span
                      >
                      <div>
                        <strong>{$t('dash.problems.kernel_missing_title')}</strong>
                        <div style="font-size: 13px; opacity: 0.9; margin-top: 2px;">
                          {$t('dash.problems.kernel_missing_desc')}
                        </div>
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
        <div style="margin-bottom: var(--spacing-4);">
          <Card title={$t('dash.service_status')}>
            {#if statusLoading}
              <div class="status-badges-row">
                <Skeleton type="rect" width="120px" height="34px" />
                <Skeleton type="rect" width="160px" height="34px" />
                <Skeleton type="rect" width="160px" height="34px" />
                <Skeleton type="rect" width="130px" height="34px" />
              </div>
            {:else if statusError}
              <div class="status-error-row">
                <span><Icon name="warning" size={14} /> {$t('dash.status_error')}</span>
                <Button variant="secondary" onclick={fetchLiveStatus} title={$t('app.refresh')}>
                  ↺ {$t('app.refresh')}
                </Button>
              </div>
            {:else}
              <div class="status-badges-row">
                <div class="status-badge-item">
                  <span class="status-dot {statusColor(serviceStatus.xkeen)}"></span>
                  <span class="status-badge-label">XKeen</span>
                  <span class="status-badge-value status-{statusColor(serviceStatus.xkeen)}">
                    {serviceStatus.xkeen === 'running' ? $t('app.running') : $t('app.stop')}
                  </span>
                </div>
                <div class="status-badge-item">
                  <span class="status-dot {statusColor(serviceStatus.xray)}"></span>
                  <span class="status-badge-label">Xray</span>
                  <span class="status-badge-value status-{statusColor(serviceStatus.xray)}">
                    {$t('kernel.status.' + (serviceStatus.xray || 'unknown'))}
                    {#if serviceStatus.xrayVersion && serviceStatus.xray !== 'not_installed'}
                      <span class="version-badge">{serviceStatus.xrayVersion}</span>
                    {/if}
                  </span>
                </div>
                <div class="status-badge-item">
                  <span class="status-dot {statusColor(serviceStatus.mihomo)}"></span>
                  <span class="status-badge-label">Mihomo</span>
                  <span class="status-badge-value status-{statusColor(serviceStatus.mihomo)}">
                    {$t('kernel.status.' + (serviceStatus.mihomo || 'unknown'))}
                    {#if serviceStatus.mihomoVersion && serviceStatus.mihomo !== 'not_installed'}
                      <span class="version-badge">{serviceStatus.mihomoVersion}</span>
                    {/if}
                  </span>
                </div>
                <div class="status-badge-item">
                  <span class="status-dot {serviceStatus.connections > 0 ? 'success' : 'warning'}"
                  ></span>
                  <span class="status-badge-label">{$t('dash.connections')}</span>
                  <span class="status-badge-value">{serviceStatus.connections}</span>
                </div>
              </div>
            {/if}
          </Card>
        </div>

        <!-- Router Info, Network, Subscriptions Grid -->
        <div class="stats-grid" style="margin-bottom: var(--spacing-4);">
          <!-- Router Info -->
          <Card title={$t('dash.router_info')}>
            <div
              style="display: flex; flex-direction: column; gap: var(--spacing-2); font-size: 13px; min-height: 70px;"
            >
              <div>
                <span class="text-secondary">{$t('dash.router_model')}:</span>
                <span style="font-weight: 500; margin-left: 4px;"
                  >{systemStats?.router_model || '—'}</span
                >
              </div>
              <div>
                <span class="text-secondary">{$t('dash.router_hostname')}:</span>
                <span style="font-weight: 500; margin-left: 4px;"
                  >{systemStats?.hostname || '—'}</span
                >
              </div>
              <div>
                <span class="text-secondary">{$t('dash.dns_servers')}:</span>
                <span style="font-weight: 500; margin-left: 4px; font-family: monospace;">
                  {systemStats?.dns_servers?.join(', ') || '—'}
                </span>
              </div>
            </div>
          </Card>

          <!-- Network Diagnostics -->
          <Card title={$t('dash.network_diagnostics')}>
            <div
              style="display: flex; flex-direction: column; gap: var(--spacing-2); font-size: 13px; min-height: 70px;"
            >
              <div>
                <span class="text-secondary">{$t('dash.wan_status')}:</span>
                <span
                  style="font-weight: 500; margin-left: 4px;"
                  class={systemStats?.wan_status === 'online' ? 'status-success' : 'status-error'}
                >
                  {systemStats?.wan_status === 'online'
                    ? $t('dash.wan_online')
                    : $t('dash.wan_offline')}
                </span>
              </div>
              <div>
                <span class="text-secondary">{$t('dash.default_gateway')}:</span>
                <span style="font-weight: 500; margin-left: 4px; font-family: monospace;"
                  >{systemStats?.default_gateway || '—'}</span
                >
              </div>
              <div>
                <span class="text-secondary">{$t('dash.dns_resolving')}:</span>
                <span
                  style="font-weight: 500; margin-left: 4px;"
                  class={systemStats?.dns_resolving ? 'status-success' : 'status-error'}
                >
                  {systemStats?.dns_resolving
                    ? $t('dash.dns_resolving_ok')
                    : $t('dash.dns_resolving_fail')}
                </span>
              </div>
            </div>
          </Card>

          <!-- Subscriptions Summary -->
          <Card title={$t('dash.subscriptions_summary')}>
            <div
              style="display: flex; flex-direction: column; gap: var(--spacing-2); font-size: 13px; min-height: 70px;"
            >
              <div>
                <span class="text-secondary">{$t('dash.active_subscriptions')}:</span>
                <span style="font-weight: 500; margin-left: 4px;">{activeSubscriptionsCount}</span>
              </div>
              <div>
                <span class="text-secondary">{$t('dash.total_proxies')}:</span>
                <span style="font-weight: 500; margin-left: 4px;">{totalProxiesCount}</span>
              </div>
            </div>
          </Card>
        </div>

        {#if systemStats}
          <div style="margin-bottom: var(--spacing-4);">
            <Card title={$t('dash.system_stats')}>
              <div class="stats-grid">
                <div class="stat-box">
                  <div class="stat-label">{$t('dash.ram')}</div>
                  <div class="stat-value">
                    {formatBytes(systemStats.memory.used)} / {formatBytes(systemStats.memory.total)}
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
                </div>
                <div class="stat-box">
                  <div class="stat-label">{$t('dash.uptime')}</div>
                  <div class="stat-value">
                    {systemStats.uptime.days}d {systemStats.uptime.hours}h {systemStats.uptime
                      .minutes}m
                  </div>
                </div>
                <div class="stat-box">
                  <div class="stat-label">{$t('dash.goroutines')}</div>
                  <div class="stat-value">{systemStats.go_runtime.goroutines}</div>
                </div>
              </div>
            </Card>
          </div>
        {/if}

        <div style="margin-bottom: var(--spacing-4);">
          <Card title={$t('dash.system_info')}>
            <p style="margin: 0;"><strong>{$t('app.version')}:</strong> {version}</p>
          </Card>
        </div>

        <div style="margin-bottom: var(--spacing-2);">
          <Card title={$t('dash.quick_actions')}>
            <div class="quick-actions">
              <Button
                variant="secondary"
                onclick={() => switchTab('proxies')}
                title={$t('nav.proxies')}
              >
                <Icon name="proxies" size={16} />
                {$t('nav.proxies')}
              </Button>
              <Button
                variant="secondary"
                onclick={() => switchTab('subscriptions')}
                title={$t('nav.subscriptions')}
              >
                <Icon name="subscriptions" size={16} />
                {$t('nav.subscriptions')}
              </Button>
              <Button
                variant="secondary"
                onclick={() => switchTab('editor')}
                title={$t('nav.editor')}
              >
                <Icon name="editor" size={16} />
                {$t('nav.editor')}
              </Button>
              <Button variant="secondary" onclick={() => switchTab('logs')} title={$t('nav.logs')}>
                <Icon name="logs" size={16} />
                {$t('nav.logs')}
              </Button>
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
    {:else if currentTab === 'subscriptions'}
      <div transition:fade={{ duration: 150 }}>
        <Subscriptions onSwitchTab={switchTab} />
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
  .quick-actions {
    display: flex;
    gap: 0.5rem;
    flex-wrap: wrap;
  }

  /* Live status badges */
  .status-badges-row {
    display: flex;
    flex-wrap: wrap;
    gap: 12px;
    margin-top: 4px;
  }

  .status-badge-item {
    display: flex;
    align-items: center;
    gap: 6px;
    background: var(--bg);
    border: 1px solid var(--border);
    border-radius: 6px;
    padding: 6px 12px;
    font-size: 13px;
  }

  .status-badge-label {
    font-weight: 600;
    color: var(--fg-primary);
  }

  .status-badge-value {
    color: var(--fg-secondary);
    display: flex;
    align-items: center;
    gap: 4px;
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
    font-size: 11px;
    background: var(--border);
    border-radius: 4px;
    padding: 1px 5px;
    font-family: monospace;
    color: var(--fg-secondary);
  }

  .status-error-row {
    display: flex;
    align-items: center;
    gap: 12px;
    color: var(--danger);
    padding: 8px 0;
  }
</style>
