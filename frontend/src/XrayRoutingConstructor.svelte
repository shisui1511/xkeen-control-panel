<script lang="ts">
  import { onMount } from 'svelte';
  import { currentLang, t } from './i18n';
  import { showToast } from './stores';

  export let onSwitchTab: (tab: string) => void = () => {};
  export let selectedFile: string = '';
  export let onInsertIntoEditor: (content: string) => void = () => {};
  export let embedded: boolean = false;

  interface XrayRoutingRule {
    id: string;
    type: 'field';
    outboundTag: string;
    domain?: string[];
    ip?: string[];
    port?: string;
    network?: 'tcp' | 'udp' | 'tcp,udp';
  }

  // State
  let activeSection: 'routing' | 'inbounds' | 'dns' = 'routing';
  let outboundTags: string[] = ['direct', 'block'];
  let routingRules: XrayRoutingRule[] = [
    {
      id: crypto.randomUUID(),
      type: 'field',
      outboundTag: 'direct',
      ip: ['geoip:private']
    },
    {
      id: crypto.randomUUID(),
      type: 'field',
      outboundTag: 'block',
      domain: ['geosite:category-ads-all']
    }
  ];

  let dnsConfig = {
    domainStrategy: 'IPIfNonMatch',
    servers: ['77.88.8.8', '1.1.1.1', '8.8.8.8', '94.140.14.14']
  };

  let inbounds = [
    {
      tag: 'socks',
      port: 10808,
      listen: '127.0.0.1',
      protocol: 'socks',
      settings: { auth: 'noauth', udp: true }
    },
    {
      tag: 'http',
      port: 10809,
      listen: '127.0.0.1',
      protocol: 'http',
      settings: {}
    }
  ];

  // Forms values
  let newRule = {
    outboundTag: 'direct',
    domainRaw: '',
    ipRaw: '',
    port: '',
    network: 'tcp,udp' as const
  };

  let newServer = '';
  let showRuleForm = false;

  onMount(async () => {
    outboundTags = await loadXrayOutboundTags();
    if (outboundTags.length > 0 && !outboundTags.includes(newRule.outboundTag)) {
      newRule.outboundTag = outboundTags[0];
    }
  });

  async function loadXrayOutboundTags(): Promise<string[]> {
    const tags: string[] = [];
    try {
      const files: Array<{ name: string; path: string }> = await fetch('/api/config/list').then(r => r.json());
      const outboundFiles = files.filter(f => f.name.toLowerCase().includes('outbound'));
      for (const file of outboundFiles) {
        const content = await fetch('/api/config/read', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ path: file.path })
        }).then(r => r.text());
        try {
          const json = JSON.parse(content);
          const fileTags = (json.outbounds ?? [])
            .filter((o: any) => o.tag)
            .map((o: any) => o.tag as string);
          tags.push(...fileTags);
        } catch { /* skip malformed */ }
      }
    } catch { /* fallback below */ }
    return [...new Set([...tags, 'direct', 'block'])];
  }

  function addRule() {
    const domains = newRule.domainRaw.trim() ? newRule.domainRaw.split(/[\s,]+/).filter(Boolean) : undefined;
    const ips = newRule.ipRaw.trim() ? newRule.ipRaw.split(/[\s,]+/).filter(Boolean) : undefined;

    routingRules = [
      ...routingRules,
      {
        id: crypto.randomUUID(),
        type: 'field',
        outboundTag: newRule.outboundTag,
        domain: domains,
        ip: ips,
        port: newRule.port.trim() || undefined,
        network: newRule.network !== 'tcp,udp' ? newRule.network : undefined
      }
    ];

    showRuleForm = false;
    newRule.domainRaw = '';
    newRule.ipRaw = '';
    newRule.port = '';
    newRule.network = 'tcp,udp';
  }

  function removeRule(id: string) {
    routingRules = routingRules.filter(r => r.id !== id);
  }

  function moveRule(id: string, dir: -1 | 1) {
    const idx = routingRules.findIndex(r => r.id === id);
    if (idx < 0) return;
    const next = idx + dir;
    if (next < 0 || next >= routingRules.length) return;
    const arr = [...routingRules];
    [arr[idx], arr[next]] = [arr[next], arr[idx]];
    routingRules = arr;
  }

  function addDNSServer() {
    const s = newServer.trim();
    if (s && !dnsConfig.servers.includes(s)) {
      dnsConfig.servers = [...dnsConfig.servers, s];
    }
    newServer = '';
  }

  function removeDNSServer(s: string) {
    dnsConfig.servers = dnsConfig.servers.filter(srv => srv !== s);
  }

  function buildXrayConfig(rules: XrayRoutingRule[], inboundsList: any[], nameserversList: string[], domainStrategy: string): object {
    return {
      log: { loglevel: 'warning' },
      inbounds: inboundsList.map(i => ({
        tag: i.tag,
        port: Number(i.port),
        listen: i.listen,
        protocol: i.protocol,
        settings: i.settings
      })),
      outbounds: [
        { tag: 'PROXY_TAG', protocol: 'freedom' },
        { tag: 'direct', protocol: 'freedom' },
        { tag: 'block', protocol: 'blackhole' }
      ],
      routing: {
        domainStrategy,
        rules: rules.map(r => {
          const cleaned: any = { type: 'field', outboundTag: r.outboundTag };
          if (r.domain && r.domain.length > 0) cleaned.domain = r.domain;
          if (r.ip && r.ip.length > 0) cleaned.ip = r.ip;
          if (r.port && r.port.trim()) cleaned.port = r.port.trim();
          if (r.network && r.network !== 'tcp,udp') cleaned.network = r.network;
          return cleaned;
        })
      },
      dns: {
        servers: nameserversList
      }
    };
  }

  let json = '';
  $: json = JSON.stringify(buildXrayConfig(routingRules, inbounds, dnsConfig.servers, dnsConfig.domainStrategy), null, 2);

  async function copyJSON() {
    await navigator.clipboard.writeText(json);
    showToast('success', ru ? 'JSON скопирован' : 'JSON copied');
  }

  function openInEditor() {
    if (onInsertIntoEditor) {
      onInsertIntoEditor(json);
    } else {
      onSwitchTab('editor');
    }
  }

  const ru = $currentLang === 'ru';
