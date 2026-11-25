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
  import { ageTick } from '../../lib/clockTick';
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
  // Возраст замера обновляется общим тиком раз в 30 с, как «Последняя проверка» панели пинга
  const ago = $derived.by(() => {
    void $ageTick;
    return result.at !== undefined ? formatTimeAgo(result.at, $t) : '';
  });

  // Пояснение метки «TLS» или «через {ядро}»: по нажатию, а не только title (клавиатура, касание)
  let hint = $state<'tls' | 'kernel' | null>(null);
  const hintText = $derived(
    hint === 'tls'
      ? $t('proxies.rt.node.tls_title')
      : hint === 'kernel'
        ? $t('proxies.rt.node.via_kernel_title', {
            kernel: result.otherKernel ?? '',
            active: result.active ?? ''
          })
        : ''
  );
  const hintTitleKernel = $derived(
    $t('proxies.rt.node.via_kernel_title', {
      kernel: result.otherKernel ?? '',
      active: result.active ?? ''
    })
  );
  const hintId = $derived(`pns-hint-${groupId}-${index}`);
  let tagEls = $state<HTMLElement[]>([]);
  function toggleHint(kind: 'tls' | 'kernel', e: MouseEvent) {
    e.stopPropagation();
    hint = hint === kind ? null : kind;
  }

  // Пояснение закрывается по Esc и нажатию вне метки и самого пояснения
  $effect(() => {
    if (hint === null) return;
    function onKey(e: KeyboardEvent) {
      if (e.key === 'Escape') hint = null;
    }
    function onDown(e: PointerEvent) {
      const target = e.target as Node | null;
      if (target && tagEls.some((el) => el?.contains(target))) return;
      if (target && document.getElementById(hintId)?.contains(target)) return;
      hint = null;
    }
    window.addEventListener('keydown', onKey);
    window.addEventListener('pointerdown', onDown, true);
    return () => {
      window.removeEventListener('keydown', onKey);
      window.removeEventListener('pointerdown', onDown, true);
    };
  });
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
    // Фокус — на статус до окна диагностики: Modal запоминает его и вернёт по закрытию
    close(true);
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
        return;
      }
      // Окно вынесено в конец body: Tab изнутри увёл бы фокус из страницы. Закрыть окно и
      // вернуть фокус на статус, следующий Tab пойдёт по плиткам
      if (e.key === 'Tab' && node.contains(document.activeElement)) {
        e.preventDefault();
        // Фокус на самом окне (после нажатия мышью на текст причины): Tab ведёт на его кнопку
        if (document.activeElement === node && !e.shiftKey) {
          node.querySelector<HTMLElement>('button')?.focus();
          return;
        }
        close(true);
      }
    }

    // Окно не модальное: фокус ушёл из него (Tab) — закрыть, не оставляя висеть
    function onFocusOut(e: FocusEvent) {
      const next = e.relatedTarget as Node | null;
      // Нажатие внутри окна (или на его кнопку в WebKit) даёт relatedTarget === null:
      // нажатия вне окна закрывает onPointerDown, здесь — только уход фокуса на другой элемент
      if (!next || node.contains(next) || statusEl?.contains(next)) return;
      close(false);
    }
    node.addEventListener('focusout', onFocusOut);

    // statusEl читается без отслеживания: смена якоря не перезапускает вложение
    // (перенос узла и перехват фокуса)
    untrack(place);
    node.querySelector<HTMLElement>('button')?.focus();
    window.addEventListener('pointerdown', onPointerDown, true);
    window.addEventListener('keydown', onKeyDown);
    window.addEventListener('resize', place);
    window.addEventListener('scroll', place, true);

    return () => {
      node.removeEventListener('focusout', onFocusOut);
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
        title={$t('proxies.rt.node.history_title')}
        aria-label={$t('proxies.rt.node.history_aria', { ms: result.ms ?? 0 })}
        onclick={(e) => {
          e.stopPropagation();
          onHistory(e);
        }}
      >
        {result.ms}
        {$t('app.ms')}
      </button>
    {:else if failed}
      <button
        bind:this={statusEl}
        type="button"
        class="pns-fail"
        class:pns-fail-other={other}
        data-testid="proxy-node-status-{groupId}-{index}"
        aria-haspopup="dialog"
        aria-expanded={open}
        onclick={toggle}
      >
        {statusText}
      </button>
    {/if}
    {#if result.tls}
      <button
        type="button"
        class="badge badge-neutral pns-tag tap-zone-44"
        aria-expanded={hint === 'tls'}
        aria-controls={hint === 'tls' ? hintId : undefined}
        bind:this={tagEls[0]}
        title={$t('proxies.rt.node.tls_title')}
        onclick={(e) => toggleHint('tls', e)}
      >
        {$t('proxies.rt.node.tls')}
      </button>
    {/if}
    {#if result.otherKernel}
      <button
        type="button"
        class="badge badge-neutral pns-tag tap-zone-44"
        aria-expanded={hint === 'kernel'}
        aria-controls={hint === 'kernel' ? hintId : undefined}
        bind:this={tagEls[1]}
        title={hintTitleKernel}
        onclick={(e) => toggleHint('kernel', e)}
      >
        {$t('proxies.rt.node.via_kernel', { kernel: result.otherKernel })}
      </button>
    {/if}
    {#if ago}
      <span class="pns-age" title={$t('proxies.rt.node.age_title', { timeAgo: ago })}>{ago}</span>
    {/if}
    {#if hint !== null}
      <span class="pns-hint" id={hintId}>{hintText}</span>
    {/if}
  {/if}
</span>

{#if open}
  <div
    {@attach popover}
    class="pns-pop"
    tabindex="-1"
    style:top="{top}px"
    style:left="{left}px"
    role="dialog"
    aria-label={statusText}
  >
    <p class="pns-reason">{$t('proxies.rt.reason_label', { reason: reasonText })}</p>
    <Button variant="secondary" size="sm" class="tap-zone-44" onclick={openDiagnostics}>
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
    font-weight: 600;
    color: var(--fg-dim);
  }
  .lat {
    border: none;
    cursor: pointer;
    font-family: var(--font-family-mono);
    font-size: var(--font-size-xs);
    font-weight: 400;
    line-height: 1.5;
    padding: var(--spacing-1);
    border-radius: var(--radius-sm);
    white-space: nowrap;
    display: inline-flex;
    align-items: center;
  }
  .lat:focus-visible,
  .pns-fail:focus-visible,
  .pns-tag:focus-visible {
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
  .pns-tag {
    cursor: pointer;
    font-family: inherit;
  }
  /* Без смены фона: --hover несёт акцентный оттенок, а на метках акцента нет */
  .pns-tag:hover,
  .pns-tag[aria-expanded='true'] {
    border-color: var(--border-strong);
    color: var(--fg-primary);
  }
  .pns-pop:focus {
    outline: none;
  }
  .pns-hint {
    flex: 1 0 100%;
    font-size: var(--font-size-xs);
    font-weight: 600;
    line-height: 1.5;
    color: var(--fg-secondary);
    overflow-wrap: anywhere;
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
