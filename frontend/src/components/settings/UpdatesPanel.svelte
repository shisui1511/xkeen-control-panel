<script lang="ts">
  import { onMount, onDestroy } from 'svelte';
  import { t, currentLang } from '../../i18n';
  import { apiFetch, apiFetchJSON } from '../../lib/api';
  import { showToast, showConfirm } from '../../stores';
  import { formatBytes } from '../../lib/format';
  import {
    parseReleaseNotes,
    splitInlineCode,
    releaseKind,
    type ReleaseKind
  } from '../../lib/releaseNotes';
  import SegmentedControl, { type SegmentItem } from '../SegmentedControl.svelte';
  import Modal from '../Modal.svelte';

  interface ReleaseNote {
    version: string;
    prerelease: boolean;
    published_at?: string;
    url?: string;
    body: string;
  }

  interface UpdateInfo {
    current_version: string;
    latest_version: string;
    has_update: boolean;
    channel: string;
    download_size?: number;
    prerelease?: boolean;
    published_at?: string;
    changelog?: string;
    releases?: ReleaseNote[];
  }

  interface UpdateStatus {
    status: string;
    message: string;
    progress: number;
    downloaded?: number;
    total?: number;
  }

  interface Backup {
    name: string;
    version?: string;
    created_at: number;
    size: number;
  }

  type Channel = 'stable' | 'beta';

  let { version }: { version: string } = $props();

  let channel = $state<Channel>('stable');
  let info = $state<UpdateInfo | null>(null);
  let checkError = $state('');
  let checkedAt = $state<Date | null>(null);
  let checking = $state(false);
  let status = $state<UpdateStatus | null>(null);
  let installing = $state(false);
  let backups = $state<Backup[]>([]);
  let showConfirmInstall = $state(false);

  let sseSource: EventSource | null = null;
  let reconnecting = $state(false);
  let reconnectAttempt = 0;
  let reconnectTimer: ReturnType<typeof setTimeout> | null = null;
  const RECONNECT_INTERVAL_MS = 1500;
  const RECONNECT_MAX_ATTEMPTS = 40; // 40 × 1.5 с = 60 с

  const channelItems: SegmentItem[] = $derived([
    { value: 'stable', label: $t('settings.channel_stable') },
    { value: 'beta', label: $t('settings.channel_beta') }
  ]);

  const currentKind = $derived(releaseKind(info?.current_version || version || ''));

  const notes = $derived(
    (info?.releases?.length
      ? info.releases
      : info?.has_update && info.changelog
        ? [
            {
              version: info.latest_version,
              prerelease: !!info.prerelease,
              published_at: info.published_at,
              body: info.changelog
            }
          ]
        : []
    ).map((r) => ({ ...r, sections: parseReleaseNotes(r.body) }))
  );

  const busy = $derived(
    installing || (!!status && !['idle', 'done', 'failed'].includes(status.status))
  );

  function kindBadge(kind: ReleaseKind): string {
    return kind === 'stable' ? 'badge-success' : kind === 'rc' ? 'badge-primary' : 'badge-warning';
  }

  function formatDate(value: string | number | undefined): string {
    if (!value) return '';
    const d = typeof value === 'number' ? new Date(value * 1000) : new Date(value);
    if (Number.isNaN(d.getTime())) return '';
    return d.toLocaleString($currentLang === 'ru' ? 'ru-RU' : 'en-US', {
      day: 'numeric',
      month: 'short',
      hour: '2-digit',
      minute: '2-digit'
    });
  }

  function formatTime(d: Date): string {
    return d.toLocaleTimeString($currentLang === 'ru' ? 'ru-RU' : 'en-US', {
      hour: '2-digit',
      minute: '2-digit'
    });
  }

  function stageLabel(s: string): string {
    const key = `settings.update_stage_${s}`;
    const label = $t(key);
    return label === key ? s : label;
  }

  async function fetchChannel() {
    try {
      const data = await apiFetchJSON<{ channel?: Channel }>('/api/update/channel');
      channel = data.channel === 'beta' ? 'beta' : 'stable';
    } catch (_: any) {}
  }

  async function saveChannel(value: string) {
    const next: Channel = value === 'beta' ? 'beta' : 'stable';
    const prev = channel;
    channel = next;
    try {
      await apiFetchJSON('/api/update/channel', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ channel: next })
      });
      showToast('success', $t('settings.channel_saved'));
      checkUpdate();
    } catch (e: any) {
      channel = prev;
      if (e?.status === 401) return;
      showToast('error', e instanceof Error ? e.message : String(e));
    }
  }

  async function checkUpdate() {
    checking = true;
    checkError = '';
    try {
      info = await apiFetchJSON<UpdateInfo>(`/api/update/check?channel=${channel}`);
      checkedAt = new Date();
    } catch (e: any) {
      if (e?.status === 401) return;
      checkError = e instanceof Error ? e.message : String(e);
    } finally {
      checking = false;
    }
  }

  async function fetchBackups() {
    try {
      backups = (await apiFetchJSON<Backup[]>('/api/update/backups')) ?? [];
    } catch (_: any) {
      backups = [];
    }
  }

  async function fetchStatus() {
    try {
      status = await apiFetchJSON<UpdateStatus>('/api/update/status');
    } catch (_: any) {}
  }

  function stopReconnectPolling() {
    if (reconnectTimer !== null) {
      clearTimeout(reconnectTimer);
      reconnectTimer = null;
    }
    reconnecting = false;
    reconnectAttempt = 0;
  }

  function startReconnectPolling() {
    if (reconnecting) return;
    reconnecting = true;
    reconnectAttempt = 0;
    pollVersion();
  }

  function pollVersion() {
    reconnectTimer = setTimeout(async () => {
      reconnectAttempt++;
      try {
        const res = await apiFetch('/api/version', { cache: 'no-store' });
        if (res.ok) {
          stopReconnectPolling();
          window.location.reload();
          return;
        }
      } catch (_: any) {
        // сервер ещё не поднялся
      }
      if (reconnectAttempt < RECONNECT_MAX_ATTEMPTS) {
        pollVersion();
      } else {
        stopReconnectPolling();
        installing = false;
        status = {
          status: 'failed',
          message: $t('settings.update_reconnect_timeout'),
          progress: 0
        };
      }
    }, RECONNECT_INTERVAL_MS);
  }

  function closeSSE() {
    sseSource?.close();
    sseSource = null;
  }

  function startStatusSSE() {
    closeSSE();
    installing = true;
    sseSource = new EventSource('/api/update/events');
    sseSource.onmessage = (event) => {
      try {
        const state = JSON.parse(event.data) as UpdateStatus;
        status = state;
        if (state.status === 'restarting') {
          closeSSE();
          startReconnectPolling();
        } else if (state.status === 'done') {
          installing = false;
          closeSSE();
          window.location.reload();
        } else if (state.status === 'failed') {
          installing = false;
          closeSSE();
          fetchBackups();
        }
      } catch (_) {
        // битое сообщение — ждём следующее
      }
    };
    sseSource.onerror = () => {
      closeSSE();
      if (installing && !reconnecting) {
        startReconnectPolling();
      } else {
        installing = false;
      }
    };
  }

  async function installUpdate() {
    showConfirmInstall = false;
    installing = true;
    try {
      await apiFetchJSON(`/api/update/install?channel=${channel}`, { method: 'POST' });
      startStatusSSE();
    } catch (e: any) {
      installing = false;
      if (e?.status === 401) return;
      showToast('error', e instanceof Error ? e.message : String(e));
    }
  }

  async function rollback(backup: Backup) {
    const target = backup.version || $t('settings.backup_unknown_version');
    const ok = await showConfirm({
      title: $t('settings.rollback_confirm_title'),
      message: $t('settings.rollback_confirm_text', { version: target }),
      confirmLabel: $t('settings.rollback'),
      variant: 'warning'
    });
    if (!ok) return;
    installing = true;
    try {
      await apiFetchJSON('/api/update/rollback', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ backup: backup.name })
      });
      startStatusSSE();
    } catch (e: any) {
      installing = false;
      if (e?.status === 401) return;
      showToast('error', e instanceof Error ? e.message : String(e));
    }
  }

  onMount(async () => {
    await Promise.all([fetchChannel(), fetchStatus()]);
    fetchBackups();
    if (status && !['idle', 'done', 'failed'].includes(status.status)) {
      startStatusSSE();
    } else {
      checkUpdate();
    }
  });

  onDestroy(() => {
    closeSSE();
    stopReconnectPolling();
  });
