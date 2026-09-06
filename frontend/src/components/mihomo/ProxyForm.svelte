<script lang="ts">
  import { t } from '../../i18n';

  let {
    np = $bindable(),
    onSave,
    onCancel,
    isEdit = false
  }: {
    np: any;
    onSave: () => void;
    onCancel: () => void;
    isEdit?: boolean;
  } = $props();

  const PROXY_TYPES = [
    'vless',
    'hysteria2',
    'tuic',
    'ss',
    'vmess',
    'trojan',
    'wireguard',
    'socks5',
    'http'
  ];
  const CIPHERS = ['aes-256-gcm', 'aes-128-gcm', 'chacha20-poly1305', '2022-blake3-aes-256-gcm'];

  // Ограничения AmneziaWG (TMPL-08, D-07):
  // H1–H4 попарно различны и > 4, Jmin < Jmax, S1 + 56 ≠ S2
  const awgCountsH = $derived.by(() => {
    const counts = new Map<number, number>();
    for (const h of [Number(np.awgH1), Number(np.awgH2), Number(np.awgH3), Number(np.awgH4)]) {
      counts.set(h, (counts.get(h) || 0) + 1);
    }
    return counts;
  });

  const isH1Invalid = $derived(
    np.awgEnabled && (Number(np.awgH1) <= 4 || (awgCountsH.get(Number(np.awgH1)) || 0) > 1)
  );
  const isH2Invalid = $derived(
    np.awgEnabled && (Number(np.awgH2) <= 4 || (awgCountsH.get(Number(np.awgH2)) || 0) > 1)
  );
  const isH3Invalid = $derived(
    np.awgEnabled && (Number(np.awgH3) <= 4 || (awgCountsH.get(Number(np.awgH3)) || 0) > 1)
  );
  const isH4Invalid = $derived(
    np.awgEnabled && (Number(np.awgH4) <= 4 || (awgCountsH.get(Number(np.awgH4)) || 0) > 1)
  );

  const isJInvalid = $derived(np.awgEnabled && Number(np.awgJmin) >= Number(np.awgJmax));
  const isSInvalid = $derived(np.awgEnabled && Number(np.awgS1) + 56 === Number(np.awgS2));

  const awgConstraintsOk = $derived(
    !np.awgEnabled ||
      (!isJInvalid && !isSInvalid && !isH1Invalid && !isH2Invalid && !isH3Invalid && !isH4Invalid)
  );
</script>

