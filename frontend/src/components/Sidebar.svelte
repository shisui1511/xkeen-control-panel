<script lang="ts">
  import { t } from '../i18n'
  import { isSidebarOpen } from '../stores'
  import Icon from '../lib/components/Icon.svelte'

  export let currentTab: string = 'dashboard'
  export let onSwitchTab: (tab: string) => void = () => {}
  export let theme: string = 'light'
  export let onToggleTheme: () => void = () => {}
  export let onLogout: () => void = () => {}
  export let loading: boolean = false
  export let pwaInstallPrompt: any = null
  export let onInstallPWA: () => void = () => {}

  function navigate(tab: string) {
    onSwitchTab(tab)
    // Auto-close on mobile after navigation
    isSidebarOpen.set(false)
  }

  let groupStates = {
    core: localStorage.getItem('sidebar.groups.core') !== 'false',
    services: localStorage.getItem('sidebar.groups.services') !== 'false',
    proxy: localStorage.getItem('sidebar.groups.proxy') !== 'false',
    tools: localStorage.getItem('sidebar.groups.tools') !== 'false'
  }

  function toggleGroup(group: keyof typeof groupStates, open: boolean) {
    groupStates[group] = open
    localStorage.setItem(`sidebar.groups.${group}`, String(open))
  }

  $: {
    if (['dashboard', 'services', 'editor', 'settings'].includes(currentTab)) {
      if (!groupStates.core) toggleGroup('core', true)
    }
    if (['logs', 'connections', 'dat', 'console'].includes(currentTab)) {
      if (!groupStates.services) toggleGroup('services', true)
    }
    if (['proxies', 'rules', 'subscriptions', 'smartproxy'].includes(currentTab)) {
      if (!groupStates.proxy) toggleGroup('proxy', true)
    }
    if (['traffic', 'trafficquotas', 'network'].includes(currentTab)) {
      if (!groupStates.tools) toggleGroup('tools', true)
    }
  }
</script>

<div class="sidebar-logo">
  <span style="display: inline-flex; align-items: center; gap: 8px;"><Icon name="smartproxy" size={18} /> XKeen CP</span>
</div>

