<script lang="ts">
  import { currentLang, t } from '../../i18n';

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

  const ru = $derived($currentLang === 'ru');

  const PROXY_TYPES = ['vless', 'hysteria2', 'tuic', 'ss', 'vmess', 'trojan', 'socks', 'http'];
  const CIPHERS = ['aes-256-gcm', 'aes-128-gcm', 'chacha20-poly1305', '2022-blake3-aes-256-gcm'];
</script>

<div class="form-card">
  <div class="form-row">
    <label class="form-label" for="proxy-type">{ru ? 'Тип' : 'Type'}</label>
    <select id="proxy-type" class="form-select" bind:value={np.type}>
      {#each PROXY_TYPES as pt}<option value={pt}>{pt}</option>{/each}
    </select>
  </div>
  <div class="form-row">
    <label class="form-label" for="proxy-name">{ru ? 'Имя' : 'Name'}</label>
    <input id="proxy-name" class="form-input" bind:value={np.name} placeholder="my-proxy" />
  </div>
  <div class="form-row2">
    <div class="form-col">
      <label class="form-label" for="proxy-server">{ru ? 'Сервер' : 'Server'}</label>
      <input id="proxy-server" class="form-input" bind:value={np.server} placeholder="example.com" />
    </div>
    <div class="form-col form-col-sm">
      <label class="form-label" for="proxy-port">{ru ? 'Порт' : 'Port'}</label>
      <input id="proxy-port" class="form-input" type="number" bind:value={np.port} min="1" max="65535" />
    </div>
  </div>

  {#if np.type === 'vless'}
    <div class="form-row">
      <label class="form-label" for="proxy-vless-uuid">UUID</label>
      <div class="input-with-btn">
        <input id="proxy-vless-uuid" class="form-input" bind:value={np.uuid} placeholder="uuid" />
        <button class="btn-gen" onclick={() => (np.uuid = crypto.randomUUID())} title="Generate"
          >⟳</button
        >
      </div>
    </div>
    <div class="form-row">
      <label class="form-label" for="proxy-vless-public-key">Reality Public Key</label>
      <input id="proxy-vless-public-key" class="form-input" bind:value={np.publicKey} placeholder="public-key" />
    </div>
    <div class="form-row2">
      <div class="form-col">
        <label class="form-label" for="proxy-vless-short-id">Short ID</label>
        <input id="proxy-vless-short-id" class="form-input" bind:value={np.shortId} placeholder="short-id" />
      </div>
      <div class="form-col">
        <label class="form-label" for="proxy-vless-sni">SNI</label>
        <input id="proxy-vless-sni" class="form-input" bind:value={np.servername} placeholder="www.apple.com" />
      </div>
    </div>
  {:else if np.type === 'hysteria2'}
    <div class="form-row">
      <label class="form-label" for="proxy-hysteria2-password">{ru ? 'Пароль' : 'Password'}</label>
      <input id="proxy-hysteria2-password" class="form-input" bind:value={np.password} placeholder="password" />
    </div>
    <div class="form-row">
      <label class="form-label" for="proxy-hysteria2-sni">SNI</label>
      <input id="proxy-hysteria2-sni" class="form-input" bind:value={np.sni} placeholder="example.com" />
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
        <label class="form-label" for="proxy-hysteria2-obfs-password">{$t('editor.obfsPassword')}</label>
        <input id="proxy-hysteria2-obfs-password" class="form-input" bind:value={np.obfsPassword} placeholder="obfs password" />
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
      <label class="form-label" for="proxy-tuic-password">{ru ? 'Пароль' : 'Password'}</label>
      <input id="proxy-tuic-password" class="form-input" bind:value={np.password} placeholder="password" />
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
      <label class="form-label" for="proxy-ss-password">{ru ? 'Пароль' : 'Password'}</label>
      <input id="proxy-ss-password" class="form-input" bind:value={np.password} placeholder="password" />
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
        <input id="proxy-vmess-sni" class="form-input" bind:value={np.sni} placeholder="example.com" />
      </div>
    {/if}
  {:else if np.type === 'trojan'}
    <div class="form-row">
      <label class="form-label" for="proxy-trojan-password">{ru ? 'Пароль' : 'Password'}</label>
      <input id="proxy-trojan-password" class="form-input" bind:value={np.password} placeholder="password" />
    </div>
    <div class="form-row">
      <label class="form-label" for="proxy-trojan-sni">SNI</label>
      <input id="proxy-trojan-sni" class="form-input" bind:value={np.sni} placeholder="example.com" />
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
        <input id="proxy-trojan-ws-path" class="form-input" bind:value={np.wsPath} placeholder="/" />
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
  {:else if np.type === 'socks'}
    <div class="form-row">
      <label class="form-label" for="proxy-socks-username"
        >{ru ? 'Имя пользователя (опционально)' : 'Username (optional)'}</label
      >
      <input id="proxy-socks-username" class="form-input" bind:value={np.username} placeholder="username" />
    </div>
    <div class="form-row">
      <label class="form-label" for="proxy-socks-password">{ru ? 'Пароль (опционально)' : 'Password (optional)'}</label>
      <input id="proxy-socks-password" class="form-input" bind:value={np.password} placeholder="password" />
    </div>
  {:else if np.type === 'http'}
    <div class="form-row">
      <label class="form-label" for="proxy-http-username"
        >{ru ? 'Имя пользователя (опционально)' : 'Username (optional)'}</label
      >
      <input id="proxy-http-username" class="form-input" bind:value={np.username} placeholder="username" />
    </div>
    <div class="form-row">
      <label class="form-label" for="proxy-http-password">{ru ? 'Пароль (опционально)' : 'Password (optional)'}</label>
      <input id="proxy-http-password" class="form-input" bind:value={np.password} placeholder="password" />
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
    <button class="btn btn-secondary" onclick={onCancel}>{ru ? 'Отмена' : 'Cancel'}</button>
    <button class="btn btn-primary" onclick={onSave}
      >{isEdit ? (ru ? 'Сохранить' : 'Save') : ru ? 'Добавить' : 'Add'}</button
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
</style>
