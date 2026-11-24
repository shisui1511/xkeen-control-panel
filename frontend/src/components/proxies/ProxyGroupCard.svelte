<script lang="ts">
  import { t, tp } from '../../i18n';
  import Pin from '../../lib/components/icons/Pin.svelte';
  import ChevronDown from '../../lib/components/icons/ChevronDown.svelte';
  import HealthBar from './HealthBar.svelte';
  import { getMissingCountryFlag } from '../../lib/countryFlags';
  import { isSystemProxy, type GroupRole } from '../../lib/proxyClassification';
  import { isProxyAlive, computeGroupHealthStats } from '../../lib/proxyStats';
  import type { ProxiesViewMode } from '../../lib/proxyViewPrefs';
  import Button from '../Button.svelte';
  import Spinner from '../Spinner.svelte';
  import Icon from '../../lib/components/Icon.svelte';
  import ProxyNodeStatus from './ProxyNodeStatus.svelte';
  import GroupStateLine from './GroupStateLine.svelte';
  import GroupSchedule from './GroupSchedule.svelte';
  import GroupQuota from './GroupQuota.svelte';
  import { trafficLimits } from '../../lib/trafficLimits';
  import {
    splitMembers,
    nowLine,
    noReply,
    isFallbackNode,
    resultView,
    historyView,
    proxyLike,
    sortByPing,
    kernelLabel,
    otherKernel,
    type PanelCard,
    type NowLine,
    type ResultView
  } from '../../lib/proxyRuntimeAdapter';
  import type { RuntimeGroup, RuntimeMember } from '../../lib/configProxies';

  interface ChainItem {
    name: string;
    isGroup: boolean;
  }

  interface Props {
    group: any;
    role: GroupRole;
    groups?: any[];
    proxies?: Record<string, any>;
    isCollapsed?: boolean;
    viewMode?: ProxiesViewMode;
    searchQuery?: string;
    isPinned?: boolean;
    isAutoCore?: boolean;
    testingGroup?: boolean;
    testingProxy?: string;
    batchProgress?: any;
    groupFilter?: string;
    renderLimit?: number;
    quickSelectGroupName?: string | null;
    cardEl?: HTMLElement | null;
    onToggleCollapse?: (groupName: string) => void;
    onTogglePin?: (groupName: string) => void;
    onSelectProxy?: (groupName: string, proxyName: string) => void;
    onTestGroupLatency?: (group: any) => void;
    onTestProxyLatency?: (proxyName: string) => void;
    onOpenQuickSelect?: (anchorEl: HTMLElement, groupName: string) => void;
    onFocusGroupCard?: (groupName: string) => void;
    onSetGroupFilter?: (groupName: string, filter: string) => void;
    onIncreaseRenderLimit?: (groupName: string, total: number) => void;
    onBadgeMouseEnter?: (e: MouseEvent, name: string) => void;
    onBadgeMouseLeave?: () => void;
    onBadgeClick?: (e: MouseEvent, name: string) => void;
    resolveNodeSnapshot?: (name: string) => any;
    getLatencyClass?: (name: string) => string;
    getLatencyText?: (name: string) => string;
    getLatencyTitle?: (name: string) => string;
    getFilteredNodes?: (group: any, query: string) => string[];
    getFilteredGroupNodes?: (groupName: string, nodes: string[]) => string[];
    /**
     * Режим данных «панель» (слой «Конфигурация», D-13): группа панели из вида
     * работающих групп. Без пропа карточка остаётся карточкой Clash API. В режиме
     * «панель» обязательные group и role не читаются (страница передаёт группу панели
     * и role="core"), а isCollapsed, groupFilter, renderLimit и их колбэки получают ID группы.
     */
    panel?: PanelCard;
  }

  let {
    group,
    role,
    groups = [],
    proxies = {},
    isCollapsed = false,
    viewMode = 'grid',
    searchQuery = '',
    isPinned = false,
    isAutoCore = false,
    testingGroup = false,
    testingProxy = '',
    batchProgress = null,
    groupFilter = 'all',
    renderLimit = 60,
    quickSelectGroupName = null,
    cardEl = $bindable(),
    onToggleCollapse,
    onTogglePin,
    onSelectProxy,
    onTestGroupLatency,
    onTestProxyLatency,
    onOpenQuickSelect,
    onFocusGroupCard,
    onSetGroupFilter,
    onIncreaseRenderLimit,
    onBadgeMouseEnter,
    onBadgeMouseLeave,
    onBadgeClick,
    resolveNodeSnapshot = () => ({}),
    getLatencyClass = () => '',
    getLatencyText = () => '',
    getLatencyTitle = () => '',
    getFilteredNodes = (g) => g?.all || [],
    getFilteredGroupNodes = (_, n) => n,
    panel
  }: Props = $props();

  function getProxyTypeLabel(proxy: any): string {
    if (!proxy) return '';
    const type = (proxy.type || '').toLowerCase();
    if (type === 'shadowsocks') return 'SS';
    if (type === 'shadowsocksr') return 'SSR';
    if (type === 'vmess') return 'VMess';
    if (type === 'vless') return 'VLess';
    if (type === 'trojan') return 'Trojan';
    if (type === 'hysteria') return 'Hysteria';
    if (type === 'hysteria2') return 'Hysteria 2';
    if (type === 'tuic') return 'TUIC';
    if (type === 'socks5') return 'Socks5';
    if (type === 'http') return 'HTTP';
    if (type === 'wireguard') return 'WG';
    return proxy.type || '';
  }

  function getGroupTypeLabel(type: string): string {
    const key = (type || '').toLowerCase();
    const labelKeys: Record<string, string> = {
      selector: 'proxies.group_type_selector',
      urltest: 'proxies.group_type_urltest',
      fallback: 'proxies.group_type_fallback',
      loadbalance: 'proxies.group_type_loadbalance',
      relay: 'proxies.group_type_relay'
    };
    const translationKey = labelKeys[key];
    return translationKey ? $t(translationKey) : type;
  }

  function getSelectionChain(groupName: string): ChainItem[] {
    const chain: ChainItem[] = [];
    let current = groupName;
    const visited: string[] = [];
    while (current && !visited.includes(current)) {
      visited.push(current);
      const grp = groups.find((g) => g.name === current);
      if (!grp) break;
      const selected = grp.now;
      if (!selected) break;
      const isSelectedGroup = groups.some((g) => g.name === selected);
      chain.push({ name: selected, isGroup: isSelectedGroup });
      current = selected;
    }
    return chain;
  }

  function getGroupProviderName(grp: any): string | null {
    if (!grp || !Array.isArray(grp.all) || grp.all.length === 0) return null;
    const providers: string[] = grp.all
      .map((nodeName: string) => proxies[nodeName]?.provider)
      .filter((prov: unknown) => prov && typeof prov === 'string' && prov.trim() !== '');
    if (providers.length === 0) return null;
    let topProv: string | null = null;
    let topCount = 0;
    for (const prov of new Set(providers)) {
      const count = providers.filter((p) => p === prov).length;
      if (count > topCount) {
        topCount = count;
        topProv = prov;
      }
    }
    return topProv;
  }

  const nodes = $derived(getFilteredNodes(group, searchQuery));
  const isMini = $derived(role === 'system');
  const nowUpper = $derived((group.now || '').toUpperCase());
  const groupTypeKey = $derived((group.type || '').toLowerCase());
  const providerName = $derived(getGroupProviderName(group));

  const displayChain = $derived.by(() => {
    const fullChain = getSelectionChain(group.name);
    const fullText = fullChain.map((item) => item.name).join(' › ');
    if (fullChain.length <= 2) {
      return {
        items: fullChain,
        truncated: false,
        fullText
      };
    }
    return {
      items: [fullChain[0], fullChain[fullChain.length - 1]],
      truncated: true,
      fullText
    };
  });

  const filteredNodesList = $derived(getFilteredGroupNodes(group.name, nodes));
  const renderedNodes = $derived(filteredNodesList.slice(0, renderLimit));

  function handleHeadClick(e: MouseEvent) {
    const target = e.target as HTMLElement;
    if (target.closest('[data-stop-head-click]')) {
      return;
    }
    onToggleCollapse?.(group.name);
  }

  function handleHeadKeydown(e: KeyboardEvent) {
    if (e.key === 'Enter' || e.key === ' ') {
      e.preventDefault();
      onToggleCollapse?.(group.name);
    }
  }

  // --- Режим «панель» (D-13): группа панели, выбор кликом по плитке ---

  const MODE_KEYS: Record<RuntimeGroup['mode'], string> = {
    manual: 'proxies.rt.mode.manual',
    auto: 'proxies.rt.mode.auto',
    fallback: 'proxies.rt.mode.fallback',
    source: 'proxies.rt.mode.source'
  };

  const pSplit = $derived(
    panel ? splitMembers(panel.group, panel.kernel) : { visible: [], hidden: [] }
  );
  // Индекс участника в составе группы: testId плитки не зависит от сортировки
  const pIndex = $derived(new Map((panel?.group.members ?? []).map((m, i) => [m.member, i])));
  // Нет совместимых участников (D-23): сетки нет, выбор и пинг недоступны
  const pEmpty = $derived(!!panel && pSplit.visible.length === 0);
  // Совместимые узлы есть, но ни один не ответил (D-24): выбор остаётся доступен
  const pNoReply = $derived(
    !!panel && !pEmpty && noReply(panel.group, panel.results, panel.kernel)
  );
  const pNow: NowLine = $derived(
    panel ? nowLine(panel.group, panel.now, pNoReply) : { kind: 'none' }
  );
  const pAutoLike = $derived(panel?.group.mode === 'auto' || panel?.group.mode === 'fallback');
  const pFallbackName = $derived.by(() => {
    if (!panel || panel.group.mode !== 'fallback' || !panel.group.fallback) return '';
    const p = panel;
    const m = p.group.members.find((x) => isFallbackNode(p.group, x));
    return m?.name || p.group.fallback || '';
  });

  /** Замер узла: пинг панели, для авто и резерва без него — история ядра Mihomo (D-05). */
  function pResult(p: PanelCard, m: RuntimeMember): ResultView {
    const v = resultView(p.results, m.ref, p.kernel);
    if (v.state !== 'none' || p.group.mode === 'manual' || !m.ref) return v;
    return historyView(p.history[m.ref]);
  }

  // Узлы сетки: порядок состава или по задержке, затем фильтр карточки
  const pOrdered = $derived.by(() => {
    if (!panel) return [];
    const list = panel.sorted
      ? sortByPing(pSplit.visible, panel.results, panel.kernel)
      : pSplit.visible;
    if (groupFilter !== 'working' && groupFilter !== 'timeouts') return list;
    const p = panel;
    return list.filter((m) => {
      const v = pResult(p, m);
      if (m.kind !== 'node' || v.otherKernel !== undefined) return false;
      return groupFilter === 'working'
        ? v.state === 'ok'
        : v.state === 'timeout' || v.state === 'error';
    });
  });
  const pRendered = $derived(pOrdered.slice(0, renderLimit));

  // Полоса здоровья — только по узлам, которые умеет активное ядро
  const pHealth = $derived.by(() => {
    const snaps: Record<string, ReturnType<typeof proxyLike>> = {};
    if (panel) {
      for (const m of pSplit.visible) {
        if (m.kind === 'node') snaps[m.member] = proxyLike(m.name, pResult(panel, m));
      }
    }
    return computeGroupHealthStats(Object.keys(snaps), (name) => {
      const s = snaps[name];
      return s && s.delay !== undefined ? s : undefined;
    });
  });

  /** Идёт пачка, охватившая группу, а свежего замера узла ещё нет. */
  function pPending(p: PanelCard, m: RuntimeMember, v: ResultView): boolean {
    if (!p.pinging || m.kind !== 'node') return false;
    const since = p.pingSince ? Date.parse(p.pingSince) : NaN;
    if (Number.isNaN(since)) return v.state === 'none';
    return v.at === undefined || v.at < since || v.otherKernel !== undefined;
  }

  let hiddenOpen = $state(false);

  /** Выбор в группе доступен: ручная группа панели, выбор разрешён сервером. */
  function pCanSelect(p: PanelCard): boolean {
    return p.group.mode === 'manual' && p.group.selectable;
  }

  /**
   * Выбор сейчас заблокирован: недоступен, не подтверждён активный узел (D-16) или идёт
   * переключение своей или связанной группы (D-17); независимые группы не блокируются.
   */
  function pBlocked(p: PanelCard): boolean {
    return !pCanSelect(p) || p.group.unconfirmed || p.selecting !== '' || p.locked;
  }

  /** Подсказка заблокированной плитки: почему выбор недоступен. */
  function pBlockTitle(p: PanelCard): string | undefined {
    if (p.group.mode === 'auto' || p.group.mode === 'fallback') return $t('proxies.rt.auto_hint');
    if (p.group.unconfirmed) return $t('proxies.rt.unconfirmed_title');
    if (p.selecting !== '' || p.locked) return $t('proxies.rt.locked_title');
    if (p.group.mode === 'source') return $t('proxies.rt.source_title');
    if (!pCanSelect(p)) return $t('proxies.rt.select_unavailable_title');
    return undefined;
  }

  // Подсказка «проверка пойдёт через временный процесс»: ядра стоят, замеров в группе нет
  const pStoppedHint = $derived(
    !!panel &&
      panel.view.state === 'stopped' &&
      pSplit.visible.some((m) => m.kind === 'node') &&
      pSplit.visible.some((m) => m.kind === 'node' && pResult(panel, m).state === 'none')
  );

  /**
   * Участник — текущий выбор (ручная) или текущий узел ядра (авто и резерв). Выбор с
   * неподтверждённым активным узлом текущим не изображается (D-16).
   */
  function pIsCurrent(p: PanelCard, m: RuntimeMember): boolean {
    if (p.group.unconfirmed) return false;
    if (p.group.mode === 'manual') return p.group.current === m.member;
    return !!p.now?.ref && m.ref === p.now.ref;
  }

  // Повторные нажатия во время переключения не ставятся в очередь (D-17)
  function pSelect(p: PanelCard, m: RuntimeMember) {
    if (pBlocked(p) || pIsCurrent(p, m)) return;
    p.onSelect(m.member);
  }

  function pHeadClick(e: MouseEvent, id: string) {
    const target = e.target as HTMLElement;
    if (target.closest('[data-stop-head-click]')) return;
    onToggleCollapse?.(id);
  }

  /** Текст шаблона вокруг подстановки {name}: интерактивные звенья встают на её место. */
  function aroundName(text: (name: string) => string): [string, string] {
    const mark = '\u0000';
    const parts = text(mark).split(mark);
    return [parts[0] ?? '', parts.slice(1).join('')];
  }
