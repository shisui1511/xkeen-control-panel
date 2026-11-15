<script lang="ts">
  import { onDestroy, tick } from 'svelte';
  import { t } from '../../../i18n';
  import Button from '../../Button.svelte';
  import Icon from '../../../lib/components/Icon.svelte';
  import ApplyProgress, { applyResultText } from '../ApplyProgress.svelte';
  import ImmediateMark from '../ImmediateMark.svelte';
  import { applyGate, applyGateHint } from '../applyGate';
  import { conflictVisible } from '../../../stores';
  import {
    applyRunning,
    applyStartedHere,
    draftChanges,
    layerSnapshot,
    type LayerResult
  } from '../../../lib/configLayer';
  import { confirmKernelStop, stopKernelAndReport } from '../../../lib/serviceControl';
  import { kernelLabel, type KernelName } from '../../../lib/kernelState';
  import {
    WIZARD_STEP_IDS,
    loadWizard,
    wizardView,
    type WizardStep
  } from '../../../lib/configWizard';
  import { VALUE_MARK, valueParts } from './valueParts';

  // Тело шага 5 «Применение» (D-02, D-03, D-15, D-16, 149 D-23): счётчик изменений и
  // единственная на экране кнопка «Применить» — главная (primary), только пока шаг текущий.
  // Нажатие идёт только через onapply —
  // apply() голой полосы черновика: тот же перехват и то же окно изменений, что в разделе;
  // своего пути применения здесь нет. Причины блокировки — общий стор applyGate (как у
  // полосы). Конфликт ядер показывается по стору conflictVisible при любом состоянии
  // черновика: рядом с причиной — «Остановить Xray» и «Остановить Mihomo», ядро выбирает
  // пользователь (FND-05). Ход применения — ApplyProgress прямо в теле шага; сбой — общий
  // блок ошибки с «Повторить применение»: пока он виден, это единственная главная кнопка
  // шага, а «Применить» скрыта; черновик не сбрасывается (144 D-07). Повтор держит блок
  // и кнопку на месте до исхода (D-16): кнопка в ходе становится disabled, браузер снимает с
  // неё фокус — после исхода он возвращается на действие шага.

  interface Props {
    step: WizardStep | undefined;
    onapply: () => void;
  }

  let { step, onapply }: Props = $props();

  const KERNELS: KernelName[] = ['xray', 'mihomo'];
  const APPLY_N = WIZARD_STEP_IDS.indexOf('apply') + 1;

  const isCurrent = $derived($wizardView?.current === APPLY_N);

  // Итог применения показывается как сбой шага, только если применение шло при открытой
  // странице: свежая загрузка со старым неудачным итогом блока ошибки не рисует
  let seenRunning = $state(false);

  // Итог сбоя, из которого нажат повтор: начало нового запуска сбрасывает итог в снимке
  // (apply_step), и без удержания блок с кнопкой повтора исчез бы вместе с фокусом
  let heldFailure = $state<LayerResult | null>(null);
  let retryPressed = false;
  let root = $state<HTMLDivElement>();

  // Ход применения: идёт на сервере или запущено отсюда и ждёт ответа
  let running = false;
  let startedHere = false;
  let wasBusy = false;
  function syncBusy() {
    const busy = running || startedHere;
    if (busy) seenRunning = true;
    if (wasBusy && !busy) {
      // Исход пришёл: удержанный итог больше не нужен, фокус — на действие шага
      heldFailure = null;
      if (retryPressed) {
        retryPressed = false;
        void tick().then(restoreFocus);
      }
    }
    wasBusy = busy;
  }
  onDestroy(
    applyRunning.subscribe((r) => {
      running = r;
      syncBusy();
    })
  );
  onDestroy(
    applyStartedHere.subscribe((here) => {
      startedHere = here;
      syncBusy();
    })
  );

  // Фокус потерян — на «Повторить применение» (новый сбой), иначе на «Применить», если она
  // видна и активна; иначе не трогается
  function restoreFocus() {
    const active = document.activeElement;
    const lost = active === null || active === document.body || !active.isConnected;
    if (!root || !lost) return;
    for (const cls of ['step-apply-retry', 'step-apply-main']) {
      const btn = root.querySelector<HTMLButtonElement>(`button.${cls}`);
      if (btn && !btn.disabled) {
        btn.focus();
        return;
      }
    }
  }

  function retry() {
    heldFailure = shownFailure;
    retryPressed = true;
    onapply();
  }

  const conflict = $derived($conflictVisible);
  // Применять нечего: черновик чист и применённое состояние пустое
  const nothingToApply = $derived(!$applyGate.dirty && step?.applied_empty === true);
  // Причина блокировки полосы (без конфликта: у него свой блок выше)
  const blockedHint = $derived(
    !conflict && $applyGate.dirty && $applyGate.reason !== '' ? applyGateHint($applyGate, $t) : ''
  );

  const result = $derived($layerSnapshot?.apply.result ?? null);
  const failure = $derived(
    seenRunning && !$applyRunning && result !== null && !result.ok ? result : null
  );
  const shownFailure = $derived(
    failure ??
      (heldFailure !== null && ($applyRunning || $applyGate.applyingHere) ? heldFailure : null)
  );
  const failureReason = $derived.by(() => {
    if (!shownFailure) return '';
    const text =
      applyResultText(shownFailure, $t) ||
      (shownFailure.message ?? '').split('\n')[0] ||
      shownFailure.code;
    // Текст ошибки сам ставит точку после причины
    return text.trim().replace(/[.。]+$/, '');
  });

  // «Применить» и «Повторить применение» описывает одна и та же причина блокировки
  const describedBy = $derived(
    conflict ? 'setup-apply-conflict-hint' : blockedHint ? 'setup-apply-blocked-hint' : undefined
  );

  // «Изменений в черновике: N» — число моноширинным
  const countParts = $derived(valueParts($t('cfg.setup.apply.count', { n: VALUE_MARK })));

  let stopping = $state<KernelName | null>(null);

  // Остановка выбранного ядра идёт сразу, мимо черновика: существующие подтверждение и
  // тосты исхода, затем перечитывание статуса мастера
  async function stop(kernel: KernelName) {
    if (stopping !== null) return;
    if (!(await confirmKernelStop(kernel))) return;
    stopping = kernel;
    try {
      await stopKernelAndReport(kernel);
    } finally {
      stopping = null;
      void loadWizard();
    }
  }
