<script lang="ts">
  import { onMount } from 'svelte';
  import { t } from '../../../i18n';
  import { activeKernelName } from '../../../stores';
  import Button from '../../Button.svelte';
  import EmptyState from '../../EmptyState.svelte';
  import Skeleton from '../../Skeleton.svelte';
  import Icon from '../../../lib/components/Icon.svelte';
  import { applyRunning, layerSnapshot } from '../../../lib/configLayer';
  import {
    fetchCores,
    onCoresChanged,
    type CoreKernel,
    type KernelView,
    type SectionView
  } from '../../../lib/configCores';
  import KernelSwitch from './KernelSwitch.svelte';
  import SectionCard from './SectionCard.svelte';
  import MihomoProfileNotice from './MihomoProfileNotice.svelte';
  import ProfilesCard from '../profiles/ProfilesCard.svelte';
  import TakeSectionModal from './TakeSectionModal.svelte';
  import { coresBusy } from './coresOwner';

  // Вкладка «Xray │ Mihomo»: переключатель ядра, строка состояния, загрузка, пустое
  // состояние и карточки секций (SEC-01). Оба вида ядер загружаются при открытии и остаются
  // смонтированными (U-03): переключение не перезагружает данные и не сбрасывает правки.
  // Вид только читается; владение секциями меняют карточки и диалог взятия — в черновик.
  const KERNELS: readonly CoreKernel[] = ['xray', 'mihomo'];

  // Постоянный порядок карточек (UI-SPEC)
  const ORDER: Record<CoreKernel, readonly string[]> = {
    xray: ['log', 'dns', 'inbounds', 'policy', 'other'],
    mihomo: ['main', 'dns', 'inbounds', 'sniffer', 'other']
  };

  // Раскрытие карточек: ключ «ядро.секция». Состояние между сессиями не сохраняется
  const open = $state<Record<string, boolean>>(
    Object.fromEntries(KERNELS.flatMap((k) => ORDER[k].map((id) => [`${k}.${id}`, false])))
  );

  let views = $state<Record<CoreKernel, KernelView | null>>({ xray: null, mihomo: null });
  let failed = $state<Record<CoreKernel, boolean>>({ xray: false, mihomo: false });
  // Ядро, выбранное пользователем; до выбора берётся активное ядро
  let chosen = $state<CoreKernel | null>(null);

  // Номер запроса на ядро: поздний ответ устаревшего запроса не затирает свежий вид
  const seq: Record<CoreKernel, number> = { xray: 0, mihomo: 0 };

  async function load(kernel: CoreKernel): Promise<void> {
    const mine = ++seq[kernel];
    try {
      const view = await fetchCores(kernel);
      if (mine !== seq[kernel]) return;
      views[kernel] = view;
      failed[kernel] = false;
    } catch {
      if (mine !== seq[kernel]) return;
      // Прежний вид остаётся на экране; ошибка нужна, только пока видов ещё нет
      failed[kernel] = true;
    }
  }

  function loadAll(): void {
    for (const kernel of KERNELS) void load(kernel);
  }

  onMount(() => {
    loadAll();
    return onCoresChanged(loadAll);
  });

  // Активное ядро; при конфликте или без запущенного — Xray, если установлен, иначе Mihomo
  const defaultKernel = $derived.by<CoreKernel>(() => {
    const active = $activeKernelName;
    if (active !== '') return active;
    return views.xray?.installed === false && views.mihomo?.installed === true ? 'mihomo' : 'xray';
  });
  const kernel = $derived<CoreKernel>(chosen ?? defaultKernel);

  const current = $derived(views[kernel]);
  const stateText = $derived.by(() => {
    if (current === null) return '';
    if (!current.installed) return $t('cfg.sec.kernel_not_installed');
    return current.running ? $t('cfg.sec.kernel_running') : $t('cfg.sec.kernel_stopped_state');
  });
  const stopped = $derived(current !== null && current.installed && !current.running);

  // Плашка профиля показывает primary «Управлять в панели», когда Mihomo ведёт XKeen,
  // пользователь или файла нет: тогда «Новый профиль» в карточке — secondary
  function mihomoNoticePrimary(owner: string | undefined): boolean {
    return owner === 'user' || owner === 'xkeen' || owner === 'none';
  }

  function kernelTitle(k: CoreKernel): string {
    return k === 'xray' ? 'Xray' : 'Mihomo';
  }

  /** Секции вида в постоянном порядке карточек; неизвестные серверу секции — в конце. */
  function ordered(view: KernelView): SectionView[] {
    const rank = (id: string): number => {
      const i = ORDER[view.kernel].indexOf(id);
      return i === -1 ? ORDER[view.kernel].length : i;
    };
    return [...view.sections].sort((a, b) => rank(a.id) - rank(b.id));
  }

  // Маршрутизация у панели или желаема в черновике: от этого зависит блок группы
  // force-proxy в формах «Входящие» (D-12)
  function routingTaken(k: CoreKernel): boolean {
    const r = $layerSnapshot?.routing;
    if (!r) return false;
    return r.desired === true || r[k]?.taken === true;
  }

  // Блок api Xray не пишется ниже минимальной версии ядра: доступность функции решает
  // сервер (features снимка слоя), интерфейс номера версий не сравнивает. Строка с причиной
  // нужна, когда Xray установлен и маршрутизация Xray у панели или желаема в черновике;
  // «Применить» она не блокирует
  const apiUnavailable = $derived(
    views.xray?.installed === true &&
      $layerSnapshot?.features?.xray_hot_swap_api === 'unavailable' &&
      routingTaken('xray')
  );
  const apiMinVersion = $derived(
    ($layerSnapshot?.kernels ?? []).find((kn) => kn.name === 'xray')?.min_version ?? ''
  );

  // Диалог взятия: одна секция или все секции ядра
  let takeOpen = $state(false);
  let takeKernel = $state<CoreKernel>('xray');
  let takeMode = $state<'one' | 'all'>('one');
  let takeSection = $state('');

  function openTake(k: CoreKernel, mode: 'one' | 'all', id = ''): void {
    takeKernel = k;
    takeMode = mode;
    takeSection = id;
    takeOpen = true;
  }

  // Только что взятые секции раскрываются: owner-бейдж «Будет взята при «Применить»»
  function taken(ids: string[]): void {
    for (const id of ids) open[`${takeKernel}.${id}`] = true;
  }

  const takeView = $derived(views[takeKernel]);
  const takeAllIds = $derived(
    takeView
      ? ordered(takeView)
          .filter((s) => s.owner !== 'panel' && s.owner !== 'pending' && s.owner !== 'releasing')
          .map((s) => s.id)
      : []
  );
  const takeBlocked = $derived($coresBusy || $applyRunning);
