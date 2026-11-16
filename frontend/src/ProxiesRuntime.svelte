<script lang="ts">
  // Страница «Прокси» при включённом слое «Конфигурация»: что выбрано сейчас в группах
  // панели для обоих ядер. Без флага слоя Dashboard рисует прежний Proxies.svelte, и этот
  // модуль вместе с клиентом configProxies.ts не загружается.
  import { onMount } from 'svelte';
  import { SvelteMap, SvelteSet } from 'svelte/reactivity';
  import { t } from './i18n';
  import { activeKernelName, runningKernels, showToast } from './stores';
  import PageHeader from './PageHeader.svelte';
  import Skeleton from './components/Skeleton.svelte';
  import EmptyState from './components/EmptyState.svelte';
  import ClientExitIpBadge from './components/network/ClientExitIpBadge.svelte';
  import ProxyGroupCard from './components/proxies/ProxyGroupCard.svelte';
  import {
    runtimeView,
    runtimeLoadError,
    pingSnapshot,
    runtimeNow,
    loadRuntime,
    loadResults,
    selectNode,
    confirmGroup,
    attachProxiesLive,
    proxiesErrorText,
    type ProxyKernel,
    type RuntimeGroup,
    type RuntimeView
  } from './lib/configProxies';
  import { kernelLabel, type ActiveKernel, type PanelCard } from './lib/proxyRuntimeAdapter';
  import type { SelectResult } from './lib/configRouting';

  // Перечитывание вида после смены ядра: события процесса приходят пачкой
  const KERNEL_DEBOUNCE_MS = 300;

  const view = $derived($runtimeView);
  const loadError = $derived($runtimeLoadError);

  // Активное ядро карточек: при остановленных ядрах — '' (видны узлы обоих ядер)
  const kernel: ActiveKernel = $derived(
    $activeKernelName === 'xray' || $activeKernelName === 'mihomo' ? $activeKernelName : ''
  );

  // --- Свёрнутость, фильтры и порог плиток карточек (ключ — ID группы) ---
  const expanded = new SvelteSet<string>();
  const groupFilters = new SvelteMap<string, string>();
  const renderLimits = new SvelteMap<string, number>();
  const sortedGroups = new SvelteSet<string>();
  const RENDER_STEP = 60;

  function toggleCollapse(id: string) {
    if (expanded.has(id)) expanded.delete(id);
    else expanded.add(id);
  }

  // --- Выбор (D-11, D-15…D-18) ---

  /** Идущий выбор: группа и участник; повторные нажатия во время него игнорируются. */
  let selecting = $state<{ group: string; member: string } | null>(null);

  /** Группа заблокирована идущим выбором в связанной группе (D-17). */
  function lockedBy(g: RuntimeGroup, v: RuntimeView): boolean {
    const s = selecting;
    if (!s || s.group === g.id) return false;
    if (g.locked_with.includes(s.group)) return true;
    const sg = v.groups.find((x) => x.id === s.group);
    return !!sg && sg.locked_with.includes(g.id);
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

  async function onSelect(g: RuntimeGroup, member: string) {
    const v = view;
    if (!v || selecting !== null) return;
    const name = memberName(g, member);
    selecting = { group: g.id, member };
    try {
      const res = await selectNode(g.id, member);
      notifySelect(v, res, name);
    } catch (e) {
      const code = (e as { code?: unknown } | null)?.code;
      showToast('error', proxiesErrorText($t, typeof code === 'string' ? code : undefined));
    } finally {
      selecting = null;
    }
    // Фокус остаётся на плитке: карточка перерисовывает плитки по ключу участника
    await loadRuntime();
  }

  async function onRetry(id: string) {
    try {
      await confirmGroup(id);
    } catch {
      showToast('error', $t('cfg.routing.err.confirm_failed'));
    }
    await loadRuntime();
  }

  function panelOf(g: RuntimeGroup, v: RuntimeView): PanelCard {
    return {
      group: g,
      view: v,
      kernel,
      results: $pingSnapshot?.results ?? {},
      now: $runtimeNow?.groups[g.id] ?? null,
      history: $runtimeNow?.history ?? {},
      selecting: selecting?.group === g.id ? selecting.member : '',
      locked: lockedBy(g, v),
      pinging: false,
      batchRunning: false,
      sorted: sortedGroups.has(g.id),
      onSelect: (member) => void onSelect(g, member),
      onPing: () => {},
      onRetry: () => void onRetry(g.id),
      onToggleSort: () => {
        if (sortedGroups.has(g.id)) sortedGroups.delete(g.id);
        else sortedGroups.add(g.id);
      },
      onFocusGroup: (id) => {
        expanded.add(id);
      },
      onDiagnostics: () => {},
      onHistory: () => {}
    };
  }

  // --- Загрузка и живое обновление (D-14) ---

  function reload() {
    void loadRuntime();
    void loadResults();
  }

  onMount(() => {
    reload();
    const detach = attachProxiesLive();
    return () => {
      detach();
      if (kernelTimer !== null) clearTimeout(kernelTimer);
      kernelTimer = null;
    };
  });

  // Смена активного или запущенных ядер меняет режим страницы: перечитать вид
  let kernelTimer: ReturnType<typeof setTimeout> | null = null;
  let kernelSeen = '';
  $effect(() => {
    const key = `${$activeKernelName}|${$runningKernels.join(',')}`;
    if (kernelSeen === '') {
      kernelSeen = key;
      return;
    }
    if (key === kernelSeen) return;
    kernelSeen = key;
    if (kernelTimer !== null) clearTimeout(kernelTimer);
    kernelTimer = setTimeout(() => {
      kernelTimer = null;
      void loadRuntime();
    }, KERNEL_DEBOUNCE_MS);
  });

  const showEmpty = $derived(
    !!view && view.groups.length === 0 && (view.state === 'panel' || view.state === 'stopped')
  );
</script>

<div class="container" data-testid="proxies-layer-page">
  <PageHeader title={$t('proxies.title')} subtitle={$t('proxies.subtitle')}>
    <ClientExitIpBadge />
  </PageHeader>

  {#if !view && loadError !== ''}
    <EmptyState
      title={$t('cfg.load_error_title')}
      description={$t('cfg.load_error_body')}
      ctaText={$t('cfg.load_error_retry')}
      oncta={reload}
    />
  {:else if !view}
    <div class="pr-stack" aria-busy="true">
      <Skeleton type="card" height="120px" />
      <Skeleton type="card" height="120px" />
    </div>
  {:else if showEmpty}
    <EmptyState
      title={$t('proxies.rt.empty.title')}
      description={$t('proxies.rt.empty.body')}
      ctaText={$t('proxies.rt.empty.cta')}
      ctaVariant="secondary"
      oncta={() => (window.location.hash = '#/config/groups')}
    />
  {:else}
    <div class="pr-stack">
      {#each view.groups as g (g.id)}
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
        />
      {/each}
    </div>
  {/if}
</div>

<style>
  .pr-stack {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-4);
  }
</style>
