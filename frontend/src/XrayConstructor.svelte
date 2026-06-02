<script lang="ts">
  import { t } from './i18n';
  import { showToast } from './stores';

  export let onSwitchTab: (tab: string) => void = () => {};
  export let selectedFile: string = '';
  export let onInsertIntoEditor: (content: string) => void = () => {};
  export let embedded: boolean = false;

  type XrayProtocol = 'vless-reality' | 'vless-ws-tls' | 'vmess-ws-tls' | 'trojan-tls' | 'hysteria2';
  type GenerateMode = 'outbound' | 'full';
  type Scenario = 'bypass-ru' | 'full-vpn' | 'gaming' | 'minimal';

  let protocol: XrayProtocol = 'vless-reality';
  let mode: GenerateMode = 'outbound';
  let scenario: Scenario = 'bypass-ru';

  let params = {
    server: '',
    port: 443,
    uuid: crypto.randomUUID(),
    flow: 'xtls-rprx-vision',
    publicKey: '',
    shortId: '',
    sni: 'www.apple.com',
    fingerprint: 'chrome',
    wsPath: '/',
    password: '',
    alterID: 0,
    auth: ''
  };

  const protocols: { value: XrayProtocol; label: string }[] = [
    { value: 'vless-reality', label: 'VLESS + Reality' },
    { value: 'vless-ws-tls', label: 'VLESS + WS + TLS' },
    { value: 'vmess-ws-tls', label: 'VMess + WS + TLS' },
    { value: 'trojan-tls', label: 'Trojan + TLS' },
    { value: 'hysteria2', label: 'Hysteria2' }
  ];

  const scenarios: { value: Scenario; labelKey: string }[] = [
    { value: 'bypass-ru', labelKey: 'editor.scenario_ru_bypass' },
    { value: 'full-vpn', labelKey: 'editor.scenario_full_vpn' },
    { value: 'gaming', labelKey: 'editor.scenario_gaming' },
    { value: 'minimal', labelKey: 'editor.scenario_minimal' }
  ];

  function buildStreamSettings(proto: XrayProtocol): object {
    switch (proto) {
      case 'vless-reality':
        return {
          network: 'tcp',
          security: 'reality',
          realitySettings: {
            serverName: params.sni,
            fingerprint: params.fingerprint,
            publicKey: params.publicKey,
            shortId: params.shortId
          }
        };
      case 'vless-ws-tls':
        return {
          network: 'ws',
          security: 'tls',
          wsSettings: { path: params.wsPath },
          tlsSettings: { serverName: params.sni }
        };
      case 'vmess-ws-tls':
        return {
          network: 'ws',
          security: 'tls',
          wsSettings: { path: params.wsPath },
          tlsSettings: { serverName: params.sni }
        };
      case 'trojan-tls':
        return {
          network: 'tcp',
          security: 'tls',
          tlsSettings: { serverName: params.sni }
        };
      case 'hysteria2':
        return {
          network: 'udp',
          security: 'tls',
          tlsSettings: { serverName: params.sni }
        };
      default:
        return {};
    }
  }

  function buildOutbound(proto: XrayProtocol, tag: string = 'proxy'): object {
    const streamSettings = buildStreamSettings(proto);
    switch (proto) {
      case 'vless-reality':
        return {
          tag,
          protocol: 'vless',
          settings: {
            vnext: [{
              address: params.server || 'your-server.com',
              port: params.port,
              users: [{ id: params.uuid, flow: params.flow, encryption: 'none' }]
            }]
          },
          streamSettings
        };
      case 'vless-ws-tls':
        return {
          tag,
          protocol: 'vless',
          settings: {
            vnext: [{
              address: params.server || 'your-server.com',
              port: params.port,
              users: [{ id: params.uuid, encryption: 'none' }]
            }]
          },
          streamSettings
        };
      case 'vmess-ws-tls':
        return {
          tag,
          protocol: 'vmess',
          settings: {
            vnext: [{
              address: params.server || 'your-server.com',
              port: params.port,
              users: [{ id: params.uuid, alterId: params.alterID, security: 'auto' }]
            }]
          },
          streamSettings
        };
      case 'trojan-tls':
        return {
          tag,
          protocol: 'trojan',
          settings: {
            servers: [{
              address: params.server || 'your-server.com',
              port: params.port,
              password: params.password || 'your-password'
            }]
          },
          streamSettings
        };
      case 'hysteria2':
        return {
          tag,
          protocol: 'hysteria2',
          settings: {
            servers: [{
              address: params.server || 'your-server.com',
              port: params.port,
              password: params.auth || 'your-auth'
            }]
          },
          streamSettings
        };
      default:
        return { tag, protocol: 'freedom' };
    }
  }

  function buildRoutingRules(sc: Scenario): object[] {
    switch (sc) {
      case 'bypass-ru':
        return [
          { type: 'field', ip: ['geoip:private', 'geoip:ru'], outboundTag: 'direct' },
          { type: 'field', domain: ['geosite:category-gov-ru'], outboundTag: 'direct' },
          { type: 'field', outboundTag: 'proxy' }
        ];
      case 'full-vpn':
        return [
          { type: 'field', ip: ['geoip:private'], outboundTag: 'direct' },
          { type: 'field', outboundTag: 'proxy' }
        ];
      case 'gaming':
        return [
          { type: 'field', ip: ['geoip:private', 'geoip:ru', 'geoip:cn'], outboundTag: 'direct' },
          { type: 'field', outboundTag: 'proxy' }
        ];
      case 'minimal':
        return [
          { type: 'field', ip: ['geoip:private'], outboundTag: 'direct' },
          { type: 'field', outboundTag: 'proxy' }
        ];
      default:
        return [{ type: 'field', outboundTag: 'proxy' }];
    }
  }

  function generateConfig(): string {
    if (mode === 'outbound') {
      const outbound = buildOutbound(protocol);
      return JSON.stringify(outbound, null, 2);
    }

    // Full config
    const config = {
      log: { loglevel: 'warning' },
      inbounds: [
        {
          tag: 'socks',
          port: 10808,
          listen: '127.0.0.1',
          protocol: 'socks',
          settings: { udp: true }
        }
      ],
      outbounds: [
        buildOutbound(protocol, 'proxy'),
        { tag: 'direct', protocol: 'freedom' },
        { tag: 'block', protocol: 'blackhole' }
      ],
      routing: {
        domainStrategy: scenario === 'bypass-ru' ? 'IPIfNonMatch' : 'AsIs',
        rules: buildRoutingRules(scenario)
      }
    };
    return JSON.stringify(config, null, 2);
  }

  $: generatedJSON = generateConfig();
  $: isValid = (() => {
    try {
      JSON.parse(generatedJSON);
      return true;
    } catch {
      return false;
    }
  })();
  $: validationErrors = isValid ? [] : ['Не удалось разобрать JSON. Проверьте параметры конфигурации.'];

  function openInEditor() {
    if (onInsertIntoEditor) {
      onInsertIntoEditor(generatedJSON);
      showToast('success', $t('editor.yaml_inserted') || 'Конфигурация вставлена в редактор.');
    } else {
      onSwitchTab('editor');
    }
  }

  function handleProtocolChange(proto: XrayProtocol) {
    protocol = proto;
    // Reset protocol-specific fields
    if (proto === 'vless-reality') {
      params.flow = 'xtls-rprx-vision';
      params.sni = 'www.apple.com';
      params.fingerprint = 'chrome';
    } else if (proto === 'vless-ws-tls' || proto === 'vmess-ws-tls') {
      params.wsPath = '/';
    }
  }
