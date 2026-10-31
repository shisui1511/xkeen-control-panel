<script lang="ts">
  import { onMount } from 'svelte';
  import { t } from '../../../i18n';
  import { activeKernelName } from '../../../stores';
  import EmptyState from '../../EmptyState.svelte';
  import Skeleton from '../../Skeleton.svelte';
  import {
    fetchCores,
    onCoresChanged,
    type CoreKernel,
    type KernelView
  } from '../../../lib/configCores';
  import KernelSwitch from './KernelSwitch.svelte';

  // Вкладка «Xray │ Mihomo»: каркас — переключатель ядра, строка состояния, загрузка и
  // пустое состояние. Оба вида ядер загружаются при открытии и остаются смонтированными
  // (U-03): переключение не перезагружает данные и не сбрасывает правки. Вкладка ничего
  // не запускает и не меняет, только читает. Карточки секций добавляют следующие планы.
  const KERNELS: readonly CoreKernel[] = ['xray', 'mihomo'];

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

  function kernelTitle(k: CoreKernel): string {
    return k === 'xray' ? 'Xray' : 'Mihomo';
  }
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
        <div class="cores-sections" data-testid="cores-sections-{k}"></div>
      {/if}
    </div>
  {/each}
</div>

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

  .cores-panel[hidden] {
    display: none;
  }

  .cores-skeleton,
  .cores-sections {
    display: flex;
    flex-direction: column;
    gap: var(--grid-gap, 16px);
    min-width: 0;
  }
</style>
