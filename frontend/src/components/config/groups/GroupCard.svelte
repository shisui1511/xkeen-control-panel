<script lang="ts">
  import { untrack } from 'svelte';
  import { t, tp } from '../../../i18n';
  import { showToast, activeKernelState } from '../../../stores';
  import Button from '../../Button.svelte';
  import Select from '../../Select.svelte';
  import Icon from '../../../lib/components/Icon.svelte';
  import { applyRunning, handleLayerError } from '../../../lib/configLayer';
  import { configSubTabHash } from '../../../lib/configSubTab';
  import { sourcesView, type KernelName } from '../../../lib/configSources';
  import {
    previewGroup,
    selectMember,
    routingErrorText,
    type GroupPreview,
    type GroupView,
    type IssueView
  } from '../../../lib/configRouting';
  import { kernelLabel } from '../../../lib/kernelState';
  import { PAGE_FIRST, PAGE_STEP, SEARCH_THRESHOLD } from '../../../lib/nodesView';
  import {
    chipKind,
    issueTextKey,
    kernelSummary,
    nestedCount,
    targetLabel
  } from '../../../lib/routingView';

  let {
    group,
    groups,
    kernel,
    installed,
    expanded,
    ontoggle,
    onedit,
    ondelete
  }: {
    group: GroupView;
    /** Все группы представления: имена вложенных участников. */
    groups: GroupView[];
    kernel: KernelName;
    installed: Record<string, boolean>;
    expanded: boolean;
    ontoggle: () => void;
    onedit?: (group: GroupView) => void;
    ondelete: (group: GroupView) => void;
  } = $props();

  const uid = $props.id();

  // --- Шапка, сводка, чипы ---
  const manual = $derived(group.mode === 'manual');
  const summary = $derived(kernelSummary($t, group.counts, installed, nestedCount(group)));

  interface Chip {
    kind: 'danger' | 'warning';
    code: string;
    text: string;
  }

  function chipText(issue: IssueView): string {
    switch (issue.code) {
      case 'group_empty':
        return $t('cfg.groups.chip_empty');
      case 'group_cycle':
        return $t('cfg.groups.chip_cycle');
      case 'group_empty_kernel':
        return $t('cfg.groups.chip_empty_kernel', {
          kernel: kernelLabel(issue.params?.kernel ?? '')
        });
      case 'mode_unavailable':
        return $t('cfg.groups.chip_version', {
          kernel: kernelLabel(issue.params?.kernel ?? ''),
          min: issue.params?.min ?? '?'
        });
      case 'pick_stale':
        return $t('cfg.groups.chip_pick_stale');
      default:
        return $t(issueTextKey(issue.code), issue.params ?? {});
    }
  }

  const chips = $derived<Chip[]>([
    ...group.issues.map((i: IssueView) => ({
      kind: chipKind(i, true),
      code: i.code,
      text: chipText(i)
    })),
    ...group.warnings.map((w: IssueView) => ({
      kind: chipKind(w, false),
      code: w.code,
      text: chipText(w)
    }))
  ]);
  const emptyAll = $derived(group.issues.some((i: IssueView) => i.code === 'group_empty'));

  // --- Резервный узел ---
  const reserveName = $derived.by(() => {
    if (group.mode !== 'fallback' || !group.reserve) return '';
    for (const s of $sourcesView?.sources ?? []) {
      const n = s.nodes.find((x) => x.ref === group.reserve);
      if (n) return n.name;
    }
    return '?';
  });

  // --- Строка «Сейчас» (D-38) ---
  let selecting = $state(false);
  // Показанный выбор: следует за ответом сервера, а на время запроса и после сбоя
  // переопределяется присваиванием
  let pick = $derived<string>(group.current);

  const options = $derived.by(() => {
    const list: { value: string; label: string; disabled?: boolean }[] = group.member_options.map(
      (o: { member: string; label: string }) => ({
        value: o.member,
        label: o.label
      })
    );
    if (!list.some((o: { value: string }) => o.value === group.current)) {
      list.unshift({
        value: group.current,
        label: group.current === '' ? '—' : group.current_label || group.current,
        disabled: group.current === ''
      });
    }
    return list;
  });
  const notApplied = $derived(!group.applied || group.current === '');
  const selectDisabled = $derived(selecting || $applyRunning || notApplied);
  const selectTitle = $derived(
    $applyRunning
      ? $t('cfg.routing.apply_busy_hint')
      : !group.applied
        ? routingErrorText($t, 'group_not_applied')
        : group.current === ''
          ? $t('cfg.groups.current_empty')
          : undefined
  );
  const hintXray = $derived(installed.xray === true);
  // Ядро не запущено: выбор сохраняется в панели и ставится при запуске
  const hintStopped = $derived(
    (installed.xray === true || installed.mihomo === true) && $activeKernelState === 'none'
  );

  async function onPick(e: Event & { currentTarget: HTMLSelectElement }) {
    const member = e.currentTarget.value;
    if (member === group.current || selecting) return;
    const label = options.find((o: { value: string }) => o.value === member)?.label ?? member;
    selecting = true;
    pick = member;
    try {
      const res = await selectMember(group.id, member);
      // Выбор в панели записан, но ядро могло его не принять: «Выбрано» только без исхода failed
      const failed = Object.entries(res?.kernels ?? {})
        .filter(([, outcome]) => outcome === 'failed')
        .map(([name]) => kernelLabel(name));
      if (failed.length > 0) {
        showToast(
          'warning',
          $t(
            failed.length > 1 ? 'cfg.groups.select_partial_both' : 'cfg.groups.select_partial_one',
            {
              name: label,
              kernels: failed.join(', ')
            }
          )
        );
      } else {
        showToast('success', $t('cfg.groups.toast_selected', { name: label }));
      }
    } catch (err) {
      pick = group.current;
      const code = (err as { code?: unknown })?.code;
      if (code === 'apply_busy') handleLayerError(err);
      else if (code === 'group_not_applied' || code === 'member_invalid')
        showToast('error', routingErrorText($t, code));
      else showToast('error', $t('cfg.groups.select_failed', { name: label }));
    } finally {
      selecting = false;
    }
  }

  // --- Раскрытый состав выбранного ядра ---
  let preview = $state<GroupPreview | null>(null);
  let previewState = $state<'idle' | 'loading' | 'error'>('idle');
  let previewReason = $state('');
  let retryTick = $state(0);
  let query = $state('');
  let limit = $state(PAGE_FIRST);

  // Состав берётся у сервера (Resolve); перечитывается, когда меняется описание группы
  // или счётчики
  const previewKey = $derived(
    JSON.stringify([
      group.mode,
      group.sources,
      group.include,
      group.exclude,
      group.regex,
      group.picks,
      group.members,
      group.counts
    ])
  );

  // Прежний расчёт не обрывается: ответ устаревшего состава отбрасывается
  async function loadPreview(isStale: () => boolean) {
    previewState = 'loading';
    try {
      const res = await previewGroup({
        id: group.id,
        name: group.name,
        mode: group.mode,
        sources: group.sources,
        include: group.include,
        exclude: group.exclude,
        regex: group.regex,
        picks: group.picks,
        members: group.members,
        reserve: group.reserve
      });
      if (isStale()) return;
      preview = res;
      previewState = 'idle';
    } catch (err) {
      if (isStale()) return;
      const e = err as { code?: string; params?: Record<string, string>; message?: string };
      previewReason = e.code ? routingErrorText($t, e.code, e.params) : (e.message ?? '');
      previewState = 'error';
    }
  }

  $effect(() => {
    if (!expanded) return;
    void previewKey;
    void retryTick;
    let stale = false;
    // Поля группы читаются без отслеживания: перечитывание задаёт previewKey
    untrack(() => void loadPreview(() => stale));
    return () => {
      stale = true;
    };
  });

  const kernelPreview = $derived((kernel === 'xray' ? preview?.xray : preview?.mihomo) ?? null);
  const nested = $derived(kernelPreview?.members ?? []);
  const names = $derived(kernelPreview?.names ?? []);
  const filtered = $derived.by(() => {
    const q = query.trim().toLowerCase();
    return q === '' ? names : names.filter((n: string) => n.toLowerCase().includes(q));
  });
  const shown = $derived(filtered.slice(0, limit));
  const searchable = $derived(names.length > SEARCH_THRESHOLD);

  function onQuery(e: Event) {
    query = (e.currentTarget as HTMLInputElement).value;
    limit = PAGE_FIRST;
  }
