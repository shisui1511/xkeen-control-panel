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
    onRestart,
    onStart,
    onStop,
    onMigrate
  } = $props<{
    name: string;
    serviceId: string;
    status: string;
    version?: string;
    isActiveKernel?: boolean;
    isInstalled?: boolean;
    isInsecureLan?: boolean;
    onRestart?: () => Promise<void> | void;
    onStart?: () => Promise<void> | void;
    onStop?: () => Promise<void> | void;
    onMigrate?: () => void;
  }>();

  let isRestarting = $state(false);
  let isStarting = $state(false);
  let isStopping = $state(false);

  const isBusy = $derived(isRestarting || isStarting || isStopping);

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

  async function handleRestart() {
    if (isBusy || !onRestart) return;
    isRestarting = true;
    try {
      await onRestart();
    } finally {
      isRestarting = false;
    }
  }

  async function handleStart() {
    if (isBusy || !onStart) return;
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
  class="service-card"
  class:service-card-active={isActiveKernel}
  class:service-card-disabled={isNotInstalled}
>
  <div class="card-header">
    <div class="header-left">
      <span class="status-dot {dotColorClass}" aria-hidden="true"></span>
      <div class="title-group">
        <div class="name-row">
          <span class="service-name">{name}</span>
          {#if isActiveKernel}
            <span class="badge badge-active" title="Активное ядро маршрутизации">
              {$t('svc.active_kernel_badge') || 'Активно'}
            </span>
          {/if}
          {#if version && !isNotInstalled}
            <span class="version-badge">{version}</span>
          {/if}
        </div>
        {#if subLabel}
          <span class="sub-label">{subLabel}</span>
        {/if}
      </div>
    </div>

    <div class="header-right">
      <span class="status-text status-text-{status}">
        {statusText}
      </span>
      {#if isInsecureLan && onMigrate}
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
  </div>

  <div class="card-actions">
    {#if isNotInstalled}
      <a href="#/services" class="btn btn-secondary btn-sm install-link">
        {$t('dash.problems.kernel_missing_cta')}
      </a>
    {:else}
      <div class="btn-group">
        {#if onRestart}
          <Button
            variant="secondary"
            onclick={handleRestart}
            disabled={isBusy}
            loading={isRestarting}
            title={$t('app.restart')}
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
            <Icon name="stop" size={13} color="var(--error, #f4707f)" />
            <span>{$t('app.stop')}</span>
          </Button>
        {:else if !isRunning && onStart}
          <Button
            variant="secondary"
            onclick={handleStart}
            disabled={isBusy}
            loading={isStarting}
            title={$t('app.start')}
          >
            <Icon name="play" size={13} color="var(--success, #46d18a)" />
            <span>{$t('app.start')}</span>
          </Button>
        {/if}
      </div>
    {/if}
  </div>
</div>

<style>
  .service-card {
    display: flex;
    flex-direction: column;
    justify-content: space-between;
    background:
      linear-gradient(180deg, rgba(255, 255, 255, 0.02), transparent 70%), var(--bg-card, #102a44);
    border: 1px solid var(--border, rgba(255, 255, 255, 0.08));
    border-radius: var(--radius-lg, 12px);
    padding: 16px;
    gap: 14px;
    box-shadow: var(--shadow, 0 4px 12px rgba(0, 0, 0, 0.2));
    transition:
      border-color 0.2s ease,
      transform 0.15s ease,
      box-shadow 0.2s ease;
  }

  .service-card:hover {
    border-color: rgba(41, 194, 240, 0.3);
  }

  .service-card-active {
    border-color: rgba(41, 194, 240, 0.4);
    box-shadow:
      0 0 16px rgba(41, 194, 240, 0.08),
      var(--shadow, 0 4px 12px rgba(0, 0, 0, 0.2));
  }

  .service-card-disabled {
    opacity: 0.75;
  }

  .card-header {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    gap: 12px;
  }

  .header-left {
    display: flex;
    align-items: flex-start;
    gap: 10px;
    min-width: 0;
  }

  .title-group {
    display: flex;
    flex-direction: column;
    gap: 2px;
    min-width: 0;
  }

  .name-row {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: 6px;
  }

  .service-name {
    font-size: 15px;
    font-weight: 600;
    color: var(--fg-primary, #ffffff);
    letter-spacing: -0.01em;
  }

  .sub-label {
    font-size: 12px;
    font-weight: 400;
    color: var(--fg-muted, var(--fg-secondary, #8fa3b8));
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }

  .header-right {
    display: flex;
    flex-direction: column;
    align-items: flex-end;
    gap: 4px;
    flex-shrink: 0;
  }

  .status-text {
    font-size: 12px;
    font-weight: 600;
  }

  .status-text-running {
    color: var(--success, #46d18a);
  }

  .status-text-stopped,
  .status-text-error {
    color: var(--error, #f4707f);
  }

  .status-text-loading {
    color: var(--warning, #f0b450);
  }

  .status-text-not_installed {
    color: var(--fg-dim, #63778a);
  }

  .status-dot {
    width: 9px;
    height: 9px;
    border-radius: 50%;
    margin-top: 5px;
    flex-shrink: 0;
    transition:
      background-color 0.25s ease,
      box-shadow 0.25s ease;
  }

  .dot-running {
    background-color: var(--success, #46d18a);
    box-shadow: 0 0 8px rgba(70, 209, 138, 0.5);
  }

  .dot-stopped,
  .dot-error {
    background-color: var(--error, #f4707f);
    box-shadow: 0 0 6px rgba(244, 112, 127, 0.4);
  }

  .dot-loading {
    background-color: var(--warning, #f0b450);
    box-shadow: 0 0 6px rgba(240, 180, 80, 0.4);
    animation: pulse 1.5s infinite;
  }

  .dot-uninstalled,
  .dot-unknown {
    background-color: var(--fg-dim, #63778a);
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

  .badge-active {
    background: rgba(41, 194, 240, 0.15);
    color: var(--accent, #29c2f0);
    border: 1px solid rgba(41, 194, 240, 0.3);
    font-size: 10.5px;
    font-weight: 600;
    padding: 1px 6px;
    border-radius: var(--radius-sm, 6px);
  }

  .version-badge {
    background: rgba(255, 255, 255, 0.06);
    color: var(--fg-secondary, #8fa3b8);
    border: 1px solid rgba(255, 255, 255, 0.08);
    font-size: 11px;
    font-family: var(--font-family-mono, monospace);
    padding: 1px 5px;
    border-radius: var(--radius-sm, 6px);
  }

  .migrate-badge {
    cursor: pointer;
    border: none;
    font-size: 11px;
    padding: 2px 6px;
  }

  .card-actions {
    display: flex;
    align-items: center;
    justify-content: flex-end;
    padding-top: 10px;
    border-top: 1px solid rgba(255, 255, 255, 0.05);
  }

  .btn-group {
    display: flex;
    align-items: center;
    gap: 8px;
    width: 100%;
    justify-content: flex-end;
  }

  .install-link {
    width: 100%;
    justify-content: center;
    text-align: center;
  }
</style>
