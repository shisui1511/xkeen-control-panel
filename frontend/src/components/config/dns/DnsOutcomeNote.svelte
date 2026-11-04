<script lang="ts">
  import { t } from '../../../i18n';
  import Icon from '../../../lib/components/Icon.svelte';
  import {
    dnsClockOffset,
    dnsKernelTitle,
    type DnsOutcome,
    type DnsState
  } from '../../../lib/configDns';

  // Строки исхода без пользователя в карточке перехвата (D-28): откат по таймеру, возврат
  // сторожем, смена ядра вне панели, непрошедшая проба после отката. Строка висит до
  // следующего действия пользователя (карточка скрывает её сама, бэкенд снимает исход тем же
  // действием). Ожидание ядра (D-14, D-16) — нейтральная строка без тоста.
  let {
    outcome,
    state,
    kernel = ''
  }: {
    /** Исход из вида перехвата; undefined — строки исхода нет. */
    outcome: DnsOutcome | undefined;
    /** Состояние перехвата: в «Ждёт ядро» показывается строка ожидания. */
    state: DnsState;
    /** Ядро-перехватчик из вида: подставляется, если в исходе ядро не пришло. */
    kernel?: string;
  } = $props();

  /** Время сервера в локальных часах: «ЧЧ:ММ:СС». */
  function clock(serverMs: number): string {
    const d = new Date(serverMs - $dnsClockOffset);
    return [d.getHours(), d.getMinutes(), d.getSeconds()]
      .map((n) => String(n).padStart(2, '0'))
      .join(':');
  }

  // Текст строки по виду исхода; ручной откат («Откатить сейчас») строки не даёт: решение
  // принял сам пользователь
  const text = $derived.by<string | null>(() => {
    const o = outcome;
    if (!o) return null;
    const time = clock(o.at_ms);
    switch (o.kind) {
      case 'timeout':
        return $t('cfg.dns.outcome_timeout', { time });
      case 'probe_failed':
        return $t('cfg.dns.outcome_probe_failed', { reason: $t('cfg.dns.reason_probe'), time });
      case 'watchdog':
        return $t('cfg.dns.outcome_watchdog', { kernel: dnsKernelTitle(o.kernel || kernel), time });
      case 'kernel_changed':
        return $t('cfg.dns.outcome_kernel_changed', {
          from: dnsKernelTitle(o.from),
          to: dnsKernelTitle(o.to),
          time
        });
      default:
        return null;
    }
  });
</script>

{#if outcome && text !== null}
  <div
    class="alert alert-warning dns-outcome"
    role="status"
    data-testid="dns-outcome"
    data-kind={outcome.kind}
  >
    <Icon name="warning" size={16} class="dns-outcome-icon" />
    <span class="dns-outcome-text">{text}</span>
  </div>
{/if}
{#if state === 'waiting'}
  <p class="dns-outcome-wait" role="status" data-testid="dns-outcome" data-kind="waiting">
    <Icon name="info" size={14} class="dns-outcome-icon" />
    <span class="dns-outcome-text">{$t('cfg.dns.outcome_waiting')}</span>
  </p>
{/if}

<style>
  .dns-outcome {
    margin: 0;
    min-width: 0;
    gap: var(--spacing-2);
    padding: var(--spacing-2) var(--spacing-4);
    font-size: var(--font-size-base);
    line-height: 1.5;
  }

  .dns-outcome-wait {
    display: flex;
    align-items: flex-start;
    gap: var(--spacing-1);
    margin: 0;
    min-width: 0;
    font-size: var(--font-size-sm);
    line-height: 1.5;
    color: var(--fg-secondary);
  }

  .dns-outcome :global(.dns-outcome-icon),
  .dns-outcome-wait :global(.dns-outcome-icon) {
    flex-shrink: 0;
    margin-top: 2px;
  }

  .dns-outcome-text {
    min-width: 0;
    overflow-wrap: anywhere;
  }
</style>
