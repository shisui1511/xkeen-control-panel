<script lang="ts">
  import { currentLang } from './i18n';
  import { showToast } from './stores';

  export let onSwitchTab: (tab: string) => void = () => {};

  type ProxyType = 'vless' | 'hysteria2' | 'tuic' | 'ss' | 'vmess';
  type GroupType = 'select' | 'url-test' | 'fallback' | 'load-balance';
  type RuleType = 'DOMAIN-SUFFIX' | 'DOMAIN-KEYWORD' | 'DOMAIN' | 'GEOIP' | 'GEOSITE' | 'IP-CIDR' | 'PROCESS-NAME' | 'MATCH';

  interface Proxy {
    id: string;
    name: string;
    type: ProxyType;
    server: string;
    port: number;
    // vless/vmess
    uuid?: string;
    flow?: string;
    // reality
    publicKey?: string;
    shortId?: string;
    servername?: string;
    // hy2
    password?: string;
    sni?: string;
    // tuic
    congestion?: string;
    // ss
    cipher?: string;
    // vmess ws
    network?: string;
    wsPath?: string;
    tls?: boolean;
    fingerprint?: string;
  }

  interface ProxyGroup {
    id: string;
    name: string;
    type: GroupType;
    proxies: string[];
    url?: string;
    interval?: number;
  }

  interface Rule {
    id: string;
    type: RuleType;
    value: string;
    outbound: string;
  }

  interface DNSConfig {
    enabled: boolean;
    nameservers: string[];
    fallback: string[];
    enhancedMode: 'fake-ip' | 'redir-host';
    fakeIPRange: string;
  }

  interface TUNConfig {
    enabled: boolean;
    stack: 'system' | 'gvisor' | 'mixed';
    autoRoute: boolean;
    autoDetectInterface: boolean;
    dnsHijack: string[];
  }

  // State
  let activeSection: 'proxies' | 'groups' | 'rules' | 'dns' | 'tun' = 'proxies';
  let proxies: Proxy[] = [];
  let groups: ProxyGroup[] = [];
  let rules: Rule[] = [];
  let dns: DNSConfig = {
    enabled: false,
    nameservers: ['https://doh.pub/dns-query', '223.5.5.5'],
    fallback: ['https://8.8.8.8/dns-query', '1.1.1.1'],
    enhancedMode: 'fake-ip',
    fakeIPRange: '198.18.0.1/16'
  };
  let tun: TUNConfig = {
    enabled: false,
    stack: 'mixed',
    autoRoute: true,
    autoDetectInterface: true,
    dnsHijack: ['any:53']
  };

  // Form visibility
  let showProxyForm = false;
  let showGroupForm = false;
  let showRuleForm = false;

  // New proxy form
  let np: Omit<Proxy, 'id'> = newProxyDefaults('vless');
  function newProxyDefaults(type: ProxyType): Omit<Proxy, 'id'> {
    return {
      name: '', type, server: '', port: 443,
      uuid: crypto.randomUUID(), flow: 'xtls-rprx-vision',
      publicKey: '', shortId: '', servername: 'www.apple.com',
      password: '', sni: '', congestion: 'bbr',
      cipher: 'aes-256-gcm', network: 'ws', wsPath: '/', tls: true,
      fingerprint: 'chrome'
    };
  }
  $: if (np.type) np = { ...newProxyDefaults(np.type), name: np.name, server: np.server, port: np.port };

  // New group form
  let ng: Omit<ProxyGroup, 'id'> = { name: '', type: 'select', proxies: [], url: 'https://www.gstatic.com/generate_204', interval: 300 };
  let ngProxyInput = '';

  // New rule form
  let nr: Omit<Rule, 'id'> = { type: 'DOMAIN-SUFFIX', value: '', outbound: 'DIRECT' };

  function addProxy() {
    if (!np.name.trim() || !np.server.trim()) return;
    proxies = [...proxies, { ...np, id: crypto.randomUUID() }];
    showProxyForm = false;
    np = newProxyDefaults('vless');
  }

  function removeProxy(id: string) {
    proxies = proxies.filter(p => p.id !== id);
  }

  function addGroup() {
    if (!ng.name.trim()) return;
    groups = [...groups, { ...ng, id: crypto.randomUUID(), proxies: [...ng.proxies] }];
    showGroupForm = false;
    ng = { name: '', type: 'select', proxies: [], url: 'https://www.gstatic.com/generate_204', interval: 300 };
    ngProxyInput = '';
  }

  function removeGroup(id: string) {
    groups = groups.filter(g => g.id !== id);
  }

  function addRule() {
    if (nr.type !== 'MATCH' && !nr.value.trim()) return;
    rules = [...rules, { ...nr, id: crypto.randomUUID() }];
    showRuleForm = false;
    nr = { type: 'DOMAIN-SUFFIX', value: '', outbound: 'DIRECT' };
  }

  function removeRule(id: string) {
    rules = rules.filter(r => r.id !== id);
  }

  function moveRule(id: string, dir: -1 | 1) {
    const idx = rules.findIndex(r => r.id === id);
    if (idx < 0) return;
    const next = idx + dir;
    if (next < 0 || next >= rules.length) return;
    const arr = [...rules];
    [arr[idx], arr[next]] = [arr[next], arr[idx]];
    rules = arr;
  }

  function addGroupProxy() {
    const v = ngProxyInput.trim();
    if (v && !ng.proxies.includes(v)) {
      ng = { ...ng, proxies: [...ng.proxies, v] };
    }
    ngProxyInput = '';
  }

  // ── YAML generation ─────────────────────────────────────────────────────

  function q(v: string | number | boolean) {
    return typeof v === 'string' && (v.includes(':') || v.includes('#') || v === '')
      ? `"${v}"`
      : String(v);
  }

  function generateYAML(): string {
    const lines: string[] = [];

    // Proxies
    if (proxies.length > 0) {
      lines.push('proxies:');
      for (const p of proxies) {
        lines.push(`  - name: ${q(p.name)}`);
        lines.push(`    type: ${p.type}`);
        lines.push(`    server: ${q(p.server)}`);
        lines.push(`    port: ${p.port}`);

        if (p.type === 'vless') {
          lines.push(`    uuid: ${p.uuid}`);
          if (p.flow) lines.push(`    flow: ${p.flow}`);
          lines.push(`    tls: true`);
          lines.push(`    reality-opts:`);
          lines.push(`      public-key: ${q(p.publicKey || '')}`);
          lines.push(`      short-id: ${q(p.shortId || '')}`);
          lines.push(`    client-fingerprint: ${p.fingerprint || 'chrome'}`);
          if (p.servername) lines.push(`    servername: ${q(p.servername)}`);
        } else if (p.type === 'hysteria2') {
          lines.push(`    password: ${q(p.password || '')}`);
          if (p.sni) lines.push(`    sni: ${q(p.sni)}`);
        } else if (p.type === 'tuic') {
          lines.push(`    uuid: ${p.uuid}`);
          lines.push(`    password: ${q(p.password || '')}`);
          lines.push(`    congestion-controller: ${p.congestion || 'bbr'}`);
          if (p.sni) lines.push(`    sni: ${q(p.sni)}`);
        } else if (p.type === 'ss') {
          lines.push(`    cipher: ${p.cipher || 'aes-256-gcm'}`);
          lines.push(`    password: ${q(p.password || '')}`);
        } else if (p.type === 'vmess') {
          lines.push(`    uuid: ${p.uuid}`);
          lines.push(`    alterId: 0`);
          lines.push(`    cipher: auto`);
          lines.push(`    tls: ${p.tls}`);
          lines.push(`    network: ${p.network || 'ws'}`);
          if (p.network === 'ws') {
            lines.push(`    ws-opts:`);
            lines.push(`      path: ${q(p.wsPath || '/')}`);
          }
          if (p.tls && p.sni) lines.push(`    servername: ${q(p.sni)}`);
        }
      }
      lines.push('');
    }

    // Proxy groups
    if (groups.length > 0) {
      lines.push('proxy-groups:');
      for (const g of groups) {
        lines.push(`  - name: ${q(g.name)}`);
        lines.push(`    type: ${g.type}`);
        if (g.proxies.length > 0) {
          lines.push(`    proxies:`);
          for (const p of g.proxies) lines.push(`      - ${q(p)}`);
        }
        if (g.type !== 'select') {
          lines.push(`    url: ${g.url || 'https://www.gstatic.com/generate_204'}`);
          lines.push(`    interval: ${g.interval || 300}`);
        }
      }
      lines.push('');
    }

    // Rules
    if (rules.length > 0) {
      lines.push('rules:');
      for (const r of rules) {
        if (r.type === 'MATCH') {
          lines.push(`  - MATCH,${r.outbound}`);
        } else {
          lines.push(`  - ${r.type},${r.value},${r.outbound}`);
        }
      }
      lines.push('');
    }

    // DNS
    if (dns.enabled) {
      lines.push('dns:');
      lines.push(`  enable: true`);
      lines.push(`  enhanced-mode: ${dns.enhancedMode}`);
      if (dns.enhancedMode === 'fake-ip') lines.push(`  fake-ip-range: ${dns.fakeIPRange}`);
      lines.push(`  nameserver:`);
      for (const ns of dns.nameservers) lines.push(`    - ${q(ns)}`);
      if (dns.fallback.length > 0) {
        lines.push(`  fallback:`);
        for (const fb of dns.fallback) lines.push(`    - ${q(fb)}`);
      }
      lines.push('');
    }

    // TUN
    if (tun.enabled) {
      lines.push('tun:');
      lines.push(`  enable: true`);
      lines.push(`  stack: ${tun.stack}`);
      lines.push(`  auto-route: ${tun.autoRoute}`);
      lines.push(`  auto-detect-interface: ${tun.autoDetectInterface}`);
      if (tun.dnsHijack.length > 0) {
        lines.push(`  dns-hijack:`);
        for (const d of tun.dnsHijack) lines.push(`    - ${q(d)}`);
      }
      lines.push('');
    }

    return lines.join('\n').trimEnd();
  }

  $: yaml = generateYAML();

  async function copyYAML() {
    await navigator.clipboard.writeText(yaml);
    showToast('success', $currentLang === 'ru' ? 'YAML скопирован' : 'YAML copied');
  }

  function openInEditor() {
    onSwitchTab('editor');
  }

  const ru = $currentLang === 'ru';

  const PROXY_TYPES: ProxyType[] = ['vless', 'hysteria2', 'tuic', 'ss', 'vmess'];
  const GROUP_TYPES: GroupType[] = ['select', 'url-test', 'fallback', 'load-balance'];
  const RULE_TYPES: RuleType[] = ['DOMAIN-SUFFIX', 'DOMAIN-KEYWORD', 'DOMAIN', 'GEOIP', 'GEOSITE', 'IP-CIDR', 'PROCESS-NAME', 'MATCH'];
  const CIPHERS = ['aes-256-gcm', 'aes-128-gcm', 'chacha20-poly1305', '2022-blake3-aes-256-gcm'];

  $: allProxyNames = [
    'DIRECT', 'REJECT',
    ...proxies.map(p => p.name),
    ...groups.map(g => g.name)
  ];
