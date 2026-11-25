<script lang="ts" module>
  /** Ход шага 1: running — идёт установка ядра или открыто окно установки XKeen. */
  export type KernelPhase = 'idle' | 'running';
</script>

<script lang="ts">
  import { onDestroy, onMount } from 'svelte';
  import { t } from '../../../i18n';
  import Button from '../../Button.svelte';
  import Spinner from '../../Spinner.svelte';
  import Icon from '../../../lib/components/Icon.svelte';
  import ImmediateMark from '../ImmediateMark.svelte';
  import XKeenInstallCard from '../../xkeen/XKeenInstallCard.svelte';
  import { apiFetchJSON } from '../../../lib/api';
  import { runKernelInstall } from '../../../lib/kernelInstall';
  import { KERNEL_NAMES, kernelLabel, type KernelName } from '../../../lib/kernelState';
  import { failureMessage } from '../../../lib/kernelView';
  import { loadWizard, type WizardKernels, type WizardStep } from '../../../lib/configWizard';
  import { VALUE_MARK, valueParts } from './valueParts';

  // Тело шага 1 «Ядро и XKeen» (D-14, 149 D-23 шаг 1, B2, B4): XKeen ставится встроенным
  // блоком установки — без заголовка секции и карточки, терминал — в окне установщика, как
  // на «Службах»; ядро — выбором из двух карточек тем же
  // клиентом установки, что на «Службах». Установка идёт только по кнопке и сразу, мимо
  // черновика: «Сбросить» её не отменяет (пометка немедленного действия, 149 D-14). Одно
  // ядро уже стоит — шаг выполнен, второе не навязывается: ссылка на «Службы».

  interface Props {
    step: WizardStep | undefined;
    kernels: WizardKernels;
    /** Ход шага для статуса строки. */
    phase?: KernelPhase;
  }

  let { step, kernels, phase = $bindable('idle') }: Props = $props();

  const installed = $derived(KERNEL_NAMES.filter((k) => kernels[k]));

  // Доступность установщика XKeen — из статуса служб, как на «Службах»
  let xkeenAvailable = $state(false);
  let xkeenIncomplete = $state(false);
  let xkeenOpen = $state(false);

  onMount(() => {
    if (kernels.xkeen) return;
    apiFetchJSON<Record<string, unknown>>('/api/service/status')
      .then((d) => {
        xkeenAvailable = d?.xkeen_installer_available === true;
        xkeenIncomplete = d?.xkeen_setup_incomplete === true;
      })
      .catch(() => {});
  });

  let installing = $state<KernelName | ''>('');
  let failKernel = $state<KernelName | ''>('');
  let failReason = $state('');
  // Текст ошибки сам ставит точку после причины
  const failText = $derived(failReason.trim().replace(/[.。]+$/, ''));
  // Имя ядра — моноширинным; причины нет — имя ядра не выдаётся за причину, текст отсылает
  // к журналу
  const failParts = $derived(
    valueParts(
      failText !== ''
        ? $t('cfg.setup.kernel.install_failed', { kernel: VALUE_MARK, reason: failText })
        : $t('cfg.setup.kernel.install_failed_unknown', { kernel: VALUE_MARK })
    )
  );

  // «Установлено: …» — имена ядер моноширинным
  const installedParts = $derived(
    valueParts($t('cfg.setup.kernel.installed', { kernels: VALUE_MARK }))
  );

  // Уход со страницы мастера снимает опрос установки и замок меню; установка на сервере
  // продолжается, её итог покажут «Службы» и следующий вид мастера
  let installAbort: AbortController | null = null;
  onDestroy(() => installAbort?.abort());

  function syncPhase() {
    phase = installing !== '' || xkeenOpen ? 'running' : 'idle';
  }

  // Установка — только по нажатию: при монтировании шага ничего не ставится
  async function install(kernel: KernelName) {
    if (installing !== '') return;
    installing = kernel;
    failKernel = '';
    failReason = '';
    syncPhase();
    const ctrl = new AbortController();
    installAbort = ctrl;
    try {
      const st = await runKernelInstall(kernel, { signal: ctrl.signal });
      if (st.status === 'failed') {
        failKernel = kernel;
        // Причина — как на «Службах»: перевод по error_kind, иначе текст message
        const f = failureMessage(st);
        failReason = f ? $t(f.key, f.params) : (st.message ?? '');
      }
    } catch (e: unknown) {
      // Отмена при размонтировании — не сбой установки
      if (!ctrl.signal.aborted) {
        failKernel = kernel;
        failReason = e instanceof Error && e.message !== '' ? e.message : String(e);
      }
    } finally {
      if (installAbort === ctrl) installAbort = null;
      installing = '';
      syncPhase();
      void loadWizard();
    }
  }

  function onXkeenOpen(open: boolean) {
    xkeenOpen = open;
    syncPhase();
  }
</script>

