<script lang="ts">
  import { t } from '../../../i18n';
  import Button from '../../Button.svelte';
  import Icon from '../../../lib/components/Icon.svelte';
  import {
    dnsFailureReason,
    dnsFailureHeadline,
    type DnsFailure,
    type DnsStep
  } from '../../../lib/configDns';

  // Ход транзакции перехвата DNS (DNSF-03): шаги приходят с сервера (id, status), подписи —
  // cfg.dns.step.<id>; интерфейс шаги не придумывает. Сбой — блок над списком: причина по
  // коду, итог отката и вывод ядра. Вывод ядра — текстовый узел, без разметки.
  let {
    steps,
    failure,
    kind,
    oncheck
  }: {
    /** Шаги транзакции с сервера. */
    steps: DnsStep[];
    /** Сбой транзакции; нет — транзакция идёт или прошла. */
    failure: DnsFailure | undefined;
    /** Включение или выключение: от него зависит заголовок сбоя. */
    kind: 'enable' | 'disable';
    /** «Проверить резолв» при незавершённом откате: фокус в поле проверки. */
    oncheck?: () => void;
  } = $props();

  // Список «идёт», пока нет сбоя и есть незавершённые шаги
  const busy = $derived(
    failure === undefined && steps.some((s) => s.status === 'running' || s.status === 'pending')
  );

  function stepWord(status: DnsStep['status']): string {
    switch (status) {
      case 'running':
        return $t('cfg.dns.step_running');
      case 'done':
        return $t('cfg.dns.step_done');
      case 'failed':
        return $t('cfg.dns.step_failed');
      case 'skipped':
        return $t('cfg.dns.step_skipped');
      default:
        return $t('cfg.dns.step_pending');
    }
  }

  // Причина — по коду с деталью сервера в параметрах; полный вывод ядра — в раскрытии
  const reason = $derived(failure ? dnsFailureHeadline(failure) : '');
  const rollbackReason = $derived(failure ? dnsFailureReason(failure) : '');
</script>

