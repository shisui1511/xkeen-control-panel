<script lang="ts">
  import Select from '../Select.svelte';
  import Icon from '../Icon.svelte';
  import { t } from '../../i18n';
  import { getMihomoContext } from './MihomoContext.svelte';
  import type { Listener } from '../../lib/mihomoYaml';

  let ctx = getMihomoContext();

  const CIPHERS = ['aes-256-gcm', 'aes-128-gcm', 'chacha20-poly1305', '2022-blake3-aes-256-gcm'];

  let showListenerForm = $state(false);
  let editingListenerId = $state<string | null>(null);

  function newListenerDefaults(): Listener {
    return {
      id: crypto.randomUUID(),
      name: '',
      type: 'mixed',
      listen: '0.0.0.0',
      port: '',
      udp: true,
      users: []
    };
  }

  let newListener = $state<Listener>(newListenerDefaults());

  let listenerNameValid = $derived(String(newListener.name || '').trim().length > 0);
  let listenerPortValid = $derived.by(() => {
    const raw = String(newListener.port ?? '').trim();
    if (!raw) return false;
    const n = Number(raw);
    return Number.isInteger(n) && n >= 1 && n <= 65535;
  });
  let listenerSSPasswordValid = $derived(
    newListener.type !== 'shadowsocks' || String(newListener.password || '').trim().length > 0
  );
  let listenerFormValid = $derived(
    listenerNameValid && listenerPortValid && listenerSSPasswordValid
  );

  function openListenerForm(l?: Listener) {
    if (l) {
      editingListenerId = l.id;
      newListener = {
        ...l,
        users: l.users ? l.users.map((u) => ({ ...u })) : []
      };
    } else {
      editingListenerId = null;
      newListener = newListenerDefaults();
    }
    showListenerForm = true;
  }

  function cancelListenerForm() {
    showListenerForm = false;
    editingListenerId = null;
  }

  function saveListener() {
    if (!listenerFormValid) return;
    const toSave: Listener = {
      ...newListener,
      name: newListener.name.trim(),
      listen: newListener.listen.trim() || '0.0.0.0',
      port: String(newListener.port).trim(),
      proxy: newListener.proxy?.trim() || undefined,
      users:
        newListener.type === 'mixed' || newListener.type === 'socks' || newListener.type === 'http'
          ? (newListener.users || []).filter((u) => u.username.trim() && u.password.trim())
          : undefined,
      cipher: newListener.type === 'shadowsocks' ? newListener.cipher || 'aes-256-gcm' : undefined,
      password: newListener.type === 'shadowsocks' ? newListener.password || '' : undefined,
      udp:
        newListener.type !== 'http' && newListener.type !== 'redirect'
          ? !!newListener.udp
          : undefined
    };

    if (editingListenerId) {
      ctx.updateListener(editingListenerId, toSave);
    } else {
      ctx.addListener(toSave);
    }
    showListenerForm = false;
    editingListenerId = null;
  }

  function addListenerUser() {
    newListener.users = [...(newListener.users || []), { username: '', password: '' }];
  }

  function removeListenerUser(index: number) {
    newListener.users = (newListener.users || []).filter((_, i) => i !== index);
  }
</script>

