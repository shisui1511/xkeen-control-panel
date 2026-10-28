<script lang="ts">
  import { tick, untrack } from 'svelte';
  import { t, tp } from '../../../i18n';
  import { showToast } from '../../../stores';
  import Modal from '../../Modal.svelte';
  import Button from '../../Button.svelte';
  import Select from '../../Select.svelte';
  import Icon from '../../../lib/components/Icon.svelte';
  import ListPickerModal from '../rules/ListPickerModal.svelte';
  import {
    dropSource,
    loadGeodata,
    geodataErrorText,
    type DropAction,
    type GeodataListImpact,
    type ImpactView,
    type ListRef
  } from '../../../lib/configGeodata';

  // Диалог каскада при удалении или выключении источника со ссылками (D-18…D-21): списки
  // файла, на которые ссылаются правила, и что сделать с каждым — заменить другим списком,
  // убрать из условий или удалить правила целиком. Пометка уходит в черновик, файл и запись
  // уберёт «Применить». Ссылки и последствия приходят от сервера (ImpactView); интерфейс
  // ничего не вычисляет. Чужие ссылки и сырые правила ядра блокируют кнопку. Имена файлов,
  // теги и места ссылок выводятся только интерполяцией.
  const MAX_ROWS = 5;

  let {
    open,
    file,
    mode,
    name,
    impact,
    aliases = [],
    onclose,
    ondone
  }: {
    open: boolean;
    file: string;
    /** delete — удаление источника, off — выключение набора. */
    mode: 'delete' | 'off';
    /** Название набора для заголовка выключения. */
    name: string;
    /** Ссылки на файл; null — ещё ищутся. */
    impact: ImpactView | null;
    /** Парные имена файла (симлинк zkeen): уходят вместе с файлом. */
    aliases?: string[];
    onclose: () => void;
    ondone?: () => void;
  } = $props();

  const uid = $props.id();

  type Act = 'replace' | 'remove' | 'drop_rules';
  /** sel: '' — не выбрано, suggest — тот же тег в другом файле, picked — выбор из каталога. */
  interface RowState {
    action: Act;
    sel: string;
    picked: ListRef | null;
  }

  let rows = $state<Record<string, RowState>>({});
  let showAll = $state(false);
  let saving = $state(false);
  let errorText = $state('');
  // Тег списка, для которого открыт выбор «Другой список…»
  let pickTag = $state<string | null>(null);

  // Выбор начинается заново при каждом открытии и обновлении ссылок
  $effect(() => {
    void open;
    const initialLists = impact?.lists ?? [];
    untrack(() => {
      const next: Record<string, RowState> = {};
      for (const l of initialLists) next[l.tag] = { action: 'replace', sel: '', picked: null };
      rows = next;
      showAll = false;
      saving = false;
      errorText = '';
      pickTag = null;
    });
  });

  const rowOf = (tag: string): RowState =>
    rows[tag] ?? { action: 'replace', sel: '', picked: null };

  function setRow(tag: string, patch: Partial<RowState>) {
    rows[tag] = { ...rowOf(tag), ...patch };
  }

  const lists = $derived(impact?.lists ?? []);
  const foreign = $derived(impact?.foreign ?? []);
  const raw = $derived(impact?.raw ?? []);
  const blocked = $derived(foreign.length > 0 || raw.length > 0);
  const loading = $derived(impact === null);

  const shownLists = $derived(showAll ? lists : lists.slice(0, MAX_ROWS));
  const moreLists = $derived(Math.max(0, lists.length - MAX_ROWS));

  // Число правил, которых коснётся каскад: одно правило с несколькими списками считается раз
  const ruleCount = $derived(new Set(lists.flatMap((l) => l.rules.map((r) => r.id))).size);

  const links = $derived(aliases.filter((a) => a !== file).join(', '));
  const effects = $derived(
    $t('cfg.geo.del_effects', {
      symlink: links === '' ? '' : $t('cfg.geo.del_symlink', { link: links })
    })
  );

  // Фраза о чужих ссылках разрезается по месту подстановки: места выводятся моноширинным
  const PLACES_MARK = '\u0000';
  const foreignParts = $derived(
    $t('cfg.geo.del_foreign', { places: PLACES_MARK }).split(PLACES_MARK)
  );

  // Хвост строки списка после «файл · тег» (моноширинную часть рисует разметка)
  function rowTail(l: GeodataListImpact): string {
    const text = $t('cfg.geo.del_list_row', {
      file: PLACES_MARK,
      tag: PLACES_MARK,
      rules: l.rules.map((r) => r.n).join(', ')
    });
    return text.split(PLACES_MARK).pop() ?? '';
  }

  function replacement(l: GeodataListImpact): ListRef | null {
    const row = rowOf(l.tag);
    if (row.sel === 'suggest') return l.suggest ?? null;
    if (row.sel === 'picked') return row.picked;
    return null;
  }

  // Каждый список с «заменить» ждёт выбранной замены
  const complete = $derived(
    lists.every((l) => rowOf(l.tag).action !== 'replace' || replacement(l) !== null)
  );
  const canConfirm = $derived(!loading && !blocked && complete && !saving);

  const actionOptions = $derived([
    { value: 'replace', label: $t('cfg.geo.act_replace') },
    { value: 'remove', label: $t('cfg.geo.act_remove') },
    { value: 'drop_rules', label: $t('cfg.geo.act_drop') }
  ]);

  function withOptions(l: GeodataListImpact) {
    const row = rowOf(l.tag);
    return [
      { value: '', label: $t('cfg.geo.replace_choose') },
      ...(l.suggest !== undefined
        ? [
            {
              value: 'suggest',
              label: $t('cfg.geo.replace_hint', { file: l.suggest.file, tag: l.suggest.tag })
            }
          ]
        : []),
      ...(row.picked !== null
        ? [{ value: 'picked', label: `${row.picked.file} · ${row.picked.tag}` }]
        : []),
      { value: 'other', label: $t('cfg.geo.replace_other') }
    ];
  }

  function onWith(l: GeodataListImpact, value: string) {
    const before = rowOf(l.tag).sel;
    setRow(l.tag, { sel: value });
    if (value !== 'other') return;
    // «Другой список…» — не значение: открывается выбор, а показ возвращается к прежнему
    pickTag = l.tag;
    void tick().then(() => setRow(l.tag, { sel: before }));
  }

  function picked(refs: ListRef[]) {
    const tag = pickTag;
    pickTag = null;
    if (tag === null || refs.length === 0) return;
    setRow(tag, { picked: refs[0], sel: 'picked' });
  }

  const pickList = $derived(lists.find((l) => l.tag === pickTag));
  const pickKind = $derived(pickList?.kind === 'ip' ? 'ip' : 'site');

  function dieNumbers(l: GeodataListImpact): number[] {
    return l.rules.filter((r) => l.dies.includes(r.id)).map((r) => r.n);
  }

  function close() {
    // Пока идёт запись, диалог не закрывается
    if (saving) return;
    onclose();
  }

  async function confirm() {
    if (!canConfirm) return;
    saving = true;
    errorText = '';
    const actions: DropAction[] = lists.map((l): DropAction => {
      const row = rowOf(l.tag);
      if (row.action === 'replace') {
        return { tag: l.tag, action: 'replace', with: replacement(l) ?? undefined };
      }
      return { tag: l.tag, action: row.action };
    });
    try {
      await dropSource(file, mode, actions);
      showToast('success', $t('cfg.geo.del_toast'));
      saving = false;
      ondone?.();
      onclose();
    } catch (err) {
      saving = false;
      const code = (err as { code?: string } | null)?.code;
      if (code === 'draft_conflict') {
        // Ссылки устарели: свежий снимок и закрытие, повторное «Удалить» покажет новые
        void loadGeodata();
        showToast('error', geodataErrorText(err));
        onclose();
        return;
      }
      errorText = geodataErrorText(err);
    }
  }
