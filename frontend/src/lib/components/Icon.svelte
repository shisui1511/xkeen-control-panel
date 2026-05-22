<script lang="ts">
  import Dashboard from './icons/Dashboard.svelte';
  import Services from './icons/Services.svelte';
  import Editor from './icons/Editor.svelte';
  import Settings from './icons/Settings.svelte';
  import Logs from './icons/Logs.svelte';
  import Connections from './icons/Connections.svelte';
  import Dat from './icons/Dat.svelte';
  import Console from './icons/Console.svelte';
  import Proxies from './icons/Proxies.svelte';
  import Rules from './icons/Rules.svelte';
  import Subscriptions from './icons/Subscriptions.svelte';
  import SmartProxy from './icons/SmartProxy.svelte';
  import Traffic from './icons/Traffic.svelte';
  import Network from './icons/Network.svelte';
  import Pwa from './icons/Pwa.svelte';
  import Sun from './icons/Sun.svelte';
  import Moon from './icons/Moon.svelte';
  import Logout from './icons/Logout.svelte';
  import Check from './icons/Check.svelte';
  import Cross from './icons/Cross.svelte';
  import Info from './icons/Info.svelte';
  import Warning from './icons/Warning.svelte';
  import Play from './icons/Play.svelte';
  import Stop from './icons/Stop.svelte';
  import Refresh from './icons/Refresh.svelte';
  import Add from './icons/Add.svelte';
  import Delete from './icons/Delete.svelte';
  import Edit from './icons/Edit.svelte';
  import ChevronDown from './icons/ChevronDown.svelte';
  import ChevronRight from './icons/ChevronRight.svelte';
  import Pause from './icons/Pause.svelte';
  import Download from './icons/Download.svelte';
  import Trash from './icons/Trash.svelte';
  import ArrowDown from './icons/ArrowDown.svelte';

  let {
    name,
    size = 16,
    color = 'currentColor',
    class: className = ''
  } = $props<{
    name: string;
    size?: number;
    color?: string;
    class?: string;
  }>();
</script>

{#if name === 'dashboard'}
  <Dashboard {size} {color} class={className} />
{:else if name === 'services'}
  <Services {size} {color} class={className} />
{:else if name === 'editor'}
  <Editor {size} {color} class={className} />
{:else if name === 'settings'}
  <Settings {size} {color} class={className} />
{:else if name === 'logs'}
  <Logs {size} {color} class={className} />
{:else if name === 'connections'}
  <Connections {size} {color} class={className} />
{:else if name === 'dat'}
  <Dat {size} {color} class={className} />
{:else if name === 'console'}
  <Console {size} {color} class={className} />
{:else if name === 'proxies'}
  <Proxies {size} {color} class={className} />
{:else if name === 'rules'}
  <Rules {size} {color} class={className} />
{:else if name === 'subscriptions'}
  <Subscriptions {size} {color} class={className} />
{:else if name === 'smartproxy'}
  <SmartProxy {size} {color} class={className} />
{:else if name === 'traffic' || name === 'trafficquotas'}
  <Traffic {size} {color} class={className} />
{:else if name === 'network'}
  <Network {size} {color} class={className} />
{:else if name === 'pwa'}
  <Pwa {size} {color} class={className} />
{:else if name === 'sun'}
  <Sun {size} {color} class={className} />
{:else if name === 'moon'}
  <Moon {size} {color} class={className} />
{:else if name === 'logout'}
  <Logout {size} {color} class={className} />
{:else if name === 'check'}
  <Check {size} {color} class={className} />
{:else if name === 'cross' || name === 'close'}
  <Cross {size} {color} class={className} />
{:else if name === 'info'}
  <Info {size} {color} class={className} />
{:else if name === 'warning'}
  <Warning {size} {color} class={className} />
{:else if name === 'play'}
  <Play {size} {color} class={className} />
{:else if name === 'stop'}
  <Stop {size} {color} class={className} />
{:else if name === 'refresh' || name === 'restart'}
  <Refresh {size} {color} class={className} />
{:else if name === 'add'}
  <Add {size} {color} class={className} />
{:else if name === 'delete' || name === 'trash'}
  <Delete {size} {color} class={className} />
{:else if name === 'edit'}
  <Edit {size} {color} class={className} />
{:else if name === 'chevron-down'}
  <ChevronDown {size} {color} class={className} />
{:else if name === 'chevron-right'}
  <ChevronRight {size} {color} class={className} />
{:else if name === 'pause'}
  <Pause {size} {color} class={className} />
{:else if name === 'download'}
  <Download {size} {color} class={className} />
{:else if name === 'arrow-down'}
  <ArrowDown {size} {color} class={className} />
{/if}
