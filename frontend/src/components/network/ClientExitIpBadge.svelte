<script lang="ts">
  import { onMount, onDestroy } from 'svelte';
  import { t, currentLang } from '../../i18n';
  import { showToast } from '../../stores';
  import { clientExitIpStore, fetchClientExitIP, type ClientExitIPInfo } from '../../lib/clientIp';

  let { compact = false }: { compact?: boolean } = $props();

  let isOpen = $state(false);
  let containerRef: HTMLDivElement | null = $state(null);

  const info = $derived<ClientExitIPInfo>($clientExitIpStore);
  const isLoading = $derived(info.status === 'loading');

  function toggleOpen() {
    isOpen = !isOpen;
    if (isOpen && info.status === 'idle') {
      fetchClientExitIP(false, $currentLang);
    }
  }

  function handleRefresh(e?: MouseEvent) {
    if (e) e.stopPropagation();
    fetchClientExitIP(true, $currentLang);
  }

  function handleCopy(text: string, e?: MouseEvent) {
    if (e) e.stopPropagation();
    if (!text) return;
    navigator.clipboard.writeText(text).then(() => {
      showToast('success', $t('app.copied'));
    });
  }

  function handleClickOutside(event: MouseEvent) {
    if (isOpen && containerRef && !containerRef.contains(event.target as Node)) {
      isOpen = false;
    }
  }

  function handleKeyDown(event: KeyboardEvent) {
    if (event.key === 'Escape' && isOpen) {
      isOpen = false;
    }
  }

  onMount(() => {
    document.addEventListener('click', handleClickOutside);
    document.addEventListener('keydown', handleKeyDown);
    if (info.status === 'idle') {
      fetchClientExitIP(false, $currentLang);
    }
  });

  onDestroy(() => {
    document.removeEventListener('click', handleClickOutside);
    document.removeEventListener('keydown', handleKeyDown);
  });
</script>

