<script lang="ts">
  import { t } from '../../i18n';
  import Select from '../Select.svelte';
  import type { DNSServer } from './XrayContext.svelte';

  interface DnsConfig {
    tag: string;
    servers: (string | DNSServer)[];
    queryStrategy: string;
    hosts: Record<string, string>;
  }

  let {
    dnsConfig = $bindable(),
    dnsOverVless = $bindable(false),
    xkeenDns = true,
    dnsRedirectLoading = false,
    onEnableDnsRedirect,
    onchange
  }: {
    dnsConfig: DnsConfig;
    dnsOverVless: boolean;
    xkeenDns?: boolean;
    dnsRedirectLoading?: boolean;
    onEnableDnsRedirect?: () => void;
    onchange?: () => void;
  } = $props();

  let showDnsForm = $state(false);
  let newDns = $state({
    address: '',
    port: 53,
    tag: '',
    domainsRaw: '',
    skipFallback: false
  });

  let showHostForm = $state(false);
  let newHost = $state({
    domain: '',
    ip: ''
  });

  function addDNSServer() {
    if (!newDns.address.trim()) return;
    const serverObj: DNSServer = {
      address: newDns.address.trim(),
      port: Number(newDns.port) || 53
    };
    if (newDns.tag.trim()) {
      serverObj.tag = newDns.tag.trim();
    }
    if (newDns.domainsRaw.trim()) {
      serverObj.domains = newDns.domainsRaw.split(/[\s,]+/).filter(Boolean);
    }
    if (newDns.skipFallback) {
      serverObj.skipFallback = true;
    }

    dnsConfig.servers.push(serverObj);
    newDns = {
      address: '',
      port: 53,
      tag: '',
      domainsRaw: '',
      skipFallback: false
    };
    showDnsForm = false;
    onchange?.();
  }

  function removeDNSServer(idx: number) {
    if (idx >= 0 && idx < dnsConfig.servers.length) {
      dnsConfig.servers.splice(idx, 1);
      onchange?.();
    }
  }

  function addHost() {
    if (!newHost.domain.trim() || !newHost.ip.trim()) return;
    dnsConfig.hosts = {
      ...dnsConfig.hosts,
      [newHost.domain.trim()]: newHost.ip.trim()
    };
    newHost = { domain: '', ip: '' };
    showHostForm = false;
    onchange?.();
  }

  function removeHost(domain: string) {
    const updated = { ...dnsConfig.hosts };
    delete updated[domain];
    dnsConfig.hosts = updated;
    onchange?.();
  }

  function markChanged() {
    onchange?.();
  }
</script>

