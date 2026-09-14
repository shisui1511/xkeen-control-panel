<script lang="ts">
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
    background: rgba(240, 180, 80, 0.1);
    border: 1px solid rgba(240, 180, 80, 0.3);
    color: var(--warning);
  }
</style>
