<script lang="ts">
  import { untrack } from 'svelte';
  import { t, tp, currentLang } from '../../../i18n';
  import Modal from '../../Modal.svelte';
  import Button from '../../Button.svelte';
  import Select from '../../Select.svelte';
  import Icon from '../../../lib/components/Icon.svelte';
  import { handleLayerError } from '../../../lib/configLayer';
  import {
    applyPreset,
    fetchPresets,
    planPreset,
    routingErrorText
  } from '../../../lib/configRouting';
  import type {
    PresetChoice,
    PresetInfo,
    PresetPlan,
    PresetsView,
    RuleSummary,
    ServiceView
  } from '../../../lib/configRouting';
  import { condLabelKey } from '../../../lib/routingView';
  import { applyFromUi, reasonOf, routingBusy, takeIntoDraft } from '../routing/routingOwner';

  // Диалог пресета (D-24…D-31): карточки пресетов, страна, сервисы для «Выборочно», галочки
  // и план «Будет создано» либо «Уйдёт / Придёт» до записи. Создание — одна правка черновика
  // (applyPreset); в режимах take и intercept (D-04) она же берёт маршрутизацию. Закрытие без
  // выбора при взятии оставляет маршрутизацию взятой в черновике с «Всё остальное → DIRECT»
  // и пустым списком правил. Названия и значения из каталога выводятся только интерполяцией.
  let {
    open,
    mode = 'preset',
    onclose
  }: {
    open: boolean;
    /** preset — кнопка «Пресет»; take — взятие маршрутизации; intercept — взятие из «Применить». */
    mode?: 'preset' | 'take' | 'intercept';
    onclose: () => void;
  } = $props();

  const uid = $props.id();

  /** Пауза перед расчётом плана после изменения выбора. */
  const PLAN_DEBOUNCE_MS = 250;
  /** Значений условия в строке плана; остальное — «+{n}». */
  const VALUES_SHOWN = 3;
  /** Пресеты, которые показываются, пока список пресетов с роутера не прочитан. */
  const FALLBACK_IDS = ['all_proxy', 'except_country', 'selective', 'empty'];

  let view = $state<PresetsView | null>(null);
  let loading = $state(false);
  let failure = $state('');
  let selectedId = $state('');
  let country = $state('');
  let services = $state<string[]>([]);
  let flagQuic = $state(true);
  let flagTorrent = $state(true);
  let flagAds = $state(true);
  let flagGroups = $state(false);
  let plan = $state<PresetPlan | null>(null);
  let planLoading = $state(false);
  let planError = $state('');
  let busy = $state(false);
  let errorText = $state('');
  // Растёт с каждым чтением: устаревший ответ не затирает более свежий
  let seq = 0;
  let planSeq = 0;

  const takeMode = $derived(mode !== 'preset');

  async function load() {
    const mine = ++seq;
    loading = true;
    failure = '';
    try {
      const data = await fetchPresets();
      if (mine !== seq) return;
      view = {
        ...data,
        presets: Array.isArray(data.presets) ? data.presets : [],
        services: Array.isArray(data.services) ? data.services : []
      };
    } catch (e) {
      if (mine === seq) failure = reasonOf(e);
    } finally {
      if (mine === seq) loading = false;
    }
  }

  function reset() {
    seq++;
    planSeq++;
    view = null;
    loading = false;
    failure = '';
    selectedId = '';
    country = '';
    services = [];
    flagQuic = true;
    flagTorrent = true;
    flagAds = true;
    flagGroups = false;
    plan = null;
    planLoading = false;
    planError = '';
    busy = false;
    errorText = '';
  }

  $effect(() => {
    if (open) {
      untrack(() => {
        reset();
        void load();
      });
    } else {
      untrack(reset);
    }
  });

  // --- Карточки ---

  const infos = $derived(
    new Map<string, PresetInfo>((view?.presets ?? []).map((p: PresetInfo) => [p.id, p]))
  );

  /** Карточки по порядку сервера; «Начать с пустого» — только при взятии (D-04). */
  const cardIds = $derived.by(() => {
    const ids = view !== null ? view.presets.map((p: PresetInfo) => p.id) : FALLBACK_IDS;
    return ids.filter((id: string) => id !== 'empty' || takeMode);
  });

  const regionNames = $derived.by(() => {
    try {
      return new Intl.DisplayNames([$currentLang, 'en'], { type: 'region' });
    } catch {
      return null;
    }
  });

  function countryName(code: string): string {
    if (code === '') return '';
    try {
      return regionNames?.of(code.toUpperCase()) || code;
    } catch {
      return code;
    }
  }

  function defaultCountryOf(info: PresetInfo | undefined): string {
    const list = info?.countries ?? [];
    const def = view?.default_country ?? '';
    if (def !== '' && list.includes(def)) return def;
    return list[0] ?? '';
  }

  /** Страна в названии карточки: выбранная, иначе страна по умолчанию. */
  const titleCountry = $derived(countryName(country || view?.default_country || 'RU'));

  function cardTitle(id: string): string {
    switch (id) {
      case 'all_proxy':
        return $t('cfg.preset.all_proxy_title');
      case 'except_country':
        return $t('cfg.preset.except_title', { country: titleCountry });
      case 'selective':
        return $t('cfg.preset.selective_title');
      default:
        return $t('cfg.preset.empty_title');
    }
  }

  function cardDesc(id: string): string {
    switch (id) {
      case 'all_proxy':
        return $t('cfg.preset.all_proxy_desc');
      case 'except_country':
        return $t('cfg.preset.except_desc', { country: titleCountry });
      case 'selective':
        return $t('cfg.preset.selective_desc');
      default:
        return $t('cfg.preset.empty_desc');
    }
  }

  /** Карточка недоступна: нет данных стран, либо список пресетов не прочитан (нужны страны). */
  function cardDisabled(id: string): boolean {
    if (busy) return true;
    const info = infos.get(id);
    if (view === null) return id === 'except_country' || id === 'selective';
    return (info?.needs_country ?? false) && (info?.countries.length ?? 0) === 0;
  }

  function select(id: string) {
    if (selectedId === id) return;
    selectedId = id;
    errorText = '';
    const info = infos.get(id);
    const cur = view?.current ?? null;
    if (cur !== null && cur.id === id) {
      // Замена того же пресета: начинаем с прежнего выбора
      country = info?.countries.includes(cur.country) ? cur.country : defaultCountryOf(info);
      services = [...(cur.services ?? [])];
      flagQuic = cur.flags?.quic ?? true;
      flagTorrent = cur.flags?.torrent ?? true;
      flagAds = adsAvailable ? (cur.flags?.ads ?? true) : false;
      flagGroups = cur.flags?.service_groups ?? false;
      return;
    }
    if (info?.needs_country) {
      if (!info.countries.includes(country)) country = defaultCountryOf(info);
    }
  }

  // --- Поля выбранного пресета ---

  const info = $derived(selectedId === '' ? undefined : infos.get(selectedId));
  const needsCountry = $derived(info?.needs_country ?? false);
  const needsServices = $derived(info?.needs_services ?? false);
  const plain = $derived(selectedId === 'empty');
  const adsAvailable = $derived(view?.ads_available ?? false);
  const countryOptions = $derived(
    (info?.countries ?? []).map((code: string) => ({ value: code, label: countryName(code) }))
  );
  const serviceList = $derived(view?.services ?? []);

  function serviceName(sv: ServiceView): string {
    const ru = sv.name_ru || sv.name_en;
    const en = sv.name_en || sv.name_ru;
    return ($currentLang === 'ru' ? ru : en) || sv.id;
  }

  function toggleService(id: string, on: boolean) {
    services = on ? [...services.filter((s) => s !== id), id] : services.filter((s) => s !== id);
  }

  function choiceOf(): PresetChoice {
    return {
      id: selectedId,
      country: needsCountry ? country : undefined,
      services: needsServices ? [...services] : undefined,
      flags: {
        quic: flagQuic,
        torrent: flagTorrent,
        ads: adsAvailable && flagAds,
        service_groups: needsServices && flagGroups
      }
    };
  }

  // --- План ---

  function clearPlan() {
    planSeq++;
    plan = null;
    planLoading = false;
    planError = '';
  }

  async function loadPlan(choice: PresetChoice, mine: number) {
    try {
      const res = await planPreset(choice);
      if (mine !== planSeq) return;
      plan = {
        ...res,
        remove: Array.isArray(res.remove) ? res.remove : [],
        add: Array.isArray(res.add) ? res.add : [],
        groups: Array.isArray(res.groups) ? res.groups : []
      };
    } catch (e) {
      if (mine !== planSeq) return;
      plan = null;
      planError = routingErrorText(
        $t,
        (e as { code?: string })?.code,
        (e as { params?: Record<string, string> })?.params
      );
    } finally {
      if (mine === planSeq) planLoading = false;
    }
  }

  /** Ставит расчёт плана после паузы; возвращает отмену таймера для смены выбора. */
  function schedulePlan(choice: PresetChoice): () => void {
    const mine = ++planSeq;
    planLoading = true;
    planError = '';
    const timer = setTimeout(() => void loadPlan(choice, mine), PLAN_DEBOUNCE_MS);
    return () => clearTimeout(timer);
  }

  $effect(() => {
    // Выбор целиком — зависимость эффекта; запись плана в untrack
    const ready = open && view !== null && selectedId !== '';
    const choice = ready ? choiceOf() : null;
    if (choice === null) {
      untrack(clearPlan);
      return;
    }
    return untrack(() => schedulePlan(choice));
  });

  const replacing = $derived(plan !== null && plan.remove.length > 0);

  function summaryOf(r: RuleSummary): string {
    const parts = r.conds.map((c) => {
      const vals = [
        ...(c.type === 'network' ? c.values.map((v) => v.toUpperCase()) : c.values),
        ...c.lists.map((l) => `${l.file} · ${l.tag}`)
      ];
      const shown = vals.slice(0, VALUES_SHOWN).join(', ');
      const more =
        vals.length > VALUES_SHOWN
          ? ` ${$t('cfg.rules.more_values', { n: vals.length - VALUES_SHOWN })}`
          : '';
      return vals.length > 0
        ? `${$t(condLabelKey(c.type))} · ${shown}${more}`
        : $t(condLabelKey(c.type));
    });
    const head = parts.join(` ${$t('cfg.rules.and')} `);
    const target =
      r.target.kind === 'direct'
        ? 'DIRECT'
        : r.target.kind === 'reject'
          ? 'REJECT'
          : r.target_name || '?';
    return `${head} → ${target}`;
  }

  // --- Создание и закрытие ---

  const canCreate = $derived(
    !busy &&
      !loading &&
      selectedId !== '' &&
      (view !== null || selectedId === 'empty' || selectedId === 'all_proxy') &&
      (!needsCountry || country !== '')
  );

  async function afterDraft() {
    if (mode === 'intercept') await applyFromUi();
  }

  async function create() {
    if (!canCreate) return;
    busy = true;
    errorText = '';
    if (takeMode) routingBusy.set(true);
    try {
      if (selectedId === 'empty' && view === null) {
        // Список пресетов не прочитан: «пустой» — это просто взятие в черновике
        const ok = await takeIntoDraft();
        if (ok) await afterDraft();
        onclose();
        return;
      }
      await applyPreset(choiceOf(), takeMode);
    } catch (e) {
      if ((e as { code?: string })?.code === 'draft_conflict') {
        handleLayerError(e);
      } else {
        errorText = routingErrorText(
          $t,
          (e as { code?: string })?.code,
          (e as { params?: Record<string, string> })?.params
        );
      }
      busy = false;
      if (takeMode) routingBusy.set(false);
      return;
    }
    if (takeMode) routingBusy.set(false);
    await afterDraft();
    busy = false;
    onclose();
  }

  /** Закрытие без выбора: при взятии маршрутизация остаётся взятой в черновике (D-04). */
  async function close() {
    if (busy) return;
    if (!takeMode) {
      onclose();
      return;
    }
    busy = true;
    try {
      const ok = await takeIntoDraft();
      if (ok) await afterDraft();
    } finally {
      busy = false;
    }
    onclose();
  }