</script>

<div class="step-apply" bind:this={root}>
  {#if conflict}
    <div class="step-apply-conflict">
      <p
        class="step-apply-blocked"
        id="setup-apply-conflict-hint"
        data-testid="setup-apply-blocked"
      >
        <span class="step-apply-icon" aria-hidden="true"><Icon name="warning" size={16} /></span>
        <span>{$t('cfg.setup.apply.conflict')}</span>
      </p>
      <div class="step-apply-actions">
        {#each KERNELS as k (k)}
          <Button
            variant="secondary"
            size="sm"
            data-testid="setup-stop-{k}"
            loading={stopping === k}
            disabled={stopping !== null && stopping !== k}
            onclick={() => stop(k)}
          >
            {$t('cfg.setup.apply.stop', { kernel: kernelLabel(k) })}
          </Button>
        {/each}
        <ImmediateMark />
      </div>
    </div>
  {/if}

  {#if nothingToApply && !conflict}
    <p class="step-apply-note">{$t('cfg.setup.apply.need_changes')}</p>
  {:else}
    <p class="step-apply-count" data-testid="setup-apply-count">
      {countParts[0]}<span class="setup-mono">{$draftChanges}</span>{countParts[1]}
    </p>
    <!-- При сбое единственный вход в применение — «Повторить применение» в блоке ошибки -->
    {#if !shownFailure}
      <div class="step-apply-actions">
        <Button
          variant={isCurrent ? 'primary' : 'secondary'}
          size="sm"
          class="step-apply-main"
          data-testid="setup-apply"
          disabled={conflict || !$applyGate.canApply}
          loading={$applyGate.applyingHere}
          ariaDescribedby={describedBy}
          onclick={() => onapply()}
        >
          {$t('cfg.apply')}
        </Button>
      </div>
    {/if}
    {#if blockedHint}
      <p class="step-apply-blocked" id="setup-apply-blocked-hint" data-testid="setup-apply-blocked">
        <span class="step-apply-icon" aria-hidden="true"><Icon name="warning" size={16} /></span>
        <span>{blockedHint}</span>
      </p>
    {/if}
  {/if}

  <ApplyProgress />

  {#if shownFailure}
    <div class="alert alert-error step-apply-error" role="alert" data-testid="setup-apply-error">
      <span class="step-apply-icon" aria-hidden="true"><Icon name="warning" size={16} /></span>
      <div class="step-apply-error-body">
        <span>{$t('cfg.setup.apply.failed', { reason: failureReason })}</span>
        <div class="step-apply-actions">
          <Button
            variant="primary"
            size="sm"
            class="step-apply-retry"
            data-testid="setup-apply-retry"
            disabled={conflict || !$applyGate.canApply}
            loading={$applyGate.applyingHere}
            ariaDescribedby={describedBy}
            onclick={retry}
          >
            {$t('cfg.setup.apply.retry')}
          </Button>
        </div>
      </div>
    </div>
  {/if}
</div>

<style>
  .step-apply,
  .step-apply-conflict {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-4);
    min-width: 0;
  }

  .step-apply-conflict {
    gap: var(--spacing-2);
  }

  .step-apply-count,
  .step-apply-note {
    margin: 0;
    line-height: 1.5;
    overflow-wrap: anywhere;
  }

  .step-apply-count {
    color: var(--fg-primary);
  }

  .step-apply-note {
    color: var(--fg-secondary);
  }

  .step-apply-actions {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--spacing-2);
    min-width: 0;
  }

  .step-apply-blocked {
    display: flex;
    align-items: flex-start;
    gap: var(--spacing-2);
    margin: 0;
    min-width: 0;
    color: var(--warning);
    font-size: var(--font-size-xs);
    font-weight: 600;
    line-height: 1.5;
    overflow-wrap: anywhere;
  }

  .step-apply-icon {
    display: inline-flex;
    flex-shrink: 0;
    align-items: center;
    height: 1lh;
  }

  .step-apply-error {
    margin-bottom: 0;
  }

  .step-apply-error-body {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-2);
    min-width: 0;
    color: var(--fg-primary);
    font-size: var(--font-size-base);
    line-height: 1.5;
    overflow-wrap: anywhere;
  }

  @media (max-width: 480px) {
    .step-apply-actions :global(.btn) {
      width: 100%;
    }
  }
</style>
