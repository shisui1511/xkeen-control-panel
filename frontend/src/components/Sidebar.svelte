<script lang="ts">
  import { t } from '../i18n';
  import { isSidebarOpen, isSidebarCollapsed, capabilities, mihomoApiAvailable } from '../stores';
  import Icon from '../lib/components/Icon.svelte';

  let {
    currentTab = 'dashboard',
    onSwitchTab = () => {},
    theme = 'light',
    onToggleTheme = () => {},
    onLogout = () => {},
    loading = false,
    pwaInstallPrompt = null,
    onInstallPWA = () => {}
  }: {
    currentTab?: string;
    onSwitchTab?: (tab: string) => void;
    theme?: string;
    onToggleTheme?: () => void;
    onLogout?: () => void;
    loading?: boolean;
    pwaInstallPrompt?: unknown;
    onInstallPWA?: () => void;
  } = $props();

  type GroupKey = 'overview' | 'proxy_subs' | 'routing' | 'observability' | 'system';
  const DEFAULT_GROUP_OPEN: Record<GroupKey, boolean> = {
    overview: true,
    proxy_subs: true,
    routing: true,
    observability: true,
    system: true
  };

  function loadGroupOpenState(): Record<GroupKey, boolean> {
    const state = { ...DEFAULT_GROUP_OPEN };
    try {
      const raw = localStorage.getItem('sidebar_group_state');
      if (raw) {
        const parsed = JSON.parse(raw);
        if (parsed && typeof parsed === 'object' && !Array.isArray(parsed)) {
          for (const key of Object.keys(DEFAULT_GROUP_OPEN) as GroupKey[]) {
            if (typeof parsed[key] === 'boolean') {
              state[key] = parsed[key];
            }
          }
        }
      }
    } catch (e) {
      // localStorage unavailable or corrupted state — fail-closed to defaults (all open)
    }
    return state;
  }

  const groupOpen = $state<Record<GroupKey, boolean>>(loadGroupOpenState());

  $effect(() => {
    const snapshot = {
      overview: groupOpen.overview,
      proxy_subs: groupOpen.proxy_subs,
      routing: groupOpen.routing,
      observability: groupOpen.observability,
      system: groupOpen.system
    };
    try {
      localStorage.setItem('sidebar_group_state', JSON.stringify(snapshot));
    } catch (e) {
      // localStorage may be unavailable
    }
  });

  // Page-global Ctrl+B / Cmd+B hotkey toggles icon-rail collapse (D-13),
  // ignored while a text input/textarea/select/contenteditable has focus.
  function handleGlobalKeydown(e: KeyboardEvent) {
    if (!(e.ctrlKey || e.metaKey) || (e.key !== 'b' && e.key !== 'B')) {
      return;
    }
    const target = e.target as HTMLElement | null;
    const tag = target?.tagName;
    const isTextInput =
      tag === 'INPUT' || tag === 'TEXTAREA' || tag === 'SELECT' || target?.isContentEditable;
    if (isTextInput) {
      return;
    }
    e.preventDefault();
    isSidebarCollapsed.update((v) => !v);
  }

  $effect(() => {
    window.addEventListener('keydown', handleGlobalKeydown);
    return () => {
      window.removeEventListener('keydown', handleGlobalKeydown);
    };
  });
</script>

<!-- Brand block -->
<div class="sidebar-logo">
  <span class="brand-mark" aria-hidden="true">
    <!-- XKeen logo: cross-routing — 4 endpoints, crossed paths, central hub.
         Reads as both "X" (the X in XKeen) and a proxy-switch diagram. -->
    <svg width="22" height="22" viewBox="0 0 24 24" fill="none" aria-hidden="true">
      <circle cx="4" cy="4" r="1.8" fill="currentColor" />
      <circle cx="20" cy="4" r="1.8" fill="currentColor" />
      <circle cx="4" cy="20" r="1.8" fill="currentColor" />
      <circle cx="20" cy="20" r="1.8" fill="currentColor" />
      <path
        d="M5.4 5.4 L18.6 18.6"
        stroke="currentColor"
        stroke-width="2.4"
        stroke-linecap="round"
      />
      <path
        d="M18.6 5.4 L5.4 18.6"
        stroke="currentColor"
        stroke-width="2.4"
        stroke-linecap="round"
      />
      <circle cx="12" cy="12" r="2.8" fill="currentColor" />
      <circle cx="12" cy="12" r="1.1" fill="#0c2237" />
    </svg>
  </span>
  <span class="brand-text">
    <span class="b1"><span class="x">X</span>Keen</span>
    <span class="b2">Control&nbsp;Panel</span>
  </span>
