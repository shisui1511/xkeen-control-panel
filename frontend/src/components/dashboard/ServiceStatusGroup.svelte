<script lang="ts">
  import { t } from '../../i18n';
  import {
    notifySwitchOutcome,
    serviceAction,
    stopKernelProcess,
    switchKernel,
    type KernelName
  } from '../../lib/serviceControl';
  import { kernelLabel } from '../../lib/kernelState';
  import { activeKernelName, fetchCapabilities, isConflict, showToast } from '../../stores';
  import Icon from '../../lib/components/Icon.svelte';
  import Button from '../Button.svelte';
  import Skeleton from '../Skeleton.svelte';
  import StatusBadge from '../StatusBadge.svelte';
  import ServiceCard from './ServiceCard.svelte';
  import { anyKernelInstalled } from '../../lib/navCaps';

  let {
    serviceStatus,
    capabilities,
    xkeenVersion = '',
    statusLoading = false,
    statusError = false,
    staleLabel = null,
    staleTitle = '',
    onRefresh,
    onShowMihomoMigrateModal
  } = $props<{
    serviceStatus: {
      xkeen: string;
      xray: string;
      mihomo: string;
      xrayVersion: string;
      mihomoVersion: string;
    };
    capabilities: any;
    xkeenVersion?: string;
    statusLoading?: boolean;
    statusError?: boolean;
    /** Текст бейджа «данные от HH:MM»; пусто — статус свежий, бейджа нет. */
    staleLabel?: string | null;
    /** Подсказка к бейджу: сколько секунд статус не обновлялся. */
    staleTitle?: string;
    onRefresh?: () => Promise<void> | void;
    onShowMihomoMigrateModal?: () => void;
  }>();

  let isRefreshing = $state(false);

  async function handleManualRefresh() {
    if (isRefreshing || !onRefresh) return;
    isRefreshing = true;
    try {
      await onRefresh();
    } finally {
      isRefreshing = false;
    }
  }

  /** Общая обёртка действий: ошибки (кроме 401) — текстом ошибки клиента, затем обновление статуса. */
  async function runAction(action: () => Promise<void>, refreshDelay: number): Promise<void> {
    try {
      await action();
      if (onRefresh) setTimeout(onRefresh, refreshDelay);
      await fetchCapabilities();
    } catch (e: any) {
      if (e?.status === 401) return;
      showToast('error', e?.message || $t('app.error'));
    }
  }

  async function restartXkeen() {
    showToast('info', $t('capsule.toast_restarting_xkeen'));
    await runAction(async () => {
      await serviceAction('restart');
      showToast('success', $t('app.restart') + ' XKeen: OK');
    }, 2500);
  }

  async function startXkeen() {
    showToast('info', $t('capsule.start_service') + ' XKeen...');
    await runAction(async () => {
      await serviceAction('start');
      showToast('success', $t('capsule.start_service') + ': OK');
    }, 2000);
  }

  async function stopXkeen() {
    await runAction(async () => {
      await serviceAction('stop');
      showToast('warning', $t('capsule.stop_service') + ' XKeen');
    }, 1500);
  }

  async function restartKernel(kernel: KernelName) {
    const label = kernelLabel(kernel);
    showToast('info', `${$t('app.restart')} ${label}...`);
    await runAction(async () => {
      if (kernel === $activeKernelName) {
        await serviceAction('restart');
        showToast('success', `${label}: ${$t('app.restart')} OK`);
      } else {
        notifySwitchOutcome(await switchKernel(kernel));
      }
    }, 2500);
  }

  async function startKernel(kernel: KernelName) {
    const label = kernelLabel(kernel);
    showToast('info', `${$t('app.start')} ${label}...`);
    await runAction(async () => {
      if (kernel === $activeKernelName) {
        await serviceAction('start');
        showToast('success', `${label}: ${$t('app.start')} OK`);
      } else {
        notifySwitchOutcome(await switchKernel(kernel));
      }
    }, 2000);
  }

  async function stopKernel(kernel: KernelName) {
    const label = kernelLabel(kernel);
    await runAction(async () => {
      if ($isConflict) {
        // В конфликте общий stop погасил бы всё через XKeen: гасим только выбранное ядро.
        const result = await stopKernelProcess(kernel);
        if (result.outcome === 'still_running') {
          showToast('error', $t('kernel.stop_still_running', { kernel: label }), 10000);
          return;
        }
      } else {
        await serviceAction('stop');
      }
      showToast('warning', `${$t('app.stop')} ${label}`);
    }, 1500);
  }

  const isMihomoInstalled = $derived(
    capabilities?.kernels?.mihomo?.installed ?? serviceStatus.mihomo !== 'not_installed'
  );
  const isXrayInstalled = $derived(
    capabilities?.kernels?.xray?.installed ?? serviceStatus.xray !== 'not_installed'
  );

  const mutationBlockedReason = $derived($isConflict ? $t('kernel.conflict_blocked') : '');
