<script lang="ts">
  import { t } from '../i18n';
  import { isSidebarOpen } from '../stores';
  import Icon from '../lib/components/Icon.svelte';

  export let currentTab: string = 'dashboard';
  export let onSwitchTab: (tab: string) => void = () => {};
  export let theme: string = 'light';
  export let onToggleTheme: () => void = () => {};
  export let onLogout: () => void = () => {};
  export let loading: boolean = false;
  export let pwaInstallPrompt: any = null;
  export let onInstallPWA: () => void = () => {};
  export let isCollapsed: boolean = false;
  export let onToggleCollapse: () => void = () => {};


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
  <!-- Core group -->
  <details class="nav-group" open>
    <summary>
      <span class="group-ttl">
        <!-- Основное → compass: центр / навигация -->
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
        {$t('nav.group_core')}
      </span>
      <span class="nav-group-arrow">▶</span>
    </summary>
    <a
      href="#/dashboard"
      class="nav-item"
      class:active={currentTab === 'dashboard'}
      on:click={() => isSidebarOpen.set(false)}
      title={$t('nav.dashboard')}
    >
      <Icon name="dashboard" size={16} />
      {$t('nav.dashboard')}
    </a>
    <a
      href="#/services"
      class="nav-item"
      class:active={currentTab === 'services'}
      on:click={() => isSidebarOpen.set(false)}
      title={$t('nav.services')}
    >
      <Icon name="services" size={16} />
      {$t('nav.services')}
    </a>
    <a
      href="#/editor"
      class="nav-item"
      class:active={currentTab === 'editor'}
      on:click={() => isSidebarOpen.set(false)}
      title={$t('nav.editor')}
    >
      <Icon name="editor" size={16} />
      {$t('nav.editor')}
    </a>
    <a
      href="#/settings"
      class="nav-item"
      class:active={currentTab === 'settings'}
      on:click={() => isSidebarOpen.set(false)}
      title={$t('nav.settings')}
    >
      <Icon name="settings" size={16} />
      {$t('nav.settings')}
    </a>
  </details>

  <!-- Services group -->
  <details class="nav-group" open>
    <summary>
      <span class="group-ttl">
        <!-- Сервисы → server stack -->
        <svg
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          stroke-width="2"
          stroke-linecap="round"
          stroke-linejoin="round"
          aria-hidden="true"
          ><rect x="3" y="3.5" width="18" height="6" rx="1.5" /><rect
            x="3"
            y="14.5"
            width="18"
            height="6"
            rx="1.5"
          /><circle cx="7" cy="6.5" r=".9" fill="currentColor" stroke="none" /><circle
            cx="7"
            cy="17.5"
            r=".9"
            fill="currentColor"
            stroke="none"
          /><path d="M11 6.5h7M11 17.5h7" /></svg
        >
        {$t('nav.group_services')}
      </span>
      <span class="nav-group-arrow">▶</span>
    </summary>
    <a
      href="#/logs"
      class="nav-item"
      class:active={currentTab === 'logs'}
      on:click={() => isSidebarOpen.set(false)}
      title={$t('nav.logs')}
    >
      <Icon name="logs" size={16} />
      {$t('nav.logs')}
    </a>
    <a
      href="#/connections"
      class="nav-item"
      class:active={currentTab === 'connections'}
      on:click={() => isSidebarOpen.set(false)}
      title={$t('nav.connections')}
    >
      <Icon name="connections" size={16} />
      {$t('nav.connections')}
    </a>
    <a
      href="#/dat"
      class="nav-item"
      class:active={currentTab === 'dat'}
      on:click={() => isSidebarOpen.set(false)}
      title={$t('nav.dat')}
    >
      <Icon name="dat" size={16} />
      {$t('nav.dat')}
    </a>
    <a
      href="#/mihomo-gen"
      class="nav-item"
      class:active={currentTab === 'mihomo-gen'}
      on:click={() => isSidebarOpen.set(false)}
      title="Mihomo Generator"
    >
      <Icon name="settings" size={16} />
      Mihomo Generator
    </a>
    <a
      href="#/console"
      class="nav-item"
      class:active={currentTab === 'console'}
      on:click={() => isSidebarOpen.set(false)}
      title={$t('nav.console')}
    >
      <Icon name="console" size={16} />
      {$t('nav.console')}
    </a>
  </details>

  <!-- Proxy & Rules group -->
  <details class="nav-group" open>
    <summary>
      <span class="group-ttl">
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
        {$t('nav.group_proxy')}
      </span>
      <span class="nav-group-arrow">▶</span>
    </summary>
    <a
      href="#/proxies"
      class="nav-item"
      class:active={currentTab === 'proxies'}
      on:click={() => isSidebarOpen.set(false)}
      title={$t('nav.proxies')}
    >
      <Icon name="proxies" size={16} />
      {$t('nav.proxies')}
    </a>
    <a
      href="#/rules"
      class="nav-item"
      class:active={currentTab === 'rules'}
      on:click={() => isSidebarOpen.set(false)}
      title={$t('nav.rules')}
    >
      <Icon name="rules" size={16} />
      {$t('nav.rules')}
    </a>
    <a
      href="#/subscriptions"
      class="nav-item"
      class:active={currentTab === 'subscriptions'}
      on:click={() => isSidebarOpen.set(false)}
      title={$t('nav.subscriptions')}
    >
      <Icon name="subscriptions" size={16} />
      {$t('nav.subscriptions')}
    </a>
    <a
      href="#/smartproxy"
      class="nav-item"
      class:active={currentTab === 'smartproxy'}
      on:click={() => isSidebarOpen.set(false)}
      title={$t('nav.smartproxy')}
    >
      <Icon name="smartproxy" size={16} />
      {$t('nav.smartproxy')}
    </a>
  </details>

  <!-- Tools group -->
  <details class="nav-group" open>
    <summary>
      <span class="group-ttl">
        <!-- Инструменты → wrench + screwdriver crossed -->
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
        {$t('nav.group_tools')}
      </span>
      <span class="nav-group-arrow">▶</span>
    </summary>
    <a
      href="#/traffic"
      class="nav-item"
      class:active={currentTab === 'traffic'}
      on:click={() => isSidebarOpen.set(false)}
      title={$t('nav.traffic')}
    >
      <Icon name="traffic" size={16} />
      {$t('nav.traffic')}
    </a>
    <a
      href="#/trafficquotas"
      class="nav-item"
      class:active={currentTab === 'trafficquotas'}
      on:click={() => isSidebarOpen.set(false)}
      title={$t('nav.trafficquotas')}
    >
      <Icon name="trafficquotas" size={16} />
      {$t('nav.trafficquotas')}
    </a>
    <a
      href="#/network"
      class="nav-item"
      class:active={currentTab === 'network'}
      on:click={() => isSidebarOpen.set(false)}
      title={$t('nav.network')}
    >
      <Icon name="network" size={16} />
      {$t('nav.network')}
    </a>
  </details>
</nav>

<div style="border-top: 1px solid #03101e; padding: 0.5rem 0; background: var(--bg-deep);">
  {#if pwaInstallPrompt}
    <button class="nav-item" on:click={onInstallPWA} title={$t('nav.install_pwa')}>
      <Icon name="pwa" size={16} />
      {$t('nav.install_pwa')}
    </button>
  {/if}
  <button
    class="nav-item"
    on:click={onToggleTheme}
    title={theme === 'dark' ? $t('nav.theme_light') : $t('nav.theme_dark')}
  >
    <Icon name={theme === 'dark' ? 'sun' : 'moon'} size={16} />
    {theme === 'dark' ? $t('nav.theme_light') : $t('nav.theme_dark')}
  </button>
  <button class="nav-item" on:click={onLogout} disabled={loading} title={$t('auth.logout')}>
    <Icon name="logout" size={16} />
    {loading ? $t('auth.logging_out') : $t('auth.logout')}
  </button>
</div>