</script>

{#if panel}
  {@const p = panel}
  {@const g = p.group}
  <div
    class="group-card pc-card"
    class:expanded={!isCollapsed}
    data-group={g.id}
    data-testid="proxy-group-{g.id}"
    bind:this={cardEl}
  >
    <!-- Шапка кликается мышью для удобства; с клавиатуры и читалкой группу сворачивает
         кнопка с именем (aria-expanded), а действия шапки — её соседи, не вложены в неё -->
    <!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_static_element_interactions -->
    <div class="gc-head collapsible" onclick={(e) => pHeadClick(e, g.id)}>
      <div class="gc-head-row1 pc-head-row1">
        <button
          type="button"
          class="pc-toggle"
          aria-expanded={!isCollapsed}
          data-stop-head-click
          onclick={(e) => {
            e.stopPropagation();
            onToggleCollapse?.(g.id);
          }}
        >
          <span class="pc-name">{g.name}</span>
        </button>
        <span class="badge badge-neutral">{$t(MODE_KEYS[g.mode])}</span>

        <div class="gc-head-actions pc-head-actions">
          {#if !pEmpty}
            <Button
              variant="secondary"
              size="sm"
              class="tap-zone-44"
              data-testid="proxy-group-ping-{g.id}"
              data-stop-head-click
              ariaLabel={$t('proxies.rt.ping.group_aria', { name: g.name })}
              aria-disabled={p.batchRunning ? 'true' : undefined}
              title={p.batchRunning ? $t('proxies.rt.ping.busy') : undefined}
              onclick={(e: MouseEvent) => {
                e.stopPropagation();
                if (!p.batchRunning) p.onPing();
              }}
            >
              {$t('proxies.rt.ping.group')}
            </Button>
          {/if}
          <button
            type="button"
            class="gc-chevron-btn"
            data-stop-head-click
            tabindex="-1"
            aria-hidden="true"
            onclick={(e) => {
              e.stopPropagation();
              onToggleCollapse?.(g.id);
            }}
          >
            <span class="chevron-wrap" class:rotated={!isCollapsed} aria-hidden="true">
              <ChevronDown size={14} color={isCollapsed ? 'var(--fg-dim)' : 'var(--accent)'} />
            </span>
          </button>
        </div>
      </div>

      {#if pNow.kind !== 'none'}
        <div class="pc-now" data-testid="proxy-group-now-{g.id}">
          {#if pNow.kind === 'chain'}
            {@const [before, after] = aroundName((name) => $t('proxies.rt.now', { name }))}
            {@const links = pNow.links}
            {@const full = links.map((l) => l.name).join(' › ')}
            {@const shown = links.length > 2 ? [links[0], links[links.length - 1]] : links}
            <span class="pc-now-text" title={full}>
              {before}{#each shown as link, i (i)}
                {#if i > 0}
                  <span class="gc-arrow" aria-hidden="true">›</span>
                  {#if links.length > 2}
                    <span class="gc-chain-ellipsis" title={full}>…</span>
                    <span class="gc-arrow" aria-hidden="true">›</span>
                  {/if}
                {/if}
                {#if link.kind === 'group' && link.id}
                  {@const linkId = link.id}
                  <button
                    type="button"
                    class="pc-now-link"
                    data-stop-head-click
                    aria-label={$t('proxies.rt.now_chain_aria', { name: link.name })}
                    onclick={(e) => {
                      e.stopPropagation();
                      p.onFocusGroup(linkId);
                    }}>{link.name}</button
                  >
                {:else}
                  <span class="pc-now-node">{link.name}</span>
                {/if}
              {/each}{after}
            </span>
          {:else if pNow.kind === 'fastest'}
            <span class="pc-now-text">{$t('proxies.rt.now_fastest')}</span>
          {:else}
            <span class="pc-now-text">{$t('proxies.rt.now_unknown')}</span>
          {/if}
          {#if pFallbackName}
            <span class="pc-now-fallback">
              {$t('proxies.rt.fallback_node', { name: pFallbackName })}
            </span>
          {/if}
        </div>
      {/if}
      <!-- UI-SPEC 1.1: состояние удержания группы арбитром, видно и в свёрнутом виде -->
      <GroupStateLine group={g} clock={p.view.clock} />
    </div>

    {#if g.unconfirmed}
      <!-- D-16: фактический выбор неизвестен — не «Сейчас» с акцентом, а ошибка и повтор -->
      <div
        class="alert alert-error pc-alert"
        role="alert"
        data-testid="proxy-group-unconfirmed-{g.id}"
      >
        <span class="pc-alert-text">{$t('proxies.rt.unconfirmed')}</span>
        <Button
          variant="secondary"
          size="sm"
          class="tap-zone-44"
          data-testid="proxy-group-retry-{g.id}"
          loading={p.confirming}
          aria-disabled={p.locked ? 'true' : undefined}
          title={p.locked ? $t('proxies.rt.locked_title') : undefined}
          onclick={() => {
            // D-17: пока группа переключается (в том числе в другой вкладке или при сверке),
            // «Повторить» получил бы select_busy
            if (!p.locked) p.onRetry();
          }}
        >
          {$t('proxies.rt.retry')}
        </Button>
      </div>
    {/if}

    {#if isCollapsed}
      {#if !pEmpty}
        <HealthBar stats={pHealth} />
      {/if}
    {:else}
      <div class="gc-body">
        <div class="gc-body-inner">
          {#if g.mode === 'manual'}<GroupSchedule group={g} clock={p.view.clock} />{/if}
          <GroupQuota group={g} limits={$trafficLimits} clock={p.view.clock} />
          {#if pEmpty}
            <!-- D-23: совместимых участников нет — вместо сетки сообщение и ссылка на группу -->
            <div class="pc-state" data-testid="proxy-group-empty-{g.id}">
              <span class="pc-state-text">
                {$t('proxies.rt.no_nodes', { kernel: kernelLabel(p.kernel) || 'Xray / Mihomo' })}
              </span>
              <a class="pc-link" href="#/config/groups?group={g.id}"
                >{$t('proxies.rt.open_in_config')}</a
              >
            </div>
          {:else}
            <div class="group-filters">
              <button
                type="button"
                class="filter-chip"
                class:active={groupFilter !== 'working' && groupFilter !== 'timeouts'}
                onclick={() => onSetGroupFilter?.(g.id, 'all')}
              >
                {$t('proxies.filter_all')}
                <span class="filter-count">{pSplit.visible.length}</span>
              </button>
              <button
                type="button"
                class="filter-chip"
                class:active={groupFilter === 'working'}
                onclick={() => onSetGroupFilter?.(g.id, 'working')}
              >
                {$t('proxies.filter_working')}
              </button>
              <button
                type="button"
                class="filter-chip"
                class:active={groupFilter === 'timeouts'}
                onclick={() => onSetGroupFilter?.(g.id, 'timeouts')}
              >
                {$t('proxies.filter_timeouts')}
              </button>

              <div class="group-actions-spacer"></div>

              <button
                type="button"
                class="filter-chip"
                class:active={p.sorted}
                aria-pressed={p.sorted}
                data-testid="proxy-group-sort-{g.id}"
                onclick={() => p.onToggleSort()}
              >
                {$t('proxies.rt.sort_by_ping')}
              </button>
            </div>

            {#if pNoReply}
              <!-- D-24: узлы есть, но ни один не ответил — выбор остаётся доступен -->
              <div class="pc-state pc-noreply" data-testid="proxy-group-noreply-{g.id}">
                <span class="pc-state-text">
                  <Icon name="info" size={14} />
                  {$t('proxies.rt.no_reply')}
                </span>
                <Button
                  variant="secondary"
                  size="sm"
                  class="tap-zone-44"
                  aria-disabled={p.batchRunning ? 'true' : undefined}
                  title={p.batchRunning ? $t('proxies.rt.ping.busy') : undefined}
                  onclick={() => {
                    if (!p.batchRunning) p.onPing();
                  }}
                >
                  {$t('proxies.rt.ping_again')}
                </Button>
              </div>
            {/if}

            <div class="proxy-grid pc-grid">
              {#each pRendered as m (m.member)}
                {@const idx = pIndex.get(m.member) ?? 0}
                {@const current = pIsCurrent(p, m)}
                {@const busy = p.selecting === m.member}
                {@const v = pResult(p, m)}
                {@const flag = m.kind === 'node' ? getMissingCountryFlag(m.name) : null}
                {@const reserve = isFallbackNode(g, m)}
                <div
                  class="proxy-card pc-tile"
                  class:now={current && g.mode === 'manual'}
                  class:pc-tile-current={current && pAutoLike}
                  class:pc-subgroup={m.kind === 'group'}
                  class:pc-static={pBlocked(p) || current}
                >
                  <button
                    type="button"
                    class="proxy-select-btn pc-select"
                    data-testid="proxy-node-{g.id}-{idx}"
                    aria-pressed={g.mode === 'manual' ? current : undefined}
                    aria-disabled={pBlocked(p) ? 'true' : undefined}
                    aria-busy={busy ? 'true' : undefined}
                    title={pBlockTitle(p)}
                    onclick={() => pSelect(p, m)}
                  >
                    <span class="p-header">
                      <span class="p-name">
                        {#if flag}
                          <span class="flag-icon" aria-hidden="true">{flag}</span>
                        {/if}
                        {m.name}
                      </span>
                      {#if m.kind === 'group'}
                        <span class="badge badge-neutral pc-tag">{$t('proxies.rt.chip.group')}</span
                        >
                      {:else if m.proto}
                        <span class="p-type">{getProxyTypeLabel({ type: m.proto })}</span>
                      {/if}
                      {#if reserve}
                        <span class="badge badge-neutral pc-tag"
                          >{$t('proxies.rt.chip.fallback')}</span
                        >
                      {/if}
                    </span>
                  </button>

                  <div class="p-footer">
                    {#if pPending(p, m, v)}
                      <span class="lat dim"><span class="lat-spinner"></span></span>
                    {:else if m.kind === 'node'}
                      <ProxyNodeStatus
                        result={v}
                        groupId={g.id}
                        index={idx}
                        onDiagnostics={() => p.onDiagnostics(m)}
                        onHistory={(e) => p.onHistory(e, m.ref ?? '')}
                      />
                    {:else}
                      <span></span>
                    {/if}
                    {#if busy}
                      <Spinner size={12} tone="current" />
                    {:else if g.mode === 'manual'}
                      <span class="selector-dot" class:active={current} aria-hidden="true"
                        >{current ? '●' : '○'}</span
                      >
                    {:else if pAutoLike && current}
                      <span class="selector-dot pc-auto-dot" aria-hidden="true">●</span>
                    {/if}
                  </div>
                </div>
              {/each}
            </div>

            {#if pAutoLike}
              <p class="pc-hint">{$t('proxies.rt.auto_hint')}</p>
            {/if}
            {#if pStoppedHint}
              <p class="pc-hint" data-testid="proxy-group-stopped-hint-{g.id}">
                {$t('proxies.rt.node.stopped_hint')}
              </p>
            {/if}

            {#if pOrdered.length > renderLimit}
              {@const remaining = pOrdered.length - renderLimit}
              <div class="proxy-grid-footer">
                <button
                  type="button"
                  class="proxy-grid-more"
                  onclick={() => onIncreaseRenderLimit?.(g.id, pOrdered.length)}
                >
                  {$t('proxies.show_more_nodes')}
                  {remaining}
                  {$tp('proxies.nodes', remaining)}
                </button>
                <span class="rendered-nodes-hint">
                  {$t('proxies.rendered_nodes_hint', {
                    shown: renderLimit,
                    total: pOrdered.length
                  })}
                </span>
              </div>
            {/if}
          {/if}

          {#if pSplit.hidden.length > 0}
            <div class="pc-hidden">
              <button
                type="button"
                class="pc-hidden-toggle"
                aria-expanded={hiddenOpen}
                data-testid="proxy-group-hidden-{g.id}"
                onclick={() => (hiddenOpen = !hiddenOpen)}
              >
                <Icon name={hiddenOpen ? 'chevron-down' : 'chevron-right'} size={12} />
                {$tp('proxies.rt.hidden', pSplit.hidden.length, {
                  kernel: kernelLabel(otherKernel(p.kernel))
                })}
              </button>
              {#if hiddenOpen}
                <p class="pc-hidden-hint">{$t('proxies.rt.hidden_hint')}</p>
                <ul class="pc-hidden-list">
                  {#each pSplit.hidden as m (m.member)}
                    <li>{m.name}</li>
                  {/each}
                </ul>
              {/if}
            </div>
          {/if}
        </div>
      </div>
    {/if}
  </div>
{:else}
  <div
    class="group-card"
    class:expanded={!isCollapsed}
    class:gc-mini={isMini}
    class:out-direct={isMini && nowUpper === 'DIRECT'}
    class:out-reject={isMini && (nowUpper === 'REJECT' || nowUpper === 'REJECT-DROP')}
    class:out-pass={isMini && nowUpper === 'PASS'}
    data-group={group.name}
    data-role={role}
    bind:this={cardEl}
  >
    <div
      class="gc-head"
      class:collapsible={!isMini}
      role="button"
      tabindex={isMini ? -1 : 0}
      aria-expanded={isMini ? undefined : !isCollapsed}
      onclick={isMini ? undefined : handleHeadClick}
      onkeydown={isMini ? undefined : handleHeadKeydown}
    >
      <div class="gc-head-row1">
        {#if group.icon}
          <span class="group-icon-wrap" aria-hidden="true">
            <img
              src={group.icon}
              alt=""
              loading="lazy"
              referrerpolicy="no-referrer"
              class="brand-icon"
              onerror={(e) => {
                const target = e.currentTarget as HTMLElement;
                if (target) target.style.display = 'none';
              }}
            />
          </span>
        {/if}
        <span class="name">{group.name}</span>
        <span class="type-badge" title={getGroupTypeLabel(group.type)}>
          <span class="type-badge-icon" aria-hidden="true">
            {#if groupTypeKey === 'selector'}
              <svg
                width="11"
                height="11"
                viewBox="0 0 24 24"
                fill="none"
                stroke="currentColor"
                stroke-width="2"><polyline points="20 6 9 17 4 12" /></svg
              >
            {:else if groupTypeKey === 'urltest' || groupTypeKey === 'fallback'}
              <svg
                width="11"
                height="11"
                viewBox="0 0 24 24"
                fill="none"
                stroke="currentColor"
                stroke-width="2"><path d="M21 12a9 9 0 1 1-3-6.7" /><path d="M21 3v6h-6" /></svg
              >
            {:else if groupTypeKey === 'loadbalance'}
              <svg
                width="11"
                height="11"
                viewBox="0 0 24 24"
                fill="none"
                stroke="currentColor"
                stroke-width="2"><path d="M7 7h11l-3-3" /><path d="M17 17H6l3 3" /></svg
              >
            {:else if groupTypeKey === 'relay'}
              <svg
                width="11"
                height="11"
                viewBox="0 0 24 24"
                fill="none"
                stroke="currentColor"
                stroke-width="2"
                ><circle cx="8" cy="12" r="3" /><circle cx="16" cy="12" r="3" /><path
                  d="M10.5 12h3"
                /></svg
              >
            {/if}
          </span>
          <span class="type-badge-label">{getGroupTypeLabel(group.type)}</span>
        </span>

        <div class="gc-head-actions">
          {#if role === 'core' || role === 'service'}
            <button
              type="button"
              class="gc-pin-btn"
              data-stop-head-click
              aria-pressed={isPinned}
              title={isAutoCore
                ? $t('proxies.pin_auto_core')
                : isPinned
                  ? $t('proxies.unpin_from_core')
                  : $t('proxies.pin_to_core')}
              aria-label={isAutoCore
                ? $t('proxies.pin_auto_core')
                : isPinned
                  ? $t('proxies.unpin_from_core')
                  : $t('proxies.pin_to_core')}
              disabled={isAutoCore}
              onclick={(e) => {
                e.stopPropagation();
                onTogglePin?.(group.name);
              }}
            >
              <Pin size={13} />
            </button>
          {/if}

          {#if isMini}
            <span class="gc-static-out" title={$t('proxies.static_output')}>{group.now}</span>
          {:else if group.now}
            {@const latencyClass = getLatencyClass(group.now)}
            {@const latencyText = getLatencyText(group.now)}
            <button
              type="button"
              class="gc-lat-box {latencyClass}"
              data-stop-head-click
              title={getLatencyTitle(group.now)}
              onmouseenter={(e) => onBadgeMouseEnter?.(e, group.now)}
              onmouseleave={() => onBadgeMouseLeave?.()}
              onclick={(e) => {
                e.stopPropagation();
                onBadgeClick?.(e, group.now);
              }}
              onkeydown={(e) => {
                if (e.key === 'Enter' || e.key === ' ') {
                  e.preventDefault();
                  e.stopPropagation();
                  onBadgeClick?.(e as any, group.now);
                }
              }}
            >
              {latencyText}
            </button>
            <button
              type="button"
              class="gc-ping-btn"
              data-stop-head-click
              title={$t('proxies.test_group')}
              aria-label={$t('proxies.test_group')}
              aria-busy={testingGroup}
              disabled={testingGroup}
              onclick={(e) => {
                e.stopPropagation();
                onTestGroupLatency?.(group);
              }}
            >
              {#if testingGroup}
                <span
                  class="spinner"
                  style="--spinner-size: 12px; --spinner-track: currentColor; --spinner-color: transparent;"
                ></span>
              {:else}
                <svg
                  width="12"
                  height="12"
                  viewBox="0 0 24 24"
                  fill="none"
                  stroke="currentColor"
                  stroke-width="2"
                >
                  <polygon points="13 2 3 14 12 14 11 22 21 10 12 10 13 2" fill="currentColor" />
                </svg>
              {/if}
            </button>
          {/if}

          {#if viewMode === 'list' && !isMini}
            <HealthBar compact={true} stats={computeGroupHealthStats(nodes, resolveNodeSnapshot)} />
          {/if}

          {#if !isMini}
            <button
              type="button"
              class="gc-chevron-btn"
              data-stop-head-click
              aria-expanded={!isCollapsed}
              aria-label={$t('proxies.toggle_group_nodes')}
              onclick={(e) => {
                e.stopPropagation();
                onToggleCollapse?.(group.name);
              }}
            >
              <span class="chevron-wrap" class:rotated={!isCollapsed} aria-hidden="true">
                <ChevronDown size={14} color={isCollapsed ? 'var(--fg-dim)' : 'var(--accent)'} />
              </span>
            </button>
          {/if}
        </div>
      </div>

      {#if !isMini}
        <div class="gc-head-row2" title={displayChain.fullText ? displayChain.fullText : undefined}>
          <span class="gc-count-text">
            {group.all.length}
            {$tp('proxies.nodes', group.all.length)}
          </span>
          {#if providerName}
            <span class="gc-provider-badge" title={$t('proxies.from_provider')}>
              <svg
                width="10"
                height="10"
                viewBox="0 0 24 24"
                fill="currentColor"
                aria-hidden="true"
              >
                <polygon points="13 2 3 14 12 14 11 22 21 10 12 10 13 2" />
              </svg>
              {providerName}
            </span>
          {/if}
          <span class="gc-separator">·</span>
          <span class="gc-active-label">{$t('proxies.active')}:</span>

          {#snippet chainPill(item: ChainItem)}
            {@const itemFlag = !item.isGroup ? getMissingCountryFlag(item.name) : null}
            {@const itemLatencyText = getLatencyText(item.name)}
            {@const itemLatencyClass = getLatencyClass(item.name)}
            {#if item.isGroup}
              <button
                type="button"
                class="gc-now-pill gc-now-pill-link"
                class:lat-ok={itemLatencyClass === 'lat ok'}
                class:lat-mid={itemLatencyClass === 'lat mid'}
                class:lat-bad={itemLatencyClass === 'lat bad'}
                title={$t('proxies.goto_parent_group')}
                data-stop-head-click
                onclick={(e) => {
                  e.stopPropagation();
                  onFocusGroupCard?.(item.name);
                }}
              >
                <div
                  class="gc-now-dot"
                  class:lat-ok={itemLatencyClass === 'lat ok'}
                  class:lat-mid={itemLatencyClass === 'lat mid'}
                  class:lat-bad={itemLatencyClass === 'lat bad'}
                ></div>
                {#if itemFlag}
                  <span class="flag-icon" aria-hidden="true">{itemFlag}</span>
                {/if}{item.name}
              </button>
            {:else}
              <button
                type="button"
                class="gc-now-pill is-leaf gc-now-pill-trigger"
                class:lat-ok={itemLatencyClass === 'lat ok'}
                class:lat-mid={itemLatencyClass === 'lat mid'}
                class:lat-bad={itemLatencyClass === 'lat bad'}
                data-stop-head-click
                aria-haspopup="listbox"
                aria-expanded={quickSelectGroupName === group.name}
                title={$t('proxies.quick_select_title')}
                onclick={(e) => {
                  e.stopPropagation();
                  onOpenQuickSelect?.(e.currentTarget as HTMLElement, group.name);
                }}
              >
                <div
                  class="gc-now-dot is-leaf"
                  class:lat-ok={itemLatencyClass === 'lat ok'}
                  class:lat-mid={itemLatencyClass === 'lat mid'}
                  class:lat-bad={itemLatencyClass === 'lat bad'}
                ></div>
                {#if itemFlag}
                  <span class="flag-icon" aria-hidden="true">{itemFlag}</span>
                {/if}{item.name}
              </button>
            {/if}
          {/snippet}

          {#if displayChain.truncated}
            {@render chainPill(displayChain.items[0])}
            <span class="gc-arrow">›</span>
            <span class="gc-chain-ellipsis" title={displayChain.fullText}>…</span>
            <span class="gc-arrow">›</span>
            {@render chainPill(displayChain.items[1])}
          {:else if displayChain.items.length > 0}
            {#each displayChain.items as item, index (index)}
              {#if index > 0}
                <span class="gc-arrow">›</span>
              {/if}
              {@render chainPill(item)}
            {/each}
          {:else}
            <span style="color:var(--fg-dim)" aria-label={$t('proxies.not_tested')}>—</span>
          {/if}
        </div>
      {/if}
    </div>

    {#if !isMini}
      {#if viewMode === 'grid' && isCollapsed}
        <HealthBar stats={computeGroupHealthStats(nodes, resolveNodeSnapshot)} />
      {:else if !isCollapsed}
        <div class="gc-body">
          <div class="gc-body-inner">
            <div class="group-filters">
              <button
                type="button"
                class="filter-chip"
                class:active={groupFilter === 'all'}
                onclick={() => onSetGroupFilter?.(group.name, 'all')}
              >
                {$t('proxies.filter_all')}
                <span class="filter-count">{nodes.length}</span>
              </button>
              <button
                type="button"
                class="filter-chip"
                class:active={groupFilter === 'working'}
                onclick={() => onSetGroupFilter?.(group.name, 'working')}
              >
                {$t('proxies.filter_working')}
              </button>
              <button
                type="button"
                class="filter-chip"
                class:active={groupFilter === 'timeouts'}
                onclick={() => onSetGroupFilter?.(group.name, 'timeouts')}
              >
                {$t('proxies.filter_timeouts')}
              </button>
              <button
                type="button"
                class="filter-chip"
                class:active={groupFilter === 'latency'}
                onclick={() => onSetGroupFilter?.(group.name, 'latency')}
              >
                {$t('proxies.filter_by_latency')}
              </button>

              <div class="group-actions-spacer"></div>

              <button
                type="button"
                class="filter-chip group-test-btn"
                onclick={() => onTestGroupLatency?.(group)}
                disabled={testingGroup}
                title={$t('proxies.test_group')}
              >
                <svg
                  width="11"
                  height="11"
                  viewBox="0 0 24 24"
                  fill="currentColor"
                  style="margin-right: 4px;"
                >
                  <polygon points="5 3 19 12 5 21 5 3"></polygon>
                </svg>
                {$t('proxies.test_group')}
              </button>
            </div>

            <div class="proxy-grid">
              {#each renderedNodes as proxyName, i (i)}
                {@const proxy = proxies[proxyName]}
                {@const isActive = group.now === proxyName}
                {@const flag = getMissingCountryFlag(proxyName)}
                {@const healthClass = getLatencyClass(proxyName)}
                {@const healthText = getLatencyText(proxyName)}
                <div class="proxy-card" class:now={isActive}>
                  <div
                    class="proxy-select-btn"
                    role="button"
                    tabindex={group.type === 'Selector' ? 0 : -1}
                    aria-disabled={group.type !== 'Selector'}
                    title={group.type !== 'Selector'
                      ? $t('proxies.managed_automatically')
                      : undefined}
                    onclick={() =>
                      group.type === 'Selector' && onSelectProxy?.(group.name, proxyName)}
                    onkeydown={(e) => {
                      if (group.type === 'Selector' && (e.key === 'Enter' || e.key === ' ')) {
                        e.preventDefault();
                        onSelectProxy?.(group.name, proxyName);
                      }
                    }}
                  >
                    <div class="p-header">
                      <span class="p-name" title={proxyName}>
                        {#if flag}
                          <span class="flag-icon" aria-hidden="true">{flag}</span>
                        {/if}
                        {proxyName}
                      </span>
                      <span class="p-type">{getProxyTypeLabel(proxy)}</span>
                    </div>
                  </div>

                  <div class="p-footer">
                    {#if (batchProgress?.running && batchProgress?.currentNode === proxyName) || testingProxy === proxyName}
                      <span class="lat dim">
                        <span class="lat-spinner"></span>
                      </span>
                    {:else}
                      <button
                        type="button"
                        class="lat {healthClass}"
                        title={getLatencyTitle(proxyName)}
                        onmouseenter={(e) => onBadgeMouseEnter?.(e, proxyName)}
                        onmouseleave={() => onBadgeMouseLeave?.()}
                        onclick={(e) => onBadgeClick?.(e, proxyName)}
                        onkeydown={(e) => {
                          if (e.key === 'Enter' || e.key === ' ') {
                            e.preventDefault();
                            onBadgeClick?.(e as any, proxyName);
                          }
                        }}
                      >
                        {healthText}
                      </button>
                    {/if}

                    <div class="p-actions-wrap">
                      {#if !isSystemProxy(proxyName, proxy?.type)}
                        <button
                          type="button"
                          class="btn-latency-test"
                          onclick={() => onTestProxyLatency?.(proxyName)}
                          disabled={testingProxy === proxyName}
                          title={$t('proxies.test_single')}
                        >
                          {#if testingProxy === proxyName}
                            <span
                              class="spinner"
                              style="--spinner-size: 12px; --spinner-track: currentColor; --spinner-color: transparent;"
                            ></span>
                          {:else}
                            <svg
                              width="12"
                              height="12"
                              viewBox="0 0 24 24"
                              fill="none"
                              stroke="currentColor"
                              stroke-width="2"
                              style="opacity: 0.6;"
                              ><path d="M13 2L3 14h9l-1 8 10-12h-9l1-8z" /></svg
                            >
                          {/if}
                        </button>
                      {/if}

                      {#if group.type === 'Selector'}
                        <span class="selector-dot" class:active={isActive}
                          >{isActive ? '●' : '○'}</span
                        >
                      {/if}
                    </div>
                  </div>
                </div>
              {/each}
            </div>

            {#if filteredNodesList.length > renderLimit}
              {@const remaining = filteredNodesList.length - renderLimit}
              <div class="proxy-grid-footer">
                <button
                  type="button"
                  class="proxy-grid-more"
                  onclick={() => onIncreaseRenderLimit?.(group.name, filteredNodesList.length)}
                >
                  {$t('proxies.show_more_nodes')}
                  {remaining}
                  {$tp('proxies.nodes', remaining)}
                </button>
                <span class="rendered-nodes-hint">
                  {$t('proxies.rendered_nodes_hint', {
                    shown: renderLimit,
                    total: filteredNodesList.length
                  })}
                </span>
              </div>
            {/if}
          </div>
        </div>
      {/if}
    {/if}
  </div>
{/if}

<style>
  .group-card {
    background: var(--bg-card);
    border: 1px solid var(--border);
    border-radius: var(--radius-lg);
    overflow: hidden;
    box-shadow: var(--shadow-sm);
    transition: box-shadow var(--transition-normal);
  }
  .group-card.expanded {
    grid-column: 1 / -1;
  }
  .group-card:hover {
    box-shadow:
      0 4px 20px rgba(0, 0, 0, 0.35),
      0 0 0 1px color-mix(in srgb, var(--accent) 15%, transparent);
  }
  .group-card .gc-head {
    background: linear-gradient(135deg, var(--bg-group-head-from), var(--bg-group-head-to));
    padding: var(--spacing-3-5) 18px;
    display: flex;
    flex-direction: column;
    gap: var(--spacing-2);
    border-bottom: 1px solid var(--border-strong);
    position: relative;
    overflow: hidden;
    width: 100%;
    text-align: left;
  }
  .group-card .gc-head::before {
    content: '';
    position: absolute;
    top: 0;
    left: 0;
    right: 0;
    bottom: 0;
    background: radial-gradient(
      ellipse at top left,
      color-mix(in srgb, var(--accent) 5%, transparent),
      transparent 60%
    );
    pointer-events: none;
  }
  .group-card .gc-head.collapsible {
    cursor: pointer;
    user-select: none;
  }
  .group-card .gc-head.collapsible:hover {
    background: var(--hover);
  }
  .group-card .gc-head.collapsible:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: -2px;
  }
  .gc-head-row1 {
    display: flex;
    align-items: center;
    width: 100%;
    gap: var(--spacing-2);
  }
  .gc-head-row2 {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: var(--spacing-1-5);
    font-size: var(--font-size-xs);
    color: var(--fg-secondary);
    width: 100%;
    margin-top: var(--spacing-0-5);
  }
  .group-card .gc-head .name {
    font-weight: 800;
    color: var(--fg-primary);
    font-size: var(--font-size-lg);
    letter-spacing: -0.01em;
  }
  /* Длинное имя занимает всю свободную ширину и обрезается на второй строке,
     а не сжимается в столбик по слову */
  .group-card .gc-head-row1 .name {
    flex: 1 1 auto;
    min-width: 0;
    overflow-wrap: anywhere;
    display: -webkit-box;
    -webkit-box-orient: vertical;
    -webkit-line-clamp: 2;
    line-clamp: 2;
    overflow: hidden;
  }
  .group-card .gc-head-row1 .type-badge {
    flex-shrink: 0;
  }
  .gc-head-actions {
    margin-left: auto;
    display: flex;
    align-items: center;
    gap: var(--spacing-2);
  }
  .type-badge {
    display: inline-flex;
    align-items: center;
    gap: var(--spacing-1);
    font-size: var(--font-size-xs);
    padding: 0;
    color: var(--fg-dim);
    font-family: var(--font-family-sans);
    font-weight: 600;
    letter-spacing: 0;
    text-transform: none;
  }
  .type-badge-icon {
    display: inline-flex;
    opacity: 0.7;
  }
  .gc-pin-btn {
    background: none;
    border: none;
    padding: 3px;
    cursor: pointer;
    color: var(--fg-faint);
    border-radius: var(--radius-sm);
    display: inline-flex;
    align-items: center;
    justify-content: center;
    transition:
      color var(--transition-fast),
      background var(--transition-fast),
      opacity var(--transition-fast);
    opacity: 0.6;
  }
  .gc-pin-btn:hover:not(:disabled) {
    opacity: 1;
    color: var(--accent);
    background: var(--hover);
  }
  .gc-pin-btn[aria-pressed='true'] {
    color: var(--accent);
    opacity: 1;
  }
  .gc-pin-btn:disabled {
    opacity: 0.3;
    cursor: default;
  }
  .gc-static-out {
    font-family: var(--font-family-mono);
    font-size: var(--font-size-xs);
    font-weight: 700;
    padding: var(--spacing-0-5) var(--spacing-2);
    border-radius: var(--radius-sm);
    background: var(--surface-tint);
    color: var(--fg-secondary);
  }
  .gc-lat-box {
    padding: 3px var(--spacing-2-5);
    border-radius: 99px;
    font-family: var(--font-family-sans);
    font-variant-numeric: tabular-nums;
    font-size: var(--font-size-xs);
    font-weight: 800;
    background: none;
    border: none;
    cursor: pointer;
    display: inline-flex;
    align-items: center;
  }
  .gc-lat-box:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: 1px;
  }
  .gc-lat-box.lat.ok {
    color: var(--success);
    background: color-mix(in srgb, var(--success) 15%, transparent);
    border: 1px solid color-mix(in srgb, var(--success) 35%, transparent);
  }
  .gc-lat-box.lat.mid {
    color: var(--warning);
    background: color-mix(in srgb, var(--warning) 15%, transparent);
    border: 1px solid color-mix(in srgb, var(--warning) 35%, transparent);
  }
  .gc-lat-box.lat.bad {
    color: var(--danger);
    background: color-mix(in srgb, var(--danger) 15%, transparent);
    border: 1px solid color-mix(in srgb, var(--danger) 35%, transparent);
  }
  .gc-lat-box.lat.dim {
    color: var(--fg-dim);
    background: color-mix(in srgb, var(--fg-dim) 15%, transparent);
    border: 1px solid color-mix(in srgb, var(--fg-dim) 35%, transparent);
  }
  .gc-ping-btn {
    background: none;
    border: none;
    padding: var(--spacing-0-5) var(--spacing-1);
    cursor: pointer;
    color: var(--fg-faint);
    border-radius: var(--radius-sm);
    display: inline-flex;
    align-items: center;
    justify-content: center;
    transition:
      color var(--transition-fast),
      background var(--transition-fast),
      opacity var(--transition-fast);
  }
  .gc-ping-btn:hover:not(:disabled) {
    color: var(--accent);
    background: var(--hover);
  }
  .gc-ping-btn:disabled {
    opacity: 0.5;
    cursor: default;
  }
  .gc-ping-btn:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: 1px;
  }
  .gc-chevron-btn {
    background: none;
    border: none;
    padding: var(--spacing-0-5);
    cursor: pointer;
    border-radius: var(--radius-sm);
    display: inline-flex;
    align-items: center;
    justify-content: center;
  }
  .chevron-wrap {
    display: inline-flex;
    align-items: center;
    transition: transform var(--transition-move);
  }
  .chevron-wrap.rotated {
    transform: rotate(180deg);
  }
  .gc-count-text {
    color: var(--fg-dim);
  }
  .gc-provider-badge {
    display: inline-flex;
    align-items: center;
    gap: 3px;
    font-size: var(--font-size-2xs);
    color: var(--accent);
    background: color-mix(in srgb, var(--accent) 8%, transparent);
    padding: 1px var(--spacing-1-5);
    border-radius: var(--radius-sm);
  }
  .gc-separator {
    color: var(--fg-faint);
  }
  .gc-active-label {
    color: var(--fg-secondary);
    font-size: var(--font-size-xs);
  }
  .gc-arrow {
    color: var(--fg-faint);
    margin: 0 var(--spacing-0-5);
  }
  .gc-chain-ellipsis {
    color: var(--fg-dim);
  }
  .gc-now-pill {
    display: inline-flex;
    align-items: center;
    gap: 5px;
    padding: var(--spacing-0-5) var(--spacing-2-5);
    border-radius: var(--radius-lg);
    background: var(--surface-tint);
    border: 1px solid var(--border);
    color: var(--fg-primary);
    font-size: var(--font-size-xs);
    font-weight: 600;
    transition:
      color var(--transition-normal),
      background var(--transition-normal),
      border-color var(--transition-normal);
    text-align: left;
    font-family: inherit;
  }
  .gc-now-pill.is-leaf {
    background: color-mix(in srgb, var(--accent) 8%, transparent);
    border-color: color-mix(in srgb, var(--accent) 20%, transparent);
    color: var(--accent);
  }
  .gc-now-dot {
    width: 6px;
    height: 6px;
    border-radius: 50%;
    background: var(--fg-dim);
  }
  .gc-now-dot.is-leaf {
    background: var(--accent);
  }
  .gc-now-dot.lat-ok {
    background: var(--success);
  }
  .gc-now-dot.lat-mid {
    background: var(--warning);
  }
  .gc-now-dot.lat-bad {
    background: var(--danger);
  }
  .gc-now-pill.lat-ok {
    background: color-mix(in srgb, var(--success) 8%, transparent);
    border-color: color-mix(in srgb, var(--success) 20%, transparent);
    color: var(--success);
  }
  .gc-now-pill.lat-mid {
    background: color-mix(in srgb, var(--warning) 8%, transparent);
    border-color: color-mix(in srgb, var(--warning) 20%, transparent);
    color: var(--warning);
  }
  .gc-now-pill.lat-bad {
    background: color-mix(in srgb, var(--danger) 8%, transparent);
    border-color: color-mix(in srgb, var(--danger) 20%, transparent);
    color: var(--danger);
  }
  .gc-now-pill-link {
    cursor: pointer;
  }
  .gc-now-pill-link:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: 1px;
  }
  .gc-now-pill-trigger {
    cursor: pointer;
  }
  .gc-now-pill-trigger:hover {
    box-shadow: inset 0 0 0 1px var(--accent);
  }
  .gc-now-pill-trigger:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: 1px;
  }

  .proxy-grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(180px, 1fr));
    gap: var(--spacing-2);
    padding: var(--spacing-3);
    content-visibility: auto;
    contain-intrinsic-size: 80px;
  }

  .proxy-card {
    background: var(--bg-elevated);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    padding: 0;
    display: flex;
    flex-direction: column;
    justify-content: space-between;
    position: relative;
    transition:
      background var(--transition-fast),
      border-color var(--transition-fast),
      box-shadow var(--transition-fast),
      transform var(--transition-fast);
    min-height: 84px;
  }
  .proxy-select-btn {
    display: flex;
    flex-direction: column;
    width: 100%;
    background: none;
    border: 0;
    padding: var(--spacing-2-5) var(--spacing-3) 0;
    color: inherit;
    font: inherit;
    text-align: left;
    cursor: pointer;
    border-radius: var(--radius-md) var(--radius-md) 0 0;
    flex: 1;
  }
  .proxy-select-btn:disabled,
  .proxy-select-btn[aria-disabled='true'] {
    cursor: default;
  }
  .proxy-select-btn:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: -2px;
  }
  .proxy-card::after {
    content: '';
    position: absolute;
    inset: 0;
    border-radius: var(--radius-md);
    background: linear-gradient(
      135deg,
      color-mix(in srgb, var(--accent) 3%, transparent),
      transparent
    );
    opacity: 0;
    transition: opacity var(--transition-fast);
    pointer-events: none;
  }
  .proxy-card:hover {
    border-color: var(--border-strong);
    transform: translateY(-1px);
    background: var(--hover);
  }
  .proxy-card:hover::after {
    opacity: 1;
  }
  .proxy-card.now {
    background: linear-gradient(
      135deg,
      color-mix(in srgb, var(--accent) 12%, transparent),
      color-mix(in srgb, var(--accent) 4%, transparent)
    );
    border-color: color-mix(in srgb, var(--accent) 45%, transparent);
    box-shadow:
      inset 0 0 0 1px color-mix(in srgb, var(--accent) 8%, transparent),
      0 2px 8px color-mix(in srgb, var(--accent) 8%, transparent);
  }
  .proxy-card .p-header {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-0-5);
    margin-bottom: var(--spacing-2);
  }
  .proxy-card .p-name {
    font-weight: 600;
    color: var(--fg-primary);
    font-size: var(--font-size-sm);
    word-break: break-all;
  }
  .flag-icon {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    font-family: 'TwemojiMozilla', var(--font-family-sans);
    font-size: 1.15em;
    line-height: 1;
    vertical-align: -0.1em;
    margin-right: 0.35rem;
    flex-shrink: 0;
    user-select: none;
  }
  .proxy-card .p-type {
    color: var(--fg-dim);
    font-size: var(--font-size-xs);
    font-family: var(--font-family-mono);
  }
  .proxy-card .p-footer {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--spacing-1-5);
    padding: var(--spacing-1) var(--spacing-3) var(--spacing-2-5);
    margin-top: auto;
  }
  .p-actions-wrap {
    display: flex;
    align-items: center;
    gap: var(--spacing-1-5);
  }
  .btn-latency-test {
    background: transparent;
    border: none;
    padding: var(--spacing-1);
    width: 24px;
    height: 24px;
    color: var(--fg-dim);
    cursor: pointer;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    transition:
      color var(--transition-normal),
      background var(--transition-normal);
    border-radius: var(--radius-sm);
    flex-shrink: 0;
  }
  .btn-latency-test:hover {
    color: var(--fg-primary);
    background: var(--hover);
  }
  .btn-latency-test:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: 1px;
  }
  .group-filters {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--spacing-1-5);
    padding: var(--spacing-2) var(--spacing-4) var(--spacing-2-5);
    border-bottom: 1px solid var(--border);
  }
  .filter-chip {
    display: inline-flex;
    align-items: center;
    gap: var(--spacing-1-5);
    padding: 3px var(--spacing-2-5);
    border-radius: var(--radius-full);
    font-size: var(--font-size-xs);
    font-weight: 500;
    background: var(--surface-tint);
    border: 1px solid var(--border);
    color: var(--fg-secondary);
    cursor: pointer;
    transition:
      color var(--transition-fast),
      background var(--transition-fast),
      border-color var(--transition-fast);
  }
  .filter-chip:hover {
    background: var(--hover);
    color: var(--fg-primary);
  }
  .filter-chip.active {
    background: color-mix(in srgb, var(--accent) 12%, transparent);
    border-color: var(--accent);
    color: var(--accent);
    font-weight: 600;
  }
  .filter-chip .filter-count {
    font-size: var(--font-size-xs);
    opacity: 0.7;
  }
  .group-actions-spacer {
    flex: 1;
  }
  .group-test-btn {
    margin-left: auto;
    color: var(--accent);
    border-color: color-mix(in srgb, var(--accent) 30%, transparent);
  }
  .group-test-btn:hover {
    background: color-mix(in srgb, var(--accent) 15%, transparent);
    border-color: var(--accent);
    color: var(--accent);
  }
  .group-test-btn:disabled {
    opacity: 0.5;
    cursor: not-allowed;
  }
  .selector-dot {
    font-size: var(--font-size-base);
    color: var(--fg-dim);
    font-weight: 700;
  }
  .selector-dot.active {
    color: var(--accent);
  }
  .group-icon-wrap {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 26px;
    height: 26px;
    flex: 0 0 26px;
    border-radius: var(--radius);
    background: var(--bg-elevated);
    border: 1px solid var(--border);
    overflow: hidden;
  }
  .group-icon-wrap .brand-icon {
    width: 18px;
    height: 18px;
    object-fit: contain;
    display: block;
  }
  .lat {
    font-family: var(--font-family-mono);
    font-size: var(--font-size-xs);
    font-weight: 600;
    padding: var(--spacing-0-5) var(--spacing-1-5);
    border-radius: var(--radius-sm);
    white-space: nowrap;
  }
  button.lat {
    border: none;
    cursor: pointer;
    font: inherit;
    font-family: var(--font-family-mono);
    font-size: var(--font-size-xs);
    font-weight: 600;
    line-height: 1.2;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    transition: opacity var(--transition-fast);
  }
  button.lat:hover {
    opacity: 0.85;
  }
  button.lat:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: 1px;
  }
  .lat.ok {
    color: var(--success);
    background: color-mix(in srgb, var(--success) 10%, transparent);
  }
  .lat.mid {
    color: var(--warning);
    background: color-mix(in srgb, var(--warning) 10%, transparent);
  }
  .lat.bad {
    color: var(--danger);
    background: color-mix(in srgb, var(--danger) 10%, transparent);
  }
  .lat.dim {
    color: var(--fg-dim);
    background: color-mix(in srgb, var(--fg-dim) 10%, transparent);
  }
  .lat-spinner {
    display: inline-block;
    width: 8px;
    height: 8px;
    border: 2px solid currentColor;
    border-right-color: transparent;
    border-radius: 50%;
    animation: spin var(--duration-spin) linear infinite;
  }
  .proxy-grid-footer {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--spacing-2);
    padding: var(--spacing-2) var(--spacing-3) var(--spacing-3);
    border-top: 1px solid var(--border);
  }
  .proxy-grid-more {
    background: var(--bg-card);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    color: var(--accent);
    padding: var(--spacing-1-5) var(--spacing-3);
    font-size: var(--font-size-xs);
    cursor: pointer;
    transition:
      background var(--transition-fast),
      border-color var(--transition-fast);
  }
  .proxy-grid-more:hover {
    background: var(--bg-card-hover);
    border-color: var(--accent);
  }
  .rendered-nodes-hint {
    font-size: var(--font-size-xs);
    color: var(--fg-dim);
  }

  /* --- Режим «панель» (D-13): новые классы pc-*, прежние классы режима Clash не меняются --- */
  .pc-head-row1 {
    flex-wrap: wrap;
  }
  /* Кнопка-имя группы: сворачивает и раскрывает карточку (клавиатура, читалка экрана) */
  .pc-toggle {
    flex: 1 1 auto;
    min-width: 0;
    padding: 0;
    background: none;
    border: 0;
    font: inherit;
    text-align: left;
    cursor: pointer;
  }
  .pc-toggle:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: 2px;
    border-radius: var(--radius-sm);
  }
  .pc-name {
    display: block;
    min-width: 0;
    color: var(--fg-primary);
    font-size: var(--font-size-lg);
    font-weight: 600;
    line-height: 1.2;
    overflow-wrap: anywhere;
  }
  .pc-head-actions {
    flex-wrap: wrap;
    justify-content: flex-end;
  }
  .pc-now {
    font-size: var(--font-size-sm);
    line-height: 1.5;
    color: var(--fg-secondary);
    overflow-wrap: anywhere;
  }
  .pc-now-link {
    background: none;
    border: 0;
    padding: 0;
    font: inherit;
    font-family: var(--font-family-mono);
    font-size: var(--font-size-xs);
    color: var(--accent);
    cursor: pointer;
    text-align: left;
    overflow-wrap: anywhere;
  }
  .pc-now-link:hover,
  .pc-now-link:focus-visible {
    text-decoration: underline;
  }
  .pc-now-link:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: 1px;
    border-radius: var(--radius-sm);
  }
  .pc-now-node {
    color: var(--fg-primary);
  }
  .pc-card .proxy-grid-footer {
    padding-left: var(--spacing-4);
    padding-right: var(--spacing-4);
  }
  /* Левый край шапки, фильтров, сетки и вспомогательных строк карточки один: 16px */
  .pc-card .gc-head {
    padding-left: var(--spacing-4);
    padding-right: var(--spacing-4);
  }
  .pc-grid {
    grid-template-columns: repeat(auto-fill, minmax(min(100%, 180px), 1fr));
    gap: var(--spacing-2);
    padding: var(--spacing-4);
  }
  /* Кнопка выбора растянута на всю плитку; кнопки нижней строки лежат поверх неё */
  .pc-select {
    flex: 0 0 auto;
  }
  .pc-select::before {
    content: '';
    position: absolute;
    inset: 0;
    border-radius: var(--radius-md);
  }
  .pc-select:focus-visible {
    outline: none;
  }
  .pc-select:focus-visible::before {
    outline: 2px solid var(--accent);
    outline-offset: -2px;
  }
  .pc-tile .p-footer {
    position: relative;
    z-index: 1;
    pointer-events: none;
  }
  .pc-tile .p-footer > :global(*) {
    pointer-events: auto;
  }
  .pc-now-fallback {
    display: block;
    color: var(--fg-secondary);
  }
  /* Вложенная группа среди участников — строка на всю ширину сетки (D-10) */
  .pc-subgroup {
    grid-column: 1 / -1;
    min-height: 0;
  }
  .pc-tag {
    align-self: flex-start;
  }
  /* Плитка без действия (выбор недоступен или узел уже выбран): не «приподнимается» под курсором */
  .pc-tile.pc-static:hover {
    transform: none;
  }
  .pc-tile.pc-static:not(.now):not(.pc-tile-current):hover {
    border-color: var(--border);
    background: var(--bg-elevated);
  }
  .pc-tile.pc-static.pc-tile-current:hover {
    border-color: var(--border-strong);
    background: var(--bg-elevated);
  }
  .pc-tile.pc-static:hover::after {
    opacity: 0;
  }
  .pc-select[aria-disabled='true'],
  .pc-tile.pc-static .pc-select {
    cursor: default;
  }
  /* Текущий узел авто и резерва: тот же индикатор, без акцентной рамки (D-09) */
  .pc-tile-current {
    border-color: var(--border-strong);
  }
  .selector-dot.pc-auto-dot {
    color: var(--fg-secondary);
  }
  .pc-alert {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    justify-content: space-between;
    gap: var(--spacing-2);
    margin: var(--spacing-2) var(--spacing-4) 0;
  }
  .pc-alert-text {
    flex: 1 1 auto;
    min-width: 0;
    overflow-wrap: anywhere;
  }
  /* Нет совместимых узлов (D-23) и никто не ответил (D-24) */
  .pc-state {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--spacing-2);
    padding: var(--spacing-2) var(--spacing-4);
    font-size: var(--font-size-sm);
    color: var(--fg-secondary);
  }
  .pc-state-text {
    display: inline-flex;
    align-items: center;
    gap: var(--spacing-1);
    flex: 1 1 auto;
    min-width: 0;
    overflow-wrap: anywhere;
  }
  .pc-noreply {
    border-bottom: 1px solid var(--border);
  }
  .pc-link {
    color: var(--accent);
    text-decoration: none;
  }
  .pc-link:hover,
  .pc-link:focus-visible {
    text-decoration: underline;
  }
  .pc-link:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: 1px;
    border-radius: var(--radius-sm);
  }
  .pc-hint {
    margin: 0;
    padding: 0 var(--spacing-4) var(--spacing-2);
    font-size: var(--font-size-xs);
    font-weight: 600;
    color: var(--fg-dim);
  }
  @media (max-width: 480px) {
    .pc-grid {
      grid-template-columns: 1fr;
    }
    .pc-state {
      display: grid;
    }
  }
  @media (pointer: coarse), (max-width: 768px) {
    .pc-card .gc-chevron-btn,
    .pc-now-link,
    .pc-link {
      min-height: 44px;
      min-width: 44px;
    }
    .pc-now-link,
    .pc-link {
      display: inline-flex;
      align-items: center;
    }
  }
  /* Узлы, которые активное ядро не умеет (D-06): свёрнутая строка и серый список */
  .pc-hidden {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-1);
    padding: var(--spacing-2) var(--spacing-4);
    border-top: 1px solid var(--border);
  }
  .pc-hidden-toggle {
    display: inline-flex;
    align-items: center;
    gap: var(--spacing-1);
    align-self: flex-start;
    background: none;
    border: 0;
    padding: var(--spacing-1) 0;
    font: inherit;
    font-size: var(--font-size-xs);
    font-weight: 600;
    color: var(--fg-dim);
    cursor: pointer;
    text-align: left;
  }
  .pc-hidden-toggle:hover {
    color: var(--fg-secondary);
  }
  .pc-hidden-toggle:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: 1px;
    border-radius: var(--radius-sm);
  }
  .pc-hidden-hint {
    margin: 0;
    font-size: var(--font-size-xs);
    color: var(--fg-dim);
  }
  .pc-hidden-list {
    margin: 0;
    padding: 0;
    list-style: none;
    display: flex;
    flex-direction: column;
    gap: var(--spacing-1);
    max-height: 40dvh;
    overflow-y: auto;
    font-size: var(--font-size-xs);
    color: var(--fg-dim);
    overflow-wrap: anywhere;
  }
  @media (pointer: coarse), (max-width: 768px) {
    .pc-hidden-toggle {
      min-height: 44px;
    }
  }

  /* Group List View (D-18, D-19) */
  :global(.group-grid.group-list) .group-card {
    border-radius: var(--radius-sm);
  }
  :global(.group-grid.group-list) .group-card.expanded {
    grid-column: auto;
  }
  :global(.group-list) .gc-head {
    flex-direction: row;
    align-items: center;
    height: 40px;
    padding: 0 var(--spacing-3);
    gap: var(--spacing-2-5);
  }
  :global(.group-list) .gc-head-row1 {
    flex: 0 1 auto;
    width: auto;
    gap: var(--spacing-2);
  }
  :global(.group-list) .gc-head-row2 {
    width: auto;
    margin-top: 0;
    flex: 1 1 auto;
    min-width: 0;
    justify-content: flex-end;
    gap: var(--spacing-1-5);
  }
  :global(.group-list) .gc-count-text,
  :global(.group-list) .gc-active-label,
  :global(.group-list) .gc-provider-badge {
    display: none;
  }
  :global(.group-list) .gc-head .name {
    font-size: var(--font-size-sm);
    max-width: 220px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  :global(.group-list) .health-bar {
    width: 64px;
    margin: 0;
    flex: 0 0 64px;
  }
  :global(.group-list) .proxy-grid {
    grid-template-columns: repeat(auto-fill, minmax(min(100%, 200px), 1fr));
    gap: var(--spacing-1-5);
    padding: var(--spacing-2) var(--spacing-3);
  }
  :global(.group-list) .gc-body {
    display: grid;
    grid-template-rows: 0fr;
    transition: grid-template-rows var(--transition-move);
  }
  :global(.group-list) .expanded .gc-body,
  :global(.group-list .group-card.expanded) .gc-body {
    grid-template-rows: 1fr;
  }
  :global(.group-list) .gc-body-inner {
    overflow: hidden;
    min-height: 0;
  }

  /* Узкий телефон: тип группы — только значком (подпись остаётся для диктора
     и во всплывающей подсказке), чтобы флаг не отрывался от имени группы */
  @media (max-width: 480px) {
    .type-badge-label {
      position: absolute;
      width: 1px;
      height: 1px;
      overflow: hidden;
      clip: rect(0 0 0 0);
      white-space: nowrap;
    }

    .type-badge-icon {
      opacity: 1;
    }
  }

  /* Телефон: карточки групп плоские — тени под каждой в столбике сливались */
  @media (max-width: 768px) {
    .group-card,
    .group-card:hover {
      box-shadow: none;
    }
  }

  @media (max-width: 768px) {
    :global(.group-list) .gc-head {
      height: 44px;
    }
    :global(.group-list) .gc-chevron-btn,
    :global(.group-list) .gc-ping-btn,
    :global(.group-list) .gc-pin-btn {
      min-width: 44px;
      min-height: 44px;
      justify-content: center;
    }
  }
</style>
