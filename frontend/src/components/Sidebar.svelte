<script lang="ts">
  import { t } from '../i18n'
  import { isSidebarOpen } from '../stores'

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
</script>

<div class="sidebar-logo">
  ⚡ XKeen CP
</div>

<nav style="flex: 1; overflow-y: auto; padding: 8px 0;">
  <!-- Core group -->
  <details class="nav-group" open>
    <summary>
      {$t('nav.group_core')}
      <span class="nav-group-arrow">▶</span>
    </summary>
    <button
      class="nav-item"
      class:active={currentTab === 'dashboard'}
      on:click={() => navigate('dashboard')}
      title={$t('nav.dashboard')}
    >
      📊 {$t('nav.dashboard')}
    </button>
    <button
      class="nav-item"
      class:active={currentTab === 'services'}
      on:click={() => navigate('services')}
      title={$t('nav.services')}
    >
      🚀 {$t('nav.services')}
    </button>
    <button
      class="nav-item"
      class:active={currentTab === 'editor'}
      on:click={() => navigate('editor')}
      title={$t('nav.editor')}
    >
      📝 {$t('nav.editor')}
    </button>
    <button
      class="nav-item"
      class:active={currentTab === 'settings'}
      on:click={() => navigate('settings')}
      title={$t('nav.settings')}
    >
      ⚙️ {$t('nav.settings')}
    </button>
  </details>

  <!-- Services group -->
  <details class="nav-group" open>
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
      📋 {$t('nav.logs')}
    </button>
    <button
      class="nav-item"
      class:active={currentTab === 'connections'}
      on:click={() => navigate('connections')}
      title={$t('nav.connections')}
    >
      🔗 {$t('nav.connections')}
    </button>
    <button
      class="nav-item"
      class:active={currentTab === 'dat'}
      on:click={() => navigate('dat')}
      title={$t('nav.dat')}
    >
      🌍 {$t('nav.dat')}
    </button>
    <button
      class="nav-item"
      class:active={currentTab === 'console'}
      on:click={() => navigate('console')}
      title={$t('nav.console')}
    >
      💻 {$t('nav.console')}
    </button>
  </details>

  <!-- Proxy & Rules group -->
  <details class="nav-group" open>
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
      🌐 {$t('nav.proxies')}
    </button>
    <button
      class="nav-item"
      class:active={currentTab === 'rules'}
      on:click={() => navigate('rules')}
      title={$t('nav.rules')}
    >
      📋 {$t('nav.rules')}
    </button>
    <button
      class="nav-item"
      class:active={currentTab === 'subscriptions'}
      on:click={() => navigate('subscriptions')}
      title={$t('nav.subscriptions')}
    >
      📡 {$t('nav.subscriptions')}
    </button>
    <button
      class="nav-item"
      class:active={currentTab === 'smartproxy'}
      on:click={() => navigate('smartproxy')}
      title={$t('nav.smartproxy')}
    >
      ⚡ {$t('nav.smartproxy')}
    </button>
  </details>

  <!-- Tools group -->
  <details class="nav-group" open>
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
      📈 {$t('nav.traffic')}
    </button>
    <button
      class="nav-item"
      class:active={currentTab === 'trafficquotas'}
      on:click={() => navigate('trafficquotas')}
      title={$t('nav.trafficquotas')}
    >
      📊 {$t('nav.trafficquotas')}
    </button>
    <button
      class="nav-item"
      class:active={currentTab === 'network'}
      on:click={() => navigate('network')}
      title={$t('nav.network')}
    >
      🌐 {$t('nav.network')}
    </button>
  </details>
</nav>

<div style="border-top: 1px solid var(--border); padding: 0.5rem 0;">
  {#if pwaInstallPrompt}
    <button class="nav-item" on:click={onInstallPWA} title={$t('nav.install_pwa')}>
      📲 {$t('nav.install_pwa')}
    </button>
  {/if}
  <button
    class="nav-item"
    on:click={onToggleTheme}
    title={theme === 'dark' ? $t('nav.theme_light') : $t('nav.theme_dark')}
  >
    {theme === 'dark' ? '☀️' : '🌙'}
    {theme === 'dark' ? $t('nav.theme_light') : $t('nav.theme_dark')}
  </button>
  <button
    class="nav-item"
    on:click={onLogout}
    disabled={loading}
    title={$t('auth.logout')}
  >
    🚪 {loading ? $t('auth.logging_out') : $t('auth.logout')}
  </button>
</div>
