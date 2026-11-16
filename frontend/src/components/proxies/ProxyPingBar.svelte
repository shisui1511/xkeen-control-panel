<script lang="ts">
  // Панель пинга страницы «Прокси» при слое (D-05, D-08, D-19…D-22): «Проверить все»,
  // ход общей пачки, отмена из любой вкладки и итог до следующего запуска.
  import { t, tp } from '../../i18n';
  import { formatTimeAgo } from '../../lib/batchLatencyTester';
  import type { PingBatch } from '../../lib/configProxies';
  import Button from '../Button.svelte';
  import Icon from '../../lib/components/Icon.svelte';

  interface Props {
    /** Последняя пачка с роутера; undefined — пачек ещё не было. */
    batch?: PingBatch;
    /** Время самого свежего замера, мс от эпохи; undefined — замеров нет. */
    lastAt?: number;
    /** Запуск присоединился к уже идущей пачке. */
    joined?: boolean;
    /** Причина, по которой пачка не стартовала или упала; '' — ошибки нет. */
    errorReason?: string;
    /** Ядро перезапускается (409 kernel_restarting): нейтральная строка. */
    restarting?: boolean;
    /** Подпись активного ядра для итога смены ядра. */
    kernel?: string;
    onpingall: () => void;
    oncancel: () => Promise<void>;
  }

  let {
    batch,
    lastAt,
    joined = false,
    errorReason = '',
    restarting = false,
    kernel = '',
    onpingall,
    oncancel
  }: Props = $props();

  const running = $derived(batch?.state === 'running');

  // Ход для читалки экрана — не чаще раза в секунду
  const THROTTLE_MS = 1000;
  let shownDone = $state(0);
  let shownAt = 0;
  $effect(() => {
    const done = batch?.done ?? 0;
    const wait = THROTTLE_MS - (Date.now() - shownAt);
    if (!running || wait <= 0) {
      shownDone = done;
      shownAt = Date.now();
      return;
    }
    const timer = setTimeout(() => {
      shownDone = done;
      shownAt = Date.now();
    }, wait);
    return () => clearTimeout(timer);
  });

  // Возраст самого свежего замера обновляется раз в 30 с
  let clock = $state(0);
  $effect(() => {
    const timer = setInterval(() => (clock += 1), 30_000);
    return () => clearInterval(timer);
  });
  const lastAgo = $derived.by(() => {
    void clock;
    return lastAt !== undefined ? formatTimeAgo(lastAt, $t) : '';
  });

  let root = $state<HTMLElement | null>(null);
  let cancelling = $state(false);

  async function cancel() {
    if (cancelling) return;
    cancelling = true;
    try {
      await oncancel();
    } finally {
      cancelling = false;
    }
    // После отмены фокус — на «Проверить все» (UI-SPEC §8)
    root?.querySelector<HTMLElement>('.ppb-all button')?.focus();
  }

  function pingAll() {
    if (running) return;
    onpingall();
  }
</script>

<div class="ppb" bind:this={root}>
  <div class="ppb-row" class:running>
    <div class="ppb-all">
      <Button
        variant="secondary"
        class="ppb-btn"
        data-testid="proxies-ping-all"
        aria-disabled={running ? 'true' : undefined}
        title={running ? $t('proxies.rt.ping.busy') : undefined}
        onclick={pingAll}
      >
        <Icon name="refresh" size={14} />
        {$t('proxies.rt.ping.all')}
      </Button>
    </div>

    <div class="ppb-status" data-testid="proxies-ping-status" role="status" aria-live="polite">
      {#if running && batch}
        {#if joined}
          <span>{$t('proxies.rt.ping.joined')}</span>
        {/if}
        <span>{$t('proxies.rt.ping.progress', { done: shownDone, total: batch.total })}</span>
      {:else if restarting}
        <span>{$t('proxies.rt.ping.restarting')}</span>
      {:else if batch?.state === 'done'}
        <span>{$tp('proxies.rt.ping.done', batch.total)}</span>
      {:else if batch?.state === 'cancelled'}
        <span>{$t('proxies.rt.ping.cancelled', { done: batch.done, total: batch.total })}</span>
      {:else if batch?.state === 'kernel_changed'}
        <span>{$t('proxies.rt.ping.kernel_changed', { kernel: kernel || 'Xray / Mihomo' })}</span>
      {/if}
      {#if batch && batch.excluded > 0}
        <span class="ppb-secondary">{$tp('proxies.rt.ping.excluded', batch.excluded)}</span>
      {/if}
      {#if !running && lastAgo}
        <span class="ppb-last">{$t('proxies.rt.ping.last', { ago: lastAgo })}</span>
      {/if}
    </div>

    {#if running}
      <div class="ppb-cancel">
        <Button
          variant="secondary"
          class="ppb-btn"
          data-testid="proxies-ping-cancel"
          loading={cancelling}
          onclick={cancel}
        >
          {$t('proxies.rt.ping.cancel')}
        </Button>
      </div>
    {/if}
  </div>

  {#if errorReason !== '' && !running}
    <div class="alert alert-error ppb-error" role="alert">
      {$t('proxies.rt.ping.failed', { reason: errorReason })}
    </div>
  {/if}
</div>

<style>
  .ppb {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-2);
  }

  .ppb-row {
    display: grid;
    grid-template-columns: auto minmax(0, 1fr) auto;
    grid-template-areas: 'all status cancel';
    align-items: center;
    column-gap: var(--spacing-4);
    row-gap: var(--spacing-2);
  }

  .ppb-all {
    grid-area: all;
  }

  .ppb-status {
    grid-area: status;
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    column-gap: var(--spacing-4);
    row-gap: var(--spacing-1);
    min-width: 0;
    font-size: var(--font-size-sm);
    line-height: 1.5;
    color: var(--fg-primary);
    overflow-wrap: anywhere;
  }

  .ppb-secondary {
    color: var(--fg-secondary);
  }

  .ppb-last {
    font-size: var(--font-size-xs);
    font-weight: 600;
    color: var(--fg-dim);
  }

  .ppb-cancel {
    grid-area: cancel;
  }

  .ppb-error {
    margin-bottom: 0;
    overflow-wrap: anywhere;
  }

  /* Узкая раскладка: кнопки в ряд на всю ширину, ход и итог под ними */
  @media (max-width: 768px) {
    .ppb-row {
      grid-template-columns: minmax(0, 1fr);
      grid-template-areas:
        'all'
        'status';
    }

    .ppb-row.running {
      grid-template-columns: minmax(0, 1fr) minmax(0, 1fr);
      grid-template-areas:
        'all cancel'
        'status status';
      column-gap: var(--spacing-2);
    }

    .ppb-row :global(.ppb-btn) {
      width: 100%;
    }
  }
</style>
