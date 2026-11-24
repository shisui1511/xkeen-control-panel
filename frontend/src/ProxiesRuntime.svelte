<script lang="ts">
  // Страница «Прокси»: что выбрано сейчас в группах панели для обоих ядер. Для своего
  // профиля Mihomo (user_profile) внутри лениво грузится Proxies.svelte — страница
  // Clash API запущенного профиля.
  import { onMount, tick, untrack } from 'svelte';
  import { writable } from 'svelte/store';
  import { SvelteMap, SvelteSet } from 'svelte/reactivity';
  import { t } from './i18n';
  import { activeKernelName, capabilities, runningKernels, showToast } from './stores';
  import { usePoller } from './lib/poller';
  import { motion } from './lib/motion.svelte';
  import PageHeader from './PageHeader.svelte';
  import Skeleton from './components/Skeleton.svelte';
  import EmptyState from './components/EmptyState.svelte';
  import LatencyHistoryPopover from './components/LatencyHistoryPopover.svelte';
  import ClientExitIpBadge from './components/network/ClientExitIpBadge.svelte';
  import ProxyGroupCard from './components/proxies/ProxyGroupCard.svelte';
  import ProxyFilterBar from './components/proxies/ProxyFilterBar.svelte';
  import ObservatoryPanel from './components/proxies/ObservatoryPanel.svelte';
  import MihomoModeSwitch from './components/proxies/MihomoModeSwitch.svelte';
  import ProxyRuntimeNotice from './components/proxies/ProxyRuntimeNotice.svelte';
  import ProxyPingBar from './components/proxies/ProxyPingBar.svelte';
  import SourceDiagnosticsModal from './components/config/nodes/SourceDiagnosticsModal.svelte';
  import {
    runtimeView,
    runtimeLoadError,
    pingSnapshot,
    runtimeNow,
    runtimeRestarting,
    loadRuntime,
    loadResults,
    loadNow,
    resetNow,
    selectNode,
    startPing,
    cancelPing,
    confirmGroup,
    attachProxiesLive,
    proxiesErrorText,
    type PingBatch,
    type ProxyKernel,
    type RuntimeGroup,
    type RuntimeMember,
    type RuntimeView
  } from './lib/configProxies';
  import {
    kernelLabel,
    splitMembers,
    resultView,
    historyView,
    proxyLike,
    pingHistory,
    holdWindowMember,
    quotaHoldParams,
    type ActiveKernel,
    type PanelCard
  } from './lib/proxyRuntimeAdapter';
  import {
    computeObservatoryStats,
    groupMatchesLatencyFilter,
    type NodeSnapshot,
    type ObservatoryFilter,
    type ProxyLike
  } from './lib/proxyStats';
  import {
    layerSnapshot,
    visibleNotices,
    selectBgReason,
    type LayerNotice
  } from './lib/configLayer';
  import { loadLimits } from './lib/trafficLimits';
  import { formatRouterTime } from './lib/scheduleWindows';
  import type { ProxiesViewMode } from './lib/proxyViewPrefs';
  import type { SelectResult } from './lib/configRouting';
  import type { KernelName } from './lib/configSources';

  // Перечитывание вида после смены ядра: события процесса приходят пачкой
  const KERNEL_DEBOUNCE_MS = 300;
  // Опрос «Сейчас» авто- и резервных групп (D-14)
  const NOW_POLL_MS = 12000;
  const RENDER_STEP = 60;

  const view = $derived($runtimeView);
  const loadError = $derived($runtimeLoadError);
  const results = $derived($pingSnapshot?.results ?? {});
  const batch: PingBatch | undefined = $derived($pingSnapshot?.batch);
  const batchRunning = $derived(batch?.state === 'running');

  // Активное ядро карточек: при остановленных ядрах — '' (видны узлы обоих ядер)
  const kernel: ActiveKernel = $derived(
    $activeKernelName === 'xray' || $activeKernelName === 'mihomo' ? $activeKernelName : ''
  );

  // --- Свёрнутость, фильтры и порог плиток карточек (ключ — ID группы) ---
  const expanded = new SvelteSet<string>();
  const groupFilters = new SvelteMap<string, string>();
  const renderLimits = new SvelteMap<string, number>();
  const sortedGroups = new SvelteSet<string>();
  let cardEls = $state<Record<string, HTMLElement | null>>({});

  function toggleCollapse(id: string) {
    if (expanded.has(id)) expanded.delete(id);
    else expanded.add(id);
  }

  // --- Поиск и фильтр сводки (D-04) ---
  let filterQuery = $state('');
  // Карточка панели всегда сеткой: режим вида влияет только на прежнюю страницу
  let viewMode = $state<ProxiesViewMode>('grid');
  let obsFilter = $state<ObservatoryFilter>(null);
  let refreshing = $state(false);

  /** Замер узла: пинг панели, для авто и резерва без него — история ядра Mihomo (D-05). */
  function measure(g: RuntimeGroup, m: RuntimeMember) {
    const v = resultView(results, m.ref, kernel);
    if (v.state !== 'none' || g.mode === 'manual' || !m.ref) return v;
    return historyView($runtimeNow?.history[m.ref]);
  }

  // Узлы, которые умеет активное ядро, по ссылкам: сводка и фильтр групп
  const snapshots = $derived.by(() => {
    const out = new Map<string, NodeSnapshot & ProxyLike>();
    const system = new Map<string, ProxyLike>();
    for (const g of view?.groups ?? []) {
      for (const m of splitMembers(g, kernel).visible) {
        if (m.kind === 'direct' || m.kind === 'reject') {
          system.set(m.kind, { name: m.kind.toUpperCase(), type: m.kind });
          continue;
        }
        if (m.kind !== 'node' || !m.ref) continue;
        const prev = out.get(m.ref);
        const v = measure(g, m);
        if (prev && v.state === 'none') continue;
        out.set(m.ref, { ...proxyLike(m.ref, v), type: m.proto });
      }
    }
    return { nodes: out, system };
  });

  const observatoryStats = $derived(
    computeObservatoryStats(
      Object.fromEntries([...snapshots.nodes, ...snapshots.system].map(([k, v]) => [k, v]))
    )
  );

  function nodeRefs(g: RuntimeGroup): string[] {
    return splitMembers(g, kernel)
      .visible.filter((m) => m.kind === 'node' && !!m.ref)
      .map((m) => m.ref as string);
  }

  const shownGroups = $derived.by(() => {
    if (!view) return [];
    const q = filterQuery.trim().toLowerCase();
    const filter = obsFilter;
    return view.groups.filter((g) => {
      if (
        filter !== null &&
        !groupMatchesLatencyFilter(nodeRefs(g), filter, (ref) => snapshots.nodes.get(ref))
      ) {
        return false;
      }
      if (q === '') return true;
      return (
        g.name.toLowerCase().includes(q) || g.members.some((m) => m.name.toLowerCase().includes(q))
      );
    });
  });

  // --- Выбор (D-11, D-15…D-18) ---

  /**
   * Идущие выборы: ID группы → участник. Повторные нажатия в той же группе и в связанных
   * с ней игнорируются, независимые группы выбираются параллельно (D-17).
   */
  const selecting = new SvelteMap<string, string>();

  /**
   * Группа заблокирована идущим на сервере выбором (в том числе из другой вкладки) или
   * выбором этой вкладки в связанной группе (D-17).
   */
  function lockedBy(g: RuntimeGroup, v: RuntimeView): boolean {
    if (g.switching) return true;
    for (const sid of selecting.keys()) {
      if (sid === g.id) continue;
      if (g.locked_with.includes(sid)) return true;
      const sg = v.groups.find((x) => x.id === sid);
      if (sg && sg.locked_with.includes(g.id)) return true;
    }
    return false;
  }

  /** Ядро, по исходу которого судим о выборе: активное, при остановленных — первое управляемое. */
  function outcomeKernel(v: RuntimeView, res: SelectResult): string {
    if (kernel !== '') return kernel;
    const managed = (['xray', 'mihomo'] as ProxyKernel[]).find((k) => v.modes[k] === 'panel');
    if (managed) return managed;
    return Object.keys(res.kernels ?? {})[0] ?? '';
  }

  function memberName(g: RuntimeGroup, member: string): string {
    return g.members.find((m) => m.member === member)?.name || member;
  }

  function codeOf(e: unknown): string | undefined {
    const code = (e as { code?: unknown } | null)?.code;
    return typeof code === 'string' && code !== '' ? code : undefined;
  }

  /** Уведомление по исходу выбора: «возвращён» — только когда сервер вернул прежний выбор. */
  function notifySelect(v: RuntimeView, res: SelectResult, name: string) {
    if (res.reverted) {
      showToast('error', $t('proxies.rt.toast.reverted', { name }));
      return;
    }
    // Ядро не подтвердило выбор (D-16): плашка «Повторить» в карточке после перечитывания
    if ((res.unconfirmed ?? []).length > 0) return;
    const outcomes = res.kernels ?? {};
    const failed = Object.entries(outcomes)
      .filter(([, outcome]) => outcome === 'failed')
      .map(([k]) => kernelLabel(k) || k);
    if (failed.length > 0) {
      showToast(
        'warning',
        $t(failed.length > 1 ? 'cfg.groups.select_partial_both' : 'cfg.groups.select_partial_one', {
          name,
          kernels: failed.join(', ')
        })
      );
      return;
    }
    const outcome = outcomes[outcomeKernel(v, res)];
    if (outcome === 'applied') {
      showToast('success', $t('cfg.groups.toast_selected', { name }));
    } else if (outcome === 'restarted') {
      // Исчезает сам (D-18): тост успеха с обычным сроком
      const reason = res.restart_reason;
      showToast(
        'success',
        reason
          ? $t('proxies.rt.toast.restarted', {
              reason: $t(`proxies.rt.restart_reason.${reason}`)
            })
          : $t('proxies.rt.toast.restarted_plain'),
        4000
      );
    } else if (outcome === 'pending_stopped') {
      showToast('success', $t('proxies.rt.toast.pending', { name }));
    } else {
      showToast('error', $t('cfg.groups.select_failed', { name }));
    }
  }

  /** Текст удержания лимитом: тот же, что в подсказке плиток (D-05). */
  function quotaText(g: RuntimeGroup): string {
    return $t('proxies.rt.quota.tile_title', g.hold ? quotaHoldParams(g.hold, view?.clock) : {});
  }

  /** Выбор прошёл на ядре без отказов и возврата (исход для тоста перехвата окна). */
  function selectSucceeded(v: RuntimeView, res: SelectResult): boolean {
    if (res.reverted || (res.unconfirmed ?? []).length > 0) return false;
    const outcomes = res.kernels ?? {};
    if (Object.values(outcomes).some((o) => o === 'failed')) return false;
    const outcome = outcomes[outcomeKernel(v, res)];
    return outcome === 'applied' || outcome === 'restarted' || outcome === 'pending_stopped';
  }

  async function onSelect(g: RuntimeGroup, member: string) {
    const v = view;
    // Квота держит группу (D-05): запрос не отправляется, причина — тостом; сервер всё
    // равно отклонил бы выбор (quota_held, T-152-33)
    if (g.hold?.kind === 'quota') {
      showToast('warning', quotaText(g));
      return;
    }
    if (!v || selecting.has(g.id) || lockedBy(g, v)) return;
    const name = memberName(g, member);
    // Группу держит окно, выбран не его узел (D-04): узел ставится сразу, перехват — до конца окна
    const hold = g.hold;
    const overrides =
      (hold?.kind === 'schedule' || hold?.kind === 'override') &&
      !!hold.until &&
      member !== holdWindowMember(g);
    const until =
      overrides && hold?.until ? formatRouterTime(hold.until, v.clock?.offset ?? 0) : '';
    selecting.set(g.id, member);
    try {
      const res = await selectNode(g.id, member);
      if (overrides && selectSucceeded(v, res)) {
        showToast('success', $t('proxies.rt.sched.toast_override', { time: until }));
      } else {
        notifySelect(v, res, name);
      }
    } catch (e) {
      const code = codeOf(e);
      // Лимит исчерпан между загрузкой вида и кликом
      if (code === 'quota_held') showToast('warning', quotaText(g));
      else showToast('error', proxiesErrorText($t, code));
    } finally {
      selecting.delete(g.id);
    }
    // Фокус остаётся на плитке: карточка перерисовывает плитки по ключу участника
    await loadRuntime();
  }

  // Отказ фонового прохода выбора при запущенном Mihomo (D-11): сервер публикует уведомление
  // routing_select:<gid>:<n> с новым n на каждый отказ, и каждый новый идентификатор даёт один
  // тост ошибки. Уведомления первого загруженного снимка слоя появились до открытия страницы
  // и тоста не дают. Виденные идентификаторы — служебное множество вне реактивности
  const SELECT_NOTICE_PREFIX = 'routing_select:';
  const seenSelectNotices = new Set<string>();
  let selectNoticesPrimed = false;
  const layerLoaded = $derived($layerSnapshot !== null);

  /** ID группы уведомления: params.gid, иначе часть идентификатора между префиксом и поколением. */
  function selectNoticeGid(n: LayerNotice): string {
    const gid = n.params?.gid;
    if (gid) return gid;
    const rest = n.id.slice(SELECT_NOTICE_PREFIX.length);
    const cut = rest.lastIndexOf(':');
    return cut > 0 ? rest.slice(0, cut) : rest;
  }

  function notifySelectRejected(n: LayerNotice) {
    const gid = selectNoticeGid(n);
    const name = n.params?.group || view?.groups.find((g) => g.id === gid)?.name || gid;
    const reason = $t(`proxies.rt.select_bg_reason.${selectBgReason(n.params?.reason)}`);
    // Возврат прежнего выбора не подтверждён (reverted = 'false'): «Прежний узел возвращён» не
    // пишется (D-16). Без параметра — сервер его не шлёт — прежний текст о возврате
    const key =
      n.params?.reverted === 'false'
        ? 'proxies.rt.toast.unconfirmed_bg'
        : 'proxies.rt.toast.reverted_bg';
    showToast('error', $t(key, { name, reason }));
  }

  $effect(() => {
    const notices = $visibleNotices;
    if (!layerLoaded) return;
    const fresh = notices.filter(
      (n) => n.id.startsWith(SELECT_NOTICE_PREFIX) && !seenSelectNotices.has(n.id)
    );
    for (const n of fresh) seenSelectNotices.add(n.id);
    if (!selectNoticesPrimed) {
      selectNoticesPrimed = true;
      return;
    }
    untrack(() => {
      for (const n of fresh) notifySelectRejected(n);
    });
  });

  // Группы с идущим «Повторить» (D-16): повторное применение может перезапустить Xray
  // и идти долго, каждое нажатие не должно отправлять новый confirm
  const confirming = new SvelteSet<string>();

  async function onRetry(id: string) {
    if (confirming.has(id)) return;
    confirming.add(id);
    try {
      await confirmGroup(id);
    } catch (e) {
      // Занятость выбора или применения и снятая группа — своими текстами; без кода — сверка
      const code = codeOf(e);
      showToast('error', code ? proxiesErrorText($t, code) : $t('cfg.routing.err.confirm_failed'));
    } finally {
      confirming.delete(id);
    }
    await loadRuntime();
    // Кнопка на время запроса неактивна и теряет фокус: вернуть его на кнопку (сбой)
    // или на имя группы (плашка исчезла), если он никуда больше не ушёл
    await tick();
    if (document.activeElement && document.activeElement !== document.body) return;
    const card = cardEls[id];
    (
      card?.querySelector<HTMLElement>(`[data-testid="proxy-group-retry-${CSS.escape(id)}"]`) ??
      card?.querySelector<HTMLElement>('.pc-toggle')
    )?.focus();
  }

  // --- Пинг (D-05, D-07, D-08, D-19…D-22) ---

  let joined = $state(false);
  let pingError = $state('');

  /** Причина отказа для строки «Не удалось запустить проверку: …» без точки в конце. */
  function reasonText(code: string | undefined): string {
    const text = proxiesErrorText($t, code);
    // Незнакомый код даёт общий текст про обновление страницы: в середину фразы он не встаёт
    if (text === proxiesErrorText($t, undefined)) return $t('proxies.rt.reason.unknown');
    return lowerFirst(text.replace(/\.\s*$/, ''));
  }

  // После двоеточия фраза продолжает предложение: «…проверку: проверка узлов недоступна»
  function lowerFirst(text: string): string {
    // Имена и сокращения («Xray», «Mihomo», «TLS», «API») остаются как есть
    if (text === '' || /^(Xray|Mihomo|TLS|API|DNS|SSH)\b/.test(text)) return text;
    return text.charAt(0).toLowerCase() + text.slice(1);
  }

  /** Причина сбоя пачки: код пачки (proxies.rt.reason.*), а не код ответа API. */
  function batchReasonText(code: string | undefined): string {
    // Причина пачки встаёт после «Не удалось запустить проверку:», поэтому для start_failed
    // свой короткий текст без повтора глагола
    const key = code === 'start_failed' ? 'proxies.rt.ping.reason_start_failed' : '';
    return key !== '' ? $t(key) : reasonText(code);
  }

  function setBatch(b: PingBatch) {
    pingSnapshot.update((s) => ({ results: s?.results ?? {}, batch: b }));
  }

  async function runPing(scope: 'all' | 'group', group?: string) {
    if (batchRunning) return;
    pingError = '';
    joined = false;
    try {
      const res = await startPing(scope, group);
      joined = res.joined;
      setBatch(res.batch);
      // Пачка стартовала — окно перезапуска закончилось: прежний 409 не держит строку
      runtimeRestarting.set(false);
    } catch (e) {
      const code = codeOf(e);
      if (code === 'kernel_restarting') runtimeRestarting.set(true);
      else pingError = reasonText(code);
    }
  }

  // Отмена общей пачки из любой вкладки (D-22); закрытие вкладки пачку не трогает
  async function stopPing() {
    try {
      setBatch(await cancelPing());
    } catch (e) {
      showToast('error', proxiesErrorText($t, codeOf(e)));
    }
  }

  const pingErrorReason = $derived(
    pingError !== ''
      ? pingError
      : batch?.state === 'failed'
        ? batchReasonText(batch.reason_code)
        : ''
  );

  // Отказ запуска относится к своей попытке: новая пачка (в том числе из другой вкладки)
  // не должна возвращать его рядом с итогом «Проверено N узлов»
  let seenBatch = '';
  $effect(() => {
    const id = batch?.id ?? '';
    if (id === seenBatch) return;
    seenBatch = id;
    untrack(() => (pingError = ''));
  });

  const lastAt = $derived.by(() => {
    let max: number | undefined;
    for (const r of Object.values(results)) {
      const at = Date.parse(r.at);
      if (!Number.isNaN(at) && (max === undefined || at > max)) max = at;
    }
    return max;
  });

  /** Идущая пачка охватывает группу. */
  function pingCovers(g: RuntimeGroup): boolean {
    if (!batch || batch.state !== 'running') return false;
    if (batch.scope === 'all') return true;
    // У пачки repick группа есть всегда (proxyping.ScopeRepick)
    return batch.group === g.id;
  }

  // --- История и диагностика узла ---

  let popover = $state<{
    name: string;
    history: { time: string; delay: number }[];
    el: HTMLElement;
  } | null>(null);

  function openHistory(e: MouseEvent, g: RuntimeGroup, ref: string) {
    const el = e.currentTarget as HTMLElement | null;
    if (!el || !ref) return;
    let history = pingHistory(results[ref]);
    // Авто и резерв без замера панели — история ядра Mihomo (D-04, D-05)
    const delay = $runtimeNow?.history[ref];
    if (history.length === 0 && delay) history = [{ time: delay.at, delay: delay.ms }];
    popover = { name: g.members.find((m) => m.ref === ref)?.name || ref, history, el };
  }

  let diag = $state<{ sourceId: string; name: string } | null>(null);

  const installed = $derived({
    xray: view?.modes.xray !== undefined && view.modes.xray !== 'not_installed',
    mihomo: view?.modes.mihomo !== undefined && view.modes.mihomo !== 'not_installed'
  });
  const diagKernel: KernelName = $derived(
    kernel !== '' ? kernel : installed.xray || !installed.mihomo ? 'xray' : 'mihomo'
  );

  // Диагностика узла — диагностика его источника (Open Question 1): заголовок — имя
  // источника, не узла. Группы source:<id> есть только у «Xray у пользователя»,
  // поэтому имя приходит в самом участнике (source_name)
  function openDiagnostics(m: RuntimeMember) {
    if (!m.source) return;
    const src = m.source;
    const sourceGroup = view?.groups.find((g) => g.id === `source:${src}`);
    diag = { sourceId: src, name: m.source_name || sourceGroup?.name || src };
  }

  // --- Раскрытие группы по адресу и по звену цепочки (D-10, D-12) ---

  async function focusGroup(id: string) {
    if (!view?.groups.some((g) => g.id === id)) return;
    expanded.add(id);
    // Фильтры страницы не должны прятать группу, к которой ведёт ссылка
    if (!shownGroups.some((g) => g.id === id)) {
      filterQuery = '';
      obsFilter = null;
    }
    await tick();
    const el = cardEls[id];
    if (!el) return;
    el.scrollIntoView({ block: 'start', behavior: motion.reduced ? 'auto' : 'smooth' });
    el.querySelector<HTMLElement>('.pc-toggle')?.focus({ preventScroll: true });
  }

  let pendingGroup = $state('');

  function groupFromHash(): string {
    const q = window.location.hash.split('?')[1] ?? '';
    return new URLSearchParams(q).get('group') ?? '';
  }

  // Группа из адреса раскрывается, как только загружен вид
  $effect(() => {
    const id = pendingGroup;
    if (id === '' || !view) return;
    untrack(() => {
      pendingGroup = '';
      void focusGroup(id);
    });
  });

  function panelOf(g: RuntimeGroup, v: RuntimeView): PanelCard {
    return {
      group: g,
      view: v,
      kernel,
      results,
      now: $runtimeNow?.groups[g.id] ?? null,
      history: $runtimeNow?.history ?? {},
      selecting: selecting.get(g.id) ?? '',
      confirming: confirming.has(g.id),
      locked: lockedBy(g, v),
      pinging: pingCovers(g),
      pingSince: pingCovers(g) ? batch?.started_at : undefined,
      batchRunning,
      sorted: sortedGroups.has(g.id),
      onSelect: (member) => void onSelect(g, member),
      onPing: () => void runPing('group', g.id),
      onRetry: () => void onRetry(g.id),
      onToggleSort: () => {
        if (sortedGroups.has(g.id)) sortedGroups.delete(g.id);
        else sortedGroups.add(g.id);
      },
      onFocusGroup: (id) => void focusGroup(id),
      onDiagnostics: openDiagnostics,
      onHistory: (e, ref) => openHistory(e, g, ref)
    };
  }

  // --- Загрузка и живое обновление (D-14) ---

  async function reload() {
    refreshing = true;
    try {
      await Promise.all([loadRuntime(), loadResults()]);
    } finally {
      refreshing = false;
    }
  }

  // «Сейчас» авто-групп — опрос только при видимой вкладке, запущенном ядре и наличии
  // авто- или резервных групп; выбор и пинг приходят событиями общего SSE. Пока страница
  // считает ядро перезапускающимся (409 запуска пинга), опрос идёт и без авто-групп: его
  // первый успешный ответ снимает строку «Ядро перезапускается»
  const nowPollEnabled = writable(false);
  usePoller(loadNow, NOW_POLL_MS, { enabledWhen: nowPollEnabled });
  const needsNow = $derived(
    $runtimeRestarting ||
      ($runningKernels.length > 0 &&
        (view?.groups.some((g) => g.mode === 'auto' || g.mode === 'fallback') ?? false))
  );
  $effect(() => {
    nowPollEnabled.set(needsNow);
    // Опрос остановлен (ядра стоят, авто-групп нет): прежний «Сейчас» не показывается (D-26)
    if (!needsNow) resetNow();
  });

  // Расход лимитов для блока «Лимит» карточек (D-17): опрос раз в 5 с только при видимой
  // вкладке и наличии групп панели (у групп источника блока нет, T-152-34)
  const LIMITS_POLL_MS = 5000;
  const limitsPollEnabled = writable(false);
  usePoller(
    async (signal) => {
      await loadLimits(signal);
    },
    LIMITS_POLL_MS,
    { enabledWhen: limitsPollEnabled }
  );
  $effect(() => {
    limitsPollEnabled.set(
      !!view && view.state !== 'user_profile' && view.groups.some((g) => g.mode !== 'source')
    );
  });

  let kernelTimer: ReturnType<typeof setTimeout> | null = null;

  onMount(() => {
    // Данные прошлого визита страницы могли устареть на минуты
    resetNow();
    void reload();
    const detach = attachProxiesLive();
    const onHash = () => {
      const id = groupFromHash();
      if (id !== '') pendingGroup = id;
    };
    onHash();
    window.addEventListener('hashchange', onHash);
    return () => {
      detach();
      window.removeEventListener('hashchange', onHash);
      if (kernelTimer !== null) clearTimeout(kernelTimer);
      kernelTimer = null;
    };
  });

  // Смена активного или запущенных ядер меняет режим страницы: перечитать вид. Признак
  // «нет сокета» из capabilities тоже входит в ключ: профиль поправили и перезапустили
  // Mihomo быстрее такта опроса — набор ядер тот же, а вид страницы другой
  let kernelSeen = '';
  const capsNoSocket = $derived($capabilities?.mihomo?.no_socket === true);
  $effect(() => {
    const key = `${$activeKernelName}|${$runningKernels.join(',')}|${capsNoSocket}`;
    if (kernelSeen === '') {
      kernelSeen = key;
      return;
    }
    if (key === kernelSeen) return;
    kernelSeen = key;
    // «Сейчас» и история другого ядра не показываются до ответа нового
    resetNow();
    if (kernelTimer !== null) clearTimeout(kernelTimer);
    kernelTimer = setTimeout(() => {
      kernelTimer = null;
      void loadRuntime();
      // Опрос не перезапускается сам, если остался включённым: не ждать его такта
      if (needsNow) loadNow().catch(() => {});
    }, KERNEL_DEBOUNCE_MS);
  });

  const showEmpty = $derived(
    !!view && view.groups.length === 0 && (view.state === 'panel' || view.state === 'stopped')
  );
  // Режим Mihomo — только когда Mihomo работает на профиле панели (D-04)
  const showModeSwitch = $derived($activeKernelName === 'mihomo' && view?.modes.mihomo === 'panel');
  // Свой профиль без external-controller-unix (D-13): прежней странице Clash API не к чему
  // подключаться, вместо неё — подсказка S3. Остановленное ядро и профиль панели её не дают
  const noSocket = $derived(view?.state === 'user_profile' && view.no_socket);
