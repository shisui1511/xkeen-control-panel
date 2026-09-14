<script lang="ts">
  import { t } from '../../i18n';
  import Select from '../Select.svelte';
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
        <span class="badge type-{inbound.protocol}">{inbound.protocol}</span>
        <strong>{inbound.tag}</strong>
        <button
          type="button"
          class="item-del"
          style="margin-left:auto"
          onclick={() => removeInbound(inbound.tag)}
          title={$t('app.delete')}
        >
          ✕
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
    gap: 12px;
  }

  .section-title {
    font-size: 0.9375rem;
    font-weight: 600;
    color: var(--fg-primary);
  }

  .inbound-card {
    padding: 12px;
  }

  .inbound-title {
    display: flex;
    align-items: center;
    gap: 8px;
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
    color: var(--danger);
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
    border: solid var(--color-on-primary);
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
</style>