</script>

<Modal
  isOpen={open}
  title={mode === 'delete' ? $t('cfg.geo.del_title', { file }) : $t('cfg.geo.off_title', { name })}
  maxWidth="720px"
  dataTestid="geo-delete-modal"
  onclose={close}
>
  <div class="dm-body">
    {#if loading}
      <div class="dm-loading" role="status" data-testid="geo-delete-loading">
        <span class="spinner" aria-hidden="true"></span>
        <span>{$t('cfg.geo.del_loading')}</span>
      </div>
    {:else}
      {#if lists.length > 0}
        <p class="dm-summary" data-testid="geo-delete-summary">
          {$tp('cfg.geo.del_summary', ruleCount)}
        </p>

        <ul class="dm-rows" data-testid="geo-delete-rows">
          {#each shownLists as l (l.tag)}
            {@const row = rowOf(l.tag)}
            <li class="dm-row" data-testid="geo-delete-row-{l.tag}">
              <p class="dm-row-head">
                <span class="dm-mono">{file} · {l.tag}</span>
                <span class="dm-rules">{rowTail(l)}</span>
              </p>
              <div class="dm-controls">
                <Select
                  wrapperClass="dm-select"
                  ariaLabel={$t('cfg.geo.act_label', { file, tag: l.tag })}
                  options={actionOptions}
                  disabled={saving}
                  data-testid="geo-delete-action-{l.tag}"
                  bind:value={() => row.action, (v) => setRow(l.tag, { action: v as Act })}
                />
                {#if row.action === 'replace'}
                  <Select
                    wrapperClass="dm-select"
                    ariaLabel={$t('cfg.geo.act_replace')}
                    options={withOptions(l)}
                    disabled={saving}
                    data-testid="geo-delete-with-{l.tag}"
                    bind:value={() => row.sel, (v) => onWith(l, v)}
                  />
                {/if}
              </div>
              {#if row.action === 'remove'}
                {#each dieNumbers(l) as n (n)}
                  <p class="dm-warn" data-testid="geo-delete-dies-{l.tag}">
                    <Icon name="warning" size={14} class="dm-warn-icon" />
                    <span>{$t('cfg.geo.del_rule_dies', { n })}</span>
                  </p>
                {/each}
              {/if}
            </li>
          {/each}
        </ul>

        {#if moreLists > 0 && !showAll}
          <div>
            <button
              type="button"
              class="btn btn-secondary btn-sm"
              data-testid="geo-delete-more"
              onclick={() => (showAll = true)}
            >
              {$t('cfg.groups.del_more', { n: moreLists })}
            </button>
          </div>
        {/if}
      {/if}

      {#if foreign.length > 0}
        <div class="alert alert-error dm-alert" role="alert" data-testid="geo-delete-foreign">
          <Icon name="warning" size={16} class="dm-alert-icon" />
          <div class="dm-alert-text">
            <span>
              {foreignParts[0]}
              {#each foreign as f, i (f.file + ':' + f.line + ':' + i)}
                <code class="dm-mono">{f.file}:{f.line}</code>{i < foreign.length - 1 ? ', ' : ''}
              {/each}
              {foreignParts[1] ?? ''}
            </span>
            <a class="dm-link" href="#/editor" data-testid="geo-delete-editor">
              {$t('cfg.geo.open_editor')}
            </a>
          </div>
        </div>
      {/if}

      {#if raw.length > 0}
        <div class="alert alert-error dm-alert" role="alert" data-testid="geo-delete-raw">
          <Icon name="warning" size={16} class="dm-alert-icon" />
          <div class="dm-alert-text">
            <span>{$t('cfg.geo.err.raw_ref')}</span>
            <span class="dm-mono">№ {raw.map((r) => r.n).join(', ')}</span>
          </div>
        </div>
      {/if}

      <div class="alert alert-warning dm-alert" data-testid="geo-delete-effects">
        <Icon name="warning" size={16} class="dm-alert-icon" />
        <span class="dm-alert-text">{effects}</span>
      </div>

      {#if errorText !== ''}
        <div class="alert alert-error dm-alert" role="alert" data-testid="geo-delete-error">
          <Icon name="warning" size={16} class="dm-alert-icon" />
          <span class="dm-alert-text">{errorText}</span>
        </div>
      {/if}
    {/if}

    <div class="dm-footer">
      <Button
        variant="danger"
        loading={saving}
        disabled={!canConfirm}
        title={!loading && !blocked && !complete ? $t('cfg.geo.replace_choose') : undefined}
        data-testid="geo-delete-confirm"
        onclick={confirm}
      >
        {$t(mode === 'delete' ? 'cfg.geo.del_submit' : 'cfg.geo.off_submit')}
      </Button>
      <Button variant="secondary" disabled={saving} data-testid="geo-delete-cancel" onclick={close}>
        {$t('cfg.nodes.cancel')}
      </Button>
    </div>
  </div>
</Modal>

<ListPickerModal
  open={pickTag !== null}
  kind={pickKind}
  selected={[]}
  hideFiles={[file, ...aliases]}
  single
  onpick={picked}
  onclose={() => (pickTag = null)}
/>

<style>
  .dm-body {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-4);
    min-width: 0;
  }

  .dm-loading {
    display: flex;
    align-items: center;
    gap: var(--spacing-2);
    font-size: var(--font-size-base);
    color: var(--fg-secondary);
  }

  .dm-summary {
    margin: 0;
    font-size: var(--font-size-base);
    color: var(--fg-primary);
    overflow-wrap: anywhere;
  }

  .dm-rows {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-4);
    margin: 0;
    padding: 0;
    list-style: none;
    min-width: 0;
  }

  .dm-row {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-2);
    min-width: 0;
    padding-top: var(--spacing-2);
    border-top: 1px solid var(--border);
  }

  .dm-row-head {
    display: flex;
    flex-wrap: wrap;
    align-items: baseline;
    gap: var(--spacing-1) var(--spacing-2);
    margin: 0;
    min-width: 0;
  }

  .dm-mono {
    font-family: var(--font-family-mono);
    font-size: var(--font-size-xs);
    color: var(--fg-primary);
    overflow-wrap: anywhere;
  }

  .dm-rules {
    font-size: var(--font-size-base);
    color: var(--fg-secondary);
    overflow-wrap: anywhere;
  }

  .dm-controls {
    display: flex;
    flex-wrap: wrap;
    gap: var(--spacing-2);
    min-width: 0;
  }

  .dm-controls :global(.dm-select) {
    flex: 1 1 220px;
    min-width: 0;
    max-width: 320px;
  }

  .dm-warn {
    display: flex;
    align-items: flex-start;
    gap: var(--spacing-1);
    margin: 0;
    font-size: var(--font-size-xs);
    line-height: 1.5;
    color: var(--warning);
    overflow-wrap: anywhere;
  }

  .dm-warn :global(.dm-warn-icon) {
    flex-shrink: 0;
    margin-top: 2px;
  }

  .dm-alert {
    margin: 0;
    min-width: 0;
    overflow-wrap: anywhere;
  }

  .dm-alert :global(.dm-alert-icon) {
    flex-shrink: 0;
    margin-top: 3px;
  }

  .dm-alert-text {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-1);
    min-width: 0;
  }

  .dm-link {
    align-self: flex-start;
    color: var(--accent-text, var(--accent));
    text-decoration: underline;
  }

  .dm-footer {
    display: flex;
    flex-wrap: wrap;
    justify-content: flex-end;
    gap: var(--spacing-2);
  }

  @media (pointer: coarse), (max-width: 768px) {
    .dm-footer :global(.btn),
    .dm-rows :global(.btn),
    .dm-body :global(.btn-sm) {
      min-height: 44px;
    }

    .dm-link {
      display: inline-flex;
      align-items: center;
      min-height: 44px;
    }
  }
</style>
