<script lang="ts">
  import { t } from '../../i18n';
  import Icon from '../../lib/components/Icon.svelte';
  import Button from '../Button.svelte';

  let {
    name,
    serviceId,
    status = 'loading',
    version = '',
    isActiveKernel = false,
    isInstalled = true,
    isInsecureLan = false,
    startDisabledReason = '',
    mutationBlockedReason = '',
    onRestart,
    onStart,
    onStop,
    onMigrate
  }: {
    name: string;
    serviceId: string;
    status: string;
    version?: string;
    isActiveKernel?: boolean;
    isInstalled?: boolean;
    isInsecureLan?: boolean;
    /** Причина, по которой «Запустить» неактивна (например, не установлено ни одно ядро); пусто — кнопка доступна. */
    startDisabledReason?: string;
    /** Причина блокировки запуска и перезапуска, например конфликт ядер; пусто — доступны. */
    mutationBlockedReason?: string;
    onRestart?: () => Promise<void> | void;
    onStart?: () => Promise<void> | void;
    onStop?: () => Promise<void> | void;
    onMigrate?: () => void;
  } = $props();

  let isRestarting = $state(false);
  let isStarting = $state(false);
  let isStopping = $state(false);

  const isBusy = $derived(isRestarting || isStarting || isStopping);

  // Блокировка запуска приоритетнее причины «нет ядра»: конфликт объясняет всё сразу.
  const startBlocked = $derived(mutationBlockedReason || startDisabledReason);

  const isRunning = $derived(status === 'running');
  const isStopped = $derived(status === 'stopped');
  const isNotInstalled = $derived(status === 'not_installed' || isInstalled === false);
  const isError = $derived(status === 'error');
  const isLoading = $derived(status === 'loading');

  const dotColorClass = $derived.by(() => {
    if (isRunning) return 'dot-running';
    if (isLoading) return 'dot-loading';
    if (isNotInstalled) return 'dot-uninstalled';
    if (isStopped || isError) return 'dot-stopped';
    return 'dot-unknown';
  });

  const statusText = $derived.by(() => {
    if (isRunning) return $t('app.running');
    if (isStopped) return $t('kernel.status.stopped');
    if (isNotInstalled) return $t('kernel.status.not_installed');
    if (isError) return $t('kernel.status.error');
    if (isLoading) return $t('kernel.status.loading');
    return $t('kernel.status.unknown');
  });

  const subLabel = $derived.by(() => {
    if (serviceId === 'xkeen') return $t('dash.xkeen_sub');
    if (serviceId === 'mihomo') return $t('dash.mihomo_sub');
    if (serviceId === 'xray') return $t('dash.xray_sub');
    return '';
  });

  const formattedVersion = $derived.by(() => {
    if (!version || isNotInstalled) return '';
    const cleanVer = version.trim();
    if (!cleanVer || cleanVer === 'unknown') return '';
    // Bare semver strings ("1.19.30") get a "v" prefix; strings that already
    // carry a product name/suffix (e.g. "XKeen 2.0.1 Beta") are shown as-is,
    // otherwise "v" ends up glued onto the product name ("vXKeen 2.0.1 Beta").
    if (cleanVer.startsWith('v')) return cleanVer;
    return /^\d/.test(cleanVer) ? `v${cleanVer}` : cleanVer;
  });

  async function handleRestart() {
    if (isBusy || !onRestart || mutationBlockedReason) return;
    isRestarting = true;
    try {
      await onRestart();
    } finally {
      isRestarting = false;
    }
  }

  async function handleStart() {
    if (isBusy || !onStart || startBlocked) return;
    isStarting = true;
    try {
      await onStart();
    } finally {
      isStarting = false;
    }
  }

  async function handleStop() {
    if (isBusy || !onStop) return;
    isStopping = true;
    try {
      await onStop();
    } finally {
      isStopping = false;
    }
  }
</script>

<div
  class="service-row"
  class:service-row-active={isActiveKernel}
  class:service-row-disabled={isNotInstalled}