</script>

<div class="card mb-2">
  <div class="card-label">{$t('settings.update')}</div>
  <div class="field-group">
    <div class="field-row">
      <span class="field-row-name">{$t('settings.current_version')}</span>
      <span class="field-row-val version-val">
        <span class="mono">{version}</span>
        {#if version && version !== '...'}
          <span class="badge {kindBadge(currentKind)}"
            >{$t(`settings.update_kind_${currentKind}`)}</span
          >
        {/if}
      </span>
    </div>
    <div class="field-row channel-row">
      <div>
        <span class="field-row-name">{$t('settings.update_channel')}</span>
        <div class="field-row-desc">{$t(`settings.channel_${channel}_desc`)}</div>
      </div>
      <div class="field-row-val">
        <SegmentedControl
          items={channelItems}
          value={channel}
          ariaLabel={$t('settings.update_channel')}
          onchange={saveChannel}
        />
      </div>
    </div>
  </div>

  {#if status && status.status !== 'idle' && (busy || status.status === 'failed')}
    <div class="update-progress" class:failed={status.status === 'failed'} aria-live="polite">
      <div class="progress-head">
        <span class="progress-stage">{stageLabel(status.status)}</span>
        {#if status.status === 'downloading' && status.total}
          <span class="progress-bytes mono"
            >{$t('settings.update_downloaded', {
              done: formatBytes(status.downloaded || 0),
              total: formatBytes(status.total)
            })}</span
          >
        {/if}
      </div>
      {#if status.status !== 'failed'}
        <div class="progress-bar">
          <div
            class="progress-fill"
            class:progress-pulse={reconnecting}
            style:width="{reconnecting ? 100 : status.progress}%"
          ></div>
        </div>
      {/if}
      {#if status.message}
        <span class="progress-text">{status.message}</span>
      {/if}
    </div>
  {/if}

  {#if reconnecting}
    <div class="reconnect-overlay">
      <div class="spinner reconnect-spinner"></div>
      <div class="reconnect-text">
        <span>{$t('settings.update_reconnecting')}</span>
        <span class="reconnect-dots"></span>
      </div>
      <div class="reconnect-sub">{$t('settings.update_reconnecting_sub')}</div>
    </div>
  {:else if !busy}
    {#if checkError}
      <div class="update-state state-error">{checkError}</div>
    {:else if info?.has_update}
      {@const kind = releaseKind(info.latest_version)}
      <div class="update-available">
        <div class="available-head">
          <span class="available-title"
            >{$t('settings.update_available_title', { version: info.latest_version })}</span
          >
          <span class="badge {kindBadge(kind)}">{$t(`settings.update_kind_${kind}`)}</span>
        </div>
        <div class="available-meta">
          {#if info.published_at}<span>{formatDate(info.published_at)}</span>{/if}
          {#if info.download_size}<span
              >{$t('settings.update_download_size', {
                size: formatBytes(info.download_size)
              })}</span
            >{/if}
          {#if info.releases && info.releases.length > 1}<span
              >{$t('settings.update_versions_behind', { count: info.releases.length })}</span
            >{/if}
        </div>
        <button class="btn btn-primary" onclick={() => (showConfirmInstall = true)}>
          {$t('settings.install_update')}
        </button>
      </div>
    {:else if info}
      <div class="update-state state-ok">
        {$t('settings.update_up_to_date')}
      </div>
    {/if}
  {/if}

  <div class="card-actions">
    <button class="btn btn-secondary" onclick={checkUpdate} disabled={checking || busy}>
      {checking ? $t('settings.checking') : $t('settings.check_update')}
    </button>
    {#if checkedAt && !checking}
      <span class="checked-at"
        >{$t('settings.update_checked_at', { time: formatTime(checkedAt) })}</span
      >
    {/if}
  </div>
</div>

{#if notes.length > 0 && !busy}
  <div class="card mb-2">
    <div class="card-label">{$t('settings.update_whats_new')}</div>
    <div class="release-list">
      {#each notes as note (note.version)}
        {@const kind = releaseKind(note.version)}
        <section class="release">
          <header class="release-head">
            <span class="release-version mono">v{note.version}</span>
            {#if kind !== 'stable'}
              <span class="badge {kindBadge(kind)}">{$t(`settings.update_kind_${kind}`)}</span>
            {/if}
            {#if note.published_at}<span class="release-date">{formatDate(note.published_at)}</span
              >{/if}
            {#if note.url}
              <a class="release-link" href={note.url} target="_blank" rel="noopener noreferrer"
                >{$t('settings.update_release_page')}</a
              >
            {/if}
          </header>
          {#if note.sections.length === 0}
            <p class="release-empty">{$t('settings.update_no_notes')}</p>
          {:else}
            {#each note.sections as section, i (i)}
              <div class="release-section kind-{section.kind}">
                <div class="section-title">{section.title}</div>
                <ul>
                  {#each section.items as item, j (j)}
                    <li>
                      {#each splitInlineCode(item.text) as seg, k (k)}
                        {#if seg.code}<code>{seg.text}</code>{:else}{seg.text}{/if}
                      {/each}
                      {#if item.commit}
                        <a
                          class="commit-link mono"
                          href={item.commit.url}
                          target="_blank"
                          rel="noopener noreferrer">{item.commit.sha}</a
                        >
                      {/if}
                    </li>
                  {/each}
                </ul>
              </div>
            {/each}
          {/if}
        </section>
      {/each}
    </div>
  </div>
{/if}

<div class="card mb-2">
  <div class="card-label">{$t('settings.update_backups_title')}</div>
  <div class="field-row-desc backups-hint">{$t('settings.update_backups_hint')}</div>
  {#if backups.length === 0}
    <div class="backups-empty">{$t('settings.update_backups_empty')}</div>
  {:else}
    <div class="field-group">
      {#each backups as backup (backup.name)}
        <div class="field-row">
          <div>
            <span class="field-row-name mono"
              >{backup.version ? `v${backup.version}` : $t('settings.backup_unknown_version')}</span
            >
            <div class="field-row-desc">
              {formatDate(backup.created_at)} · {formatBytes(backup.size)}
            </div>
          </div>
          <button class="btn btn-secondary btn-sm" onclick={() => rollback(backup)} disabled={busy}>
            {$t('settings.rollback')}
          </button>
        </div>
      {/each}
    </div>
  {/if}
</div>

<Modal
  isOpen={showConfirmInstall}
  title={$t('settings.update_confirm_title')}
  onclose={() => (showConfirmInstall = false)}
>
  <p class="confirm-text">
    {$t('settings.update_confirm_text', {
      version: info?.latest_version ?? '',
      size: info?.download_size ? formatBytes(info.download_size) : '—'
    })}
  </p>
  <div class="modal-actions">
    <button class="btn btn-secondary" onclick={() => (showConfirmInstall = false)}
      >{$t('app.cancel')}</button
    >
    <button class="btn btn-primary" onclick={installUpdate}
      >{$t('settings.update_install_btn')}</button
    >
  </div>
</Modal>

<style>
  .card-label {
    font-size: 12px;
    font-weight: 600;
    letter-spacing: 0.04em;
    color: var(--fg-dim);
    margin-bottom: 14px;
  }

  .mb-2 {
    margin-bottom: 12px;
  }

  .mono {
    font-family: var(--font-mono, monospace);
    font-size: 12px;
  }

  .field-group {
    display: flex;
    flex-direction: column;
  }

  .field-row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 10px 0;
    border-bottom: 1px solid var(--border);
    gap: 16px;
  }

  .field-row:first-child {
    padding-top: 0;
  }

  .field-row:last-child {
    border-bottom: none;
    padding-bottom: 0;
  }

  .field-row-name {
    font-size: 14px;
    font-weight: 500;
    color: var(--fg-primary);
  }

  .field-row-val {
    font-size: 13px;
    color: var(--fg-secondary);
    text-align: right;
  }

  .field-row-desc {
    font-size: 12px;
    color: var(--fg-dim);
    margin-top: 2px;
  }

  .version-val {
    display: inline-flex;
    align-items: center;
    gap: 8px;
  }

  .card-actions {
    display: flex;
    align-items: center;
    gap: 12px;
    margin-top: 14px;
    flex-wrap: wrap;
  }

  .checked-at {
    font-size: 12px;
    color: var(--fg-dim);
  }

  .btn-sm {
    padding: 6px 12px;
    font-size: 12px;
  }

  .update-state {
    margin-top: 14px;
    padding: 10px 12px;
    border-radius: var(--radius-md);
    font-size: 13px;
  }

  .state-ok {
    color: var(--success);
    background: color-mix(in srgb, var(--success) 8%, transparent);
    border: 1px solid color-mix(in srgb, var(--success) 25%, transparent);
  }

  .state-error {
    color: var(--danger);
    background: color-mix(in srgb, var(--danger) 8%, transparent);
    border: 1px solid color-mix(in srgb, var(--danger) 25%, transparent);
    word-break: break-word;
  }

  .update-available {
    margin-top: 14px;
    padding: 14px;
    border-radius: var(--radius-md);
    background: var(--accent-soft);
    border: 1px solid var(--accent-line);
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    gap: 8px;
  }

  .available-head {
    display: flex;
    align-items: center;
    gap: 8px;
    flex-wrap: wrap;
  }

  .available-title {
    font-size: 15px;
    font-weight: 600;
    color: var(--fg-primary);
  }

  .available-meta {
    display: flex;
    flex-wrap: wrap;
    gap: 4px 14px;
    font-size: 12px;
    color: var(--fg-secondary);
  }

  .update-progress {
    margin-top: 14px;
  }

  .progress-head {
    display: flex;
    justify-content: space-between;
    align-items: baseline;
    gap: 12px;
    margin-bottom: 6px;
  }

  .progress-stage {
    font-size: 13px;
    font-weight: 600;
    color: var(--fg-primary);
  }

  .update-progress.failed .progress-stage {
    color: var(--danger);
  }

  .progress-bytes {
    color: var(--fg-secondary);
  }

  .progress-bar {
    height: 6px;
    background: var(--border);
    border-radius: 4px;
    overflow: hidden;
    margin-bottom: 6px;
  }

  .progress-fill {
    height: 100%;
    background: var(--accent);
    border-radius: 4px;
    transition: width 0.3s ease;
  }

  .progress-fill.progress-pulse {
    animation: progress-shimmer 1.5s ease-in-out infinite;
    background: linear-gradient(
      90deg,
      var(--accent) 0%,
      color-mix(in srgb, var(--accent) 60%, var(--bg-card)) 50%,
      var(--accent) 100%
    );
    background-size: 200% 100%;
  }

  @keyframes progress-shimmer {
    0% {
      background-position: 200% 0;
    }
    100% {
      background-position: -200% 0;
    }
  }

  .progress-text {
    font-size: 12px;
    color: var(--fg-secondary);
    word-break: break-word;
  }

  .reconnect-overlay {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 10px;
    padding: 18px 16px;
    margin-top: 14px;
    background: color-mix(in srgb, var(--accent) 8%, var(--bg-card));
    border: 1px solid color-mix(in srgb, var(--accent) 30%, transparent);
    border-radius: var(--radius-md);
  }

  .reconnect-spinner {
    --spinner-size: 28px;
    --spinner-w: 3px;
    --spinner-track: color-mix(in srgb, var(--accent) 25%, transparent);
  }

  .reconnect-text {
    display: flex;
    align-items: center;
    gap: 4px;
    font-size: 14px;
    font-weight: 600;
    color: var(--fg-primary);
  }

  .reconnect-dots::after {
    content: '';
    animation: dots 1.5s steps(4, end) infinite;
  }

  @keyframes dots {
    0% {
      content: '';
    }
    25% {
      content: '.';
    }
    50% {
      content: '..';
    }
    75% {
      content: '...';
    }
    100% {
      content: '';
    }
  }

  .reconnect-sub {
    font-size: 12px;
    color: var(--fg-secondary);
    text-align: center;
  }

  .release-list {
    display: flex;
    flex-direction: column;
    gap: 16px;
    max-height: 480px;
    overflow-y: auto;
    scrollbar-width: thin;
    scrollbar-color: var(--border) transparent;
  }

  .release + .release {
    padding-top: 16px;
    border-top: 1px solid var(--border);
  }

  .release-head {
    display: flex;
    align-items: center;
    gap: 8px;
    flex-wrap: wrap;
    margin-bottom: 8px;
  }

  .release-version {
    font-size: 13px;
    font-weight: 600;
    color: var(--fg-primary);
  }

  .release-date {
    font-size: 12px;
    color: var(--fg-dim);
  }

  .release-link {
    margin-left: auto;
    font-size: 12px;
    color: var(--accent);
  }

  .release-empty {
    margin: 0;
    font-size: 13px;
    color: var(--fg-dim);
  }

  .release-section + .release-section {
    margin-top: 10px;
  }

  .section-title {
    font-size: 12px;
    font-weight: 600;
    margin-bottom: 4px;
    color: var(--fg-secondary);
  }

  .kind-feat .section-title {
    color: var(--accent);
  }

  .kind-fix .section-title {
    color: var(--success);
  }

  .release-section ul {
    margin: 0;
    padding-left: 18px;
    display: flex;
    flex-direction: column;
    gap: 3px;
  }

  .release-section li {
    font-size: 13px;
    color: var(--fg-primary);
    line-height: 1.45;
  }

  .release-section code {
    font-family: var(--font-mono, monospace);
    font-size: 12px;
    padding: 0 4px;
    border-radius: var(--radius-xs);
    background: var(--surface-tint);
  }

  .commit-link {
    margin-left: 6px;
    font-size: 11px;
    color: var(--fg-dim);
  }

  .commit-link:hover {
    color: var(--accent);
  }

  .backups-hint {
    margin: -8px 0 10px;
  }

  .backups-empty {
    font-size: 13px;
    color: var(--fg-dim);
  }

  .confirm-text {
    margin: 0;
    font-size: 14px;
    color: var(--fg-secondary);
    line-height: 1.5;
  }

  .modal-actions {
    display: flex;
    justify-content: flex-end;
    gap: 12px;
    margin-top: 16px;
  }

  @media (max-width: 560px) {
    .channel-row {
      flex-direction: column;
      align-items: flex-start;
    }

    .channel-row .field-row-val {
      text-align: left;
    }

    .release-link {
      margin-left: 0;
    }
  }
</style>
