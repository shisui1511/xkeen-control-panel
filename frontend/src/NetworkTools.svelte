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
  }

  onMount(() => {
    fetchIP();
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
        <input class="input" bind:value={host} placeholder="cloudflare.com" disabled={loading} />
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
        <input class="input" bind:value={url} placeholder="https://google.com" disabled={loading} />
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
</div>

<style>
  .nt-grid {
    display: grid;
    grid-template-columns: repeat(4, 1fr);
    gap: 14px;
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
