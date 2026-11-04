<script module lang="ts">
  import { dnsTakeover as dnsTakeoverStore } from '../../../lib/configDns';

  // Отметка «сбой просмотрен» переживает перемонтирование карточки при смене ядра: ключ
  // сбоя, после которого пользователь нажал действие карточки. Новая транзакция снимает её
  // даже без открытой карточки: сбой новой транзакции показывается, даже если совпадает с
  // прежним.
  const failureSeen = $state({ key: '' });
  // Отметка «исход просмотрен»: время исхода, после которого пользователь нажал действие
  // карточки. Строка исхода скрывается сразу, не дожидаясь перечитывания вида (бэкенд
  // снимает исход тем же действием)
  const outcomeSeen = $state({ at: 0 });
  // Состояние раскрытия «Параметры перехвата» сохраняется при переключении вкладки ядра
  const paramsOpen = $state({ open: false });
  dnsTakeoverStore.subscribe((v) => {
    if (v?.state === 'applying' && failureSeen.key !== '') failureSeen.key = '';
  });
</script>

<script lang="ts">
  import { onDestroy } from 'svelte';
  import { createSubscriber } from 'svelte/reactivity';
  import { t } from '../../../i18n';
  import { showConfirm, showToast } from '../../../stores';
  import Button from '../../Button.svelte';
  import Select from '../../Select.svelte';
  import Skeleton from '../../Skeleton.svelte';
  import Icon from '../../../lib/components/Icon.svelte';
  import DnsTakeoverSteps from './DnsTakeoverSteps.svelte';
  import DnsResolveCheck from './DnsResolveCheck.svelte';
  import DnsLocalZones from './DnsLocalZones.svelte';
  import DnsOutcomeNote from './DnsOutcomeNote.svelte';
  import DnsLegacyNote from './DnsLegacyNote.svelte';
  import {
    dnsTakeover,
    dnsLoadError,
    dnsClockOffset,
    loadDns,
    enableDns,
    disableDns,
    saveDnsParams,
    DEFAULT_DNS_GROUP_NAME,
    dnsActiveKernel,
    dnsFailureText,
    dnsKernelTitle,
    dnsRequestErrorText,
    dnsWindowSeconds,
    type DnsBlock,
    type DnsParams,
    type DnsState,
    type DnsView
  } from '../../../lib/configDns';
  import { currentRevision, refetchLayerState } from '../../../lib/configLayer';
  import { routingView, loadRouting } from '../../../lib/configRouting';
  import type { CoreKernel } from '../../../lib/configCores';

  // Карточка «Перехват DNS роутера» (DNSF-02, D-02): одна на оба ядра, стоит перед карточкой
  // секции DNS выбранного ядра. Состояние, блокировки, апстримы и последняя проверка — из
  // общего стора dnsTakeover (вид считает бэкенд); переключение ядра карточку не
  // перезагружает. Ход транзакции (DnsTakeoverSteps) виден, пока идёт транзакция, и после
  // сбоя до следующего действия; проверка резолва (DnsResolveCheck) и параметры перехвата
  // (группа и локальные зоны DnsLocalZones) — под сеткой. Уведомления (стык со старым
  // перенаправлением DnsLegacyNote, исход без пользователя DnsOutcomeNote) идут под
  // блокировками, перед ходом транзакции.
  let {
    currentKernel,
    onopenform
  }: {
    /** Ядро выбранной вкладки. */
    currentKernel: CoreKernel;
    /** Открыть карточку секции DNS выбранного ядра (ссылка «К форме DNS»). */
    onopenform?: () => void;
  } = $props();

  const uid = $props.id();
  const titleId = `${uid}-title`;
  const groupSelectId = `${uid}-group`;
  const groupErrorId = `${uid}-group-error`;

  // Порядок блокировок (UI-SPEC «Порядок блокировок»): первая причина идёт первой
  const BLOCK_ORDER: readonly DnsBlock[] = [
    'conflict',
    'busy',
    'window',
    'draft',
    'stopped',
    'section',
    'group'
  ];
  // «Выключить перехват» блокируют только строки 1–3: выключение безопасно
  const DISABLE_BLOCKS: ReadonlySet<DnsBlock> = new Set(['conflict', 'busy', 'window']);
  const MAX_BLOCKS = 3;
  const MAX_UPSTREAMS = 4;
  // Отсчёт окна в строке состояния обновляется не чаще раза в 10 с (точный — в баннере)
  const WINDOW_TICK_MS = 10000;
  // Состояния, в которых перехват ведёт ядро или идёт к нему
  const ENGAGED: ReadonlySet<DnsState> = new Set(['applying', 'window', 'on', 'waiting']);

  const view = $derived($dnsTakeover);
  const dnsState = $derived<DnsState | null>(view?.state ?? null);

  let pending = $state(false);
  // Выключение идёт в фоне: итог (тост) показывается, когда состояние станет off или
  // придёт сбой
  let awaitingDisable = $state(false);

  const stateClass = $derived.by(() => {
    switch (dnsState) {
      case 'on':
        return 'badge-success';
      case 'window':
      case 'waiting':
        return 'badge-warning';
      case 'applying':
        return 'badge-primary';
      default:
        return '';
    }
  });

  // Остаток окна подтверждения для строки состояния
  const subscribeTick = createSubscriber((update) => {
    const id = setInterval(update, WINDOW_TICK_MS);
    return () => clearInterval(id);
  });
  function now(): number {
    subscribeTick();
    return Date.now();
  }
  const windowLeft = $derived(
    view?.window
      ? Math.max(0, Math.ceil((view.window.deadline_ms - (now() + $dnsClockOffset)) / 1000))
      : 0
  );

  function waitingReason(code: string | undefined): string {
    switch (code) {
      case 'kernel_stopped':
      case 'reboot':
      case 'watchdog':
      case 'probe':
        return $t(`cfg.dns.reason_${code}`);
      default:
        // Причина, которой нет в словаре, сырым кодом не показывается
        return $t('cfg.dns.no_reason');
    }
  }

  const statusText = $derived.by(() => {
    if (!view) return '';
    switch (view.state) {
      case 'applying':
        // Набор шагов приходит с сервера: шаг возврата DNS прошивке есть только у выключения
        return $t(
          view.steps.some((s) => s.id === 'release')
            ? 'cfg.dns.status_disabling'
            : 'cfg.dns.status_enabling'
        );
      case 'window':
        return $t('cfg.dns.status_window', { n: windowLeft });
      case 'on':
        return $t('cfg.dns.status_kernel', { kernel: dnsKernelTitle(view.kernel) });
      case 'waiting':
        return $t('cfg.dns.status_waiting', { reason: waitingReason(view.waiting_reason) });
      default:
        return $t('cfg.dns.status_firmware');
    }
  });

  // Блокировки: в «выключен» — все (они держат «Включить перехват»), в остальных — только
  // те, что держат «Выключить перехват»; не больше трёх строк
  const blocks = $derived.by<DnsBlock[]>(() => {
    if (!view || view.state === 'unavailable') return [];
    const all = BLOCK_ORDER.filter((b) => view.blocks.includes(b));
    const shown = view.state === 'off' ? all : all.filter((b) => DISABLE_BLOCKS.has(b));
    return shown.slice(0, MAX_BLOCKS);
  });
  const blockId = (b: DnsBlock): string => `${uid}-block-${b}`;
  const describedBy = $derived(blocks.length > 0 ? blocks.map(blockId).join(' ') : undefined);

  function sectionOwnerText(owner: string | undefined): string {
    switch (owner) {
      case 'xkeen':
        return $t('cfg.sec.owner_xkeen');
      case 'user':
        return $t('cfg.sec.owner_user');
      case 'none':
        return $t('cfg.sec.owner_none');
      default:
        return $t('cfg.dns.owner_unknown');
    }
  }

  function blockText(b: DnsBlock, v: DnsView): string {
    switch (b) {
      case 'conflict':
        return $t('kernel.conflict_blocked');
      case 'busy':
        return $t('cfg.routing.apply_busy_hint');
      case 'section':
        return $t('cfg.dns.block_section', { owner: sectionOwnerText(v.section_owner) });
      default:
        return $t(`cfg.dns.block_${b}`);
    }
  }

  const enableBlocked = $derived(pending || (view?.blocks.length ?? 1) > 0);
  const disableBlocked = $derived(
    pending || awaitingDisable || (view?.blocks.some((b) => DISABLE_BLOCKS.has(b)) ?? true)
  );

  const showKernelBadge = $derived(
    view !== null && dnsState !== null && view.kernel !== '' && ENGAGED.has(dnsState)
  );
  const otherKernel = $derived(
    view !== null &&
      dnsState !== null &&
      ENGAGED.has(dnsState) &&
      view.kernel !== '' &&
      view.kernel !== currentKernel
  );

  // Владелец порта: адрес — mono внутри фразы
  const ADDR_TOKEN = '\u0000addr\u0000';
  const ownerParts = $derived.by<{ text: string; addr: string } | null>(() => {
    if (!view) return null;
    const o = view.owner;
    switch (o.kind) {
      case 'kernel':
        if (!o.addr) {
          return {
            text: $t('cfg.dns.owner_kernel_noaddr', { kernel: dnsKernelTitle(o.kernel) }),
            addr: ''
          };
        }
        return {
          text: $t('cfg.dns.owner_kernel', { kernel: dnsKernelTitle(o.kernel), addr: ADDR_TOKEN }),
          addr: o.addr
        };
      case 'firmware':
        return { text: $t('cfg.dns.owner_firmware'), addr: '' };
      case 'other':
        return {
          text: o.process
            ? $t('cfg.dns.owner_other', { process: o.process })
            : $t('cfg.dns.owner_other_noname'),
          addr: ''
        };
      default:
        return { text: $t('cfg.dns.owner_none'), addr: '' };
    }
  });
  const ownerSplit = $derived(ownerParts ? ownerParts.text.split(ADDR_TOKEN) : []);

  const kernelText = $derived.by(() => {
    if (!view) return '';
    const k = dnsActiveKernel(view);
    if (k === 'none' || k === '') return $t('cfg.dns.kernel_none');
    // Оба ядра запущены (конфликт ядер)
    if (k === 'both') return $t('cfg.dns.kernel_both_running');
    return $t(view.kernel_running ? 'cfg.dns.kernel_running' : 'cfg.dns.kernel_stopped', {
      kernel: dnsKernelTitle(k)
    });
  });

  const upstreamsShown = $derived(view ? view.upstreams.slice(0, MAX_UPSTREAMS) : []);
  const upstreamsMore = $derived(view ? Math.max(0, view.upstreams.length - MAX_UPSTREAMS) : 0);

  // Имена узлов показываются, когда перехват включён или готов к включению
  const showNodes = $derived(
    view !== null &&
      (view.state === 'on' ||
        view.state === 'window' ||
        (view.state === 'off' && view.blocks.length === 0))
  );

  // Ход транзакции. Набор шагов приходит с сервера: шаг возврата DNS прошивке есть только у
  // выключения
  const stepsKind = $derived<'enable' | 'disable'>(
    view?.steps.some((s) => s.id === 'release') ? 'disable' : 'enable'
  );
  const failureKey = $derived(view?.failure ? JSON.stringify(view.failure) : '');
  // Сбой виден после транзакции, пока пользователь не нажал следующее действие карточки
  const stepsFailure = $derived(
    view?.failure && dnsState !== 'applying' && failureSeen.key !== failureKey
      ? view.failure
      : undefined
  );
  const showSteps = $derived(dnsState === 'applying' || stepsFailure !== undefined);

  // Исход без пользователя виден до следующего действия пользователя с карточкой
  const shownOutcome = $derived(
    view?.outcome && outcomeSeen.at !== view.outcome.at_ms ? view.outcome : undefined
  );

  let checkRef = $state<ReturnType<typeof DnsResolveCheck>>();

  // Параметры перехвата: группа для DNS-запросов и локальные зоны из черновика или
  // применённого состояния
  const draftParams = $derived<DnsParams>(
    view?.draft_params ?? { group: '', zones: view?.zones ?? [] }
  );
  const draftZones = $derived(draftParams.zones ?? []);
  const rawDraftGroup = $derived(draftParams.group ?? '');

  // Список групп загружается при первом раскрытии параметров, если вкладка «Группы» ещё
  // не открывалась
  $effect(() => {
    if (paramsOpen.open && $routingView === null) {
      void loadRouting();
    }
  });

  const availableGroups = $derived.by<Array<{ id: string; name: string }>>(() => {
    const list = $routingView?.groups ?? [];
    if (list.length > 0) return list.map((g) => ({ id: g.id, name: g.name }));
    if ($routingView === null && view?.group && !view.group.missing && view.group.id) {
      return [{ id: view.group.id, name: view.group.name || view.group.id }];
    }
    return [];
  });

  // Группа-цель черновика: пустой id — группа по умолчанию (D-09, DEFAULT_DNS_GROUP_NAME).
  // Группа не найдена среди имеющихся — пустое значение и строка ошибки; групп нет вовсе
  // (маршрутизацию ведёт не панель) — поле неактивно без ошибки, причину называет строка
  // блокировки 7
  const DEFAULT_GROUP_NAME = DEFAULT_DNS_GROUP_NAME;
  const groupTarget = $derived.by<{ id: string; missing: string }>(() => {
    if (availableGroups.length === 0) return { id: '', missing: '' };
    // Пока список групп не загружен, «удалена» не показывается: в списке только группа
    // применённого состояния
    const loaded = $routingView !== null;
    if (rawDraftGroup !== '') {
      if (availableGroups.some((g) => g.id === rawDraftGroup)) {
        return { id: rawDraftGroup, missing: '' };
      }
      const name = view?.group?.id === rawDraftGroup && view.group.name ? view.group.name : '';
      return { id: '', missing: loaded ? name || rawDraftGroup : '' };
    }
    const def = availableGroups.find((g) => g.name === DEFAULT_GROUP_NAME);
    if (def) return { id: def.id, missing: '' };
    if (!loaded) return { id: availableGroups[0].id, missing: '' };
    return { id: '', missing: DEFAULT_GROUP_NAME };
  });
  const selectedGroupId = $derived(groupTarget.id);
  const groupMissing = $derived(groupTarget.missing !== '');
  const noGroups = $derived($routingView !== null && availableGroups.length === 0);

  const paramsLocked = $derived(
    dnsState === 'applying' || (view?.blocks.some((b) => DISABLE_BLOCKS.has(b)) ?? false)
  );
  const showEditWindowHint = $derived(
    Boolean(
      view?.params_changed &&
      (view.state === 'on' || view.state === 'window' || view.state === 'waiting')
    )
  );

  async function saveParams(next: DnsParams): Promise<void> {
    try {
      try {
        await saveDnsParams(currentRevision(), next);
      } catch (err) {
        if ((err as { code?: string })?.code === 'draft_conflict') {
          await refetchLayerState();
          await saveDnsParams(currentRevision(), next);
        } else {
          throw err;
        }
      }
      await loadDns();
    } catch (err) {
      showToast('error', dnsRequestErrorText(err), 8000);
      void loadDns();
    }
  }

  // Правки идут по очереди: вторая правка, сделанная до ответа на первую, строится от
  // значения первой, а не от устаревшего вида (иначе выбор группы затёр бы новые зоны)
  let queuedParams: DnsParams | null = null;
  let saveChain: Promise<void> = Promise.resolve();

  function enqueueParams(patch: Partial<DnsParams>): void {
    if (paramsLocked) return;
    const base = queuedParams ?? { group: rawDraftGroup, zones: draftZones };
    const next: DnsParams = { ...base, ...patch };
    queuedParams = next;
    saveChain = saveChain
      .then(() => saveParams(next))
      .then(() => {
        if (queuedParams === next) queuedParams = null;
      });
  }

  function updateGroup(nextId: string): void {
    if (nextId === '') return;
    enqueueParams({ group: nextId });
  }

  function updateZones(nextZones: string[]): void {
    enqueueParams({ zones: nextZones });
  }

  function markFailureSeen(): void {
    failureSeen.key = failureKey;
    if (view?.outcome) outcomeSeen.at = view.outcome.at_ms;
  }

  async function enable(): Promise<void> {
    if (enableBlocked) return;
    markFailureSeen();
    pending = true;
    try {
      await enableDns();
    } catch (err) {
      showToast('error', dnsRequestErrorText(err), 8000);
      void loadDns();
    } finally {
      pending = false;
    }
  }

  async function disable(): Promise<void> {
    if (disableBlocked || !view) return;
    const waiting = view.state === 'waiting';
    const ok = await showConfirm({
      title: $t(waiting ? 'cfg.dns.cancel_wait_title' : 'cfg.dns.disable_title'),
      message: $t(waiting ? 'cfg.dns.cancel_wait_body' : 'cfg.dns.disable_body'),
      confirmLabel: $t('cfg.dns.disable_btn'),
      cancelLabel: $t('app.cancel'),
      variant: 'warning'
    });
    if (!ok) return;
    markFailureSeen();
    pending = true;
    try {
      await disableDns();
      watchDisable();
    } catch (err) {
      showToast('error', dnsRequestErrorText(err), 8000);
      void loadDns();
    } finally {
      pending = false;
    }
  }

  // Итог выключения: состояние off — тост успеха, сбой — текст по коду. Вид, бывший в сторе
  // до запроса, итогом не считается
  let offDisable: (() => void) | null = null;

  function watchDisable(): void {
    offDisable?.();
    awaitingDisable = true;
    let first = true;
    const finish = (): void => {
      awaitingDisable = false;
      queueMicrotask(() => {
        offDisable?.();
        offDisable = null;
      });
    };
    offDisable = dnsTakeover.subscribe((v) => {
      if (first) {
        first = false;
        return;
      }
      if (!awaitingDisable || !v) return;
      if (v.failure) {
        finish();
        showToast('error', dnsFailureText(v.failure), 8000);
      } else if (v.state === 'off') {
        finish();
        showToast('success', $t('cfg.dns.toast_disabled'));
      }
    });
  }

  onDestroy(() => offDisable?.());