<div class="form-card">
  <div class="form-row">
    <label class="form-label" for="proxy-type">{$t('proxies.type')}</label>
    <select id="proxy-type" class="form-select" bind:value={np.type}>
      {#each PROXY_TYPES as pt}<option value={pt}>{pt}</option>{/each}
    </select>
  </div>
  <div class="form-row">
    <label class="form-label" for="proxy-name">{$t('subscr.name')}</label>
    <input id="proxy-name" class="form-input" bind:value={np.name} placeholder="my-proxy" />
  </div>
  <div class="form-row2">
    <div class="form-col">
      <label class="form-label" for="proxy-server">{$t('proxies.server')}</label>
      <input
        id="proxy-server"
        class="form-input"
        bind:value={np.server}
        placeholder="example.com"
      />
    </div>
    <div class="form-col form-col-sm">
      <label class="form-label" for="proxy-port">{$t('proxies.port')}</label>
      <input
        id="proxy-port"
        class="form-input"
        type="number"
        bind:value={np.port}
        min="1"
        max="65535"
      />
    </div>
  </div>

  {#if np.type === 'vless'}
    <div class="form-row">
      <label class="form-label" for="proxy-vless-uuid">UUID</label>
      <div class="input-with-btn">
        <input id="proxy-vless-uuid" class="form-input" bind:value={np.uuid} placeholder="uuid" />
        <button
          class="btn-gen"
          onclick={() => (np.uuid = crypto.randomUUID())}
          title={$t('app.generate')}>⟳</button
        >
      </div>
    </div>
    <div class="form-row">
      <label class="form-label" for="proxy-vless-public-key">Reality Public Key</label>
      <input
        id="proxy-vless-public-key"
        class="form-input"
        bind:value={np.publicKey}
        placeholder="public-key"
      />
    </div>
    <div class="form-row2">
      <div class="form-col">
        <label class="form-label" for="proxy-vless-short-id">Short ID</label>
        <input
          id="proxy-vless-short-id"
          class="form-input"
          bind:value={np.shortId}
          placeholder="short-id"
        />
      </div>
      <div class="form-col">
        <label class="form-label" for="proxy-vless-sni">SNI</label>
        <input
          id="proxy-vless-sni"
          class="form-input"
          bind:value={np.servername}
          placeholder="www.apple.com"
        />
      </div>
    </div>
  {:else if np.type === 'hysteria2'}
    <div class="form-row">
      <label class="form-label" for="proxy-hysteria2-password">{$t('proxies.password')}</label>
      <input
        id="proxy-hysteria2-password"
        class="form-input"
        bind:value={np.password}
        placeholder="password"
      />
    </div>
    <div class="form-row">
      <label class="form-label" for="proxy-hysteria2-sni">SNI</label>
      <input
        id="proxy-hysteria2-sni"
        class="form-input"
        bind:value={np.sni}
        placeholder="example.com"
      />
    </div>
    <div class="form-row">
      <label class="form-label" for="proxy-hysteria2-obfs-type">{$t('editor.obfsType')}</label>
      <select id="proxy-hysteria2-obfs-type" class="form-select" bind:value={np.obfsType}>
        <option value="none">{$t('editor.none')}</option>
        <option value="simple">{$t('editor.simple')}</option>
      </select>
    </div>
    {#if np.obfsType === 'simple'}
      <div class="form-row">
        <label class="form-label" for="proxy-hysteria2-obfs-password"
          >{$t('editor.obfsPassword')}</label
        >
        <input
          id="proxy-hysteria2-obfs-password"
          class="form-input"
          bind:value={np.obfsPassword}
          placeholder="obfs password"
        />
      </div>
    {/if}
    <div class="form-row">
      <label
        class="toggle-label"
        style="display: flex; align-items: center; gap: 8px; cursor: pointer; user-select: none;"
      >
        <input type="checkbox" bind:checked={np.skipCertVerify} />
        <span>{$t('editor.skipCertVerify')}</span>
      </label>
    </div>
  {:else if np.type === 'tuic'}
    <div class="form-row">
      <label class="form-label" for="proxy-tuic-uuid">UUID</label>
      <div class="input-with-btn">
        <input id="proxy-tuic-uuid" class="form-input" bind:value={np.uuid} placeholder="uuid" />
        <button class="btn-gen" onclick={() => (np.uuid = crypto.randomUUID())} title="Generate"
          >⟳</button
        >
      </div>
    </div>
    <div class="form-row">
      <label class="form-label" for="proxy-tuic-password">{$t('proxies.password')}</label>
      <input
        id="proxy-tuic-password"
        class="form-input"
        bind:value={np.password}
        placeholder="password"
      />
    </div>
    <div class="form-row">
      <label class="form-label" for="proxy-tuic-sni">SNI</label>
      <input id="proxy-tuic-sni" class="form-input" bind:value={np.sni} placeholder="example.com" />
    </div>
  {:else if np.type === 'ss'}
    <div class="form-row">
      <label class="form-label" for="proxy-ss-cipher">Cipher</label>
      <select id="proxy-ss-cipher" class="form-select" bind:value={np.cipher}>
        {#each CIPHERS as c}<option value={c}>{c}</option>{/each}
      </select>
    </div>
    <div class="form-row">
      <label class="form-label" for="proxy-ss-password">{$t('proxies.password')}</label>
      <input
        id="proxy-ss-password"
        class="form-input"
        bind:value={np.password}
        placeholder="password"
      />
    </div>
  {:else if np.type === 'vmess'}
    <div class="form-row">
      <label class="form-label" for="proxy-vmess-uuid">UUID</label>
      <div class="input-with-btn">
        <input id="proxy-vmess-uuid" class="form-input" bind:value={np.uuid} placeholder="uuid" />
        <button class="btn-gen" onclick={() => (np.uuid = crypto.randomUUID())} title="Generate"
          >⟳</button
        >
      </div>
    </div>
    <div class="form-row2">
      <div class="form-col">
        <label class="form-label" for="proxy-vmess-network">Network</label>
        <select id="proxy-vmess-network" class="form-select" bind:value={np.network}>
          <option value="ws">WebSocket</option>
          <option value="tcp">TCP</option>
          <option value="grpc">gRPC</option>
        </select>
      </div>
      <div class="form-col">
        <label class="form-label" for="proxy-vmess-tls">TLS</label>
        <input id="proxy-vmess-tls" type="checkbox" bind:checked={np.tls} style="margin-top:8px" />
      </div>
    </div>
    {#if np.network === 'ws'}
      <div class="form-row">
        <label class="form-label" for="proxy-vmess-ws-path">WS Path</label>
        <input id="proxy-vmess-ws-path" class="form-input" bind:value={np.wsPath} placeholder="/" />
      </div>
    {/if}
    {#if np.tls}
      <div class="form-row">
        <label class="form-label" for="proxy-vmess-sni">SNI</label>
        <input
          id="proxy-vmess-sni"
          class="form-input"
          bind:value={np.sni}
          placeholder="example.com"
        />
      </div>
    {/if}
  {:else if np.type === 'trojan'}
    <div class="form-row">
      <label class="form-label" for="proxy-trojan-password">{$t('proxies.password')}</label>
      <input
        id="proxy-trojan-password"
        class="form-input"
        bind:value={np.password}
        placeholder="password"
      />
    </div>
    <div class="form-row">
      <label class="form-label" for="proxy-trojan-sni">SNI</label>
      <input
        id="proxy-trojan-sni"
        class="form-input"
        bind:value={np.sni}
        placeholder="example.com"
      />
    </div>
    <div class="form-row2">
      <div class="form-col">
        <label class="form-label" for="proxy-trojan-network">Network</label>
        <select id="proxy-trojan-network" class="form-select" bind:value={np.network}>
          <option value="tcp">TCP</option>
          <option value="ws">WebSocket</option>
        </select>
      </div>
    </div>
    {#if np.network === 'ws'}
      <div class="form-row">
        <label class="form-label" for="proxy-trojan-ws-path">WS Path</label>
        <input
          id="proxy-trojan-ws-path"
          class="form-input"
          bind:value={np.wsPath}
          placeholder="/"
        />
      </div>
    {/if}
    <div class="form-row">
      <label
        class="toggle-label"
        style="display: flex; align-items: center; gap: 8px; cursor: pointer; user-select: none;"
      >
        <input type="checkbox" bind:checked={np.skipCertVerify} />
        <span>{$t('editor.skipCertVerify')}</span>
      </label>
    </div>
  {:else if np.type === 'wireguard'}
    <div class="form-row">
      <label class="form-label" for="proxy-wg-private-key">{$t('proxies.wg_private_key')}</label>
      <input
        id="proxy-wg-private-key"
        class="form-input"
        bind:value={np.wgPrivateKey}
        placeholder="private-key"
      />
    </div>
    <div class="form-row">
      <label class="form-label" for="proxy-wg-public-key">{$t('proxies.wg_public_key')}</label>
      <input
        id="proxy-wg-public-key"
        class="form-input"
        bind:value={np.wgPublicKey}
        placeholder="public-key"
      />
    </div>
    <div class="form-row2">
      <div class="form-col">
        <label class="form-label" for="proxy-wg-ip">{$t('proxies.wg_ip')}</label>
        <input id="proxy-wg-ip" class="form-input" bind:value={np.wgIp} placeholder="10.2.0.2/32" />
      </div>
      <div class="form-col form-col-sm">
        <label class="form-label" for="proxy-wg-mtu">MTU</label>
        <input
          id="proxy-wg-mtu"
          class="form-input"
          type="number"
          bind:value={np.wgMtu}
          min="1280"
          max="1500"
        />
      </div>
    </div>
    <div class="form-row">
      <label class="form-label" for="proxy-wg-preshared-key">Pre-shared Key</label>
      <input
        id="proxy-wg-preshared-key"
        class="form-input"
        bind:value={np.wgPresharedKey}
        placeholder="pre-shared-key (optional)"
      />
    </div>
    <div class="form-row">
      <label
        class="toggle-label"
        style="display: flex; align-items: center; gap: 8px; cursor: pointer; user-select: none;"
      >
        <input type="checkbox" id="proxy-awg-enabled" bind:checked={np.awgEnabled} />
        <span>{$t('proxies.awg_section')}</span>
      </label>
    </div>

    {#if np.awgEnabled}
      <div class="awg-options-block" class:has-warning={!awgConstraintsOk}>
        <div class="awg-group-title">{$t('proxies.awg_junk')}</div>
        <div class="form-row2" style="grid-template-columns: repeat(3, 1fr); gap: 8px;">
          <div class="form-col">
            <label class="form-label" for="proxy-awg-jc">Jc</label>
            <input
              id="proxy-awg-jc"
              class="form-input"
              type="number"
              bind:value={np.awgJc}
              min="1"
              max="128"
            />
          </div>
          <div class="form-col">
            <label class="form-label" for="proxy-awg-jmin">Jmin</label>
            <input
              id="proxy-awg-jmin"
              class="form-input"
              class:input-warning={isJInvalid}
              aria-invalid={isJInvalid}
              type="number"
              bind:value={np.awgJmin}
              min="0"
            />
          </div>
          <div class="form-col">
            <label class="form-label" for="proxy-awg-jmax">Jmax</label>
            <input
              id="proxy-awg-jmax"
              class="form-input"
              class:input-warning={isJInvalid}
              aria-invalid={isJInvalid}
              type="number"
              bind:value={np.awgJmax}
              min="0"
            />
          </div>
        </div>

        <div class="awg-group-title" style="margin-top: 8px;">{$t('proxies.awg_padding')}</div>
        <div class="form-row2" style="grid-template-columns: 1fr 1fr; gap: 8px;">
          <div class="form-col">
            <label class="form-label" for="proxy-awg-s1">S1</label>
            <input
              id="proxy-awg-s1"
              class="form-input"
              class:input-warning={isSInvalid}
              aria-invalid={isSInvalid}
              type="number"
              bind:value={np.awgS1}
              min="0"
            />
          </div>
          <div class="form-col">
            <label class="form-label" for="proxy-awg-s2">S2</label>
            <input
              id="proxy-awg-s2"
              class="form-input"
              class:input-warning={isSInvalid}
              aria-invalid={isSInvalid}
              type="number"
              bind:value={np.awgS2}
              min="0"
            />
          </div>
        </div>

        <div class="awg-group-title" style="margin-top: 8px;">{$t('proxies.awg_headers')}</div>
        <div class="form-row2 awg-headers-grid">
          <div class="form-col">
            <label class="form-label" for="proxy-awg-h1">H1</label>
            <input
              id="proxy-awg-h1"
              class="form-input"
              class:input-warning={isH1Invalid}
              aria-invalid={isH1Invalid}
              type="number"
              bind:value={np.awgH1}
            />
          </div>
          <div class="form-col">
            <label class="form-label" for="proxy-awg-h2">H2</label>
            <input
              id="proxy-awg-h2"
              class="form-input"
              class:input-warning={isH2Invalid}
              aria-invalid={isH2Invalid}
              type="number"
              bind:value={np.awgH2}
            />
          </div>
          <div class="form-col">
            <label class="form-label" for="proxy-awg-h3">H3</label>
            <input
              id="proxy-awg-h3"
              class="form-input"
              class:input-warning={isH3Invalid}
              aria-invalid={isH3Invalid}
              type="number"
              bind:value={np.awgH3}
            />
          </div>
          <div class="form-col">
            <label class="form-label" for="proxy-awg-h4">H4</label>
            <input
              id="proxy-awg-h4"
              class="form-input"
              class:input-warning={isH4Invalid}
              aria-invalid={isH4Invalid}
              type="number"
              bind:value={np.awgH4}
            />
          </div>
        </div>

        <p class="form-hint" style="margin-top: 8px;">
          {$t('proxies.awg_hint')}
        </p>
      </div>
    {/if}
  {:else if np.type === 'socks5' || np.type === 'socks'}
    <div class="form-row">
      <label class="form-label" for="proxy-socks-username">{$t('proxies.username_optional')}</label>
      <input
        id="proxy-socks-username"
        class="form-input"
        bind:value={np.username}
        placeholder="username"
      />
    </div>
    <div class="form-row">
      <label class="form-label" for="proxy-socks-password">{$t('proxies.password_optional')}</label>
      <input
        id="proxy-socks-password"
        class="form-input"
        bind:value={np.password}
        placeholder="password"
      />
    </div>
  {:else if np.type === 'http'}
    <div class="form-row">
      <label class="form-label" for="proxy-http-username">{$t('proxies.username_optional')}</label>
      <input
        id="proxy-http-username"
        class="form-input"
        bind:value={np.username}
        placeholder="username"
      />
    </div>
    <div class="form-row">
      <label class="form-label" for="proxy-http-password">{$t('proxies.password_optional')}</label>
      <input
        id="proxy-http-password"
        class="form-input"
        bind:value={np.password}
        placeholder="password"
      />
    </div>
    <div class="form-row">
      <label
        class="toggle-label"
        style="display: flex; align-items: center; gap: 8px; cursor: pointer; user-select: none;"
      >
        <input type="checkbox" bind:checked={np.tls} />
        <span>TLS</span>
      </label>
    </div>
    {#if np.tls}
      <div class="form-row">
        <label
          class="toggle-label"
          style="display: flex; align-items: center; gap: 8px; cursor: pointer; user-select: none;"
        >
          <input type="checkbox" bind:checked={np.skipCertVerify} />
          <span>{$t('editor.skipCertVerify')}</span>
        </label>
      </div>
    {/if}
  {/if}

  <div class="form-actions">
    <button class="btn btn-secondary" onclick={onCancel}>{$t('app.cancel')}</button>
    <button class="btn btn-primary" onclick={onSave}
      >{isEdit ? $t('app.save') : $t('app.create')}</button
    >
  </div>
</div>

<style>
  .form-card {
    background: var(--bg-elevated);
    border: 1px solid var(--border-strong);
    border-radius: var(--radius);
    padding: 16px;
    display: flex;
    flex-direction: column;
    gap: 10px;
  }

  .form-row {
    display: flex;
    flex-direction: column;
    gap: 4px;
  }
  .form-row2 {
    display: flex;
    gap: 10px;
  }
  .form-col {
    display: flex;
    flex-direction: column;
    gap: 4px;
    flex: 1;
  }
  .form-col-sm {
    flex: 0 0 100px;
  }

  .form-label {
    font-size: 11px;
    color: var(--fg-dim);
    font-weight: 500;
    text-transform: none; /* Keep label lowercase/normal text as standard */
  }

  .form-input,
  .form-select {
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

  .form-input:focus,
  .form-select:focus {
    border-color: var(--primary);
  }

  .input-with-btn {
    display: flex;
    gap: 6px;
    align-items: center;
  }

  .input-with-btn .form-input {
    flex: 1;
  }

  .btn-gen {
    background: rgba(255, 255, 255, 0.05);
    border: 1px solid var(--border);
    color: var(--fg-secondary);
    border-radius: var(--radius-sm);
    padding: 6px 10px;
    cursor: pointer;
    font-size: 14px;
    transition: background var(--transition-fast);
    flex-shrink: 0;
  }

  .btn-gen:hover {
    background: rgba(255, 255, 255, 0.1);
    color: var(--fg-primary);
  }

  .toggle-label {
    display: flex;
    align-items: center;
    gap: 8px;
    cursor: pointer;
    font-size: 13px;
    color: var(--fg-primary);
  }

  .form-actions {
    display: flex;
    gap: 8px;
    justify-content: flex-end;
    margin-top: 4px;
  }

  .awg-options-block {
    display: flex;
    flex-direction: column;
    gap: 8px;
    padding: 12px;
    background: var(--bg-surface-raised, rgba(255, 255, 255, 0.03));
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    margin-bottom: 12px;
  }

  .awg-options-block.has-warning {
    border-color: color-mix(in srgb, var(--warning) 50%, var(--border));
  }

  .awg-headers-grid {
    display: grid;
    grid-template-columns: repeat(4, 1fr);
    gap: 8px;
  }

  @media (max-width: 480px) {
    .awg-headers-grid {
      grid-template-columns: repeat(2, 1fr);
    }
  }

  .awg-group-title {
    font-size: 11px;
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.04em;
    color: var(--fg-secondary);
  }

  .form-hint {
    font-size: 12px;
    color: var(--fg-secondary);
    line-height: 1.4;
    margin: 0;
  }
</style>
