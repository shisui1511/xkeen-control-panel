<script lang="ts">
  import { t } from '../../i18n';
  import Icon from '../../lib/components/Icon.svelte';
  import Modal from '../Modal.svelte';
  import Button from '../Button.svelte';
  import Badge from '../Badge.svelte';
  import type { SystemStats } from './SystemResourcesWidget.svelte';

  let {
    open: isOpen = false,
    onclose: onClose,
    systemStats,
    panelVersion
  }: {
    open: boolean;
    onclose: () => void;
    systemStats: SystemStats | null;
    panelVersion: string;
  } = $props();

  const goRuntime = $derived(systemStats?.go_runtime);
</script>

<Modal
  open={isOpen}
  title={$t('dash.system_about_title')}
  maxWidth="640px"
  onclose={onClose}
  testid="system-about-modal"
>
  <div class="about-modal-body">
    <!-- Section 1: Go Runtime Telemetry -->
    <section class="diag-section">
      <div class="section-head">
        <Icon name="console" size={16} color="var(--accent)" />
        <h3 class="section-title">{$t('dash.go_runtime_title')}</h3>
      </div>
      <p class="section-desc">{$t('dash.go_runtime_desc')}</p>

      <div class="diag-grid">
        <div class="diag-item">
          <span class="diag-label">Goroutines</span>
          <span class="diag-val mono">{goRuntime?.goroutines ?? '—'}</span>
        </div>
        <div class="diag-item">
          <span class="diag-label">Heap Alloc</span>
          <span class="diag-val mono">
            {goRuntime ? `${(goRuntime.heap_alloc / 1024 / 1024).toFixed(2)} MB` : '—'}
          </span>
        </div>
        <div class="diag-item">
          <span class="diag-label">Heap Sys</span>
          <span class="diag-val mono">
            {goRuntime ? `${(goRuntime.heap_sys / 1024 / 1024).toFixed(2)} MB` : '—'}
          </span>
        </div>
        <div class="diag-item">
          <span class="diag-label">Num GC</span>
          <span class="diag-val mono">{goRuntime?.num_gc ?? '—'}</span>
        </div>
        <div class="diag-item">
          <span class="diag-label">GOMAXPROCS</span>
          <span class="diag-val mono">{goRuntime?.gomaxprocs ?? '—'}</span>
        </div>
        <div class="diag-item">
          <span class="diag-label">Go Version</span>
          <span class="diag-val mono">{goRuntime?.go_version || '—'}</span>
        </div>
        <div class="diag-item diag-item-full">
          <span class="diag-label">Architecture</span>
          <span class="diag-val mono">{goRuntime?.goarch || '—'}</span>
        </div>
      </div>
    </section>

    <!-- Section 2: Entware & System Environment -->
    <section class="diag-section">
      <div class="section-head">
        <Icon name="network" size={16} color="var(--success)" />
        <h3 class="section-title">{$t('dash.entware_diag_title')}</h3>
      </div>
      <p class="section-desc">{$t('dash.entware_diag_desc')}</p>

      <div class="diag-grid">
        <div class="diag-item">
          <span class="diag-label">{$t('dash.router_model')}</span>
          <span class="diag-val">{systemStats?.router_model || '—'}</span>
        </div>
        <div class="diag-item">
          <span class="diag-label">{$t('dash.router_hostname')}</span>
          <span class="diag-val mono">{systemStats?.hostname || '—'}</span>
        </div>
        <div class="diag-item">
          <span class="diag-label">{$t('dash.info_platform')}</span>
          <span class="diag-val mono">{systemStats?.platform || '—'}</span>
        </div>
        <div class="diag-item">
          <span class="diag-label">{$t('dash.info_kernel')}</span>
          <span class="diag-val mono">{systemStats?.kernel_version || '—'}</span>
        </div>
        <div class="diag-item">
          <span class="diag-label">{$t('dash.wan_status')}</span>
          <span class="diag-val">
            <Badge tone={systemStats?.wan_status === 'online' ? 'success' : 'neutral'}>
              {systemStats?.wan_status || '—'}
            </Badge>
          </span>
        </div>
        <div class="diag-item">
          <span class="diag-label">{$t('dash.default_gateway')}</span>
          <span class="diag-val mono">{systemStats?.default_gateway || '—'}</span>
        </div>
        <div class="diag-item diag-item-full">
          <span class="diag-label">{$t('dash.dns_servers')}</span>
          <span class="diag-val mono">
            {systemStats?.dns_servers && systemStats.dns_servers.length > 0
              ? systemStats.dns_servers.join(', ')
              : '—'}
          </span>
        </div>
        <div class="diag-item">
          <span class="diag-label">{$t('dash.dns_resolving')}</span>
          <span class="diag-val">
            {#if systemStats?.dns_resolving}
              <span class="text-success">{$t('dash.dns_resolving_ok')}</span>
            {:else}
              <span class="text-error">{$t('dash.dns_resolving_fail')}</span>
            {/if}
          </span>
        </div>
        <div class="diag-item">
          <span class="diag-label">{$t('dash.info_boot_time')}</span>
          <span class="diag-val mono">{systemStats?.boot_time || '—'}</span>
        </div>
      </div>
    </section>

    <div class="modal-actions-footer">
      <Button variant="secondary" onclick={onClose}>
        {$t('app.close')}
      </Button>
    </div>
  </div>
</Modal>

<style>
  .about-modal-body {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-5);
    max-height: 75vh;
    overflow-y: auto;
    padding-right: var(--spacing-1);
    scrollbar-width: thin;
  }

  .diag-section {
    background: var(--surface-tint-faint);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    padding: var(--spacing-4);
    display: flex;
    flex-direction: column;
    gap: var(--spacing-3);
  }

  .section-head {
    display: flex;
    align-items: center;
    gap: var(--spacing-2);
  }

  .section-title {
    font-size: var(--font-size-base);
    font-weight: 600;
    color: var(--fg-primary);
    margin: 0;
  }

  .section-desc {
    font-size: var(--font-size-xs);
    color: var(--fg-secondary);
    margin: calc(-1 * var(--spacing-1-5)) 0 var(--spacing-1);
    line-height: 1.4;
  }

  .diag-grid {
    display: grid;
    grid-template-columns: repeat(2, minmax(0, 1fr));
    gap: var(--spacing-2-5);
  }

  @media (max-width: 480px) {
    .diag-grid {
      grid-template-columns: 1fr;
    }
  }

  .diag-item {
    background: rgba(0, 0, 0, 0.2);
    border: 1px solid var(--border-light);
    border-radius: var(--radius-sm);
    padding: var(--spacing-2) var(--spacing-3);
    display: flex;
    flex-direction: column;
    gap: 3px;
  }

  .diag-item-full {
    grid-column: 1 / -1;
  }

  .diag-label {
    font-size: var(--font-size-xs);
    color: var(--fg-secondary);
    font-weight: 500;
  }

  .diag-val {
    font-size: var(--font-size-sm);
    font-weight: 600;
    color: var(--fg-primary);
    word-break: break-all;
  }

  .mono {
    font-family: var(--font-family-mono);
  }

  .text-success {
    color: var(--success);
  }

  .text-error {
    color: var(--danger);
  }

  .modal-actions-footer {
    display: flex;
    justify-content: flex-end;
    padding-top: var(--spacing-1);
  }
</style>
