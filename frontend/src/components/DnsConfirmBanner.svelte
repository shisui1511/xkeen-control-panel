<script lang="ts">
  import { onMount, untrack } from 'svelte';
  import { createSubscriber } from 'svelte/reactivity';
  import { t } from '../i18n';
  import Icon from '../lib/components/Icon.svelte';
  import Button from './Button.svelte';
  import { showToast } from '../stores';
  import { motion } from '../lib/motion.svelte';
  import {
    dnsTakeover,
    dnsWindowActive,
    dnsClockOffset,
    startDns,
    stopDns,
    loadDns,
    keepDns,
    rollbackDns,
    outcomeSeenDns,
    dnsRequestErrorText,
    dnsRequestHasCode,
    dnsRequestReason,
    dnsKernelTitle,
    dnsWindowSeconds,
    type DnsOutcome,
    type DnsView,
    type DnsWindow
  } from '../lib/configDns';

  // Общий баннер окна подтверждения перехвата DNS (DNSF-05, D-05…D-08, D-25, D-26):
  // монтируется оболочкой при включённом слое «Конфигурация» и виден на любой странице,
  // пока бэкенд сообщает окно. Срок окончания и «сейчас» приходят с сервера: отсчёт
  // считается от смещения часов (dnsClockOffset), таймер только показывает — откат по
  // таймеру делает бэкенд (D-07). Здесь же — однократный тост об исходе без пользователя
  // (D-28): признак «показано» хранится на бэкенде (outcome-seen).

  // Тик отсчёта — 4 раза в секунду, пока окно открыто (читается только при открытом окне);
  // без движения полоса идёт шагами раз в секунду
  const TICK_MS = 250;
  // Нет события о закрытии окна столько после нуля — одно перечитывание состояния
  const STALE_AFTER_ZERO_MS = 10000;
  // Пороги объявления для скринридера, секунды (по возрастанию)
  const ANNOUNCE_AT: readonly number[] = [10, 30];
  const OUTCOME_TOAST_MS = 10000;
  const CARD_HASH = '#/config/cores';

  onMount(() => {
    startDns();
    return () => {
      stopDns();
      setBannerHeight(0);
    };
  });

  const view = $derived($dnsTakeover);
  const win = $derived<DnsWindow | null>($dnsWindowActive ? (view?.window ?? null) : null);

  let keeping = $state(false);
  let rolling = $state(false);
  // Ошибка относится к окну с этим сроком: новое окно её не показывает
  let error = $state<{ deadline: number; text: string } | null>(null);
  let height = $state(0);

  const subscribeTick = createSubscriber((update) => {
    const id = setInterval(update, TICK_MS);
    return () => clearInterval(id);
  });
  function now(): number {
    subscribeTick();
    return Date.now();
  }

  /** Остаток окна по часам сервера, мс; отрицательный — срок прошёл. */
  function remainingMs(w: DnsWindow, at: number): number {
    return w.deadline_ms - (at + $dnsClockOffset);
  }

  const rawLeftMs = $derived(win ? remainingMs(win, now()) : 0);
  const leftMs = $derived(Math.max(0, rawLeftMs));
  const seconds = $derived(Math.ceil(leftMs / 1000));
  const totalMs = $derived((win && win.total_s > 0 ? win.total_s : $dnsWindowSeconds) * 1000);
  const fraction = $derived(Math.min(1, (motion.reduced ? seconds * 1000 : leftMs) / totalMs));
  const expired = $derived(win !== null && leftMs <= 0);
  const busy = $derived(keeping || rolling || expired);
  const errorText = $derived(
    error !== null && error.deadline === win?.deadline_ms ? error.text : ''
  );

  // Тело баннера называет остаток на момент появления окна и дальше не меняется: иначе
  // область role="alert" озвучивалась бы каждую секунду
  const bodySeconds = $derived.by(() => {
    const w = win;
    if (!w) return 0;
    return untrack(() => Math.max(0, Math.ceil(remainingMs(w, Date.now()) / 1000)));
  });

  // Скрытая зона aria-live: «Осталось 30 с» и «Осталось 10 с» — только пороги, пройденные
  // при открытом баннере; текст меняется лишь на пороге, поэтому объявляется один раз
  const srText = $derived.by(() => {
    if (!win || seconds <= 0) return '';
    for (const mark of ANNOUNCE_AT) {
      if (seconds <= mark && bodySeconds > mark) {
        return $t('cfg.dns.countdown_sr', { seconds: String(mark) });
      }
    }
    return '';
  });

  const bodyText = $derived.by(() => {
    if (!win) return '';
    const params = { kernel: dnsKernelTitle(view?.kernel), seconds: String(bodySeconds) };
    if (win.kind === 'apply') return $t('cfg.dns.banner_apply', params);
    if (win.kind === 'kernel_switch') return $t('cfg.dns.banner_switch', params);
    return $t('cfg.dns.banner_enable', params);
  });

  // Окно не закрылось за 10 с после нуля (событие потерялось) — одно перечитывание
  let staleReloadedFor = 0;
  $effect(() => {
    if (!win || rawLeftMs > -STALE_AFTER_ZERO_MS || staleReloadedFor === win.deadline_ms) return;
    staleReloadedFor = win.deadline_ms;
    void loadDns();
  });

  // Высота баннера для смещения липких блоков страниц (DraftBar): 0, пока окна нет
  function setBannerHeight(px: number): void {
    document.documentElement.style.setProperty('--dns-banner-h', `${px}px`);
  }
  $effect(() => {
    setBannerHeight(win ? height : 0);
  });

  async function keep(): Promise<void> {
    if (busy || !win) return;
    const deadline = win.deadline_ms;
    keeping = true;
    error = null;
    try {
      await keepDns();
      showToast('success', $t('cfg.dns.toast_kept'));
    } catch (err) {
      // Ответ с кодом (окна уже нет, идёт другая операция) — свой текст; сеть и прочие
      // сбои — «Не удалось подтвердить: {причина}. Окно продолжается.»
      const text = dnsRequestHasCode(err)
        ? dnsRequestErrorText(err)
        : $t('cfg.dns.keep_error', { reason: dnsRequestReason(err) });
      error = { deadline, text };
    } finally {
      keeping = false;
    }
  }

  /**
   * Ждёт, пока бэкенд закроет окно и завершит транзакцию отката. null — слежение снято
   * (флаг слоя выключен), итог неизвестен.
   */
  function windowClosed(): Promise<DnsView | null> {
    return new Promise((resolve) => {
      let done = false;
      let unsub: (() => void) | null = null;
      unsub = dnsTakeover.subscribe((v) => {
        if (done) return;
        const open = v !== null && (v.state === 'window' || v.state === 'applying');
        if (open) return;
        done = true;
        resolve(v);
        unsub?.();
      });
      if (done) unsub();
    });
  }

  async function rollback(): Promise<void> {
    if (busy || !win) return;
    const kind = win.kind;
    const deadline = win.deadline_ms;
    rolling = true;
    error = null;
    try {
      await rollbackDns();
      const after = await windowClosed();
      // Сбой транзакции отката показывает карточка; без вида итог неизвестен
      if (after !== null && !after.failure) {
        showToast(
          'info',
          kind === 'apply' ? $t('cfg.dns.toast_rolled_apply') : $t('cfg.dns.toast_rolled')
        );
      }
    } catch (err) {
      error = { deadline, text: dnsRequestErrorText(err) };
    } finally {
      rolling = false;
    }
  }

  // --- Однократный тост об исходе без пользователя (D-28) ---

  /** Время сервера в локальных часах: «ЧЧ:ММ:СС». */
  function clock(serverMs: number): string {
    const d = new Date(serverMs - $dnsClockOffset);
    return [d.getHours(), d.getMinutes(), d.getSeconds()]
      .map((n) => String(n).padStart(2, '0'))
      .join(':');
  }

  function outcomeToastText(o: DnsOutcome): string | null {
    switch (o.kind) {
      case 'timeout':
      case 'probe_failed':
        return $t('cfg.dns.toast_timeout', { seconds: String($dnsWindowSeconds) });
      case 'watchdog':
        return $t('cfg.dns.outcome_watchdog', {
          kernel: dnsKernelTitle(o.kernel || view?.kernel),
          time: clock(o.at_ms)
        });
      case 'kernel_changed':
        return $t('cfg.dns.outcome_kernel_changed', {
          from: dnsKernelTitle(o.from),
          to: dnsKernelTitle(o.to),
          time: clock(o.at_ms)
        });
      default:
        // Ожидание ядра и ручной откат тоста не дают (UI-SPEC «Сообщения об откате»)
        return null;
    }
  }

  // Один и тот же исход не показывается дважды, пока outcome-seen не дошёл до сервера
  let shownOutcomeAt = 0;
  $effect(() => {
    const o = view?.outcome;
    if (!o || !o.toast_pending || o.at_ms === shownOutcomeAt) return;
    shownOutcomeAt = o.at_ms;
    untrack(() => {
      const text = outcomeToastText(o);
      if (text !== null) {
        showToast('warning', text, OUTCOME_TOAST_MS, {
          label: $t('cfg.dns.toast_open'),
          onClick: () => {
            window.location.hash = CARD_HASH;
          }
        });
      }
    });
    // Не дошло — признак останется на сервере, тост покажется при следующем открытии панели
    outcomeSeenDns().catch(() => {});
  });
