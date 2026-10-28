<script lang="ts">
  import type { Snippet } from 'svelte';
  import { t, currentLang } from '../../../i18n';
  import { formatBytes } from '../../../lib/format';
  import Icon from '../../../lib/components/Icon.svelte';
  import type { SourceView, FileResultView } from '../../../lib/configGeodata';

  // Строка источника вкладки «Списки». Строка в целом не интерактивна: <li> без role и без
  // onclick, имя файла — отдельная кнопка, остальные действия — соседние кнопки. Статусы,
  // итог последнего запуска и пометки приходят от сервера готовыми, здесь только показ.
  let {
    source,
    lead,
    onopen,
    ondedup,
    onfinish,
    onedit,
    ondelete,
    onrestore
  }: {
    source: SourceView;
    /** Переключатель или иной элемент слева от имени. */
    lead?: Snippet;
    /** Имя файла открывает инспектор тегов. */
    onopen?: (source: SourceView) => void;
    /** «Убрать дубль». */
    ondedup?: (source: SourceView) => void;
    /** «Довести включение» или «Довести удаление»: режим выбирает вызывающий по состоянию строки. */
    onfinish?: (source: SourceView) => void;
    /** «Изменить ссылку»: у своих записей (в том числе вписанных вручную). */
    onedit?: (source: SourceView) => void;
    /** «Удалить источник»: у своих записей и файлов dat без записи. */
    ondelete?: (source: SourceView) => void;
    /** «Вернуть источник»: снимает пометку удаления или выключения из черновика. */
    onrestore?: (source: SourceView) => void;
  } = $props();

  const id = $derived(source.id);

  /** Текст причины: известный код словаря либо сам код. */
  function reasonText(code: string): string {
    const key = `cfg.geo.reason_${code}`;
    const text = $t(key);
    return text === key ? code : text;
  }

  /** День и месяц из RFC3339 как есть: время роутера, без пересчёта в пояс браузера. */
  function routerDate(iso: string | undefined): string {
    const m = /^(\d{4})-(\d{2})-(\d{2})/.exec(iso ?? '');
    if (m === null) return iso ?? '';
    const d = new Date(Number(m[1]), Number(m[2]) - 1, Number(m[3]));
    return d.toLocaleDateString($currentLang, { day: 'numeric', month: 'short' });
  }

  const meta = $derived(
    source.present
      ? $t('cfg.geo.meta', {
          size: formatBytes(source.size ?? 0),
          date: routerDate(source.mtime),
          type: source.kind,
          tags: String(source.tags ?? 0),
          records: String(source.entries ?? 0)
        })
      : ''
  );

  interface ResultChip {
    cls: string;
    text: string;
  }

  function resultChip(r: FileResultView): ResultChip {
    if (r.status === 'updated') return { cls: 'chip-success', text: $t('cfg.geo.res_updated') };
    if (r.status === 'unchanged') return { cls: 'chip-default', text: $t('cfg.geo.res_same') };
    const code = r.reason !== undefined && r.reason !== '' ? r.reason : 'failed_new';
    return { cls: 'chip-warning', text: $t('cfg.geo.res_kept', { reason: reasonText(code) }) };
  }

  const result = $derived(source.result !== undefined ? resultChip(source.result) : null);
  const unverified = $derived(
    source.result?.status === 'updated' && source.result.unverified === true
      ? $t(source.result.limit === true ? 'cfg.geo.res_unverified_limit' : 'cfg.geo.res_unverified')
      : ''
  );
  // Файла нет, запись есть и скачивание не ждёт: запись «висит» без файла
  const noFileYet = $derived(source.entry && !source.present && source.pending !== true);
  // Ссылка правится у записи geodata своего источника; у источника с пометкой черновика кнопки нет
  const canEdit = $derived(
    onedit !== undefined && source.origin === 'own' && source.entry && !source.draft
  );
  // Удаляется свой источник или файл без записи; у источника с пометкой кнопки нет
  const canDelete = $derived(
    ondelete !== undefined && (source.origin === 'own' || source.origin === 'file') && !source.draft
  );
  const canRestore = $derived(onrestore !== undefined && !!source.draft);
  const incomplete = $derived(
    source.delete_incomplete !== undefined && source.delete_incomplete !== ''
  );
  const showOrigin = $derived(source.origin === 'panel' || source.origin === 'own');
