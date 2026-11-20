<script lang="ts">
  import { onMount } from 'svelte';
  import type { Attachment } from 'svelte/attachments';
  import { t } from '../../i18n';
  import Modal from '../Modal.svelte';
  import Button from '../Button.svelte';
  import Skeleton from '../Skeleton.svelte';
  import Icon from '../../lib/components/Icon.svelte';
  import { showToast } from '../../stores';
  import { layerSnapshot } from '../../lib/configLayer';
  import {
    legacyView,
    legacyLoadError,
    watchLegacy,
    closeLegacy,
    dismissLegacy
  } from '../../lib/configLegacy';

  // Окно чистого старта (фаза 151, D-06, UI-SPEC S1). Рисуется, пока сервер сообщает, что
  // окно не закрыто; закрытие пишется на сервер. Адреса прежних подписок — секрет: только
  // текст в <code>, без атрибутов, тостов и консоли; у кнопки копирования — номер адреса.

  const COPIED_MS = 2000;
  // Хвост адреса, который не обрезается: у адресов подписок общий префикс, различается конец
  const URL_TAIL = 14;

  const view = $derived($legacyView);
  const archivePath = $derived(view?.archive_path ?? $layerSnapshot?.legacy.archive_path ?? '');
  const unavailable = $derived($legacyLoadError || (view !== null && !view.urls_available));

  let settingUp = $state(false);
  let copied = $state<number | null>(null);
  let copiedTimer: ReturnType<typeof setTimeout> | null = null;
  // Узлы адресов по номеру строки: запасной путь копирования выделяет текст узла
  const codeEls: Record<number, HTMLElement | undefined> = {};

  // Пока окно на экране — список читается и перечитывается по событию legacy; при снятии
  // окна адреса забываются
  onMount(() => {
    const stop = watchLegacy();
    return () => {
      stop();
      if (copiedTimer !== null) clearTimeout(copiedTimer);
    };
  });

  function codeNode(i: number): Attachment<HTMLElement> {
    return (node) => {
      codeEls[i] = node;
      return () => {
        if (codeEls[i] === node) codeEls[i] = undefined;
      };
    };
  }

  // Начальный фокус — «Настроить VPN». Modal ставит фокус на первый элемент в следующей
  // задаче; перенос идёт задачей позже, чтобы его не перебить
  const focusPrimary: Attachment<HTMLElement> = (node) => {
    let inner: ReturnType<typeof setTimeout> | null = null;
    const outer = setTimeout(() => {
      inner = setTimeout(() => node.focus(), 0);
    }, 0);
    return () => {
      clearTimeout(outer);
      if (inner !== null) clearTimeout(inner);
    };
  };

  const urlHead = (url: string): string => url.slice(0, Math.max(0, url.length - URL_TAIL));
  const urlTail = (url: string): string => url.slice(Math.max(0, url.length - URL_TAIL));

  function markCopied(i: number): void {
    copied = i;
    if (copiedTimer !== null) clearTimeout(copiedTimer);
    copiedTimer = setTimeout(() => {
      copied = null;
      copiedTimer = null;
    }, COPIED_MS);
  }

  /**
   * Запасной путь без буфера обмена (страница по http, отказ браузера): адрес выделяется в
   * разметке и копируется командой копирования выделения. Выделение берёт текст узла
   * целиком, а не видимую обрезанную часть. Не вышло — адрес остаётся выделенным.
   */
  function copyBySelection(el: HTMLElement | undefined): boolean {
    const sel = window.getSelection();
    if (!el || !sel) return false;
    sel.removeAllRanges();
    const range = document.createRange();
    range.selectNodeContents(el);
    sel.addRange(range);
    let ok = false;
    try {
      ok = document.execCommand('copy');
    } catch {
      ok = false;
    }
    if (ok) sel.removeAllRanges();
    return ok;
  }

  async function copy(i: number, url: string): Promise<void> {
    let ok = false;
    try {
      if (navigator.clipboard && window.isSecureContext) {
        await navigator.clipboard.writeText(url);
        ok = true;
      }
    } catch {
      ok = false;
    }
    if (!ok) ok = copyBySelection(codeEls[i]);
    if (ok) markCopied(i);
    else showToast('warning', $t('cfg.reset.copy_failed'));
  }

  // Пока идёт запись закрытия из «Настроить VPN», второе закрытие не отправляется
  function closeWindow(): void {
    if (!settingUp) dismissLegacy();
  }

  // «Настроить VPN»: сначала запись закрытия, затем мастер — и при сбое записи (тост
  // показывает closeLegacy)
  async function setup(): Promise<void> {
    if (settingUp) return;
    settingUp = true;
    try {
      await closeLegacy();
    } finally {
      settingUp = false;
    }
    window.location.hash = '#/config/setup';
  }
</script>

<Modal
  open={true}
  title={$t('cfg.reset.title')}
  maxWidth="560px"
  testid="legacy-reset-window"
  onclose={closeWindow}