</script>

<section class="card dns-card" aria-labelledby={titleId} data-testid="dns-takeover-card">
  <div class="dns-head">
    <div class="dns-head-text">
      <h3 class="dns-title" id={titleId}>{$t('cfg.dns.title')}</h3>
      <p class="dns-caption">{$t('cfg.dns.subtitle')}</p>
    </div>
    {#if view && dnsState}
      <div class="dns-badges">
        <span class="badge {stateClass}" data-testid="dns-state-badge">
          {$t(`cfg.dns.state.${dnsState}`)}
        </span>
        {#if showKernelBadge}
          <span class="badge badge-type" data-testid="dns-kernel-badge">
            {dnsKernelTitle(view.kernel)}
          </span>
        {/if}
      </div>
    {/if}
  </div>

  {#if view === null && $dnsLoadError}
    <div class="alert alert-error dns-alert" role="alert" data-testid="dns-load-error">
      <Icon name="warning" size={16} class="icon-first-line" />
      <div class="dns-alert-body">
        <span>{$t('cfg.dns.load_error')}</span>
        <Button variant="secondary" data-testid="dns-load-retry" onclick={() => void loadDns()}>
          {$t('cfg.load_error_retry')}
        </Button>
      </div>
    </div>
  {:else if view === null}
    <div class="dns-skeleton" aria-hidden="true">
      <Skeleton type="rect" height="56px" />
      <Skeleton type="rect" height="56px" />
    </div>
  {:else}
    {#if otherKernel}
      <p class="dns-caption" role="status" data-testid="dns-other-kernel">
        {$t('cfg.dns.other_kernel', {
          kernel: dnsKernelTitle(view.kernel),
          current: dnsKernelTitle(currentKernel)
        })}
      </p>
    {/if}

    <p
      class="dns-status"
      role="status"
      aria-live="polite"
      data-testid="dns-state"
      data-state={view.state}
    >
      {statusText}
    </p>

    {#if blocks.length > 0}
      <ul class="dns-blocks">
        {#each blocks as b (b)}
          <li class="warning-text dns-block" id={blockId(b)} data-testid="dns-block-{b}">
            <Icon name="warning" size={14} class="icon-first-line" />
            <span class="dns-block-text">
              {blockText(b, view)}
              {#if b === 'section' && onopenform}
                <button
                  type="button"
                  class="dns-link"
                  data-testid="dns-open-form"
                  onclick={() => onopenform?.()}
                >
                  {$t('cfg.dns.open_dns_form')}
                </button>
              {:else if b === 'group'}
                <a class="dns-link" href="#/config/groups" data-testid="dns-open-groups">
                  {$t('cfg.dns.open_groups')}
                </a>
              {/if}
            </span>
          </li>
        {/each}
      </ul>
    {/if}

    {#if view.state !== 'unavailable' && view.state !== 'applying'}
      <DnsLegacyNote {view} onaction={markFailureSeen} />
      <DnsOutcomeNote outcome={shownOutcome} state={view.state} kernel={view.kernel} />
    {/if}

    {#if showSteps && view.state !== 'unavailable'}
      <DnsTakeoverSteps
        steps={view.steps}
        failure={stepsFailure}
        kind={stepsKind}
        oncheck={() => checkRef?.focus()}
      />
    {/if}

    {#if view.state === 'unavailable'}
      <div class="dns-unavailable" data-testid="dns-unavailable">
        <Icon name="info" size={16} class="icon-first-line" />
        <div class="dns-unavailable-text">
          <p class="dns-body">{$t('cfg.dns.unavailable_body')}</p>
          {#if view.unavailable_reason}
            <p class="dns-caption dns-mono" data-testid="dns-unavailable-reason">
              {view.unavailable_reason}
            </p>
          {/if}
        </div>
      </div>
    {:else}
      <dl class="dns-grid">
        <div class="dns-row" data-testid="dns-row-owner">
          <dt class="form-label">{$t('cfg.dns.row_owner')}</dt>
          <dd>
            <span
              >{ownerSplit[0]}{#if ownerSplit.length > 1}<span class="dns-mono"
                  >{ownerParts?.addr}</span
                >{ownerSplit[1]}{/if}</span
            >
          </dd>
        </div>
        <div class="dns-row" data-testid="dns-row-kernel">
          <dt class="form-label">{$t('cfg.dns.row_kernel')}</dt>
          <dd>{kernelText}</dd>
        </div>
        {#if view.group}
          <div class="dns-row" data-testid="dns-row-group">
            <dt class="form-label">{$t('cfg.dns.row_group')}</dt>
            <dd>
              {#if view.group.missing}
                <span class="warning-text dns-inline-warning">
                  {$t('cfg.dns.group_missing', { name: view.group.name || view.group.id })}
                </span>
              {:else}
                <span class="dns-mono">{view.group.name}</span>
              {/if}
              <a class="dns-link" href="#/config/groups">{$t('cfg.dns.open_groups')}</a>
            </dd>
          </div>
        {/if}
        {#if view.upstreams.length > 0}
          <div class="dns-row" data-testid="dns-row-upstreams">
            <dt class="form-label">{$t('cfg.dns.row_upstreams')}</dt>
            <dd>
              <ul class="dns-list">
                {#each upstreamsShown as u, i (i)}
                  <li class="dns-mono">{u}</li>
                {/each}
              </ul>
              {#if upstreamsMore > 0}
                <p class="dns-caption">{$t('cfg.dns.and_more', { n: upstreamsMore })}</p>
              {/if}
              {#if view.group && !view.group.missing && (view.group.name || view.group.id)}
                <p class="dns-caption">
                  {$t('cfg.dns.upstreams_via', { group: view.group.name || view.group.id })}
                </p>
              {/if}
            </dd>
          </div>
        {/if}
        {#if showNodes}
          <div class="dns-row" data-testid="dns-row-nodes">
            <dt class="form-label">{$t('cfg.dns.row_nodes')}</dt>
            <dd>{$t('cfg.dns.nodes_value')}</dd>
          </div>
        {/if}
        <div class="dns-row" data-testid="dns-row-ipv6">
          <dt class="form-label">{$t('cfg.sec.f.ipv6')}</dt>
          <dd>{$t(view.ipv6 ? 'cfg.dns.ipv6_on' : 'cfg.dns.ipv6_off')}</dd>
        </div>
        <!-- Итог последней проверки показывает блок проверки резолва сразу под сеткой; строка
             сетки остаётся только для пустого состояния, чтобы итог и время не шли дважды -->
        {#if !view.last_check}
          <div class="dns-row" data-testid="dns-row-last-check">
            <dt class="form-label">{$t('cfg.dns.row_last_check')}</dt>
            <dd>{$t('cfg.dns.check_none')}</dd>
          </div>
        {/if}
      </dl>

      <DnsResolveCheck bind:this={checkRef} {view} />

      <details
        class="dns-params"
        data-testid="dns-params"
        open={paramsOpen.open}
        ontoggle={(e) => {
          paramsOpen.open = e.currentTarget.open;
        }}
      >
        <summary class="dns-params-summary" data-testid="dns-params-summary">
          <span>{$t('cfg.dns.params')}</span>
          {#if view.params_changed}
            <span class="badge badge-warning" data-testid="dns-params-changed">
              {$t('cfg.dns.changed_badge')}
            </span>
          {/if}
        </summary>

        <div class="dns-params-body">
          {#if showEditWindowHint}
            <p class="warning-text dns-params-warn" data-testid="dns-params-window-hint">
              <Icon name="warning" size={14} class="icon-first-line" />
              <span>{$t('cfg.dns.edit_window_hint', { seconds: String($dnsWindowSeconds) })}</span>
            </p>
          {/if}

          <div class="dns-field">
            <label class="form-label" for={groupSelectId}>{$t('cfg.dns.group_label')}</label>
            <Select
              id={groupSelectId}
              class={groupMissing ? 'input input-error' : 'input'}
              value={selectedGroupId}
              disabled={paramsLocked || availableGroups.length === 0}
              data-testid="dns-group-select"
              onchange={(e) => updateGroup(e.currentTarget.value)}
            >
              {#if selectedGroupId === ''}
                <option value="" disabled></option>
              {/if}
              {#each availableGroups as g (g.id)}
                <option value={g.id}>{g.name}</option>
              {/each}
            </Select>
            {#if groupMissing}
              <p
                class="error-text dns-field-error"
                id={groupErrorId}
                role="alert"
                data-testid="dns-group-missing"
              >
                {$t('cfg.dns.group_missing', { name: groupTarget.missing })}
              </p>
            {:else if noGroups}
              <p class="warning-text dns-field-error" data-testid="dns-group-none">
                {$t('cfg.dns.block_group')}
              </p>
            {/if}
            <p class="dns-caption">
              {$t('cfg.dns.group_hint')}
              <a class="dns-link" href="#/config/groups">{$t('cfg.dns.open_groups')}</a>
            </p>
          </div>

          <DnsLocalZones
            builtin={view.zones_builtin}
            zones={draftZones}
            readonly={paramsLocked}
            onchange={(zones) => updateZones(zones)}
          />
        </div>
      </details>

      <div class="dns-foot">
        {#if view.state === 'off'}
          <div class="dns-actions">
            <Button
              variant="primary"
              disabled={enableBlocked}
              loading={pending}
              ariaDescribedby={describedBy}
              data-testid="dns-enable"
              onclick={() => void enable()}
            >
              {$t('cfg.dns.enable_btn')}
            </Button>
          </div>
        {:else if view.state === 'on' || view.state === 'waiting'}
          <div class="dns-actions">
            <Button
              variant="secondary"
              disabled={disableBlocked}
              loading={pending}
              ariaDescribedby={describedBy}
              data-testid="dns-disable"
              onclick={() => void disable()}
            >
              {$t('cfg.dns.disable_btn')}
            </Button>
          </div>
        {/if}
        <p class="dns-note">
          <Icon name="info" size={14} class="icon-first-line" />
          <span>{$t('cfg.dns.firmware_note')}</span>
        </p>
        <p class="dns-note">
          <Icon name="info" size={14} class="icon-first-line" />
          <span>{$t('cfg.dns.scope_note')}</span>
        </p>
      </div>
    {/if}
  {/if}
</section>

<style>
  .dns-card {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-4);
    min-width: 0;
    overflow-wrap: anywhere;
  }

  .dns-head {
    display: flex;
    flex-wrap: wrap;
    align-items: flex-start;
    justify-content: space-between;
    gap: var(--spacing-2) var(--spacing-4);
    min-width: 0;
  }

  .dns-head-text {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-1);
    flex: 1 1 240px;
    min-width: 0;
  }

  .dns-title {
    margin: 0;
    font-size: var(--font-size-lg);
    font-weight: 600;
    line-height: 1.2;
    color: var(--fg-primary);
  }

  .dns-badges {
    display: inline-flex;
    flex-wrap: wrap;
    gap: var(--spacing-1);
  }

  .dns-caption {
    margin: 0;
    font-size: var(--font-size-xs);
    line-height: 1.5;
    color: var(--fg-secondary);
    overflow-wrap: anywhere;
  }

  .dns-status,
  .dns-body {
    margin: 0;
    font-size: var(--font-size-base);
    line-height: 1.5;
    color: var(--fg-primary);
    overflow-wrap: anywhere;
  }

  .dns-mono {
    font-family: var(--font-family-mono);
    overflow-wrap: anywhere;
  }

  .dns-skeleton {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-2);
  }

  /* Текст в .alert — Body 14/400 (у .alert проекта 13px) */
  .dns-alert {
    margin: 0;
    min-width: 0;
    gap: var(--spacing-2);
    padding: var(--spacing-2) var(--spacing-4);
    font-size: var(--font-size-base);
    line-height: 1.5;
  }

  .dns-alert-body {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--spacing-2);
    min-width: 0;
  }

  .dns-blocks {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-1);
    margin: 0;
    padding: 0;
    list-style: none;
  }

  .dns-block {
    display: flex;
    align-items: flex-start;
    gap: var(--spacing-1);
    margin: 0;
    line-height: 1.5;
  }

  .dns-block-text {
    min-width: 0;
    overflow-wrap: anywhere;
  }

  .dns-link {
    display: inline;
    margin: 0 0 0 var(--spacing-1);
    padding: 0;
    border: 0;
    background: none;
    font: inherit;
    color: var(--accent-text);
    text-decoration: underline;
    cursor: pointer;
  }

  .dns-unavailable {
    display: flex;
    align-items: flex-start;
    gap: var(--spacing-2);
    padding: var(--spacing-8) 0;
    color: var(--fg-secondary);
    min-width: 0;
  }

  .dns-unavailable-text {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-2);
    min-width: 0;
  }

  /* Между заголовочной частью и сеткой — lg 24px: зазор карточки 16 + 8 */
  .dns-grid {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-4);
    margin: var(--spacing-2) 0 0;
    min-width: 0;
  }

  .dns-row {
    display: grid;
    grid-template-columns: minmax(0, 1.1fr) minmax(0, 2fr);
    gap: var(--spacing-2);
    min-width: 0;
  }

  .dns-row dt {
    margin: 0;
    min-width: 0;
  }

  .dns-row dd {
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    gap: var(--spacing-1);
    margin: 0;
    min-width: 0;
    font-size: var(--font-size-base);
    line-height: 1.5;
    color: var(--fg-primary);
    overflow-wrap: anywhere;
  }

  .dns-row dd .dns-link {
    margin: 0;
  }

  .dns-inline-warning {
    margin: 0;
  }

  .dns-list {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-1);
    margin: 0;
    padding: 0;
    list-style: none;
    min-width: 0;
  }

  .dns-params {
    min-width: 0;
  }

  .dns-params-summary {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--spacing-2);
    min-height: 32px;
    font-size: var(--font-size-sm);
    font-weight: 600;
    color: var(--fg-secondary);
    cursor: pointer;
  }

  .dns-params-body {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-4);
    padding-top: var(--spacing-2);
    min-width: 0;
  }

  .dns-params-warn {
    display: flex;
    align-items: flex-start;
    gap: var(--spacing-1);
    margin: 0;
    line-height: 1.5;
    min-width: 0;
    overflow-wrap: anywhere;
  }

  .dns-field {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-2);
    min-width: 0;
  }

  .dns-field-error {
    margin: 0;
    min-width: 0;
    overflow-wrap: anywhere;
  }

  .dns-foot {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-2);
    padding-top: var(--spacing-2);
    min-width: 0;
  }

  .dns-actions {
    display: flex;
    flex-wrap: wrap;
    gap: var(--spacing-2);
  }

  .dns-note {
    display: flex;
    align-items: flex-start;
    gap: var(--spacing-1);
    margin: 0;
    font-size: var(--font-size-xs);
    line-height: 1.5;
    color: var(--fg-secondary);
    min-width: 0;
    overflow-wrap: anywhere;
  }

  @media (max-width: 768px) {
    .dns-row {
      grid-template-columns: minmax(0, 1fr);
      gap: var(--spacing-1);
    }
  }

  @media (pointer: coarse), (max-width: 768px) {
    .dns-actions :global(.btn) {
      flex: 1 1 auto;
      min-height: 44px;
    }

    .dns-alert-body :global(.btn) {
      min-height: 44px;
    }

    .dns-params-summary,
    .dns-params :global(.input) {
      min-height: 44px;
    }

    .dns-link {
      display: inline-flex;
      align-items: center;
      min-height: 44px;
    }
  }

  @media (max-width: 480px) {
    .dns-actions :global(.btn) {
      flex: 1 1 100%;
    }
  }
</style>
