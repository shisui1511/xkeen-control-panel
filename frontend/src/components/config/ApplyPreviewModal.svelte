<script lang="ts">
  import { untrack } from 'svelte';
  import { t, tp } from '../../i18n';
  import Modal from '../Modal.svelte';
  import Button from '../Button.svelte';
  import Badge from '../Badge.svelte';
  import EmptyState from '../EmptyState.svelte';
  import Skeleton from '../Skeleton.svelte';
  import Icon from '../../lib/components/Icon.svelte';
  import DiffView from './DiffView.svelte';
  import { conflictVisible, runningKernels } from '../../stores';
  import { handleLayerError, onLayerEvent } from '../../lib/configLayer';
  import {
    fetchApplyPreview,
    kernelTitle,
    rowText,
    PREVIEW_TAB_ORDER,
    SECRET_MASK,
    type ApplyPreview,
    type ChangeKind,
    type ChangeRow,
    type PreviewFile
  } from '../../lib/configPreview';

  // Окно «Изменения перед применением» (D-08…D-13, CFG-02). Не знает о разделе: открытие,
  // закрытие и запуск применения приходят пропсами, поэтому мастер первого запуска
  // вызывает это же окно (D-23, D-28). Применение идёт только по «Применить изменения»
  // с ревизией черновика из ответа предпросмотра. Отмены отдельной строки нет (D-13):
  // весь черновик сбрасывает «Сбросить» в полосе. Секреты маскирует сервер (D-11);
  // содержимое файлов выводится только текстовыми узлами через DiffView.
  let {
    open,
    onclose,
    onconfirm
  }: {
    open: boolean;
    onclose: () => void;
    /** Запуск применения с ревизией, которую показало окно. */
    onconfirm: (revision: number) => Promise<void>;
  } = $props();

  // Сумма строк обеих сторон, выше которой построчное сравнение в браузере не строится
  // (тот же предел, что у сравнения Редактора)
  const DIFF_LINE_LIMIT = 2000;

  let preview = $state<ApplyPreview | null>(null);
  let loading = $state(false);
  let failed = $state(false);
  let confirming = $state(false);
  // Черновик изменился, пока окно было открыто (событие draft или 409 draft_conflict)
  let stale = $state(false);
  let applyError = $state('');
  let filesOpen = $state(false);
  let expanded = $state<Record<string, boolean>>({});
  // Растёт с каждым чтением: устаревший ответ не затирает более свежий
  let seq = 0;

  /** Читает предпросмотр. markStale — перечитывание из-за изменившегося черновика. */
  async function load(markStale = false) {
    const mine = ++seq;
    loading = true;
    failed = false;
    try {
      const data = await fetchApplyPreview();
      if (mine !== seq) return;
      if (markStale && preview !== null && preview.draft_revision !== data.draft_revision) {
        stale = true;
      }
      preview = data;
    } catch {
      if (mine === seq) failed = true;
    } finally {
      if (mine === seq) loading = false;
    }
  }

  function reset() {
    seq++;
    preview = null;
    loading = false;
    failed = false;
    confirming = false;
    stale = false;
    applyError = '';
    filesOpen = false;
    expanded = {};
  }

  $effect(() => {
    if (open) untrack(() => void load());
    else untrack(reset);
  });

  // Пока окно открыто, событие draft (правка в другой вкладке, сброс) перечитывает список
  $effect(() => {
    if (!open) return;
    return onLayerEvent('draft', () => {
      if (!confirming) void load(true);
    });
  });

  const rows = $derived<ChangeRow[]>(preview?.changes ?? []);
  const empty = $derived(preview !== null && rows.length === 0);

  const groups = $derived(
    PREVIEW_TAB_ORDER.map((tab) => ({
      tab,
      rows: rows.filter((r) => r.tab === tab)
    })).filter((g) => g.rows.length > 0)
  );

  const KIND_TONE: Record<ChangeKind, 'success' | 'neutral' | 'danger'> = {
    added: 'success',
    changed: 'neutral',
    removed: 'danger'
  };

  function rowKey(row: ChangeRow): string {
    return `${row.section}|${row.entity}|${row.id}|${row.kind}`;
  }

  // Объявления перезапуска: остановленное ядро перезапуска не ждёт, правки вступят в силу
  // при запуске
  const notes = $derived.by(() => {
    if (preview === null || rows.length === 0) return [];
    const running: string[] = $runningKernels;
    return (preview.restart ?? []).map((n) => ({
      kernel: n.kernel,
      reason: n.reason,
      text: !running.includes(n.kernel)
        ? $t('cfg.sec.kernel_stopped')
        : n.reason === 'port'
          ? $t('cfg.sec.restart_xkeen')
          : n.reason === 'profile'
            ? $t('cfg.sec.restart_profile')
            : $t('cfg.sec.restart_tun')
    }));
  });

  const kernels = $derived((preview?.kernels ?? []).filter((k) => (k.files ?? []).length > 0));
  const moved = $derived(preview?.moved ?? []);
  const fileCount = $derived(kernels.reduce((n, k) => n + k.files.length, 0));

  // «Секреты скрыты»: сервер пометил изменившиеся секреты или в файлах есть маска
  const secretsHidden = $derived(
    preview !== null &&
      (preview.secrets_changed > 0 ||
        kernels.some((k) =>
          k.files.some((f) => f.before.includes(SECRET_MASK) || f.after.includes(SECRET_MASK))
        ))
  );

  function lineCount(s: string): number {
    return s === '' ? 0 : s.split('\n').length;
  }

  function tooLarge(f: PreviewFile): boolean {
    if (f.truncated) return true;
    if (f.missing || f.action === 'delete') return false;
    return lineCount(f.before) + lineCount(f.after) > DIFF_LINE_LIMIT;
  }

  function fileId(kernel: string, f: PreviewFile): string {
    return `${kernel}:${f.key}`;
  }

  function toggleFile(id: string) {
    expanded = { ...expanded, [id]: !expanded[id] };
  }

  // Причина, по которой «Применить» сейчас откажет: конфликт ядер — по стору (мог
  // возникнуть при открытом окне, D-18), остальное — из ответа предпросмотра
  const blockedText = $derived.by(() => {
    if ($conflictVisible) return $t('cfg.apply_blocked_conflict');
    const b = preview?.blocked;
    if (!b) return '';
    switch (b.code) {
      case 'drift_blocked':
        return $t('cfg.apply_blocked_drift', { k: b.count });
      case 'checks_failed':
        return $tp('cfg.routing.apply_blocked', b.count);
      case 'geodata_busy':
        return $t('cfg.geo.ug_busy');
      default:
        return $t('cfg.routing.apply_busy_hint');
    }
  });

  const canConfirm = $derived(
    preview !== null &&
      !loading &&
      !confirming &&
      rows.length > 0 &&
      blockedText === '' &&
      !$conflictVisible
  );

  function close() {
    if (confirming) return;
    onclose();
  }

  async function confirm() {
    if (!canConfirm || preview === null || $conflictVisible) return;
    confirming = true;
    applyError = '';
    try {
      await onconfirm(preview.draft_revision);
    } catch (err) {
      confirming = false;
      if ((err as { code?: string } | null)?.code === 'draft_conflict') {
        // Устаревшая ревизия: список перечитывается, окно остаётся открытым
        stale = true;
        void load();
        return;
      }
      handleLayerError(err);
      applyError = err instanceof Error ? err.message : String(err);
      return;
    }
    confirming = false;
    onclose();
  }
