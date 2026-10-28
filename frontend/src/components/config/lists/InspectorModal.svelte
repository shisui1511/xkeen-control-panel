<script lang="ts">
  import { tick, untrack } from 'svelte';
  import { t, tp } from '../../../i18n';
  import Modal from '../../Modal.svelte';
  import Button from '../../Button.svelte';
  import Icon from '../../../lib/components/Icon.svelte';
  import {
    fetchEntries,
    fetchTags,
    type ListRef,
    type TagInfo,
    type TagsView
  } from '../../../lib/configGeodata';
  import { listKind, reasonOf } from './reason';

  // Инспектор файла dat (D-25), две ступени в одном окне: «Теги» (поиск, строки «тег ·
  // записей», у строки «Открыть записи» и «Создать правило в черновике») и «Записи тега»
  // (поиск, строки записей по 100, «Показать ещё записи», «Назад к тегам»). Теги и записи
  // читает сервер; интерфейс ничего не разбирает. Записи dat выводятся только текстовой
  // интерполяцией, без разметки. Окно только читает: файл и xkeen.json не меняются.
  const PAGE = 100;
  // Файл geosite бывает на полторы тысячи тегов: больше строк за раз не рисуется, остальное
  // находится поиском
  const MAX_TAG_ROWS = 300;
  const ENTRY_DEBOUNCE_MS = 300;

  let {
    open,
    file,
    onclose,
    onmakerule
  }: {
    open: boolean;
    file: string;
    onclose: () => void;
    /** Создать правило с этим списком: окно закрывает вызывающий. */
    onmakerule: (ref: ListRef) => void;
  } = $props();

  const uid = $props.id();

  let stage = $state<'tags' | 'entries'>('tags');
  let tagsView = $state<TagsView | null>(null);
  let tagsLoading = $state(false);
  let tagsError = $state('');
  let tagQuery = $state('');

  let tag = $state('');
  let entries = $state<string[]>([]);
  let total = $state(0);
  let more = $state(false);
  let entryQuery = $state('');
  let entriesLoading = $state(false);
  let entriesError = $state('');

  let headingEl = $state<HTMLElement | undefined>();
  // Устаревшие ответы (смена файла, быстрый ввод) отбрасываются по номеру запроса
  let tagsSeq = 0;
  let entriesSeq = 0;
  let timer: ReturnType<typeof setTimeout> | null = null;

  function clearTimer() {
    if (timer !== null) {
      clearTimeout(timer);
      timer = null;
    }
  }

  async function loadTags() {
    const seq = ++tagsSeq;
    tagsLoading = true;
    tagsError = '';
    try {
      const view = await fetchTags(file);
      if (seq !== tagsSeq) return;
      tagsView = view;
    } catch (err) {
      if (seq !== tagsSeq) return;
      tagsView = null;
      tagsError = reasonOf(err);
    } finally {
      if (seq === tagsSeq) tagsLoading = false;
    }
  }

  // Каждое открытие начинает с тегов файла
  $effect(() => {
    if (!open) {
      untrack(() => {
        clearTimer();
        tagsSeq++;
        entriesSeq++;
      });
      return;
    }
    void file;
    untrack(() => {
      clearTimer();
      stage = 'tags';
      tagsView = null;
      tagQuery = '';
      tag = '';
      entries = [];
      entryQuery = '';
      entriesError = '';
      void loadTags();
    });
  });

  const allTags = $derived<TagInfo[]>(tagsView?.tags ?? []);
  const filteredTags = $derived.by(() => {
    const q = tagQuery.trim().toLowerCase();
    return q === '' ? allTags : allTags.filter((x) => x.tag.toLowerCase().includes(q));
  });
  const shownTags = $derived(filteredTags.slice(0, MAX_TAG_ROWS));
  const kind = $derived(listKind(tagsView?.kind ?? ''));

  async function focusHeading() {
    await tick();
    headingEl?.focus();
  }

  async function loadEntries(offset: number) {
    const seq = ++entriesSeq;
    entriesLoading = true;
    entriesError = '';
    try {
      const page = await fetchEntries(file, tag, entryQuery.trim(), offset, PAGE);
      if (seq !== entriesSeq) return;
      entries = offset === 0 ? page.entries : [...entries, ...page.entries];
      total = page.total;
      more = page.more;
    } catch (err) {
      if (seq !== entriesSeq) return;
      if (offset === 0) entries = [];
      more = false;
      entriesError = reasonOf(err);
    } finally {
      if (seq === entriesSeq) entriesLoading = false;
    }
  }

  function openEntries(name: string) {
    clearTimer();
    tag = name;
    entries = [];
    total = 0;
    more = false;
    entryQuery = '';
    stage = 'entries';
    void loadEntries(0);
    void focusHeading();
  }

  function backToTags() {
    clearTimer();
    entriesSeq++;
    entriesLoading = false;
    stage = 'tags';
    void focusHeading();
  }

  function onEntryInput() {
    clearTimer();
    timer = setTimeout(() => {
      timer = null;
      void loadEntries(0);
    }, ENTRY_DEBOUNCE_MS);
  }

  function makeRule(name: string) {
    onmakerule({ file, tag: name, kind });
  }