<div class="dns-steps-wrap">
  {#if failure}
    <div class="alert alert-error dns-fail" role="alert" data-testid="dns-fail">
      <Icon name="warning" size={16} class="icon-first-line" />
      <div class="dns-fail-body">
        <p class="dns-fail-title" data-testid="dns-fail-title">
          {$t(kind === 'enable' ? 'cfg.dns.fail_title' : 'cfg.dns.fail_title_disable')}
        </p>
        <p class="dns-fail-text" data-testid="dns-fail-reason">{reason}</p>
        {#if failure.rolled_back}
          {#if kind === 'enable'}
            <p class="dns-fail-text" data-testid="dns-fail-rolled-back">
              {$t('cfg.dns.fail_rolled_back')}
            </p>
          {/if}
        {:else}
          <div class="dns-fail-rollback" data-testid="dns-fail-rollback-failed">
            <p class="dns-fail-text">
              {$t('cfg.dns.fail_rollback_failed', { reason: rollbackReason })}
            </p>
            {#if oncheck}
              <Button variant="secondary" data-testid="dns-fail-check" onclick={() => oncheck?.()}>
                {$t('cfg.dns.check_btn')}
              </Button>
            {/if}
          </div>
        {/if}
        {#if failure.detail}
          <details class="dns-fail-output" data-testid="dns-fail-output">
            <summary>
              <span class="dns-summary-label">
                <span class="dns-summary-chevron" aria-hidden="true">
                  <Icon name="chevron-right" size={14} />
                </span>
                <span>{$t('cfg.dns.fail_output')}</span>
              </span>
            </summary>
            <pre>{failure.detail}</pre>
          </details>
        {/if}
      </div>
    </div>
  {/if}

  {#if steps.length > 0}
    <ol class="dns-steps" data-testid="dns-steps" aria-live="polite" aria-busy={busy}>
      {#each steps as step (step.id)}
        <li class="dns-step" data-testid="dns-step-{step.id}" data-status={step.status}>
          <span class="dns-step-marker dns-step-{step.status}" aria-hidden="true">
            {#if step.status === 'running'}
              <span class="spinner"></span>
            {:else if step.status === 'done'}
              <Icon name="check" size={16} />
            {:else if step.status === 'failed'}
              <Icon name="warning" size={16} />
            {:else if step.status === 'skipped'}
              <span class="dns-step-dash">–</span>
            {:else}
              <span class="dns-step-ring"></span>
            {/if}
          </span>
          <span class="dns-step-label">{$t(`cfg.dns.step.${step.id}`)}</span>
          <span class="sr-only">{stepWord(step.status)}</span>
        </li>
      {/each}
    </ol>
  {/if}
</div>

<style>
  .dns-steps-wrap {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-2);
    min-width: 0;
  }

  .dns-fail {
    margin: 0;
    min-width: 0;
    gap: var(--spacing-2);
    padding: var(--spacing-2) var(--spacing-4);
    line-height: 1.5;
  }

  .dns-fail-body {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-1);
    min-width: 0;
    flex: 1 1 auto;
    overflow-wrap: anywhere;
  }

  /* Заголовок сбоя — Heading 16/600/1.2, как заголовок баннера */
  .dns-fail-title {
    margin: 0;
    font-size: var(--font-size-lg);
    font-weight: 600;
    line-height: 1.2;
  }

  .dns-fail-text {
    margin: 0;
    font-size: var(--font-size-base);
    line-height: 1.5;
  }

  .dns-fail-rollback {
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    gap: var(--spacing-2);
    min-width: 0;
  }

  .dns-fail-output {
    min-width: 0;
  }

  /* Признак раскрытия — свой шеврон на всех ширинах (штатный маркер у display: flex
     пропадает), повёрнутый при [open] */
  .dns-fail-output summary {
    display: flex;
    align-items: center;
    gap: var(--spacing-1);
    cursor: pointer;
    list-style: none;
    font-size: var(--font-size-sm);
    font-weight: 600;
    line-height: 1.4;
  }

  .dns-fail-output summary::-webkit-details-marker {
    display: none;
  }

  /* Шеврон и подпись — одна группа с зазором xs 4px (одинаково у всех раскрытий) */
  .dns-summary-label {
    display: inline-flex;
    align-items: center;
    gap: var(--spacing-1);
    min-width: 0;
  }

  .dns-summary-chevron {
    display: inline-flex;
    flex: none;
  }

  .dns-fail-output[open] .dns-summary-chevron {
    transform: rotate(90deg);
  }

  @media (prefers-reduced-motion: no-preference) {
    .dns-summary-chevron {
      transition: transform var(--transition-fast);
    }
  }

  .dns-fail-output pre {
    margin: var(--spacing-1) 0 0;
    padding: var(--spacing-2) var(--spacing-4);
    max-height: 160px;
    overflow: auto;
    scrollbar-width: thin;
    scrollbar-color: var(--border) transparent;
    background: var(--bg-elevated);
    border-radius: var(--radius-md);
    color: var(--fg-primary);
    font-family: var(--font-family-mono);
    font-size: var(--font-size-sm);
    line-height: 1.4;
    white-space: pre-wrap;
    overflow-wrap: anywhere;
  }

  .dns-fail-output pre::-webkit-scrollbar {
    width: 6px;
    height: 6px;
  }

  .dns-fail-output pre::-webkit-scrollbar-thumb {
    background: var(--border);
    border-radius: var(--radius-full);
  }

  .dns-steps {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-2);
    margin: 0;
    padding: 0;
    list-style: none;
    min-width: 0;
  }

  .dns-step {
    display: flex;
    align-items: center;
    gap: var(--spacing-2);
    min-width: 0;
  }

  .dns-step-marker {
    flex-shrink: 0;
    width: 20px;
    height: 20px;
    display: inline-flex;
    align-items: center;
    justify-content: center;
  }

  .dns-step-marker .spinner {
    --spinner-size: 16px;
    --spinner-color: var(--accent);
  }

  .dns-step-done {
    color: var(--success);
  }

  .dns-step-failed {
    color: var(--warning);
  }

  .dns-step-skipped {
    color: var(--fg-dim);
  }

  .dns-step-dash {
    font-size: var(--font-size-base);
    line-height: 1;
  }

  .dns-step-ring {
    box-sizing: border-box;
    width: 12px;
    height: 12px;
    border-radius: var(--radius-full);
    border: 2px solid var(--fg-faint);
  }

  .dns-step-label {
    min-width: 0;
    font-size: var(--font-size-sm);
    font-weight: 600;
    line-height: 1.4;
    color: var(--fg-primary);
    overflow-wrap: anywhere;
  }

  @media (pointer: coarse), (max-width: 768px) {
    .dns-fail-rollback :global(.btn) {
      min-height: 44px;
    }

    .dns-fail-output summary {
      min-height: 44px;
    }
  }
</style>
