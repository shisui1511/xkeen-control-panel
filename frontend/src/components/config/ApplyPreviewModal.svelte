<script lang="ts">
  import { untrack } from 'svelte';
  import { t, tp } from '../../i18n';
  import Modal from '../Modal.svelte';
  import Button from '../Button.svelte';
  import Badge from '../Badge.svelte';
  import { handleLayerError } from '../../lib/configLayer';
  import {
    fetchApplyPreview,
    rowText,
    PREVIEW_TAB_ORDER,
    type ApplyPreview,
    type ChangeKind,
    type ChangeRow
  } from '../../lib/configPreview';

  // Окно «Изменения перед применением» (D-08…D-13, CFG-02). Не знает о разделе: открытие,
  // закрытие и запуск применения приходят пропсами, поэтому мастер первого запуска
  // вызывает это же окно (D-23, D-28). Применение идёт только по «Применить изменения»
  // с ревизией черновика из ответа предпросмотра.
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

  let preview = $state<ApplyPreview | null>(null);
  let loading = $state(false);
  let failed = $state(false);
  let confirming = $state(false);
  // Растёт с каждым чтением: устаревший ответ не затирает более свежий
  let seq = 0;

  async function load() {
    const mine = ++seq;
    loading = true;
    failed = false;
    try {
      const data = await fetchApplyPreview();
      if (mine === seq) preview = data;
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
  }

  $effect(() => {
    if (open) untrack(load);
    else untrack(reset);
  });

  const groups = $derived.by(() => {
    const rows = preview?.changes ?? [];
    return PREVIEW_TAB_ORDER.map((tab) => ({
      tab,
      rows: rows.filter((r) => r.tab === tab)
    })).filter((g) => g.rows.length > 0);
  });

  const KIND_TONE: Record<ChangeKind, 'success' | 'neutral' | 'danger'> = {
    added: 'success',
    changed: 'neutral',
    removed: 'danger'
  };

  function rowKey(row: ChangeRow): string {
    return `${row.section}|${row.entity}|${row.id}|${row.kind}`;
  }

  const canConfirm = $derived(
    preview !== null && !loading && !confirming && (preview.changes?.length ?? 0) > 0
  );

  function close() {
    if (confirming) return;
    onclose();
  }

  async function confirm() {
    if (!canConfirm || preview === null) return;
    confirming = true;
    try {
      await onconfirm(preview.draft_revision);
    } catch (err) {
      handleLayerError(err);
      confirming = false;
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
  testid="apply-preview-modal"
  onclose={close}
>
  <div class="ap-body">
    {#if preview}
      <p class="ap-count" data-testid="apply-preview-count">
        {$tp('cfg.preview.count', preview.count)}
      </p>
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
    {/if}

    <div class="ap-footer">
      <Button
        variant="secondary"
        data-testid="apply-preview-cancel"
        disabled={confirming}
        onclick={close}
      >
        {$t('cfg.preview.cancel')}
      </Button>
      <Button
        variant="primary"
        data-testid="apply-preview-confirm"
        disabled={!canConfirm}
        loading={confirming}
        onclick={confirm}
      >
        {$t('cfg.preview.confirm')}
      </Button>
    </div>
  </div>
</Modal>

<style>
  .ap-body {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-4);
    min-width: 0;
  }

  .ap-count {
    margin: 0;
    font-size: var(--font-size-xs);
    color: var(--fg-dim);
  }

  .ap-list,
  .ap-rows {
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

  .ap-rows {
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

  .ap-row-text {
    min-width: 0;
    overflow-wrap: anywhere;
  }

  .ap-footer {
    display: flex;
    flex-wrap: wrap;
    justify-content: flex-end;
    gap: var(--spacing-2);
  }
</style>
