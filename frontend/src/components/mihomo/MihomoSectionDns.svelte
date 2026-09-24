<script lang="ts">
  import { DEFAULT_PROXY_SERVER_NAMESERVERS } from '../../lib/mihomoYaml';
  import Select from '../Select.svelte';
  import { t } from '../../i18n';
  import { capabilities, showToast, fetchCapabilities } from '../../stores';
  import { apiFetch } from '../../lib/api';
  import { getMihomoContext } from './MihomoContext.svelte';

  let ctx = getMihomoContext();
  let dnsRedirectLoading = $state(false);

  async function enableDNSRedirect() {
    dnsRedirectLoading = true;
    try {
      const res = await apiFetch('/api/service/dns-redirect', {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json'
        },
        body: JSON.stringify({ enabled: true })
      });
      if (res.ok) {
        showToast('success', $t('mihomo.dns_intercept_enabled'));
        await fetchCapabilities();
      } else {
        const text = await res.text();
        showToast('error', text || $t('mihomo.dns_intercept_error'));
      }
    } catch (err: any) {
      if (err?.status === 401) return;
      showToast('error', err.message || String(err));
    } finally {
      dnsRedirectLoading = false;
    }
  }

  const dnsGroupOptions = $derived([
    { value: '', label: $t('mihomo.dns_proxy_group_direct') },
    ...ctx.groups
      .map((g) => g.name)
      .filter((n, idx, all) => n && all.indexOf(n) === idx)
      .map((n) => ({ value: n, label: n }))
  ]);
</script>

<div class="sec-body" data-testid="mihomo-section-dns">
  <div class="toggle-row">
    <label class="toggle-label">
      <input type="checkbox" bind:checked={ctx.dns.enabled} onchange={() => ctx.markDirty()} />
      <span>{$t('mihomo.enable_dns')}</span>
    </label>
  </div>
  {#if ctx.dns.enabled}
    {#if $capabilities?.xkeen_dns === false}
      <div
        class="alert alert-warning"
        style="margin: 0 0 16px 0; display: flex; flex-direction: column; gap: 8px; align-items: flex-start;"
        role="status"
      >
        <div style="display: flex; gap: 8px; align-items: center;">
          <span aria-hidden="true">⚠️</span>
          <span>{$t('editor.dns_intercept_warning')}</span>
        </div>
        <button
          type="button"
          class="btn btn-secondary btn-sm"
          style="font-size: 12px; padding: 4px 8px; display: flex; align-items: center; gap: 4px;"
          onclick={enableDNSRedirect}
          disabled={dnsRedirectLoading}
        >
          {#if dnsRedirectLoading}
            <span
              class="spinner"
              style="--spinner-size: 12px; --spinner-track: currentColor; --spinner-color: transparent;"
            ></span>
          {/if}
          {$t('editor.dns_intercept_enable')}
        </button>
      </div>
    {/if}
    <div class="form-row">
      <label class="form-label" for="mihomo-dns-enhanced-mode">{$t('mihomo.enhanced_mode')}</label>
      <Select
        id="mihomo-dns-enhanced-mode"
        class="form-select"
        bind:value={ctx.dns.enhancedMode}
        onchange={() => ctx.markDirty()}
      >
        <option value="fake-ip">fake-ip</option>
        <option value="redir-host">redir-host</option>
      </Select>
    </div>
    {#if ctx.dns.enhancedMode === 'fake-ip'}
      <div class="form-row">
        <label class="form-label" for="mihomo-dns-fakeip-range">Fake-IP Range</label>
        <input
          id="mihomo-dns-fakeip-range"
          class="form-input"
          bind:value={ctx.dns.fakeIPRange}
          oninput={() => ctx.markDirty()}
        />
      </div>
    {/if}
    <div class="form-row">
      <label class="form-label" for="mihomo-dns-proxy-group">{$t('mihomo.dns_proxy_group')}</label>
      <Select
        id="mihomo-dns-proxy-group"
        value={ctx.dns.proxyGroup ?? ''}
        options={dnsGroupOptions}
        onchange={(e) => {
          ctx.dns.proxyGroup = e.currentTarget.value;
          ctx.markDirty();
        }}
      />
      <p class="form-hint">{$t('mihomo.dns_proxy_group_hint')}</p>
    </div>
    {#if ctx.dns.proxyGroup}
      <div class="form-row">
        <label class="form-label" for="mihomo-dns-proxy-server-ns"
          >{$t('mihomo.dns_proxy_server_ns')}</label
        >
        <input
          id="mihomo-dns-proxy-server-ns"
          class="form-input"
          value={(ctx.dns.proxyServerNameservers?.length
            ? ctx.dns.proxyServerNameservers
            : DEFAULT_PROXY_SERVER_NAMESERVERS
          ).join(', ')}
          onchange={(e) => {
            ctx.dns.proxyServerNameservers = e.currentTarget.value.split(/[\s,]+/).filter(Boolean);
            ctx.markDirty();
          }}
        />
        <p class="form-hint">{$t('mihomo.dns_proxy_server_ns_hint')}</p>
      </div>
    {/if}
    <div class="form-row">
      <label class="form-label" for="mihomo-dns-nameservers">Nameservers</label>
      <textarea
        id="mihomo-dns-nameservers"
        class="form-textarea"
        value={ctx.dns.nameservers.join('\n')}
        rows="3"
        onchange={(e) => {
          ctx.dns.nameservers = e.currentTarget.value.split('\n').filter(Boolean);
          ctx.markDirty();
        }}></textarea>
    </div>
    <div class="form-row">
      <label class="form-label" for="mihomo-dns-fallback">Fallback</label>
      <textarea
        id="mihomo-dns-fallback"
        class="form-textarea"
        value={ctx.dns.fallback.join('\n')}
        rows="3"
        onchange={(e) => {
          ctx.dns.fallback = e.currentTarget.value.split('\n').filter(Boolean);
          ctx.markDirty();
        }}></textarea>
    </div>
  {/if}
</div>

<style>
  .sec-body {
    padding: 16px;
  }

  .toggle-row {
    margin-bottom: 12px;
  }

  .toggle-label {
    display: flex;
    align-items: center;
    gap: 8px;
    font-size: 13px;
    cursor: pointer;
    user-select: none;
    font-weight: 500;
  }

  .form-row {
    margin-bottom: 12px;
    display: flex;
    flex-direction: column;
    gap: 4px;
  }

  .form-label {
    font-size: var(--font-size-xs);
    font-weight: 500;
    color: var(--fg-secondary);
  }

  .form-input,
  .form-textarea {
    width: 100%;
    box-sizing: border-box;
    background: var(--bg-card);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    padding: 8px;
    color: var(--fg-primary);
    font-family: var(--font-mono, monospace);
    font-size: 12px;
  }

  .form-textarea {
    resize: vertical;
  }

  .alert {
    padding: 10px 12px;
    border-radius: var(--radius-sm);
  }

  .alert-warning {
    background: color-mix(in srgb, var(--warning) 12%, transparent);
    border: 1px solid color-mix(in srgb, var(--warning) 30%, transparent);
    color: var(--warning);
  }

  .form-hint {
    margin: 4px 0 0;
    font-size: var(--font-size-xs);
    color: var(--fg-muted);
  }
</style>