</script>

{#snippet userProfileNotice()}
  <div class="pr-notice-slot">
    <ProxyRuntimeNotice viewState="user_profile" />
  </div>
{/snippet}

<div data-testid="proxies-layer-page">
  {#if noSocket}
    <div class="container">
      <PageHeader title={$t('proxies.title')} subtitle={$t('proxies.subtitle')} />
      <ProxyRuntimeNotice viewState="no_socket" />
    </div>
  {:else if view?.state === 'user_profile'}
    <!-- D-02: свой профиль Mihomo — прежняя страница Clash API запущенного профиля:
         выбор сразу через API, State.Live не пишется -->
    {#await import('./Proxies.svelte')}
      <div class="container">
        <Skeleton type="card" height="120px" />
      </div>
    {:then { default: Proxies }}
      <Proxies notice={userProfileNotice} />
    {:catch}
      <div class="container">
        <EmptyState
          title={$t('app.chunk_load_failed')}
          description=""
          ctaText={$t('app.retry')}
          oncta={() => window.location.reload()}
        />
      </div>
    {/await}
  {:else}
    <div class="container">
      <PageHeader title={$t('proxies.title')} subtitle={$t('proxies.subtitle')}>
        <ClientExitIpBadge />
        {#if showModeSwitch}
          <MihomoModeSwitch onchanged={() => void loadRuntime()} />
        {/if}
        <ProxyFilterBar
          bind:filterQuery
          bind:viewMode
          showViewMode={false}
          loading={refreshing}
          onExpandAll={() => shownGroups.forEach((g) => expanded.add(g.id))}
          onCollapseAll={() => expanded.clear()}
          onRefresh={() => void reload()}
        />
      </PageHeader>

      <div class="pr-body">
        {#if !view && loadError !== ''}
          <EmptyState
            title={$t('cfg.load_error_title')}
            description={$t('cfg.load_error_body')}
            ctaText={$t('cfg.load_error_retry')}
            oncta={() => void reload()}
          />
        {:else if !view}
          <div class="pr-stack" aria-busy="true">
            <Skeleton type="card" height="120px" />
            <Skeleton type="card" height="120px" />
          </div>
        {:else}
          {#if view.state === 'stopped' || view.state === 'user_managed' || batch?.fallback_reason}
            <div class="pr-notices">
              <ProxyRuntimeNotice
                viewState={view.state}
                fallbackReason={batch?.fallback_reason ?? ''}
                onstarted={() => void loadRuntime()}
              />
            </div>
          {/if}

          {#if showEmpty}
            <EmptyState
              title={$t('proxies.rt.empty.title')}
              description={$t('proxies.rt.empty.body')}
              ctaText={$t('proxies.rt.empty.cta')}
              ctaVariant="secondary"
              oncta={() => (window.location.hash = '#/config/groups')}
            />
          {:else}
            <ObservatoryPanel
              stats={observatoryStats}
              activeFilter={obsFilter}
              onFilterChange={(f) => (obsFilter = f)}
            />

            <ProxyPingBar
              {batch}
              {lastAt}
              {joined}
              errorReason={pingErrorReason}
              restarting={$runtimeRestarting}
              kernel={kernelLabel(kernel)}
              onpingall={() => void runPing('all')}
              oncancel={stopPing}
            />

            <div class="pr-stack">
              {#each shownGroups as g (g.id)}
                <ProxyGroupCard
                  group={g}
                  role="core"
                  panel={panelOf(g, view)}
                  isCollapsed={!expanded.has(g.id)}
                  onToggleCollapse={toggleCollapse}
                  groupFilter={groupFilters.get(g.id) ?? 'all'}
                  onSetGroupFilter={(id, f) => groupFilters.set(id, f)}
                  renderLimit={renderLimits.get(g.id) ?? RENDER_STEP}
                  onIncreaseRenderLimit={(id) =>
                    renderLimits.set(id, (renderLimits.get(id) ?? RENDER_STEP) + RENDER_STEP)}
                  bind:cardEl={cardEls[g.id]}
                />
              {/each}
            </div>
          {/if}
        {/if}
      </div>
    </div>
  {/if}
</div>

{#if popover}
  <LatencyHistoryPopover
    proxyName={popover.name}
    history={popover.history}
    anchorEl={popover.el}
    onclose={() => (popover = null)}
  />
{/if}

{#if diag}
  <SourceDiagnosticsModal
    open={true}
    name={diag.name}
    sourceId={diag.sourceId}
    {installed}
    initialKernel={diagKernel}
    onclose={() => (diag = null)}
  />
{/if}

<style>
  .pr-body {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-4);
  }

  .pr-notices {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-2);
  }

  .pr-notice-slot {
    margin-bottom: var(--spacing-4);
  }

  .pr-stack {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-4);
  }

  /* ObservatoryPanel несёт свой нижний отступ на прежней странице */
  .pr-body :global(.obs-card) {
    margin-bottom: 0;
  }

  @media (min-width: 769px) {
    .pr-body {
      gap: var(--spacing-6);
    }
  }
</style>