</script>

<Modal
  {open}
  title={$t('cfg.preview.title')}
  maxWidth="720px"
  class="apply-preview"
  testid="apply-preview-modal"
  onclose={close}
>
  <div class="ap-body">
    {#if stale}
      <div class="alert alert-warning ap-alert" role="status" data-testid="apply-preview-stale">
        <Icon name="info" size={16} class="ap-alert-icon" />
        <span>{$t('cfg.preview.stale')}</span>
      </div>
    {/if}

    {#if failed}
      <div class="alert alert-error ap-alert" role="alert" data-testid="apply-preview-error">
        <Icon name="warning" size={16} class="ap-alert-icon" />
        <span>{$t('cfg.preview.error')}</span>
      </div>
      <div class="ap-retry">
        <Button variant="secondary" data-testid="apply-preview-retry" onclick={() => void load()}>
          {$t('cfg.preview.retry')}
        </Button>
      </div>
    {/if}

    {#if preview === null}
      {#if loading}
        <div class="ap-skeleton" aria-hidden="true" data-testid="apply-preview-loading">
          <Skeleton type="text-line" width="160px" />
          <Skeleton type="rect" height="24px" />
          <Skeleton type="rect" height="24px" />
          <Skeleton type="rect" height="24px" />
        </div>
      {/if}
    {:else if empty}
      <EmptyState
        plain
        title={$t('cfg.preview.empty_title')}
        description={$t('cfg.preview.empty_body')}
      />
    {:else}
      <div class="ap-summary">
        <span class="ap-count" data-testid="apply-preview-count">
          {$tp('cfg.preview.count', rows.length)}
        </span>
        {#if secretsHidden}
          <Badge tone="neutral" testid="apply-preview-secrets">
            {$t('cfg.preview.secrets_hidden')}
          </Badge>
        {/if}
      </div>

      <section class="ap-section" aria-labelledby="apply-preview-what">
        <h3 id="apply-preview-what" class="ap-heading">{$t('cfg.preview.what_changes')}</h3>
        <ul class="ap-list" data-testid="apply-preview-list">
          {#each groups as group (group.tab)}
            <li class="ap-group" data-testid="apply-preview-group-{group.tab}">
              <h3 class="ap-group-title">
                {$t(`cfg.preview.group.${group.tab}`)} ({group.rows.length})
              </h3>
              <ul class="ap-rows">
                {#each group.rows as row (rowKey(row))}
                  <li class="ap-row" data-testid="apply-preview-row-{row.kind}-{row.id}">
                    <Badge tone={KIND_TONE[row.kind] ?? 'neutral'}>
                      {$t(`cfg.preview.kind.${row.kind}`)}
                    </Badge>
                    <span class="ap-row-text">{rowText(row, $t)}</span>
                  </li>
                {/each}
              </ul>
            </li>
          {/each}
        </ul>
      </section>

      {#if notes.length > 0}
        <div class="ap-notes" data-testid="apply-preview-notes">
          {#each notes as note (note.kernel + ':' + note.reason)}
            <div
              class="alert alert-warning ap-alert"
              role="status"
              data-testid="draft-restart-note"
              data-reason={note.reason}
              data-kernel={note.kernel}
            >
              <Icon name="info" size={16} class="ap-alert-icon" />
              <span>{note.text}</span>
            </div>
          {/each}
        </div>
      {/if}

      {#if moved.length > 0}
        <ul class="ap-moved" data-testid="apply-preview-moved">
          {#each moved as m (m.kernel + ':' + m.path)}
            <li class="ap-moved-row">{$t('cfg.preview.moved', { path: m.path })}</li>
          {/each}
        </ul>
      {/if}

      {#if fileCount > 0}
        <section class="ap-section ap-files">
          <h3 class="ap-heading">
            <button
              type="button"
              class="ap-files-toggle"
              aria-expanded={filesOpen}
              aria-controls="apply-preview-files"
              data-testid="apply-preview-files-toggle"
              onclick={() => (filesOpen = !filesOpen)}
            >
              <Icon name={filesOpen ? 'chevron-down' : 'chevron-right'} size={16} />
              <span>{$t('cfg.preview.files')} ({fileCount})</span>
            </button>
          </h3>
          {#if filesOpen}
            <div id="apply-preview-files" class="ap-kernels">
              {#each kernels as k (k.kernel)}
                <div class="ap-kernel" data-testid="apply-preview-kernel-{k.kernel}">
                  <p class="ap-kernel-title">
                    {#if k.active}
                      {$t('cfg.preview.kernel_active', { kernel: kernelTitle(k.kernel) })}
                    {:else}
                      <span>{kernelTitle(k.kernel)}</span>
                      {#if k.deferred}
                        <Badge tone="neutral">{$t('cfg.preview.kernel_deferred')}</Badge>
                      {/if}
                    {/if}
                  </p>
                  <ul class="ap-file-list">
                    {#each k.files as f (f.key)}
                      {@const id = fileId(k.kernel, f)}
                      {@const big = tooLarge(f)}
                      <li class="ap-file" data-testid="apply-preview-file-{f.key}">
                        {#if big}
                          <div class="ap-file-head">
                            <span class="ap-path">{f.path}</span>
                            <span class="ap-stats">
                              {$t('cfg.preview.too_large', { a: f.added, r: f.removed })}
                            </span>
                          </div>
                        {:else}
                          <button
                            type="button"
                            class="ap-file-head ap-file-btn"
                            aria-expanded={!!expanded[id]}
                            onclick={() => toggleFile(id)}
                          >
                            <Icon
                              name={expanded[id] ? 'chevron-down' : 'chevron-right'}
                              size={12}
                            />
                            <span class="ap-path">{f.path}</span>
                            <span class="ap-stats">
                              {f.action === 'delete'
                                ? $t('cfg.preview.file_delete')
                                : $t('cfg.preview.file_stats', { a: f.added, r: f.removed })}
                            </span>
                          </button>
                        {/if}
                        {#if f.secrets_changed > 0}
                          <Badge tone="warning" variant="tag">
                            {$t('cfg.preview.secret_changed')}
                          </Badge>
                        {/if}
                        {#if !big && expanded[id]}
                          <div class="ap-diff">
                            <DiffView
                              id="apply-preview-diff-{k.kernel}-{f.key}"
                              actual={f.before}
                              expected={f.after}
                              missing={f.missing}
                              truncated={f.truncated}
                            />
                          </div>
                        {/if}
                      </li>
                    {/each}
                  </ul>
                </div>
              {/each}
            </div>
          {/if}
        </section>
      {/if}
    {/if}

    {#if applyError !== ''}
      <div class="alert alert-error ap-alert" role="alert" data-testid="apply-preview-apply-error">
        <Icon name="warning" size={16} class="ap-alert-icon" />
        <span>{applyError}</span>
      </div>
    {/if}

    <div class="ap-footer">
      {#if blockedText !== '' && preview !== null && !empty}
        <p class="ap-blocked" data-testid="apply-preview-blocked">{blockedText}</p>
      {/if}
      <div class="ap-actions">
        <Button
          variant="secondary"
          class="ap-btn"
          data-testid="apply-preview-cancel"
          disabled={confirming}
          onclick={close}
        >
          {$t('cfg.preview.cancel')}
        </Button>
        <Button
          variant="primary"
          class="ap-btn"
          data-testid="apply-preview-confirm"
          disabled={!canConfirm}
          loading={confirming}
          title={blockedText !== '' ? blockedText : undefined}
          onclick={confirm}
        >
          {$t('cfg.preview.confirm')}
        </Button>
      </div>
    </div>
  </div>
</Modal>

<style>
  /* Поля окна по шкале; низ без отступа — его даёт липкий футер */
  :global(.modal-container.apply-preview .modal-content) {
    padding: var(--spacing-4) var(--spacing-6) 0;
  }

  .ap-body {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-4);
    min-width: 0;
  }

  .ap-alert {
    margin: 0;
    min-width: 0;
    line-height: 1.5;
    overflow-wrap: anywhere;
  }

  .ap-alert :global(.ap-alert-icon) {
    flex-shrink: 0;
    margin-top: var(--spacing-0-5);
  }

  .ap-retry {
    display: flex;
  }

  .ap-skeleton {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-2);
  }

  .ap-summary {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--spacing-2);
  }

  .ap-count {
    font-size: var(--font-size-xs);
    color: var(--fg-dim);
  }

  .ap-section {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-2);
    min-width: 0;
  }

  .ap-heading {
    margin: 0;
    font-size: var(--font-size-lg);
    font-weight: 600;
    line-height: 1.2;
    color: var(--fg-primary);
  }

  .ap-list,
  .ap-rows,
  .ap-file-list,
  .ap-moved {
    display: flex;
    flex-direction: column;
    margin: 0;
    padding: 0;
    list-style: none;
    min-width: 0;
  }

  .ap-list {
    gap: var(--spacing-6);
  }

  .ap-rows,
  .ap-file-list,
  .ap-moved {
    gap: var(--spacing-2);
  }

  .ap-group-title {
    margin: 0 0 var(--spacing-2);
    font-size: var(--font-size-base);
    font-weight: 600;
    line-height: 1.2;
    color: var(--fg-primary);
  }

  .ap-row {
    display: flex;
    align-items: flex-start;
    gap: var(--spacing-2);
    min-width: 0;
    line-height: 1.5;
  }

  .ap-row-text,
  .ap-moved-row {
    min-width: 0;
    overflow-wrap: anywhere;
  }

  .ap-moved-row {
    color: var(--fg-secondary);
    line-height: 1.5;
  }

  .ap-notes {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-2);
  }

  .ap-files-toggle {
    display: inline-flex;
    align-items: center;
    gap: var(--spacing-2);
    min-height: 44px;
    padding: 0;
    background: transparent;
    border: 0;
    color: inherit;
    font: inherit;
    cursor: pointer;
    text-align: left;
  }

  .ap-files-toggle:focus-visible,
  .ap-file-btn:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: 2px;
    border-radius: var(--radius-sm);
  }

  .ap-kernels {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-4);
  }

  .ap-kernel {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-2);
    min-width: 0;
  }

  .ap-kernel-title {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--spacing-2);
    margin: 0;
    font-weight: 600;
    color: var(--fg-primary);
  }

  .ap-file {
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    gap: var(--spacing-1);
    min-width: 0;
  }

  .ap-file-head {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--spacing-2);
    width: 100%;
    min-width: 0;
  }

  .ap-file-btn {
    min-height: 40px;
    padding: 0;
    background: transparent;
    border: 0;
    color: inherit;
    font: inherit;
    cursor: pointer;
    text-align: left;
  }

  .ap-path {
    min-width: 0;
    font-family: var(--font-family-mono);
    font-size: var(--font-size-xs);
    color: var(--fg-primary);
    overflow-wrap: anywhere;
  }

  .ap-stats {
    font-size: var(--font-size-xs);
    color: var(--fg-dim);
  }

  .ap-diff {
    width: 100%;
    min-width: 0;
  }

  .ap-diff :global(.diff-body) {
    max-height: 320px;
  }

  /* Липкий футер: кнопки на виду при прокрутке листа */
  .ap-footer {
    position: sticky;
    bottom: 0;
    z-index: 1;
    display: flex;
    flex-direction: column;
    gap: var(--spacing-2);
    margin: 0 calc(-1 * var(--spacing-6));
    padding: var(--spacing-4) var(--spacing-6) calc(var(--spacing-4) + env(safe-area-inset-bottom));
    background: var(--bg-card);
    border-top: 1px solid var(--border);
  }

  .ap-blocked {
    margin: 0;
    color: var(--warning);
    line-height: 1.5;
    overflow-wrap: anywhere;
  }

  .ap-actions {
    display: flex;
    flex-wrap: wrap;
    justify-content: flex-end;
    gap: var(--spacing-2);
  }

  @media (max-width: 768px) {
    :global(.modal-container.apply-preview .modal-content) {
      padding: var(--spacing-3) var(--spacing-4) 0;
    }

    .ap-footer {
      margin: 0 calc(-1 * var(--spacing-4));
      padding-left: var(--spacing-4);
      padding-right: var(--spacing-4);
    }

    .ap-diff :global(.diff-body) {
      max-height: 240px;
    }

    .ap-actions :global(.ap-btn) {
      flex: 1;
      min-height: 44px;
      white-space: normal;
    }
  }

  @media (max-width: 480px) {
    /* Друг под другом во всю ширину, «Применить изменения» сверху */
    .ap-actions {
      flex-direction: column-reverse;
    }

    .ap-actions :global(.ap-btn) {
      flex: 0 0 auto;
      width: 100%;
    }
  }
</style>
