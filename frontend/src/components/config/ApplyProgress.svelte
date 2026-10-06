<script lang="ts">
  import { t } from '../../i18n';
  import Button from '../Button.svelte';
  import Icon from '../../lib/components/Icon.svelte';
  import {
    layerSnapshot,
    applyStartedHere,
    type LayerStep,
    type LayerRestart,
    type StepId,
    type StepState
  } from '../../lib/configLayer';

  // Порядок шагов фиксирован (D-16): недостающие в состоянии показываются ожидающими
  const STEP_ORDER: StepId[] = ['build', 'validate_xray', 'validate_mihomo', 'write', 'restart'];
  const KERNEL_LABELS: Record<string, string> = { xray: 'Xray', mihomo: 'Mihomo' };

  let section = $state<HTMLElement | null>(null);
  // Ход виден, пока идёт применение, и после него — до «Скрыть» или следующего запуска.
  // Свежая загрузка страницы с уже завершённым применением его не показывает.
  let seenRunning = $state(false);
  let dismissed = $state(false);

  const apply = $derived($layerSnapshot?.apply ?? null);
  const running = $derived(apply?.running ?? false);
  const result = $derived(apply?.result ?? null);

  $effect(() => {
    if (running || $applyStartedHere) {
      seenRunning = true;
      dismissed = false;
    }
  });

  const visible = $derived(running || (seenRunning && !dismissed && result !== null));

  const steps = $derived(
    STEP_ORDER.map(
      (id): LayerStep => apply?.steps.find((s) => s.id === id) ?? { id, state: 'pending' }
    )
  );
  const restartRows = $derived<LayerRestart[]>(apply?.restart ?? []);

  // Карточка появилась — прокручиваем к ней, не дёргая страницу при reduced-motion
  let wasVisible = false;
  $effect(() => {
    if (visible && !wasVisible && section) {
      const reduced = window.matchMedia('(prefers-reduced-motion: reduce)').matches;
      section.scrollIntoView({ block: 'nearest', behavior: reduced ? 'auto' : 'smooth' });
    }
    wasVisible = visible;
  });

  const kernelLabel = (k: string) => KERNEL_LABELS[k] ?? k;

  function markerLabel(state: StepState): string {
    return $t('cfg.step_state.' + state);
  }

  // Заметки фолбэка Mihomo (D-17) видны, только пока идёт перезапуск
  const FALLBACK_NOTES = ['hot_reload_failed_restarting', 'restart_failed_rolling_back'];

  function stepNote(step: LayerStep): string {
    const code = step.note_code;
    if (!code) return '';
    if (FALLBACK_NOTES.includes(code)) {
      if (step.state !== 'running') return '';
      if (code === 'hot_reload_failed_restarting') return $t('cfg.restart_note.' + code);
      // Ядро известно, только когда его строка итога уже опубликована
      const failed = restartRows.find((r) => r.outcome.startsWith('failed_'));
      return failed
        ? $t('cfg.restart_note.' + code, { kernel: kernelLabel(failed.kernel) })
        : $t('cfg.restart_note.rolling_back_generic');
    }
    const key = 'cfg.step_note.' + code;
    const text = $t(key);
    return text === key ? '' : text;
  }

  // Подсказка по коду; пустая строка, если для кода нет перевода
  function hintText(code: string | undefined): string {
    if (!code) return '';
    const key = 'cfg.hint.' + code;
    const text = $t(key);
    return text === key ? '' : text;
  }

  const otherTab = $derived(running && !$applyStartedHere);
  const failure = $derived(result && !result.ok ? result : null);
  // «Файлы возвращены» — только по флагу бэкенда, а не по имени кода: при неудачном откате
  // и при ядре, не поднявшемся после отката, говорить об этом нельзя
  const rolledBack = $derived(failure !== null && failure.rolled_back === true);
  // Отказ до записи: файлы на диске не менялись
  const FILES_UNTOUCHED = [
    'validation_failed',
    'validation_timeout',
    'validation_not_run',
    'build_failed',
    'drift_blocked'
  ];
  const filesUntouched = $derived(failure !== null && FILES_UNTOUCHED.includes(failure.code));

  // Пояснение причины отказа по коду итога; вывод ядра идёт отдельным блоком
  const failureHeadline = $derived.by(() => {
    if (!failure) return '';
    const kernel = kernelLabel(failure.kernel ?? '');
    switch (failure.code) {
      case 'validation_failed':
        return $t('cfg.result.validation', { kernel });
      case 'validation_timeout':
        return $t('cfg.result.timeout', { kernel });
      case 'validation_not_run':
        return $t('cfg.result.not_run', { kernel });
      case 'build_failed':
        return $t('cfg.result.build_failed');
      case 'write_failed':
      case 'restart_failed':
        return rolledBack ? $t('cfg.result.rolled_back') : $t('cfg.result.rollback_failed');
      case 'kernel_not_recovered':
        return $t('cfg.result.kernel_not_recovered', { kernel });
      case 'rollback_failed':
        return $t('cfg.result.rollback_failed');
      case 'interrupted':
        return $t('cfg.result.interrupted');
      case 'drift_blocked':
        return $t('cfg.result.drift_blocked');
      default:
        return rolledBack ? $t('cfg.result.rolled_back') : '';
    }
  });

  const orphans = $derived(result?.orphans_removed ?? []);