</div>

<nav style="flex: 1; overflow-y: auto; padding: 4px 0 10px; scrollbar-width: none;">
  <!-- Overview group -->
  <details class="nav-group" bind:open={groupOpen.overview}>
    <summary>
      <span class="group-ttl">
        <!-- Обзор → compass: центр / навигация -->
        <svg
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          stroke-width="2.2"
          stroke-linecap="round"
          stroke-linejoin="round"
          aria-hidden="true"
          ><circle cx="12" cy="12" r="9" /><path
            d="M16.24 7.76 14 14l-6.24 2.24L10 10z"
            fill="currentColor"
            stroke="none"
            opacity=".85"
          /></svg
        >
        <span class="lbl">{$t('nav.group_overview')}</span>
      </span>
      <span class="nav-group-arrow">▶</span>
    </summary>
    <a
      href="#/dashboard"
      class="nav-item"
      aria-current={currentTab === 'dashboard' ? 'page' : undefined}
      data-label={$t('nav.dashboard')}
      onclick={() => isSidebarOpen.set(false)}
      title={$t('nav.dashboard')}
    >
      <Icon name="dashboard" size={16} />
      <span class="lbl">{$t('nav.dashboard')}</span>
    </a>
  </details>

  <!-- Proxies & Subscriptions group -->
  <details class="nav-group" bind:open={groupOpen.proxy_subs}>
    <summary>
      <span class="group-ttl">
        <!-- Прокси и подписки → shield -->
        <svg
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          stroke-width="2.2"
          stroke-linecap="round"
          stroke-linejoin="round"
          aria-hidden="true"
        >
          <path d="M12 2 4 6v6c0 5 3.5 9 8 10 4.5-1 8-5 8-10V6Z" />
        </svg>
        <span class="lbl">{$t('nav.group_proxy_subs')}</span>
      </span>
      <span class="nav-group-arrow">▶</span>
    </summary>
    {#if $capabilities === null || $capabilities.active_kernel !== 'xray'}
      <a
        href="#/proxies"
        class="nav-item"
        aria-current={currentTab === 'proxies' ? 'page' : undefined}
        data-label={$t('nav.proxies')}
        onclick={() => isSidebarOpen.set(false)}
        title={$t('nav.proxies')}
      >
        <Icon name="proxies" size={16} />
        <span class="lbl">{$t('nav.proxies')}</span>
        {#if $capabilities?.active_kernel === 'mihomo' && !$mihomoApiAvailable}
          <span
            class="nav-badge-warn"
            role="img"
            aria-label={$t('dash.quickstart.sidebar_badge_aria')}>!</span
          >
        {/if}
      </a>
    {/if}
    {#if $capabilities === null || $capabilities.active_kernel !== 'xray'}
      <a
        href="#/smartproxy"
        class="nav-item"
        aria-current={currentTab === 'smartproxy' ? 'page' : undefined}
        data-label={$t('nav.smartproxy')}
        onclick={() => isSidebarOpen.set(false)}
        title={$t('nav.smartproxy')}
      >
        <Icon name="smartproxy" size={16} />
        <span class="lbl">{$t('nav.smartproxy')}</span>
      </a>
    {/if}
  </details>

  <!-- Routing group -->
  <details class="nav-group" bind:open={groupOpen.routing}>
    <summary>
      <span class="group-ttl">
        <!-- Маршрутизация → routed path between two points -->
        <svg
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          stroke-width="2.2"
          stroke-linecap="round"
          stroke-linejoin="round"
          aria-hidden="true"
        >
          <circle cx="6" cy="6" r="2.5" />
          <circle cx="18" cy="18" r="2.5" />
          <path d="M8.2 7.5C10 10 14 14 15.8 16.5" />
        </svg>
        <span class="lbl">{$t('nav.group_routing')}</span>
      </span>
      <span class="nav-group-arrow">▶</span>
    </summary>
    {#if $capabilities === null || $capabilities.active_kernel !== 'xray'}
      <a
        href="#/rules"
        class="nav-item"
        aria-current={currentTab === 'rules' ? 'page' : undefined}
        data-label={$t('nav.rules')}
        onclick={() => isSidebarOpen.set(false)}
        title={$t('nav.rules')}
      >
        <Icon name="rules" size={16} />
        <span class="lbl">{$t('nav.rules')}</span>
        {#if $capabilities?.active_kernel === 'mihomo' && !$mihomoApiAvailable}
          <span
            class="nav-badge-warn"
            role="img"
            aria-label={$t('dash.quickstart.sidebar_badge_aria')}>!</span
          >
        {/if}
      </a>
    {/if}
  </details>

  <!-- Observability group -->
  <details class="nav-group" bind:open={groupOpen.observability}>
    <summary>
      <span class="group-ttl">
        <!-- Наблюдение → activity/pulse -->
        <svg
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          stroke-width="2.2"
          stroke-linecap="round"
          stroke-linejoin="round"
          aria-hidden="true"
        >
          <path d="M3 12h4l2-7 4 14 2-7h6" />
        </svg>
        <span class="lbl">{$t('nav.group_observability')}</span>
      </span>
      <span class="nav-group-arrow">▶</span>
    </summary>
    {#if $capabilities === null || $capabilities.active_kernel !== 'xray'}
      <a
        href="#/connections"
        class="nav-item"
        aria-current={currentTab === 'connections' ? 'page' : undefined}
        data-label={$t('nav.connections')}
        onclick={() => isSidebarOpen.set(false)}
        title={$t('nav.connections')}
      >
        <Icon name="connections" size={16} />
        <span class="lbl">{$t('nav.connections')}</span>
        {#if $capabilities?.active_kernel === 'mihomo' && !$mihomoApiAvailable}
          <span
            class="nav-badge-warn"
            role="img"
            aria-label={$t('dash.quickstart.sidebar_badge_aria')}>!</span
          >
        {/if}
      </a>
    {/if}
    {#if $capabilities === null || $capabilities.active_kernel !== 'xray'}
      <a
        href="#/traffic"
        class="nav-item"
        aria-current={currentTab === 'traffic' ? 'page' : undefined}
        data-label={$t('nav.traffic')}
        onclick={() => isSidebarOpen.set(false)}
        title={$t('nav.traffic')}
      >
        <Icon name="traffic" size={16} />
        <span class="lbl">{$t('nav.traffic')}</span>
      </a>
      <a
        href="#/trafficquotas"
        class="nav-item"
        aria-current={currentTab === 'trafficquotas' ? 'page' : undefined}
        data-label={$t('nav.trafficquotas')}
        onclick={() => isSidebarOpen.set(false)}
        title={$t('nav.trafficquotas')}
      >
        <Icon name="trafficquotas" size={16} />
        <span class="lbl">{$t('nav.trafficquotas')}</span>
      </a>
    {/if}
    <a
      href="#/logs"
      class="nav-item"
      aria-current={currentTab === 'logs' ? 'page' : undefined}
      data-label={$t('nav.logs')}
      onclick={() => isSidebarOpen.set(false)}
      title={$t('nav.logs')}
    >
      <Icon name="logs" size={16} />
      <span class="lbl">{$t('nav.logs')}</span>
    </a>
  </details>

  <!-- System group -->
  <details class="nav-group" bind:open={groupOpen.system}>
    <summary>
      <span class="group-ttl">
        <!-- Система → wrench + screwdriver crossed -->
        <svg
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          stroke-width="2"
          stroke-linecap="round"
          stroke-linejoin="round"
          aria-hidden="true"
          ><path
            d="M14.7 6.3a4 4 0 0 0-5.4 5.4l-7 7V21h3.3l7-7a4 4 0 0 0 5.4-5.4l-2.3 2.3-2-2 1-1z"
          /><path d="m17 14 4 4-2 2-4-4" /></svg
        >
        <span class="lbl">{$t('nav.group_system')}</span>
      </span>
      <span class="nav-group-arrow">▶</span>
    </summary>
    <a
      href="#/services"
      class="nav-item"
      aria-current={currentTab === 'services' ? 'page' : undefined}
      data-label={$t('nav.services')}
      onclick={() => isSidebarOpen.set(false)}
      title={$t('nav.services')}
    >
      <Icon name="services" size={16} />
      <span class="lbl">{$t('nav.services')}</span>
    </a>
    <a
      href="#/dat"
      class="nav-item"
      aria-current={currentTab === 'dat' ? 'page' : undefined}
      data-label={$t('nav.dat')}
      onclick={() => isSidebarOpen.set(false)}
      title={$t('nav.dat')}
    >
      <Icon name="dat" size={16} />
      <span class="lbl">{$t('nav.dat')}</span>
    </a>
    <a
      href="#/console"
      class="nav-item"
      aria-current={currentTab === 'console' ? 'page' : undefined}
      data-label={$t('nav.console')}
      onclick={() => isSidebarOpen.set(false)}
      title={$t('nav.console')}
    >
      <Icon name="console" size={16} />
      <span class="lbl">{$t('nav.console')}</span>
    </a>
    <a
      href="#/network"
      class="nav-item"
      aria-current={currentTab === 'network' ? 'page' : undefined}
      data-label={$t('nav.network')}
      onclick={() => isSidebarOpen.set(false)}
      title={$t('nav.network')}
    >
      <Icon name="network" size={16} />
      <span class="lbl">{$t('nav.network')}</span>
    </a>
    <a
      href="#/editor"
      class="nav-item"
      aria-current={currentTab === 'editor' ? 'page' : undefined}
      data-label={$t('nav.editor')}
      onclick={() => isSidebarOpen.set(false)}
      title={$t('nav.editor')}
    >
      <Icon name="editor" size={16} />
      <span class="lbl">{$t('nav.editor')}</span>
    </a>
    <a
      href="#/settings"
      class="nav-item"
      aria-current={currentTab === 'settings' ? 'page' : undefined}
      data-label={$t('nav.settings')}
      onclick={() => isSidebarOpen.set(false)}
      title={$t('nav.settings')}
    >
      <Icon name="settings" size={16} />
      <span class="lbl">{$t('nav.settings')}</span>
    </a>
  </details>
</nav>

<div style="border-top: 1px solid var(--border); padding: 0.5rem 0; background: var(--bg-card);">
  {#if pwaInstallPrompt}
    <button class="nav-item" onclick={onInstallPWA} title={$t('nav.install_pwa')}>
      <Icon name="pwa" size={16} />
      <span class="lbl">{$t('nav.install_pwa')}</span>
    </button>
  {/if}
  <button
    class="nav-item"
    onclick={onToggleTheme}
    title={theme === 'dark' ? $t('nav.theme_light') : $t('nav.theme_dark')}
  >
    <Icon name={theme === 'dark' ? 'sun' : 'moon'} size={16} />
    <span class="lbl">{theme === 'dark' ? $t('nav.theme_light') : $t('nav.theme_dark')}</span>
  </button>
  <button class="nav-item" onclick={onLogout} disabled={loading} title={$t('auth.logout')}>
    <Icon name="logout" size={16} />
    <span class="lbl">{loading ? $t('auth.logging_out') : $t('auth.logout')}</span>
  </button>
  <button
    class="nav-item"
    onclick={() => isSidebarCollapsed.update((v) => !v)}
    title={$t($isSidebarCollapsed ? 'nav.expand_sidebar' : 'nav.collapse_sidebar')}
    data-label={$t($isSidebarCollapsed ? 'nav.expand_sidebar' : 'nav.collapse_sidebar')}
  >
    <Icon
      name="chevron-right"
      size={16}
      class={$isSidebarCollapsed ? 'collapse-toggle-icon' : 'collapse-toggle-icon is-expanded'}
    />
    <span class="lbl"
      >{$t($isSidebarCollapsed ? 'nav.expand_sidebar' : 'nav.collapse_sidebar')}</span
    >
  </button>
</div>

<style>
  :global(.collapse-toggle-icon) {
    flex-shrink: 0;
    transition: transform 0.2s ease;
  }
  :global(.collapse-toggle-icon.is-expanded) {
    transform: rotate(180deg);
  }

  .nav-badge-warn {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 16px;
    height: 16px;
    border-radius: 50%;
    background: var(--warning);
    color: #03182a;
    font-size: 9px;
    font-weight: 600;
    line-height: 1;
    margin-left: auto;
    flex-shrink: 0;
    box-shadow: 0 0 6px rgba(240, 180, 80, 0.5);
  }
</style>
