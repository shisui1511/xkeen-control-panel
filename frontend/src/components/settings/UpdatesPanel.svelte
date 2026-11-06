<script lang="ts">
  import { onMount, onDestroy } from 'svelte';
  import { t, tp, currentLang } from '../../i18n';
  import { apiFetch, apiFetchJSON } from '../../lib/api';
  import { showToast, showConfirm } from '../../stores';
  import { formatBytes } from '../../lib/format';
  import {
    parseRelease,
    splitInlineCode,
    releaseKind,
    type NoteItem,
    type NoteKind,
    type ReleaseKind
  } from '../../lib/releaseNotes';
  import Icon from '../Icon.svelte';
  import SegmentedControl, { type SegmentItem } from '../SegmentedControl.svelte';
  import Modal from '../Modal.svelte';
  import Section from '../Section.svelte';
  import Select from '../Select.svelte';
  import { updateState, refreshUpdateState, type UpdateCheckState } from '../../lib/updateNotify';
  import {
    stepLabel,
    failureView,
    apiErrorView,
    type UpdateStatusPayload
  } from '../../lib/updateStatusView';
  import { autoInstallView, autoStateOf, otherAutoVersion } from '../../lib/autoInstallView';

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
  let checkErrorDetail = $state('');
  let checkedAt = $state<Date | null>(null);
  let checking = $state(false);
  let status = $state<UpdateStatusPayload | null>(null);
  const stepRef = $derived(status ? stepLabel(status) : null);
  const failure = $derived(status ? failureView(status) : null);
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
    ).map((r) => ({ ...r, ...parseRelease(r.body) }))
  );

  // «Что нового» — одна сводка изменений, которые придут с обновлением, а не лента
  // выпусков: так показывают обновления Home Assistant, DSM и магазины приложений.
  // Все выпуски между установленной и новой версией сливаются: главное из «Что вошло»
  // каждого, затем пункты по типам без повторов (новые выпуски первыми).
  const KIND_ORDER: NoteKind[] = ['feat', 'fix', 'other'];
  const KIND_TITLE: Record<NoteKind, string> = {
    feat: 'settings.update_group_feat',
    fix: 'settings.update_group_fix',
    other: 'settings.update_group_other'
  };
  // Сколько пунктов группы видно до «Показать все»
  const GROUP_PREVIEW = 6;
  const HIGHLIGHTS_PREVIEW = 4;

  const digest = $derived.by(() => {
    const highlights: string[] = [];
    const seenHighlight = new Set<string>();
    const groups = new Map<NoteKind, { items: NoteItem[]; more: number }>();
    const seenItem = new Set<string>();
    for (const note of notes) {
      for (const line of note.summary) {
        const text = capitalize(line);
        const key = text.toLocaleLowerCase();
        if (seenHighlight.has(key)) continue;
        seenHighlight.add(key);
        highlights.push(text);
      }
      for (const section of note.sections) {
        const group = groups.get(section.kind) ?? { items: [], more: 0 };
        groups.set(section.kind, group);
        group.more += section.more;
        for (const item of section.items) {
          const key = item.text.toLocaleLowerCase();
          if (seenItem.has(key)) continue;
          seenItem.add(key);
          group.items.push({ ...item, text: capitalize(item.text) });
        }
      }
    }
    // Пункт, повторяющий главное слово в слово, в главном и остаётся
    const onlyItems = [...groups.values()].flatMap((g) => g.items);
    const shownHighlights =
      onlyItems.length === highlights.length &&
      onlyItems.every((it) => seenHighlight.has(capitalize(it.text).toLocaleLowerCase()))
        ? []
        : highlights;
    return {
      highlights: shownHighlights,
      groups: KIND_ORDER.filter((k) => groups.has(k)).map((k) => ({ kind: k, ...groups.get(k)! }))
    };
  });

  let showAllHighlights = $state(false);
  let expandedGroups = $state<Record<string, boolean>>({});

  // Полный список — сравнение версий на GitHub; ссылка строится от страницы новейшего выпуска
  const compareUrl = $derived.by(() => {
    const latest = notes[0];
    const from = info?.current_version || version;
    if (!latest?.url) return '';
    const m = latest.url.match(/^(https:\/\/github\.com\/[^/]+\/[^/]+)\/releases\/tag\/(.+)$/);
    if (!m || !from || from === '...' || notes.length < 2) return latest.url;
    return `${m[1]}/compare/v${from.replace(/^v/, '')}...${m[2]}`;
  });

  function capitalize(text: string): string {
    return text.charAt(0).toLocaleUpperCase() + text.slice(1);
  }

  // Окна автоустановки: ночные часы, когда сетью почти не пользуются
  const WINDOW_PRESETS = [
    '00:00-02:00',
    '01:00-03:00',
    '02:00-04:00',
    '03:00-05:00',
    '04:00-06:00',
    '05:00-07:00'
  ];
  const windowOptions = $derived(
    (!$updateState?.install_window || WINDOW_PRESETS.includes($updateState.install_window)
      ? WINDOW_PRESETS
      : [...WINDOW_PRESETS, $updateState.install_window]
    ).map((w) => ({ value: w, label: w.replace('-', ' – ') }))
  );
  let savingAuto = $state(false);

  // Состояние автоустановки из последней проверки планировщика; текст и дата — из общего помощника
  const autoView = $derived(autoInstallView($updateState, $currentLang, 'card'));
  const autoCode = $derived(autoStateOf($updateState));
  // Состояние относится к версии и каналу последней проверки планировщика: у другой версии
  // (смена канала, ручная проверка опередила планировщик) чужие срок и «Срочное» не рисуются
  const availableMatches = $derived(
    !!info?.has_update &&
      $updateState?.latest_version === info.latest_version &&
      $updateState?.channel === channel
  );
  const availableView = $derived(
    availableMatches && autoCode && autoCode !== 'dev_build' ? autoView : null
  );
  // «Срочное» — только когда срочный кандидат и есть показанная версия
  const availableUrgent = $derived(
    availableMatches && autoCode === 'urgent' && !otherAutoVersion($updateState)
  );

  async function saveAuto(
    patch: Partial<Pick<UpdateCheckState, 'auto_check' | 'auto_install' | 'install_window'>>
  ) {
    savingAuto = true;
    try {
      const st = await apiFetchJSON<UpdateCheckState>('/api/update/settings', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(patch)
      });
      updateState.set(st);
      showToast('success', $t('settings.update_auto_saved'));
    } catch (e: any) {
      await refreshUpdateState();
      if (e?.status === 401) return;
      const view = apiErrorView(e);
      showToast('error', view ? $t(view.key) : e instanceof Error ? e.message : String(e));
    } finally {
      savingAuto = false;
    }
  }

  const busy = $derived(
    installing || (!!status && !['idle', 'done', 'failed'].includes(status.status))
  );

  function kindBadge(kind: ReleaseKind): string {
    return kind === 'stable'
      ? 'badge-success'
      : kind === 'beta'
        ? 'badge-primary'
        : 'badge-warning';
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
      const view = apiErrorView(e);
      showToast('error', view ? $t(view.key) : e instanceof Error ? e.message : String(e));
    }
  }

  async function checkUpdate() {
    checking = true;
    checkError = '';
    checkErrorDetail = '';
    try {
      info = await apiFetchJSON<UpdateInfo>(`/api/update/check?channel=${channel}`);
      checkedAt = new Date();
      // Ручная проверка обновляет и уведомления (точка в меню, баннер)
      refreshUpdateState();
    } catch (e: any) {
      if (e?.status === 401) return;
      const view = apiErrorView(e);
      checkError = view ? $t(view.key) : e instanceof Error ? e.message : String(e);
      checkErrorDetail = view?.detail ?? '';
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
      status = await apiFetchJSON<UpdateStatusPayload>('/api/update/status');
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
        const state = JSON.parse(event.data) as UpdateStatusPayload;
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
      const view = apiErrorView(e);
      showToast('error', view ? $t(view.key) : e instanceof Error ? e.message : String(e));
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
      const view = apiErrorView(e);
      showToast('error', view ? $t(view.key) : e instanceof Error ? e.message : String(e));
    }
  }

  onMount(async () => {
    await Promise.all([fetchChannel(), fetchStatus()]);
    fetchBackups();
    refreshUpdateState();
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

<div class="page-sections">
  <Section title={$t('settings.update')}>
    <div class="card card-rows">
      <div class="field-group">
        <div class="field-row">
          <span class="field-row-name">{$t('settings.current_version')}</span>
          <span class="field-row-val version-val">
            <span class="mono">{version}</span>
            {#if version && version !== '...'}
              <span class="badge {kindBadge(currentKind)}" data-testid="update-kind-current"
                >{$t(`settings.update_kind_${currentKind}`)}</span
              >
            {/if}
          </span>
        </div>
        <div class="field-row channel-row" data-testid="update-channel-row">
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
          {#if failure}
            <span class="progress-text">{$t(failure.reason.key, failure.reason.params)}</span>
            {#if failure.detail}
              <span class="progress-detail">{failure.detail}</span>
            {/if}
          {:else if stepRef}
            <span class="progress-text">{$t(stepRef.key, stepRef.params)}</span>
          {:else if status.message}
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
          <div class="update-state state-error">
            {checkError}
            {#if checkErrorDetail}
              <span class="progress-detail">{checkErrorDetail}</span>
            {/if}
          </div>
        {:else if info?.has_update}
          {@const kind = releaseKind(info.latest_version)}
          <div class="update-available">
            <div class="available-head">
              <span class="available-title"
                >{$t('settings.update_available_title', { version: info.latest_version })}</span
              >
              <span class="badge {kindBadge(kind)}">{$t(`settings.update_kind_${kind}`)}</span>
              {#if availableUrgent}
                <span class="badge badge-warning" data-testid="update-urgent-badge"
                  >{$t('settings.update_urgent_badge')}</span
                >
              {/if}
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
            {#if availableView}
              <div class="available-auto" data-testid="update-available-auto" data-state={autoCode}>
                {$t(availableView.key, availableView.params)}
              </div>
            {/if}
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

      <div class="card-footer">
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
  </Section>

  {#if $updateState}
    {@const auto = $updateState}
    <Section title={$t('settings.update_auto_title')}>
      <div class="card card-rows">
        <div class="field-group">
          <div class="field-row">
            <div>
              <span class="field-row-name">{$t('settings.update_auto_check')}</span>
              <div class="field-row-desc">{$t('settings.update_auto_check_desc')}</div>
            </div>
            <label class="toggle-switch">
              <input
                type="checkbox"
                aria-label={$t('settings.update_auto_check')}
                checked={auto.auto_check}
                disabled={savingAuto}
                onchange={(e) => saveAuto({ auto_check: e.currentTarget.checked })}
              />
              <span class="toggle-slider"></span>
            </label>
          </div>
          <div class="field-row">
            <div>
              <span class="field-row-name">{$t('settings.update_auto_install')}</span>
              <div class="field-row-desc">{$t('settings.update_auto_install_desc')}</div>
            </div>
            <label class="toggle-switch">
              <input
                type="checkbox"
                aria-label={$t('settings.update_auto_install')}
                checked={auto.auto_install}
                disabled={savingAuto}
                onchange={(e) => saveAuto({ auto_install: e.currentTarget.checked })}
              />
              <span class="toggle-slider"></span>
            </label>
          </div>
          {#if auto.auto_install}
            <div class="field-row">
              <div>
                <span class="field-row-name">{$t('settings.update_window')}</span>
                <div class="field-row-desc">{$t('settings.update_window_desc')}</div>
              </div>
              <Select
                value={auto.install_window}
                options={windowOptions}
                ariaLabel={$t('settings.update_window')}
                disabled={savingAuto}
                onchange={(e) => saveAuto({ install_window: e.currentTarget.value })}
              />
            </div>
          {/if}
        </div>

        {#if autoView}
          <div
            class="update-state state-info auto-status"
            role="status"
            data-testid="update-auto-status"
            data-state={autoCode}
          >
            {$t(autoView.key, autoView.params)}
          </div>
        {/if}

        {#if auto.last_auto_install}
          {@const last = auto.last_auto_install}
          <div class="update-state {last.ok ? 'state-ok' : 'state-error'}">
            {last.ok
              ? $t('settings.update_auto_last_ok', {
                  from: last.from,
                  version: last.version,
                  date: formatDate(last.done_at || last.started_at)
                })
              : $t('settings.update_auto_last_failed', { version: last.version, from: last.from })}
          </div>
        {/if}
        {#if auto.checked_at}
          <div class="checked-at auto-checked">
            {$t('settings.update_auto_checked', { date: formatDate(auto.checked_at) })}
            {#if auto.error}·
              <span class="auto-error">{$t('settings.update_err_update_check_failed')}</span>
              <span class="progress-detail">{auto.error}</span>{/if}
          </div>
        {/if}
      </div>
    </Section>
  {/if}

  {#if notes.length > 0 && !busy}
    {@const from = info?.current_version || version}
    <Section
      title={$t('settings.update_whats_new')}
      description={notes.length > 1 && from && from !== '...'
        ? $tp('settings.update_whats_new_range', notes.length, {
            from: `v${from}`,
            to: `v${notes[0].version}`
          })
        : $t('settings.update_whats_new_single', { version: `v${notes[0].version}` })}
      testid="update-whats-new"
    >
      <div class="card card-rows whats-new">
        {#if digest.highlights.length > 0}
          {@const visible = showAllHighlights
            ? digest.highlights
            : digest.highlights.slice(0, HIGHLIGHTS_PREVIEW)}
          <div class="digest-block digest-highlights">
            <h3 class="digest-title">{$t('settings.update_highlights')}</h3>
            <ul class="highlight-list">
              {#each visible as line, i (i)}
                <li>
                  {#each splitInlineCode(line) as seg, k (k)}
                    {#if seg.code}<code>{seg.text}</code>{:else}{seg.text}{/if}
                  {/each}
                </li>
              {/each}
            </ul>
            {#if digest.highlights.length > HIGHLIGHTS_PREVIEW}
              <button
                type="button"
                class="digest-more"
                aria-expanded={showAllHighlights}
                onclick={() => (showAllHighlights = !showAllHighlights)}
              >
                {showAllHighlights
                  ? $t('settings.update_show_less')
                  : $t('settings.update_show_all', { count: digest.highlights.length })}
              </button>
            {/if}
          </div>
        {/if}

        {#if digest.groups.length === 0}
          <p class="digest-block release-empty">{$t('settings.update_no_notes')}</p>
        {:else}
          {#each digest.groups as group (group.kind)}
            {@const total = group.items.length + group.more}
            {@const expanded = !!expandedGroups[group.kind]}
            <details
              class="digest-block digest-group kind-{group.kind}"
              open={group.kind !== 'other'}
            >
              <summary class="group-head">
                <span class="group-title">{$t(KIND_TITLE[group.kind])}</span>
                <span class="group-count">{total}</span>
                <span class="group-chevron" aria-hidden="true"
                  ><Icon name="chevron-down" size={14} /></span
                >
              </summary>
              <ul class="change-list">
                {#each expanded ? group.items : group.items.slice(0, GROUP_PREVIEW) as item, j (j)}
                  <li>
                    <span class="item-text">
                      {#each splitInlineCode(item.text) as seg, k (k)}
                        {#if seg.code}<code>{seg.text}</code>{:else}{seg.text}{/if}
                      {/each}
                    </span>
                    {#if item.commit}
                      <a
                        class="commit-link"
                        href={item.commit.url}
                        target="_blank"
                        rel="noopener noreferrer"
                        aria-label={$t('settings.update_commit', { sha: item.commit.sha })}
                        >{item.commit.sha}</a
                      >
                    {/if}
                  </li>
                {/each}
              </ul>
              {#if group.items.length > GROUP_PREVIEW}
                <button
                  type="button"
                  class="digest-more"
                  aria-expanded={expanded}
                  onclick={() => (expandedGroups[group.kind] = !expanded)}
                >
                  {expanded
                    ? $t('settings.update_show_less')
                    : $t('settings.update_show_all', { count: group.items.length })}
                </button>
              {/if}
              {#if group.more > 0 && (expanded || group.items.length <= GROUP_PREVIEW)}
                <p class="group-more">{$tp('settings.update_more_items', group.more)}</p>
              {/if}
            </details>
          {/each}
        {/if}

        {#if compareUrl}
          <div class="digest-footer">
            <a class="release-link" href={compareUrl} target="_blank" rel="noopener noreferrer"
              >{notes.length > 1
                ? $t('settings.update_compare_link')
                : $t('settings.update_release_page')}</a
            >
          </div>
        {/if}
      </div>
    </Section>
  {/if}

  <Section
    title={$t('settings.update_backups_title')}
    description={$t('settings.update_backups_hint')}
  >
    <div class="card card-rows">
      {#if backups.length === 0}
        <div class="card-empty">{$t('settings.update_backups_empty')}</div>
      {:else}
        <div class="field-group">
          {#each backups as backup (backup.name)}
            <div class="field-row">
              <div>
                <span class="field-row-name mono"
                  >{backup.version
                    ? `v${backup.version}`
                    : $t('settings.backup_unknown_version')}</span
                >
                <div class="field-row-desc">
                  {formatDate(backup.created_at)} · {formatBytes(backup.size)}
                </div>
              </div>
              <button
                class="btn btn-secondary btn-sm"
                onclick={() => rollback(backup)}
                disabled={busy}
              >
                {$t('settings.rollback')}
              </button>
            </div>
          {/each}
        </div>
      {/if}
    </div>
  </Section>
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
  .mono {
    font-family: var(--font-mono, monospace);
    font-size: var(--font-size-xs);
  }

  .field-group {
    display: flex;
    flex-direction: column;
  }

  .field-row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 16px;
  }

  /* Сообщения и прогресс внутри карточки-списка — полосы на всю ширину
     между строками, без собственной рамки (не карточка в карточке). */
  .card-rows > .update-state,
  .card-rows > .update-available,
  .card-rows > .update-progress,
  .card-rows > .reconnect-overlay,
  .card-rows > .auto-checked {
    margin: 0;
    border: 0;
    border-top: 1px solid var(--border);
    border-radius: 0;
    padding: 14px var(--card-pad);
  }

  .field-row-name {
    font-size: var(--font-size-base);
    font-weight: 500;
    color: var(--fg-primary);
  }

  .field-row-val {
    font-size: var(--font-size-sm);
    color: var(--fg-secondary);
    text-align: right;
  }

  .field-row-desc {
    font-size: var(--font-size-xs);
    color: var(--fg-dim);
    margin-top: 2px;
  }

  .version-val {
    display: inline-flex;
    align-items: center;
    gap: 8px;
  }

  .checked-at {
    font-size: var(--font-size-xs);
    color: var(--fg-dim);
  }

  .update-state {
    margin-top: 14px;
    padding: 10px 12px;
    border-radius: var(--radius-md);
    font-size: var(--font-size-sm);
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

  .state-info {
    color: var(--fg-secondary);
    background: var(--surface-tint);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
  }

  .auto-status {
    margin-top: 16px;
    padding: 8px 16px;
    font-size: var(--font-size-sm);
    line-height: 1.5;
    overflow-wrap: anywhere;
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
    font-size: var(--font-size-lg);
    font-weight: 600;
    color: var(--fg-primary);
  }

  .available-meta {
    display: flex;
    flex-wrap: wrap;
    gap: 4px 14px;
    font-size: var(--font-size-xs);
    color: var(--fg-secondary);
  }

  .available-auto {
    margin-top: 4px;
    display: flex;
    gap: 8px;
    font-size: var(--font-size-xs);
    color: var(--fg-secondary);
    overflow-wrap: anywhere;
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
    font-size: var(--font-size-sm);
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
    transition: width var(--duration-slow) var(--ease-out);
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
    font-size: var(--font-size-xs);
    color: var(--fg-secondary);
    word-break: break-word;
  }

  /* Технический текст ошибки Go под переведённой причиной сбоя */
  .progress-detail {
    display: block;
    font-family: var(--font-family-mono);
    font-size: var(--font-size-2xs);
    color: var(--fg-secondary);
    overflow-wrap: anywhere;
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
    font-size: var(--font-size-base);
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
    font-size: var(--font-size-xs);
    color: var(--fg-secondary);
    text-align: center;
  }

  /* «Что нового»: сводка обновления. Главное — крупным текстом, ниже группы по типу
     изменений; цвет типа — только точка у заголовка и линия вдоль списка. */
  .digest-block {
    padding: 18px var(--card-pad);
  }

  .digest-block + .digest-block,
  .digest-footer {
    border-top: 1px solid var(--border);
  }

  .digest-title {
    margin: 0 0 12px;
    font-size: var(--font-size-sm);
    font-weight: 600;
    color: var(--fg-secondary);
  }

  .highlight-list {
    margin: 0;
    padding: 0;
    list-style: none;
    display: flex;
    flex-direction: column;
    gap: 10px;
  }

  .highlight-list li {
    position: relative;
    max-width: 68ch;
    padding-left: 18px;
    font-size: var(--font-size-lg);
    line-height: 1.45;
    color: var(--fg-primary);
    overflow-wrap: anywhere;
  }

  .highlight-list li::before {
    content: '';
    position: absolute;
    left: 0;
    top: 0.62em;
    width: 7px;
    height: 7px;
    border-radius: var(--radius-full);
    background: var(--accent);
    box-shadow: 0 0 0 3px var(--accent-soft);
  }

  .digest-group {
    --kind: var(--fg-dim);
  }

  .digest-group.kind-feat {
    --kind: var(--accent);
  }

  .digest-group.kind-fix {
    --kind: var(--success);
  }

  .group-head {
    display: flex;
    align-items: center;
    gap: 10px;
    margin: -6px -10px;
    padding: 6px 10px;
    border-radius: var(--radius-md);
    cursor: pointer;
    list-style: none;
    transition: background-color var(--duration-enter) var(--ease-out);
  }

  .group-head::-webkit-details-marker {
    display: none;
  }

  .group-head:hover {
    background: var(--hover);
  }

  .group-head:focus-visible {
    outline: 2px solid var(--border-focus);
    outline-offset: -2px;
  }

  .group-head::before {
    content: '';
    flex: none;
    width: 7px;
    height: 7px;
    border-radius: var(--radius-full);
    background: var(--kind);
  }

  .group-title {
    font-size: var(--font-size-base);
    font-weight: 600;
    color: var(--fg-primary);
  }

  .group-count {
    font-size: var(--font-size-xs);
    color: var(--fg-dim);
    font-variant-numeric: tabular-nums;
  }

  .group-chevron {
    display: inline-flex;
    margin-left: auto;
    color: var(--fg-dim);
    transition: transform var(--duration-move) var(--ease-out);
  }

  .digest-group[open] .group-chevron {
    transform: rotate(180deg);
  }

  .change-list {
    margin: 14px 0 0 3px;
    padding: 0 0 0 17px;
    list-style: none;
    display: flex;
    flex-direction: column;
    gap: 8px;
    border-left: 1px solid color-mix(in srgb, var(--kind) 35%, transparent);
  }

  .change-list li {
    display: flex;
    align-items: baseline;
    justify-content: space-between;
    gap: 16px;
    max-width: 80ch;
    font-size: var(--font-size-sm);
    line-height: 1.5;
    color: var(--fg-primary);
  }

  .item-text {
    min-width: 0;
    overflow-wrap: anywhere;
  }

  .whats-new code {
    font-family: var(--font-family-mono);
    font-size: 0.92em;
    padding: 1px 5px;
    border-radius: var(--radius-xs);
    background: var(--surface-tint);
  }

  .commit-link {
    flex: none;
    font-family: var(--font-family-mono);
    font-size: var(--font-size-2xs);
    color: var(--fg-dim);
    text-decoration: none;
    font-variant-numeric: tabular-nums;
    padding: 0 2px;
    border-radius: var(--radius-xs);
  }

  .commit-link:hover,
  .commit-link:focus-visible,
  .release-link:hover,
  .release-link:focus-visible {
    color: var(--accent-text);
    text-decoration: underline;
    text-underline-offset: 3px;
  }

  .digest-more {
    margin: 12px 0 0 20px;
    padding: 4px 0;
    border: 0;
    background: none;
    font: inherit;
    font-size: var(--font-size-sm);
    font-weight: 500;
    color: var(--accent-text);
    cursor: pointer;
  }

  .digest-highlights .digest-more {
    margin-left: 18px;
  }

  .digest-more:focus-visible {
    outline: 2px solid var(--border-focus);
    outline-offset: 2px;
    border-radius: var(--radius-xs);
  }

  .group-more {
    margin: 10px 0 0 20px;
    font-size: var(--font-size-xs);
    color: var(--fg-dim);
  }

  .digest-footer {
    padding: 14px var(--card-pad);
  }

  .release-link {
    font-size: var(--font-size-sm);
    font-weight: 500;
    color: var(--accent-text);
    text-decoration: none;
  }

  .release-empty {
    margin: 0;
    font-size: var(--font-size-sm);
    color: var(--fg-dim);
  }

  @media (prefers-reduced-motion: reduce) {
    .group-head,
    .group-chevron {
      transition: none;
    }
  }

  .auto-checked {
    display: block;
    margin-top: 12px;
  }

  .auto-error {
    color: var(--danger);
    word-break: break-word;
  }

  .confirm-text {
    margin: 0;
    font-size: var(--font-size-base);
    color: var(--fg-secondary);
    line-height: 1.5;
  }

  .modal-actions {
    display: flex;
    justify-content: flex-end;
    gap: 12px;
    margin-top: 16px;
  }

  @media (max-width: 480px) {
    .channel-row {
      flex-direction: column;
      align-items: flex-start;
    }

    .channel-row .field-row-val {
      text-align: left;
    }

    /* Хэши коммитов на телефоне — шум; полный список с ними — по ссылке на GitHub */
    .commit-link {
      display: none;
    }

    .highlight-list li {
      font-size: var(--font-size-base);
    }
  }
</style>
