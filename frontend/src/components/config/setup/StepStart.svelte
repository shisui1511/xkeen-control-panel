<script lang="ts" module>
  /** Ход запуска ядра в шаге 6: idle — ничего не идёт, running — идёт запрос, error — сбой. */
  export type StartPhase = 'idle' | 'running' | 'error';
</script>

<script lang="ts">
  import { t } from '../../../i18n';
  import Button from '../../Button.svelte';
  import SegmentedControl from '../../SegmentedControl.svelte';
  import StatusBadge from '../../StatusBadge.svelte';
  import Icon from '../../../lib/components/Icon.svelte';
  import ImmediateMark from '../ImmediateMark.svelte';
  import { apiFetchJSON } from '../../../lib/api';
  import { activeKernelName } from '../../../stores';
  import { notifySwitchOutcome, switchKernel } from '../../../lib/serviceControl';
  import { KERNEL_NAMES, kernelLabel, type KernelName } from '../../../lib/kernelState';
  import { loadWizard, type WizardKernels, type WizardStep } from '../../../lib/configWizard';

  // Тело шага 6 «Запуск ядра» (149 D-23, D-15, D-16, FND-05): ядро запускается только
  // явной кнопкой — тем же switchKernel и тостом исхода, что на «Службах»; при двух
  // установленных ядрах выбор Xray | Mihomo. Сбой запуска показывается в шаге: причина,
  // до 8 последних строк лога ядра (только текстом), «Повторить», «Журнал» и «Файлы».
  // Конфликт ядер — не сбой запуска: кнопка неактивна с причиной, кнопки остановки стоят
  // в шаге 5.

  interface Props {
    step: WizardStep | undefined;
    kernels: WizardKernels;
    onstarted: (kernel: KernelName) => void;
    /** Ход запуска для статуса строки шага. */
    phase?: StartPhase;
  }

  let { step, kernels, onstarted, phase = $bindable('idle') }: Props = $props();

  const installed = $derived(KERNEL_NAMES.filter((k) => kernels[k]));
  // По умолчанию — ядро, которое XKeen уже запустит (активное), иначе Mihomo, иначе
  // единственное установленное
  const defaultKernel = $derived.by((): KernelName | '' => {
    const active = $activeKernelName;
    if (active !== '' && installed.includes(active)) return active;
    if (installed.includes('mihomo')) return 'mihomo';
    return installed[0] ?? '';
  });
  let picked = $state<KernelName | ''>('');
  const chosen = $derived(picked !== '' && installed.includes(picked) ? picked : defaultKernel);

  const segments = $derived(
    installed.map((k) => ({ value: k, label: kernelLabel(k), testId: `setup-start-kernel-${k}` }))
  );

  const running = $derived(phase === 'running');
  const conflict = $derived(kernels.conflict);
  const done = $derived(step?.done === true && !conflict);
  const runningLabel = $derived(kernelLabel(kernels.running[0] ?? $activeKernelName));

  let failReason = $state('');
  let logLines = $state<string[]>([]);

  // Первая строка вывода без метки источника в квадратных скобках
  function firstLine(text: string): string {
    const line =
      text
        .split('\n')
        .map((l) => l.trim())
        .find((l) => l !== '') ?? '';
    return line.replace(/^\[[^\]]*\]\s*/, '').replace(/[.。]+$/, '');
  }

  interface LogHistory {
    entries?: Array<{ message?: unknown }>;
  }

  // Хвост лога ядра из истории панели: последние 8 записей, поле message каждой
  async function loadLog(kernel: KernelName): Promise<string[]> {
    try {
      // у Xray в буферах панели есть только xray-error и xray-access: хвост сбоя — в первом
      const source = kernel === 'xray' ? 'xray-error' : kernel;
      const data = await apiFetchJSON<LogHistory>('/api/logs/history?source=' + source);
      const entries = Array.isArray(data?.entries) ? data.entries : [];
      return entries
        .slice(-8)
        .map((e) => (typeof e?.message === 'string' ? e.message : ''))
        .filter((m) => m !== '');
    } catch {
      return [];
    }
  }

  async function fail(kernel: KernelName, reason: string) {
    failReason = reason || kernelLabel(kernel);
    logLines = await loadLog(kernel);
    phase = 'error';
  }

  // Запрос уходит только по нажатию: автозапуска нет (FND-05)
  async function start() {
    const kernel = chosen;
    if (kernel === '' || running || conflict) return;
    phase = 'running';
    failReason = '';
    logLines = [];
    try {
      const result = await switchKernel(kernel);
      notifySwitchOutcome(result);
      if (result.outcome === 'new_not_started') {
        await fail(kernel, firstLine(result.output));
      } else {
        phase = 'idle';
        if (result.outcome === 'switched') onstarted(kernel);
      }
    } catch (e: unknown) {
      const err = e as { code?: unknown; status?: unknown } | null;
      if (err?.code === 'kernel_conflict' || err?.status === 401) {
        // Конфликт возник между ответом и нажатием: это не сбой запуска — шаг покажет
        // причину конфликта после перечитывания
        phase = 'idle';
      } else {
        await fail(kernel, firstLine(e instanceof Error ? e.message : String(e)));
      }
    } finally {
      void loadWizard();
    }
  }
