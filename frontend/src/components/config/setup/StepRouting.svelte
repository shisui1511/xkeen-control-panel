<script lang="ts" module>
  /** Ход шага 4: running — открыт диалог пресета. */
  export type RoutingPhase = 'idle' | 'running';
</script>

<script lang="ts">
  import { get } from 'svelte/store';
  import { t, currentLang } from '../../../i18n';
  import Button from '../../Button.svelte';
  import PresetModal from '../rules/PresetModal.svelte';
  import { ownerState } from '../routing/routingOwner';
  import { applyRunning } from '../../../lib/configLayer';
  import {
    WIZARD_STEP_IDS,
    loadWizard,
    wizardView,
    type WizardStep
  } from '../../../lib/configWizard';

  // Тело шага 4 «Маршрутизация» (D-02, D-04.4, 146 D-04): тот же диалог пресета, что в
  // разделе. Маршрутизация у XKeen (или ядер нет) — режим взятия: диалог создаёт правила и
  // берёт маршрутизацию в черновик; у панели или взятие ждёт «Применить» — режим пресета.
  // Окно изменений из мастера диалог сам не открывает: «Применить» — только в шаге 5.
  // Закрытие диалога без выбора шаг не закрывает — это условие сервера (A1).

  interface Props {
    step: WizardStep | undefined;
    /** Ход шага для статуса строки. */
    phase?: RoutingPhase;
  }

  let { step, phase = $bindable('idle') }: Props = $props();

  const ROUTING_N = WIZARD_STEP_IDS.indexOf('routing') + 1;

  let open = $state(false);
  let mode = $state<'take' | 'preset'>('preset');

  const isCurrent = $derived($wizardView?.current === ROUTING_N);
  const done = $derived(step?.done === true);
  const presetId = $derived(step?.preset ?? '');

  // Название страны выхода пресета «Всё, кроме страны» на текущем языке; при сбое — код
  function countryName(code: string): string {
    if (code === '') return '';
    try {
      return new Intl.DisplayNames([$currentLang, 'en'], { type: 'region' }).of(code) || code;
    } catch {
      return code;
    }
  }

  const presetName = $derived.by((): string => {
    switch (presetId) {
      case 'all_proxy':
        return $t('cfg.preset.all_proxy_title');
      case 'except_country':
        return $t('cfg.preset.except_title', {
          country: countryName((step?.country ?? '').toUpperCase())
        });
      case 'selective':
        return $t('cfg.preset.selective_title');
      case 'empty':
        return $t('cfg.preset.empty_title');
      default:
        return '';
    }
  });

  function openPreset() {
    if (open || $applyRunning) return;
    // Режим выбирается в момент открытия и не меняется, пока диалог открыт
    const kind = get(ownerState).kind;
    mode = kind === 'xkeen' || kind === 'none' ? 'take' : 'preset';
    open = true;
    phase = 'running';
  }

  function closePreset() {
    open = false;
    phase = 'idle';
    void loadWizard();
  }
</script>

<div class="step-routing">
  {#if done}
    <p class="step-routing-current" data-testid="setup-preset-name">
      {presetName !== ''
        ? $t('cfg.setup.routing.current', { name: presetName })
        : $t('cfg.setup.routing.own_rules')}
    </p>
  {/if}
  <div class="step-routing-actions">
    <Button
      variant={isCurrent && !done ? 'primary' : 'secondary'}
      size="sm"
      loading={open}
      disabled={open || $applyRunning}
      data-testid="setup-preset-open"
      onclick={openPreset}
    >
      {done ? $t('cfg.setup.routing.change') : $t('cfg.setup.routing.choose')}
    </Button>
  </div>
</div>

<PresetModal {open} {mode} scope="all" onclose={closePreset} />

<style>
  .step-routing {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-2);
    min-width: 0;
  }

  .step-routing-current {
    margin: 0;
    color: var(--fg-primary);
    line-height: 1.5;
    overflow-wrap: anywhere;
  }

  .step-routing-actions {
    display: flex;
    flex-wrap: wrap;
    gap: var(--spacing-2);
  }

  @media (max-width: 480px) {
    .step-routing-actions :global(.btn) {
      width: 100%;
    }
  }
</style>
