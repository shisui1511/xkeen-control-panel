<script lang="ts" module>
  import Button from '../../Button.svelte';

  /** Ход шага 2: running — грузится вид секций или открыт диалог взятия. */
  export type SectionsPhase = 'idle' | 'running';

  export { takeRest };
</script>

<script lang="ts">
  import { get } from 'svelte/store';
  import { t } from '../../../i18n';
  import TakeSectionModal from '../sections/TakeSectionModal.svelte';
  import TakeRoutingModal from '../routing/TakeRoutingModal.svelte';
  import { fetchCores } from '../../../lib/configCores';
  import { applyRunning, layerSnapshot, refetchLayerState } from '../../../lib/configLayer';
  import {
    WIZARD_STEP_IDS,
    loadWizard,
    wizardView,
    type WizardKernels,
    type WizardStep
  } from '../../../lib/configWizard';
  import { showToast } from '../../../stores';

  // Тело шага 2 «Секции ядер» (D-02, 149 D-23 шаг 2, 147 D-03, B3): только пояснение и
  // кнопки, взятие — теми же диалогами, что в разделе, и только подтверждением в диалоге
  // (147 D-01). Xray — диалог взятия всех секций ядра; Mihomo — диалог взятия профиля
  // панели (секции Mihomo без профиля не берутся). При пустых правилах диалог Mihomo сам
  // ведёт в пресет: шаги 2 и 4 закрываются одним проходом.
  //
  // Состав диалога Xray — секции не в черновике: признак desired снимка слоя (на сервере
  // тот же DesiredCores(Draft).Managed, по которому считаются шаг и «частично, N из M»).
  // Владелец секции для отбора не годится: drift бывает и у секции в черновике, и у
  // отпущенной. Отпущенные в черновике секции входят — сервер берёт их снова с прежним
  // значением панели, а диалог показывает это строкой. Так диалог не бывает пустым, пока
  // шаг для Xray не выполнен, а «Взять остальные» берёт ровно секции, вошедшие в M.

  interface Props {
    step: WizardStep | undefined;
    kernels: WizardKernels;
    /** Ход шага для статуса строки. */
    phase?: SectionsPhase;
  }

  let { step, kernels, phase = $bindable('idle') }: Props = $props();

  const SECTIONS_N = WIZARD_STEP_IDS.indexOf('sections') + 1;

  const both = $derived(kernels.xray && kernels.mihomo);
  const showXray = $derived(kernels.xray && step?.kernels?.xray?.done !== true);
  const showMihomo = $derived(kernels.mihomo && step?.kernels?.mihomo?.done !== true);
  // Главная кнопка — только у текущего шага и только первая из кнопок шага
  const isCurrent = $derived($wizardView?.current === SECTIONS_N);
  const busy = $derived(phase === 'running' || $applyRunning);

  let loading = $state(false);
  let xrayOpen = $state(false);
  let xrayIds = $state<string[]>([]);
  let forceGroup = $state('');
  let mihomoOpen = $state(false);

  function syncPhase() {
    phase = loading || xrayOpen || mihomoOpen ? 'running' : 'idle';
  }

  /** Открывает диалог взятия секций Xray: вид секций и свежий снимок слоя читаются вместе. */
  export async function openXray(): Promise<void> {
    if (busy) return;
    loading = true;
    syncPhase();
    try {
      const [view] = await Promise.all([fetchCores('xray'), refetchLayerState()]);
      const snap = get(layerSnapshot)?.cores.xray ?? {};
      const ids = view.sections.filter((s) => snap[s.id]?.desired !== true).map((s) => s.id);
      if (ids.length === 0) {
        // Вид устарел: всё уже в черновике — шаг перечитывается, пустой диалог не открывается
        void loadWizard();
        return;
      }
      xrayIds = ids;
      forceGroup = view.force_proxy?.group ?? '';
      xrayOpen = true;
    } catch {
      showToast('error', $t('cfg.load_error_body'));
    } finally {
      loading = false;
      syncPhase();
    }
  }

  function setXrayOpen(v: boolean) {
    xrayOpen = v;
    syncPhase();
  }

  function openMihomo() {
    if (busy) return;
    mihomoOpen = true;
    syncPhase();
  }

  function closeMihomo() {
    mihomoOpen = false;
    syncPhase();
    void loadWizard();
  }
</script>

<!-- «Взять остальные» для строки шага (сниппет aside SetupWizard, виден и в свёрнутом
     шаге): открывает тот же диалог, что тело шага, — экземпляр диалога один -->
{#snippet takeRest(onclick: () => void, disabled: boolean, label: string)}
  <Button variant="secondary" size="sm" {disabled} data-testid="setup-take-rest" {onclick}>
    {label}
  </Button>
{/snippet}

<div class="step-sections">
  {#if !kernels.xray && !kernels.mihomo}
    <p class="step-sections-note">{$t('cfg.setup.sections.need_kernel')}</p>
  {:else if showXray || showMihomo}
    <div class="step-sections-actions">
      {#if showXray}
        <Button
          variant={isCurrent ? 'primary' : 'secondary'}
          size="sm"
          {loading}
          disabled={busy}
          data-testid="setup-take-xray"
          onclick={() => void openXray()}
        >
          {both ? $t('cfg.setup.sections.take_xray') : $t('cfg.setup.sections.take')}
        </Button>
      {/if}
      {#if showMihomo}
        <Button
          variant={isCurrent && !showXray ? 'primary' : 'secondary'}
          size="sm"
          disabled={busy}
          data-testid="setup-take-mihomo"
          onclick={openMihomo}
        >
          {$t('cfg.setup.sections.take_mihomo')}
        </Button>
      {/if}
    </div>
    {#if showMihomo}
      <p class="step-sections-note">{$t('cfg.setup.sections.mihomo_hint')}</p>
    {/if}
  {/if}
</div>

<TakeSectionModal
  bind:open={() => xrayOpen, setXrayOpen}
  kernel="xray"
  mode="all"
  ids={xrayIds}
  {forceGroup}
  ontaken={() => void loadWizard()}
/>

<TakeRoutingModal open={mihomoOpen} mode="take" scope="mihomo" onclose={closeMihomo} />

<style>
  .step-sections {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-2);
    min-width: 0;
  }

  .step-sections-actions {
    display: flex;
    flex-wrap: wrap;
    gap: var(--spacing-2);
    min-width: 0;
  }

  .step-sections-note {
    margin: 0;
    color: var(--fg-secondary);
    line-height: 1.5;
    overflow-wrap: anywhere;
  }

  @media (max-width: 480px) {
    .step-sections-actions :global(.btn) {
      width: 100%;
    }
  }
</style>