</script>

<div class="step-start">
  {#if installed.length === 0}
    <p class="step-start-note">{$t('cfg.setup.sections.need_kernel')}</p>
  {:else if done}
    <div class="step-start-row">
      <StatusBadge
        variant="running"
        label={$t('cfg.setup.start.running', { kernel: runningLabel })}
      />
    </div>
  {:else}
    {#if installed.length > 1}
      <div class="step-start-choice" data-testid="setup-start-kernel">
        <SegmentedControl
          items={segments}
          value={chosen}
          ariaLabel={$t('cfg.setup.start.segment_aria')}
          onchange={(v) => (picked = v as KernelName)}
        />
        <p class="step-start-note">{$t('cfg.setup.start.choice_hint')}</p>
      </div>
    {/if}

    <div class="step-start-row">
      <Button
        variant="primary"
        data-testid="setup-start"
        disabled={conflict || chosen === ''}
        loading={running}
        ariaDescribedby={conflict ? 'setup-start-conflict-hint' : undefined}
        onclick={start}
      >
        {$t('cfg.setup.start.run', { kernel: kernelLabel(chosen) })}
      </Button>
      <ImmediateMark />
    </div>

    {#if conflict}
      <p
        class="step-start-conflict"
        id="setup-start-conflict-hint"
        data-testid="setup-start-conflict"
      >
        <span class="step-start-icon" aria-hidden="true"><Icon name="warning" size={16} /></span>
        <span>{$t('kernel.conflict_blocked')}</span>
      </p>
    {:else if phase === 'error'}
      <div class="alert alert-error step-start-error" role="alert" data-testid="setup-start-error">
        <span class="step-start-icon" aria-hidden="true"><Icon name="warning" size={16} /></span>
        <div class="step-start-error-body">
          <span>{$t('cfg.setup.start.failed', { reason: failReason })}</span>
          {#if logLines.length > 0}
            <!-- Строки лога — только текстовый узел: разметка из лога не исполняется -->
            <!-- svelte-ignore a11y_no_noninteractive_tabindex -->
            <pre
              class="step-start-log"
              role="region"
              aria-label={$t('cfg.setup.start.log_aria')}
              tabindex="0"
              data-testid="setup-start-log">{logLines.join('\n')}</pre>
          {/if}
          <div class="step-start-actions">
            <Button variant="primary" size="sm" data-testid="setup-start-retry" onclick={start}>
              {$t('cfg.setup.start.retry')}
            </Button>
            <a class="btn btn-ghost btn-sm" href="#/logs" data-testid="setup-start-logs-link">
              {$t('cfg.setup.start.logs')}
            </a>
            <a
              class="btn btn-ghost btn-sm"
              href="#/config/files"
              data-testid="setup-start-files-link"
            >
              {$t('cfg.setup.start.files')}
            </a>
          </div>
        </div>
      </div>
    {/if}
  {/if}
</div>

<style>
  .step-start,
  .step-start-choice {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-4);
    min-width: 0;
  }

  .step-start-choice {
    gap: var(--spacing-2);
  }

  .step-start-row,
  .step-start-actions {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--spacing-2);
    min-width: 0;
  }

  .step-start-note {
    margin: 0;
    color: var(--fg-secondary);
    line-height: 1.5;
    overflow-wrap: anywhere;
  }

  .step-start-conflict {
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

  .step-start-icon {
    display: inline-flex;
    flex-shrink: 0;
    align-items: center;
    height: 1lh;
  }

  .step-start-error {
    margin-bottom: 0;
    border-color: var(--danger);
    background: var(--danger-soft);
  }

  .step-start-error-body {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-2);
    min-width: 0;
    color: var(--fg-primary);
    font-size: var(--font-size-base);
    line-height: 1.5;
    overflow-wrap: anywhere;
  }

  /* Хвост лога: до 8 строк по высоте, дальше — внутренняя прокрутка */
  .step-start-log {
    --log-line: 1.5;
    margin: 0;
    padding: var(--spacing-2);
    max-height: calc(var(--log-line) * 8em + 2 * var(--spacing-2));
    overflow: auto;
    background: var(--bg-terminal);
    color: var(--fg-terminal);
    border-radius: var(--radius-sm);
    font-family: var(--font-family-mono);
    font-size: var(--font-size-xs);
    line-height: var(--log-line);
    white-space: pre-wrap;
    overflow-wrap: anywhere;
    user-select: text;
    scrollbar-width: thin;
    scrollbar-color: var(--border) transparent;
  }

  .step-start-log::-webkit-scrollbar {
    width: 4px;
    height: 4px;
  }
  .step-start-log::-webkit-scrollbar-track {
    background: transparent;
  }
  .step-start-log::-webkit-scrollbar-thumb {
    background: var(--border);
    border-radius: var(--radius-sm);
  }

  @media (max-width: 480px) {
    .step-start-row :global(.btn),
    .step-start-actions :global(.btn) {
      width: 100%;
      min-height: 44px;
    }
  }
</style>
