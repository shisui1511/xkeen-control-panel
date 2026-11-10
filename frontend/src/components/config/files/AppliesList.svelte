<script lang="ts">
  import { untrack } from 'svelte';
  import { t, currentLang } from '../../../i18n';
  import Button from '../../Button.svelte';
  import EmptyState from '../../EmptyState.svelte';
  import Skeleton from '../../Skeleton.svelte';
  import DiffView from '../DiffView.svelte';
  import {
    fetchApplies,
    fetchApplyContent,
    type ApplyCopy,
    type LayerFile
  } from '../../../lib/configLayer';

  // «Последние применения» для файлов панели (D-06, CFG-03).
  // До 5 строк «Версия до применения от …» со сравнением с текущим файлом.
  // Кнопки отката нет: отката к отдельному применению для файла панели нет (D-06).
  let {
    file,
    current = ''
  }: {
    file: LayerFile;
    current?: string;
  } = $props();

  const MAX_ROWS = 5;

  let loading = $state(true);
  let error = $state('');
  let applies = $state<ApplyCopy[]>([]);
  let selectedId = $state<string | null>(null);
  let selectedContent = $state<string | null>(null);
  let selectedTruncated = $state(false);
  let contentLoading = $state(false);
  let contentError = $state('');
  // Кнопки строк по id копии: на них возвращается фокус при «Повторить». Нереактивны.
  const rowButtons: Record<string, HTMLButtonElement | null> = {};

  function formatDate(iso: string): string {
    try {
      const d = new Date(iso);
      if (isNaN(d.getTime())) return iso;
      return d.toLocaleString($currentLang === 'ru' ? 'ru-RU' : 'en-US');
    } catch {
      return iso;
    }
  }

  // Идентичность запросов (G149-02): список принадлежит activeKey и поколению
  // listSeq, содержимое — activeKey, selectedId и поколению contentSeq. Ответ,
  // ветвь catch и finally публикуют результат только при совпадении всех трёх,
  // поэтому поздний ответ прошлого файла или прошлой строки не меняет текущий
  // выбор, ошибку и индикатор загрузки. Счётчики и ключ нереактивны.
  let activeKey: string | null = null;
  let listSeq = 0;
  let contentSeq = 0;

  // Сбросить раскрытую строку и инвалидировать её незавершённое чтение.
  function clearSelection(): void {
    contentSeq++;
    selectedId = null;
    selectedContent = null;
    selectedTruncated = false;
    contentLoading = false;
    contentError = '';
  }

  // Новый файл не наследует ни список, ни раскрытую строку прошлого файла.
  function openFile(key: string): void {
    activeKey = key;
    listSeq++;
    clearSelection();
    applies = [];
    error = '';
    loading = true;
    void loadList(key);
  }

  // Чтение списка. Повтор для того же файла не стирает успешно загруженный
  // список до нового успеха (UI-SPEC: сбой фонового чтения не закрывает список).
  async function loadList(key: string): Promise<void> {
    const mine = ++listSeq;
    const owns = () => mine === listSeq && activeKey === key;
    loading = true;
    error = '';
    try {
      const res = await fetchApplies(key);
      if (!owns()) return;
      const next = res.applies ?? [];
      applies = next;
      if (selectedId !== null && !next.slice(0, MAX_ROWS).some((a) => a.id === selectedId)) {
        clearSelection();
      }
    } catch {
      // Текст сервера в строку не выводится: только фраза на языке панели
      if (owns()) error = $t('cfg.files.applies_error');
    } finally {
      if (owns()) loading = false;
    }
  }

  function retryList(): void {
    if (activeKey !== null) void loadList(activeKey);
  }

  // Чтение содержимого выбранной копии: key и id захватываются до await.
  async function loadContent(item: ApplyCopy): Promise<void> {
    const key = activeKey;
    const id = item.id;
    if (key === null || selectedId !== id) return;
    const mine = ++contentSeq;
    const owns = () => mine === contentSeq && activeKey === key && selectedId === id;
    selectedContent = null;
    selectedTruncated = false;
    contentError = '';
    contentLoading = true;
    try {
      const res = await fetchApplyContent(key, id);
      if (!owns()) return;
      selectedContent = res.content;
      selectedTruncated = res.truncated;
    } catch {
      if (owns()) contentError = $t('cfg.files.applies_error');
    } finally {
      if (owns()) contentLoading = false;
    }
  }

  // Выбор или сворачивание строки. Новый выбор сразу убирает прошлый diff.
  function toggleApply(item: ApplyCopy): void {
    if (selectedId === item.id) {
      clearSelection();
      return;
    }
    clearSelection();
    selectedId = item.id;
    void loadContent(item);
  }

  // «Повторить» в раскрытой строке: новое чтение того же id, строка остаётся раскрытой.
  function retrySelected(item: ApplyCopy, event: MouseEvent): void {
    if (activeKey === null || selectedId !== item.id) return;
    const retryButton = event.currentTarget;
    if (retryButton instanceof HTMLElement && retryButton === document.activeElement) {
      rowButtons[item.id]?.focus();
    }
    void loadContent(item);
  }

  // Зависимость эффекта — только ключ файла: снимок слоя присылает новый объект
  // файла с тем же ключом, и $derived гасит такое обновление. Чтение состояния
  // внутри загрузки не отслеживается. Cleanup (смена файла, уничтожение)
  // инвалидирует оба поколения.
  const fileKey = $derived(file.key);

  $effect(() => {
    const key = fileKey;
    untrack(() => openFile(key));
    return () => {
      activeKey = null;
      listSeq++;
      contentSeq++;
    };
  });