</script>

<div class="container">
  {#if !embedded}
    <div class="page-head">
      <div>
        <div class="crumbs">
          {ru ? 'Сервисы' : 'Services'} <span class="crumb-sep">/</span>
          {ru ? 'Конструктор Xray' : 'Xray Constructor'}
        </div>
        <h1>{ru ? 'Визуальный конструктор Xray' : 'Xray Visual Constructor'}</h1>
        <p class="sub">
          {ru
            ? 'Сборка routing rules, DNS и inbounds для Xray без ручного редактирования JSON.'
            : 'Build routing rules, DNS and inbounds for Xray without hand-editing JSON.'}
        </p>
      </div>
      <div class="ph-actions">
        <button class="btn btn-secondary" on:click={openInEditor}>
          <svg
            width="13"
            height="13"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            stroke-width="2"
            style="margin-right:5px"
            ><path d="M12 20h9" /><path
              d="M16.5 3.5a2.121 2.121 0 0 1 3 3L7 19l-4 1 1-4L16.5 3.5z"
            /></svg
          >
          {#if selectedFile}
            {ru ? 'Вставить в редактор' : 'Insert into Editor'}
          {:else}
            {ru ? 'Открыть в редакторе' : 'Open in Editor'}
          {/if}
        </button>
        <button class="btn btn-primary" on:click={copyJSON} disabled={!json}>
          <svg
            width="13"
            height="13"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            stroke-width="2"
            style="margin-right:5px"
            ><rect x="9" y="9" width="13" height="13" rx="2" /><path
              d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"
            /></svg
          >
          {ru ? 'Копировать JSON' : 'Copy JSON'}
        </button>
      </div>
    </div>
  {/if}

  <div class="gen-layout">
    <!-- Left Panel: Form / Rules list -->
    <div class="gen-left">
      <!-- Section tabs -->
      <div class="sec-tabs">
        <button
          class="sec-tab"
          class:active={activeSection === 'routing'}
          on:click={() => {
            activeSection = 'routing';
            showRuleForm = false;
          }}
        >
          {ru ? 'Маршрутизация' : 'Routing'}
          {#if routingRules.length > 0}<span class="sec-count">{routingRules.length}</span>{/if}
        </button>
        <button
          class="sec-tab"
          class:active={activeSection === 'inbounds'}
          on:click={() => {
            activeSection = 'inbounds';
            showRuleForm = false;
          }}
        >
          {ru ? 'Входящие (Inbounds)' : 'Inbounds'}
        </button>
        <button
          class="sec-tab"
          class:active={activeSection === 'dns'}
          on:click={() => {
            activeSection = 'dns';
            showRuleForm = false;
          }}
        >
          DNS
        </button>
      </div>

      <!-- ROUTING SECTION -->
      {#if activeSection === 'routing'}
        <div class="sec-body">
          <div class="form-row" style="margin-bottom: var(--spacing-4, 16px);">
            <label class="form-label" for="domain-strategy">{$t('editor.xray_domain_strategy')}</label>
            <select
              id="domain-strategy"
              class="form-select"
              bind:value={dnsConfig.domainStrategy}
            >
              <option value="IPIfNonMatch">IPIfNonMatch</option>
              <option value="IPOnDemand">IPOnDemand</option>
              <option value="AsIs">AsIs</option>
            </select>
          </div>

          <div class="section-title">{$t('editor.xray_routing_rules')}</div>
          
          <div class="routing-rules-list" data-testid="routing-rules-list">
            {#each routingRules as rule, idx (rule.id)}
              <div class="card rule-card">
                <div class="rule-header">
                  <span class="badge badge-tag">{rule.outboundTag}</span>
                  <div class="rule-actions">
                    <button class="rule-move" on:click={() => moveRule(rule.id, -1)} disabled={idx === 0}>▲</button>
                    <button class="rule-move" on:click={() => moveRule(rule.id, 1)} disabled={idx === routingRules.length - 1}>▼</button>
                    <button class="rule-del" on:click={() => removeRule(rule.id)}>✕</button>
                  </div>
                </div>
                
                <div class="rule-details">
                  {#if rule.domain && rule.domain.length > 0}
                    <div class="rule-detail-item">
                      <strong>{ru ? 'Домены' : 'Domains'}:</strong>
                      <span class="rule-chips">
                        {#each rule.domain as d}
                          <span class="chip chip-domain">{d}</span>
                        {/each}
                      </span>
                    </div>
                  {/if}
                  
                  {#if rule.ip && rule.ip.length > 0}
                    <div class="rule-detail-item">
                      <strong>IP:</strong>
                      <span class="rule-chips">
                        {#each rule.ip as ip}
                          <span class="chip chip-ip">{ip}</span>
                        {/each}
                      </span>
                    </div>
                  {/if}
                  
                  {#if rule.port}
                    <div class="rule-detail-item">
                      <strong>{ru ? 'Порты' : 'Ports'}:</strong> <code>{rule.port}</code>
                    </div>
                  {/if}
                  
                  {#if rule.network}
                    <div class="rule-detail-item">
                      <strong>{ru ? 'Сеть' : 'Network'}:</strong> <span class="badge">{rule.network}</span>
                    </div>
                  {/if}
                </div>
              </div>
            {/each}
          </div>

          {#if showRuleForm}
            <div class="form-card">
              <div class="form-row">
                <label class="form-label" for="rule-outbound">{$t('editor.xray_outbound_tag')}</label>
                <select
                  id="rule-outbound"
                  class="form-select rule-outbound-select"
                  data-testid="rule-outbound-select"
                  bind:value={newRule.outboundTag}
                >
                  {#each outboundTags as tag}
                    <option value={tag}>{tag}</option>
                  {/each}
                </select>
              </div>

              <div class="form-row">
                <label class="form-label" for="rule-domains">{$t('editor.xray_domain_list')} ({ru ? 'через запятую' : 'comma separated'})</label>
                <input
                  id="rule-domains"
                  class="form-input"
                  data-testid="rule-domain-input"
                  bind:value={newRule.domainRaw}
                  placeholder="geosite:youtube, google.com"
                />
              </div>

              <div class="form-row">
                <label class="form-label" for="rule-ips">{$t('editor.xray_ip_list')} ({ru ? 'через запятую' : 'comma separated'})</label>
                <input
                  id="rule-ips"
                  class="form-input"
                  bind:value={newRule.ipRaw}
                  placeholder="geoip:private, 1.1.1.1"
                />
              </div>

              <div class="form-row2">
                <div class="form-col">
                  <label class="form-label" for="rule-ports">{$t('editor.xray_port_range')}</label>
                  <input
                    id="rule-ports"
                    class="form-input"
                    bind:value={newRule.port}
                    placeholder="80,443,1000-2000"
                  />
                </div>
                <div class="form-col">
                  <label class="form-label" for="rule-network">{$t('editor.xray_network')}</label>
                  <select
                    id="rule-network"
                    class="form-select"
                    bind:value={newRule.network}
                  >
                    <option value="tcp,udp">tcp+udp</option>
                    <option value="tcp">tcp</option>
                    <option value="udp">udp</option>
                  </select>
                </div>
              </div>

              <div class="form-actions">
                <button class="btn btn-secondary" on:click={() => (showRuleForm = false)}>
                  {ru ? 'Отмена' : 'Cancel'}
                </button>
                <button class="btn btn-primary" on:click={addRule}>
                  {ru ? 'Добавить' : 'Add'}
                </button>
              </div>
            </div>
          {:else}
            <button
              class="add-btn"
              data-testid="add-routing-rule"
              on:click={() => (showRuleForm = true)}
            >
              + {$t('editor.xray_routing_add_rule')}
            </button>
          {/if}
        </div>
      {/if}

      <!-- INBOUNDS SECTION -->
      {#if activeSection === 'inbounds'}
        <div class="sec-body">
          <div class="section-title">{$t('editor.xray_inbounds')}</div>
          {#each inbounds as inbound}
            <div class="card inbound-card">
              <div class="inbound-title">
                <span class="badge type-{inbound.protocol}">{inbound.protocol}</span>
                <strong>{inbound.tag}</strong>
              </div>
              <div class="form-row2" style="margin-top:var(--spacing-2, 8px)">
                <div class="form-col">
                  <label class="form-label">{ru ? 'Порт входящего' : 'Inbound port'}</label>
                  <input
                    class="form-input"
                    type="number"
                    bind:value={inbound.port}
                    min="1"
                    max="65535"
                  />
                </div>
                <div class="form-col">
                  <label class="form-label">{ru ? 'Адрес прослушивания' : 'Listen address'}</label>
                  <input
                    class="form-input"
                    bind:value={inbound.listen}
                  />
                </div>
              </div>
            </div>
          {/each}
        </div>
      {/if}

      <!-- DNS SECTION -->
      {#if activeSection === 'dns'}
        <div class="sec-body">
          <div class="section-title">{$t('editor.xray_dns')}</div>
          
          <div class="dns-servers-list">
            {#each dnsConfig.servers as srv}
              <div class="item-row">
                <span class="item-name">{srv}</span>
                <button class="item-del" on:click={() => removeDNSServer(srv)}>✕</button>
              </div>
            {/each}
          </div>

          <div class="form-card" style="margin-top:var(--spacing-4, 16px)">
            <div class="form-row">
              <label class="form-label" for="new-dns-server">{ru ? 'Добавить DNS Сервер' : 'Add DNS Server'}</label>
              <div class="input-with-btn">
                <input
                  id="new-dns-server"
                  class="form-input"
                  bind:value={newServer}
                  placeholder="8.8.8.8"
                />
                <button class="btn btn-primary" on:click={addDNSServer}>{ru ? 'Добавить' : 'Add'}</button>
              </div>
            </div>
          </div>
        </div>
      {/if}
    </div>

    <!-- Right Panel: Preview -->
    <div class="gen-right">
      <div class="preview-header">
        <span class="preview-title">JSON {ru ? 'превью' : 'preview'}</span>
      </div>
      <pre class="constructor-preview-pane" data-testid="xray-json-preview">{json}</pre>
    </div>
  </div>
</div>

<style>
  .container {
    display: flex;
    flex-direction: column;
    height: 100%;
  }

  .crumbs {
    font-size: var(--font-size-xs, 0.75rem);
    color: var(--fg-secondary);
    margin-bottom: 4px;
  }
  .crumb-sep {
    margin: 0 4px;
  }
  h1 {
    font-size: 1.5rem;
    font-weight: 600;
    margin: 0 0 4px 0;
    color: var(--fg);
  }
  .sub {
    color: var(--fg-secondary);
    font-size: var(--font-size-sm, 0.8125rem);
    margin: 0 0 20px 0;
  }

  .page-head {
    display: flex;
    justify-content: space-between;
    align-items: flex-start;
    margin-bottom: var(--spacing-4, 16px);
  }

  .ph-actions {
    display: flex;
    gap: var(--spacing-2, 8px);
  }

  .gen-layout {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: var(--spacing-4, 16px);
    align-items: start;
  }

  @media (max-width: 1024px) {
    .gen-layout {
      grid-template-columns: 1fr;
    }
  }

  .sec-tabs {
    display: flex;
    gap: var(--spacing-2, 8px);
    border-bottom: 1px solid var(--border);
    margin-bottom: var(--spacing-4, 16px);
  }

  .sec-tab {
    padding: 8px 12px;
    background: transparent;
    border: none;
    border-bottom: 2px solid transparent;
    color: var(--fg-secondary);
    font-size: var(--font-size-sm, 0.8125rem);
    cursor: pointer;
    display: flex;
    align-items: center;
    gap: 6px;
    margin-bottom: -1px;
    min-height: 36px;
  }

  .sec-tab.active {
    color: var(--accent);
    border-bottom-color: var(--accent);
    font-weight: 500;
  }

  .sec-count {
    background: var(--bg-surface-hover, rgba(255, 255, 255, 0.1));
    color: var(--fg);
    font-size: 0.6875rem;
    padding: 1px 5px;
    border-radius: 10px;
    font-weight: 600;
  }

  .sec-body {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-4, 16px);
  }

  .section-title {
    font-size: 0.875rem;
    font-weight: 600;
    color: var(--fg);
    margin-bottom: var(--spacing-2, 8px);
  }

  /* Rules list */
  .routing-rules-list {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-2, 8px);
    max-height: 400px;
    overflow-y: auto;
    scrollbar-width: thin;
  }

  .rule-card {
    padding: var(--spacing-3, 12px);
    background: var(--bg-surface);
    border: 1px solid var(--border);
    border-radius: var(--radius-md, 6px);
  }

  .rule-header {
    display: flex;
    justify-content: space-between;
    align-items: center;
    margin-bottom: 8px;
  }

  .badge-tag {
    background: var(--bg-surface-hover);
    color: var(--fg);
    font-weight: 500;
    padding: 2px 6px;
    border-radius: 4px;
    font-size: 0.75rem;
  }

  .rule-actions {
    display: flex;
    gap: 4px;
  }

  .rule-move, .rule-del {
    background: transparent;
    border: none;
    color: var(--fg-secondary);
    width: 20px;
    height: 20px;
    display: flex;
    align-items: center;
    justify-content: center;
    font-size: 0.6875rem;
    cursor: pointer;
    border-radius: 4px;
  }

  .rule-move:hover, .rule-del:hover {
    background: var(--bg-surface-hover);
    color: var(--fg);
  }

  .rule-move:disabled {
    opacity: 0.3;
    cursor: not-allowed;
  }

  .rule-details {
    display: flex;
    flex-direction: column;
    gap: 6px;
    font-size: var(--font-size-sm, 0.8125rem);
  }

  .rule-detail-item {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 6px;
  }

  .rule-chips {
    display: flex;
    flex-wrap: wrap;
    gap: 4px;
  }

  .chip {
    padding: 1px 6px;
    border-radius: 4px;
    font-size: 0.6875rem;
    font-weight: 500;
  }

  .chip-domain {
    background: rgba(13, 110, 253, 0.15);
    color: #0d6efd;
  }

  .chip-ip {
    background: rgba(25, 135, 84, 0.15);
    color: #198754;
  }

  /* Form controls */
  .form-card {
    background: var(--bg-surface);
    border: 1px solid var(--border);
    border-radius: var(--radius-md, 6px);
    padding: var(--spacing-4, 16px);
    display: flex;
    flex-direction: column;
    gap: var(--spacing-3, 12px);
  }

  .form-row {
    display: flex;
    flex-direction: column;
    gap: 6px;
  }

  .form-row2 {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: 12px;
  }

  .form-col {
    display: flex;
    flex-direction: column;
    gap: 6px;
  }

  .form-label {
    font-size: var(--font-size-sm, 0.8125rem);
    color: var(--fg-secondary);
    font-weight: 500;
  }

  .form-input, .form-select {
    padding: 8px 12px;
    background: var(--bg-surface);
    border: 1px solid var(--border);
    border-radius: var(--radius-md, 6px);
    color: var(--fg);
    font-size: var(--font-size-sm, 0.8125rem);
    font-family: inherit;
    outline: none;
    transition: border-color var(--transition-fast);
  }

  .form-input:focus, .form-select:focus {
    border-color: var(--accent);
  }

  .input-with-btn {
    display: flex;
    gap: 8px;
  }
  .input-with-btn .form-input {
    flex: 1;
  }

  .form-actions {
    display: flex;
    justify-content: flex-end;
    gap: 8px;
    margin-top: 8px;
  }

  .btn {
    padding: 8px 16px;
    border-radius: var(--radius-md, 6px);
    font-size: var(--font-size-sm, 0.8125rem);
    font-weight: 500;
    cursor: pointer;
    border: none;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    transition: background-color var(--transition-fast);
  }

  .btn-primary {
    background: var(--accent);
    color: #fff;
  }
  .btn-primary:hover {
    background: var(--accent-hover, #0056b3);
  }

  .btn-secondary {
    background: var(--bg-surface-hover);
    color: var(--fg);
    border: 1px solid var(--border);
  }
  .btn-secondary:hover {
    background: var(--bg-surface-active);
  }

  .btn-secondary:disabled {
    opacity: 0.5;
    cursor: not-allowed;
  }

  .add-btn {
    width: 100%;
    padding: var(--spacing-3, 12px);
    background: transparent;
    border: 1px dashed var(--border);
    color: var(--fg-secondary);
    border-radius: var(--radius-md, 6px);
    cursor: pointer;
    transition: border-color var(--transition-fast), color var(--transition-fast);
    font-size: var(--font-size-sm, 0.8125rem);
  }

  .add-btn:hover {
    border-color: var(--accent);
    color: var(--accent);
  }

  /* Inbounds */
  .inbound-card {
    padding: var(--spacing-4, 16px);
    background: var(--bg-surface);
    border: 1px solid var(--border);
    border-radius: var(--radius-md, 6px);
  }

  .inbound-title {
    display: flex;
    align-items: center;
    gap: 8px;
    font-size: 0.875rem;
  }

  .type-socks {
    background: rgba(13, 110, 253, 0.15);
    color: #0d6efd;
  }

  .type-http {
    background: rgba(111, 66, 193, 0.15);
    color: #6f42c1;
  }

  /* DNS List */
  .dns-servers-list {
    display: flex;
    flex-direction: column;
    gap: 4px;
  }

  .item-row {
    display: flex;
    justify-content: space-between;
    align-items: center;
    padding: 8px 12px;
    background: var(--bg-surface);
    border: 1px solid var(--border);
    border-radius: var(--radius-md, 6px);
  }

  .item-name {
    font-size: var(--font-size-sm, 0.8125rem);
    color: var(--fg);
  }

  .item-del {
    background: transparent;
    border: none;
    color: var(--fg-secondary);
    cursor: pointer;
    padding: 0 4px;
  }
  .item-del:hover {
    color: var(--fg);
  }

  /* Preview Pane */
  .gen-right {
    display: flex;
    flex-direction: column;
    height: 100%;
    min-height: 450px;
  }

  .preview-header {
    padding: 8px 12px;
    background: var(--bg-surface);
    border: 1px solid var(--border);
    border-bottom: none;
    border-radius: var(--radius-md, 6px) var(--radius-md, 6px) 0 0;
  }

  .preview-title {
    font-size: var(--font-size-xs, 0.75rem);
    color: var(--fg-secondary);
    font-weight: 600;
    text-transform: uppercase;
  }

  .constructor-preview-pane {
    flex: 1;
    margin: 0;
    padding: var(--spacing-4, 16px);
    background: #1e1e1e;
    color: #d4d4d4;
    border: 1px solid var(--border);
    border-radius: 0 0 var(--radius-md, 6px) var(--radius-md, 6px);
    font-family: var(--font-mono, monospace);
    font-size: var(--font-size-xs, 0.75rem);
    line-height: 1.5;
    overflow: auto;
    scrollbar-width: thin;
    max-height: 500px;
  }
</style>