<nav style="flex: 1; overflow-y: auto; padding: 8px 0;">
  <!-- Core group -->
  <details class="nav-group" open={groupStates.core} on:toggle={(e) => toggleGroup('core', e.currentTarget.open)}>
    <summary>
      {$t('nav.group_core')}
      <span class="nav-group-arrow">▶</span>
    </summary>
    <button
      class="nav-item"
      class:active={currentTab === 'dashboard'}
      on:click={() => navigate('dashboard')}
      title={$t('nav.monitoring')}
    >
      <Icon name="dashboard" size={16} /> {$t('nav.monitoring')}
    </button>
    <button
      class="nav-item"
      class:active={currentTab === 'services'}
      on:click={() => navigate('services')}
      title={$t('nav.services')}
    >
      <Icon name="services" size={16} /> {$t('nav.services')}
    </button>
    <button
      class="nav-item"
      class:active={currentTab === 'editor'}
      on:click={() => navigate('editor')}
      title={$t('nav.editor')}
    >
      <Icon name="editor" size={16} /> {$t('nav.editor')}
    </button>
    <button
      class="nav-item"
      class:active={currentTab === 'settings'}
      on:click={() => navigate('settings')}
      title={$t('nav.settings')}
    >
      <Icon name="settings" size={16} /> {$t('nav.settings')}
    </button>
  </details>

  <!-- Services group -->
  <details class="nav-group" open={groupStates.services} on:toggle={(e) => toggleGroup('services', e.currentTarget.open)}>
    <summary>
      {$t('nav.group_services')}
      <span class="nav-group-arrow">▶</span>
    </summary>
    <button
      class="nav-item"
      class:active={currentTab === 'logs'}
      on:click={() => navigate('logs')}
      title={$t('nav.logs')}
    >
      <Icon name="logs" size={16} /> {$t('nav.logs')}
    </button>
    <button
      class="nav-item"
      class:active={currentTab === 'connections'}
      on:click={() => navigate('connections')}
      title={$t('nav.connections')}
    >
      <Icon name="connections" size={16} /> {$t('nav.connections')}
    </button>
    <button
      class="nav-item"
      class:active={currentTab === 'dat'}
      on:click={() => navigate('dat')}
      title={$t('nav.dat')}
    >
      <Icon name="dat" size={16} /> {$t('nav.dat')}
    </button>
    <button
      class="nav-item"
      class:active={currentTab === 'console'}
      on:click={() => navigate('console')}
      title={$t('nav.console')}
    >
      <Icon name="console" size={16} /> {$t('nav.console')}
    </button>
  </details>

  <!-- Proxy & Rules group -->
  <details class="nav-group" open={groupStates.proxy} on:toggle={(e) => toggleGroup('proxy', e.currentTarget.open)}>
    <summary>
      {$t('nav.group_proxy')}
      <span class="nav-group-arrow">▶</span>
    </summary>
    <button
      class="nav-item"
      class:active={currentTab === 'proxies'}
      on:click={() => navigate('proxies')}
      title={$t('nav.proxies')}
    >
      <Icon name="proxies" size={16} /> {$t('nav.proxies')}
    </button>
    <button
      class="nav-item"
      class:active={currentTab === 'rules'}
      on:click={() => navigate('rules')}
      title={$t('nav.rules')}
    >
      <Icon name="rules" size={16} /> {$t('nav.rules')}
    </button>
    <button
      class="nav-item"
      class:active={currentTab === 'subscriptions'}
      on:click={() => navigate('subscriptions')}
      title={$t('nav.subscriptions')}
    >
      <Icon name="subscriptions" size={16} /> {$t('nav.subscriptions')}
    </button>
    <button
      class="nav-item"
      class:active={currentTab === 'smartproxy'}
      on:click={() => navigate('smartproxy')}
      title={$t('nav.smartproxy')}
    >
      <Icon name="smartproxy" size={16} /> {$t('nav.smartproxy')}
    </button>
  </details>

  <!-- Tools group -->
  <details class="nav-group" open={groupStates.tools} on:toggle={(e) => toggleGroup('tools', e.currentTarget.open)}>
    <summary>
      {$t('nav.group_tools')}
      <span class="nav-group-arrow">▶</span>
    </summary>
    <button
      class="nav-item"
      class:active={currentTab === 'traffic'}
      on:click={() => navigate('traffic')}
      title={$t('nav.traffic')}
    >
      <Icon name="traffic" size={16} /> {$t('nav.traffic')}
    </button>
    <button
      class="nav-item"
      class:active={currentTab === 'trafficquotas'}
      on:click={() => navigate('trafficquotas')}
      title={$t('nav.trafficquotas')}
    >
      <Icon name="trafficquotas" size={16} /> {$t('nav.trafficquotas')}
    </button>
    <button
      class="nav-item"
      class:active={currentTab === 'network'}
      on:click={() => navigate('network')}
      title={$t('nav.network')}
    >
      <Icon name="network" size={16} /> {$t('nav.network')}
    </button>
  </details>
</nav>

<div style="border-top: 1px solid var(--border); padding: 0.5rem 0;">
  {#if pwaInstallPrompt}
    <button class="nav-item" on:click={onInstallPWA} title={$t('nav.install_pwa')}>
      <Icon name="pwa" size={16} /> {$t('nav.install_pwa')}
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
  <button
    class="nav-item"
    on:click={onLogout}
    disabled={loading}
    title={$t('auth.logout')}
  >
    <Icon name="logout" size={16} />
    {loading ? $t('auth.logging_out') : $t('auth.logout')}
  </button>
</div>
