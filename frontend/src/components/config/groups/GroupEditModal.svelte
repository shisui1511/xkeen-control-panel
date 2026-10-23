<script lang="ts">
  import { untrack } from 'svelte';
  import { t, tp } from '../../../i18n';
  import Modal from '../../Modal.svelte';
  import Button from '../../Button.svelte';
  import Input from '../../Input.svelte';
  import Select from '../../Select.svelte';
  import SegmentedControl from '../../SegmentedControl.svelte';
  import Icon from '../../../lib/components/Icon.svelte';
  import { handleLayerError, layerSnapshot } from '../../../lib/configLayer';
  import { loadSources, sourcesView, type KernelName } from '../../../lib/configSources';
  import {
    previewGroup,
    routingErrorText,
    saveGroup,
    type FeatureView,
    type GroupMode,
    type GroupPreview,
    type GroupView,
    type Member
  } from '../../../lib/configRouting';
  import { kernelLabel } from '../../../lib/kernelState';
  import { PAGE_FIRST, PAGE_STEP, SEARCH_THRESHOLD } from '../../../lib/nodesView';
  import { targetLabel } from '../../../lib/routingView';

  // Диалог группы: имя, режим, состав (источники, подстроки, отдельные узлы, участники
  // «Вручную», резервный узел «Резерв») и предпросмотр состава по каждому ядру. Состав
  // считает сервер (previewGroup): интерфейс ничего не вычисляет сам и только показывает
  // ответ. Правка пишется в черновик одним запросом saveGroup.
  // Корень диалога получает data-testid="group-modal" через проп dataTestid у Modal.
  const PREVIEW_DELAY_MS = 250;

  let { open, group, groups, features, onclose } = $props<{
    open: boolean;
    /** Правимая группа; null — новая. */
    group: GroupView | null;
    groups: GroupView[];
    features: Record<string, FeatureView>;
    onclose: () => void;
  }>();

  const uid = $props.id();
  const KERNELS: { id: KernelName; name: string }[] = [
    { id: 'xray', name: 'Xray' },
    { id: 'mihomo', name: 'Mihomo' }
  ];

  let name = $state('');
  let mode = $state<GroupMode>('manual');
  let allSources = $state(true);
  let sourceIds = $state<string[]>([]);
  let includeText = $state('');
  let excludeText = $state('');
  let expert = $state(false);
  let picks = $state<string[]>([]);
  let members = $state<Member[]>([]);
  let reserve = $state('');
  let addValue = $state('');
  let pickQuery = $state('');
  let pickLimit = $state(PAGE_FIRST);

  // Предпросмотр
  let previewData = $state<GroupPreview | null>(null);
  let previewBusy = $state(false);
  let previewError = $state('');
  let retryTick = $state(0);
  let previewLimits = $state<Record<KernelName, number>>({ xray: PAGE_FIRST, mihomo: PAGE_FIRST });

  // Запись
  let saving = $state(false);
  let nameServerError = $state('');
  let reserveServerError = $state('');
  let cycleError = $state('');
  let generalError = $state('');
  let filterErrors = $state<Record<string, string>>({});

  // --- Данные источников ---
  const sourceList = $derived(
    ($sourcesView?.sources ?? []).map((s) => ({
      id: s.id,
      label: s.kind === 'my' ? $t('cfg.nodes.my_name') : s.name
    }))
  );
  const allNodes = $derived(
    ($sourcesView?.sources ?? []).flatMap((s) =>
      s.nodes.map((n) => ({
        ref: n.ref,
        name: n.name,
        source: s.kind === 'my' ? $t('cfg.nodes.my_name') : s.name
      }))
    )
  );
  const filteredNodes = $derived.by(() => {
    const q = pickQuery.trim().toLowerCase();
    return q === '' ? allNodes : allNodes.filter((n) => n.name.toLowerCase().includes(q));
  });
  const shownNodes = $derived(filteredNodes.slice(0, pickLimit));
  const searchableNodes = $derived(allNodes.length > SEARCH_THRESHOLD);

  const modeItems = $derived([
    { value: 'manual', label: $t('cfg.groups.mode_manual') },
    { value: 'auto', label: $t('cfg.groups.mode_auto') },
    { value: 'fallback', label: $t('cfg.groups.mode_fallback') }
  ]);

  // --- Группа к отправке ---
  // Подстроки: через запятую или с новой строки; в режиме «Эксперт» запятая может быть
  // частью выражения, поэтому делится только по строкам
  function splitFilter(text: string, regex: boolean): string[] {
    return text
      .split(regex ? /\n/ : /[,\n]/)
      .map((v) => v.trim())
      .filter((v) => v !== '');
  }

  const draftGroup = $derived({
    id: group?.id ?? '',
    name: name.trim(),
    mode,
    sources: { all: allSources, ids: allSources ? [] : sourceIds },
    include: splitFilter(includeText, expert),
    exclude: splitFilter(excludeText, expert),
    regex: expert,
    picks,
    members: mode === 'manual' ? members : [],
    reserve: mode === 'fallback' ? reserve : ''
  });
  // Имя на состав не влияет: набор имени запросов предпросмотра не вызывает
  const payloadKey = $derived(JSON.stringify({ ...draftGroup, name: '' }));

  // --- Проверки полей ---
  const trimmedName = $derived(name.trim());
  const nameError = $derived.by(() => {
    if (nameServerError) return nameServerError;
    if (trimmedName === '') return $t('cfg.groups.name_required');
    const dup = groups.some(
      (g: GroupView) => g.id !== group?.id && g.name.toLowerCase() === trimmedName.toLowerCase()
    );
    return dup ? $t('cfg.groups.name_dup') : '';
  });
  const reserveError = $derived(
    reserveServerError ||
      (mode === 'fallback' && reserve === '' ? $t('cfg.groups.reserve_required') : '')
  );
  const hasError = $derived(nameError !== '' || reserveError !== '');

  // Режим ниже порога версии ядра: предупреждение, сохранение не блокируется
  const versionWarnings = $derived.by(() => {
    const out: string[] = [];
    const kernels = $layerSnapshot?.kernels ?? [];
    const version = (k: string) => kernels.find((x) => x.name === k)?.version || '?';
    const modeName = $t(`cfg.groups.mode_${mode}`);
    const xrayFeature =
      mode === 'auto' ? 'xray_auto_group' : mode === 'fallback' ? 'xray_fallback_group' : '';
    const xf = xrayFeature ? features[xrayFeature] : undefined;
    if (xf?.status === 'unavailable') {
      out.push(
        $t('cfg.groups.version_threshold', {
          kernel: 'Xray',
          version: version('xray'),
          mode: modeName,
          min: xf.min
        })
      );
    }
    const mf = features.mihomo_groups;
    if (mf?.status === 'unavailable') {
      out.push(
        $t('cfg.groups.version_threshold', {
          kernel: 'Mihomo',
          version: version('mihomo'),
          mode: modeName,
          min: mf.min
        })
      );
    }
    return out;
  });

  // --- Участники «Вручную» ---
  const addOptions = $derived.by(() => {
    const opts: { value: string; label: string; disabled?: boolean }[] = [
      { value: '', label: $t('cfg.groups.add_member'), disabled: true }
    ];
    for (const g of groups as GroupView[]) {
      if (g.id === group?.id) continue;
      if (members.some((m) => m.kind === 'group' && m.id === g.id)) continue;
      opts.push({ value: `group:${g.id}`, label: $t('cfg.groups.member_group', { name: g.name }) });
    }
    if (!members.some((m) => m.kind === 'direct')) opts.push({ value: 'direct', label: 'DIRECT' });
    if (!members.some((m) => m.kind === 'reject')) opts.push({ value: 'reject', label: 'REJECT' });
    return opts;
  });

  function memberName(m: Member): string {
    return targetLabel($t, m, groups);
  }

  function onAddMember(e: Event & { currentTarget: HTMLSelectElement }) {
    const v = e.currentTarget.value;
    if (v === '') return;
    const m: Member = v.startsWith('group:') ? { kind: 'group', id: v.slice(6) } : { kind: v };
    members = [...members, m];
    addValue = '';
    cycleError = '';
  }

  function moveMember(i: number, delta: number) {
    const j = i + delta;
    if (j < 0 || j >= members.length) return;
    const next = [...members];
    [next[i], next[j]] = [next[j], next[i]];
    members = next;
  }

  function removeMember(i: number) {
    members = members.filter((_, k) => k !== i);
    cycleError = '';
  }

  // --- Источники и узлы ---
  function toggleAllSources(e: Event) {
    const on = (e.currentTarget as HTMLInputElement).checked;
    allSources = on;
    // Снятая отметка «Все источники» начинает с полного набора, а не с пустого
    if (!on && sourceIds.length === 0) sourceIds = sourceList.map((s) => s.id);
  }

  function toggleSource(id: string, on: boolean) {
    sourceIds = on ? [...sourceIds.filter((x) => x !== id), id] : sourceIds.filter((x) => x !== id);
  }

  function togglePick(ref: string, on: boolean) {
    picks = on ? [...picks.filter((x) => x !== ref), ref] : picks.filter((x) => x !== ref);
  }

  function onPickQuery(e: Event) {
    pickQuery = (e.currentTarget as HTMLInputElement).value;
    pickLimit = PAGE_FIRST;
  }

  function onMode(v: string) {
    mode = v as GroupMode;
    reserveServerError = '';
  }

  // --- Предпросмотр: через 250 мс после последнего изменения, прежний запрос отменяется ---
  $effect(() => {
    if (!open) return;
    const key = payloadKey;
    void retryTick;
    const ctrl = new AbortController();
    previewBusy = true;
    const timer = setTimeout(() => {
      previewGroup(JSON.parse(key), ctrl.signal)
        .then((res) => {
          previewData = res;
          previewError = '';
          previewBusy = false;
        })
        .catch((err) => {
          if (ctrl.signal.aborted) return;
          const e = err as { code?: string; params?: Record<string, string>; message?: string };
          previewError = e.code ? routingErrorText($t, e.code, e.params) : (e.message ?? '');
          previewBusy = false;
        });
    }, PREVIEW_DELAY_MS);
    return () => {
      clearTimeout(timer);
      ctrl.abort();
    };
  });

  const kernelRows = $derived(
    KERNELS.map((k) => ({ ...k, p: previewData?.[k.id] ?? null })).filter(
      (r): r is { id: KernelName; name: string; p: NonNullable<typeof r.p> } => r.p !== null
    )
  );
  const emptyKernels = $derived(
    kernelRows.filter((r) => r.p.count === 0 && r.p.members.length === 0)
  );
  const emptyAll = $derived(kernelRows.length > 0 && emptyKernels.length === kernelRows.length);
  const emptyOne = $derived(emptyAll ? [] : emptyKernels);
  const previewLine = $derived(
    kernelRows
      .map((r) => $tp('cfg.groups.preview_nodes', r.p.count, { kernel: r.name }))
      .join(' · ')
  );

  // --- Открытие и закрытие ---
  function initialize() {
    name = group?.name ?? '';
    mode = group?.mode ?? 'manual';
    allSources = group ? group.sources.all : true;
    sourceIds = group ? [...group.sources.ids] : [];
    expert = group?.regex ?? false;
    includeText = (group?.include ?? []).join('\n');
    excludeText = (group?.exclude ?? []).join('\n');
    picks = [...(group?.picks ?? [])];
    members = (group?.members ?? []).map((m: Member) => ({ ...m }));
    reserve = group?.reserve ?? '';
    addValue = '';
    pickQuery = '';
    pickLimit = PAGE_FIRST;
    previewData = null;
    previewError = '';
    previewBusy = false;
    previewLimits = { xray: PAGE_FIRST, mihomo: PAGE_FIRST };
    saving = false;
    nameServerError = '';
    reserveServerError = '';
    cycleError = '';
    generalError = '';
    filterErrors = {};
    void loadSources();
  }

  $effect(() => {
    if (open) untrack(initialize);
  });

  function close() {
    // Пока идёт запись, диалог не закрывается: ответ определит, остаются ли значения
    if (saving) return;
    onclose();
  }

  function clearServerErrors() {
    nameServerError = '';
    reserveServerError = '';
    cycleError = '';
    generalError = '';
    filterErrors = {};
  }

  function showSaveError(err: unknown) {
    const e = err as { code?: string; params?: Record<string, string>; message?: string };
    if (e.code === 'draft_conflict' || e.code === 'apply_busy') {
      // Конфликт ревизии: тост и тихое перечитывание; диалог остаётся с введённым
      handleLayerError(err);
      return;
    }
    if (e.code === 'group_name_taken') {
      nameServerError = $t('cfg.groups.name_dup');
      return;
    }
    if (e.code === 'group_cycle') {
      cycleError = $t('cfg.groups.err_cycle', { group: e.params?.group ?? '?' });
      return;
    }
    if (e.code === 'invalid_input') {
      const field = e.params?.field ?? '';
      const reason = e.params?.reason ?? '';
      if (field === 'name') {
        nameServerError =
          reason === 'name_empty'
            ? $t('cfg.groups.name_required')
            : reason === 'name_dup'
              ? $t('cfg.groups.name_dup')
              : $t('cfg.groups.group_name_invalid');
        return;
      }
      if (field === 'reserve' && reason === 'reserve_required') {
        reserveServerError = $t('cfg.groups.reserve_required');
        return;
      }
      if (field === 'include' || field === 'exclude') {
        filterErrors = { ...filterErrors, [field]: routingErrorText($t, e.code, e.params) };
        return;
      }
    }
    generalError = e.code ? routingErrorText($t, e.code, e.params) : (e.message ?? '');
  }

  async function save() {
    if (hasError || saving) return;
    saving = true;
    clearServerErrors();
    try {
      await saveGroup(draftGroup);
      saving = false;
      onclose();
    } catch (err) {
      showSaveError(err);
    } finally {
      saving = false;
    }
  }
