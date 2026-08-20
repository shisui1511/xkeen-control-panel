<script lang="ts">
  import { t } from '../../i18n';
  import Icon from '../../lib/components/Icon.svelte';
  import Modal from '../Modal.svelte';
  import Button from '../Button.svelte';
  import type { SystemStats } from './SystemResourcesWidget.svelte';

  let {
    isOpen = false,
    onClose,
    systemStats,
    panelVersion
  } = $props<{
    isOpen: boolean;
    onClose: () => void;
    systemStats: SystemStats | null;
    panelVersion: string;
  }>();

  const goRuntime = $derived(systemStats?.go_runtime);
</script>

<Modal
  {isOpen}
  title={$t('dash.system_about_title') || 'О системе и диагностика'}
  maxWidth="640px"
  onclose={onClose}
  dataTestid="system-about-modal"
>
  <div class="about-modal-body">
    <!-- Section 1: Go Runtime Telemetry -->
    <section class="diag-section">
      <div class="section-head">
        <Icon name="console" size={16} color="var(--accent, #29c2f0)" />
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
        <Icon name="network" size={16} color="var(--success, #46d18a)" />
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
            <span class="status-pill" class:status-pill-ok={systemStats?.wan_status === 'online'}>
              {systemStats?.wan_status || '—'}
            </span>
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
        {$t('app.close') || 'Закрыть'}
      </Button>
    </div>
  </div>
</Modal>

<style>
  .about-modal-body {
    display: flex;
    flex-direction: column;
    gap: 20px;
    max-height: 75vh;
    overflow-y: auto;
    padding-right: 4px;
    scrollbar-width: thin;
  }

  .diag-section {
    background: rgba(255, 255, 255, 0.02);
    border: 1px solid var(--border, rgba(255, 255, 255, 0.08));
    border-radius: var(--radius-md, 10px);
    padding: 16px;
    display: flex;
    flex-direction: column;
    gap: 12px;
  }

  .section-head {
    display: flex;
    align-items: center;
    gap: 8px;
  }

  .section-title {
    font-size: 14px;
    font-weight: 600;
    color: var(--fg-primary, #ffffff);
    margin: 0;
  }

  .section-desc {
    font-size: 12px;
    color: var(--fg-muted, var(--fg-secondary, #8fa3b8));
    margin: -6px 0 4px;
    line-height: 1.4;
  }

  .diag-grid {
    display: grid;
    grid-template-columns: repeat(2, minmax(0, 1fr));
    gap: 10px;
  }

  @media (max-width: 540px) {
    .diag-grid {
      grid-template-columns: 1fr;
    }
  }

  .diag-item {
    background: rgba(0, 0, 0, 0.2);
    border: 1px solid rgba(255, 255, 255, 0.04);
    border-radius: var(--radius-sm, 6px);
    padding: 8px 12px;
    display: flex;
    flex-direction: column;
    gap: 3px;
  }

  .diag-item-full {
    grid-column: 1 / -1;
  }

  .diag-label {
    font-size: 11px;
    color: var(--fg-secondary, #8fa3b8);
    font-weight: 500;
  }

  .diag-val {
    font-size: 13px;
    font-weight: 600;
    color: var(--fg-primary, #ffffff);
    word-break: break-all;
  }

  .mono {
    font-family: var(--font-family-mono, monospace);
  }

  .status-pill {
    display: inline-block;
    padding: 1px 6px;
    border-radius: 4px;
    font-size: 11px;
    font-weight: 600;
    background: rgba(255, 255, 255, 0.08);
    color: var(--fg-secondary);
  }

  .status-pill-ok {
    background: rgba(70, 209, 138, 0.15);
    color: var(--success, #46d18a);
  }

  .text-success {
    color: var(--success, #46d18a);
  }

  .text-error {
    color: var(--error, #f4707f);
  }

  .modal-actions-footer {
    display: flex;
    justify-content: flex-end;
    padding-top: 4px;
  }
</style>
