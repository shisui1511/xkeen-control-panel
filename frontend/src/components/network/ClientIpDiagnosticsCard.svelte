<script lang="ts">
  import { onMount } from 'svelte';
  import { t, currentLang } from '../../i18n';
  import { showToast } from '../../stores';
  import { clientExitIpStore, fetchClientExitIP, type ClientExitIPInfo } from '../../lib/clientIp';

  const info = $derived<ClientExitIPInfo>($clientExitIpStore);
  const isLoading = $derived(info.status === 'loading');

  function handleRefresh() {
    fetchClientExitIP(true, $currentLang);
  }

  function handleCopy(text?: string) {
    if (!text) return;
    navigator.clipboard.writeText(text).then(() => {
      showToast('success', $t('app.copied'));
    });
  }

  onMount(() => {
    if (info.status === 'idle') {
      fetchClientExitIP(false, $currentLang);
    }
  });
</script>

<div class="card ip-diag-card client-ip-diagnostics-card mb-3">
  <div class="card-header">
    <div class="header-left">
      <div class="header-icon-wrap">
        <svg
          width="16"
          height="16"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          stroke-width="2"
        >
          <circle cx="12" cy="12" r="10" />
          <line x1="2" y1="12" x2="22" y2="12" />
          <path
            d="M12 2a15.3 15.3 0 0 1 4 10 15.3 15.3 0 0 1-4 10 15.3 15.3 0 0 1-4-10 15.3 15.3 0 0 1 4-10z"
          />
        </svg>
      </div>
      <div>
        <h3 class="card-title">{$t('net.ip_diag_title')}</h3>
        <p class="card-subtitle">{$t('net.ip_diag_sub')}</p>
      </div>
    </div>
    <div class="header-actions">
      <button
        type="button"
        class="btn btn-secondary btn-sm"
        disabled={isLoading}
        onclick={handleRefresh}
        title={$t('net.check_client_ip')}
      >
        <svg
          class:spinning={isLoading}
          width="13"
          height="13"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          stroke-width="2.5"
        >
          <path d="M21.5 2v6h-6M2.5 22v-6h6" />
          <path d="M2 11.5a10 10 0 0 1 18.8-4.3M22 12.5a10 10 0 0 1-18.8 4.2" />
        </svg>
        <span>{isLoading ? $t('net.checking_ip') : $t('net.check_client_ip')}</span>
      </button>
    </div>
  </div>

  <div class="card-body">
    <div class="ip-columns-grid">
      <!-- Client Device Exit IP Block -->
      <div class="ip-block primary-block">
        <div class="block-top">
          <span class="block-badge client-badge">{$t('net.client_exit_ip')}</span>
          {#if info.isProxied !== undefined}
            <span
              class="routing-status-tag route-badge"
              class:tag-proxy={info.isProxied}
              class:tag-direct={!info.isProxied}
              class:badge-proxied={info.isProxied}
            >
              <span class="status-dot"></span>
              {info.isProxied ? $t('net.status_proxied') : $t('net.status_direct')}
            </span>
          {/if}
        </div>

        <div class="ip-display-row">
          {#if isLoading && !info.ip}
            <span class="ip-placeholder">{$t('net.checking_ip')}</span>
          {:else if info.ip}
            <span class="ip-value mono">{info.ip}</span>
            {#if info.flag}
              <span class="flag-large" title={info.countryName}>{info.flag}</span>
            {/if}
            <button
              type="button"
              class="btn-copy"
              onclick={() => handleCopy(info.ip)}
              title={$t('app.copy')}
            >
              <svg
                width="13"
                height="13"
                viewBox="0 0 24 24"
                fill="none"
                stroke="currentColor"
                stroke-width="2"
              >
                <rect x="9" y="9" width="13" height="13" rx="2" ry="2" />
                <path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1" />
              </svg>
            </button>
          {:else}
            <span class="ip-placeholder text-muted"
              >{$t('net.error_with_msg', { error: info.error || '—' })}</span
            >
          {/if}
        </div>

        <!-- Meta list -->
        <div class="block-meta-list">
          {#if info.countryName || info.city}
            <div class="meta-item">
              <span class="meta-k">{$t('net.location')}</span>
              <span class="meta-v">
                {[info.city, info.countryName].filter(Boolean).join(', ')}
              </span>
            </div>
          {/if}
          {#if info.isp}
            <div class="meta-item">
              <span class="meta-k">{$t('net.isp_asn')}</span>
              <span class="meta-v truncate">
                {info.isp}
                {#if info.asn && info.isp !== info.asn}({info.asn}){/if}
              </span>
            </div>
          {/if}
        </div>

        <div
          class="routing-desc-box"
          class:is-proxy={info.isProxied}
          class:is-direct={info.isProxied === false}
        >
          <div class="box-icon">
            {#if info.isProxied}
              <svg
                width="14"
                height="14"
                viewBox="0 0 24 24"
                fill="none"
                stroke="currentColor"
                stroke-width="2"
              >
                <path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z" />
                <path d="m9 12 2 2 4-4" />
              </svg>
            {:else}
              <svg
                width="14"
                height="14"
                viewBox="0 0 24 24"
                fill="none"
                stroke="currentColor"
                stroke-width="2"
              >
                <circle cx="12" cy="12" r="10" />
                <line x1="12" y1="8" x2="12" y2="12" />
                <line x1="12" y1="16" x2="12.01" y2="16" />
              </svg>
            {/if}
          </div>
          <span class="box-text">
            {info.isProxied ? $t('net.status_proxied_desc') : $t('net.status_direct_desc')}
          </span>
        </div>
      </div>

      <!-- Router WAN IP Block -->
      <div class="ip-block secondary-block">
        <div class="block-top">
          <span class="block-badge router-badge">{$t('net.router_wan_ip')}</span>
        </div>

        <div class="ip-display-row">
          {#if info.routerWanIp}
            <span class="ip-value mono text-secondary">{info.routerWanIp}</span>
            <button
              type="button"
              class="btn-copy"
              onclick={() => handleCopy(info.routerWanIp)}
              title={$t('app.copy')}
            >
              <svg
                width="13"
                height="13"
                viewBox="0 0 24 24"
                fill="none"
                stroke="currentColor"
                stroke-width="2"
              >
                <rect x="9" y="9" width="13" height="13" rx="2" ry="2" />
                <path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1" />
              </svg>
            </button>
          {:else}
            <span class="ip-placeholder text-muted">—</span>
          {/if}
        </div>

        <div class="router-info-text">
          {$t('net.router_wan_desc')}
        </div>
      </div>
    </div>
  </div>
</div>

<style>
  .ip-diag-card {
    padding: 0;
    overflow: hidden;
  }

  .card-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 14px 18px;
    border-bottom: 1px solid var(--border-color);
    background: color-mix(in srgb, var(--bg-card) 96%, var(--bg-hover));
  }

  .header-left {
    display: flex;
    align-items: center;
    gap: 12px;
  }

  .header-icon-wrap {
    display: flex;
    align-items: center;
    justify-content: center;
    width: 32px;
    height: 32px;
    border-radius: var(--radius-md, 8px);
    background: color-mix(in srgb, var(--color-primary, #0070f3) 12%, transparent);
    color: var(--color-primary, #0070f3);
  }

  .card-title {
    margin: 0;
    font-size: 14px;
    font-weight: 600;
    color: var(--fg-primary);
  }

  .card-subtitle {
    margin: 2px 0 0;
    font-size: 12px;
    color: var(--fg-muted);
  }

  .card-body {
    padding: 16px 18px;
  }

  .ip-columns-grid {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: 16px;
  }

  .ip-block {
    display: flex;
    flex-direction: column;
    gap: 10px;
    padding: 14px 16px;
    border-radius: var(--radius-lg, 12px);
    background: var(--bg-hover);
    border: 1px solid var(--border-color);
  }

  .primary-block {
    background: color-mix(in srgb, var(--bg-card) 85%, var(--bg-hover));
  }

  .block-top {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 8px;
  }

  .block-badge {
    font-size: 11px;
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.5px;
  }

  .client-badge {
    color: var(--color-primary, #0070f3);
  }

  .router-badge {
    color: var(--fg-muted);
  }

  .routing-status-tag {
    display: inline-flex;
    align-items: center;
    gap: 5px;
    padding: 2px 8px;
    border-radius: 9999px;
    font-size: 11px;
    font-weight: 600;
  }

  .tag-proxy {
    background: color-mix(in srgb, #10b981 15%, transparent);
    color: #10b981;
    border: 1px solid color-mix(in srgb, #10b981 30%, transparent);
  }

  .tag-direct {
    background: color-mix(in srgb, #3b82f6 15%, transparent);
    color: #3b82f6;
    border: 1px solid color-mix(in srgb, #3b82f6 30%, transparent);
  }

  .status-dot {
    width: 6px;
    height: 6px;
    border-radius: 50%;
    background: currentColor;
  }

  .ip-display-row {
    display: flex;
    align-items: center;
    gap: 8px;
    flex-wrap: wrap;
  }

  .ip-value {
    font-size: 18px;
    font-weight: 600;
    letter-spacing: -0.3px;
    color: var(--fg-primary);
  }

  .ip-value.text-secondary {
    color: var(--fg-secondary);
  }

  .ip-placeholder {
    font-size: 14px;
    color: var(--fg-secondary);
  }

  .flag-large {
    font-size: 20px;
    line-height: 1;
    display: inline-flex;
    align-items: center;
  }

  .btn-copy {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 22px;
    height: 22px;
    padding: 0;
    border: none;
    background: transparent;
    color: var(--fg-muted);
    border-radius: var(--radius-sm, 4px);
    cursor: pointer;
    transition: all 0.15s;
  }

  .btn-copy:hover {
    color: var(--fg-primary);
    background: var(--bg-hover);
  }

  .block-meta-list {
    display: flex;
    flex-direction: column;
    gap: 4px;
    margin-top: 2px;
  }

  .meta-item {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 8px;
    font-size: 12px;
  }

  .meta-k {
    color: var(--fg-muted);
    font-size: 11px;
    text-transform: uppercase;
    letter-spacing: 0.4px;
    font-weight: 500;
  }

  .meta-v {
    color: var(--fg-primary);
    font-weight: 500;
  }

  .routing-desc-box {
    display: flex;
    align-items: flex-start;
    gap: 8px;
    padding: 8px 10px;
    border-radius: var(--radius-md, 8px);
    font-size: 11.5px;
    line-height: 1.4;
    margin-top: 4px;
  }

  .routing-desc-box.is-proxy {
    background: color-mix(in srgb, #10b981 10%, transparent);
    border: 1px solid color-mix(in srgb, #10b981 25%, transparent);
    color: var(--fg-primary);
  }

  .routing-desc-box.is-proxy .box-icon {
    color: #10b981;
  }

  .routing-desc-box.is-direct {
    background: color-mix(in srgb, #3b82f6 10%, transparent);
    border: 1px solid color-mix(in srgb, #3b82f6 25%, transparent);
    color: var(--fg-primary);
  }

  .routing-desc-box.is-direct .box-icon {
    color: #3b82f6;
  }

  .box-icon {
    flex-shrink: 0;
    margin-top: 1px;
  }

  .router-info-text {
    font-size: 12px;
    line-height: 1.45;
    color: var(--fg-muted);
    margin-top: auto;
  }

  .btn-sm {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    height: 30px;
    padding: 0 12px;
    font-size: 12px;
    font-weight: 500;
  }

  .spinning {
    animation: spin 0.85s linear infinite;
  }

  @keyframes spin {
    from {
      transform: rotate(0deg);
    }
    to {
      transform: rotate(360deg);
    }
  }

  @media (max-width: 768px) {
    .ip-columns-grid {
      grid-template-columns: 1fr;
    }
  }
</style>