</script>

{#if visible}
  <section
    class="card apply-progress"
    data-testid="config-progress"
    aria-label={$t('cfg.progress_title')}
    bind:this={section}
  >
    <div class="card-title">{$t('cfg.progress_title')}</div>

    {#if otherTab}
      <span class="caption">{$t('cfg.step.other_tab')}</span>
    {/if}

    <ol class="steps" aria-live="polite">
      {#each steps as step (step.id)}
        {@const note = stepNote(step)}
        <li class="step" data-step={step.id} data-state={step.state}>
          <span class="marker marker-{step.state}" role="img" aria-label={markerLabel(step.state)}>
            {#if step.state === 'running'}
              <span class="spinner" aria-hidden="true"></span>
            {:else if step.state === 'done'}
              <Icon name="check" size={16} />
            {:else if step.state === 'failed'}
              <Icon name="cross" size={16} />
            {:else if step.state === 'deferred'}
              <Icon name="info" size={16} />
            {:else if step.state === 'skipped'}
              <span class="dash" aria-hidden="true">–</span>
            {:else}
              <span class="ring" aria-hidden="true"></span>
            {/if}
          </span>
          <div class="step-body">
            <span class="step-label">{$t('cfg.step.' + step.id)}</span>
            {#if note}
              <span class="step-note">{note}</span>
            {/if}
            {#if step.id === 'restart' && restartRows.length > 0}
              <ul class="restart-rows">
                {#each restartRows as row (row.kernel)}
                  <li class="restart-row" data-kernel={row.kernel} data-outcome={row.outcome}>
                    {$t('cfg.restart.' + row.outcome, { kernel: kernelLabel(row.kernel) })}
                  </li>
                {/each}
              </ul>
            {/if}
          </div>
        </li>
      {/each}
    </ol>

    {#if failure}
      <div class="alert alert-error result" role="alert" data-testid="config-apply-failure">
        <Icon name="cross" size={16} />
        <div class="result-body">
          <strong>{$t('cfg.result.failed_title')}</strong>
          {#if failureHeadline}
            <span>{failureHeadline}</span>
          {/if}
          {#if failure.message}
            <!-- Вывод ядра — только текстовый узел (T-144-48): разметка ядра не исполняется -->
            <!-- svelte-ignore a11y_no_noninteractive_tabindex -->
            <pre
              class="kernel-output"
              role="region"
              aria-label={$t('cfg.result.output_label')}
              tabindex="0">{failure.message}</pre>
          {/if}
          {#if hintText(failure.hint_code)}
            <span>{$t('cfg.result.fix_hint', { hint: hintText(failure.hint_code) })}</span>
          {/if}
          {#if failure.issues && failure.issues.length > 0}
            <ul class="issues">
              {#each failure.issues as issue, i (issue.key + ':' + issue.code + ':' + i)}
                <li>
                  <code class="issue-key">{issue.key}</code>
                  <span>{hintText(issue.code) || issue.detail || issue.code}</span>
                </li>
              {/each}
            </ul>
          {/if}
          {#if filesUntouched}
            <span>{$t('cfg.result.reassurance')}</span>
          {/if}
        </div>
      </div>
    {/if}

    {#if result?.ok}
      <div class="alert alert-success result" role="status" data-testid="config-apply-success">
        <Icon name="check" size={16} />
        <div class="result-body">
          <span>{$t('cfg.result.success', { n: result.written })}</span>
          {#if orphans.length > 0}
            <span class="result-line">{$t('cfg.result.orphans', { list: orphans.join(', ') })}</span
            >
          {/if}
        </div>
      </div>
    {/if}

    {#if !running && result}
      <div class="progress-actions">
        <Button variant="secondary" class="btn-sm" onclick={() => (dismissed = true)}
          >{$t('cfg.hide')}</Button
        >
      </div>
    {/if}
  </section>
{/if}

<style>
  .apply-progress {
    display: flex;
    flex-direction: column;
    gap: 12px;
  }

  .steps {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 12px;
  }

  .step {
    display: flex;
    align-items: flex-start;
    gap: 8px;
    min-width: 0;
  }

  .marker {
    flex-shrink: 0;
    width: 20px;
    height: 20px;
    display: inline-flex;
    align-items: center;
    justify-content: center;
  }

  .marker-done {
    color: var(--success);
  }

  .marker-failed {
    color: var(--danger);
  }

  .marker-skipped {
    color: var(--fg-dim);
  }

  .marker-deferred {
    color: var(--fg-secondary);
  }

  .marker .spinner {
    --spinner-size: 16px;
    --spinner-color: var(--accent);
  }

  .ring {
    box-sizing: border-box;
    width: 12px;
    height: 12px;
    border-radius: var(--radius-full);
    border: 2px solid var(--fg-faint);
  }

  .dash {
    font-size: var(--font-size-base);
    line-height: 1;
  }

  .step-body {
    display: flex;
    flex-direction: column;
    gap: 2px;
    min-width: 0;
    overflow-wrap: anywhere;
  }

  .step-label {
    font-size: var(--font-size-base);
    font-weight: 600;
    line-height: 1.5;
    color: var(--fg-primary);
  }

  .step-note {
    font-size: var(--font-size-xs);
    line-height: 1.4;
    color: var(--fg-dim);
  }

  .restart-rows {
    list-style: none;
    margin: 4px 0 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 4px;
  }

  .restart-row {
    font-size: var(--font-size-sm);
    line-height: 1.5;
    color: var(--fg-secondary);
  }

  .result {
    margin-bottom: 0;
    align-items: flex-start;
  }

  .result-body {
    display: flex;
    flex-direction: column;
    gap: 4px;
    min-width: 0;
    overflow-wrap: anywhere;
    color: var(--fg-primary);
  }

  .caption {
    font-size: var(--font-size-xs);
    line-height: 1.4;
    color: var(--fg-dim);
  }

  .kernel-output {
    margin: 0;
    padding: 8px 12px;
    background: var(--bg-elevated);
    border: 1px solid var(--border-light);
    border-radius: var(--radius-sm);
    color: var(--fg-primary);
    font-family: var(--font-family-mono);
    font-size: var(--font-size-xs);
    line-height: 1.5;
    white-space: pre-wrap;
    overflow-wrap: anywhere;
    max-height: 200px;
    overflow: auto;
    user-select: text;
    scrollbar-width: thin;
  }

  .kernel-output::-webkit-scrollbar {
    width: 4px;
    height: 4px;
  }
  .kernel-output::-webkit-scrollbar-track {
    background: transparent;
  }
  .kernel-output::-webkit-scrollbar-thumb {
    background: var(--border);
    border-radius: var(--radius-sm);
  }

  .issues {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 4px;
  }

  .issue-key {
    font-family: var(--font-family-mono);
    font-size: var(--font-size-xs);
    margin-right: 4px;
  }

  .progress-actions {
    display: flex;
    justify-content: flex-start;
  }

  @media (max-width: 768px) {
    .progress-actions :global(.btn) {
      min-height: 44px;
      flex: 1;
    }
  }
</style>