<div class="sec-body" data-testid="mihomo-section-listeners">
  {#if ctx.listenersReadOnly}
    <div class="alert alert-warning" role="status">
      <div style="font-weight: 600; margin-bottom: 4px;">
        {$t('mihomo.listener_readonly_title')}
      </div>
      <div style="font-size: 13px; margin-bottom: 8px;">
        {$t('mihomo.listener_readonly_body')}
      </div>
      <div class="safe-merge-tags">
        <span class="directive-tag"><code>listeners</code></span>
      </div>
    </div>
  {:else if showListenerForm}
    <div class="form-card">
      <div class="form-row">
        <label class="form-label" for="listener-name">{$t('mihomo.listener_name')}</label>
        <input
          id="listener-name"
          class="form-input"
          bind:value={newListener.name}
          placeholder="my-listener"
        />
        {#if !listenerNameValid}
          <span class="form-validation-msg">
            {$t('mihomo.listener_name_required')}
          </span>
        {/if}
      </div>

      <div class="form-row2">
        <div class="form-col">
          <label class="form-label" for="listener-type">{$t('mihomo.listener_type')}</label>
          <Select
            id="listener-type"
            class="form-select"
            bind:value={newListener.type}
            onchange={() => {
              if (newListener.type === 'shadowsocks' && !newListener.cipher) {
                newListener.cipher = 'aes-256-gcm';
              }
            }}
          >
            <option value="mixed">mixed</option>
            <option value="socks">socks</option>
            <option value="http">http</option>
            <option value="shadowsocks">shadowsocks</option>
            <option value="tproxy">tproxy</option>
            <option value="redirect">redirect</option>
          </Select>
        </div>

        <div class="form-col">
          <label class="form-label" for="listener-listen">{$t('mihomo.listener_listen')}</label>
          <input
            id="listener-listen"
            class="form-input"
            bind:value={newListener.listen}
            placeholder="0.0.0.0"
          />
        </div>

        <div class="form-col form-col-sm">
          <label class="form-label" for="listener-port">{$t('mihomo.listener_port')}</label>
          <input
            id="listener-port"
            type="number"
            min="1"
            max="65535"
            class="form-input"
            bind:value={newListener.port}
            placeholder="7890"
          />
          {#if !listenerPortValid}
            <span class="form-validation-msg">
              {$t('mihomo.listener_port_required')}
            </span>
          {/if}
        </div>
      </div>

      <div class="form-row">
        <label class="form-label" for="listener-destination"
          >{$t('mihomo.listener_destination')}</label
        >
        <Select
          id="listener-destination"
          class="form-select"
          value={newListener.proxy &&
          (newListener.proxy === 'DIRECT' ||
            newListener.proxy === 'REJECT' ||
            ctx.groups.some((g) => g.name === newListener.proxy) ||
            ctx.proxies.some((p) => p.name === newListener.proxy))
            ? newListener.proxy
            : ''}
          onchange={(e) => {
            newListener.proxy = e.currentTarget.value || undefined;
          }}
        >
          <option value="">{$t('mihomo.listener_dest_rules')}</option>
          <optgroup label={$t('mihomo.listener_dest_special')}>
            <option value="DIRECT">DIRECT</option>
            <option value="REJECT">REJECT</option>
          </optgroup>
          {#if ctx.groups.length > 0}
            <optgroup label={$t('mihomo.listener_dest_groups')}>
              {#each ctx.groups as g}
                <option value={g.name}>{g.name}</option>
              {/each}
            </optgroup>
          {/if}
          {#if ctx.proxies.length > 0}
            <optgroup label={$t('mihomo.listener_dest_nodes')}>
              {#each ctx.proxies as p}
                <option value={p.name}>{p.name}</option>
              {/each}
            </optgroup>
          {/if}
        </Select>
        <div class="form-hint">
          {$t('mihomo.listener_destination_hint')}
        </div>
      </div>

      {#if newListener.type !== 'http' && newListener.type !== 'redirect'}
        <div class="toggle-row" style="margin-top: 4px;">
          <label class="toggle-label">
            <input type="checkbox" bind:checked={newListener.udp} />
            <span>{$t('mihomo.listener_udp')}</span>
          </label>
        </div>
      {/if}

      {#if newListener.type === 'shadowsocks'}
        <div class="form-row">
          <label class="form-label" for="listener-cipher">{$t('mihomo.listener_cipher')}</label>
          <Select id="listener-cipher" class="form-select" bind:value={newListener.cipher}>
            {#each CIPHERS as c}
              <option value={c}>{c}</option>
            {/each}
          </Select>
        </div>
        <div class="form-row">
          <label class="form-label" for="listener-password">{$t('mihomo.listener_password')}</label>
          <input
            id="listener-password"
            type="password"
            class="form-input"
            bind:value={newListener.password}
          />
          {#if !listenerSSPasswordValid}
            <span class="form-validation-msg">
              {$t('mihomo.listener_ss_password_required')}
            </span>
          {/if}
        </div>
      {/if}

      {#if newListener.type === 'mixed' || newListener.type === 'socks' || newListener.type === 'http'}
        <div class="form-row" style="margin-top: 6px;">
          <div class="form-users-header">
            <span class="form-label">{$t('mihomo.listener_users')}</span>
            <button
              type="button"
              class="btn btn-secondary btn-sm form-users-add-btn"
              onclick={addListenerUser}
            >
              + {$t('mihomo.listener_add_user')}
            </button>
          </div>
          {#if newListener.users && newListener.users.length > 0}
            <div class="form-users-list">
              {#each newListener.users as user, uIdx}
                <div class="form-user-row">
                  <input
                    type="text"
                    class="form-input"
                    placeholder={$t('mihomo.listener_username')}
                    aria-label={$t('mihomo.listener_username')}
                    bind:value={user.username}
                  />
                  <input
                    type="password"
                    class="form-input"
                    placeholder={$t('mihomo.listener_password')}
                    aria-label={$t('mihomo.listener_password')}
                    bind:value={user.password}
                  />
                  <button
                    type="button"
                    class="item-del"
                    aria-label={$t('app.delete')}
                    title={$t('app.delete')}
                    onclick={() => removeListenerUser(uIdx)}
                  >
                    <Icon name="close" size={12} />
                  </button>
                </div>
              {/each}
            </div>
          {/if}
          <div class="form-hint">
            {$t('mihomo.listener_open_proxy_hint')}
          </div>
        </div>
      {/if}

      <div class="form-actions form-actions-spaced">
        <button type="button" class="btn btn-secondary" onclick={cancelListenerForm}>
          {$t('app.cancel')}
        </button>
        <button
          type="button"
          class="btn btn-primary"
          disabled={!listenerFormValid}
          onclick={saveListener}
        >
          {editingListenerId ? $t('app.save') : $t('app.add')}
        </button>
      </div>
    </div>
  {:else if ctx.listeners.length === 0}
    <div class="rulesets-hint form-hint-spaced">
      {$t('mihomo.listeners_hint')}
    </div>
    <button type="button" class="add-btn btn-secondary" onclick={() => openListenerForm()}>
      + {$t('mihomo.add_listener')}
    </button>
  {:else}
    {#each ctx.listeners as l (l.id)}
      <div class="item-row">
        <span class="item-badge type-{l.type}">{l.type}</span>
        <span class="item-name" title={l.name}>{l.name}</span>
        <span class="item-meta">{l.listen}:{l.port}</span>
        <span
          class="item-meta item-dest-meta"
          title={l.proxy ? `→ ${l.proxy}` : $t('mihomo.listener_dest_rules')}
        >
          {l.proxy ? `→ ${l.proxy}` : $t('mihomo.listener_dest_rules')}
        </span>
        <div class="item-actions">
          <button
            type="button"
            class="item-edit"
            aria-label={$t('app.edit')}
            title={$t('app.edit')}
            onclick={() => openListenerForm(l)}
          >
            <Icon name="edit" />
          </button>
          <button
            type="button"
            class="item-del"
            aria-label={$t('app.delete')}
            title={$t('app.delete')}
            onclick={() => ctx.removeListener(l.id)}
          >
            <Icon name="close" size={12} />
          </button>
        </div>
      </div>
    {/each}
    <div style="margin-top: 12px;">
      <button type="button" class="add-btn btn-secondary" onclick={() => openListenerForm()}>
        + {$t('mihomo.add_listener')}
      </button>
    </div>
  {/if}
</div>

<style>
  .sec-body {
    padding: 16px;
  }

  .form-card {
    background: var(--bg-elevated);
    border: 1px solid var(--border-strong);
    border-radius: var(--radius);
    padding: 16px;
    display: flex;
    flex-direction: column;
    gap: 10px;
    margin-bottom: 16px;
  }

  .form-row {
    display: flex;
    flex-direction: column;
    gap: 4px;
  }

  .form-row2 {
    display: flex;
    gap: 12px;
  }

  .form-col {
    flex: 1;
    display: flex;
    flex-direction: column;
    gap: 4px;
  }

  .form-col-sm {
    flex: 0 0 120px;
  }

  .form-label {
    font-size: var(--font-size-xs);
    font-weight: 500;
    color: var(--fg-secondary);
  }

  .form-input {
    width: 100%;
    box-sizing: border-box;
    background: var(--bg-card);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    padding: 8px;
    color: var(--fg-primary);
    font-size: 12px;
  }

  .form-validation-msg {
    color: var(--danger);
    font-size: var(--font-size-xs);
  }

  .form-hint {
    font-size: var(--font-size-xs);
    color: var(--fg-dim);
  }

  .form-hint-spaced {
    margin-bottom: 12px;
    font-size: 13px;
    color: var(--fg-dim);
  }

  .form-users-header {
    display: flex;
    justify-content: space-between;
    align-items: center;
    margin-bottom: 6px;
  }

  .form-users-list {
    display: flex;
    flex-direction: column;
    gap: 6px;
    margin-bottom: 6px;
  }

  .form-user-row {
    display: flex;
    gap: 8px;
    align-items: center;
  }

  .form-actions {
    display: flex;
    justify-content: flex-end;
    gap: 8px;
  }

  .form-actions-spaced {
    margin-top: 12px;
    border-top: 1px solid var(--border);
    padding-top: 12px;
  }

  .toggle-row {
    margin-bottom: 8px;
  }

  .toggle-label {
    display: flex;
    align-items: center;
    gap: 8px;
    font-size: 13px;
    cursor: pointer;
    user-select: none;
  }

  .item-row {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 8px 12px;
    background: var(--bg-card);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    margin-bottom: 6px;
  }

  .item-badge {
    font-size: var(--font-size-xs);
    padding: 2px 6px;
    border-radius: 4px;
    font-weight: 500;
    text-transform: uppercase;
    flex-shrink: 0;
  }

  .type-mixed {
    background: color-mix(in srgb, var(--primary) 15%, transparent);
    color: var(--primary);
  }
  .type-socks {
    background: color-mix(in srgb, var(--success) 15%, transparent);
    color: var(--success);
  }
  .type-http {
    background: color-mix(in srgb, var(--seq-2) 15%, transparent);
    color: var(--seq-2);
  }
  .type-shadowsocks {
    background: color-mix(in srgb, var(--danger) 15%, transparent);
    color: var(--danger);
  }
  .type-tproxy {
    background: color-mix(in srgb, var(--warning) 15%, transparent);
    color: var(--warning);
  }
  .type-redirect {
    background: color-mix(in srgb, var(--warning) 15%, transparent);
    color: var(--warning);
  }

  .item-name {
    flex: 1;
    min-width: 50px;
    font-size: 13px;
    font-weight: 500;
    color: var(--fg-primary);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .item-meta {
    font-size: var(--font-size-xs);
    color: var(--fg-dim);
    flex-shrink: 0;
  }

  .item-dest-meta {
    max-width: 220px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .item-actions {
    display: flex;
    align-items: center;
    gap: 4px;
    margin-left: auto;
  }

  .item-edit,
  .item-del {
    background: none;
    border: none;
    color: var(--fg-faint);
    cursor: pointer;
    font-size: var(--font-size-xs);
    padding: 2px 4px;
    border-radius: var(--radius-sm);
    transition: color var(--transition-fast);
    flex-shrink: 0;
    line-height: 1;
    display: inline-flex;
    align-items: center;
  }

  .item-edit:hover {
    color: var(--primary);
  }

  .item-del:hover {
    color: var(--danger);
  }

  .add-btn {
    font-size: 12px;
    padding: 6px 12px;
    border-radius: var(--radius-sm);
    cursor: pointer;
    transition: all var(--transition-fast);
  }

  .btn-secondary {
    background: var(--bg-card);
    border: 1px solid var(--border);
    color: var(--fg-primary);
  }

  .btn-secondary:hover {
    background: var(--bg-hover);
  }

  .alert {
    padding: 10px 12px;
    border-radius: var(--radius-sm);
    margin-bottom: 12px;
  }

  .alert-warning {
    background: color-mix(in srgb, var(--warning) 10%, transparent);
    border: 1px solid color-mix(in srgb, var(--warning) 30%, transparent);
    color: var(--warning);
  }

  .directive-tag {
    background: var(--bg-surface);
    padding: 2px 6px;
    border-radius: 4px;
    font-size: var(--font-size-xs);
  }
</style>