</script>

{#if win}
  <div
    class="alert alert-warning dns-banner"
    role="alert"
    aria-label={$t('cfg.dns.banner_title')}
    aria-busy={expired}
    data-testid="dns-confirm-banner"
    data-kind={win.kind}
    bind:offsetHeight={height}
  >
    <span class="dns-banner__icon icon-first-line" aria-hidden="true">
      <Icon name="warning" size={18} />
    </span>
    <div class="dns-banner__main">
      <strong>{$t('cfg.dns.banner_title')}</strong>
      {#if expired}
        <p class="dns-banner__body" data-testid="dns-confirm-rolling">
          {$t('cfg.dns.banner_rolling')}
        </p>
      {:else}
        <p class="dns-banner__body">{bodyText}</p>
      {/if}
      <div class="dns-banner__countdown">
        <div class="dns-banner__track" role="presentation">
          <div
            class={['dns-banner__fill', { 'dns-banner__fill--smooth': !motion.reduced }]}
            style:width="{fraction * 100}%"
          ></div>
        </div>
        <span
          class="dns-banner__timer"
          role="timer"
          aria-live="off"
          data-testid="dns-confirm-seconds"
          data-seconds={seconds}
        >
          {$t('cfg.dns.countdown', { seconds: String(seconds) })}
        </span>
      </div>
      {#if errorText}
        <p class="error-text dns-banner__error" data-testid="dns-confirm-error">{errorText}</p>
      {/if}
      <span class="sr-only" aria-live="polite">{srText}</span>
    </div>
    <div class="dns-banner__actions">
      <Button
        variant="primary"
        class="btn-sm"
        data-testid="dns-confirm-keep"
        loading={keeping}
        disabled={busy}
        onclick={keep}
      >
        {$t('cfg.dns.keep_btn')}
      </Button>
      <Button
        variant="secondary"
        class="btn-sm"
        data-testid="dns-confirm-rollback"
        loading={rolling}
        disabled={busy}
        onclick={rollback}
      >
        {$t('cfg.dns.rollback_btn')}
      </Button>
    </div>
  </div>
{/if}

<style>
  .dns-banner {
    position: sticky;
    top: 0;
    z-index: 6;
    margin: var(--spacing-2) var(--spacing-4) 0;
    padding: var(--spacing-2) var(--spacing-4);
    justify-content: space-between;
    align-items: flex-start;
    gap: var(--spacing-4);
    /* Под полупрозрачным градиентом .alert-warning — непрозрачная подложка: содержимое
       страницы при прокрутке не просвечивает сквозь приклеенный баннер */
    background-color: var(--bg-card);
  }

  /* Иконка по первой строке заголовка (Heading 16/1.2): высота .icon-first-line — 1lh */
  .dns-banner__icon {
    display: inline-flex;
    flex: none;
    font-size: var(--font-size-lg);
    line-height: 1.2;
  }

  .dns-banner__main {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: var(--spacing-2);
  }

  .dns-banner__main strong {
    font-size: var(--font-size-lg);
    font-weight: 600;
    line-height: 1.2;
    overflow-wrap: anywhere;
  }

  .dns-banner__body {
    margin: 0;
    font-size: var(--font-size-base);
    font-weight: 400;
    line-height: 1.5;
    color: var(--fg-primary);
    overflow-wrap: anywhere;
  }

  .dns-banner__countdown {
    display: flex;
    align-items: center;
    gap: var(--spacing-2);
    min-width: 0;
  }

  .dns-banner__track {
    flex: 1;
    min-width: 0;
    height: var(--spacing-1);
    border-radius: var(--radius-sm);
    background: var(--border);
    overflow: hidden;
  }

  .dns-banner__fill {
    height: 100%;
    background: var(--warning);
  }

  .dns-banner__fill--smooth {
    transition: width 250ms linear;
  }

  .dns-banner__timer {
    flex: none;
    font-size: var(--font-size-sm);
    font-weight: 600;
    line-height: 1.4;
    font-variant-numeric: tabular-nums;
    color: var(--warning);
  }

  .dns-banner__error {
    margin: 0;
    overflow-wrap: anywhere;
  }

  .dns-banner__actions {
    display: flex;
    flex: none;
    flex-wrap: wrap;
    gap: var(--spacing-2);
    align-items: center;
  }

  @media (pointer: coarse), (max-width: 768px) {
    .dns-banner__actions :global(.btn) {
      min-height: 44px;
    }
  }

  @media (max-width: 768px) {
    .dns-banner {
      flex-wrap: wrap;
      top: calc(var(--mhdr-h) + env(safe-area-inset-top));
    }

    .dns-banner__main {
      flex: 1 1 0;
    }

    .dns-banner__actions {
      flex: 1 1 100%;
    }

    .dns-banner__actions :global(.btn) {
      flex: 1 1 auto;
    }
  }

  @media (max-width: 480px) {
    .dns-banner__actions {
      flex-direction: column;
      align-items: stretch;
    }

    .dns-banner__actions :global(.btn) {
      width: 100%;
    }
  }
</style>
