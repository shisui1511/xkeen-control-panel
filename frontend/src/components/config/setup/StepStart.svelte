<script lang="ts" module>
  /** Ход запуска ядра в шаге 6: idle — ничего не идёт, running — идёт запрос, error — сбой. */
  export type StartPhase = 'idle' | 'running' | 'error';
</script>

<script lang="ts">
  import { tick } from 'svelte';
  import { t } from '../../../i18n';
  import Button from '../../Button.svelte';
  import SegmentedControl from '../../SegmentedControl.svelte';
  import StatusBadge from '../../StatusBadge.svelte';
  import Icon from '../../../lib/components/Icon.svelte';
  import ImmediateMark from '../ImmediateMark.svelte';
  import { apiFetchJSON } from '../../../lib/api';
  import { activeKernelName } from '../../../stores';
  // SWITCH_BANNER — строка скрипта XKeen о смене ядра, причиной и строкой хвоста не бывает;
  // ERROR_MARK — признак строки ошибки
  import {
    ERROR_MARK,
    SWITCH_BANNER,
    notifySwitchOutcome,
    switchKernel
  } from '../../../lib/serviceControl';
  import { KERNEL_NAMES, kernelLabel, type KernelName } from '../../../lib/kernelState';
  import {
    WIZARD_STEP_IDS,
    loadWizard,
    wizardView,
    type WizardKernels,
    type WizardStep
  } from '../../../lib/configWizard';
  import { VALUE_MARK, valueParts } from './valueParts';

  // Тело шага 6 «Запуск ядра» (149 D-23, D-15, D-16, FND-05): ядро запускается только
  // явной кнопкой — тем же switchKernel и тостом исхода, что на «Службах»; при двух
  // установленных ядрах выбор Xray | Mihomo. «Запустить …» — главная (primary), только
  // пока шаг текущий. Сбой запуска показывается в шаге общим блоком ошибки: причина, до 8
  // последних строк (только текстом), «Повторить запуск» — тогда единственный вход в запуск,
  // строка «Запустить …» скрыта, — «Журнал» и «Файлы». Причина и хвост берутся из проверки
  // конфигурации сервером, вывода переключения и буфера ядра — в этом порядке; строка
  // скрипта XKeen о смене ядра отбрасывается: это не причина, а сообщение об успехе смены
  // (G-149.1-8). Конфликт ядер — не сбой запуска: кнопка неактивна с причиной, кнопки
  // остановки стоят в шаге 5.
  // Повтор держит блок ошибки и «Повторить запуск» на месте до исхода (D-16): кнопка в ходе
  // становится disabled, браузер снимает с неё фокус — после исхода он возвращается на
  // действие шага.

  interface Props {
    step: WizardStep | undefined;
    kernels: WizardKernels;
    onstarted: (kernel: KernelName) => void;
    /** Ход запуска для статуса строки шага. */
    phase?: StartPhase;
  }

  let { step, kernels, onstarted, phase = $bindable('idle') }: Props = $props();

  const START_N = WIZARD_STEP_IDS.indexOf('start') + 1;
  const isCurrent = $derived($wizardView?.current === START_N);

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
  // Откуда взята причина: проверка конфигурации, вывод переключения, буфер ядра или нигде
  let failSource = $state<'check' | 'output' | 'buffer' | 'none'>('none');
  let failKernel = $state<KernelName | ''>('');
  let logLines = $state<string[]>([]);
  // Запуск начат из блока ошибки: блок с прежними причиной и логом остаётся до исхода
  let retrying = $state(false);
  const showError = $derived(phase === 'error' || (running && retrying));

  let root = $state<HTMLDivElement>();

  // Причины нет — имя ядра не выдаётся за причину: текст называет ядро и отсылает к логу
  const failKey = $derived.by(() => {
    const withLog = logLines.length > 0;
    if (failReason !== '')
      return withLog ? 'cfg.setup.start.failed' : 'cfg.setup.start.failed_nolog';
    return withLog ? 'cfg.setup.start.failed_unknown_log' : 'cfg.setup.start.failed_unknown';
  });
  // Имя ядра — моноширинным
  const failParts = $derived(valueParts($t(failKey, { kernel: VALUE_MARK, reason: failReason })));

  // Сколько строк хвоста показывает блок ошибки
  const TAIL_LINES = 8;

  // Строки без пустых и без строки о смене ядра
  function cleanLines(lines: string[]): string[] {
    return lines.map((l) => l.trim()).filter((l) => l !== '' && !SWITCH_BANNER.test(l));
  }

  // Последние строки текста без пустых и без строки о смене ядра
  function textLines(text: string): string[] {
    return cleanLines(text.split('\n')).slice(-TAIL_LINES);
  }

  // Причина — первая строка с признаком ошибки, без метки источника в квадратных скобках
  // и точек в конце; строки без признака ошибки причиной не выдаются
  function reasonOf(lines: string[]): string {
    const line = lines.find((l) => ERROR_MARK.test(l)) ?? '';
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

  // Сбой запуска: check — вывод проверки конфигурации сервером, output — вывод
  // переключения (или текст ошибки запроса), затем буфер ядра. Хвост — строки в этом
  // порядке без повторов, причина — из первого источника, где есть строка ошибки
  async function fail(kernel: KernelName, check: string, output: string) {
    const sources: Array<['check' | 'output' | 'buffer', string[]]> = [
      ['check', textLines(check)],
      ['output', textLines(output)],
      ['buffer', cleanLines(await loadLog(kernel))]
    ];
    const tail: string[] = [];
    for (const [, lines] of sources) {
      for (const line of lines) {
        if (tail.length < TAIL_LINES && !tail.includes(line)) tail.push(line);
      }
    }
    let reason = '';
    let source: 'check' | 'output' | 'buffer' | 'none' = 'none';
    for (const [name, lines] of sources) {
      reason = reasonOf(lines);
      if (reason !== '') {
        source = name;
        break;
      }
    }
    failReason = reason;
    failSource = source;
    failKernel = kernel;
    logLines = tail;
    phase = 'error';
  }

  function clearFailure() {
    failReason = '';
    failSource = 'none';
    failKernel = '';
    logLines = [];
  }

  function focusLost(): boolean {
    const active = document.activeElement;
    return active === null || active === document.body || !active.isConnected;
  }

  // Фокус, снятый с кнопки в ходе, возвращается на действие шага: «Повторить запуск»
  // (новый сбой), иначе «Запустить …», если она видна и активна; ни одной — на заголовок
  // шага (успех убирает кнопки), чтобы клавиатура не начинала с начала страницы
  function restoreFocus() {
    if (!root || !focusLost()) return;
    for (const cls of ['step-start-retry', 'step-start-main']) {
      const btn = root.querySelector<HTMLButtonElement>(`button.${cls}`);
      if (btn && !btn.disabled) {
        btn.focus();
        return;
      }
    }
    root.closest('.setup-step')?.querySelector<HTMLButtonElement>('.setup-step-toggle')?.focus();
  }

  // Запрос уходит только по нажатию: автозапуска нет (FND-05)
  async function start() {
    const kernel = chosen;
    if (kernel === '' || running || conflict) return;
    const hadFocus = root?.contains(document.activeElement) === true;
    // В ошибке строка «Запустить …» скрыта: вход из ошибки — это повтор
    retrying = phase === 'error';
    phase = 'running';
    if (!retrying) clearFailure();
    try {
      const result = await switchKernel(kernel);
      notifySwitchOutcome(result);
      if (result.outcome === 'new_not_started') {
        await fail(kernel, result.check, result.output);
      } else {
        clearFailure();
        phase = 'idle';
        if (result.outcome === 'switched') onstarted(kernel);
      }
    } catch (e: unknown) {
      const err = e as { code?: unknown; status?: unknown } | null;
      if (err?.code === 'kernel_conflict' || err?.status === 401) {
        // Конфликт возник между ответом и нажатием: это не сбой запуска — шаг покажет
        // причину конфликта после перечитывания
        clearFailure();
        phase = 'idle';
      } else {
        // При HTTP 500 сервер уже дописал к тексту ошибки строки проверки конфигурации
        await fail(kernel, '', e instanceof Error ? e.message : String(e));
      }
    } finally {
      retrying = false;
      void loadWizard();
      if (hadFocus) {
        await tick();
        restoreFocus();
      }
    }
  }
</script>

<div class="step-start" bind:this={root}>
  {#if installed.length === 0}
    <p class="step-start-note">{$t('cfg.setup.sections.need_kernel')}</p>
  {:else if done}
    <div class="step-start-row" data-testid="setup-start-running">
      <span class="setup-mono">{runningLabel}</span>
      <StatusBadge variant="running" label={$t('cfg.setup.start.running_badge')} />
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

    {#if !(showError && !conflict)}
      <div class="step-start-row">
        <Button
          variant={isCurrent ? 'primary' : 'secondary'}
          size="sm"
          class="step-start-main"
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
    {/if}

    {#if conflict}
      <p
        class="step-start-conflict"
        id="setup-start-conflict-hint"
        data-testid="setup-start-conflict"
      >
        <span class="step-start-icon" aria-hidden="true"><Icon name="warning" size={16} /></span>
        <span>{$t('kernel.conflict_blocked')}</span>
      </p>
    {:else if showError}
      <div
        class="alert alert-error step-start-error"
        role="alert"
        data-testid="setup-start-error"
        data-reason={failSource}
      >
        <span class="step-start-icon" aria-hidden="true"><Icon name="warning" size={16} /></span>
        <div class="step-start-error-body">
          <span>
            {failParts[0]}{#if failReason === ''}<span class="setup-mono">
                {kernelLabel(failKernel || chosen)}
              </span>{/if}{failParts[1]}
          </span>
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
            <Button
              variant="primary"
              size="sm"
              class="step-start-retry"
              data-testid="setup-start-retry"
              disabled={chosen === ''}
              loading={running}
              onclick={start}
            >
              {$t('cfg.setup.start.retry')}
            </Button>
            <ImmediateMark />
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
    }
  }
</style>