</script>

<Modal
  isOpen={open}
  title={group ? $t('cfg.groups.modal_edit') : $t('cfg.groups.modal_new')}
  maxWidth={mode === 'manual' ? '720px' : '640px'}
  dataTestid="group-modal"
  onclose={close}
>
  <div class="gm-body">
    <!-- Имя -->
    <Input
      id="{uid}-name"
      label={$t('cfg.groups.field_name')}
      maxlength={64}
      bind:value={name}
      error={nameError}
      oninput={() => (nameServerError = '')}
    />

    <!-- Режим -->
    <div class="gm-field">
      <span class="form-label" id="{uid}-mode-label">{$t('cfg.groups.field_mode')}</span>
      <div class="gm-mode" data-testid="group-mode">
        <SegmentedControl
          items={modeItems}
          value={mode}
          ariaLabel={$t('cfg.groups.mode_aria')}
          onchange={onMode}
        />
      </div>
      <p class="gm-hint" data-testid="group-mode-desc">{$t(`cfg.groups.mode_${mode}_desc`)}</p>
      {#each versionWarnings as text (text)}
        <p class="gm-warn" data-testid="group-version-warning">
          <span class="chip chip-warning">{text}</span>
        </p>
      {/each}
    </div>

    <!-- Состав -->
    <fieldset class="gm-set">
      <legend class="form-label">
        {mode === 'fallback' ? $t('cfg.groups.field_primary') : $t('cfg.groups.members_title')}
      </legend>

      <div class="gm-field">
        <label class="gm-check" data-testid="group-sources-all">
          <input type="checkbox" checked={allSources} onchange={toggleAllSources} />
          <span>{$t('cfg.groups.sources_all')}</span>
        </label>
        {#if !allSources}
          <div
            class="gm-checks"
            role="group"
            aria-label={$t('cfg.groups.field_sources')}
            data-testid="group-sources"
          >
            {#each sourceList as s (s.id)}
              <label class="gm-check">
                <input
                  type="checkbox"
                  checked={sourceIds.includes(s.id)}
                  onchange={(e) => toggleSource(s.id, e.currentTarget.checked)}
                />
                <span class="gm-wrap">{s.label}</span>
              </label>
            {/each}
          </div>
        {/if}
      </div>

      <div class="gm-field">
        <label class="form-label" for="{uid}-include">
          {$t('cfg.groups.field_include')}
          {#if expert}<span class="gm-tag">{$t('cfg.groups.regex_label')}</span>{/if}
        </label>
        <textarea
          id="{uid}-include"
          class="input gm-mono"
          rows="2"
          spellcheck="false"
          autocomplete="off"
          autocapitalize="off"
          bind:value={includeText}
          aria-describedby={filterErrors.include ? `${uid}-include-error` : `${uid}-subs-hint`}
          data-testid="group-include"></textarea>
        {#if filterErrors.include}
          <span class="error-text" id="{uid}-include-error">{filterErrors.include}</span>
        {/if}
      </div>

      <div class="gm-field">
        <label class="form-label" for="{uid}-exclude">
          {$t('cfg.groups.field_exclude')}
          {#if expert}<span class="gm-tag">{$t('cfg.groups.regex_label')}</span>{/if}
        </label>
        <textarea
          id="{uid}-exclude"
          class="input gm-mono"
          rows="2"
          spellcheck="false"
          autocomplete="off"
          autocapitalize="off"
          bind:value={excludeText}
          aria-describedby={filterErrors.exclude ? `${uid}-exclude-error` : `${uid}-subs-hint`}
          data-testid="group-exclude"></textarea>
        {#if filterErrors.exclude}
          <span class="error-text" id="{uid}-exclude-error">{filterErrors.exclude}</span>
        {/if}
        {#if !expert}
          <p class="gm-hint" id="{uid}-subs-hint">{$t('cfg.groups.subs_hint')}</p>
        {:else}
          <p class="gm-hint" id="{uid}-subs-hint">{$t('cfg.groups.regex_label')}</p>
        {/if}
      </div>

      <div class="gm-field">
        <span class="form-label" id="{uid}-picks-label">{$t('cfg.groups.field_picks')}</span>
        {#if searchableNodes}
          <input
            class="input gm-search"
            type="search"
            value={pickQuery}
            placeholder={$t('cfg.groups.picks_search')}
            aria-label={$t('cfg.groups.picks_search')}
            data-testid="group-picks-search"
            oninput={onPickQuery}
          />
        {/if}
        <div
          class="gm-checks gm-scroll"
          role="group"
          aria-labelledby="{uid}-picks-label"
          data-testid="group-picks"
        >
          {#each shownNodes as n (n.ref)}
            <label class="gm-check">
              <input
                type="checkbox"
                checked={picks.includes(n.ref)}
                onchange={(e) => togglePick(n.ref, e.currentTarget.checked)}
              />
              <span class="gm-wrap">{n.name}</span>
            </label>
          {/each}
          {#if filteredNodes.length > pickLimit}
            <div>
              <button
                type="button"
                class="btn btn-secondary btn-sm"
                onclick={() => (pickLimit += PAGE_STEP)}
              >
                {$t('cfg.groups.show_more', { n: PAGE_STEP })}
              </button>
            </div>
          {/if}
        </div>
        {#if picks.length === 0}
          <p class="gm-hint">{$t('cfg.groups.picks_none')}</p>
        {/if}
      </div>

      {#if mode === 'manual'}
        <div class="gm-field" data-testid="group-members">
          <span class="form-label" id="{uid}-members-label">{$t('cfg.groups.field_members')}</span>
          {#if members.length > 0}
            <ul class="gm-members" aria-labelledby="{uid}-members-label">
              {#each members as m, i (`${m.kind}:${m.id ?? ''}`)}
                {@const label = memberName(m)}
                <li class="gm-member">
                  <span class="gm-wrap gm-member-name">{label}</span>
                  <span class="gm-member-actions">
                    <button
                      type="button"
                      class="btn btn-secondary btn-sm"
                      disabled={i === 0}
                      aria-label={$t('cfg.groups.member_up_aria', { name: label })}
                      onclick={() => moveMember(i, -1)}
                    >
                      {$t('cfg.groups.member_up')}
                    </button>
                    <button
                      type="button"
                      class="btn btn-secondary btn-sm"
                      disabled={i === members.length - 1}
                      aria-label={$t('cfg.groups.member_down_aria', { name: label })}
                      onclick={() => moveMember(i, 1)}
                    >
                      {$t('cfg.groups.member_down')}
                    </button>
                    <button
                      type="button"
                      class="btn btn-secondary btn-sm"
                      aria-label={$t('cfg.groups.member_remove_aria', { name: label })}
                      onclick={() => removeMember(i)}
                    >
                      {$t('cfg.groups.member_remove')}
                    </button>
                  </span>
                </li>
              {/each}
            </ul>
          {/if}
          <Select
            bind:value={addValue}
            options={addOptions}
            ariaLabel={$t('cfg.groups.add_member')}
            data-testid="group-member-add"
            onchange={onAddMember}
          />
          {#if cycleError}
            <span class="error-text" role="alert" data-testid="group-cycle-error">
              {cycleError}
            </span>
          {/if}
        </div>
      {/if}

      {#if mode === 'fallback'}
        <div
          class="gm-field"
          role="group"
          aria-labelledby="{uid}-reserve-label"
          aria-describedby={reserveError ? `${uid}-reserve-error` : undefined}
        >
          <span class="form-label" id="{uid}-reserve-label">{$t('cfg.groups.field_reserve')}</span>
          <Select
            bind:value={reserve}
            options={[
              { value: '', label: '—', disabled: true },
              ...allNodes.map((n) => ({ value: n.ref, label: n.name }))
            ]}
            ariaLabel={$t('cfg.groups.field_reserve')}
            data-testid="group-reserve"
            onchange={() => (reserveServerError = '')}
          />
          {#if reserveError}
            <span class="error-text" id="{uid}-reserve-error" data-testid="group-reserve-error">
              {reserveError}
            </span>
          {/if}
        </div>
      {/if}
    </fieldset>

    <!-- Предпросмотр -->
    <section class="gm-preview" aria-labelledby="{uid}-preview-title" data-testid="group-preview">
      <h3 class="gm-preview-title" id="{uid}-preview-title">{$t('cfg.groups.preview_title')}</h3>
      <div class="gm-preview-status" aria-live="polite">
        {#if previewBusy}
          <span class="gm-status" data-testid="group-preview-busy">
            <span class="spinner" aria-hidden="true"></span>
            {$t('cfg.groups.computing')}
          </span>
        {:else if previewError}
          <span class="gm-status gm-status-error" data-testid="group-preview-error">
            <Icon name="warning" size={14} />
            <span>{$t('cfg.groups.preview_failed', { reason: previewError })}</span>
            <button type="button" class="btn btn-secondary btn-sm" onclick={() => (retryTick += 1)}>
              {$t('cfg.nodes.retry')}
            </button>
          </span>
        {/if}
      </div>
      <div class="gm-preview-body" class:gm-stale={previewBusy}>
        {#if previewLine}
          <p class="gm-preview-line" data-testid="group-preview-line">{previewLine}</p>
        {/if}
        {#each kernelRows as r (r.id)}
          {#if r.p.names.length > 0}
            <div class="gm-names" data-testid="group-preview-{r.id}">
              {#if kernelRows.length > 1}
                <span class="gm-names-title">{kernelLabel(r.id)}</span>
              {/if}
              <ul class="gm-names-list">
                {#each r.p.names.slice(0, previewLimits[r.id]) as n, i (i)}
                  <li class="gm-wrap">{n}</li>
                {/each}
              </ul>
              {#if r.p.names.length > previewLimits[r.id]}
                <button
                  type="button"
                  class="btn btn-secondary btn-sm"
                  onclick={() => (previewLimits[r.id] += PAGE_STEP)}
                >
                  {$t('cfg.groups.preview_more', { n: PAGE_STEP })}
                </button>
              {/if}
            </div>
          {/if}
        {/each}
      </div>
      {#if emptyAll}
        <div class="alert alert-error gm-alert" role="alert" data-testid="group-empty-all">
          <Icon name="warning" size={16} class="gm-alert-icon" />
          <span>{$t('cfg.groups.empty_all')}</span>
        </div>
      {/if}
      {#each emptyOne as r (r.id)}
        <p class="gm-warn" data-testid="group-empty-{r.id}">
          <span class="chip chip-warning">
            {$t('cfg.groups.empty_one_kernel', { kernel: r.name })}
          </span>
        </p>
      {/each}
    </section>

    <!-- Эксперт -->
    <div class="gm-expert">
      <label class="toggle-switch">
        <input type="checkbox" id="{uid}-expert" bind:checked={expert} data-testid="group-expert" />
        <span class="toggle-slider"></span>
      </label>
      <label class="gm-expert-label" for="{uid}-expert">{$t('cfg.groups.expert')}</label>
    </div>

    {#if generalError}
      <div class="alert alert-error gm-alert" role="alert" data-testid="group-save-error">
        <Icon name="warning" size={16} class="gm-alert-icon" />
        <span>{generalError}</span>
      </div>
    {/if}

    <div class="gm-footer">
      <Button
        variant="primary"
        disabled={hasError}
        loading={saving}
        data-testid="group-save"
        onclick={save}
      >
        {$t('cfg.nodes.save_draft')}
      </Button>
      <Button variant="secondary" disabled={saving} data-testid="group-cancel" onclick={close}>
        {$t('cfg.nodes.cancel')}
      </Button>
    </div>
  </div>
</Modal>

<style>
  .gm-body {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-4);
    min-width: 0;
  }

  .gm-body :global(.form-group) {
    margin-bottom: 0;
  }

  .gm-field {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-2);
    min-width: 0;
  }

  .gm-set {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-3);
    min-width: 0;
    margin: 0;
    padding: var(--spacing-3);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
  }

  .gm-set > legend {
    padding: 0 var(--spacing-2);
  }

  .gm-hint {
    margin: 0;
    font-size: var(--font-size-xs);
    font-weight: 400;
    line-height: 1.4;
    color: var(--fg-dim);
    overflow-wrap: anywhere;
  }

  .gm-warn {
    margin: 0;
    font-size: var(--font-size-xs);
    overflow-wrap: anywhere;
  }

  .gm-warn .chip {
    white-space: normal;
    max-width: 100%;
    overflow-wrap: anywhere;
  }

  .gm-tag {
    margin-left: var(--spacing-2);
    font-size: var(--font-size-xs);
    font-weight: 400;
    color: var(--fg-dim);
  }

  .gm-mode {
    max-width: 100%;
    overflow-x: auto;
  }

  .gm-wrap {
    min-width: 0;
    overflow-wrap: anywhere;
  }

  textarea.gm-mono {
    width: 100%;
    height: auto;
    min-height: 0;
    padding: var(--spacing-2) var(--spacing-3);
    resize: vertical;
    overflow-y: auto;
    font-family: var(--font-family-mono);
    font-size: var(--font-size-xs);
    font-weight: 400;
    line-height: 1.5;
    overflow-wrap: anywhere;
    word-break: break-word;
    white-space: pre-wrap;
  }

  .gm-search {
    width: 100%;
    max-width: 360px;
  }

  .gm-checks {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-1);
    min-width: 0;
  }

  .gm-scroll {
    max-height: 220px;
    overflow-y: auto;
    padding: var(--spacing-2);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    scrollbar-width: thin;
  }

  .gm-check {
    display: flex;
    align-items: flex-start;
    gap: var(--spacing-2);
    min-width: 0;
    min-height: 28px;
    font-size: var(--font-size-base);
    color: var(--fg-primary);
    cursor: pointer;
  }

  .gm-check input {
    flex-shrink: 0;
    margin-top: 3px;
    accent-color: var(--accent);
  }

  .gm-members {
    display: flex;
    flex-direction: column;
    margin: 0;
    padding: 0;
    list-style: none;
  }

  .gm-member {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    justify-content: space-between;
    gap: var(--spacing-1) var(--spacing-2);
    min-width: 0;
    padding: var(--spacing-2) 0;
    border-top: 1px solid var(--border);
  }

  .gm-member:first-child {
    border-top: 0;
  }

  .gm-member-name {
    font-size: var(--font-size-base);
    font-weight: 600;
  }

  .gm-member-actions {
    display: inline-flex;
    flex-wrap: wrap;
    gap: var(--spacing-1);
  }

  .gm-preview {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-2);
    min-width: 0;
    padding: var(--spacing-3);
    background: var(--bg-elevated);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
  }

  .gm-preview-title {
    margin: 0;
    font-size: var(--font-size-base);
    font-weight: 600;
    color: var(--fg-primary);
  }

  /* Строка состояния занимает место всегда: высота блока не прыгает */
  .gm-preview-status {
    min-height: 28px;
    display: flex;
    align-items: center;
  }

  .gm-status {
    display: inline-flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--spacing-2);
    font-size: var(--font-size-xs);
    color: var(--fg-secondary);
    overflow-wrap: anywhere;
  }

  .gm-status-error {
    color: var(--fg-secondary);
  }

  .gm-preview-body {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-2);
    min-height: 24px;
    min-width: 0;
  }

  .gm-stale {
    color: var(--fg-dim);
  }

  .gm-preview-line {
    margin: 0;
    font-size: var(--font-size-base);
    font-weight: 600;
    color: inherit;
    overflow-wrap: anywhere;
  }

  .gm-names {
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    gap: var(--spacing-1);
    min-width: 0;
  }

  .gm-names-title {
    font-size: var(--font-size-xs);
    font-weight: 600;
    color: var(--fg-secondary);
  }

  .gm-names-list {
    width: 100%;
    max-height: 180px;
    margin: 0;
    padding: 0 var(--spacing-2) 0 0;
    list-style: none;
    overflow-y: auto;
    scrollbar-width: thin;
    font-size: var(--font-size-xs);
    line-height: 1.5;
    color: var(--fg-secondary);
  }

  .gm-alert {
    margin: 0;
    min-width: 0;
    overflow-wrap: anywhere;
  }

  .gm-alert :global(.gm-alert-icon) {
    flex-shrink: 0;
    margin-top: 3px;
  }

  .gm-expert {
    display: flex;
    align-items: center;
    gap: var(--spacing-3);
  }

  .gm-expert-label {
    font-size: var(--font-size-base);
    color: var(--fg-secondary);
    cursor: pointer;
  }

  .gm-footer {
    display: flex;
    flex-wrap: wrap;
    justify-content: flex-end;
    gap: var(--spacing-2);
  }

  @media (pointer: coarse), (max-width: 768px) {
    .gm-check {
      min-height: 44px;
      align-items: center;
    }

    .gm-check input {
      margin-top: 0;
    }

    .gm-member-actions .btn {
      min-height: 44px;
    }
  }

  @media (max-width: 480px) {
    .gm-footer {
      flex-direction: column;
    }

    .gm-footer :global(.btn) {
      width: 100%;
    }
  }
</style>
