<script lang="ts">
  import { t } from '../../i18n';
  import Select from '../Select.svelte';
  import Icon from '../Icon.svelte';
  import type { XrayInbound } from './XrayContext.svelte';

  let {
    inbounds = $bindable([]),
    onchange
  }: {
    inbounds: XrayInbound[];
    onchange?: () => void;
  } = $props();

  let showInboundForm = $state(false);
  let newInbound = $state({
    tag: '',
    port: 10808,
    protocol: 'socks',
    udp: true
  });

  function addInbound() {
    if (!newInbound.tag.trim()) return;
    const item: XrayInbound = {
      tag: newInbound.tag.trim(),
      port: Number(newInbound.port) || 10808,
      protocol: newInbound.protocol,
      settings: newInbound.protocol === 'socks' ? { udp: newInbound.udp } : {}
    };
    inbounds.push(item);
    newInbound = { tag: '', port: 10808, protocol: 'socks', udp: true };
    showInboundForm = false;
    onchange?.();
  }

  function removeInbound(tag: string) {
    const idx = inbounds.findIndex((i) => i.tag === tag);
    if (idx !== -1) {
      inbounds.splice(idx, 1);
      onchange?.();
    }
  }

  function markChanged() {
    onchange?.();
  }
</script>

<div class="sec-body">
  <div class="section-title">{$t('editor.xray_inbounds')}</div>
  {#each inbounds as inbound (inbound.tag)}
    <div class="card inbound-card">
      <div class="inbound-title">
        <span class="badge badge--tag type-{inbound.protocol}">{inbound.protocol}</span>
        <strong>{inbound.tag}</strong>
        <button
          type="button"
          class="item-del"
          style="margin-left:auto"
          onclick={() => removeInbound(inbound.tag)}
          title={$t('app.delete')}
        >
          <Icon name="close" size={12} />
        </button>
      </div>
      <div class="form-row2" style="margin-top:var(--spacing-2, 8px)">
        <div class="form-col">
          <label class="form-label" for="xray-inbound-port-{inbound.tag}"
            >{$t('xray.inbound_port')}</label
          >
          <input
            id="xray-inbound-port-{inbound.tag}"
            class="form-input"
            type="number"
            bind:value={inbound.port}
            oninput={markChanged}
            min="1"
            max="65535"
          />
        </div>
        <div class="form-col">
          <label class="form-label" for="xray-inbound-listen-{inbound.tag}"
            >{$t('xray.listen_address')}</label
          >
          <input
            id="xray-inbound-listen-{inbound.tag}"
            class="form-input"
            bind:value={inbound.listen}
            oninput={markChanged}
          />
        </div>
      </div>
    </div>
  {/each}

  {#if showInboundForm}
    <div class="form-card card">
      <div class="form-row">
        <label class="form-label" for="xray-new-inbound-tag">{$t('xray.tag')}</label>
        <input
          id="xray-new-inbound-tag"
          class="form-input"
          bind:value={newInbound.tag}
          placeholder="socks-in"
        />
      </div>
      <div class="form-row2">
        <div class="form-col">
          <label class="form-label" for="xray-new-inbound-port">{$t('xray.port')}</label>
          <input
            id="xray-new-inbound-port"
            class="form-input"
            type="number"
            bind:value={newInbound.port}
            min="1"
            max="65535"
          />
        </div>
        <div class="form-col">
          <label class="form-label" for="xray-new-inbound-protocol">{$t('xray.protocol')}</label>
          <Select
            id="xray-new-inbound-protocol"
            class="form-select"
            bind:value={newInbound.protocol}
          >
            <option value="socks">socks</option>
            <option value="http">http</option>
          </Select>
        </div>
      </div>
      {#if newInbound.protocol === 'socks'}
        <div class="form-row">
          <label class="checkbox-container">
            <input type="checkbox" bind:checked={newInbound.udp} />
            <span class="checkmark"></span>
            {$t('xray.enable_udp_socks')}
          </label>
        </div>
      {/if}
      <div class="form-actions">
        <button type="button" class="btn btn-secondary" onclick={() => (showInboundForm = false)}>
          {$t('app.cancel')}
        </button>
        <button type="button" class="btn btn-primary" onclick={addInbound}>
          {$t('app.create')}
        </button>
      </div>
    </div>
  {:else}
    <button type="button" class="add-btn" onclick={() => (showInboundForm = true)}>
      + {$t('xray.add_inbound')}
    </button>
  {/if}
</div>

<style>
  .sec-body {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-3);
  }

  .section-title {
    font-size: var(--font-size-lg);
    font-weight: 600;
    color: var(--fg-primary);
  }

  .inbound-card {
    padding: var(--spacing-3);
  }

  .inbound-title {
    display: flex;
    align-items: center;
    gap: var(--spacing-2);
  }

  .item-del {
    background: none;
    border: none;
    cursor: pointer;
    color: var(--fg-secondary);
    font-size: var(--font-size-base);
    padding: var(--spacing-1) var(--spacing-1-5);
    border-radius: var(--radius-xs);
    transition:
      color var(--transition-fast),
      background var(--transition-fast);
    display: inline-flex;
    align-items: center;
  }

  .item-del:hover {
    color: var(--danger);
    background: var(--bg-surface-active);
  }

  .form-card {
    padding: var(--spacing-4);
    display: flex;
    flex-direction: column;
    gap: var(--spacing-3);
  }

  .form-row2 {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: var(--spacing-3);
  }

  @media (max-width: 768px) {
    .form-row2 {
      grid-template-columns: 1fr;
    }
  }

  .form-col {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-1);
  }

  .form-row {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-1);
  }

  .form-label {
    font-size: var(--font-size-xs);
    font-weight: 500;
    color: var(--fg-secondary);
  }

  .form-input {
    background: var(--bg-card);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    padding: var(--spacing-1-5) var(--spacing-2-5);
    font-size: var(--font-size-sm);
    color: var(--fg-primary);
  }

  .form-input:focus {
    outline: none;
    border-color: var(--accent);
  }

  :global(.form-select) {
    background: var(--bg-card);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    padding: var(--spacing-1-5) var(--spacing-2-5);
    font-size: var(--font-size-sm);
    color: var(--fg-primary);
  }

  .checkbox-container {
    display: flex;
    align-items: center;
    gap: var(--spacing-2);
    font-size: var(--font-size-sm);
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
    background-color: var(--bg-card);
    border: 1px solid var(--border);
    border-radius: var(--radius-xs);
    display: inline-block;
    position: relative;
  }

  .checkbox-container input:checked ~ .checkmark {
    background-color: var(--accent);
    border-color: var(--accent);
  }

  .checkbox-container input:checked ~ .checkmark:after {
    content: '';
    position: absolute;
    left: 5px;
    top: 2px;
    width: 4px;
    height: 8px;
    border: solid var(--btn-primary-text);
    border-width: 0 2px 2px 0;
    transform: rotate(45deg);
  }

  .form-actions {
    display: flex;
    justify-content: flex-end;
    gap: var(--spacing-2);
    margin-top: var(--spacing-1);
  }

  .add-btn {
    background: var(--bg-card);
    border: 1px dashed var(--border);
    border-radius: var(--radius);
    padding: var(--spacing-2-5) var(--spacing-4);
    color: var(--fg-secondary);
    font-size: var(--font-size-sm);
    font-weight: 500;
    cursor: pointer;
    text-align: center;
    transition:
      color var(--transition-fast),
      background var(--transition-fast),
      border-color var(--transition-fast);
  }

  .add-btn:hover {
    border-color: var(--accent);
    color: var(--accent);
    background: var(--hover);
  }
</style>