<div class="sec-body">
  {#if xkeenDns === false && dnsConfig.servers.length > 0}
    <div
      class="alert alert-warning"
      style="margin: 0 0 16px 0; display: flex; flex-direction: column; gap: 8px; align-items: flex-start;"
      role="status"
    >
      <div style="display: flex; gap: 8px; align-items: center;">
        <svg
          width="16"
          height="16"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          stroke-width="2"
          style="flex-shrink: 0;"
          aria-hidden="true"
        >
          <path
            d="M10.29 3.86L1.82 18a2 2 0 0 0 1.71 3h16.94a2 2 0 0 0 1.71-3L13.71 3.86a2 2 0 0 0-3.42 0z"
          />
          <line x1="12" y1="9" x2="12" y2="13" />
          <line x1="12" y1="17" x2="12.01" y2="17" />
        </svg>
        <span>{$t('editor.dns_intercept_warning')}</span>
      </div>
      <button
        type="button"
        class="btn btn-secondary btn-sm"
        style="font-size: 12px; padding: 4px 8px; display: flex; align-items: center; gap: 4px;"
        onclick={onEnableDnsRedirect}
        disabled={dnsRedirectLoading}
      >
        {#if dnsRedirectLoading}
          <span
            class="spinner"
            style="--spinner-size: 12px; --spinner-track: currentColor; --spinner-color: transparent;"
          ></span>
        {/if}
        {$t('editor.dns_intercept_enable')}
      </button>
    </div>
  {/if}

  <div class="form-row">
    <label class="form-label" for="dns-query-strategy">{$t('xray.dns_query_strategy')}</label>
    <Select
      id="dns-query-strategy"
      class="form-select"
      bind:value={dnsConfig.queryStrategy}
      onchange={markChanged}
    >
      <option value="UseIP">UseIP</option>
      <option value="UseIPv4">UseIPv4</option>
      <option value="UseIPv6">UseIPv6</option>
    </Select>
  </div>

  <div
    class="card"
    style="margin-top: 16px; margin-bottom: 16px; padding: 12px; display: flex; flex-direction: column; gap: 4px;"
  >
    <label class="checkbox-container" style="margin: 0;">
      <input type="checkbox" bind:checked={dnsOverVless} onchange={markChanged} />
      <span class="checkmark" style="top: 1px;"></span>
      <span style="font-weight: 600; color: var(--fg-primary);">
        {$t('editor.dns_over_vless')}
      </span>
    </label>
    <div
      style="font-size: 0.75rem; color: var(--fg-secondary); padding-left: 28px; line-height: 1.4;"
    >
      {$t('editor.dns_over_vless_desc')}
    </div>
  </div>

  <div class="section-title">{$t('editor.xray_dns')}</div>

  <div class="dns-servers-list">
    {#each dnsConfig.servers as srv, idx}
      <div class="item-row card" style="margin-bottom: 8px;">
        {#if typeof srv === 'string'}
          <span class="item-name">{srv}</span>
        {:else}
          <div style="flex: 1;">
            <div style="font-weight: 600; color: var(--fg-primary);">
              {srv.address}:{srv.port || 53}
            </div>
            <div style="font-size: 0.75rem; color: var(--fg-secondary);">
              {$t('xray.tag')}: <span class="badge">{srv.tag}</span>
              | {$t('xray.domains')}: {srv.domains?.join(', ') || $t('app.none')}
              {#if srv.skipFallback}
                | <span class="badge">{$t('xray.skip_fallback')}</span>
              {/if}
            </div>
          </div>
        {/if}
        <button
          type="button"
          class="item-del"
          onclick={() => removeDNSServer(idx)}
          title={$t('app.delete')}
        >
          ✕
        </button>
      </div>
    {/each}
  </div>

  {#if showDnsForm}
    <div class="form-card card">
      <div class="form-row">
        <label class="form-label" for="xray-new-dns-address">{$t('xray.server_address')}</label>
        <input
          id="xray-new-dns-address"
          class="form-input"
          bind:value={newDns.address}
          placeholder="8.8.8.8"
        />
      </div>
      <div class="form-row2">
        <div class="form-col">
          <label class="form-label" for="xray-new-dns-port">{$t('xray.port')}</label>
          <input id="xray-new-dns-port" class="form-input" type="number" bind:value={newDns.port} />
        </div>
        <div class="form-col">
          <label class="form-label" for="xray-new-dns-tag">{$t('xray.tag_optional')}</label>
          <input
            id="xray-new-dns-tag"
            class="form-input"
            bind:value={newDns.tag}
            placeholder="dns-in-ytb"
          />
        </div>
      </div>
      {#if newDns.tag.trim()}
        <div class="form-row">
          <label class="form-label" for="xray-new-dns-domains">{$t('xray.redirect_domains')}</label>
          <input
            id="xray-new-dns-domains"
            class="form-input"
            bind:value={newDns.domainsRaw}
            placeholder="geosite:youtube, google.com"
          />
        </div>
        <div class="form-row" style="margin-top: 8px;">
          <label class="checkbox-container">
            <input type="checkbox" bind:checked={newDns.skipFallback} />
            <span class="checkmark"></span>
            {$t('xray.skip_fallback')}
          </label>
        </div>
      {/if}
      <div class="form-actions">
        <button type="button" class="btn btn-secondary" onclick={() => (showDnsForm = false)}>
          {$t('app.cancel')}
        </button>
        <button type="button" class="btn btn-primary" onclick={addDNSServer}>
          {$t('app.create')}
        </button>
      </div>
    </div>
  {:else}
    <button type="button" class="add-btn" onclick={() => (showDnsForm = true)}>
      + {$t('xray.add_dns_server')}
    </button>
  {/if}

  <div class="section-title" style="margin-top: 16px;">Hosts</div>
  <div class="hosts-list">
    {#each Object.entries(dnsConfig.hosts) as [domain, ip]}
      <div class="item-row card" style="margin-bottom: 8px;">
        <div style="flex: 1;">
          <code>{domain}</code> &rarr; <code>{ip}</code>
        </div>
        <button
          type="button"
          class="item-del"
          onclick={() => removeHost(domain)}
          title={$t('app.delete')}
        >
          ✕
        </button>
      </div>
    {/each}
  </div>

  {#if showHostForm}
    <div class="form-card card" style="margin-top: 8px;">
      <div class="form-row2">
        <div class="form-col">
          <label class="form-label" for="xray-new-host-domain">{$t('xray.domain')}</label>
          <input
            id="xray-new-host-domain"
            class="form-input"
            bind:value={newHost.domain}
            placeholder="dns.google"
          />
        </div>
        <div class="form-col">
          <label class="form-label" for="xray-new-host-ip">IP</label>
          <input
            id="xray-new-host-ip"
            class="form-input"
            bind:value={newHost.ip}
            placeholder="8.8.8.8"
          />
        </div>
      </div>
      <div class="form-actions">
        <button type="button" class="btn btn-secondary" onclick={() => (showHostForm = false)}>
          {$t('app.cancel')}
        </button>
        <button type="button" class="btn btn-primary" onclick={addHost}>
          {$t('app.create')}
        </button>
      </div>
    </div>
  {:else}
    <button
      type="button"
      class="add-btn"
      style="margin-top: 8px;"
      onclick={() => (showHostForm = true)}
    >
      + {$t('xray.add_host')}
    </button>
  {/if}
</div>

<style>
  .sec-body {
    display: flex;
    flex-direction: column;
    gap: 12px;
  }

  .section-title {
    font-size: 0.9375rem;
    font-weight: 600;
    color: var(--fg-primary);
  }

  .item-row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 10px 14px;
  }

  .item-name {
    font-weight: 500;
    font-size: 0.8125rem;
    color: var(--fg-primary);
  }

  .item-del {
    background: none;
    border: none;
    cursor: pointer;
    color: var(--fg-secondary);
    font-size: 0.875rem;
    padding: 4px 6px;
    border-radius: var(--radius-xs);
    transition:
      color 0.15s ease,
      background 0.15s ease;
  }

  .item-del:hover {
    color: var(--danger, #ef4444);
    background: var(--bg-surface-active);
  }

  .form-card {
    padding: 16px;
    display: flex;
    flex-direction: column;
    gap: 12px;
  }

  .form-row2 {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: 12px;
  }

  @media (max-width: 600px) {
    .form-row2 {
      grid-template-columns: 1fr;
    }
  }

  .form-col {
    display: flex;
    flex-direction: column;
    gap: 4px;
  }

  .form-row {
    display: flex;
    flex-direction: column;
    gap: 4px;
  }

  .form-label {
    font-size: 0.75rem;
    font-weight: 500;
    color: var(--fg-secondary);
  }

  .form-input {
    background: var(--bg-surface);
    border: 1px solid var(--border-color);
    border-radius: var(--radius-sm);
    padding: 6px 10px;
    font-size: 0.8125rem;
    color: var(--fg-primary);
  }

  .form-input:focus {
    outline: none;
    border-color: var(--color-primary);
  }

  :global(.form-select) {
    background: var(--bg-surface);
    border: 1px solid var(--border-color);
    border-radius: var(--radius-sm);
    padding: 6px 10px;
    font-size: 0.8125rem;
    color: var(--fg-primary);
  }

  .checkbox-container {
    display: flex;
    align-items: center;
    gap: 8px;
    font-size: 0.8125rem;
    color: var(--fg-primary);
    cursor: pointer;
    position: relative;
    user-select: none;
  }

  .checkbox-container input {
    position: absolute;
    opacity: 0;
    cursor: pointer;
    height: 0;
    width: 0;
  }

  .checkmark {
    height: 16px;
    width: 16px;
    background-color: var(--bg-surface);
    border: 1px solid var(--border-color);
    border-radius: var(--radius-xs);
    display: inline-block;
    position: relative;
  }

  .checkbox-container input:checked ~ .checkmark {
    background-color: var(--color-primary);
    border-color: var(--color-primary);
  }

  .checkbox-container input:checked ~ .checkmark:after {
    content: '';
    position: absolute;
    left: 5px;
    top: 2px;
    width: 4px;
    height: 8px;
    border: solid var(--color-on-primary, #ffffff);
    border-width: 0 2px 2px 0;
    transform: rotate(45deg);
  }

  .form-actions {
    display: flex;
    justify-content: flex-end;
    gap: 8px;
    margin-top: 4px;
  }

  .add-btn {
    background: var(--bg-surface);
    border: 1px dashed var(--border-color);
    border-radius: var(--radius);
    padding: 10px 16px;
    color: var(--fg-secondary);
    font-size: 0.8125rem;
    font-weight: 500;
    cursor: pointer;
    text-align: center;
    transition: all 0.15s ease;
  }

  .add-btn:hover {
    border-color: var(--color-primary);
    color: var(--color-primary);
    background: var(--bg-surface-hover);
  }

  code {
    font-family: var(--font-family-mono);
    font-size: 0.75rem;
    color: var(--code-fg, var(--fg-primary));
    background: var(--code-bg, var(--bg-surface-active));
    padding: 2px 4px;
    border-radius: var(--radius-xs);
  }
</style>
