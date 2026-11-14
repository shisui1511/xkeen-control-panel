<script lang="ts">
  import { onMount, tick } from 'svelte';
  import { t } from '../../../i18n';
  import PageHeader from '../../../PageHeader.svelte';
  import Button from '../../Button.svelte';
  import Badge from '../../Badge.svelte';
  import EmptyState from '../../EmptyState.svelte';
  import Skeleton from '../../Skeleton.svelte';
  import Icon from '../../../lib/components/Icon.svelte';
  import SetupStep, { type SetupStepStatus } from './SetupStep.svelte';
  import StepNodes from './StepNodes.svelte';
  import StepApply from './StepApply.svelte';
  import StepStart, { type StartPhase } from './StepStart.svelte';
  import IpCheckResult, { type IpPhase } from './IpCheckResult.svelte';
  import type { KernelName } from '../../../lib/kernelState';
  import { activeKernelName, conflictVisible, showToast } from '../../../stores';
  import {
    applyRunning,
    layerStatus,
    layerConnection,
    refetchLayerState
  } from '../../../lib/configLayer';
  import {
    WIZARD_STEP_IDS,
    closeWizard,
    loadWizard,
    startWizard,
    stopWizard,
    wizardLoadError,
    wizardView,
    type WizardPartial,
    type WizardStep,
    type WizardStepId
  } from '../../../lib/configWizard';

  // Страница мастера первого запуска (#/config/setup, D-01…D-03, D-10): шапка с «Закрыть»,
  // семь шагов списком и «Готово». Статусы шагов 1–6 считает сервер (GET
  // /api/configlayer/wizard), седьмой шаг (проверка IP) клиентский: он заперт, пока не
  // выполнены «Применение» и «Запуск ядра» (D-09). Вкладок раздела, полосы черновика и
  // общих уведомлений здесь нет (D-03). Шаги — аккордеон: раскрыт один шаг; при загрузке —
  // первый невыполненный, а когда он выполнен — следующий, если пользователь не раскрыл
  // другой шаг сам.

  interface Row {
    n: number;
    id: string;
    status: SetupStepStatus;
    partial?: WizardPartial;
  }

  // Живая область: «Шаг N выполнен», когда шаг стал выполненным при открытой странице.
  // Фокус при этом не уводится
  let liveText = $state('');

  interface Props {
    /**
     * «Применить» шага 5: Config.svelte передаёт apply() голой полосы DraftBar — тот же путь
     * через перехват и окно изменений, что в разделе (149 D-23, D-03). Его нажимает только
     * кнопка тела шага 5 (StepApply).
     */
    onapply?: () => void;
  }

  let { onapply = () => {} }: Props = $props();

  // Шаг 5 раскрывается сам с началом применения (ход виден в его теле, B5) и при конфликте
  // ядер (там причина и кнопки остановки, D-15) — даже если текущим по серверу стал шаг 6.
  // Раскрытие по конфликту снимается вместе с конфликтом: дальше — прежнее правило
  // (раскрыт текущий шаг)
  const APPLY_N = WIZARD_STEP_IDS.indexOf('apply') + 1;
  let openedByConflict = false;

  function forceOpenApply() {
    manual = { n: APPLY_N, at: currentN };
  }

  // Проверка IP сама — один раз за жизнь страницы мастера (D-09): в первый момент, когда
  // по ответу сервера выполнены шаги 5 и 6. Уже на первом ответе — сразу (ядро давно
  // запущено); позже, по переходу, — после паузы на старт ядра, у Mihomo — на прогрев API
  // (как окно прогрева после переключения ядра). Если до срабатывания шаги снова не
  // выполнены (сбой ядра), ожидание снимается и флаг возвращается. Дальше — только
  // «Проверить снова»
  const IP_CHECK_DELAY_MS = 3000;
  const IP_CHECK_DELAY_MIHOMO_MS = 6000;
  let ipAutoDone = false;
  let ipTimer: ReturnType<typeof setTimeout> | null = null;
  let ipBox = $state<IpCheckResult | undefined>();
  let ipPhase = $state<IpPhase>('idle');

  function ipReadyOf(steps: WizardStep[]): boolean {
    const done = (id: WizardStepId) => steps.find((s) => s.id === id)?.done === true;
    return done('apply') && done('start');
  }

  function cancelIpTimer(): boolean {
    if (ipTimer === null) return false;
    clearTimeout(ipTimer);
    ipTimer = null;
    return true;
  }

  onMount(() => {
    startWizard();
    let firstView = true;
    const offIp = wizardView.subscribe((v) => {
      if (!v) return;
      const first = firstView;
      firstView = false;
      if (!ipReadyOf(v.steps)) {
        if (cancelIpTimer()) ipAutoDone = false;
        return;
      }
      if (ipAutoDone) return;
      ipAutoDone = true;
      if (first) {
        // Тело шага 7 ещё может монтироваться: проверка — после обновления разметки
        void tick().then(() => ipBox?.check());
        return;
      }
      const kernel = startedKernel || $activeKernelName;
      const delay = kernel === 'mihomo' ? IP_CHECK_DELAY_MIHOMO_MS : IP_CHECK_DELAY_MS;
      ipTimer = setTimeout(() => {
        ipTimer = null;
        ipBox?.check();
      }, delay);
    });
    let wasRunning = false;
    const offRun = applyRunning.subscribe((r) => {
      if (r && !wasRunning) forceOpenApply();
      wasRunning = r;
    });
    let wasConflict = false;
    const offConflict = conflictVisible.subscribe((c) => {
      if (c && !wasConflict) {
        openedByConflict = true;
        forceOpenApply();
      } else if (!c && wasConflict && openedByConflict) {
        openedByConflict = false;
        if (manual?.n === APPLY_N) manual = null;
      }
      wasConflict = c;
    });
    let prevDone: Set<string> | null = null;
    const off = wizardView.subscribe((v) => {
      if (!v) {
        prevDone = null;
        return;
      }
      const done = v.steps.filter((s) => s.done).map((s) => s.id as string);
      const before = prevDone;
      prevDone = new Set(done);
      if (before === null) return;
      const fresh = done.find((id) => !before.has(id));
      if (fresh === undefined) return;
      const n = WIZARD_STEP_IDS.indexOf(fresh as (typeof WIZARD_STEP_IDS)[number]) + 1;
      liveText = $t('cfg.setup.step_done_live', { n: String(n) });
    });
    return () => {
      off();
      offIp();
      cancelIpTimer();
      offRun();
      offConflict();
      stopWizard();
    };
  });

  const failed = $derived(
    $layerStatus === 'error' || $layerConnection === 'lost' || $wizardLoadError
  );

  // Шаг мастера из ответа сервера по id
  function stepOf(id: WizardStepId): WizardStep | undefined {
    return $wizardView?.steps.find((s) => s.id === id);
  }

  // Статусы шагов по серверу (до наложения хода действий в телах шагов)
  const baseRows = $derived.by((): Row[] => {
    const v = $wizardView;
    if (!v) return [];
    const byId = new Map(v.steps.map((s) => [s.id, s]));
    const out: Row[] = WIZARD_STEP_IDS.map((id, i) => {
      const n = i + 1;
      const step = byId.get(id);
      // Шаг 2 при частичном взятии: выполнен, но с пометкой «частично, N из M» (D-04.2)
      const partial =
        step?.partial && step.partial.done < step.partial.total ? step.partial : undefined;
      let status: SetupStepStatus = 'pending';
      if (step?.done) status = partial ? 'partial' : 'done';
      else if (v.current === n) status = 'current';
      return { n, id, status, partial };
    });
    // Шаг 7 открывается, когда применение завершено и ядро запущено (D-09)
    const ipOpen = byId.get('apply')?.done === true && byId.get('start')?.done === true;
    out.push({
      n: 7,
      id: 'ip',
      status: !ipOpen ? 'locked' : v.current === 0 ? 'current' : 'pending'
    });
    return out;
  });

  // Ход запуска ядра в шаге 6 (StepStart) и ядро, запущенное кнопкой мастера
  let startPhase = $state<StartPhase>('idle');
  let startedKernel: KernelName | '' = '';

  function onKernelStarted(kernel: KernelName) {
    startedKernel = kernel;
  }

  // Ход действий поверх статуса сервера: шаг 5 — «Идёт…», пока идёт применение, и
  // «Заблокирован» при конфликте ядер при любом состоянии черновика (D-15); шаг 6 —
  // «Идёт…» на время запроса запуска, «Ошибка» при сбое запуска и «Заблокирован» при
  // конфликте по ответу сервера (отказ kernel_conflict сбоем не считается)
  const rows = $derived(
    baseRows.map((r): Row => {
      if (r.id === 'apply') {
        if ($applyRunning) return { ...r, status: 'running' };
        if ($conflictVisible) return { ...r, status: 'blocked' };
      }
      if (r.id === 'start') {
        if (startPhase === 'running') return { ...r, status: 'running' };
        if ($wizardView?.kernels.conflict) return { ...r, status: 'blocked' };
        if (startPhase === 'error' && r.status !== 'done') return { ...r, status: 'error' };
      }
      // Шаг 7: заперт до выполнения шагов 5 и 6, «Идёт…» во время проверки, выполнен после
      // любого итога (итог на сервер не уходит и в прогресс не входит, D-05)
      if (r.id === 'ip' && r.status !== 'locked') {
        if (ipPhase === 'loading') return { ...r, status: 'running' };
        if (ipPhase !== 'idle') return { ...r, status: 'done' };
      }
      return r;
    })
  );

  // Шаги 1–6 выполнены: «Готово» — главная кнопка
  const allDone = $derived($wizardView !== null && $wizardView.current === 0);

  // Номер раскрытого по умолчанию шага: текущий (первый невыполненный), а когда шаги 1–6
  // выполнены — проверка IP. Считается по статусу сервера: ход действий его не сдвигает
  const currentN = $derived(baseRows.find((r) => r.status === 'current')?.n ?? null);

  // Выбор пользователя: какой шаг раскрыт (null — все свёрнуты) и какой шаг был текущим в
  // момент выбора. Раскрыт текущий — выбор следует за ним (автопереход); раскрыт другой
  // шаг или всё свёрнуто — выбор держится
  let manual = $state<{ n: number | null; at: number | null } | null>(null);
  const openN = $derived(manual !== null && manual.n !== manual.at ? manual.n : currentN);

  function setOpen(n: number, open: boolean) {
    manual = { n: open ? n : null, at: currentN };
  }

  function statusText(r: Row): string {
    if (r.status === 'done' || r.status === 'partial') return $t('cfg.setup.status.done');
    if (r.status === 'running') return $t('cfg.setup.status.running');
    if (r.status === 'error') return $t('cfg.setup.status.error');
    if (r.status === 'blocked') return $t('cfg.setup.status.blocked');
    return $t('cfg.setup.status.pending');
  }

  // Пояснение шага; у запертого шага 7 причину ожидания показывает его тело (IpCheckResult)
  function bodyText(r: Row): string {
    return $t(`cfg.setup.step.${r.id}.hint`);
  }

  let closing = $state(false);

  // «Закрыть» отменяет автооткрытие (D-10) и уводит в «Узлы» даже при сбое запроса;
  // черновик не трогается
  async function close() {
    closing = true;
    try {
      await closeWizard();
    } catch (err) {
      const reason = err instanceof Error ? err.message : String(err);
      showToast('warning', $t('cfg.setup.close_failed', { reason }));
    } finally {
      closing = false;
    }
    window.location.hash = '#/config/nodes';
  }

  function retry() {
    void refetchLayerState();
    void loadWizard();
  }
