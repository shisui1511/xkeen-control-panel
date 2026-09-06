<script lang="ts">
  import { t } from '../../i18n';
  import { apiFetch } from '../../lib/api';
  import { activateRestartGrace } from '../../lib/serviceGrace';
  import { fetchCapabilities, showToast } from '../../stores';
  import Icon from '../../lib/components/Icon.svelte';
  import Button from '../Button.svelte';
  import Skeleton from '../Skeleton.svelte';
  import ServiceCard from './ServiceCard.svelte';

  let {
    serviceStatus,
    capabilities,
    xkeenVersion = '',
    statusLoading = false,
    statusError = false,
    onRefresh,
    onShowMihomoMigrateModal
  } = $props<{
    serviceStatus: {
      xkeen: string;
      xray: string;
      mihomo: string;
      connections: number;
      xrayVersion: string;
      mihomoVersion: string;
    };
    capabilities: any;
    xkeenVersion?: string;
    statusLoading?: boolean;
    statusError?: boolean;
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

  async function restartXkeen() {
    activateRestartGrace(6000);
    showToast('info', $t('capsule.toast_restarting_xkeen'));
    try {
      const res = await apiFetch('/api/service/control?action=restart', { method: 'POST' });
      if (!res.ok) {
        const txt = await res.text();
        throw new Error(txt);
      }
      showToast('success', $t('app.restart') + ' XKeen: OK');
      if (onRefresh) setTimeout(onRefresh, 2500);
      await fetchCapabilities();
    } catch (e: any) {
      if (e?.status === 401) return;
      showToast('error', e?.message || $t('app.error'));
    }
  }

  async function startXkeen() {
    activateRestartGrace(6000);
    showToast('info', $t('capsule.start_service') + ' XKeen...');
    try {
      const res = await apiFetch('/api/service/control?action=start', { method: 'POST' });
      if (!res.ok) {
        const txt = await res.text();
        throw new Error(txt);
      }
      showToast('success', $t('capsule.start_service') + ': OK');
      if (onRefresh) setTimeout(onRefresh, 2000);
      await fetchCapabilities();
    } catch (e: any) {
      if (e?.status === 401) return;
      showToast('error', e?.message || $t('app.error'));
    }
  }

  async function stopXkeen() {
    try {
      const res = await apiFetch('/api/service/control?action=stop', { method: 'POST' });
      if (!res.ok) {
        const txt = await res.text();
        throw new Error(txt);
      }
      showToast('warning', $t('capsule.stop_service') + ' XKeen');
      if (onRefresh) setTimeout(onRefresh, 1500);
      await fetchCapabilities();
    } catch (e: any) {
      if (e?.status === 401) return;
      showToast('error', e?.message || $t('app.error'));
    }
  }

  async function restartKernel(kernel: 'mihomo' | 'xray') {
    activateRestartGrace(6000);
    const kernelLabel = kernel === 'mihomo' ? 'Mihomo' : 'Xray';
    const isActive = capabilities?.active_kernel === kernel;

    showToast('info', `${$t('app.restart')} ${kernelLabel}...`);
    try {
      const url = isActive
        ? '/api/service/control?action=restart'
        : `/api/service/control?action=switch_kernel&kernel=${kernel}`;
      const res = await apiFetch(url, { method: 'POST' });
      if (!res.ok) {
        const txt = await res.text();
        throw new Error(txt);
      }
      showToast('success', `${kernelLabel}: ${$t('app.restart')} OK`);
      if (onRefresh) setTimeout(onRefresh, 2500);
      await fetchCapabilities();
    } catch (e: any) {
      if (e?.status === 401) return;
      showToast('error', e?.message || $t('app.error'));
    }
  }

  async function startKernel(kernel: 'mihomo' | 'xray') {
    activateRestartGrace(6000);
    const kernelLabel = kernel === 'mihomo' ? 'Mihomo' : 'Xray';
    showToast('info', `${$t('app.start')} ${kernelLabel}...`);
    try {
      const url =
        capabilities?.active_kernel === kernel
          ? '/api/service/control?action=start'
          : `/api/service/control?action=switch_kernel&kernel=${kernel}`;
      const res = await apiFetch(url, { method: 'POST' });
      if (!res.ok) {
        const txt = await res.text();
        throw new Error(txt);
      }
      showToast('success', `${kernelLabel}: ${$t('app.start')} OK`);
      if (onRefresh) setTimeout(onRefresh, 2000);
      await fetchCapabilities();
    } catch (e: any) {
      if (e?.status === 401) return;
      showToast('error', e?.message || $t('app.error'));
    }
  }

  async function stopKernel(kernel: 'mihomo' | 'xray') {
    const kernelLabel = kernel === 'mihomo' ? 'Mihomo' : 'Xray';
    try {
      const res = await apiFetch('/api/service/control?action=stop', { method: 'POST' });
      if (!res.ok) {
        const txt = await res.text();
        throw new Error(txt);
      }
      showToast('warning', `${$t('app.stop')} ${kernelLabel}`);
      if (onRefresh) setTimeout(onRefresh, 1500);
      await fetchCapabilities();
    } catch (e: any) {
      if (e?.status === 401) return;
      showToast('error', e?.message || $t('app.error'));
    }
  }

  const isMihomoInstalled = $derived(
    capabilities?.kernels?.mihomo?.installed ?? serviceStatus.mihomo !== 'not_installed'
  );
  const isXrayInstalled = $derived(
    capabilities?.kernels?.xray?.installed ?? serviceStatus.xray !== 'not_installed'
  );

  const activeKernel = $derived(capabilities?.active_kernel || 'none');
</script>

<div class="service-status-container">
  {#if statusLoading}
    <div class="services-grid">
      {#each [1, 2, 3] as _}
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
    <div class="services-grid">
      <!-- XKeen Daemon -->
      <ServiceCard
        name="XKeen"
        serviceId="xkeen"
        status={serviceStatus.xkeen}
        version={xkeenVersion}
        isActiveKernel={false}
        isInstalled={true}
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
        isActiveKernel={activeKernel === 'mihomo'}
        isInstalled={isMihomoInstalled}
        isInsecureLan={Boolean(capabilities?.mihomo?.is_insecure_lan)}
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
        isActiveKernel={activeKernel === 'xray'}
        isInstalled={isXrayInstalled}
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
    background: var(--bg-card, #102a44);
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
    color: var(--error, #f4707f);
    font-weight: 500;
  }
</style>
