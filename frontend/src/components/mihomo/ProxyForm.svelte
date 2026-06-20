<script lang="ts">
  import { currentLang, t } from '../../i18n';

  let {
    np = $bindable(),
    onSave,
    onCancel
  }: {
    np: any;
    onSave: () => void;
    onCancel: () => void;
  } = $props();

  const ru = $derived($currentLang === 'ru');

  const PROXY_TYPES = ['vless', 'hysteria2', 'tuic', 'ss', 'vmess'];
  const CIPHERS = ['aes-256-gcm', 'aes-128-gcm', 'chacha20-poly1305', '2022-blake3-aes-256-gcm'];
</script>

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
      <input
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
      <label class="form-label">UUID</label>
      <div class="input-with-btn">
        <input class="form-input" bind:value={np.uuid} placeholder="uuid" />
        <button
          class="btn-gen"
          onclick={() => (np.uuid = crypto.randomUUID())}
          title="Generate">⟳</button
        >
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
        <input
          class="form-input"
          bind:value={np.servername}
          placeholder="www.apple.com"
        />
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
    <div class="form-row">
      <label class="form-label">{$t('editor.obfsType')}</label>
      <select class="form-select" bind:value={np.obfsType}>
        <option value="none">{$t('editor.none')}</option>
        <option value="simple">{$t('editor.simple')}</option>
      </select>
    </div>
    {#if np.obfsType === 'simple'}
      <div class="form-row">
        <label class="form-label">{$t('editor.obfsPassword')}</label>
        <input class="form-input" bind:value={np.obfsPassword} placeholder="obfs password" />
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
      <label class="form-label">UUID</label>
      <div class="input-with-btn">
        <input class="form-input" bind:value={np.uuid} placeholder="uuid" />
        <button
          class="btn-gen"
          onclick={() => (np.uuid = crypto.randomUUID())}
          title="Generate">⟳</button
        >
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
        <button
          class="btn-gen"
          onclick={() => (np.uuid = crypto.randomUUID())}
          title="Generate">⟳</button
        >
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
    <button class="btn btn-secondary" onclick={onCancel}
      >{ru ? 'Отмена' : 'Cancel'}</button
    >
    <button class="btn btn-primary" onclick={onSave}
      >{ru ? 'Добавить' : 'Add'}</button
    >
  </div>
</div>