</script>

<div class="setup-page" data-testid="config-setup">
  <PageHeader title={$t('cfg.setup.title')} subtitle={$t('cfg.setup.subtitle')}>
    {#snippet actions()}
      <Button variant="ghost" loading={closing} data-testid="setup-close" onclick={close}>
        <Icon name="cross" size={16} />
        {$t('cfg.setup.close')}
      </Button>
    {/snippet}
  </PageHeader>

  <p class="sr-only" aria-live="polite" data-testid="setup-live">{liveText}</p>

  {#if failed}
    <EmptyState
      title={$t('cfg.load_error_title')}
      description={$t('cfg.load_error_body')}
      ctaText={$t('cfg.load_error_retry')}
      plain
      oncta={retry}
    />
  {:else if $wizardView === null}
    <div class="setup-skeleton" aria-busy="true">
      {#each [1, 2, 3, 4, 5, 6, 7] as n (n)}
        <Skeleton type="rect" height="56px" />
      {/each}
    </div>
  {:else}
    <ol class="setup-steps" data-testid="setup-steps" aria-label={$t('cfg.setup.steps_aria')}>
      {#each rows as r (r.n)}
        <SetupStep
          n={r.n}
          id={r.id}
          status={r.status}
          title={$t(`cfg.setup.step.${r.id}.title`)}
          statusText={statusText(r)}
          keepMounted={r.id === 'apply' || r.id === 'start' || r.id === 'ip'}
          bind:expanded={() => openN === r.n, (v) => setOpen(r.n, v)}
        >
          {#snippet aside()}
            {#if r.partial}
              <Badge tone="warning">
                {$t('cfg.setup.status.partial', {
                  done: String(r.partial.done),
                  total: String(r.partial.total)
                })}
              </Badge>
            {/if}
          {/snippet}
          <p class="setup-step-hint">{bodyText(r)}</p>
          {#if r.id === 'nodes'}
            <StepNodes step={stepOf('nodes')} />
          {:else if r.id === 'apply'}
            <StepApply step={stepOf('apply')} {onapply} />
          {:else if r.id === 'start' && $wizardView}
            <StepStart
              step={stepOf('start')}
              kernels={$wizardView.kernels}
              onstarted={onKernelStarted}
              bind:phase={startPhase}
            />
          {:else if r.id === 'ip'}
            <IpCheckResult
              bind:this={ipBox}
              bind:phase={ipPhase}
              ready={r.status !== 'locked'}
              presetId={stepOf('routing')?.preset ?? ''}
            />
          {/if}
        </SetupStep>
      {/each}
    </ol>

    <div class="setup-footer">
      <Button
        variant={allDone ? 'primary' : 'secondary'}
        data-testid="setup-finish"
        onclick={() => (window.location.hash = '#/dashboard')}
      >
        {$t('cfg.setup.finish')}
      </Button>
    </div>
  {/if}
</div>

<style>
  .setup-page {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-4);
    min-width: 0;
  }

  @media (min-width: 1024px) {
    .setup-page {
      max-width: 720px;
    }
  }

  .setup-skeleton,
  .setup-steps {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-2);
  }

  .setup-steps {
    list-style: none;
    margin: 0;
    padding: 0;
  }

  .setup-step-hint {
    margin: 0;
    color: var(--fg-secondary);
    line-height: 1.5;
    overflow-wrap: anywhere;
  }

  .setup-footer {
    display: flex;
    flex-wrap: wrap;
    gap: var(--spacing-2);
  }

  @media (max-width: 480px) {
    .setup-footer :global(.btn) {
      width: 100%;
    }
  }
</style>