>
  <div class="svc-ident">
    <div class="name-row">
      <span class="service-name">{name}</span>
      {#if isActiveKernel}
        <span class="badge-active" title={$t('svc.active_kernel_label')}>
          {$t('svc.active_kernel_badge')}
        </span>
      {/if}
      {#if formattedVersion}
        <span class="version-text" title={formattedVersion}>{formattedVersion}</span>
      {/if}
    </div>
    {#if subLabel}
      <span class="sub-label" title={subLabel}>{subLabel}</span>
    {/if}
  </div>

  <div class="svc-state">
    <span class="status-pill status-pill-{dotColorClass}">
      <span class="pill-dot" aria-hidden="true"></span>
      {statusText}
    </span>
    {#if !formattedVersion && isInsecureLan && onMigrate}
      <button
        type="button"
        class="badge badge-warning migrate-badge"
        onclick={onMigrate}
        title={$t('mihomo.migrate_banner_body')}
      >
        {$t('mihomo.controller_mode_insecure')}
      </button>
    {/if}
  </div>

  <div class="svc-actions">
    {#if isNotInstalled}
      <a href="#/services" class="install-link">
        {serviceId === 'xkeen'
          ? $t('dash.problems.xkeen_missing_cta')
          : $t('dash.problems.kernel_missing_cta')}
        <Icon name="chevron-right" size={14} />
      </a>
    {:else}
      {#if onRestart}
        <Button
          variant="secondary"
          onclick={handleRestart}
          disabled={isBusy || !!mutationBlockedReason}
          loading={isRestarting}
          title={mutationBlockedReason || $t('app.restart')}
          ariaLabel={mutationBlockedReason
            ? `${$t('app.restart')}: ${mutationBlockedReason}`
            : undefined}
        >
          <Icon name="refresh" size={13} />
          <span>{$t('app.restart')}</span>
        </Button>
      {/if}

      {#if isRunning && onStop}
        <Button
          variant="secondary"
          onclick={handleStop}
          disabled={isBusy}
          loading={isStopping}
          title={$t('app.stop')}
        >
          <Icon name="stop" size={13} color="var(--danger)" />
          <span>{$t('app.stop')}</span>
        </Button>
      {:else if !isRunning && onStart}
        <Button
          variant="secondary"
          onclick={handleStart}
          disabled={isBusy || !!startBlocked}
          loading={isStarting}
          title={startBlocked || $t('app.start')}
          ariaLabel={startBlocked ? `${$t('app.start')}: ${startBlocked}` : undefined}
        >
          <Icon name="play" size={13} color="var(--success)" />
          <span>{$t('app.start')}</span>
        </Button>
      {/if}
    {/if}
  </div>
</div>

<style>
  /* Строка списка сервисов: имя слева, статус и действия справа.
     Строки лежат в одной карточке и делятся тонкой линией (см. ServiceStatusGroup). */
  .service-row {
    container: svcrow / inline-size;
    position: relative;
    display: grid;
    grid-template-columns: minmax(0, 1fr) auto auto;
    align-items: center;
    gap: var(--spacing-3) var(--spacing-4);
    padding: var(--spacing-3) var(--card-pad);
    min-height: 64px;
    transition: background-color var(--transition-fast);
  }

  .service-row:hover {
    background: var(--surface-tint-faint);
  }

  /* Активное ядро: акцентная полоса у левого края вместо обводки всей плитки */
  .service-row-active::before {
    content: '';
    position: absolute;
    left: 0;
    top: var(--spacing-3);
    bottom: var(--spacing-3);
    width: 3px;
    border-radius: 0 3px 3px 0;
    background: var(--accent);
  }

  .svc-ident {
    display: flex;
    flex-direction: column;
    gap: 2px;
    min-width: 0;
  }

  .name-row {
    display: flex;
    align-items: baseline;
    gap: var(--spacing-2);
    min-width: 0;
  }

  .service-name {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-size: var(--font-size-base);
    font-weight: 600;
    color: var(--fg-primary);
    letter-spacing: -0.01em;
    line-height: 1.3;
  }

  .service-row-disabled .service-name {
    color: var(--fg-secondary);
  }

  .version-text {
    flex-shrink: 0;
    font-family: var(--font-family-mono);
    font-size: var(--font-size-xs);
    color: var(--fg-dim);
  }

  .sub-label {
    font-size: var(--font-size-xs);
    color: var(--fg-dim);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
    line-height: 1.3;
  }

  .badge-active {
    flex-shrink: 0;
    align-self: center;
    padding: 1px 7px;
    border-radius: var(--radius-full);
    background: var(--accent-soft);
    color: var(--accent-text);
    font-size: var(--font-size-xs);
    font-weight: 600;
    line-height: 1.5;
  }

  .svc-state {
    display: flex;
    align-items: center;
    gap: var(--spacing-2);
    justify-self: end;
  }

  .status-pill {
    --tone: var(--fg-dim);
    display: inline-flex;
    align-items: center;
    gap: 6px;
    padding: 2px 9px 2px 8px;
    border-radius: var(--radius-full);
    background: color-mix(in srgb, var(--tone) 12%, transparent);
    color: var(--tone);
    font-size: var(--font-size-xs);
    font-weight: 600;
    line-height: 1.5;
    white-space: nowrap;
  }

  .status-pill-dot-running {
    --tone: var(--success);
  }

  .status-pill-dot-stopped {
    --tone: var(--danger);
  }

  .status-pill-dot-loading {
    --tone: var(--warning);
  }

  .status-pill-dot-uninstalled,
  .status-pill-dot-unknown {
    --tone: var(--fg-dim);
    background: var(--surface-tint);
  }

  .pill-dot {
    width: 6px;
    height: 6px;
    border-radius: 50%;
    background: currentColor;
    flex-shrink: 0;
  }

  .status-pill-dot-running .pill-dot {
    box-shadow: 0 0 0 3px color-mix(in srgb, var(--success) 22%, transparent);
  }

  .status-pill-dot-loading .pill-dot {
    animation: pulse 1.5s infinite;
  }

  @keyframes pulse {
    0%,
    100% {
      opacity: 1;
    }
    50% {
      opacity: 0.4;
    }
  }

  .migrate-badge {
    cursor: pointer;
    border: none;
    font-size: var(--font-size-xs);
    padding: 1px 6px;
    border-radius: var(--radius-sm);
  }

  .svc-actions {
    display: flex;
    align-items: center;
    justify-content: flex-end;
    gap: var(--spacing-2);
    min-width: 0;
  }

  .service-row .svc-actions :global(.btn) {
    height: 32px;
    min-height: 32px;
    padding: 0 var(--spacing-3);
    font-size: var(--font-size-sm);
    white-space: nowrap;
  }

  /* Не установлено — тихая ссылка вместо третьей большой кнопки: главный призыв
     к установке стоит в баннере «Следующий шаг» над секциями. */
  .install-link {
    display: inline-flex;
    align-items: center;
    gap: 2px;
    padding: 6px var(--spacing-2);
    border-radius: var(--radius-md);
    color: var(--accent-text);
    font-size: var(--font-size-sm);
    font-weight: 600;
    text-decoration: none;
    white-space: nowrap;
    transition: background-color var(--transition-fast);
  }

  .install-link:hover {
    background: var(--hover);
  }

  /* Узкая строка (мобильный, узкая колонка): действия уходят под имя на всю ширину */
  @container svcrow (max-width: 520px) {
    .service-row {
      grid-template-columns: minmax(0, 1fr) auto;
    }

    .svc-actions {
      grid-column: 1 / -1;
      justify-content: flex-start;
    }

    .service-row .svc-actions :global(.btn) {
      flex: 1 1 0;
    }
  }

  @media (prefers-reduced-motion: reduce) {
    .status-pill-dot-loading .pill-dot {
      animation: none;
    }
  }
</style>
