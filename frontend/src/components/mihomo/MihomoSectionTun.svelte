<script lang="ts">
  import Select from '../Select.svelte';
  import { t } from '../../i18n';
  import { getMihomoContext } from './MihomoContext.svelte';

  let ctx = getMihomoContext();
</script>

<div class="sec-body" data-testid="mihomo-section-tun">
  <div class="toggle-row">
    <label class="toggle-label">
      <input type="checkbox" bind:checked={ctx.tun.enabled} onchange={() => ctx.markDirty()} />
      <span>{$t('mihomo.enable_tun')}</span>
    </label>
  </div>

  {#if ctx.tun.enabled}
    <div class="form-row">
      <label class="form-label" for="mihomo-tun-stack">Stack</label>
      <Select
        id="mihomo-tun-stack"
        class="form-select"
        bind:value={ctx.tun.stack}
        onchange={() => ctx.markDirty()}
      >
        <option value="system">system</option>
        <option value="gvisor">gvisor</option>
        <option value="mixed">mixed</option>
      </Select>
    </div>
    <div class="toggle-row">
      <label class="toggle-label">
        <input type="checkbox" bind:checked={ctx.tun.autoRoute} onchange={() => ctx.markDirty()} />
        <span>Auto route</span>
      </label>
    </div>
    <div class="toggle-row">
      <label class="toggle-label">
        <input
          type="checkbox"
          bind:checked={ctx.tun.autoDetectInterface}
          onchange={() => ctx.markDirty()}
        />
        <span>Auto detect interface</span>
      </label>
    </div>
    <div class="form-row">
      <label class="form-label" for="mihomo-tun-dns-hijack">DNS Hijack</label>
      <input
        id="mihomo-tun-dns-hijack"
        class="form-input"
        value={ctx.tun.dnsHijack.join(', ')}
        onchange={(e) => {
          ctx.tun.dnsHijack = e.currentTarget.value
            .split(',')
            .map((s) => s.trim())
            .filter(Boolean);
          ctx.markDirty();
        }}
      />
    </div>
  {/if}

  <div
    class="toggle-row"
    style="margin-top: 16px; border-top: 1px solid var(--border); padding-top: 16px;"
  >
    <label class="toggle-label">
      <input type="checkbox" bind:checked={ctx.sniffer.enabled} onchange={() => ctx.markDirty()} />
      <span>{$t('editor.sniffer_enable')}</span>
    </label>
  </div>

  {#if ctx.sniffer.enabled}
    <div
      style="margin-left: 20px; display: flex; flex-direction: column; gap: 8px; margin-top: 8px;"
    >
      <label
        class="checkbox-container"
        style="display: flex; align-items: center; gap: 8px; font-size: 13px; cursor: pointer; user-select: none;"
      >
        <input
          type="checkbox"
          bind:checked={ctx.sniffer.sniffHttp}
          onchange={() => ctx.markDirty()}
          style="width: auto; margin: 0;"
        />
        <span>Sniff HTTP (ports 80, 8080)</span>
      </label>
      <label
        class="checkbox-container"
        style="display: flex; align-items: center; gap: 8px; font-size: 13px; cursor: pointer; user-select: none;"
      >
        <input
          type="checkbox"
          bind:checked={ctx.sniffer.sniffTls}
          onchange={() => ctx.markDirty()}
          style="width: auto; margin: 0;"
        />
        <span>Sniff TLS (ports 443, 8443)</span>
      </label>
      <label
        class="checkbox-container"
        style="display: flex; align-items: center; gap: 8px; font-size: 13px; cursor: pointer; user-select: none;"
      >
        <input
          type="checkbox"
          bind:checked={ctx.sniffer.sniffQuic}
          onchange={() => ctx.markDirty()}
          style="width: auto; margin: 0;"
        />
        <span>Sniff QUIC (ports 443, 8443)</span>
      </label>
    </div>
  {/if}

  <div
    class="form-row"
    style="margin-top: 16px; border-top: 1px solid var(--border); padding-top: 16px;"
  >
    <label class="form-label" for="mihomo-ctrl-type">{$t('mihomo.controller_type')}</label>
    <Select
      id="mihomo-ctrl-type"
      class="form-select"
      bind:value={ctx.externalControllerType}
      onchange={() => ctx.markDirty()}
    >
      <option value="unix">{$t('mihomo.controller_unix_label')}</option>
      <option value="tcp">{$t('mihomo.controller_tcp_label')}</option>
    </Select>
  </div>

  {#if ctx.externalControllerType === 'tcp'}
    <div class="form-row">
      <label class="form-label" for="mihomo-ctrl-target"
        >{$t('mihomo.controller_tcp_address')}</label
      >
      <input
        id="mihomo-ctrl-target"
        class="form-input"
        bind:value={ctx.externalControllerTarget}
        placeholder="127.0.0.1:9090"
        oninput={() => ctx.markDirty()}
      />
    </div>
    {#if ctx.externalControllerTarget.startsWith('0.0.0.0:') || ctx.externalControllerTarget.startsWith(':') || ctx.externalControllerTarget === '0.0.0.0'}
      <div
        class="inline-warning"
        style="margin-top: 6px; font-size: 12px; color: var(--warning); display: flex; align-items: center; gap: 6px;"
      >
        <svg
          width="14"
          height="14"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          stroke-width="2"
        >
          <path
            d="M10.29 3.86L1.82 18a2 2 0 0 0 1.71 3h16.94a2 2 0 0 0 1.71-3L13.71 3.86a2 2 0 0 0-3.42 0z"
          />
          <line x1="12" y1="9" x2="12" y2="13" />
          <line x1="12" y1="17" x2="12.01" y2="17" />
        </svg>
        <span>{$t('mihomo.insecure_lan_warning')}</span>
      </div>
    {/if}
  {/if}
</div>

<style>
  .sec-body {
    padding: 16px;
  }

  .toggle-row {
    margin-bottom: 12px;
  }

  .toggle-label {
    display: flex;
    align-items: center;
    gap: 8px;
    font-size: 13px;
    cursor: pointer;
    user-select: none;
    font-weight: 500;
  }

  .form-row {
    margin-bottom: 12px;
    display: flex;
    flex-direction: column;
    gap: 4px;
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
</style>
