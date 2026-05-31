<script lang="ts">
  import { onMount } from 'svelte';
  import { slide } from 'svelte/transition';
  import { t } from './i18n';
  import PageHeader from './PageHeader.svelte';
  import Icon from './lib/components/Icon.svelte';

  export let onSwitchTab: (tab: string) => void = () => {};

  interface ToolResult {
    success: boolean;
    output?: string;
    records?: string[];
    ip?: string;
    error?: string;
  }

  let activeTool = 'ping';
  let host = '';
  let url = '';
  let recordType = 'A';
  let count = 4;
  let maxHops = 20;
  let timeout = 10;
  let loading = false;
  let result: ToolResult | null = null;
  let publicIP = '';

  let showSettings: Record<string, boolean> = {
    ping: false,
    traceroute: false,
    dns: false,
    http: false
  };

  // New tools state
  let selectedProxy = '';
  let proxyTargetPreset = 'https://www.google.com';
  let customProxyURL = 'https://';
  let proxyTimeout = 5000;
  let showProxySettings = false;

  let portHost = '';
  let portNumber: number | null = null;
  let portTimeout = 5000;
  let showPortSettings = false;

  let mihomoGroups: string[] = [];
  let mihomoProxies: string[] = [];

  // Local storage history state
  interface HistoryItem {
    type: 'ping' | 'traceroute' | 'dns' | 'http' | 'proxy' | 'port';
    label: string;
    params: any;
    timestamp: number;
  }

  let historyList: HistoryItem[] = [];

  // DOM elements for focus
  let hostInput: HTMLInputElement;
  let urlInput: HTMLInputElement;
  let proxyTargetInput: HTMLInputElement;
  let portHostInput: HTMLInputElement;
  let portNumberInput: HTMLInputElement;

  const recordTypes = ['A', 'AAAA', 'CNAME', 'MX', 'NS', 'TXT'];

  function validateHost(h: string): boolean {
    const hostRegex = /^[a-zA-Z0-9][-a-zA-Z0-9.]*[a-zA-Z0-9]$/;
    return hostRegex.test(h);
  }

  function validateURL(u: string): boolean {
    try {
      new URL(u);
      return true;
    } catch (e) {
      return false;
    }
  }

  function toggleSettings(tool: string) {
    showSettings[tool] = !showSettings[tool];
  }

  // Load history from localStorage
  function loadHistory() {
    try {
      const stored = localStorage.getItem('net_history');
      if (stored) {
        historyList = JSON.parse(stored);
      }
    } catch (e) {
      historyList = [];
    }
  }

  // Save item to history
  function saveHistory(item: Omit<HistoryItem, 'timestamp'>) {
    const newItem: HistoryItem = {
      ...item,
      timestamp: Date.now()
    };
    
    // Uniqueness constraint
    historyList = historyList.filter(x => {
      if (x.type !== newItem.type) return true;
      if (newItem.type === 'ping' || newItem.type === 'traceroute' || newItem.type === 'dns') {
        return x.params.host !== newItem.params.host;
      }
      if (newItem.type === 'http') {
        return x.params.url !== newItem.params.url;
      }
      if (newItem.type === 'proxy') {
        return x.params.proxy_name !== newItem.params.proxy_name || x.params.url !== newItem.params.url;
      }
      if (newItem.type === 'port') {
        return x.params.host !== newItem.params.host || x.params.port !== newItem.params.port;
      }
      return true;
    });

    historyList = [newItem, ...historyList].slice(0, 5);
    try {
      localStorage.setItem('net_history', JSON.stringify(historyList));
    } catch (e) {
      // ignore
    }
  }

  function clearHistory() {
    historyList = [];
    try {
      localStorage.removeItem('net_history');
    } catch (e) {
      // ignore
    }
  }

  function selectHistoryItem(item: HistoryItem) {
    activeTool = item.type;
    result = null;
    if (item.type === 'ping' || item.type === 'traceroute' || item.type === 'dns') {
      host = item.params.host;
      if (item.type === 'dns' && item.params.record_type) {
        recordType = item.params.record_type;
      }
      setTimeout(() => hostInput?.focus(), 50);
    } else if (item.type === 'http') {
      url = item.params.url;
      setTimeout(() => urlInput?.focus(), 50);
    } else if (item.type === 'proxy') {
      selectedProxy = item.params.proxy_name;
      proxyTargetPreset = item.params.preset || 'custom';
      customProxyURL = item.params.url;
      setTimeout(() => {
        if (proxyTargetPreset === 'custom') {
          proxyTargetInput?.focus();
        }
      }, 50);
    } else if (item.type === 'port') {
      portHost = item.params.host;
      portNumber = item.params.port;
      setTimeout(() => portHostInput?.focus(), 50);
    }
  }

  // Fetch Mihomo proxies to populate proxy selection dropdown
  async function fetchClashProxies() {
    try {
      const res = await fetch('/api/mihomo/proxy/proxies');
      if (res.ok) {
        const data = await res.json();
        const groups: string[] = [];
        const proxies: string[] = [];
        for (const [name, p] of Object.entries(data.proxies || {})) {
          const type = (p as any).type;
          if (type === 'Selector' || type === 'Fallback' || type === 'URLTest' || type === 'LoadBalance') {
            groups.push(name);
          } else if (type !== 'Direct' && type !== 'Reject') {
            proxies.push(name);
          }
        }
        mihomoGroups = groups.sort();
        mihomoProxies = proxies.sort();
      }
    } catch (e) {
      // ignore
    }
  }

  $: finalProxyURL = proxyTargetPreset === 'custom' ? customProxyURL : proxyTargetPreset;

  async function runPing() {
    if (!host) return;
    if (!validateHost(host)) {
      result = { success: false, error: $t('net.invalid_host') };
      return;
    }
    loading = true;
    activeTool = 'ping';
    result = null;
    try {
      const csrfToken = localStorage.getItem('csrf_token');
      const res = await fetch('/api/network/ping', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrfToken || '' },
        body: JSON.stringify({ host, count })
      });
      result = await res.json();
      if (res.ok && result.success) {
        saveHistory({
          type: 'ping',
          label: `[Ping] ${host}`,
          params: { host }
        });
      }
    } catch (e) {
      result = { success: false, error: 'Request failed' };
    } finally {
      loading = false;
    }
  }

  async function runTraceroute() {
    if (!host) return;
    if (!validateHost(host)) {
      result = { success: false, error: $t('net.invalid_host') };
      return;
    }
    loading = true;
    activeTool = 'traceroute';
    result = null;
    try {
      const csrfToken = localStorage.getItem('csrf_token');
      const res = await fetch('/api/network/traceroute', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrfToken || '' },
        body: JSON.stringify({ host, max_hops: maxHops })
      });
      result = await res.json();
      if (res.ok && result.success) {
        saveHistory({
          type: 'traceroute',
          label: `[Traceroute] ${host}`,
          params: { host }
        });
      }
    } catch (e) {
      result = { success: false, error: 'Request failed' };
    } finally {
      loading = false;
    }
  }

  async function runDNS() {
    if (!host) return;
    if (!validateHost(host)) {
      result = { success: false, error: $t('net.invalid_host') };
      return;
    }
    loading = true;
    activeTool = 'dns';
    result = null;
    try {
      const csrfToken = localStorage.getItem('csrf_token');
      const res = await fetch('/api/network/dns', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrfToken || '' },
        body: JSON.stringify({ host, record_type: recordType })
      });
      result = await res.json();
      if (res.ok && result.success) {
        saveHistory({
          type: 'dns',
          label: `[DNS ${recordType}] ${host}`,
          params: { host, record_type: recordType }
        });
      }
    } catch (e) {
      result = { success: false, error: 'Request failed' };
    } finally {
      loading = false;
    }
  }

  async function runHTTP() {
    if (!url) return;
    if (!validateURL(url)) {
      result = { success: false, error: $t('net.invalid_url') };
      return;
    }
    loading = true;
    activeTool = 'http';
    result = null;
    try {
      const csrfToken = localStorage.getItem('csrf_token');
      const res = await fetch('/api/network/http', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrfToken || '' },
        body: JSON.stringify({ url, timeout })
      });
      result = await res.json();
      if (res.ok && result.success) {
        saveHistory({
          type: 'http',
          label: `[HTTP] ${url}`,
          params: { url }
        });
      }
    } catch (e) {
      result = { success: false, error: 'Request failed' };
    } finally {
      loading = false;
    }
  }

  async function runProxyTest() {
    if (!selectedProxy) return;
    const target = finalProxyURL;
    if (!target) return;
    if (!validateURL(target)) {
      result = { success: false, error: $t('net.invalid_url') };
      return;
    }
    loading = true;
    activeTool = 'proxy';
    result = null;
    try {
      const csrfToken = localStorage.getItem('csrf_token');
      const res = await fetch('/api/network/proxy-test', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrfToken || '' },
        body: JSON.stringify({ proxy_name: selectedProxy, url: target, timeout: proxyTimeout })
      });
      const data = await res.json();
      if (res.ok && data.success) {
        result = {
          success: true,
          output: data.output || $t('net.proxy_test_ok', { rtt: data.delay })
        };
        saveHistory({
          type: 'proxy',
          label: `[Proxy ${selectedProxy}] ${target}`,
          params: { proxy_name: selectedProxy, url: target, preset: proxyTargetPreset }
        });
      } else {
        result = {
          success: false,
          error: data.error || $t('net.proxy_test_fail', { error: data.error || 'Connection failed' }),
          output: data.output
        };
      }
    } catch (e) {
      result = { success: false, error: 'Request failed' };
    } finally {
      loading = false;
    }
  }

  async function runPortCheck() {
    if (!portHost || portNumber === null) return;
    if (!validateHost(portHost)) {
      result = { success: false, error: $t('net.invalid_host') };
      return;
    }
    if (portNumber < 1 || portNumber > 65535) {
      result = { success: false, error: 'Port must be 1-65535' };
      return;
    }
    loading = true;
    activeTool = 'port';
    result = null;
    try {
      const csrfToken = localStorage.getItem('csrf_token');
      const res = await fetch('/api/network/port-check', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrfToken || '' },
        body: JSON.stringify({ host: portHost, port: portNumber, timeout: portTimeout })
      });
      const data = await res.json();
      if (res.ok && data.success) {
        result = {
          success: true,
          output: data.output || $t('net.port_open', { port: portNumber })
        };
        saveHistory({
          type: 'port',
          label: `[Port ${portNumber}] ${portHost}`,
          params: { host: portHost, port: portNumber }
        });
      } else {
        result = {
          success: false,
          error: data.error || $t('net.port_closed', { port: portNumber, error: data.error || 'Connection failed' }),
          output: data.output
        };
      }
    } catch (e) {
      result = { success: false, error: 'Request failed' };
    } finally {
      loading = false;
    }
  }

  async function fetchIP() {
    try {
      const res = await fetch('/api/network/ip');
      const data = await res.json();
      if (data.success) {
        publicIP = data.ip;
      }
    } catch (e) {
      // ignore
    }
  }

  function runTool(tool: string) {
    if (tool === 'ping') runPing();
    else if (tool === 'traceroute') runTraceroute();
    else if (tool === 'dns') runDNS();
    else if (tool === 'http') runHTTP();
    else if (tool === 'proxy') runProxyTest();
    else if (tool === 'port') runPortCheck();
  }

  onMount(() => {
    fetchIP();
    fetchClashProxies();
    loadHistory();
  });
