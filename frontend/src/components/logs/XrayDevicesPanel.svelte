<script lang="ts">
  import { onMount } from 'svelte';
  import { t } from '../../i18n';
  import { apiFetch, apiFetchJSON } from '../../lib/api';
  import { usePoller } from '../../lib/poller';
  import { activateRestartGrace } from '../../lib/serviceGrace';
  import { showConfirm, showToast } from '../../stores';
  import Button from '../Button.svelte';
  import Select from '../Select.svelte';
  import EmptyState from '../EmptyState.svelte';

  interface Status {
    config_file?: string;
    access_path: string;
    enabled: boolean;
    file_size: number;
  }

  interface Entry {
    time: string;
    source_ip: string;
    device?: string;
    status: string;
    network?: string;
    destination?: string;
    port?: string;
    inbound?: string;
    outbound?: string;
    reason?: string;
  }

  interface Device {
    ip: string;
    device?: string;
    connections: number;
    rejected: number;
    last_seen: string;
    top_destinations: string[];
  }

  interface Response {
    status: Status;
    report: { entries: Entry[]; devices: Device[]; parsed: number };
  }

  const REFRESH_MS = 5000;
  const SEARCH_DELAY_MS = 300;

  let data = $state<Response | null>(null);
  let loadError = $state('');
  let toggling = $state(false);
  let selectedIP = $state('');
  let outbound = $state('');
  let dest = $state('');
  let destInput = $state('');

  let searchTimer: ReturnType<typeof setTimeout> | undefined;

  const outbounds = $derived(
    [...(data?.report.entries ?? []).map((e) => e.outbound ?? ''), outbound]
      .filter((o, i, all) => o && all.indexOf(o) === i)
      .sort()
  );

  const outboundOptions = $derived([
    { value: '', label: $t('xlog.all_outbounds') },
    ...outbounds.map((o) => ({ value: o, label: o }))
  ]);

  async function load() {
    try {
      const params = new URLSearchParams({ ip: selectedIP, dest, outbound, limit: '300' });
      data = await apiFetchJSON<Response>(`/api/xray/access-log?${params}`);
      loadError = '';
    } catch (e: any) {
      if (e?.status === 401) return;
      loadError = e?.message || String(e);
    }
  }

  function selectDevice(ip: string) {
    selectedIP = selectedIP === ip ? '' : ip;
    load();
  }

  function onDestInput(e: Event) {
    destInput = (e.currentTarget as HTMLInputElement).value;
    clearTimeout(searchTimer);
    searchTimer = setTimeout(() => {
      dest = destInput.trim();
      load();
    }, SEARCH_DELAY_MS);
  }

  async function toggle(enabled: boolean) {
    if (enabled) {
      const ok = await showConfirm({
        title: $t('xlog.enable_confirm_title'),
        message: $t('xlog.enable_confirm_msg'),
        confirmLabel: $t('xlog.enable'),
        cancelLabel: $t('app.cancel'),
        variant: 'warning'
      });
      if (!ok) return;
    }
    toggling = true;
    try {
      const res = await apiFetch('/api/xray/access-log/toggle', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ enabled })
      });
      const payload = await res.json().catch(() => null);
      if (!res.ok || !payload?.success) throw new Error(payload?.error || `HTTP ${res.status}`);
      if (payload.data.restart_required) {
        activateRestartGrace(6000);
        const r = await apiFetch('/api/service/control?action=restart', { method: 'POST' });
        if (!r.ok) throw new Error(await r.text());
      }
      showToast('success', enabled ? $t('xlog.enabled_toast') : $t('xlog.disabled_toast'));
      await load();
    } catch (e: any) {
      if (e?.status === 401) return;
      showToast('error', `${$t('xlog.toggle_error')}: ${e?.message || e}`);
    } finally {
      toggling = false;
    }
  }

  function deviceLabel(d: { device?: string; ip?: string; source_ip?: string }): string {
    return d.device || d.ip || d.source_ip || '';
  }

  onMount(() => {
    const poller = usePoller(() => load(), REFRESH_MS);
    return () => {
      poller.stop();
      clearTimeout(searchTimer);
    };
  });
</script>

