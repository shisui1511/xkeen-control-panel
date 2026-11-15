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
  import StepKernel, { type KernelPhase } from './StepKernel.svelte';
  import StepNodes from './StepNodes.svelte';
  import StepRouting, { type RoutingPhase } from './StepRouting.svelte';
  import StepSections, { takeRest, type SectionsPhase } from './StepSections.svelte';
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

  // Проверка IP сама (D-09): в момент, когда по ответу сервера выполнены шаги 5 и 6. Уже
  // на первом ответе — сразу (ядро давно запущено); позже, по переходу, — после паузы на
  // старт ядра, у Mihomo — на прогрев API (как окно прогрева после переключения ядра).
  // Шаги снова не выполнены (сбой или остановка ядра, новые правки) — ожидание снимается,
  // флаг и итог сбрасываются, и после нового выполнения проверка идёт снова. В остальном —
  // только «Проверить снова». Тела шага 7 к сроку может не быть (список шагов ещё не
  // отрисован): тогда проверка откладывается до его монтирования
  const IP_CHECK_DELAY_MS = 3000;
  const IP_CHECK_DELAY_MIHOMO_MS = 6000;
  let ipAutoDone = false;
  let ipTimer: ReturnType<typeof setTimeout> | null = null;
  let ipBox = $state<IpCheckResult | undefined>();
  let ipPhase = $state<IpPhase>('idle');
  // Автопроверка назначена, но тела шага 7 ещё нет: её запускает монтирование тела
  let ipPending = false;

  function runIpCheck() {
    if (ipBox) ipBox.check();
    else ipPending = true;
  }

  function bindIpBox(box: IpCheckResult | undefined) {
    ipBox = box;
    if (!box || !ipPending) return;
    ipPending = false;
    // Проверка — после монтирования: тело уже получило ready
    queueMicrotask(() => box.check());
  }

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
        // Ядро остановлено или черновик снова не применён: прежний итог устарел, после
        // нового выполнения шагов 5 и 6 проверка пройдёт заново сама
        cancelIpTimer();
        ipPending = false;
        ipAutoDone = false;
        ipPhase = 'idle';
        return;
      }
      if (ipAutoDone) return;
      ipAutoDone = true;
      if (first) {
        // Тело шага 7 ещё может монтироваться: проверка — после обновления разметки, а
        // если тела нет и тогда — при его монтировании
        void tick().then(runIpCheck);
        return;
      }
      const kernel = startedKernel || $activeKernelName;
      const delay = kernel === 'mihomo' ? IP_CHECK_DELAY_MIHOMO_MS : IP_CHECK_DELAY_MS;
      ipTimer = setTimeout(() => {
        ipTimer = null;
        runIpCheck();
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

  // Экран ошибки — только когда показать нечего: снимка слоя нет или вид мастера не
  // прочитан. Обрыв SSE шаги не снимает (открытые диалоги и итоги шагов остаются):
  // предупреждение над списком, «Применить» запирает applyGate
  const failed = $derived($layerStatus === 'error' || ($wizardLoadError && $wizardView === null));
  const lost = $derived($layerConnection === 'lost');

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

  // Ход шага 2 (StepSections): грузится вид секций или открыт диалог взятия. Экземпляр
  // нужен «Взять остальные» в строке шага — она открывает тот же диалог, что тело шага
  let sectionsPhase = $state<SectionsPhase>('idle');
  let sectionsBox = $state<StepSections | undefined>();

  // Тела, которые держатся смонтированными и в свёрнутом шаге: диалоги шагов 2 и 4 не
  // снимаются автопереходом посреди выбора, ход и итоги шагов 5–7 не теряются
  const KEEP_MOUNTED = ['nodes', 'sections', 'routing', 'apply', 'start', 'ip'];

  // Ход шага 3 (StepNodes): в поле «Добавить» есть ввод или идёт добавление. Без него
  // автопереход по готовому первому узлу свернул бы поле посреди вставки второй ссылки.
  // В отличие от диалогов шагов 1, 2 и 4 ввод может висеть долго, поэтому он держит только
  // автопереход: свернуть шаг или раскрыть другой пользователь может сам
  const NODES_N = WIZARD_STEP_IDS.indexOf('nodes') + 1;
  let nodesBox = $state<StepNodes | undefined>();
  const nodesBusy = $derived(nodesBox?.isBusy() ?? false);

  // Ход шага 4 (StepRouting): открыт диалог пресета
  let routingPhase = $state<RoutingPhase>('idle');

  // Ход шага 1 (StepKernel): идёт установка ядра или открыто окно установки XKeen
  let kernelPhase = $state<KernelPhase>('idle');

  // Шаг, в теле которого идёт действие (установка, открытый диалог взятия или пресета),
  // держится раскрытым: окна диалогов живут в теле шага, и автопереход по ответу сервера
  // не должен их прятать. «Взять остальные» в свёрнутой строке шага 2 раскрывает шаг тем же
  // правилом
  const busyN = $derived(
    kernelPhase === 'running'
      ? WIZARD_STEP_IDS.indexOf('kernel') + 1
      : sectionsPhase === 'running'
        ? WIZARD_STEP_IDS.indexOf('sections') + 1
        : routingPhase === 'running'
          ? WIZARD_STEP_IDS.indexOf('routing') + 1
          : null
  );

  // Ход запуска ядра в шаге 6 (StepStart) и ядро, запущенное кнопкой мастера
  let startPhase = $state<StartPhase>('idle');
  let startedKernel: KernelName | '' = '';

  function onKernelStarted(kernel: KernelName) {
    startedKernel = kernel;
  }

  // Ход действий поверх статуса сервера: шаг 5 — «Идёт: применение», пока идёт применение,
  // и «Заблокирован» при конфликте ядер при любом состоянии черновика (D-15); шаг 6 —
  // «Идёт: запуск ядра» на время запроса запуска, «Ошибка» при сбое запуска и «Заблокирован»
  // при конфликте по ответу сервера (отказ kernel_conflict сбоем не считается)
  const rows = $derived(
    baseRows.map((r): Row => {
      if (r.id === 'kernel' && kernelPhase === 'running') return { ...r, status: 'running' };
      if (r.id === 'sections' && sectionsPhase === 'running') return { ...r, status: 'running' };
      if (r.id === 'routing' && routingPhase === 'running') return { ...r, status: 'running' };
      if (r.id === 'apply') {
        if ($applyRunning) return { ...r, status: 'running' };
        if ($conflictVisible) return { ...r, status: 'blocked' };
      }
      if (r.id === 'start') {
        if (startPhase === 'running') return { ...r, status: 'running' };
        if ($wizardView?.kernels.conflict) return { ...r, status: 'blocked' };
        if (startPhase === 'error' && r.status !== 'done') return { ...r, status: 'error' };
      }
      // Шаг 7: заперт до выполнения шагов 5 и 6, «Идёт: проверка IP» во время проверки.
      // Выполнен только при итоге через VPN или ожидаемом прямом трафике (выборочная
      // маршрутизация); прямой трафик при пресете «всё через прокси» и сбой сервиса IP —
      // маркер предупреждения со словом (D-07, D-08). Итог на сервер не уходит и в прогресс
      // не входит (D-05)
      if (r.id === 'ip' && r.status !== 'locked') {
        if (ipPhase === 'loading') return { ...r, status: 'running' };
        if (ipPhase === 'proxied' || ipPhase === 'direct_ok') return { ...r, status: 'done' };
        if (ipPhase === 'direct_warn' || ipPhase === 'failed') return { ...r, status: 'warning' };
      }
      return r;
    })
  );

  // Шаги 1–6 выполнены: «Готово» — главная кнопка; до этого — «Продолжить позже»
  const allDone = $derived($wizardView !== null && $wizardView.current === 0);

  // Объявление начала действий в шагах 1, 5 и 6: «Идёт: установка / применение / запуск
  // ядра». Шаги 2 и 4 открывают модаль, она объявляет себя сама; у шага 7 своя живая
  // область (IpCheckResult)
  const LIVE_RUNNING = ['kernel', 'apply', 'start'];
  const runningLive = $derived(
    rows
      .filter((r) => r.status === 'running' && LIVE_RUNNING.includes(r.id))
      .map((r) => statusText(r))
      .join('. ')
  );

  // Номер раскрытого по умолчанию шага: текущий (первый невыполненный), а когда шаги 1–6
  // выполнены — проверка IP. Считается по статусу сервера: ход действий его не сдвигает
  const currentN = $derived(baseRows.find((r) => r.status === 'current')?.n ?? null);

  // Выбор пользователя: какой шаг раскрыт (null — все свёрнуты) и какой шаг был текущим в
  // момент выбора. Раскрыт текущий — выбор следует за ним (автопереход); раскрыт другой
  // шаг или всё свёрнуто — выбор держится
  let manual = $state<{ n: number | null; at: number | null } | null>(null);
  // Ввод в шаге 3 держит его раскрытым, пока пользователь сам не выбрал другой шаг и пока
  // шаг 5 не раскрыт сам (началось применение или конфликт ядер)
  const holdNodes = $derived(
    nodesBusy &&
      (manual === null || manual.n === NODES_N || (manual.n === manual.at && manual.n !== APPLY_N))
  );
  const openN = $derived(
    busyN ?? (holdNodes ? NODES_N : manual !== null && manual.n !== manual.at ? manual.n : currentN)
  );

  function setOpen(n: number, open: boolean) {
    // При вводе в шаге 3 раскрытый другой шаг держится явно (at: null): «следовать за
    // текущим» иначе вернуло бы шаг 3
    const explicit = open && nodesBusy && n !== NODES_N;
    manual = { n: open ? n : null, at: explicit ? null : currentN };
  }

  function statusText(r: Row): string {
    if (r.status === 'done' || r.status === 'partial') return $t('cfg.setup.status.done');
    if (r.status === 'running') return $t(`cfg.setup.status.running_${r.id}`);
    if (r.status === 'warning') {
      return ipPhase === 'failed'
        ? $t('cfg.setup.status.check_failed')
        : $t('cfg.setup.status.warning');
    }
    if (r.status === 'error') return $t('cfg.setup.status.error');
    if (r.status === 'blocked') return $t('cfg.setup.status.blocked');
    if (r.status === 'locked') return $t('cfg.setup.status.locked');
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

  // Высота строки скелетона — высота свёрнутой строки шага, без скачка при загрузке: поля
  // строки, заголовок 14×1.3, зазор, статус 12×1.5 и рамка 2×1 px
  const SKELETON_ROW_H =
    'calc(2 * var(--spacing-4) + var(--spacing-1) + 1.3 * var(--font-size-base) + 1.5 * var(--font-size-xs) + 2px)';
</script>

<div class="setup-page" data-testid="config-setup">
  <PageHeader title={$t('cfg.setup.title')} subtitle={$t('cfg.setup.subtitle')}>
    {#snippet actions()}
      <Button variant="ghost" size="sm" loading={closing} data-testid="setup-close" onclick={close}>
        <Icon name="cross" size={16} />
        {$t('cfg.setup.close')}
      </Button>
    {/snippet}
  </PageHeader>

  <p class="sr-only" aria-live="polite" data-testid="setup-live">{liveText}</p>
  <p class="sr-only" aria-live="polite" data-testid="setup-live-running">{runningLive}</p>

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
        <Skeleton type="rect" height={SKELETON_ROW_H} />
      {/each}
    </div>
  {:else}
    {#if lost}
      <!-- Тот же вид, что у блоков шага 7: значок в тоне блока, текст 14 px основным цветом -->
      <div class="alert alert-warning setup-lost" role="status" data-testid="setup-lost">
        <span class="setup-lost-icon" aria-hidden="true"><Icon name="warning" size={16} /></span>
        <span class="setup-lost-body">{$t('cfg.sse_lost')}</span>
      </div>
    {/if}
    <ol class="setup-steps" data-testid="setup-steps" aria-label={$t('cfg.setup.steps_aria')}>
      {#each rows as r (r.n)}
        <SetupStep
          n={r.n}
          id={r.id}
          status={r.status}
          title={$t(`cfg.setup.step.${r.id}.title`)}
          statusText={statusText(r)}
          keepMounted={KEEP_MOUNTED.includes(r.id)}
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
              {@render takeRest(
                () => void sectionsBox?.openXray(),
                sectionsPhase === 'running' || $applyRunning,
                $t('cfg.setup.sections.take_rest')
              )}
            {/if}
          {/snippet}
          <p class="setup-step-hint">{bodyText(r)}</p>
          {#if r.id === 'kernel' && $wizardView}
            <StepKernel
              bind:phase={kernelPhase}
              step={stepOf('kernel')}
              kernels={$wizardView.kernels}
            />
          {:else if r.id === 'sections' && $wizardView}
            <StepSections
              bind:this={sectionsBox}
              bind:phase={sectionsPhase}
              step={stepOf('sections')}
              kernels={$wizardView.kernels}
            />
          {:else if r.id === 'nodes'}
            <StepNodes bind:this={nodesBox} step={stepOf('nodes')} />
          {:else if r.id === 'routing'}
            <StepRouting step={stepOf('routing')} bind:phase={routingPhase} />
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
              bind:this={() => ipBox, bindIpBox}
              bind:phase={ipPhase}
              ready={r.status !== 'locked'}
              presetId={stepOf('routing')?.preset ?? ''}
              lockReason={stepOf('apply')?.done === true ? 'start' : 'apply'}
              {allDone}
            />
          {/if}
        </SetupStep>
      {/each}
    </ol>

    <div class="setup-footer">
      <!-- Одна главная кнопка на экране: раскрытый шаг 3 несёт свою «Добавить» -->
      <Button
        variant={allDone && openN !== NODES_N ? 'primary' : 'secondary'}
        size="sm"
        data-testid="setup-finish"
        onclick={() => (window.location.hash = '#/dashboard')}
      >
        {allDone ? $t('cfg.setup.finish') : $t('cfg.setup.finish_later')}
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

  /* Шапку, список шагов и «Готово» разделяет только зазор страницы */
  .setup-page :global(.page-header) {
    margin-bottom: 0;
  }

  /* Цель касания 44 px: все кнопки .btn мастера — тела шагов, «Взять остальные» в строке,
     «Закрыть», «Готово», встроенные компоненты и ссылки с классом .btn — в любой плотности.
     Высоту даёт токен --btn-h, как в ConfirmDialog; селекторов .btn здесь нет */
  @media (max-width: 768px) {
    .setup-page {
      --btn-h: 44px;
    }
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

  .setup-lost {
    margin: 0;
  }

  .setup-lost-icon {
    display: inline-flex;
    flex-shrink: 0;
    align-items: center;
    height: 1lh;
  }

  .setup-lost-body {
    min-width: 0;
    color: var(--fg-primary);
    font-size: var(--font-size-base);
    line-height: 1.5;
    overflow-wrap: anywhere;
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