</script>

<div class="container">
  <PageHeader
    title={$t('net.title')}
    subtitle={$t('net.subtitle')}
    breadcrumbs={[{ label: $t('nav.group_tools') }, { label: $t('nav.network') }]}
    {onSwitchTab}
    hideHome={true}
  />

  {#if publicIP}
    <div
      class="card mb-3"
      style="padding: 12px 18px; display: flex; align-items: center; gap: 8px;"
    >
      <Icon name="network" size={14} />
      <span style="font-size: 13.5px; font-weight: 500; color: var(--fg-secondary);">
        {$t('net.your_ip', { ip: publicIP })}
      </span>
    </div>
  {/if}

  <div class="nt-grid mb-3">
    <!-- Ping -->
    <div class="nt-card">
      <h3 style="display:flex;align-items:center;gap:8px;">
        <svg
          width="14"
          height="14"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          stroke-width="2"><circle cx="12" cy="12" r="9" /><path d="M3 12h18" /></svg
        >
        {$t('net.tab_ping')}
      </h3>
      <div class="form-group" style="margin-bottom:10px;">
        <input bind:this={hostInput} class="input" bind:value={host} placeholder="cloudflare.com" disabled={loading} />
      </div>
      {#if showSettings.ping}
        <div class="extra-settings mb-2" transition:slide={{ duration: 180 }}>
          <label for="ping-count" class="lbl">{$t('net.count')}</label>
          <input
            id="ping-count"
            type="number"
            class="input input-sm"
            bind:value={count}
            min="1"
            max="20"
          />
        </div>
      {/if}
      <div style="display:flex;gap:8px;margin-top:auto;">
        <button
          class="btn btn-primary"
          style="flex:1;"
          on:click={() => runTool('ping')}
          disabled={loading || !host}
        >
          {loading && activeTool === 'ping' ? $t('net.running') : $t('net.run')}
        </button>
        <button class="btn btn-secondary" on:click={() => toggleSettings('ping')} title="Настройки"
          >⋯</button
        >
      </div>
    </div>

    <!-- Traceroute -->
    <div class="nt-card">
      <h3 style="display:flex;align-items:center;gap:8px;">
        <svg
          width="14"
          height="14"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          stroke-width="2"><path d="M3 12h4l3-8 4 16 3-8h4" /></svg
        >
        {$t('net.tab_traceroute')}
      </h3>
      <div class="form-group" style="margin-bottom:10px;">
        <input class="input" bind:value={host} placeholder="github.com" disabled={loading} />
      </div>
      {#if showSettings.traceroute}
        <div class="extra-settings mb-2" transition:slide={{ duration: 180 }}>
          <label for="trace-hops" class="lbl">{$t('net.max_hops')}</label>
          <input
            id="trace-hops"
            type="number"
            class="input input-sm"
            bind:value={maxHops}
            min="1"
            max="30"
          />
        </div>
      {/if}
      <div style="display:flex;gap:8px;margin-top:auto;">
        <button
          class="btn btn-primary"
          style="flex:1;"
          on:click={() => runTool('traceroute')}
          disabled={loading || !host}
        >
          {loading && activeTool === 'traceroute' ? $t('net.running') : $t('net.run')}
        </button>
        <button
          class="btn btn-secondary"
          on:click={() => toggleSettings('traceroute')}
          title="Настройки">⋯</button
        >
      </div>
    </div>

    <!-- DNS lookup -->
    <div class="nt-card">
      <h3 style="display:flex;align-items:center;gap:8px;">
        <svg
          width="14"
          height="14"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          stroke-width="2"><path d="M4 4h16v16H4zM4 12h16M12 4v16" /></svg
        >
        {$t('net.tab_dns')}
      </h3>
      <div class="form-group" style="margin-bottom:10px;">
        <input class="input" bind:value={host} placeholder="api.openai.com" disabled={loading} />
      </div>
      {#if showSettings.dns}
        <div class="extra-settings mb-2" transition:slide={{ duration: 180 }}>
          <label for="dns-type" class="lbl">{$t('net.record_type')}</label>
          <select id="dns-type" class="input input-sm" bind:value={recordType}>
            {#each recordTypes as type}
              <option value={type}>{type}</option>
            {/each}
          </select>
        </div>
      {/if}
      <div style="display:flex;gap:8px;margin-top:auto;">
        <button
          class="btn btn-primary"
          style="flex:1;"
          on:click={() => runTool('dns')}
          disabled={loading || !host}
        >
          {loading && activeTool === 'dns' ? $t('net.running') : $t('net.run')}
        </button>
        <button class="btn btn-secondary" on:click={() => toggleSettings('dns')} title="Настройки"
          >⋯</button
        >
      </div>
    </div>

    <!-- HTTP Test -->
    <div class="nt-card">
      <h3 style="display:flex;align-items:center;gap:8px;">
        <svg
          width="14"
          height="14"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          stroke-width="2"
          ><circle cx="12" cy="12" r="10" /><path
            d="M12 2a15.3 15.3 0 0 1 4 10 15.3 15.3 0 0 1-4 10 15.3 15.3 0 0 1-4-10 15.3 15.3 0 0 1 4-10z"
          /><path d="M2 12h20" /></svg
        >
        {$t('net.tab_http')}
      </h3>
      <div class="form-group" style="margin-bottom:10px;">
        <input bind:this={urlInput} class="input" bind:value={url} placeholder="https://google.com" disabled={loading} />
      </div>
      {#if showSettings.http}
        <div class="extra-settings mb-2" transition:slide={{ duration: 180 }}>
          <label for="http-timeout" class="lbl">{$t('net.timeout_sec')}</label>
          <input
            id="http-timeout"
            type="number"
            class="input input-sm"
            bind:value={timeout}
            min="1"
            max="60"
          />
        </div>
      {/if}
      <div style="display:flex;gap:8px;margin-top:auto;">
        <button
          class="btn btn-primary"
          style="flex:1;"
          on:click={() => runTool('http')}
          disabled={loading || !url}
        >
          {loading && activeTool === 'http' ? $t('net.running') : $t('net.run')}
        </button>
        <button class="btn btn-secondary" on:click={() => toggleSettings('http')} title="Настройки"
          >⋯</button
        >
      </div>
    </div>

    <!-- Proxy Test -->
    <div class="nt-card">
      <h3 style="display:flex;align-items:center;gap:8px;">
        <Icon name="network" size={14} />
        {$t('net.tab_proxy_test')}
      </h3>
      
      <div class="form-group" style="margin-bottom:10px; display:flex; flex-direction:column; gap:4px;">
        <label for="proxy-select" class="lbl" style="font-size: 11px; color: var(--fg-dim); text-transform: uppercase; letter-spacing: 0.05em;">
          {$t('net.proxy_select')}
        </label>
        <select id="proxy-select" class="input" bind:value={selectedProxy} disabled={loading}>
          <option value="">-- {$t('net.proxy_select')} --</option>
          {#if mihomoGroups.length > 0}
            <optgroup label="Groups">
              {#each mihomoGroups as g}
                <option value={g}>{g}</option>
              {/each}
            </optgroup>
          {/if}
          {#if mihomoProxies.length > 0}
            <optgroup label="Proxies">
              {#each mihomoProxies as p}
                <option value={p}>{p}</option>
              {/each}
            </optgroup>
          {/if}
        </select>
      </div>

      <div class="form-group" style="margin-bottom:10px; display:flex; flex-direction:column; gap:4px;">
        <label for="proxy-target" class="lbl" style="font-size: 11px; color: var(--fg-dim); text-transform: uppercase; letter-spacing: 0.05em;">
          {$t('net.target_url')}
        </label>
        <select id="proxy-target" class="input" bind:value={proxyTargetPreset} disabled={loading}>
          <option value="https://www.google.com">Google</option>
          <option value="https://www.youtube.com">YouTube</option>
          <option value="https://chatgpt.com">ChatGPT</option>
          <option value="https://github.com">GitHub</option>
          <option value="custom">{$t('net.presets')}: Свой URL...</option>
        </select>
      </div>

      {#if proxyTargetPreset === 'custom'}
        <div class="form-group" style="margin-bottom:10px;" transition:slide={{ duration: 180 }}>
          <input
            bind:this={proxyTargetInput}
            class="input"
            bind:value={customProxyURL}
            placeholder="https://..."
            disabled={loading}
          />
        </div>
      {/if}

      {#if showProxySettings}
        <div class="extra-settings mb-2" transition:slide={{ duration: 180 }}>
          <label for="proxy-timeout" class="lbl">{$t('net.timeout_sec')}</label>
          <input
            id="proxy-timeout"
            type="number"
            class="input input-sm"
            bind:value={proxyTimeout}
            min="100"
            max="15000"
            step="100"
          />
        </div>
      {/if}

      <div style="display:flex;gap:8px;margin-top:auto;">
        <button
          class="btn btn-primary"
          style="flex:1;"
          on:click={() => runTool('proxy')}
          disabled={loading || !selectedProxy}
        >
          {loading && activeTool === 'proxy' ? $t('net.running') : $t('net.run')}
        </button>
        <button
          class="btn btn-secondary"
          on:click={() => showProxySettings = !showProxySettings}
          title="Настройки"
        >
          ⋯
        </button>
      </div>
    </div>

    <!-- Port Checker -->
    <div class="nt-card">
      <h3 style="display:flex;align-items:center;gap:8px;">
        <Icon name="network" size={14} />
        {$t('net.tab_port_check')}
      </h3>

      <div class="form-group" style="margin-bottom:10px; display:flex; flex-direction:column; gap:4px;">
        <label for="port-host" class="lbl" style="font-size: 11px; color: var(--fg-dim); text-transform: uppercase; letter-spacing: 0.05em;">
          {$t('net.host_ip')}
        </label>
        <input
          id="port-host"
          bind:this={portHostInput}
          class="input"
          bind:value={portHost}
          placeholder="vpn.server.com"
          disabled={loading}
        />
      </div>

      <div class="form-group" style="margin-bottom:10px; display:flex; flex-direction:column; gap:4px;">
        <label for="port-number" class="lbl" style="font-size: 11px; color: var(--fg-dim); text-transform: uppercase; letter-spacing: 0.05em;">
          {$t('net.port')}
        </label>
        <input
          id="port-number"
          bind:this={portNumberInput}
          type="number"
          class="input"
          bind:value={portNumber}
          placeholder="443"
          disabled={loading}
          min="1"
          max="65535"
        />
      </div>

      <!-- Quick presets chips -->
      <div style="display:flex; flex-wrap:wrap; gap:6px; margin-bottom:10px;">
        {#each [22, 80, 443, 1080, 8080] as p}
          <button
            type="button"
            class="chip"
            style="background:var(--bg-card); border:1px solid var(--border); border-radius:12px; padding:2px 8px; font-size:11px; color:var(--fg-secondary); cursor:pointer; transition:all 0.15s ease;"
            on:click={() => portNumber = p}
            disabled={loading}
          >
            {p === 22 ? '22 (SSH)' : p === 80 ? '80 (HTTP)' : p === 443 ? '443 (HTTPS)' : p === 1080 ? '1080 (Socks)' : `${p}`}
          </button>
        {/each}
      </div>

      {#if showPortSettings}
        <div class="extra-settings mb-2" transition:slide={{ duration: 180 }}>
          <label for="port-timeout" class="lbl">{$t('net.timeout_sec')}</label>
          <input
            id="port-timeout"
            type="number"
            class="input input-sm"
            bind:value={portTimeout}
            min="100"
            max="15000"
            step="100"
          />
        </div>
      {/if}

      <div style="display:flex;gap:8px;margin-top:auto;">
        <button
          class="btn btn-primary"
          style="flex:1;"
          on:click={() => runTool('port')}
          disabled={loading || !portHost || portNumber === null}
        >
          {loading && activeTool === 'port' ? $t('net.running') : $t('net.run')}
        </button>
        <button
          class="btn btn-secondary"
          on:click={() => showPortSettings = !showPortSettings}
          title="Настройки"
        >
          ⋯
        </button>
      </div>
    </div>
  </div>

  {#if result}
    <div class="card card-tight">
      <h2 class="card-title" style="display:flex;justify-content:space-between;align-items:center;">
        <span
          >{$t('net.result')} — {activeTool.toUpperCase()}
          {activeTool === 'http' ? url : host}</span
        >
        <span
          class="badge"
          class:badge-success={result.success}
          class:badge-danger={!result.success}
        >
          {result.success ? $t('net.success') : $t('app.error')}
        </span>
      </h2>

      <div class="term-output" style="border:0;border-radius:0;min-height:auto;">
        {#if result.error}
          <div class="error-text" style="color:var(--danger);">
            {$t('net.error_with_msg', { error: result.error })}
          </div>
        {/if}

        {#if result.output}
          {result.output}
        {/if}

        {#if result.records}
          {#each result.records as record}
            <div>{record}</div>
          {/each}
        {/if}
      </div>
    </div>
  {/if}

  <!-- Test History Section -->
  <div class="card card-tight mb-3" style="padding: 16px 20px; margin-top: 16px;">
    <div style="display:flex; justify-content:space-between; align-items:center; margin-bottom:12px;">
      <h3 style="margin:0; font-size:14px; font-weight:700; color:var(--fg-primary); display:flex; align-items:center; gap:8px;">
        <Icon name="connections" size={14} />
        {$t('net.history')}
      </h3>
      {#if historyList.length > 0}
        <button
          class="btn btn-secondary btn-sm"
          style="padding: 2px 8px; font-size: 11px;"
          on:click={clearHistory}
        >
          {$t('console.clear')}
        </button>
      {/if}
    </div>

    {#if historyList.length === 0}
      <div style="color:var(--fg-dim); font-size:12.5px; text-align:center; padding:12px 0;">
        {$t('net.no_history')}
      </div>
    {:else}
      <div style="display:flex; flex-direction:column; gap:8px;">
        {#each historyList as item}
          <div
            role="button"
            tabindex="0"
            class="history-row"
            style="display:flex; align-items:center; justify-content:space-between; background:var(--bg-card); border:1px solid var(--border); border-radius:var(--radius-sm); padding:10px 14px; cursor:pointer; font-size:13px; transition:all 0.2s ease;"
            on:click={() => selectHistoryItem(item)}
            on:keydown={(e) => (e.key === 'Enter' || e.key === ' ') && selectHistoryItem(item)}
          >
            <div style="display:flex; align-items:center; gap:8px;">
              <span class="badge" class:badge-success={item.type === 'ping' || item.type === 'port' || item.type === 'proxy'} style="font-size:11px; text-transform:uppercase; font-weight:600;">
                {item.type}
              </span>
              <span style="color:var(--fg-primary); font-weight:500;">
                {item.label}
              </span>
            </div>
            <span style="color:var(--fg-dim); font-size:11.5px; font-family:var(--font-family-mono);">
              {new Date(item.timestamp).toLocaleTimeString()}
            </span>
          </div>
        {/each}
      </div>
    {/if}
  </div>
</div>

<style>
  .nt-grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(320px, 1fr));
    gap: 16px;
  }

  .history-row:hover {
    border-color: var(--accent) !important;
    background: rgba(41, 194, 240, 0.03) !important;
  }

  .nt-card {
    background: var(--bg-card);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    padding: 18px;
    display: flex;
    flex-direction: column;
    min-height: 150px;
  }

  .nt-card h3 {
    margin-top: 0;
    margin-bottom: 12px;
    font-size: 14px;
    font-weight: 700;
    color: var(--fg-primary);
  }

  .extra-settings {
    display: flex;
    flex-direction: column;
    gap: 4px;
  }

  .extra-settings .lbl {
    font-size: 11px;
    color: var(--fg-dim);
    text-transform: uppercase;
    letter-spacing: 0.05em;
  }

  .input-sm {
    padding: 6px 10px;
    font-size: 12px;
  }

  .term-output {
    background: #050d16;
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    padding: 14px 18px;
    font-family: var(--font-family-mono);
    font-size: 12.5px;
    line-height: 1.6;
    color: var(--fg-primary);
    min-height: 200px;
    overflow: auto;
    white-space: pre-wrap;
    word-break: break-all;
  }

  @media (max-width: 1024px) {
    .nt-grid {
      grid-template-columns: repeat(2, 1fr);
    }
  }

  @media (max-width: 600px) {
    .nt-grid {
      grid-template-columns: 1fr;
    }
  }
</style>
