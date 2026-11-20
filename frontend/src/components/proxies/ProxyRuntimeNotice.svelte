<script lang="ts">
  // Плашка состояния ядра страницы «Прокси» при слое (D-01…D-03) и строка причины
  // запасного замера по TLS (D-07). Одновременно — не больше одной плашки, по
  // приоритету «нет сокета» → «свой профиль» → «Xray у пользователя» → «остановлено».
  // «Нет сокета» (D-13, S3) — не плашка, а пустое состояние вместо содержимого страницы:
  // его же показывают «Подключения» и квоты, поэтому вид один на три страницы.
  import { t } from '../../i18n';
  import { showToast } from '../../stores';
  import { serviceAction } from '../../lib/serviceControl';
  import Button from '../Button.svelte';
  import EmptyState from '../EmptyState.svelte';
  import Icon from '../../lib/components/Icon.svelte';
  import WarningIcon from '../../lib/components/icons/Warning.svelte';

  type NoticeKind = 'no_socket' | 'user_profile' | 'user_managed' | 'stopped';

  // Профили Mihomo живут на вкладке ядер «Конфигурации» (карточка «Профили»); focus —
  // прокрутка к карточке
  const PROFILES_HASH = '#/config/cores?focus=profiles';

  interface Props {
    /**
     * Состояние вида рантайма (RuntimeView.state) или no_socket — у работающего профиля
     * пользователя нет external-controller-unix; null — плашки нет.
     */
    viewState?: string | null;
    /** Причина замера по TLS последней пачки; без неё строки нет. */
    fallbackReason?: string;
    /** Ядро запущено: страница перечитывает вид. */
    onstarted?: () => void;
    /**
     * inline — «нет сокета» плашкой над работающей страницей (квоты); по умолчанию —
     * пустое состояние вместо содержимого (прокси, подключения).
     */
    layout?: 'page' | 'inline';
  }

  let { viewState = null, fallbackReason = '', onstarted, layout = 'page' }: Props = $props();

  const PRIORITY: NoticeKind[] = ['no_socket', 'user_profile', 'user_managed', 'stopped'];
  const kind = $derived(PRIORITY.find((k) => k === viewState) ?? null);

  let starting = $state(false);

  // Тот же путь запуска, что на «Службах»: POST /api/service/control с CSRF
  async function start() {
    if (starting) return;
    starting = true;
    try {
      await serviceAction('start');
      onstarted?.();
    } catch (e) {
      const reason = (e as { message?: unknown } | null)?.message;
      showToast(
        'error',
        $t('proxies.rt.notice.start_failed', {
          reason: typeof reason === 'string' && reason !== '' ? reason : '?'
        })
      );
    } finally {
      starting = false;
    }
  }
</script>

{#if kind === 'no_socket' && layout === 'inline'}
  <div class="alert alert-warning prn-alert" data-testid="mihomo-no-socket">
    <span class="icon-first-line prn-icon" aria-hidden="true">
      <Icon name="warning" size={16} />
    </span>
    <div class="prn-body">
      <span class="prn-text">
        <strong>{$t('proxies.rt.no_socket_title')}.</strong>
        {$t('proxies.rt.no_socket_desc')}
        <a class="prn-link prn-link--plain" href={PROFILES_HASH}>{$t('proxies.rt.no_socket_cta')}</a
        >
      </span>
    </div>
  </div>
{:else if kind === 'no_socket'}
  <div data-testid="mihomo-no-socket">
    <EmptyState
      title={$t('proxies.rt.no_socket_title')}
      description={$t('proxies.rt.no_socket_desc')}
      icon={WarningIcon}
      tone="warning"
      ctaText={$t('proxies.rt.no_socket_cta')}
      ctaVariant="secondary"
      ctaHref={PROFILES_HASH}
    />
  </div>
{:else if kind}
  <div
    class="alert prn-alert"
    class:alert-warning={kind !== 'stopped'}
    class:prn-neutral={kind === 'stopped'}
    data-testid="proxies-runtime-notice"
    data-kind={kind}
  >
    <span class="icon-first-line prn-icon" aria-hidden="true">
      <Icon name={kind === 'stopped' ? 'info' : 'warning'} size={16} />
    </span>
    <div class="prn-body">
      {#if kind === 'stopped'}
        <span class="prn-text">{$t('proxies.rt.notice.stopped')}</span>
        <Button
          variant="primary"
          size="sm"
          class="prn-start tap-zone-44"
          data-testid="proxies-runtime-start"
          loading={starting}
          onclick={start}
        >
          {starting ? $t('proxies.rt.notice.starting') : $t('proxies.rt.notice.start')}
        </Button>
      {:else if kind === 'user_profile'}
        <span class="prn-text">
          {$t('proxies.rt.notice.user_profile')}
          <a class="prn-link" href="#/config/cores">{$t('proxies.rt.notice.profiles_link')}</a>
        </span>
      {:else}
        <span class="prn-text">
          {$t('proxies.rt.notice.user_managed')}
          <a class="prn-link" href="#/config/rules">{$t('proxies.rt.notice.manage_link')}</a>
        </span>
      {/if}
    </div>
  </div>
{/if}

{#if fallbackReason === 'no_xray_binary' || fallbackReason === 'low_memory' || fallbackReason === 'xray_rejected'}
  <p class="prn-tls" data-testid="proxies-tls-reason">
    <span class="icon-first-line prn-icon" aria-hidden="true">
      <Icon name="info" size={12} />
    </span>
    <span>{$t(`proxies.rt.tls_reason.${fallbackReason}`)}</span>
  </p>
{/if}

<style>
  .prn-alert {
    margin-bottom: 0;
    padding: var(--spacing-4);
    gap: var(--spacing-2);
  }

  /* Нейтральная плашка: выбор и пинг при остановленном ядре работают */
  .prn-neutral {
    background: var(--bg-card);
    border-color: var(--border);
    border-left-color: var(--border-strong);
    color: var(--fg-primary);
  }

  .prn-icon {
    display: inline-flex;
  }

  .prn-body {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--spacing-2);
    min-width: 0;
    flex: 1;
  }

  .prn-text {
    flex: 1 1 280px;
    min-width: 0;
    line-height: 1.5;
    overflow-wrap: anywhere;
  }

  .prn-link {
    color: var(--accent);
    text-decoration: none;
    white-space: nowrap;
  }

  /* Ссылка в плашке неполадки: accent закрыт — цвет текста плашки, подчёркивание */
  .prn-link--plain {
    color: inherit;
    font-weight: 600;
    text-decoration: underline;
  }

  .prn-link:hover,
  .prn-link:focus-visible {
    text-decoration: underline;
  }

  .prn-tls {
    display: flex;
    align-items: flex-start;
    gap: var(--spacing-1);
    margin: 0;
    font-size: var(--font-size-xs);
    font-weight: 600;
    line-height: 1.5;
    color: var(--fg-secondary);
  }

  @media (max-width: 768px) {
    .prn-body :global(.prn-start) {
      width: 100%;
    }
  }

  @media (pointer: coarse), (max-width: 768px) {
    .prn-link {
      display: inline-flex;
      align-items: center;
      min-height: 44px;
    }
  }
</style>