</script>

<div class="container">
  <div class="page-head">
    <div>
      <div class="crumbs">
        {ru ? 'Сервисы' : 'Services'} <span class="crumb-sep">/</span>
        {ru ? 'Генератор Mihomo' : 'Mihomo Generator'}
      </div>
      <h1>{ru ? 'Визуальный генератор Mihomo' : 'Mihomo Visual Generator'}</h1>
      <p class="sub">{ru ? 'Сборка proxy, proxy-group, rules, DNS и TUN без ручного редактирования YAML.' : 'Build proxy, proxy-group, rules, DNS and TUN without hand-editing YAML.'}</p>
    </div>
    <div class="ph-actions">
      <button class="btn btn-secondary" on:click={openInEditor}>
        <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" style="margin-right:5px"><path d="M12 20h9"/><path d="M16.5 3.5a2.121 2.121 0 0 1 3 3L7 19l-4 1 1-4L16.5 3.5z"/></svg>
        {ru ? 'Открыть в редакторе' : 'Open in Editor'}
      </button>
      <button class="btn btn-primary" on:click={copyYAML} disabled={!yaml}>
        <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" style="margin-right:5px"><rect x="9" y="9" width="13" height="13" rx="2"/><path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"/></svg>
        {ru ? 'Копировать YAML' : 'Copy YAML'}
      </button>
    </div>
  </div>

  <div class="gen-layout">
    <!-- Left: sections -->
    <div class="gen-left">
      <!-- Section tabs -->
      <div class="sec-tabs">
        {#each [['proxies', ru ? 'Прокси' : 'Proxies'], ['groups', ru ? 'Группы' : 'Groups'], ['rules', ru ? 'Правила' : 'Rules'], ['dns', 'DNS'], ['tun', 'TUN']] as [id, label]}
          <button
            class="sec-tab"
            class:active={activeSection === id}
            on:click={() => { activeSection = id; showProxyForm = false; showGroupForm = false; showRuleForm = false; }}
          >
            {label}
            {#if id === 'proxies' && proxies.length > 0}<span class="sec-count">{proxies.length}</span>{/if}
            {#if id === 'groups' && groups.length > 0}<span class="sec-count">{groups.length}</span>{/if}
            {#if id === 'rules' && rules.length > 0}<span class="sec-count">{rules.length}</span>{/if}
            {#if id === 'dns' && dns.enabled}<span class="sec-dot"></span>{/if}
            {#if id === 'tun' && tun.enabled}<span class="sec-dot"></span>{/if}
          </button>
        {/each}
      </div>

      <!-- PROXIES -->
      {#if activeSection === 'proxies'}
        <div class="sec-body">
          {#each proxies as p (p.id)}
            <div class="item-row">
              <span class="item-badge type-{p.type}">{p.type}</span>
              <span class="item-name">{p.name}</span>
              <span class="item-meta">{p.server}:{p.port}</span>
              <button class="item-del" on:click={() => removeProxy(p.id)} title={ru ? 'Удалить' : 'Remove'}>✕</button>
            </div>
          {/each}

          {#if showProxyForm}
            <div class="form-card">
              <div class="form-row">
                <label class="form-label">{ru ? 'Тип' : 'Type'}</label>
                <select class="form-select" bind:value={np.type}>
                  {#each PROXY_TYPES as t}<option value={t}>{t}</option>{/each}
                </select>
              </div>
              <div class="form-row">
                <label class="form-label">{ru ? 'Имя' : 'Name'}</label>
                <input class="form-input" bind:value={np.name} placeholder="my-proxy" />
              </div>
              <div class="form-row2">
                <div class="form-col">
                  <label class="form-label">{ru ? 'Сервер' : 'Server'}</label>
                  <input class="form-input" bind:value={np.server} placeholder="example.com" />
                </div>
                <div class="form-col form-col-sm">
                  <label class="form-label">{ru ? 'Порт' : 'Port'}</label>
                  <input class="form-input" type="number" bind:value={np.port} min="1" max="65535" />
                </div>
              </div>

              {#if np.type === 'vless'}
                <div class="form-row">
                  <label class="form-label">UUID</label>
                  <div class="input-with-btn">
                    <input class="form-input" bind:value={np.uuid} placeholder="uuid" />
                    <button class="btn-gen" on:click={() => np.uuid = crypto.randomUUID()} title="Generate">⟳</button>
                  </div>
                </div>
                <div class="form-row">
                  <label class="form-label">Reality Public Key</label>
                  <input class="form-input" bind:value={np.publicKey} placeholder="public-key" />
                </div>
                <div class="form-row2">
                  <div class="form-col">
                    <label class="form-label">Short ID</label>
                    <input class="form-input" bind:value={np.shortId} placeholder="short-id" />
                  </div>
                  <div class="form-col">
                    <label class="form-label">SNI</label>
                    <input class="form-input" bind:value={np.servername} placeholder="www.apple.com" />
                  </div>
                </div>
              {:else if np.type === 'hysteria2'}
                <div class="form-row">
                  <label class="form-label">{ru ? 'Пароль' : 'Password'}</label>
                  <input class="form-input" bind:value={np.password} placeholder="password" />
                </div>
                <div class="form-row">
                  <label class="form-label">SNI</label>
                  <input class="form-input" bind:value={np.sni} placeholder="example.com" />
                </div>
              {:else if np.type === 'tuic'}
                <div class="form-row">
                  <label class="form-label">UUID</label>
                  <div class="input-with-btn">
                    <input class="form-input" bind:value={np.uuid} placeholder="uuid" />
                    <button class="btn-gen" on:click={() => np.uuid = crypto.randomUUID()} title="Generate">⟳</button>
                  </div>
                </div>
                <div class="form-row">
                  <label class="form-label">{ru ? 'Пароль' : 'Password'}</label>
                  <input class="form-input" bind:value={np.password} placeholder="password" />
                </div>
                <div class="form-row">
                  <label class="form-label">SNI</label>
                  <input class="form-input" bind:value={np.sni} placeholder="example.com" />
                </div>
              {:else if np.type === 'ss'}
                <div class="form-row">
                  <label class="form-label">Cipher</label>
                  <select class="form-select" bind:value={np.cipher}>
                    {#each CIPHERS as c}<option value={c}>{c}</option>{/each}
                  </select>
                </div>
                <div class="form-row">
                  <label class="form-label">{ru ? 'Пароль' : 'Password'}</label>
                  <input class="form-input" bind:value={np.password} placeholder="password" />
                </div>
              {:else if np.type === 'vmess'}
                <div class="form-row">
                  <label class="form-label">UUID</label>
                  <div class="input-with-btn">
                    <input class="form-input" bind:value={np.uuid} placeholder="uuid" />
                    <button class="btn-gen" on:click={() => np.uuid = crypto.randomUUID()} title="Generate">⟳</button>
                  </div>
                </div>
                <div class="form-row2">
                  <div class="form-col">
                    <label class="form-label">Network</label>
                    <select class="form-select" bind:value={np.network}>
                      <option value="ws">WebSocket</option>
                      <option value="tcp">TCP</option>
                      <option value="grpc">gRPC</option>
                    </select>
                  </div>
                  <div class="form-col">
                    <label class="form-label">TLS</label>
                    <input type="checkbox" bind:checked={np.tls} style="margin-top:8px" />
                  </div>
                </div>
                {#if np.network === 'ws'}
                  <div class="form-row">
                    <label class="form-label">WS Path</label>
                    <input class="form-input" bind:value={np.wsPath} placeholder="/" />
                  </div>
                {/if}
                {#if np.tls}
                  <div class="form-row">
                    <label class="form-label">SNI</label>
                    <input class="form-input" bind:value={np.sni} placeholder="example.com" />
                  </div>
                {/if}
              {/if}

              <div class="form-actions">
                <button class="btn btn-secondary" on:click={() => showProxyForm = false}>{ru ? 'Отмена' : 'Cancel'}</button>
                <button class="btn btn-primary" on:click={addProxy}>{ru ? 'Добавить' : 'Add'}</button>
              </div>
            </div>
          {:else}
            <button class="add-btn" on:click={() => showProxyForm = true}>
              + {ru ? 'Добавить прокси' : 'Add proxy'}
            </button>
          {/if}
        </div>
      {/if}

      <!-- GROUPS -->
      {#if activeSection === 'groups'}
        <div class="sec-body">
          {#each groups as g (g.id)}
            <div class="item-row">
              <span class="item-badge type-group">{g.type}</span>
              <span class="item-name">{g.name}</span>
              <span class="item-meta">{g.proxies.length} {ru ? 'прокси' : 'proxies'}</span>
              <button class="item-del" on:click={() => removeGroup(g.id)}>✕</button>
            </div>
          {/each}

          {#if showGroupForm}
            <div class="form-card">
              <div class="form-row">
                <label class="form-label">{ru ? 'Тип' : 'Type'}</label>
                <select class="form-select" bind:value={ng.type}>
                  {#each GROUP_TYPES as t}<option value={t}>{t}</option>{/each}
                </select>
              </div>
              <div class="form-row">
                <label class="form-label">{ru ? 'Имя группы' : 'Group name'}</label>
                <input class="form-input" bind:value={ng.name} placeholder="Выбор прокси" />
              </div>
              <div class="form-row">
                <label class="form-label">{ru ? 'Прокси' : 'Proxies'}</label>
                <div class="tag-input-wrap">
                  {#each ng.proxies as p}
                    <span class="tag-pill">
                      {p}
                      <button class="tag-rm" on:click={() => ng = { ...ng, proxies: ng.proxies.filter(x => x !== p) }}>✕</button>
                    </span>
                  {/each}
                  <select class="form-select-inline" bind:value={ngProxyInput} on:change={addGroupProxy}>
                    <option value="">+ {ru ? 'добавить' : 'add'}...</option>
                    {#each allProxyNames as n}<option value={n}>{n}</option>{/each}
                  </select>
                </div>
              </div>
              {#if ng.type !== 'select'}
                <div class="form-row2">
                  <div class="form-col">
                    <label class="form-label">URL</label>
                    <input class="form-input" bind:value={ng.url} />
                  </div>
                  <div class="form-col form-col-sm">
                    <label class="form-label">{ru ? 'Интервал (с)' : 'Interval (s)'}</label>
                    <input class="form-input" type="number" bind:value={ng.interval} />
                  </div>
                </div>
              {/if}
              <div class="form-actions">
                <button class="btn btn-secondary" on:click={() => showGroupForm = false}>{ru ? 'Отмена' : 'Cancel'}</button>
                <button class="btn btn-primary" on:click={addGroup}>{ru ? 'Добавить' : 'Add'}</button>
              </div>
            </div>
          {:else}
            <button class="add-btn" on:click={() => showGroupForm = true}>
              + {ru ? 'Добавить группу' : 'Add group'}
            </button>
          {/if}
        </div>
      {/if}

      <!-- RULES -->
      {#if activeSection === 'rules'}
        <div class="sec-body">
          {#each rules as r, i (r.id)}
            <div class="item-row item-row-rule">
              <div class="rule-order">
                <button class="order-btn" on:click={() => moveRule(r.id, -1)} disabled={i === 0}>▲</button>
                <button class="order-btn" on:click={() => moveRule(r.id, 1)} disabled={i === rules.length - 1}>▼</button>
              </div>
              <span class="item-badge type-rule">{r.type}</span>
              {#if r.type !== 'MATCH'}
                <span class="item-name rule-value">{r.value}</span>
              {/if}
              <span class="item-meta">→ {r.outbound}</span>
              <button class="item-del" on:click={() => removeRule(r.id)}>✕</button>
            </div>
          {/each}

          {#if showRuleForm}
            <div class="form-card">
              <div class="form-row2">
                <div class="form-col">
                  <label class="form-label">{ru ? 'Тип правила' : 'Rule type'}</label>
                  <select class="form-select" bind:value={nr.type}>
                    {#each RULE_TYPES as t}<option value={t}>{t}</option>{/each}
                  </select>
                </div>
                <div class="form-col">
                  <label class="form-label">{ru ? 'Исходящий' : 'Outbound'}</label>
                  <select class="form-select" bind:value={nr.outbound}>
                    {#each allProxyNames as n}<option value={n}>{n}</option>{/each}
                  </select>
                </div>
              </div>
              {#if nr.type !== 'MATCH'}
                <div class="form-row">
                  <label class="form-label">{ru ? 'Значение' : 'Value'}</label>
                  <input class="form-input" bind:value={nr.value}
                    placeholder={nr.type === 'GEOIP' ? 'CN' : nr.type === 'GEOSITE' ? 'google' : nr.type === 'IP-CIDR' ? '192.168.0.0/16' : 'example.com'} />
                </div>
              {/if}
              <div class="form-actions">
                <button class="btn btn-secondary" on:click={() => showRuleForm = false}>{ru ? 'Отмена' : 'Cancel'}</button>
                <button class="btn btn-primary" on:click={addRule}>{ru ? 'Добавить' : 'Add'}</button>
              </div>
            </div>
          {:else}
            <button class="add-btn" on:click={() => showRuleForm = true}>
              + {ru ? 'Добавить правило' : 'Add rule'}
            </button>
          {/if}
        </div>
      {/if}

      <!-- DNS -->
      {#if activeSection === 'dns'}
        <div class="sec-body">
          <div class="toggle-row">
            <label class="toggle-label">
              <input type="checkbox" bind:checked={dns.enabled} />
              <span>{ru ? 'Включить DNS' : 'Enable DNS'}</span>
            </label>
          </div>
          {#if dns.enabled}
            <div class="form-row">
              <label class="form-label">{ru ? 'Режим' : 'Enhanced mode'}</label>
              <select class="form-select" bind:value={dns.enhancedMode}>
                <option value="fake-ip">fake-ip</option>
                <option value="redir-host">redir-host</option>
              </select>
            </div>
            {#if dns.enhancedMode === 'fake-ip'}
              <div class="form-row">
                <label class="form-label">Fake-IP Range</label>
                <input class="form-input" bind:value={dns.fakeIPRange} />
              </div>
            {/if}
            <div class="form-row">
              <label class="form-label">Nameservers</label>
              <textarea class="form-textarea" bind:value={dns.nameservers} rows="3"
                on:change={(e) => dns.nameservers = e.currentTarget.value.split('\n').filter(Boolean)}
              >{dns.nameservers.join('\n')}</textarea>
            </div>
            <div class="form-row">
              <label class="form-label">Fallback</label>
              <textarea class="form-textarea" bind:value={dns.fallback} rows="2"
                on:change={(e) => dns.fallback = e.currentTarget.value.split('\n').filter(Boolean)}
              >{dns.fallback.join('\n')}</textarea>
            </div>
          {/if}
        </div>
      {/if}

      <!-- TUN -->
      {#if activeSection === 'tun'}
        <div class="sec-body">
          <div class="toggle-row">
            <label class="toggle-label">
              <input type="checkbox" bind:checked={tun.enabled} />
              <span>{ru ? 'Включить TUN' : 'Enable TUN'}</span>
            </label>
          </div>
          {#if tun.enabled}
            <div class="form-row">
              <label class="form-label">Stack</label>
              <select class="form-select" bind:value={tun.stack}>
                <option value="mixed">mixed</option>
                <option value="system">system</option>
                <option value="gvisor">gvisor</option>
              </select>
            </div>
            <div class="toggle-row">
              <label class="toggle-label">
                <input type="checkbox" bind:checked={tun.autoRoute} />
                <span>auto-route</span>
              </label>
            </div>
            <div class="toggle-row">
              <label class="toggle-label">
                <input type="checkbox" bind:checked={tun.autoDetectInterface} />
                <span>auto-detect-interface</span>
              </label>
            </div>
            <div class="form-row">
              <label class="form-label">DNS hijack</label>
              <input class="form-input" value={tun.dnsHijack.join(', ')}
                on:change={(e) => tun.dnsHijack = e.currentTarget.value.split(',').map(s => s.trim()).filter(Boolean)} />
            </div>
          {/if}
        </div>
      {/if}
    </div>

    <!-- Right: YAML preview -->
    <div class="gen-right">
      <div class="preview-header">
        <span class="preview-title">YAML {ru ? 'превью' : 'preview'}</span>
        {#if yaml}
          <button class="btn btn-secondary btn-sm" on:click={copyYAML}>
            <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><rect x="9" y="9" width="13" height="13" rx="2"/><path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"/></svg>
          </button>
        {/if}
      </div>
      <pre class="yaml-preview">{yaml || (ru ? '# Добавьте элементы слева\n# чтобы сгенерировать YAML' : '# Add elements on the left\n# to generate YAML')}</pre>
    </div>
  </div>
</div>

<style>
  .crumb-sep { color: var(--fg-faint); margin: 0 6px; }

  .gen-layout {
    display: grid;
    grid-template-columns: 1fr 380px;
    gap: 20px;
    align-items: start;
  }

  /* Sections */
  .sec-tabs {
    display: flex;
    gap: 2px;
    background: rgba(255,255,255,0.03);
    border: 1px solid var(--border);
    border-radius: var(--radius);
    padding: 4px;
    margin-bottom: 16px;
  }

  .sec-tab {
    flex: 1;
    background: none;
    border: none;
    color: var(--fg-secondary);
    font-size: 12px;
    font-weight: 500;
    padding: 6px 8px;
    border-radius: var(--radius-sm);
    cursor: pointer;
    display: flex;
    align-items: center;
    justify-content: center;
    gap: 5px;
    transition: background var(--transition-fast), color var(--transition-fast);
  }

  .sec-tab.active {
    background: rgba(255,255,255,0.08);
    color: var(--fg-primary);
  }

  .sec-count {
    background: var(--primary);
    color: #0c2237;
    font-size: 9px;
    font-weight: 700;
    border-radius: 8px;
    padding: 1px 5px;
    line-height: 1.4;
  }

  .sec-dot {
    width: 6px; height: 6px;
    background: var(--success);
    border-radius: 50%;
  }

  .sec-body {
    display: flex;
    flex-direction: column;
    gap: 8px;
  }

  /* Item rows */
  .item-row {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 10px 14px;
    background: var(--bg-card);
    border: 1px solid var(--border);
    border-radius: var(--radius);
  }

  .item-row-rule { gap: 8px; }

  .item-badge {
    font-size: 10px;
    font-weight: 700;
    padding: 2px 7px;
    border-radius: 10px;
    text-transform: uppercase;
    flex-shrink: 0;
  }

  .type-vless    { background: rgba(41,194,240,0.15); color: var(--primary); }
  .type-hysteria2 { background: rgba(70,209,138,0.15); color: var(--success); }
  .type-tuic     { background: rgba(240,180,80,0.15); color: var(--warning); }
  .type-ss       { background: rgba(239,91,107,0.15); color: var(--danger); }
  .type-vmess    { background: rgba(255,255,255,0.08); color: var(--fg-secondary); }
  .type-group    { background: rgba(139,92,246,0.15); color: #a78bfa; }
  .type-rule     { background: rgba(255,255,255,0.05); color: var(--fg-dim); font-size: 9px; }

  .item-name {
    flex: 1;
    font-size: 13px;
    font-weight: 500;
    color: var(--fg-primary);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .rule-value {
    font-family: 'JetBrains Mono', monospace;
    font-size: 12px;
  }

  .item-meta {
    font-size: 11px;
    color: var(--fg-dim);
    flex-shrink: 0;
  }

  .item-del {
    background: none;
    border: none;
    color: var(--fg-faint);
    cursor: pointer;
    font-size: 11px;
    padding: 2px 4px;
    border-radius: var(--radius-sm);
    transition: color var(--transition-fast);
    flex-shrink: 0;
    line-height: 1;
  }

  .item-del:hover { color: var(--danger); }

  .rule-order { display: flex; flex-direction: column; gap: 1px; flex-shrink: 0; }
  .order-btn {
    background: none; border: none; color: var(--fg-faint);
    font-size: 9px; cursor: pointer; padding: 1px 3px; line-height: 1;
    transition: color var(--transition-fast);
  }
  .order-btn:hover:not(:disabled) { color: var(--fg-primary); }
  .order-btn:disabled { opacity: 0.3; cursor: default; }

  /* Form */
  .form-card {
    background: var(--bg-elevated);
    border: 1px solid var(--border-strong);
    border-radius: var(--radius);
    padding: 16px;
    display: flex;
    flex-direction: column;
    gap: 10px;
  }

  .form-row { display: flex; flex-direction: column; gap: 4px; }
  .form-row2 { display: flex; gap: 10px; }
  .form-col { display: flex; flex-direction: column; gap: 4px; flex: 1; }
  .form-col-sm { flex: 0 0 100px; }

  .form-label {
    font-size: 11px;
    color: var(--fg-dim);
    font-weight: 500;
  }

  .form-input, .form-select {
    background: var(--bg-card);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    color: var(--fg-primary);
    font-size: 13px;
    padding: 6px 10px;
    outline: none;
    width: 100%;
    transition: border-color var(--transition-fast);
  }

  .form-input:focus, .form-select:focus {
    border-color: var(--primary);
  }

  .form-textarea {
    background: var(--bg-card);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    color: var(--fg-primary);
    font-size: 12px;
    font-family: 'JetBrains Mono', monospace;
    padding: 6px 10px;
    outline: none;
    width: 100%;
    resize: vertical;
    transition: border-color var(--transition-fast);
  }

  .form-textarea:focus { border-color: var(--primary); }

  .form-select-inline {
    background: none;
    border: none;
    border-radius: var(--radius-sm);
    color: var(--fg-secondary);
    font-size: 12px;
    padding: 2px 4px;
    outline: none;
    cursor: pointer;
  }

  .input-with-btn {
    display: flex;
    gap: 6px;
    align-items: center;
  }

  .input-with-btn .form-input { flex: 1; }

  .btn-gen {
    background: rgba(255,255,255,0.05);
    border: 1px solid var(--border);
    color: var(--fg-secondary);
    border-radius: var(--radius-sm);
    padding: 6px 10px;
    cursor: pointer;
    font-size: 14px;
    transition: background var(--transition-fast);
    flex-shrink: 0;
  }

  .btn-gen:hover { background: rgba(255,255,255,0.1); color: var(--fg-primary); }

  .tag-input-wrap {
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
    background: var(--bg-card);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    padding: 6px 8px;
    align-items: center;
  }

  .tag-pill {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    background: rgba(41,194,240,0.12);
    border: 1px solid rgba(41,194,240,0.25);
    color: var(--primary);
    font-size: 11px;
    border-radius: 10px;
    padding: 2px 8px;
  }

  .tag-rm {
    background: none; border: none; color: inherit;
    cursor: pointer; font-size: 10px; padding: 0; line-height: 1;
  }

  .form-actions {
    display: flex;
    gap: 8px;
    justify-content: flex-end;
    margin-top: 4px;
  }

  .add-btn {
    width: 100%;
    background: rgba(255,255,255,0.02);
    border: 1px dashed var(--border-strong);
    border-radius: var(--radius);
    color: var(--fg-dim);
    font-size: 13px;
    padding: 12px;
    cursor: pointer;
    transition: background var(--transition-fast), color var(--transition-fast), border-color var(--transition-fast);
    text-align: center;
  }

  .add-btn:hover {
    background: rgba(41,194,240,0.05);
    border-color: rgba(41,194,240,0.3);
    color: var(--primary);
  }

  /* Toggle */
  .toggle-row {
    display: flex;
    align-items: center;
  }

  .toggle-label {
    display: flex;
    align-items: center;
    gap: 8px;
    cursor: pointer;
    font-size: 13px;
    color: var(--fg-primary);
  }

  /* YAML preview */
  .gen-right {
    position: sticky;
    top: 20px;
    background: var(--bg-card);
    border: 1px solid var(--border);
    border-radius: var(--radius);
    overflow: hidden;
    display: flex;
    flex-direction: column;
    max-height: calc(100vh - 140px);
  }

  .preview-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 10px 14px;
    border-bottom: 1px solid var(--border);
    flex-shrink: 0;
  }

  .preview-title {
    font-size: 11px;
    font-weight: 600;
    color: var(--fg-dim);
    text-transform: uppercase;
    letter-spacing: 0.05em;
  }

  .btn-sm { padding: 4px 8px; font-size: 12px; }

  .yaml-preview {
    flex: 1;
    overflow-y: auto;
    margin: 0;
    padding: 14px 16px;
    font-family: 'JetBrains Mono', 'Fira Code', monospace;
    font-size: 11.5px;
    line-height: 1.6;
    color: var(--fg-secondary);
    white-space: pre;
    scrollbar-width: thin;
    scrollbar-color: var(--border-strong) transparent;
  }

  @media (max-width: 900px) {
    .gen-layout { grid-template-columns: 1fr; }
    .gen-right { position: static; max-height: 300px; }
  }
</style>