</script>

<article class="card group-card" aria-labelledby="{uid}-title" data-testid="group-card-{group.id}">
  <div class="gc-head" data-testid="group-row-{group.id}">
    <button
      type="button"
      class="gc-expand"
      aria-expanded={expanded}
      aria-controls="{uid}-list"
      aria-label={$t('cfg.groups.expand_aria', { name: group.name })}
      data-testid="group-toggle-{group.id}"
      onclick={ontoggle}
    >
      <Icon name={expanded ? 'chevron-down' : 'chevron-right'} size={16} />
      <span class="gc-name" id="{uid}-title">{group.name}</span>
    </button>
    <span class="chip chip-default" data-testid="group-mode-{group.id}">
      {$t(`cfg.groups.mode_${group.mode}`)}
    </span>
    {#each chips as chip (chip.code + chip.text)}
      <span class="chip chip-{chip.kind} gc-chip" data-testid="group-chip-{group.id}-{chip.code}">
        {chip.text}
      </span>
    {/each}
  </div>

  <p class="gc-summary" data-testid="group-summary-{group.id}">{summary}</p>

  {#if emptyAll}
    <p class="gc-hint" data-testid="group-empty-hint-{group.id}">
      {$t('cfg.groups.nodes_none_hint')}
      <a class="gc-link" href={configSubTabHash('nodes')}>{$t('cfg.groups.nodes_none_link')}</a>.
    </p>
  {/if}

  {#if manual}
    <div class="gc-current">
      <label class="gc-current-label" for="{uid}-current">{$t('cfg.groups.current')}</label>
      <div class="gc-current-row">
        <Select
          id="{uid}-current"
          bind:value={pick}
          {options}
          disabled={selectDisabled}
          title={selectTitle}
          ariaLabel={$t('cfg.groups.current')}
          wrapperClass="gc-select"
          data-testid="group-current-{group.id}"
          onchange={onPick}
        />
        {#if selecting}
          <span class="spinner" aria-hidden="true" data-testid="group-selecting-{group.id}"></span>
        {/if}
      </div>
      {#if hintXray}
        <p class="gc-hint" data-testid="group-hint-xray-{group.id}">
          {$t('cfg.groups.current_hint_xray')}
        </p>
      {/if}
      {#if hintStopped}
        <p class="gc-hint" data-testid="group-hint-stopped-{group.id}">
          {$t('cfg.groups.current_hint_stopped')}
        </p>
      {/if}
    </div>
  {:else if group.mode === 'fallback' && reserveName}
    <p class="gc-hint" data-testid="group-reserve-{group.id}">
      {$t('cfg.groups.reserve_line', { name: reserveName })}
    </p>
  {/if}

  <div class="gc-actions">
    {#if onedit}
      <Button
        variant="secondary"
        class="btn-sm"
        ariaLabel={$t('cfg.groups.edit_aria', { name: group.name })}
        data-testid="group-edit-{group.id}"
        onclick={() => onedit(group)}
      >
        <Icon name="edit" size={14} />
        {$t('cfg.groups.edit')}
      </Button>
    {/if}
    <Button
      variant="danger"
      class="btn-sm"
      disabled={$applyRunning}
      title={$applyRunning ? $t('cfg.routing.apply_busy_hint') : undefined}
      ariaLabel={$t('cfg.groups.delete_aria', { name: group.name })}
      data-testid="group-delete-{group.id}"
      onclick={() => ondelete(group)}
    >
      <Icon name="delete" size={14} />
      {$t('cfg.groups.delete')}
    </Button>
  </div>

  {#if expanded}
    <div class="gc-list" id="{uid}-list" data-testid="group-list-{group.id}">
      {#if previewState === 'loading' && preview === null}
        <p class="gc-empty">
          <span class="spinner" aria-hidden="true"></span>
          {$t('cfg.groups.computing')}
        </p>
      {:else if previewState === 'error'}
        <p class="gc-empty" data-testid="group-list-error-{group.id}">
          {$t('cfg.groups.preview_failed', { reason: previewReason })}
        </p>
        <div class="gc-more">
          <button type="button" class="btn btn-secondary btn-sm" onclick={() => (retryTick += 1)}>
            {$t('cfg.nodes.retry')}
          </button>
        </div>
      {:else if nested.length === 0 && names.length === 0}
        <p class="gc-empty" data-testid="group-list-empty-{group.id}">
          {$t('cfg.groups.members_none')}
        </p>
      {:else}
        {#if searchable}
          <input
            class="input gc-search"
            type="search"
            value={query}
            placeholder={$t('cfg.groups.members_search')}
            aria-label={$t('cfg.groups.members_search')}
            data-testid="group-search-{group.id}"
            oninput={onQuery}
          />
        {/if}
        <ul class="gc-rows" aria-label={$t('cfg.groups.members_title')}>
          {#each nested as m, i (`${m.kind}:${m.id ?? ''}:${i}`)}
            <li class="gc-row gc-row-member">
              <span class="gc-row-name">{targetLabel($t, m, groups)}</span>
            </li>
          {/each}
          {#each shown as name, i (i)}
            <li class="gc-row">
              <span class="gc-row-name" title={name}>{name}</span>
            </li>
          {/each}
        </ul>
        {#if filtered.length > limit}
          <div class="gc-more">
            <button
              type="button"
              class="btn btn-secondary btn-sm"
              data-testid="group-more-{group.id}"
              onclick={() => (limit += PAGE_STEP)}
            >
              {$t('cfg.groups.show_more', { n: PAGE_STEP })}
            </button>
          </div>
        {/if}
        {#if kernelPreview}
          <p class="gc-hint">
            {$tp('cfg.groups.preview_nodes', kernelPreview.count, {
              kernel: kernelLabel(kernel)
            })}
          </p>
        {/if}
      {/if}
    </div>
  {/if}
</article>

<style>
  .group-card {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-3);
    min-width: 0;
  }

  .gc-head {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--spacing-2);
    min-width: 0;
  }

  .gc-expand {
    display: inline-flex;
    align-items: center;
    gap: var(--spacing-2);
    flex: 1 1 auto;
    min-width: 0;
    min-height: 32px;
    padding: 0;
    border: 0;
    background: transparent;
    color: var(--fg-primary);
    font: inherit;
    font-size: var(--font-size-lg);
    font-weight: 600;
    line-height: 1.2;
    text-align: left;
    cursor: pointer;
  }

  .gc-expand :global(svg) {
    flex-shrink: 0;
  }

  .gc-name {
    min-width: 0;
    overflow-wrap: anywhere;
  }

  .gc-chip {
    white-space: normal;
    overflow-wrap: anywhere;
    max-width: 100%;
  }

  .gc-summary {
    margin: 0;
    font-size: var(--font-size-base);
    line-height: 1.5;
    color: var(--fg-secondary);
    overflow-wrap: anywhere;
  }

  .gc-hint {
    margin: 0;
    font-size: var(--font-size-xs);
    font-weight: 400;
    line-height: 1.4;
    color: var(--fg-dim);
    overflow-wrap: anywhere;
  }

  .gc-link {
    color: var(--accent);
    text-decoration: none;
  }

  .gc-link:hover {
    text-decoration: underline;
  }

  .gc-current {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-1);
    min-width: 0;
  }

  .gc-current-label {
    font-size: var(--font-size-xs);
    font-weight: 600;
    color: var(--fg-secondary);
  }

  .gc-current-row {
    display: flex;
    align-items: center;
    gap: var(--spacing-2);
    min-width: 0;
    max-width: 420px;
  }

  .gc-current-row :global(.gc-select) {
    flex: 1 1 auto;
    min-width: 0;
  }

  .gc-actions {
    display: flex;
    flex-wrap: wrap;
    gap: var(--spacing-2);
  }

  .gc-list {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-2);
    min-width: 0;
    padding-top: var(--spacing-3);
    border-top: 1px solid var(--border);
  }

  .gc-search {
    width: 100%;
    max-width: 360px;
  }

  .gc-rows {
    list-style: none;
    margin: 0;
    padding: 0;
    min-width: 0;
  }

  .gc-row {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--spacing-1) var(--spacing-2);
    min-width: 0;
    padding: var(--spacing-2) 0;
    border-top: 1px solid var(--border);
    font-size: var(--font-size-base);
    line-height: 1.4;
    color: var(--fg-primary);
  }

  .gc-row:first-child {
    border-top: 0;
  }

  .gc-row-member .gc-row-name {
    font-weight: 600;
  }

  .gc-row-name {
    min-width: 0;
    overflow-wrap: anywhere;
  }

  .gc-empty {
    display: flex;
    align-items: center;
    gap: var(--spacing-2);
    margin: 0;
    font-size: var(--font-size-base);
    color: var(--fg-dim);
    overflow-wrap: anywhere;
  }

  .gc-more {
    display: flex;
  }

  @media (pointer: coarse), (max-width: 768px) {
    .gc-expand {
      min-height: 44px;
    }

    .gc-actions :global(.btn) {
      min-height: 44px;
    }

    .gc-link {
      display: inline-flex;
      align-items: center;
      min-height: 44px;
    }
  }
</style>