</script>

<div class="files-applies-list" data-testid="files-applies-list">
  <div class="applies-header">
    <h3 class="applies-title">{$t('cfg.files.applies_title')}</h3>
    <p class="applies-note">{$t('cfg.files.applies_note')}</p>
  </div>

  {#if error}
    <div class="alert alert-error applies-alert" role="alert">
      <span>{$t('cfg.files.applies_error')}</span>
      <Button variant="secondary" size="sm" {loading} onclick={retryList}>
        {$t('app.retry')}
      </Button>
    </div>
  {/if}

  {#if applies.length === 0}
    {#if loading}
      <div class="applies-loading" aria-busy="true">
        <Skeleton type="text-line" width="70%" />
        <Skeleton type="text-line" width="85%" />
        <Skeleton type="text-line" width="60%" />
      </div>
    {:else if !error}
      <div class="applies-empty">
        <EmptyState title={$t('editor.backups_empty')} description="" plain={true} />
      </div>
    {/if}
  {:else}
    <div class="applies-items">
      {#each applies.slice(0, MAX_ROWS) as item (item.id)}
        <div class={['apply-row', { active: selectedId === item.id }]}>
          <button
            type="button"
            class="apply-row-btn"
            bind:this={rowButtons[item.id]}
            aria-expanded={selectedId === item.id ? 'true' : 'false'}
            onclick={() => toggleApply(item)}
          >
            <span class="apply-row-title">
              {$t('cfg.files.apply_row', { date: formatDate(item.at) })}
            </span>
          </button>
          {#if selectedId === item.id}
            <div class="apply-diff-container">
              {#if contentLoading}
                <div class="diff-loading" role="status" aria-busy="true">
                  <span class="spinner" aria-hidden="true"></span>
                  <span class="sr-only">{$t('app.loading')}</span>
                </div>
              {:else if contentError}
                <div class="alert alert-error" role="alert">
                  <span>{contentError}</span>
                  <Button
                    variant="secondary"
                    size="sm"
                    onclick={(event) => retrySelected(item, event)}
                  >
                    {$t('app.retry')}
                  </Button>
                </div>
              {:else if selectedContent !== null}
                <DiffView
                  expected={current}
                  actual={selectedContent}
                  truncated={selectedTruncated}
                  caption={$t('cfg.files.apply_diff_caption', { date: formatDate(item.at) })}
                />
              {/if}
            </div>
          {/if}
        </div>
      {/each}
    </div>
  {/if}
</div>

<style>
  .files-applies-list {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-2);
    padding: var(--spacing-2) var(--spacing-3);
    height: 100%;
    overflow-y: auto;
    scrollbar-width: thin;
  }

  .applies-header {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-1);
  }

  .applies-title {
    margin: 0;
    font-size: var(--font-size-base);
    font-weight: 600;
    color: var(--fg-primary);
  }

  .applies-note {
    margin: 0;
    font-size: var(--font-size-xs);
    color: var(--fg-dim);
  }

  .applies-loading {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-2);
    padding: var(--spacing-2) 0;
  }

  .applies-alert {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--spacing-2);
    font-size: var(--font-size-xs);
  }

  .applies-empty {
    padding: var(--spacing-3) 0;
  }

  .applies-items {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-2);
  }

  .apply-row {
    display: flex;
    flex-direction: column;
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    background: var(--bg-card);
    overflow: hidden;
  }

  .apply-row.active {
    border-color: var(--border-strong);
  }

  .apply-row-btn {
    display: flex;
    align-items: center;
    justify-content: space-between;
    width: 100%;
    min-height: 40px;
    padding: var(--spacing-2) var(--spacing-3);
    background: transparent;
    border: none;
    color: var(--fg-primary);
    font-size: var(--font-size-xs);
    text-align: left;
    cursor: pointer;
    transition: background var(--transition-fast);
  }

  .apply-row-btn:hover {
    background: var(--bg-hover);
  }

  /* Строка обрезает содержимое по скруглению (overflow: hidden), поэтому кольцо фокуса
     рисуется внутри кнопки, а не снаружи, где его срезал бы край строки */
  .apply-row .apply-row-btn:focus-visible {
    outline-offset: -2px;
  }

  @media (max-width: 768px) {
    .apply-row-btn {
      min-height: 44px;
    }
  }

  .apply-diff-container {
    padding: var(--spacing-2);
    border-top: 1px solid var(--border);
    background: var(--bg-elevated);
  }

  .diff-loading {
    display: flex;
    justify-content: center;
    padding: var(--spacing-3);
  }
</style>