<div class="client-ip-wrapper" bind:this={containerRef}>
  <div
    class="client-ip-pill"
    class:is-loading={isLoading}
    class:is-open={isOpen}
    class:is-proxied={info.isProxied === true}
    class:is-direct={info.isProxied === false}
  >
    <button
      type="button"
      class="pill-main"
      onclick={toggleOpen}
      title={$t('net.client_exit_ip_desc')}
      aria-expanded={isOpen}
      aria-haspopup="dialog"
    >
      <svg
        class="icon-globe"
        width="13"
        height="13"
        viewBox="0 0 24 24"
        fill="none"
        stroke="currentColor"
        stroke-width="2"
        aria-hidden="true"
      >
        <circle cx="12" cy="12" r="10" />
        <line x1="2" y1="12" x2="22" y2="12" />
        <path
          d="M12 2a15.3 15.3 0 0 1 4 10 15.3 15.3 0 0 1-4 10 15.3 15.3 0 0 1-4-10 15.3 15.3 0 0 1 4-10z"
        />
      </svg>

      {#if info.status === 'loading' && !info.ip}
        <span class="pill-label">{$t('net.checking_ip')}</span>
      {:else if info.ip}
        <span class="pill-ip">{info.ip}</span>
        {#if info.flag}
          <span class="flag-icon" aria-hidden="true">{info.flag}</span>
        {/if}
        {#if info.isProxied !== undefined}
          <span
            class="routing-badge"
            class:badge-proxy={info.isProxied}
            class:badge-direct={!info.isProxied}
          >
            <span class="dot" aria-hidden="true"></span>
            <span class="badge-text">
              {info.isProxied ? $t('net.status_proxied') : $t('net.status_direct')}
            </span>
          </span>
        {/if}
      {:else}
        <span class="pill-label">{$t('net.check_client_ip')}</span>
      {/if}
    </button>

    <button
      type="button"
      class="refresh-icon-btn"
      class:spinning={isLoading}
      onclick={handleRefresh}
      title={$t('app.refresh')}
      aria-label={$t('app.refresh')}
    >
      <svg
        width="11"
        height="11"
        viewBox="0 0 24 24"
        fill="none"
        stroke="currentColor"
        stroke-width="2.5"
      >
        <path d="M21.5 2v6h-6M2.5 22v-6h6" />
        <path d="M2 11.5a10 10 0 0 1 18.8-4.3M22 12.5a10 10 0 0 1-18.8 4.2" />
      </svg>
    </button>
  </div>

  {#if isOpen}
    <div class="client-ip-popover" role="dialog" aria-label={$t('net.ip_diag_title')}>
      <div class="popover-header">
        <div class="popover-title-row">
          <span class="popover-title">{$t('net.ip_diag_title')}</span>
          {#if info.isProxied !== undefined}
            <span
              class="popover-status-chip"
              class:chip-proxy={info.isProxied}
              class:chip-direct={!info.isProxied}
            >
              {info.isProxied ? $t('net.status_proxied') : $t('net.status_direct')}
            </span>
          {/if}
        </div>
        <p class="popover-subtitle">{$t('net.client_exit_ip_desc')}</p>
      </div>

      <div class="popover-body">
        <!-- Client Exit IP row -->
        <div class="detail-row">
          <div class="detail-meta">
            <span class="detail-label">{$t('net.client_exit_ip')}</span>
            <div class="detail-val-copy">
              <span class="detail-value mono">{info.ip || '—'}</span>
              {#if info.ip}
                <button
                  type="button"
                  class="btn-copy-val"
                  onclick={(e) => handleCopy(info.ip, e)}
                  title={$t('app.copy')}
                >
                  <svg
                    width="11"
                    height="11"
                    viewBox="0 0 24 24"
                    fill="none"
                    stroke="currentColor"
                    stroke-width="2"
                  >
                    <rect x="9" y="9" width="13" height="13" rx="2" ry="2" />
                    <path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1" />
                  </svg>
                </button>
              {/if}
            </div>
          </div>
        </div>

        <!-- Location -->
        {#if info.countryName || info.city || info.countryCode}
          <div class="detail-row">
            <span class="detail-label">{$t('net.location')}</span>
            <span class="detail-value">
              {#if info.flag}<span class="flag-icon">{info.flag}</span>{/if}
              {[info.city, info.countryName || info.countryCode].filter(Boolean).join(', ')}
            </span>
          </div>
        {/if}

        <!-- ISP / ASN -->
        {#if info.isp || info.asn}
          <div class="detail-row">
            <span class="detail-label">{$t('net.isp_asn')}</span>
            <span class="detail-value text-muted truncate">
              {info.isp}
              {#if info.asn && info.isp !== info.asn}({info.asn}){/if}
            </span>
          </div>
        {/if}

        <!-- Router WAN IP (comparison) -->
        {#if info.routerWanIp}
          <div class="detail-row">
            <div class="detail-meta">
              <span class="detail-label">{$t('net.router_wan_ip')}</span>
              <span class="detail-value mono text-muted">{info.routerWanIp}</span>
            </div>
          </div>
        {/if}

        <!-- Routing description box -->
        <div
          class="routing-explainer"
          class:is-proxy={info.isProxied}
          class:is-direct={info.isProxied === false}
        >
          <div class="explainer-icon">
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
          <div class="explainer-text">
            {info.isProxied ? $t('net.status_proxied_desc') : $t('net.status_direct_desc')}
          </div>
        </div>

        {#if info.error}
          <div class="error-msg">{info.error}</div>
        {/if}
      </div>

      <div class="popover-footer">
        <button
          type="button"
          class="btn btn-secondary btn-sm"
          disabled={isLoading}
          onclick={handleRefresh}
        >
          <svg
            class:spinning={isLoading}
            width="12"
            height="12"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            stroke-width="2.5"
          >
            <path d="M21.5 2v6h-6M2.5 22v-6h6" />
            <path d="M2 11.5a10 10 0 0 1 18.8-4.3M22 12.5a10 10 0 0 1-18.8 4.2" />
          </svg>
          <span>{isLoading ? $t('net.checking_ip') : $t('app.refresh')}</span>
        </button>
      </div>
    </div>
  {/if}
</div>

<style>
  .client-ip-wrapper {
    position: relative;
    display: inline-flex;
    align-items: center;
  }

  .client-ip-pill {
    display: inline-flex;
    align-items: center;
    height: 30px;
    padding: 0 4px 0 0;
    background: var(--bg-card);
    border: 1px solid var(--border-color);
    border-radius: var(--radius-full, 9999px);
    font-size: 12px;
    color: var(--fg-primary);
    transition: all 0.18s ease;
    user-select: none;
    box-shadow: 0 1px 2px rgba(0, 0, 0, 0.04);
  }

  .pill-main {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    height: 100%;
    padding: 0 6px 0 10px;
    background: transparent;
    border: none;
    font: inherit;
    color: inherit;
    cursor: pointer;
  }

  .client-ip-pill:hover {
    border-color: var(--color-primary, #0070f3);
    background: var(--bg-card-hover, var(--bg-hover));
  }

  .client-ip-pill.is-open {
    border-color: var(--color-primary, #0070f3);
    box-shadow: 0 0 0 2px color-mix(in srgb, var(--color-primary, #0070f3) 20%, transparent);
  }

  .icon-globe {
    color: var(--fg-muted);
    flex-shrink: 0;
  }

  .pill-ip {
    font-family: var(--font-family-mono, monospace);
    font-weight: 500;
    font-size: 11.5px;
    letter-spacing: -0.2px;
  }

  .pill-label {
    font-size: 11.5px;
    color: var(--fg-secondary);
  }

  .flag-icon {
    font-size: 13px;
    line-height: 1;
    display: inline-flex;
    align-items: center;
  }

  .routing-badge {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    padding: 1.5px 6px;
    border-radius: 9999px;
    font-size: 10.5px;
    font-weight: 600;
  }

  .badge-proxy {
    background: color-mix(in srgb, #10b981 14%, transparent);
    color: #10b981;
  }

  .badge-direct {
    background: color-mix(in srgb, #3b82f6 14%, transparent);
    color: #3b82f6;
  }

  .routing-badge .dot {
    width: 5px;
    height: 5px;
    border-radius: 50%;
    background: currentColor;
  }

  .refresh-icon-btn {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 18px;
    height: 18px;
    border: none;
    background: transparent;
    color: var(--fg-muted);
    border-radius: 50%;
    cursor: pointer;
    padding: 0;
    transition:
      color 0.15s ease,
      transform 0.15s ease;
  }

  .refresh-icon-btn:hover {
    color: var(--fg-primary);
    background: var(--bg-hover);
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

  /* Popover */
  .client-ip-popover {
    position: absolute;
    top: calc(100% + 6px);
    right: 0;
    width: 290px;
    background: var(--bg-card);
    border: 1px solid var(--border-color);
    border-radius: var(--radius-lg, 12px);
    box-shadow:
      0 10px 25px -5px rgba(0, 0, 0, 0.25),
      0 8px 10px -6px rgba(0, 0, 0, 0.15);
    z-index: 100;
    animation: popoverFadeIn 0.15s ease;
    overflow: hidden;
  }

  @keyframes popoverFadeIn {
    from {
      opacity: 0;
      transform: translateY(-4px);
    }
    to {
      opacity: 1;
      transform: translateY(0);
    }
  }

  .popover-header {
    padding: 12px 14px 10px;
    border-bottom: 1px solid var(--border-color);
    background: color-mix(in srgb, var(--bg-card) 95%, var(--bg-hover));
  }

  .popover-title-row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 8px;
  }

  .popover-title {
    font-size: 13px;
    font-weight: 600;
    color: var(--fg-primary);
  }

  .popover-subtitle {
    font-size: 11px;
    color: var(--fg-muted);
    margin: 2px 0 0;
    line-height: 1.35;
  }

  .popover-status-chip {
    padding: 2px 7px;
    border-radius: 9999px;
    font-size: 10px;
    font-weight: 600;
  }

  .chip-proxy {
    background: color-mix(in srgb, #10b981 15%, transparent);
    color: #10b981;
    border: 1px solid color-mix(in srgb, #10b981 30%, transparent);
  }

  .chip-direct {
    background: color-mix(in srgb, #3b82f6 15%, transparent);
    color: #3b82f6;
    border: 1px solid color-mix(in srgb, #3b82f6 30%, transparent);
  }

  .popover-body {
    padding: 10px 14px;
    display: flex;
    flex-direction: column;
    gap: 8px;
  }

  .detail-row {
    display: flex;
    flex-direction: column;
    gap: 2px;
  }

  .detail-meta {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 6px;
  }

  .detail-label {
    font-size: 10.5px;
    text-transform: uppercase;
    letter-spacing: 0.5px;
    font-weight: 600;
    color: var(--fg-muted);
  }

  .detail-val-copy {
    display: inline-flex;
    align-items: center;
    gap: 4px;
  }

  .detail-value {
    font-size: 12px;
    font-weight: 500;
    color: var(--fg-primary);
    display: inline-flex;
    align-items: center;
    gap: 5px;
  }

  .detail-value.mono {
    font-family: var(--font-family-mono, monospace);
  }

  .detail-value.text-muted {
    color: var(--fg-secondary);
  }

  .btn-copy-val {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 18px;
    height: 18px;
    padding: 0;
    background: transparent;
    border: none;
    color: var(--fg-muted);
    border-radius: 4px;
    cursor: pointer;
    transition:
      color 0.15s,
      background 0.15s;
  }

  .btn-copy-val:hover {
    color: var(--fg-primary);
    background: var(--bg-hover);
  }

  .routing-explainer {
    display: flex;
    align-items: flex-start;
    gap: 8px;
    padding: 8px 10px;
    border-radius: var(--radius-md, 8px);
    font-size: 11px;
    line-height: 1.4;
    margin-top: 4px;
  }

  .routing-explainer.is-proxy {
    background: color-mix(in srgb, #10b981 10%, transparent);
    border: 1px solid color-mix(in srgb, #10b981 25%, transparent);
    color: var(--fg-primary);
  }

  .routing-explainer.is-proxy .explainer-icon {
    color: #10b981;
  }

  .routing-explainer.is-direct {
    background: color-mix(in srgb, #3b82f6 10%, transparent);
    border: 1px solid color-mix(in srgb, #3b82f6 25%, transparent);
    color: var(--fg-primary);
  }

  .routing-explainer.is-direct .explainer-icon {
    color: #3b82f6;
  }

  .explainer-icon {
    flex-shrink: 0;
    margin-top: 1px;
  }

  .error-msg {
    font-size: 11px;
    color: var(--color-danger, #ef4444);
  }

  .popover-footer {
    display: flex;
    justify-content: flex-end;
    padding: 8px 14px;
    border-top: 1px solid var(--border-color);
    background: color-mix(in srgb, var(--bg-card) 98%, var(--bg-hover));
  }

  .btn-sm {
    padding: 4px 10px;
    font-size: 11.5px;
    height: 26px;
    display: inline-flex;
    align-items: center;
    gap: 5px;
  }

  @media (max-width: 600px) {
    .badge-text {
      display: none;
    }

    .client-ip-popover {
      right: auto;
      left: 0;
      width: calc(100vw - 32px);
      max-width: 320px;
    }
  }
</style>