<div class="xdev">
  {#if loadError}
    <p class="xdev-error" role="alert">{$t('xlog.load_error')}: {loadError}</p>
  {:else if data && !data.status.enabled}
    <div class="card xdev-off">
      <EmptyState title={$t('xlog.off_title')} description={$t('xlog.off_desc')} />
      <Button variant="primary" loading={toggling} onclick={() => toggle(true)}>
        {$t('xlog.enable')}
      </Button>
    </div>
  {:else if data}
    <div class="xdev-bar">
      <span class="xdev-meta font-mono" title={data.status.config_file}>
        {data.status.access_path} · {(data.status.file_size / 1024).toFixed(0)} KB
      </span>
      <Button variant="secondary" class="btn-sm" loading={toggling} onclick={() => toggle(false)}>
        {$t('xlog.disable')}
      </Button>
    </div>

    {#if data.report.devices.length === 0}
      <EmptyState title={$t('xlog.empty_title')} description={$t('xlog.empty_desc')} />
    {:else}
      <div class="xdev-grid">
        <ul class="xdev-devices" aria-label={$t('xlog.devices')}>
          {#each data.report.devices as d (d.ip)}
            <li>
              <button
                type="button"
                class="xdev-device"
                class:active={selectedIP === d.ip}
                aria-pressed={selectedIP === d.ip}
                onclick={() => selectDevice(d.ip)}
              >
                <span class="xdev-name">{deviceLabel(d)}</span>
                {#if d.device}<span class="xdev-ip font-mono">{d.ip}</span>{/if}
                <span class="xdev-counts">
                  {$t('xlog.connections', { n: String(d.connections) })}
                  {#if d.rejected > 0}
                    · <span class="xdev-rejected"
                      >{$t('xlog.rejected', { n: String(d.rejected) })}</span
                    >
                  {/if}
                </span>
                <span class="xdev-top font-mono">{d.top_destinations.join(', ')}</span>
              </button>
            </li>
          {/each}
        </ul>

        <div class="xdev-entries">
          <div class="xdev-filters">
            <label class="visually-hidden" for="xdev-dest">{$t('xlog.filter_dest')}</label>
            <input
              id="xdev-dest"
              class="input font-mono"
              type="search"
              placeholder={$t('xlog.filter_dest')}
              value={destInput}
              oninput={onDestInput}
            />
            <Select
              id="xdev-outbound"
              bind:value={outbound}
              ariaLabel={$t('xlog.filter_outbound')}
              options={outboundOptions}
              onchange={() => load()}
            />
          </div>

          <div class="xdev-table-wrap">
            <table class="xdev-table">
              <thead>
                <tr>
                  <th>{$t('xlog.col_time')}</th>
                  <th>{$t('xlog.col_device')}</th>
                  <th>{$t('xlog.col_dest')}</th>
                  <th>{$t('xlog.col_route')}</th>
                </tr>
              </thead>
              <tbody>
                {#each data.report.entries as e, i (i)}
                  <tr class:is-rejected={e.status === 'rejected'}>
                    <td class="font-mono">{e.time.slice(11)}</td>
                    <td>{deviceLabel(e)}</td>
                    <td class="font-mono xdev-dest">
                      {#if e.status === 'rejected'}
                        {e.reason}
                      {:else}
                        {e.destination}{e.port ? `:${e.port}` : ''}
                        {#if e.network}<span class="xdev-net">{e.network}</span>{/if}
                      {/if}
                    </td>
                    <td class="font-mono">
                      {#if e.inbound}{e.inbound} →
                      {/if}{e.outbound || '—'}
                    </td>
                  </tr>
                {/each}
              </tbody>
            </table>
          </div>
        </div>
      </div>
    {/if}
  {/if}
</div>

<style>
  .xdev {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-3);
  }

  .xdev-error {
    margin: 0;
    color: var(--danger);
    font-size: var(--font-size-sm);
  }

  .xdev-off {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: var(--spacing-3);
  }

  .xdev-bar {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    justify-content: space-between;
    gap: var(--spacing-2);
  }

  .xdev-meta {
    font-size: var(--font-size-xs);
    color: var(--fg-muted);
    word-break: break-all;
  }

  .xdev-grid {
    display: grid;
    grid-template-columns: minmax(220px, 300px) 1fr;
    gap: var(--spacing-3);
    align-items: start;
  }

  @media (max-width: 900px) {
    .xdev-grid {
      grid-template-columns: 1fr;
    }
  }

  .xdev-devices {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: var(--spacing-2);
    max-height: 70vh;
    overflow-y: auto;
    scrollbar-width: thin;
    scrollbar-color: var(--scrollbar-thumb) transparent;
  }

  .xdev-device {
    width: 100%;
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    gap: 2px;
    padding: var(--spacing-2) var(--spacing-3);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    background: var(--bg-card);
    color: var(--fg-primary);
    font: inherit;
    text-align: left;
    cursor: pointer;
  }

  .xdev-device:hover {
    border-color: var(--border-hover);
  }

  .xdev-device.active {
    border-color: var(--accent-border);
    background: var(--accent-soft);
  }

  .xdev-name {
    font-weight: 600;
  }

  .xdev-ip,
  .xdev-counts,
  .xdev-top {
    font-size: var(--font-size-xs);
    color: var(--fg-muted);
  }

  .xdev-top {
    max-width: 100%;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .xdev-rejected {
    color: var(--danger);
  }

  .xdev-entries {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-2);
    min-width: 0;
  }

  .xdev-filters {
    display: flex;
    flex-wrap: wrap;
    gap: var(--spacing-2);
  }

  .xdev-filters .input {
    flex: 1 1 12rem;
    min-width: 0;
  }

  .xdev-table-wrap {
    overflow: auto;
    max-height: 70vh;
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    scrollbar-width: thin;
    scrollbar-color: var(--scrollbar-thumb) transparent;
  }

  .xdev-table {
    width: 100%;
    border-collapse: collapse;
    font-size: var(--font-size-sm);
  }

  .xdev-table th {
    position: sticky;
    top: 0;
    background: var(--bg-elevated);
    color: var(--fg-secondary);
    font-weight: 600;
    text-align: left;
    padding: var(--spacing-2);
  }

  .xdev-table td {
    padding: 4px var(--spacing-2);
    border-top: 1px solid var(--border-subtle);
    color: var(--fg-primary);
    vertical-align: top;
  }

  .xdev-table tr.is-rejected td {
    color: var(--danger);
  }

  .xdev-dest {
    word-break: break-all;
  }

  .xdev-net {
    margin-left: var(--spacing-1);
    font-size: var(--font-size-xs);
    color: var(--fg-faint);
  }

  .visually-hidden {
    position: absolute;
    width: 1px;
    height: 1px;
    overflow: hidden;
    clip: rect(0 0 0 0);
    white-space: nowrap;
  }
</style>