</script>

<div class="cores-tab" data-testid="cores-tab">
  <div class="cores-bar">
    <KernelSwitch value={kernel} onchange={(k) => (chosen = k)} />
    <div class="cores-state">
      <p class="cores-state-line" data-testid="kernel-state">{stateText}</p>
      {#if stopped}
        <p class="cores-state-line" data-testid="kernel-stopped">{$t('cfg.sec.kernel_stopped')}</p>
      {/if}
    </div>
    {#if kernel === 'xray' && current !== null && current.installed}
      {#if current.take_all}
        <Button
          variant="primary"
          disabled={takeBlocked}
          title={$applyRunning ? $t('cfg.routing.apply_busy_hint') : undefined}
          data-testid="cores-take-all"
          onclick={() => openTake('xray', 'all')}
        >
          {$t('cfg.sec.take_all_btn')}
        </Button>
      {:else}
        <p class="cores-all-panel" data-testid="cores-all-panel">
          <span class="cores-ok"><Icon name="check" size={16} /></span>
          {$t('cfg.sec.all_panel')}
        </p>
      {/if}
    {/if}
  </div>

  {#each KERNELS as k (k)}
    {@const view = views[k]}
    <div class="cores-panel" data-testid="cores-panel-{k}" hidden={k !== kernel}>
      {#if view === null && failed[k]}
        <EmptyState
          title={$t('cfg.load_error_title')}
          description={$t('cfg.load_error_body')}
          ctaText={$t('cfg.load_error_retry')}
          plain
          oncta={() => void load(k)}
        />
      {:else if view === null}
        <div class="cores-skeleton" aria-hidden="true">
          <Skeleton type="rect" height="56px" />
          <Skeleton type="rect" height="56px" />
          <Skeleton type="rect" height="56px" />
        </div>
      {:else if !view.installed}
        <EmptyState
          title={$t('cfg.sec.empty_title', { kernel: kernelTitle(k) })}
          description={$t('cfg.sec.empty_body')}
          plain
        />
      {:else}
        {#if k === 'xray' && apiUnavailable}
          <div
            class="alert alert-warning cores-api"
            role="status"
            data-testid="xray-api-unavailable"
          >
            <Icon name="warning" size={16} class="cores-api-icon" />
            <span>{$t('cfg.sec.api_unavailable', { min: apiMinVersion })}</span>
          </div>
        {/if}
        {#if k === 'mihomo' && view.profile}
          <MihomoProfileNotice profile={view.profile} stopped={!view.running} />
        {/if}
        {#if k === 'mihomo'}
          <ProfilesCard
            running={view.running}
            panelPrimary={mihomoNoticePrimary(view.profile?.owner)}
          />
        {/if}
        <div class="cores-sections" data-testid="cores-sections-{k}">
          {#each ordered(view) as sec (sec.id)}
            <SectionCard
              kernel={k}
              section={sec}
              snap={k === 'xray' ? $layerSnapshot?.cores.xray[sec.id] : undefined}
              issues={($layerSnapshot?.checks ?? []).filter((c) => c.key === `${k}_${sec.id}`)}
              forceProxy={view.force_proxy}
              routingTaken={routingTaken(k)}
              profileName={view.profile?.name ?? ''}
              bind:expanded={open[`${k}.${sec.id}`]}
              ontake={() => openTake(k, 'one', sec.id)}
            />
          {/each}
        </div>
      {/if}
    </div>
  {/each}
</div>

<TakeSectionModal
  bind:open={takeOpen}
  kernel={takeKernel}
  mode={takeMode}
  section={takeSection}
  ids={takeAllIds}
  forceGroup={takeView?.force_proxy?.group ?? ''}
  ontaken={taken}
/>

<style>
  .cores-tab {
    display: flex;
    flex-direction: column;
    gap: var(--grid-gap, 16px);
    min-width: 0;
  }

  .cores-bar {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--spacing-2) var(--spacing-4);
    min-width: 0;
  }

  .cores-state {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-1);
    min-width: 0;
    flex: 1 1 auto;
  }

  .cores-state-line {
    margin: 0;
    font-size: var(--font-size-base);
    color: var(--fg-secondary);
    overflow-wrap: anywhere;
  }

  .cores-all-panel {
    display: inline-flex;
    align-items: center;
    gap: var(--spacing-2);
    margin: 0;
    font-size: var(--font-size-base);
    color: var(--fg-secondary);
  }

  .cores-ok {
    display: inline-flex;
    flex-shrink: 0;
    color: var(--success);
  }

  @media (pointer: coarse), (max-width: 768px) {
    .cores-bar :global(.btn) {
      flex: 1 1 auto;
      min-height: 44px;
    }
  }

  @media (max-width: 480px) {
    .cores-bar :global(.btn) {
      flex: 1 1 100%;
    }
  }

  .cores-api {
    margin: 0;
    min-width: 0;
    line-height: 1.5;
    overflow-wrap: anywhere;
  }

  .cores-api :global(.cores-api-icon) {
    flex-shrink: 0;
    margin-top: 2px;
  }

  .cores-panel[hidden] {
    display: none;
  }

  .cores-panel:not([hidden]) {
    display: flex;
    flex-direction: column;
    gap: var(--grid-gap, 16px);
    min-width: 0;
  }

  .cores-skeleton,
  .cores-sections {
    display: flex;
    flex-direction: column;
    gap: var(--grid-gap, 16px);
    min-width: 0;
  }
</style>
