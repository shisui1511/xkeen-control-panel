<script lang="ts">
  import { untrack } from 'svelte';
  import { t, tp, currentLang } from '../../../i18n';
  import Modal from '../../Modal.svelte';
  import Button from '../../Button.svelte';
  import SegmentedControl from '../../SegmentedControl.svelte';
  import Icon from '../../../lib/components/Icon.svelte';
  import { fetchLists } from '../../../lib/configRouting';
  import type {
    CatalogView,
    ExtendedSet,
    ListEntry,
    ListRef,
    ServiceView
  } from '../../../lib/configRouting';
  import { reasonOf } from '../routing/routingOwner';
  import AddGeodataModal from '../routing/AddGeodataModal.svelte';

  // Выбор списка для условия geosite или geoip (D-19…D-21): каталог читается с роутера, а не
  // вводится вручную. «Сервисы» — человеческие имена каталога, за каждым «файл · тег»;
  // «Все списки» — каждый тег установленных dat с числом записей и примерами. Диалог лежит
  // поверх диалога правила и возвращает полный набор ссылок условия: убранное здесь
  // убирается и из условия. Под списком — расширенные наборы встроенного каталога с
  // «Добавить в XKeen» (D-32, D-33): блок доступен и при пустом каталоге, и при ошибке чтения
  // (тогда показываются наборы последнего успешного чтения). Всё, что приходит из dat (файлы,
  // теги, примеры) и из каталога наборов, выводится только интерполяцией.
  let { open, kind, selected, onpick, onclose } = $props<{
    open: boolean;
    /** site — условие geosite, ip — условие geoip. */
    kind: 'site' | 'ip';
    /** Ссылки, уже выбранные в условии. */
    selected: ListRef[];
    onpick: (refs: ListRef[]) => void;
    onclose: () => void;
  }>();

  const uid = $props.id();

  /** Тяжёлый список: больше этого числа записей (cfg.list.heavy). */
  const HEAVY = 50000;
  /** Строк «Всех списков» на экране; остальные — по «Показать ещё». */
  const PAGE_FIRST = 200;
  const PAGE_STEP = 100;
  const EXAMPLES_SHOWN = 3;

  let catalog = $state<CatalogView | null>(null);
  let loading = $state(false);
  let failure = $state('');
  let seg = $state('services');
  let query = $state('');
  let picked = $state<ListRef[]>([]);
  let limit = $state(PAGE_FIRST);
  // Наборы последнего успешного чтения каталога: ошибка повторного чтения их не стирает
  let extended = $state<ExtendedSet[]>([]);
  let addTarget = $state<ExtendedSet | null>(null);
  // Растёт с каждым чтением: устаревший ответ не затирает более свежий
  let seq = 0;

  async function load() {
    const mine = ++seq;
    loading = true;
    failure = '';
    try {
      const data = await fetchLists();
      if (mine !== seq) return;
      catalog = {
        ...data,
        services: Array.isArray(data.services) ? data.services : [],
        lists: Array.isArray(data.lists) ? data.lists : []
      };
      extended = Array.isArray(data.extended) ? data.extended : [];
      // Нет сервисов нужного вида — сразу полный каталог, а не пустой список сервисов
      const hasServices = catalog.services.some((sv: ServiceView) => refsOf(sv).length > 0);
      if (!hasServices) seg = 'all';
    } catch (e) {
      if (mine === seq) failure = reasonOf(e);
    } finally {
      if (mine === seq) loading = false;
    }
  }

  function reset() {
    seq++;
    catalog = null;
    extended = [];
    addTarget = null;
    loading = false;
    failure = '';
  }

  $effect(() => {
    if (open) {
      untrack(() => {
        picked = selected.map((r: ListRef) => ({ ...r }));
        seg = 'services';
        query = '';
        limit = PAGE_FIRST;
        void load();
      });
    } else {
      untrack(reset);
    }
  });

  const keyOf = (r: Pick<ListRef, 'file' | 'tag'>) => `${r.file}\u0000${r.tag}`;

  const pickedKeys = $derived(new Set(picked.map(keyOf)));
  const ready = $derived(!loading && failure === '' && catalog !== null);
  const needle = $derived(query.trim().toLowerCase());

  /** Наборы нужного вида; список диалога и без того ограничен этим видом. */
  const extendedSets = $derived(extended.filter((e: ExtendedSet) => e.kind === kind));

  const catalogLists = $derived((catalog?.lists ?? []).filter((l: ListEntry) => l.kind === kind));

  /** Ссылки каталога нужного вида: по ним определяется, что из выбранного пропало. */
  const knownKeys = $derived.by(() => {
    const keys = new Set<string>();
    for (const l of catalogLists) keys.add(keyOf(l));
    for (const sv of catalog?.services ?? []) {
      for (const r of sv.refs ?? []) if (r.kind === kind) keys.add(keyOf(r));
    }
    return keys;
  });

  const missing = $derived(ready ? picked.filter((r) => !knownKeys.has(keyOf(r))) : []);

  function refsOf(sv: ServiceView): ListRef[] {
    return (sv.refs ?? []).filter((r) => r.kind === kind);
  }

  function serviceName(sv: ServiceView): string {
    const ru = sv.name_ru || sv.name_en;
    const en = sv.name_en || sv.name_ru;
    return ($currentLang === 'ru' ? ru : en) || sv.id;
  }

  const services = $derived.by(() => {
    const out: ServiceView[] = [];
    for (const sv of catalog?.services ?? []) {
      const refs = refsOf(sv);
      if (refs.length === 0) continue;
      if (needle !== '') {
        const hay = [sv.name_ru, sv.name_en, ...refs.map((r) => `${r.file} ${r.tag}`)]
          .join('\n')
          .toLowerCase();
        if (!hay.includes(needle)) continue;
      }
      out.push(sv);
    }
    return out;
  });

  const allLists = $derived.by(() => {
    if (needle === '') return catalogLists;
    const hits = catalogLists.filter((l: ListEntry) =>
      `${l.file} ${l.tag}`.toLowerCase().includes(needle)
    );
    // Точное совпадение тега — первым
    return [
      ...hits.filter((l: ListEntry) => l.tag.toLowerCase() === needle),
      ...hits.filter((l: ListEntry) => l.tag.toLowerCase() !== needle)
    ];
  });

  const shownLists = $derived(allLists.slice(0, limit));
  const catalogEmpty = $derived(
    ready && catalogLists.length === 0 && !services.length && needle === ''
  );
  const searchEmpty = $derived(
    ready &&
      !catalogEmpty &&
      (seg === 'services' ? services.length === 0 : allLists.length === 0) &&
      missing.length === 0
  );

  const segItems = $derived([
    { value: 'services', label: $t('cfg.list.seg_services') },
    { value: 'all', label: $t('cfg.list.seg_all') }
  ]);

  function isPicked(r: Pick<ListRef, 'file' | 'tag'>): boolean {
    return pickedKeys.has(keyOf(r));
  }

  function toggleRef(r: ListRef, on: boolean) {
    if (on) {
      if (!isPicked(r)) picked.push({ file: r.file, tag: r.tag, kind: r.kind });
    } else {
      picked = picked.filter((p) => keyOf(p) !== keyOf(r));
    }
  }

  function serviceOn(sv: ServiceView): boolean {
    const refs = refsOf(sv);
    return refs.length > 0 && refs.every(isPicked);
  }

  function toggleService(sv: ServiceView, on: boolean) {
    for (const r of refsOf(sv)) toggleRef(r, on);
  }

  function onQuery(e: Event & { currentTarget: HTMLInputElement }) {
    query = e.currentTarget.value;
    limit = PAGE_FIRST;
  }

  function choose() {
    if (picked.length === 0) return;
    onpick(picked.map((r) => ({ ...r })));
  }

  const refLabel = (r: Pick<ListRef, 'file' | 'tag'>) => `${r.file} · ${r.tag}`;