>
  <div class="legacy-reset">
    <div class="legacy-intro">
      <p class="legacy-heading">{$t('cfg.reset.heading')}</p>
      <p class="legacy-body">
        {archivePath ? $t('cfg.reset.body', { path: archivePath }) : $t('cfg.reset.body_no_path')}
      </p>
    </div>

    <div class="legacy-list-block" aria-busy={view === null && !unavailable}>
      {#if unavailable}
        <p class="legacy-body" data-testid="legacy-reset-unavailable">
          {archivePath
            ? $t('cfg.reset.urls_unavailable', { path: archivePath })
            : $t('cfg.reset.urls_unavailable_no_path')}
        </p>
      {:else if view === null}
        <span class="sr-only">{$t('cfg.reset.urls_loading')}</span>
        <Skeleton type="text-line" height="var(--btn-h)" />
      {:else if view.urls.length === 0}
        <p class="legacy-body" data-testid="legacy-reset-empty">{$t('cfg.reset.urls_empty')}</p>
      {:else}
        <p class="legacy-label" id="legacy-reset-urls-label">{$t('cfg.reset.urls_label')}</p>
        <ul
          class="legacy-urls"
          aria-labelledby="legacy-reset-urls-label"
          data-testid="legacy-reset-urls"
        >
          {#each view.urls as url, i (i)}
            <li class="legacy-url-row">
              <code class="legacy-url" {@attach codeNode(i)}
                ><span class="legacy-url-head">{urlHead(url)}</span><span class="legacy-url-tail"
                  >{urlTail(url)}</span
                ></code
              >
              <Button
                variant="secondary"
                size="sm"
                ariaLabel={copied === i
                  ? $t('app.copied')
                  : $t('cfg.reset.copy_aria', { n: i + 1 })}
                testid="legacy-reset-copy"
                onclick={() => void copy(i, url)}
              >
                <Icon name={copied === i ? 'check' : 'copy'} size={14} />
                {copied === i ? $t('app.copied') : $t('app.copy')}
              </Button>
            </li>
          {/each}
        </ul>
        <p class="legacy-label legacy-secret">{$t('cfg.reset.urls_secret')}</p>
      {/if}
    </div>

    <span class="sr-only" aria-live="polite">{copied !== null ? $t('app.copied') : ''}</span>

    <div class="legacy-actions">
      <Button
        variant="primary"
        loading={settingUp}
        testid="legacy-reset-setup"
        onclick={() => void setup()}
        {@attach focusPrimary}
      >
        {$t('cfg.reset.setup')}
      </Button>
      <Button variant="ghost" testid="legacy-reset-close" onclick={closeWindow}>
        {$t('app.close')}
      </Button>
    </div>
  </div>
</Modal>

<style>
  .legacy-reset {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-6);
    min-width: 0;
  }

  .legacy-intro {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-4);
    min-width: 0;
  }

  .legacy-reset p {
    margin: 0;
  }

  .legacy-heading {
    font-size: var(--font-size-base);
    font-weight: 600;
    line-height: 1.5;
    color: var(--fg-primary);
  }

  .legacy-body {
    font-size: var(--font-size-base);
    line-height: 1.5;
    color: var(--fg-secondary);
    overflow-wrap: anywhere;
  }

  .legacy-label {
    font-size: var(--font-size-sm);
    line-height: 1.5;
    color: var(--fg-secondary);
  }

  .legacy-list-block {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-2);
    min-width: 0;
  }

  .legacy-urls {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: var(--spacing-2);
    max-height: min(40vh, 280px);
    overflow-y: auto;
  }

  .legacy-url-row {
    display: flex;
    align-items: center;
    gap: var(--spacing-2);
    min-width: 0;
  }

  /* Одна строка: многоточие в середине, начало и конец видны, копируется адрес целиком */
  .legacy-url {
    flex: 1 1 auto;
    min-width: 0;
    overflow: hidden;
    white-space: nowrap;
    padding: var(--spacing-2) var(--spacing-3);
    background: var(--bg-elevated);
    border-radius: var(--radius-md);
    font-family: var(--font-family-mono);
    font-size: var(--font-size-sm);
    font-weight: 400;
    line-height: 1.5;
    color: var(--fg-primary);
  }

  .legacy-url-head {
    display: inline-block;
    max-width: calc(100% - 14ch);
    overflow: hidden;
    text-overflow: ellipsis;
    vertical-align: bottom;
  }

  .legacy-url-tail {
    display: inline-block;
    vertical-align: bottom;
  }

  .legacy-url-row > :global(button) {
    flex: none;
  }

  /* Главная кнопка первая в разметке (порядок Tab), на экране — справа */
  .legacy-actions {
    display: flex;
    flex-direction: row-reverse;
    justify-content: flex-start;
    gap: var(--spacing-2);
  }

  /* Телефон: лист снизу, кнопки в столбец во всю ширину, главная — сверху */
  @media (max-width: 768px) {
    .legacy-actions {
      flex-direction: column;
    }

    .legacy-actions > :global(button) {
      width: 100%;
    }
  }

  /* На плотности compact высота кнопки 32px: на сенсорных и узких экранах — не ниже 44px */
  @media (max-width: 768px), (pointer: coarse) {
    .legacy-actions > :global(button),
    .legacy-url-row > :global(button) {
      min-height: 44px;
    }
  }
</style>