</script>

<Modal
  isOpen={open}
  title={$t('cfg.preset.title')}
  maxWidth="720px"
  dataTestid="preset-modal"
  onclose={() => void close()}
>
  <div class="pm-body" data-mode={mode}>
    {#if loading}
      <div class="pm-loading" role="status" data-testid="preset-loading">
        <span class="spinner" aria-hidden="true"></span>
        <span>{$t('cfg.list.loading')}</span>
      </div>
    {:else}
      <span class="sr-only" id="{uid}-cards">{$t('cfg.preset.cards_aria')}</span>
      <div class="pm-cards" role="radiogroup" aria-labelledby="{uid}-cards">
        {#each cardIds as id (id)}
          <label
            class="pm-card"
            class:pm-selected={selectedId === id}
            class:pm-disabled={cardDisabled(id)}
          >
            <input
              type="radio"
              class="sr-only"
              name="{uid}-preset"
              value={id}
              checked={selectedId === id}
              disabled={cardDisabled(id)}
              data-testid="preset-card-{id}"
              onchange={() => select(id)}
            />
            <span class="pm-card-title">{cardTitle(id)}</span>
            <span class="pm-card-desc">{cardDesc(id)}</span>
          </label>
        {/each}
      </div>

      {#if failure !== ''}
        <div class="pm-block">
          <h3 class="pm-title">{$t('cfg.preset.services')}</h3>
          <div class="alert alert-error pm-alert" role="alert" data-testid="preset-load-error">
            <Icon name="warning" size={16} class="pm-alert-icon" />
            <span>{$t('cfg.list.load_failed', { reason: failure })}</span>
          </div>
          <div>
            <Button variant="secondary" data-testid="preset-retry" onclick={() => void load()}>
              {$t('cfg.nodes.retry')}
            </Button>
          </div>
        </div>
      {/if}

      {#if needsCountry && countryOptions.length > 0}
        <div class="pm-block">
          <label class="form-label" for="{uid}-country">{$t('cfg.preset.country')}</label>
          <Select
            id="{uid}-country"
            bind:value={country}
            options={countryOptions}
            disabled={busy}
            data-testid="preset-country"
          />
        </div>
      {/if}

      {#if needsServices}
        <div class="pm-block">
          <h3 class="pm-title" id="{uid}-services">{$t('cfg.preset.services')}</h3>
          <div
            class="pm-checks pm-scroll"
            role="group"
            aria-labelledby="{uid}-services"
            data-testid="preset-services"
          >
            {#each serviceList as sv (sv.id)}
              <label class="pm-check">
                <input
                  type="checkbox"
                  checked={services.includes(sv.id)}
                  disabled={busy}
                  data-testid="preset-service-{sv.id}"
                  onchange={(e) => toggleService(sv.id, e.currentTarget.checked)}
                />
                <span class="pm-wrap">{serviceName(sv)}</span>
              </label>
            {/each}
          </div>
        </div>
      {/if}

      {#if selectedId !== '' && !plain && view !== null}
        <div class="pm-block pm-flags">
          <label class="pm-check">
            <input
              type="checkbox"
              bind:checked={flagQuic}
              disabled={busy}
              data-testid="preset-flag-quic"
            />
            <span class="pm-wrap">{$t('cfg.preset.flag_quic')}</span>
          </label>
          <label class="pm-check">
            <input
              type="checkbox"
              bind:checked={flagTorrent}
              disabled={busy}
              data-testid="preset-flag-torrent"
            />
            <span class="pm-wrap">{$t('cfg.preset.flag_torrent')}</span>
          </label>
          <label class="pm-check">
            <input
              type="checkbox"
              checked={adsAvailable && flagAds}
              disabled={busy || !adsAvailable}
              data-testid="preset-flag-ads"
              onchange={(e) => (flagAds = e.currentTarget.checked)}
            />
            <span class="pm-wrap">
              {adsAvailable ? $t('cfg.preset.flag_ads') : $t('cfg.preset.flag_ads_missing')}
            </span>
          </label>
          {#if needsServices}
            <label class="pm-check">
              <input
                type="checkbox"
                bind:checked={flagGroups}
                disabled={busy}
                data-testid="preset-flag-service_groups"
              />
              <span class="pm-wrap">{$t('cfg.preset.flag_service_groups')}</span>
            </label>
          {/if}
          <p class="pm-note">{$t('cfg.preset.private_note')}</p>
        </div>
      {/if}

      {#if selectedId !== '' && view !== null}
        <section class="pm-block" aria-live="polite" data-testid="preset-plan">
          <h3 class="pm-title">{$t('cfg.preset.plan_title')}</h3>
          {#if planError !== ''}
            <div class="alert alert-error pm-alert" role="alert" data-testid="preset-plan-error">
              <Icon name="warning" size={16} class="pm-alert-icon" />
              <span>{planError}</span>
            </div>
          {:else if plan === null}
            <div class="pm-plan-wait" data-testid="preset-plan-loading">
              <span class="spinner" aria-hidden="true"></span>
              <span>{$t('cfg.preset.plan_title')}…</span>
            </div>
          {:else if replacing}
            <div class="pm-plan-part" data-testid="preset-plan-leave">
              <h4 class="pm-plan-head pm-leave">
                <span aria-hidden="true">−</span>
                {$t('cfg.preset.plan_leave')}
              </h4>
              <ul class="pm-plan-list pm-scroll">
                {#each plan.remove as r, i (i)}
                  <li class="pm-mono">{summaryOf(r)}</li>
                {/each}
              </ul>
            </div>
            <div class="pm-plan-part" data-testid="preset-plan-come">
              <h4 class="pm-plan-head pm-come">
                <span aria-hidden="true">+</span>
                {$t('cfg.preset.plan_come')}
              </h4>
              {#if plan.add.length > 0}
                <ul class="pm-plan-list pm-scroll">
                  {#each plan.add as r, i (i)}
                    <li class="pm-mono">{summaryOf(r)}</li>
                  {/each}
                </ul>
              {:else}
                <p class="pm-note">{$t('cfg.preset.plan_none')}</p>
              {/if}
            </div>
            <p class="pm-note" data-testid="preset-plan-note">
              {$t('cfg.preset.plan_replace_note')}
            </p>
          {:else if plan.add.length === 0 && plan.groups.length === 0}
            <p class="pm-note" data-testid="preset-plan-none">{$t('cfg.preset.plan_none')}</p>
          {:else}
            <p class="pm-plan-first" data-testid="preset-plan-first">
              {$t('cfg.preset.plan_first', {
                rules: $tp('cfg.preset.plan_first_rules', plan.add.length),
                groups: $tp('cfg.preset.plan_first_groups', plan.groups.length)
              })}
            </p>
            {#if plan.add.length > 0}
              <ul
                class="pm-plan-list pm-scroll"
                aria-label={$t('cfg.preset.plan_created')}
                data-testid="preset-plan-list"
              >
                {#each plan.add as r, i (i)}
                  <li class="pm-mono">{summaryOf(r)}</li>
                {/each}
              </ul>
            {/if}
          {/if}
        </section>
      {/if}

      {#if errorText !== ''}
        <div class="alert alert-error pm-alert" role="alert" data-testid="preset-error">
          <Icon name="warning" size={16} class="pm-alert-icon" />
          <span>{errorText}</span>
        </div>
      {/if}
    {/if}

    <div class="pm-footer">
      <Button
        variant="primary"
        disabled={!canCreate}
        loading={busy}
        data-testid="preset-create"
        onclick={create}
      >
        {$t(takeMode ? 'cfg.preset.take_and_create' : 'cfg.preset.create')}
      </Button>
      <Button
        variant="secondary"
        disabled={busy}
        data-testid="preset-cancel"
        onclick={() => void close()}
      >
        {$t('app.cancel')}
      </Button>
    </div>
  </div>
</Modal>

<style>
  .pm-body {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-4);
    min-width: 0;
  }

  .pm-body :global(.form-group) {
    margin-bottom: 0;
  }

  .pm-loading {
    display: flex;
    align-items: center;
    justify-content: center;
    gap: var(--spacing-2);
    min-height: 160px;
    color: var(--fg-secondary);
  }

  .pm-cards {
    display: grid;
    grid-template-columns: repeat(2, minmax(0, 1fr));
    gap: var(--spacing-4);
  }

  .pm-card {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-1);
    min-width: 0;
    padding: var(--spacing-3) var(--spacing-4);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    background: var(--bg-card);
    cursor: pointer;
    overflow-wrap: anywhere;
  }

  .pm-card:hover {
    background: var(--bg-hover);
  }

  .pm-card.pm-selected {
    border-color: var(--accent);
    background: var(--accent-soft);
  }

  /* На подложке выбора вторичный цвет в тёмной теме не дотягивает до 4.5:1 */
  .pm-card.pm-selected .pm-card-desc {
    color: var(--fg-primary);
  }

  .pm-card:has(input:focus-visible) {
    outline: 2px solid var(--accent);
    outline-offset: 2px;
  }

  .pm-card.pm-disabled {
    opacity: 0.55;
    cursor: not-allowed;
  }

  .pm-card-title {
    font-size: var(--font-size-base);
    font-weight: 600;
    color: var(--fg-primary);
  }

  .pm-card-desc {
    font-size: var(--font-size-sm);
    color: var(--fg-secondary);
  }

  .pm-block {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-2);
    min-width: 0;
  }

  .pm-title {
    margin: 0;
    font-size: var(--font-size-base);
    font-weight: 600;
    color: var(--fg-primary);
  }

  .pm-checks {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-1);
    min-width: 0;
  }

  .pm-scroll {
    max-height: 220px;
    overflow-y: auto;
    scrollbar-width: thin;
  }

  .pm-checks.pm-scroll {
    padding: var(--spacing-2);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
  }

  .pm-check {
    display: flex;
    align-items: flex-start;
    gap: var(--spacing-2);
    min-width: 0;
    min-height: 28px;
    font-size: var(--font-size-base);
    color: var(--fg-primary);
    cursor: pointer;
  }

  .pm-check input {
    flex-shrink: 0;
    margin-top: 3px;
    accent-color: var(--accent);
  }

  .pm-wrap {
    min-width: 0;
    overflow-wrap: anywhere;
  }

  .pm-flags {
    gap: var(--spacing-1);
  }

  .pm-note {
    margin: 0;
    font-size: var(--font-size-xs);
    color: var(--fg-secondary);
    overflow-wrap: anywhere;
  }

  .pm-plan-first {
    margin: 0;
    font-size: var(--font-size-base);
    color: var(--fg-primary);
    overflow-wrap: anywhere;
  }

  .pm-plan-wait {
    display: flex;
    align-items: center;
    gap: var(--spacing-2);
    color: var(--fg-secondary);
  }

  .pm-plan-part {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-1);
    min-width: 0;
  }

  .pm-plan-head {
    display: flex;
    align-items: baseline;
    gap: var(--spacing-1);
    margin: 0;
    font-size: var(--font-size-sm);
    font-weight: 600;
    color: var(--fg-primary);
  }

  .pm-leave span {
    color: var(--danger);
  }

  .pm-come span {
    color: var(--success);
  }

  .pm-plan-list {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-1);
    margin: 0;
    padding: 0 0 0 var(--spacing-4);
    min-width: 0;
  }

  .pm-mono {
    font-family: var(--font-family-mono);
    font-size: var(--font-size-xs);
    color: var(--fg-secondary);
    overflow-wrap: anywhere;
    word-break: break-word;
  }

  .pm-alert {
    margin: 0;
    min-width: 0;
    overflow-wrap: anywhere;
  }

  .pm-alert :global(.pm-alert-icon) {
    flex-shrink: 0;
    margin-top: 3px;
  }

  .pm-footer {
    display: flex;
    flex-wrap: wrap;
    justify-content: flex-end;
    gap: var(--spacing-2);
  }

  @media (max-width: 768px) {
    .pm-cards {
      grid-template-columns: minmax(0, 1fr);
    }
  }

  @media (pointer: coarse), (max-width: 768px) {
    .pm-check {
      min-height: 44px;
      align-items: center;
    }

    .pm-check input {
      margin-top: 0;
    }

    .pm-footer :global(.btn) {
      min-height: 44px;
    }
  }

  @media (max-width: 480px) {
    .pm-footer {
      flex-direction: column;
    }

    .pm-footer :global(.btn) {
      width: 100%;
    }
  }
</style>