</script>

<Modal
  isOpen={open}
  title={$t('cfg.geo.insp_title', { file })}
  maxWidth="720px"
  dataTestid="geo-inspector"
  {onclose}
>
  <div class="im-body">
    {#if stage === 'tags'}
      <h3 class="im-heading" tabindex="-1" bind:this={headingEl} data-testid="geo-inspector-tags">
        {$t('cfg.geo.insp_tags')}
      </h3>

      <div class="form-group im-search">
        <label class="form-label" for="{uid}-tag-q">{$t('cfg.geo.insp_search_tag')}</label>
        <input
          id="{uid}-tag-q"
          type="text"
          class="input"
          autocomplete="off"
          spellcheck="false"
          placeholder={$t('cfg.geo.insp_search_tag')}
          disabled={tagsView === null}
          data-testid="geo-tag-search"
          bind:value={tagQuery}
        />
      </div>

      <div class="im-area" aria-busy={tagsLoading}>
        {#if tagsLoading}
          <div class="im-state" role="status" data-testid="geo-inspector-loading">
            <span class="spinner" aria-hidden="true"></span>
            <span>{$t('cfg.geo.insp_loading')}</span>
          </div>
        {:else if tagsError !== ''}
          <div class="alert alert-error im-alert" role="alert" data-testid="geo-inspector-error">
            <Icon name="warning" size={16} class="im-alert-icon" />
            <span class="im-alert-text">{$t('cfg.geo.insp_failed', { reason: tagsError })}</span>
            <Button
              variant="secondary"
              class="btn-sm"
              data-testid="geo-inspector-retry"
              onclick={() => void loadTags()}
            >
              {$t('cfg.nodes.retry')}
            </Button>
          </div>
        {:else if shownTags.length === 0}
          <p class="im-empty" data-testid="geo-inspector-empty">{$t('cfg.geo.insp_no_tags')}</p>
        {:else}
          <ul class="im-list" aria-label={$t('cfg.geo.insp_tags')} data-testid="geo-tag-list">
            {#each shownTags as x (x.tag)}
              <li class="im-row" data-testid="geo-tag-{x.tag}">
                <span class="im-row-text"
                  >{$tp('cfg.geo.insp_tag_row', x.count, { tag: x.tag })}</span
                >
                <span class="im-row-actions">
                  <Button
                    variant="secondary"
                    class="btn-sm"
                    data-testid="geo-open-entries"
                    onclick={() => openEntries(x.tag)}
                  >
                    {$t('cfg.geo.insp_open_entries')}
                    <span class="sr-only">{x.tag}</span>
                  </Button>
                  <Button
                    variant="secondary"
                    class="btn-sm"
                    data-testid="geo-make-rule"
                    onclick={() => makeRule(x.tag)}
                  >
                    {$t('cfg.geo.insp_make_rule')}
                    <span class="sr-only">{x.tag}</span>
                  </Button>
                </span>
              </li>
            {/each}
          </ul>
          {#if filteredTags.length > shownTags.length}
            <p class="im-note" data-testid="geo-tag-capped">
              {$t('cfg.geo.insp_tags_capped', {
                shown: shownTags.length,
                total: filteredTags.length
              })}
            </p>
          {/if}
        {/if}
      </div>
    {:else}
      <div class="im-head">
        <h3
          class="im-heading"
          tabindex="-1"
          bind:this={headingEl}
          data-testid="geo-inspector-entries"
        >
          {$t('cfg.geo.insp_entries', { tag })}
        </h3>
        <Button
          variant="secondary"
          class="btn-sm"
          data-testid="geo-make-rule"
          onclick={() => makeRule(tag)}
        >
          {$t('cfg.geo.insp_make_rule')}
        </Button>
      </div>

      <div class="form-group im-search">
        <label class="form-label" for="{uid}-entry-q">{$t('cfg.geo.insp_search_entry')}</label>
        <input
          id="{uid}-entry-q"
          type="text"
          class="input"
          autocomplete="off"
          spellcheck="false"
          placeholder={$t('cfg.geo.insp_search_entry')}
          data-testid="geo-entry-search"
          bind:value={entryQuery}
          oninput={onEntryInput}
        />
      </div>

      <div class="im-area" aria-busy={entriesLoading}>
        {#if entriesError !== ''}
          <div class="alert alert-error im-alert" role="alert" data-testid="geo-inspector-error">
            <Icon name="warning" size={16} class="im-alert-icon" />
            <span class="im-alert-text">{$t('cfg.geo.insp_failed', { reason: entriesError })}</span>
            <Button
              variant="secondary"
              class="btn-sm"
              data-testid="geo-inspector-retry"
              onclick={() => void loadEntries(0)}
            >
              {$t('cfg.nodes.retry')}
            </Button>
          </div>
        {:else if entriesLoading && entries.length === 0}
          <div class="im-state" role="status" data-testid="geo-inspector-loading">
            <span class="spinner" aria-hidden="true"></span>
            <span>{$t('cfg.geo.insp_loading')}</span>
          </div>
        {:else if entries.length === 0}
          <p class="im-empty" data-testid="geo-inspector-empty">{$t('cfg.geo.insp_no_tags')}</p>
        {:else}
          <p class="im-note" data-testid="geo-entry-total">{$tp('cfg.list.entries', total)}</p>
          <!-- svelte-ignore a11y_no_noninteractive_tabindex -->
          <ul
            class="im-list im-entries"
            tabindex="0"
            aria-label={$t('cfg.geo.insp_entries', { tag })}
            data-testid="geo-entry-list"
          >
            {#each entries as e, i (i)}
              <li class="im-entry" data-testid="geo-entry">{e}</li>
            {/each}
          </ul>
          {#if more}
            <div class="im-more">
              <Button
                variant="secondary"
                class="btn-sm"
                loading={entriesLoading}
                data-testid="geo-entry-more"
                onclick={() => void loadEntries(entries.length)}
              >
                {$t('cfg.geo.insp_more')}
              </Button>
            </div>
          {/if}
        {/if}
      </div>

      <div class="im-foot">
        <Button
          variant="secondary"
          class="btn-sm"
          data-testid="geo-inspector-back"
          onclick={backToTags}
        >
          {$t('cfg.geo.insp_back')}
        </Button>
      </div>
    {/if}
  </div>
</Modal>

<style>
  .im-body {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-4);
    min-width: 0;
  }

  .im-body :global(.form-group) {
    margin-bottom: 0;
  }

  .im-heading {
    margin: 0;
    font-size: var(--font-size-lg);
    font-weight: 600;
    line-height: 1.2;
    color: var(--fg-primary);
    overflow-wrap: anywhere;
  }

  .im-heading:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: 2px;
  }

  .im-head {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    justify-content: space-between;
    gap: var(--spacing-2);
  }

  .im-area {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-2);
    min-width: 0;
    min-height: 160px;
  }

  .im-state {
    display: flex;
    align-items: center;
    gap: var(--spacing-2);
    font-size: var(--font-size-base);
    color: var(--fg-secondary);
  }

  .im-empty {
    margin: 0;
    font-size: var(--font-size-base);
    color: var(--fg-secondary);
  }

  .im-note {
    margin: 0;
    font-size: var(--font-size-xs);
    color: var(--fg-secondary);
  }

  .im-alert {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--spacing-2);
    margin: 0;
    min-width: 0;
  }

  .im-alert :global(.im-alert-icon) {
    flex-shrink: 0;
  }

  .im-alert-text {
    flex: 1 1 200px;
    min-width: 0;
    overflow-wrap: anywhere;
  }

  .im-list {
    margin: 0;
    padding: 0;
    list-style: none;
    max-height: min(50vh, 360px);
    overflow-y: auto;
    min-width: 0;
  }

  .im-list:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: 2px;
  }

  .im-row {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    justify-content: space-between;
    gap: var(--spacing-2);
    padding: var(--spacing-2) 0;
    border-top: 1px solid var(--border);
    min-width: 0;
  }

  .im-row:first-child {
    border-top: none;
  }

  .im-row-text {
    flex: 1 1 180px;
    min-width: 0;
    font-size: var(--font-size-base);
    color: var(--fg-primary);
    overflow-wrap: anywhere;
  }

  .im-row-actions {
    display: flex;
    flex-wrap: wrap;
    gap: var(--spacing-2);
  }

  .im-entry {
    padding: var(--spacing-1) 0;
    border-top: 1px solid var(--border);
    font-family: var(--font-family-mono);
    font-size: var(--font-size-xs);
    line-height: 1.5;
    color: var(--fg-primary);
    overflow-wrap: anywhere;
  }

  .im-entry:first-child {
    border-top: none;
  }

  .im-more,
  .im-foot {
    display: flex;
    flex-wrap: wrap;
    gap: var(--spacing-2);
  }

  @media (pointer: coarse), (max-width: 768px) {
    .im-body :global(.btn-sm) {
      min-height: 44px;
    }
  }
</style>