</script>

<div class="service-status-container">
  {#if statusLoading}
    <div class="services-grid">
      {#each [1, 2, 3] as n (n)}
        <div class="service-sk-card">
          <div class="sk-head">
            <Skeleton type="circle" width="10px" height="10px" />
            <Skeleton type="rect" width="100px" height="18px" />
          </div>
          <div class="sk-body">
            <Skeleton type="rect" width="60px" height="14px" />
          </div>
          <div class="sk-footer">
            <Skeleton type="rect" width="100%" height="28px" />
          </div>
        </div>
      {/each}
    </div>
  {:else if statusError}
    <div class="status-error-card">
      <div class="error-msg">
        <Icon name="warning" size={16} color="var(--error, #f4707f)" />
        <span>{$t('dash.status_error')}</span>
      </div>
      <Button
        variant="secondary"
        onclick={handleManualRefresh}
        loading={isRefreshing}
        disabled={isRefreshing}
      >
        <Icon name="refresh" size={13} />
        <span>{$t('app.refresh')}</span>
      </Button>
    </div>
  {:else}
    {#if staleLabel}
      <div class="stale-row">
        <span data-testid="status-stale-badge" title={staleTitle}>
          <StatusBadge variant="idle" label={staleLabel} />
        </span>
      </div>
    {/if}
    <div class="services-grid">
      <!-- XKeen Daemon -->
      <ServiceCard
        name="XKeen"
        serviceId="xkeen"
        status={serviceStatus.xkeen}
        version={xkeenVersion}
        isActiveKernel={false}
        isInstalled={capabilities?.xkeen_installed !== false}
        startDisabledReason={anyKernelInstalled(capabilities) === false
          ? $t('svc.start_disabled_no_kernel')
          : undefined}
        {mutationBlockedReason}
        onRestart={restartXkeen}
        onStart={startXkeen}
        onStop={stopXkeen}
      />

      <!-- Mihomo Core -->
      <ServiceCard
        name="Mihomo"
        serviceId="mihomo"
        status={serviceStatus.mihomo}
        version={serviceStatus.mihomoVersion}
        isActiveKernel={$activeKernelName === 'mihomo'}
        isInstalled={isMihomoInstalled}
        isInsecureLan={Boolean(capabilities?.mihomo?.is_insecure_lan)}
        {mutationBlockedReason}
        onRestart={() => restartKernel('mihomo')}
        onStart={() => startKernel('mihomo')}
        onStop={() => stopKernel('mihomo')}
        onMigrate={onShowMihomoMigrateModal}
      />

      <!-- Xray Core -->
      <ServiceCard
        name="Xray"
        serviceId="xray"
        status={serviceStatus.xray}
        version={serviceStatus.xrayVersion}
        isActiveKernel={$activeKernelName === 'xray'}
        isInstalled={isXrayInstalled}
        {mutationBlockedReason}
        onRestart={() => restartKernel('xray')}
        onStart={() => startKernel('xray')}
        onStop={() => stopKernel('xray')}
      />
    </div>
  {/if}
</div>

<style>
  .service-status-container {
    width: 100%;
  }

  .stale-row {
    display: flex;
    justify-content: flex-end;
    margin-bottom: 8px;
  }

  .services-grid {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(210px, 1fr));
    gap: 12px;
  }

  @media (max-width: 640px) {
    .services-grid {
      grid-template-columns: 1fr;
    }
  }

  .service-sk-card {
    background: var(--bg-card);
    border: 1px solid var(--border, rgba(255, 255, 255, 0.08));
    border-radius: var(--radius-lg, 12px);
    padding: 16px;
    display: flex;
    flex-direction: column;
    gap: 12px;
  }

  .sk-head {
    display: flex;
    align-items: center;
    gap: 10px;
  }

  .sk-body {
    display: flex;
    justify-content: flex-end;
  }

  .sk-footer {
    padding-top: 10px;
    border-top: 1px solid rgba(255, 255, 255, 0.05);
  }

  .status-error-card {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 14px 18px;
    background: rgba(244, 112, 127, 0.08);
    border: 1px solid rgba(244, 112, 127, 0.25);
    border-radius: var(--radius-lg, 12px);
    gap: 12px;
  }

  .error-msg {
    display: flex;
    align-items: center;
    gap: 8px;
    font-size: 13px;
    color: var(--danger);
    font-weight: 500;
  }
</style>