</script>

<div class="xray-constructor">
  <!-- Protocol selector -->
  <div class="constructor-section">
    <label class="section-label">Протокол</label>
    <div class="protocol-tabs">
      {#each protocols as proto}
        <button
          class="protocol-chip"
          class:active={protocol === proto.value}
          aria-pressed={protocol === proto.value}
          on:click={() => handleProtocolChange(proto.value)}
        >
          {proto.label}
        </button>
      {/each}
    </div>
  </div>

  <!-- Mode selector -->
  <div class="constructor-section mode-section">
    <label class="section-label">{$t('editor.constructor_preview')}</label>
    <div class="mode-radios">
      <label class="mode-radio">
        <input
          type="radio"
          name="xray-mode"
          value="outbound"
          bind:group={mode}
        />
        <span>{$t('editor.xray_mode_outbound')}</span>
      </label>
      <label class="mode-radio">
        <input
          type="radio"
          name="xray-mode"
          value="full"
          bind:group={mode}
        />
        <span>{$t('editor.xray_mode_full')}</span>
      </label>
    </div>
  </div>

  <!-- Scenario bar (only visible in full mode) -->
  {#if mode === 'full'}
    <div class="constructor-section">
      <label class="section-label">{$t('editor.constructor_scenario')}</label>
      <div class="constructor-scenario-bar">
        {#each scenarios as sc}
          <button
            class="scenario-chip"
            class:active={scenario === sc.value}
            aria-pressed={scenario === sc.value}
            on:click={() => { scenario = sc.value; }}
          >
            {$t(sc.labelKey)}
          </button>
        {/each}
      </div>
    </div>
  {/if}

  <!-- Connection parameters -->
  <div class="constructor-section">
    <label class="section-label">Параметры подключения</label>
    <div class="params-grid">
      <div class="param-row">
        <label class="param-label" for="xray-server">Сервер</label>
        <input
          id="xray-server"
          class="form-input"
          type="text"
          name="server"
          placeholder="example.com"
          bind:value={params.server}
        />
      </div>
      <div class="param-row">
        <label class="param-label" for="xray-port">Порт</label>
        <input
          id="xray-port"
          class="form-input"
          type="number"
          min="1"
          max="65535"
          bind:value={params.port}
        />
      </div>

      {#if protocol === 'vless-reality' || protocol === 'vless-ws-tls' || protocol === 'vmess-ws-tls'}
        <div class="param-row">
          <label class="param-label" for="xray-uuid">UUID</label>
          <input
            id="xray-uuid"
            class="form-input"
            type="text"
            bind:value={params.uuid}
          />
        </div>
      {/if}

      {#if protocol === 'vless-reality'}
        <div class="param-row">
          <label class="param-label" for="xray-pk">Public Key</label>
          <input
            id="xray-pk"
            class="form-input"
            type="text"
            placeholder="base64 публичный ключ"
            bind:value={params.publicKey}
          />
        </div>
        <div class="param-row">
          <label class="param-label" for="xray-sid">Short ID</label>
          <input
            id="xray-sid"
            class="form-input"
            type="text"
            placeholder="hex short ID"
            bind:value={params.shortId}
          />
        </div>
        <div class="param-row">
          <label class="param-label" for="xray-sni">SNI</label>
          <input
            id="xray-sni"
            class="form-input"
            type="text"
            bind:value={params.sni}
          />
        </div>
        <div class="param-row">
          <label class="param-label" for="xray-fp">Fingerprint</label>
          <input
            id="xray-fp"
            class="form-input"
            type="text"
            bind:value={params.fingerprint}
          />
        </div>
      {/if}

      {#if protocol === 'vless-ws-tls' || protocol === 'vmess-ws-tls'}
        <div class="param-row">
          <label class="param-label" for="xray-wspath">WS Path</label>
          <input
            id="xray-wspath"
            class="form-input"
            type="text"
            bind:value={params.wsPath}
          />
        </div>
        <div class="param-row">
          <label class="param-label" for="xray-sni2">SNI</label>
          <input
            id="xray-sni2"
            class="form-input"
            type="text"
            bind:value={params.sni}
          />
        </div>
      {/if}

      {#if protocol === 'vmess-ws-tls'}
        <div class="param-row">
          <label class="param-label" for="xray-alterid">Alter ID</label>
          <input
            id="xray-alterid"
            class="form-input"
            type="number"
            min="0"
            bind:value={params.alterID}
          />
        </div>
      {/if}

      {#if protocol === 'trojan-tls'}
        <div class="param-row">
          <label class="param-label" for="xray-password">Пароль</label>
          <input
            id="xray-password"
            class="form-input"
            type="text"
            placeholder="пароль Trojan"
            bind:value={params.password}
          />
        </div>
        <div class="param-row">
          <label class="param-label" for="xray-trojan-sni">SNI</label>
          <input
            id="xray-trojan-sni"
            class="form-input"
            type="text"
            bind:value={params.sni}
          />
        </div>
      {/if}

      {#if protocol === 'hysteria2'}
        <div class="param-row">
          <label class="param-label" for="xray-auth">Auth</label>
          <input
            id="xray-auth"
            class="form-input"
            type="text"
            placeholder="auth строка"
            bind:value={params.auth}
          />
        </div>
        <div class="param-row">
          <label class="param-label" for="xray-h2-sni">SNI</label>
          <input
            id="xray-h2-sni"
            class="form-input"
            type="text"
            bind:value={params.sni}
          />
        </div>
      {/if}
    </div>
  </div>

  <!-- JSON Preview -->
  <div class="constructor-section">
    <label class="section-label">{$t('editor.constructor_preview')}</label>
    <div
      class="constructor-preview-pane json-preview"
      class:invalid={!isValid}
      aria-label="JSON превью"
    >
      <pre class="xray-preview">{generatedJSON}</pre>
    </div>
    {#if !isValid}
      <div class="validation-errors" aria-live="polite">
        {#each validationErrors as err}
          <p class="validation-error">{$t('editor.constructor_invalid')}: {err}</p>
        {/each}
      </div>
    {/if}
  </div>

  <!-- Actions -->
  <div class="constructor-actions">
    <button
      class="btn btn-secondary"
      disabled={!isValid}
      aria-disabled={!isValid}
      on:click={openInEditor}
    >
      {$t('editor.open_in_editor')}
    </button>
  </div>
</div>

<style>
  .xray-constructor {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-4, 16px);
  }

  .constructor-section {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-2, 8px);
  }

  .section-label {
    font-size: var(--font-size-xs, 0.75rem);
    font-weight: 700;
    color: var(--fg-secondary);
    text-transform: uppercase;
    letter-spacing: 0.12em;
  }

  .protocol-tabs {
    display: flex;
    flex-wrap: wrap;
    gap: var(--spacing-2, 8px);
  }

  .protocol-chip {
    padding: 4px 12px;
    border: 1px solid var(--border);
    border-radius: 20px;
    background: transparent;
    color: var(--fg-secondary);
    font-size: var(--font-size-sm, 0.8125rem);
    cursor: pointer;
    transition: border-color var(--transition-fast), color var(--transition-fast), background var(--transition-fast);
    min-height: 36px;
  }

  .protocol-chip:hover {
    border-color: var(--accent);
    color: var(--fg);
  }

  .protocol-chip.active {
    border-color: var(--accent);
    color: var(--accent);
    background: color-mix(in srgb, var(--accent) 10%, transparent);
  }

  .mode-section .section-label {
    display: none;
  }

  .mode-radios {
    display: flex;
    gap: var(--spacing-4, 16px);
  }

  .mode-radio {
    display: flex;
    align-items: center;
    gap: var(--spacing-1, 4px);
    cursor: pointer;
    color: var(--fg-secondary);
    font-size: var(--font-size-sm, 0.8125rem);
  }

  .mode-radio input[type="radio"] {
    accent-color: var(--accent);
  }

  .constructor-scenario-bar {
    display: flex;
    gap: var(--spacing-2, 8px);
    overflow-x: auto;
    scrollbar-width: thin;
    scrollbar-color: var(--border-strong, var(--border)) transparent;
    padding-bottom: 2px;
  }

  .constructor-scenario-bar::-webkit-scrollbar {
    height: 4px;
  }

  .constructor-scenario-bar::-webkit-scrollbar-thumb {
    background: var(--border-strong, var(--border));
    border-radius: 2px;
  }

  .scenario-chip {
    padding: 4px 12px;
    border: 1px solid var(--border);
    border-radius: 20px;
    background: transparent;
    color: var(--fg-secondary);
    font-size: 12px;
    cursor: pointer;
    white-space: nowrap;
    transition: border-color var(--transition-fast), color var(--transition-fast), background var(--transition-fast);
    min-height: 36px;
  }

  .scenario-chip:hover {
    border-color: var(--accent);
    color: var(--fg);
  }

  .scenario-chip.active {
    border-color: var(--accent);
    color: var(--accent);
    background: color-mix(in srgb, var(--accent) 10%, transparent);
  }

  .params-grid {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-2, 8px);
  }

  .param-row {
    display: grid;
    grid-template-columns: 120px 1fr;
    gap: var(--spacing-2, 8px);
    align-items: center;
  }

  .param-label {
    font-size: var(--font-size-sm, 0.8125rem);
    color: var(--fg-secondary);
    text-align: right;
  }

  .form-input {
    width: 100%;
    padding: 6px 10px;
    background: var(--bg-card, var(--bg));
    border: 1px solid var(--border);
    border-radius: var(--radius-md, 6px);
    color: var(--fg);
    font-size: var(--font-size-sm, 0.8125rem);
    font-family: inherit;
    transition: border-color var(--transition-fast);
    box-sizing: border-box;
  }

  .form-input:focus {
    outline: none;
    border-color: var(--accent);
  }

  .constructor-preview-pane {
    min-height: 200px;
    max-height: 320px;
    overflow-y: auto;
    border: 1px solid var(--border);
    border-radius: var(--radius-md, 6px);
    background: var(--bg-deep, var(--bg));
    padding: var(--spacing-2, 8px);
    scrollbar-width: thin;
    scrollbar-color: var(--border-strong, var(--border)) transparent;
    transition: border-color var(--transition-fast);
  }

  .constructor-preview-pane::-webkit-scrollbar {
    width: 4px;
  }

  .constructor-preview-pane::-webkit-scrollbar-thumb {
    background: var(--border-strong, var(--border));
    border-radius: 2px;
  }

  .constructor-preview-pane.invalid {
    border-color: var(--danger);
  }

  .xray-preview {
    margin: 0;
    font-family: 'JetBrains Mono', monospace;
    font-size: var(--font-size-sm, 0.8125rem);
    line-height: 1.5;
    color: var(--fg);
    white-space: pre;
    word-break: break-all;
  }

  .validation-errors {
    display: flex;
    flex-direction: column;
    gap: 4px;
  }

  .validation-error {
    margin: 0;
    font-size: 12px;
    color: var(--danger);
  }

  .constructor-actions {
    display: flex;
    justify-content: flex-end;
    padding-top: var(--spacing-2, 8px);
  }

  .btn {
    padding: 8px 16px;
    border-radius: var(--radius-md, 6px);
    font-size: var(--font-size-sm, 0.8125rem);
    font-weight: 500;
    cursor: pointer;
    border: 1px solid transparent;
    transition: background var(--transition-fast), color var(--transition-fast), border-color var(--transition-fast);
    min-height: 36px;
  }

  .btn-secondary {
    background: var(--bg-card, var(--bg));
    border-color: var(--accent);
    color: var(--accent);
  }

  .btn-secondary:hover:not(:disabled) {
    background: color-mix(in srgb, var(--accent) 15%, transparent);
  }

  .btn:disabled,
  .btn[aria-disabled="true"] {
    opacity: 0.4;
    cursor: not-allowed;
    pointer-events: none;
  }
</style>
