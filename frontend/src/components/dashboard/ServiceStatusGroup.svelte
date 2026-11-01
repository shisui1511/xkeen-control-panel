<script lang="ts">
  import { t } from '../../i18n';
  import {
    confirmKernelStop,
    notifySwitchOutcome,
    serviceAction,
    stopKernelAndReport,
    switchKernel,
    type KernelName
  } from '../../lib/serviceControl';
  import { kernelLabel } from '../../lib/kernelState';
  import { activeKernelName, fetchCapabilities, isConflict, showToast } from '../../stores';
  import Icon from '../../lib/components/Icon.svelte';
  import Button from '../Button.svelte';
  import Skeleton from '../Skeleton.svelte';
  import Card from '../Card.svelte';
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
  }: {
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
  } = $props();

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
    if ($isConflict) {
      // В конфликте общий stop погасил бы всё через XKeen: гасим только выбранное ядро —
      // с тем же подтверждением и проверенным итогом, что у баннера (D-02, D-04).
      if (!(await confirmKernelStop(kernel))) return;
      await stopKernelAndReport(kernel);
      if (onRefresh) setTimeout(onRefresh, 1500);
      return;
    }
    const label = kernelLabel(kernel);
    await runAction(async () => {
      await serviceAction('stop');
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
    <Card flush>
      <div class="services-list">
        {#each [1, 2, 3] as n (n)}
          <div class="service-sk-row">
            <div class="sk-ident">
              <Skeleton type="rect" width="96px" height="16px" />
              <Skeleton type="rect" width="72px" height="12px" />
            </div>
            <Skeleton type="rect" width="72px" height="20px" />
            <Skeleton type="rect" width="180px" height="32px" />
          </div>
        {/each}
      </div>
    </Card>
  {:else if statusError}
    <div class="status-error-card">
      <div class="error-msg">
        <Icon name="warning" size={16} color="var(--danger)" />
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
    <Card flush>
      <div class="services-list">
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
    </Card>
  {/if}
</div>

<style>
  .service-status-container {
    width: 100%;
  }

  .stale-row {
    display: flex;
    justify-content: flex-end;
    margin-bottom: var(--spacing-2);
  }

  /* Сервисы — строки одного списка, между ними тонкая линия */
  .services-list {
    display: flex;
    flex-direction: column;
  }

  .services-list > :global(* + *) {
    border-top: 1px solid var(--border);
  }

  .service-sk-row {
    display: grid;
    grid-template-columns: minmax(0, 1fr) auto auto;
    align-items: center;
    gap: var(--spacing-4);
    padding: var(--spacing-3) var(--card-pad);
    min-height: 64px;
  }

  .sk-ident {
    display: flex;
    flex-direction: column;
    gap: 6px;
  }

  .status-error-card {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 14px 18px;
    background: color-mix(in srgb, var(--danger) 8%, transparent);
    border: 1px solid color-mix(in srgb, var(--danger) 25%, transparent);
    border-radius: var(--radius-lg);
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