</script>

<Modal
  isOpen={open}
  title={$t('cfg.list.title')}
  maxWidth="720px"
  dataTestid="list-picker"
  {onclose}
>
  <div class="lp-body">
    <div class="lp-controls">
      <div data-testid="list-picker-seg" class="lp-seg" class:lp-off={!ready}>
        <SegmentedControl
          items={segItems}
          bind:value={seg}
          ariaLabel={$t('cfg.list.seg_aria')}
          onchange={() => (limit = PAGE_FIRST)}
        />
      </div>
      <label class="sr-only" for="{uid}-search">{$t('cfg.list.search')}</label>
      <input
        id="{uid}-search"
        class="input lp-search"
        type="search"
        value={query}
        placeholder={$t('cfg.list.search')}
        autocomplete="off"
        spellcheck="false"
        disabled={!ready}
        data-testid="list-picker-search"
        oninput={onQuery}
      />
    </div>

    <div class="lp-area" data-testid="list-picker-area" aria-busy={loading}>
      {#if loading}
        <div class="lp-state" role="status" data-testid="list-picker-loading">
          <span class="spinner" aria-hidden="true"></span>
          <span>{$t('cfg.list.loading')}</span>
        </div>
      {:else if failure !== ''}
        <div class="alert alert-error lp-alert" role="alert" data-testid="list-picker-error">
          <Icon name="warning" size={16} class="lp-alert-icon" />
          <span>{$t('cfg.list.load_failed', { reason: failure })}</span>
        </div>
        <div>
          <Button variant="secondary" data-testid="list-picker-retry" onclick={() => void load()}>
            {$t('cfg.nodes.retry')}
          </Button>
        </div>
      {:else if catalogEmpty}
        <div class="lp-state lp-empty" data-testid="list-picker-empty">
          <strong>{$t('cfg.list.empty_title')}</strong>
          <span>{$t('cfg.list.empty_body')}</span>
        </div>
      {:else}
        {#if missing.length > 0}
          <ul class="lp-list" aria-label={$t('cfg.list.missing')} data-testid="list-picker-missing">
            {#each missing as r (keyOf(r))}
              <li>
                <label class="lp-row lp-row-missing">
                  <input
                    type="checkbox"
                    checked={true}
                    data-testid="list-row-{r.file}:{r.tag}"
                    onchange={(e) => toggleRef(r, e.currentTarget.checked)}
                  />
                  <span class="lp-main">
                    <span class="lp-mono">{refLabel(r)}</span>
                    <span class="chip chip-danger lp-chip">{$t('cfg.list.missing')}</span>
                  </span>
                </label>
              </li>
            {/each}
          </ul>
        {/if}

        {#if searchEmpty}
          <p class="lp-state" role="status" data-testid="list-picker-search-empty">
            {$t('cfg.list.search_empty')}
          </p>
        {:else if seg === 'services'}
          <ul class="lp-list" data-testid="list-picker-services">
            {#each services as sv (sv.id)}
              <li>
                <label class="lp-row">
                  <input
                    type="checkbox"
                    checked={serviceOn(sv)}
                    data-testid="list-service-{sv.id}"
                    onchange={(e) => toggleService(sv, e.currentTarget.checked)}
                  />
                  <span class="lp-main">
                    <span class="lp-name">{serviceName(sv)}</span>
                    <span class="lp-mono lp-dim">
                      {refsOf(sv).map(refLabel).join(', ')}
                    </span>
                  </span>
                </label>
              </li>
            {/each}
          </ul>
        {:else}
          <ul class="lp-list" data-testid="list-picker-all">
            {#each shownLists as l (keyOf(l))}
              <li>
                <label class="lp-row">
                  <input
                    type="checkbox"
                    checked={isPicked(l)}
                    data-testid="list-row-{l.file}:{l.tag}"
                    onchange={(e) => toggleRef(l, e.currentTarget.checked)}
                  />
                  <span class="lp-main">
                    <span class="lp-line">
                      <span class="lp-mono">{refLabel(l)}</span>
                      <span class="lp-count">{$tp('cfg.list.entries', l.count)}</span>
                      {#if l.count > HEAVY}
                        <span class="chip chip-warning lp-chip">{$t('cfg.list.heavy')}</span>
                      {/if}
                    </span>
                    {#if (l.examples ?? []).length > 0}
                      <span class="lp-mono lp-dim">
                        {(l.examples ?? []).slice(0, EXAMPLES_SHOWN).join(', ')}
                      </span>
                    {/if}
                  </span>
                </label>
              </li>
            {/each}
          </ul>
          {#if allLists.length > limit}
            <div>
              <button
                type="button"
                class="btn btn-secondary btn-sm"
                data-testid="list-picker-more"
                onclick={() => (limit += PAGE_STEP)}
              >
                {$t('cfg.groups.show_more', { n: PAGE_STEP })}
              </button>
            </div>
          {/if}
        {/if}
      {/if}
    </div>

    {#if !loading}
      <section class="lp-ext" aria-labelledby="{uid}-ext" data-testid="list-picker-extended">
        <h3 class="lp-ext-title" id="{uid}-ext">{$t('cfg.list.extended_title')}</h3>
        <p class="lp-ext-desc">{$t('cfg.list.extended_desc')}</p>
        {#if extendedSets.length > 0}
          <ul class="lp-ext-list">
            {#each extendedSets as set (set.id)}
              <li class="lp-ext-card" data-testid="list-ext-{set.id}">
                <span class="lp-main">
                  <span class="lp-line">
                    <span class="lp-name">{set.name}</span>
                    {#if set.heavy}
                      <span class="chip chip-warning lp-chip">{$t('cfg.list.heavy')}</span>
                    {/if}
                    {#if set.installed}
                      <span class="chip chip-default lp-chip">{$t('setup.installed')}</span>
                    {/if}
                  </span>
                  <span class="lp-mono lp-dim">{set.file} · {set.size_hint}</span>
                </span>
                {#if set.in_xkeen}
                  <span class="chip chip-success lp-chip" data-testid="list-ext-in-{set.id}">
                    {$t('cfg.list.in_xkeen')}
                  </span>
                {:else}
                  <button
                    type="button"
                    class="btn btn-secondary btn-sm lp-ext-add"
                    data-testid="list-ext-add-{set.id}"
                    onclick={() => (addTarget = set)}
                  >
                    {$t('cfg.list.add_xkeen')}
                  </button>
                {/if}
              </li>
            {/each}
          </ul>
        {/if}
      </section>
    {/if}

    <div class="lp-footer">
      <Button
        variant="primary"
        disabled={picked.length === 0}
        data-testid="list-picker-pick"
        onclick={choose}
      >
        {$t('cfg.list.pick')}
      </Button>
      <Button variant="secondary" data-testid="list-picker-cancel" onclick={onclose}>
        {$t('app.cancel')}
      </Button>
    </div>
  </div>
</Modal>

<AddGeodataModal
  open={addTarget !== null}
  set={addTarget}
  geodata={catalog?.geodata ?? null}
  onadded={() => void load()}
  onclose={() => (addTarget = null)}
/>

<style>
  .lp-body {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-4);
    min-width: 0;
  }

  .lp-controls {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--spacing-2) var(--spacing-4);
    min-width: 0;
  }

  .lp-seg.lp-off {
    pointer-events: none;
    opacity: 0.6;
  }

  .lp-search {
    flex: 1 1 220px;
    min-width: 0;
  }

  /* Высота области не прыгает между чтением, ошибкой и списком */
  .lp-area {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-2);
    min-width: 0;
    min-height: 240px;
    max-height: 50dvh;
    overflow-y: auto;
    padding: var(--spacing-2);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    scrollbar-width: thin;
  }

  .lp-state {
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: var(--spacing-2);
    flex: 1 1 auto;
    margin: 0;
    padding: var(--spacing-4);
    text-align: center;
    color: var(--fg-secondary);
    overflow-wrap: anywhere;
  }

  .lp-empty strong {
    color: var(--fg-primary);
  }

  .lp-alert {
    margin: 0;
    min-width: 0;
    overflow-wrap: anywhere;
  }

  .lp-alert :global(.lp-alert-icon) {
    flex-shrink: 0;
    margin-top: 3px;
  }

  .lp-list {
    display: flex;
    flex-direction: column;
    margin: 0;
    padding: 0;
    list-style: none;
  }

  .lp-row {
    display: flex;
    align-items: flex-start;
    gap: var(--spacing-2);
    min-width: 0;
    min-height: 32px;
    padding: var(--spacing-1) var(--spacing-2);
    border-radius: var(--radius-sm);
    cursor: pointer;
  }

  .lp-row:hover {
    background: var(--bg-hover);
  }

  .lp-row input {
    flex-shrink: 0;
    margin-top: 3px;
    accent-color: var(--accent);
  }

  .lp-main {
    display: flex;
    flex-direction: column;
    gap: 2px;
    min-width: 0;
  }

  .lp-line {
    display: flex;
    flex-wrap: wrap;
    align-items: baseline;
    gap: var(--spacing-1) var(--spacing-2);
    min-width: 0;
  }

  .lp-name {
    font-size: var(--font-size-base);
    color: var(--fg-primary);
    overflow-wrap: anywhere;
  }

  .lp-mono {
    font-family: var(--font-family-mono);
    font-size: var(--font-size-sm);
    color: var(--fg-primary);
    overflow-wrap: anywhere;
    word-break: break-word;
  }

  .lp-dim {
    font-size: var(--font-size-xs);
    color: var(--fg-dim);
  }

  .lp-count {
    font-size: var(--font-size-xs);
    color: var(--fg-secondary);
  }

  .lp-chip {
    align-self: flex-start;
  }

  .lp-row-missing {
    border-left: 3px solid var(--danger);
  }

  .lp-ext {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-2);
    min-width: 0;
  }

  .lp-ext-title {
    margin: 0;
    font-size: var(--font-size-base);
    font-weight: 600;
    color: var(--fg-primary);
  }

  .lp-ext-desc {
    margin: 0;
    font-size: var(--font-size-sm);
    color: var(--fg-secondary);
  }

  .lp-ext-list {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-2);
    margin: 0;
    padding: 0;
    list-style: none;
  }

  .lp-ext-card {
    display: flex;
    align-items: center;
    justify-content: space-between;
    flex-wrap: wrap;
    gap: var(--spacing-2) var(--spacing-4);
    min-width: 0;
    padding: var(--spacing-2) var(--spacing-3);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    background: var(--bg-card);
  }

  .lp-ext-add {
    flex-shrink: 0;
  }

  .lp-footer {
    display: flex;
    flex-wrap: wrap;
    justify-content: flex-end;
    gap: var(--spacing-2);
  }

  @media (pointer: coarse), (max-width: 768px) {
    .lp-row {
      min-height: 44px;
      align-items: center;
    }

    .lp-row input {
      margin-top: 0;
    }
  }

  @media (max-width: 480px) {
    .lp-footer {
      flex-direction: column;
    }

    .lp-footer :global(.btn) {
      width: 100%;
    }
  }
</style>