</script>

<li class="sr-row" data-testid="geo-source-{id}">
  {#if lead}
    <div class="sr-lead">{@render lead()}</div>
  {/if}

  <div class="sr-main">
    <div class="sr-name-line">
      {#if source.present}
        <button
          type="button"
          class="sr-name"
          data-testid="geo-source-open-{id}"
          aria-label={$t('cfg.geo.open', { file: source.file })}
          onclick={() => onopen?.(source)}
        >
          {source.file}
        </button>
      {:else}
        <span class="sr-name sr-name-off">{source.file}</span>
      {/if}
    </div>

    <div class="sr-chips">
      {#if result !== null}
        <span class="chip {result.cls}" data-testid="geo-source-status-{id}">{result.text}</span>
      {/if}
      {#if source.outside !== undefined && source.outside !== ''}
        <span class="chip chip-default" data-testid="geo-source-outside-{id}">
          {$t('cfg.geo.chip_outside', { date: routerDate(source.outside) })}
        </span>
      {/if}
      {#if source.pending}
        <span class="chip chip-default" data-testid="geo-source-pending-{id}">
          {$t('cfg.geo.chip_pending')}
        </span>
      {/if}
      {#if source.include_stuck}
        <span class="chip chip-warning">{$t('cfg.geo.chip_include_partial')}</span>
      {/if}
      {#if source.dup}
        <span class="chip chip-warning">{$t('cfg.geo.chip_dup')}</span>
      {:else if source.dup_url}
        <span class="chip chip-warning">{$t('cfg.geo.chip_dup_url')}</span>
      {/if}
      {#if source.draft === 'delete'}
        <span class="chip chip-default" data-testid="geo-source-draft-{id}">
          {$t('cfg.geo.chip_draft_delete')}
        </span>
      {:else if source.draft === 'off'}
        <span class="chip chip-default" data-testid="geo-source-draft-{id}">
          {$t('cfg.geo.chip_draft_off')}
        </span>
      {/if}
      {#if incomplete}
        <span class="chip chip-danger" data-testid="geo-source-incomplete-{id}">
          {$t('cfg.geo.chip_delete_incomplete', {
            reason: reasonText(source.delete_incomplete ?? '')
          })}
        </span>
      {/if}
      {#if noFileYet}
        <span class="chip chip-warning" data-testid="geo-source-nofile-{id}">
          {$t('cfg.geo.no_file')}
        </span>
      {/if}
      {#if source.invalid}
        <span class="chip chip-warning">{$t('cfg.geo.chip_invalid')}</span>
      {/if}
      {#if source.origin === 'panel'}
        <span class="chip chip-default">{$t('cfg.geo.chip_panel')}</span>
      {:else if source.origin === 'own'}
        <span class="chip chip-default">{$t('cfg.geo.chip_own')}</span>
      {/if}
    </div>

    {#if unverified !== ''}
      <p class="sr-hint">{unverified}</p>
    {/if}
    {#if source.dup}
      <p class="sr-hint">{$t('cfg.geo.dup_hint')}</p>
    {:else if source.dup_url}
      <p class="sr-hint">{$t('cfg.geo.dup_url_hint')}</p>
    {/if}
  </div>

  {#if meta !== ''}
    <div class="sr-meta" data-testid="geo-source-meta-{id}">{meta}</div>
  {:else if !source.present && !source.entry && !showOrigin}
    <div class="sr-meta sr-meta-off">{$t('cfg.geo.no_file')}</div>
  {/if}

  {#if canEdit || canDelete || canRestore || (source.dup && ondedup) || ((incomplete || source.include_stuck) && onfinish)}
    <div class="sr-actions">
      {#if canEdit}
        <button
          type="button"
          class="btn btn-secondary btn-sm sr-icon"
          data-testid="geo-source-edit-{id}"
          aria-label={$t('cfg.geo.edit_url')}
          title={$t('cfg.geo.edit_url')}
          onclick={() => onedit?.(source)}
        >
          <Icon name="edit" size={14} />
        </button>
      {/if}
      {#if canDelete}
        <button
          type="button"
          class="btn btn-secondary btn-sm sr-icon"
          data-testid="geo-source-delete-{id}"
          aria-label={$t('cfg.geo.delete')}
          title={$t('cfg.geo.delete')}
          onclick={() => ondelete?.(source)}
        >
          <Icon name="trash" size={14} />
        </button>
      {/if}
      {#if canRestore}
        <button
          type="button"
          class="btn btn-secondary btn-sm"
          data-testid="geo-source-restore-{id}"
          onclick={() => onrestore?.(source)}
        >
          {$t('cfg.geo.restore')}
        </button>
      {/if}
      {#if source.dup && ondedup}
        <button
          type="button"
          class="btn btn-secondary btn-sm"
          data-testid="geo-source-dedup-{id}"
          onclick={() => ondedup?.(source)}
        >
          {$t('cfg.geo.dup_fix')}
        </button>
      {/if}
      {#if (incomplete || source.include_stuck) && onfinish}
        <button
          type="button"
          class="btn btn-secondary btn-sm"
          data-testid="geo-source-finish-{id}"
          onclick={() => onfinish?.(source)}
        >
          {$t(incomplete ? 'cfg.geo.finish_delete' : 'cfg.geo.finish_include')}
        </button>
      {/if}
    </div>
  {/if}
</li>

<style>
  .sr-row {
    display: flex;
    flex-wrap: wrap;
    align-items: flex-start;
    gap: var(--spacing-2) var(--spacing-4);
    min-width: 0;
    padding: var(--spacing-2) 0;
    border-top: 1px solid var(--border);
    list-style: none;
  }

  .sr-lead {
    flex: 0 0 auto;
    min-width: 0;
  }

  .sr-main {
    display: flex;
    flex: 1 1 240px;
    flex-direction: column;
    gap: var(--spacing-1);
    min-width: 0;
  }

  .sr-name-line {
    min-width: 0;
  }

  .sr-name {
    margin: 0;
    padding: 0;
    border: none;
    background: none;
    font: inherit;
    font-size: var(--font-size-base);
    font-weight: 600;
    line-height: 1.5;
    color: var(--fg-primary);
    text-align: left;
    overflow-wrap: anywhere;
  }

  button.sr-name {
    cursor: pointer;
  }

  button.sr-name:hover,
  button.sr-name:focus-visible {
    color: var(--accent-text, var(--accent));
    text-decoration: underline;
  }

  .sr-name-off {
    color: var(--fg-dim);
  }

  .sr-chips {
    display: flex;
    flex-wrap: wrap;
    gap: var(--spacing-1);
    min-width: 0;
  }

  .sr-chips:empty {
    display: none;
  }

  .sr-chips :global(.chip) {
    white-space: normal;
    overflow-wrap: anywhere;
    text-align: left;
  }

  .sr-hint {
    margin: 0;
    font-size: var(--font-size-xs);
    line-height: 1.5;
    color: var(--fg-secondary);
    overflow-wrap: anywhere;
  }

  .sr-meta {
    flex: 0 1 auto;
    min-width: 0;
    font-family: var(--font-family-mono);
    font-size: var(--font-size-xs);
    line-height: 1.5;
    color: var(--fg-secondary);
    overflow-wrap: anywhere;
  }

  .sr-meta-off {
    font-family: inherit;
    color: var(--fg-dim);
  }

  .sr-actions {
    display: flex;
    flex-wrap: wrap;
    justify-content: flex-end;
    gap: var(--spacing-2);
    margin-left: auto;
  }

  .sr-actions :global(.sr-icon) {
    min-width: var(--btn-h, 32px);
    padding: 0;
    justify-content: center;
  }

  @media (max-width: 768px) {
    /* Мета переносится под имя, действия идут отдельной строкой справа */
    .sr-meta {
      flex: 1 1 100%;
    }

    .sr-actions {
      flex: 1 1 100%;
    }
  }

  @media (pointer: coarse), (max-width: 768px) {
    .sr-name {
      min-height: 44px;
      display: inline-flex;
      align-items: center;
    }

    .sr-actions :global(.btn) {
      min-height: 44px;
    }

    .sr-actions :global(.sr-icon) {
      min-width: 44px;
    }
  }
</style>
