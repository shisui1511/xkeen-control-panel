<script lang="ts">
  // Статус замера в нижней строке плитки узла (режим «панель» ProxyGroupCard).
  // Замер активного ядра — число с цветом задержки; запасной TLS-замер — с меткой «TLS»
  // (D-07); замер другим ядром — приглушён и с меткой ядра (D-20); возраст замера (D-05).
  // «Таймаут» и «Ошибка» — отдельная кнопка: нажатие не выбирает узел, а открывает окно
  // с причиной и входом в диагностику (D-25).
  import { untrack } from 'svelte';
  import type { Attachment } from 'svelte/attachments';
  import { t } from '../../i18n';
  import Button from '../Button.svelte';
  import { formatTimeAgo } from '../../lib/batchLatencyTester';
  import { LATENCY_FAST_MS, LATENCY_DEGRADED_MS } from '../../lib/proxyClassification';
  import type { ResultView } from '../../lib/proxyRuntimeAdapter';

  interface Props {
    result: ResultView;
    groupId: string;
    index: number;
    onDiagnostics: () => void;
    onHistory: (e: MouseEvent) => void;
  }

  let { result, groupId, index, onDiagnostics, onHistory }: Props = $props();

  // Причины, у которых есть текст proxies.rt.reason.<код>; неизвестный код показывается как есть
  const REASON_KEYS: Record<string, string> = {
    timeout: 'proxies.rt.reason.timeout',
    connect_failed: 'proxies.rt.reason.connect_failed',
    tunnel_failed: 'proxies.rt.reason.tunnel_failed',
    tls_failed: 'proxies.rt.reason.tls_failed',
    resolve_failed: 'proxies.rt.reason.resolve_failed',
    delay_failed: 'proxies.rt.reason.delay_failed',
    start_failed: 'proxies.rt.reason.start_failed'
  };

  let statusEl: HTMLButtonElement | null = $state(null);
  let top = $state(0);
  let left = $state(0);

  const failed = $derived(result.state === 'timeout' || result.state === 'error');
  // Окно открывает нажатие (переопределение значения); смена исхода замера закрывает
  // его: у «ok» кнопки-якоря нет, и окно не должно висеть на прежнем месте
  let open = $derived.by(() => {
    void failed;
    return false;
  });
  const other = $derived(result.otherKernel !== undefined);
  // Цвет задержки — только у замера активного ядра (D-20)
  const latClass = $derived.by(() => {
    if (other || result.state !== 'ok') return 'dim';
    const ms = result.ms ?? 0;
    if (ms > 0 && ms < LATENCY_FAST_MS) return 'ok';
    if (ms > 0 && ms < LATENCY_DEGRADED_MS) return 'mid';
    return 'bad';
  });
  const ago = $derived(result.at !== undefined ? formatTimeAgo(result.at, $t) : '');
  const statusText = $derived(
    result.state === 'timeout' ? $t('proxies.rt.status.timeout') : $t('proxies.rt.status.error')
  );
  const reasonText = $derived.by(() => {
    const code = result.reason ?? result.state;
    const key = REASON_KEYS[code];
    return key ? $t(key) : code;
  });

  function close(refocus: boolean) {
    if (!open) return;
    open = false;
    if (refocus) statusEl?.focus();
  }

  function toggle(e: MouseEvent) {
    // Нажатие на статус не выбирает узел и не закрывает окно сразу же (D-25)
    e.stopPropagation();
    if (open) {
      close(true);
      return;
    }
    open = true;
  }

  function openDiagnostics() {
    close(false);
    onDiagnostics();
  }

  // Окно у статуса выносится в body: сетка плиток и карточка обрезают всё, что выходит
  // за их край. Позиция пересчитывается при прокрутке и смене размера окна; Esc и клик
  // вне окна закрывают его, фокус возвращается на статус.
  const popover: Attachment<HTMLElement> = (node) => {
    document.body.appendChild(node);

    function place() {
      if (!statusEl) return;
      const anchor = statusEl.getBoundingClientRect();
      const box = node.getBoundingClientRect();
      const pad = 8;
      let y = anchor.bottom + pad / 2;
      if (y + box.height > window.innerHeight - pad) y = anchor.top - box.height - pad / 2;
      let x = anchor.left;
      if (x + box.width > window.innerWidth - pad) x = window.innerWidth - box.width - pad;
      top = Math.max(pad, y);
      left = Math.max(pad, x);
    }

    function onPointerDown(e: PointerEvent) {
      const target = e.target as Node | null;
      if (node.contains(target) || statusEl?.contains(target)) return;
      close(false);
    }

    function onKeyDown(e: KeyboardEvent) {
      if (e.key === 'Escape') {
        e.preventDefault();
        close(true);
      }
    }

    // statusEl читается без отслеживания: смена якоря не перезапускает вложение
    // (перенос узла и перехват фокуса)
    untrack(place);
    node.querySelector<HTMLElement>('button')?.focus();
    window.addEventListener('pointerdown', onPointerDown, true);
    window.addEventListener('keydown', onKeyDown);
    window.addEventListener('resize', place);
    window.addEventListener('scroll', place, true);

    return () => {
      window.removeEventListener('pointerdown', onPointerDown, true);
      window.removeEventListener('keydown', onKeyDown);
      window.removeEventListener('resize', place);
      window.removeEventListener('scroll', place, true);
      node.remove();
    };
  };