<div class="step-kernel">
  {#if !kernels.xkeen}
    <p class="step-kernel-note">{$t('cfg.setup.kernel.xkeen_first')}</p>
    <div class="step-kernel-xkeen" data-testid="setup-kernel-xkeen">
      <ImmediateMark />
      <XKeenInstallCard
        embedded
        available={xkeenAvailable}
        incomplete={xkeenIncomplete}
        onfinished={() => void loadWizard()}
        onopenchange={onXkeenOpen}
      />
    </div>
  {/if}

  {#if installed.length === 0}
    <div class="step-kernel-head">
      <p class="step-kernel-title">{$t('cfg.setup.kernel.choose')}</p>
      <ImmediateMark />
    </div>
    <div class="step-kernel-choices">
      {#each KERNEL_NAMES as k (k)}
        <div class="step-kernel-choice" data-testid="setup-kernel-choice-{k}">
          <p class="step-kernel-name">{kernelLabel(k)}</p>
          <p class="step-kernel-hint">{$t(`cfg.setup.kernel.${k}_hint`)}</p>
          <div class="step-kernel-actions">
            <Button
              variant="secondary"
              size="sm"
              loading={installing === k}
              disabled={installing !== ''}
              data-testid="setup-kernel-install-{k}"
              onclick={() => void install(k)}
            >
              {$t('cfg.setup.kernel.install', { kernel: kernelLabel(k) })}
            </Button>
          </div>
          {#if installing === k}
            <!-- Начало установки объявляет живая область страницы мастера -->
            <p class="step-kernel-status">
              <Spinner size={16} />
              {$t('cfg.setup.kernel.installing', { kernel: kernelLabel(k) })}
            </p>
          {/if}
          {#if failKernel === k && installing === ''}
            <div
              class="alert alert-error step-kernel-error"
              role="alert"
              data-testid="setup-kernel-error-{k}"
            >
              <span class="step-kernel-icon" aria-hidden="true">
                <Icon name="warning" size={16} />
              </span>
              <div class="step-kernel-error-body">
                {failParts[0]}<span class="setup-mono">{kernelLabel(k)}</span>{failParts[1]}
              </div>
            </div>
          {/if}
        </div>
      {/each}
    </div>
  {:else}
    <p class="step-kernel-note">
      {installedParts[0]}<span class="setup-mono">
        {installed.map(kernelLabel).join(', ')}
      </span>{installedParts[1]}
    </p>
    {#if installed.length === 1}
      <a class="step-kernel-link" href="#/services" data-testid="setup-kernel-services-link">
        {$t('cfg.setup.kernel.second')}
        <Icon name="chevron-right" size={16} />
      </a>
    {/if}
  {/if}
</div>

<style>
  .step-kernel {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-4);
    min-width: 0;
  }

  .step-kernel-note,
  .step-kernel-hint,
  .step-kernel-title,
  .step-kernel-name,
  .step-kernel-status {
    margin: 0;
    line-height: 1.5;
    overflow-wrap: anywhere;
  }

  .step-kernel-note,
  .step-kernel-hint,
  .step-kernel-status {
    color: var(--fg-secondary);
  }

  .step-kernel-title,
  .step-kernel-name {
    color: var(--fg-primary);
    font-weight: 600;
  }

  .step-kernel-name {
    font-family: var(--font-family-mono);
  }

  /* Встроенная установка XKeen: блок шага, терминал — в окне установщика */
  .step-kernel-xkeen {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-2);
    min-width: 0;
  }

  .step-kernel-head {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--spacing-2);
    min-width: 0;
  }

  .step-kernel-choices {
    display: grid;
    grid-template-columns: repeat(2, minmax(0, 1fr));
    gap: var(--spacing-4);
  }

  .step-kernel-choice {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-2);
    min-width: 0;
    padding: var(--spacing-4);
    background: var(--bg-elevated);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
  }

  .step-kernel-actions {
    display: flex;
    flex-wrap: wrap;
    gap: var(--spacing-2);
    margin-top: auto;
  }

  .step-kernel-status {
    display: flex;
    align-items: center;
    gap: var(--spacing-2);
  }

  /* Блок сбоя — как в шагах 5 и 6: рамка, фон и значок в тоне блока, текст 14 px основным
     цветом */
  .step-kernel-error {
    margin-bottom: 0;
  }

  .step-kernel-icon {
    display: inline-flex;
    flex-shrink: 0;
    align-items: center;
    height: 1lh;
  }

  .step-kernel-error-body {
    min-width: 0;
    color: var(--fg-primary);
    font-size: var(--font-size-base);
    line-height: 1.5;
    overflow-wrap: anywhere;
  }

  .step-kernel-link {
    display: inline-flex;
    align-items: center;
    gap: var(--spacing-1);
    align-self: flex-start;
    color: var(--fg-secondary);
    text-decoration: none;
  }

  .step-kernel-link:hover {
    text-decoration: underline;
  }

  @media (max-width: 768px) {
    .step-kernel-link {
      min-height: 44px;
    }
  }

  /* На 481–768 px выбор ядра остаётся в две колонки, на телефоне — в одну */
  @media (max-width: 480px) {
    .step-kernel-actions :global(.btn) {
      width: 100%;
    }

    .step-kernel-choices {
      grid-template-columns: minmax(0, 1fr);
    }
  }
</style>