</script>

<span class="pns" class:pns-other={other}>
  {#if result.state === 'none'}
    <span class="pns-dim">{$t('proxies.rt.node.not_measured')}</span>
  {:else}
    {#if result.state === 'ok'}
      <button
        type="button"
        class="lat {latClass}"
        onclick={(e) => {
          e.stopPropagation();
          onHistory(e);
        }}>{result.ms} {$t('app.ms')}</button
      >
    {:else if failed}
      <button
        bind:this={statusEl}
        type="button"
        class="pns-fail"
        class:pns-fail-other={other}
        data-testid="proxy-node-status-{groupId}-{index}"
        aria-haspopup="dialog"
        aria-expanded={open}
        onclick={toggle}>{statusText}</button
      >
    {/if}
    {#if result.tls}
      <span class="badge badge-neutral" title={$t('proxies.rt.node.tls_title')}
        >{$t('proxies.rt.node.tls')}</span
      >
    {/if}
    {#if result.otherKernel}
      <span
        class="badge badge-neutral"
        title={$t('proxies.rt.node.via_kernel_title', {
          kernel: result.otherKernel,
          active: result.active ?? ''
        })}>{$t('proxies.rt.node.via_kernel', { kernel: result.otherKernel })}</span
      >
    {/if}
    {#if ago}
      <span class="pns-age" title={$t('proxies.stale_tooltip', { timeAgo: ago })}>{ago}</span>
    {/if}
  {/if}
</span>

{#if open}
  <div
    {@attach popover}
    class="pns-pop"
    style:top="{top}px"
    style:left="{left}px"
    role="dialog"
    aria-label={statusText}
  >
    <p class="pns-reason">{$t('proxies.rt.reason_label', { reason: reasonText })}</p>
    <Button variant="secondary" size="sm" onclick={openDiagnostics}>
      {$t('proxies.rt.diagnostics')}
    </Button>
  </div>
{/if}

<style>
  .pns {
    display: inline-flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--spacing-1);
    min-width: 0;
  }
  .pns-dim,
  .pns-age {
    font-size: var(--font-size-xs);
    color: var(--fg-dim);
  }
  .lat {
    border: none;
    cursor: pointer;
    font-family: var(--font-family-mono);
    font-size: var(--font-size-xs);
    font-weight: 400;
    line-height: 1.2;
    padding: var(--spacing-1);
    border-radius: var(--radius-sm);
    white-space: nowrap;
    display: inline-flex;
    align-items: center;
  }
  .lat:focus-visible,
  .pns-fail:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: 1px;
  }
  .lat.ok {
    color: var(--success);
    background: color-mix(in srgb, var(--success) 10%, transparent);
  }
  .lat.mid {
    color: var(--warning);
    background: color-mix(in srgb, var(--warning) 10%, transparent);
  }
  .lat.bad {
    color: var(--danger);
    background: color-mix(in srgb, var(--danger) 10%, transparent);
  }
  /* Не измерено активным ядром: без цвета задержки */
  .lat.dim {
    color: var(--fg-dim);
    background: var(--surface-tint);
  }
  .pns-fail {
    background: none;
    border: none;
    padding: var(--spacing-1);
    border-radius: var(--radius-sm);
    font: inherit;
    font-size: var(--font-size-xs);
    font-weight: 600;
    color: var(--danger);
    cursor: pointer;
    text-decoration: underline dotted;
    text-underline-offset: 2px;
  }
  .pns-fail.pns-fail-other {
    color: var(--fg-dim);
  }
  .pns-pop {
    position: fixed;
    z-index: var(--z-popover);
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    gap: var(--spacing-2);
    max-width: min(320px, calc(100vw - var(--spacing-4)));
    padding: var(--spacing-2);
    background: var(--bg-card);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    box-shadow: var(--shadow-md);
  }
  .pns-reason {
    margin: 0;
    font-family: var(--font-family-mono);
    font-size: var(--font-size-xs);
    color: var(--fg-secondary);
    overflow-wrap: anywhere;
  }
  @media (pointer: coarse), (max-width: 768px) {
    .lat,
    .pns-fail {
      min-height: 44px;
      min-width: 44px;
      justify-content: center;
    }
  }
</style>
