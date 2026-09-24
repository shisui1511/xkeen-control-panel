<script lang="ts">
  import { t } from '../../i18n';
  import {
    checkRuleProviderURL,
    type RuleProvider,
    type RuleProviderDetails,
    type RuleProviderURLCheck
  } from '../../lib/api';
  import RuleProviderContentModal from './RuleProviderContentModal.svelte';
  import EmptyState from '../EmptyState.svelte';
  import Button from '../Button.svelte';
  import DatIcon from '../../lib/components/icons/Dat.svelte';

  interface Props {
    providers: RuleProvider[];
    /** Config-level details by provider name (file state, URL). */
    details?: Record<string, RuleProviderDetails>;
    loading?: boolean;
    onUpdate: (name: string) => Promise<void>;
    onUpdateAll: () => Promise<void>;
  }

  let { providers = [], details = {}, loading = false, onUpdate, onUpdateAll }: Props = $props();

  let updatingProvider = $state<string | null>(null);
  let updatingAll = $state(false);
  let contentName = $state<string | null>(null);
  let checkingURL = $state<string | null>(null);
  let urlChecks = $state<Record<string, RuleProviderURLCheck>>({});

  const emptyCount = $derived(providers.filter((p) => isEmpty(p)).length);

  function isInline(p: RuleProvider): boolean {
    return (details[p.name]?.type || p.vehicleType || '').toLowerCase() === 'inline';
  }

  function isEmpty(p: RuleProvider): boolean {
    return !isInline(p) && p.ruleCount === 0;
  }

  async function handleCheckURL(name: string) {
    if (checkingURL) return;
    checkingURL = name;
    try {
      urlChecks[name] = await checkRuleProviderURL(name);
    } catch (e: any) {
      if (e?.status === 401) return;
      urlChecks[name] = {
        name,
        url: '',
        ok: false,
        error: e?.message || String(e),
        duration_ms: 0
      };
    } finally {
      checkingURL = null;
    }
  }

  function urlCheckText(c: RuleProviderURLCheck): string {
    if (c.ok) return $t('rules.provider_url_ok', { code: String(c.status_code ?? '') });
    if (c.status_code) return $t('rules.provider_url_http_error', { code: String(c.status_code) });
    return $t('rules.provider_url_error', { error: c.error || '' });
  }

  async function handleUpdate(name: string) {
    if (updatingProvider || updatingAll) return;
    updatingProvider = name;
    try {
      await onUpdate(name);
    } finally {
      updatingProvider = null;
    }
  }

  async function handleUpdateAll() {
    if (updatingAll || updatingProvider) return;
    updatingAll = true;
    try {
      await onUpdateAll();
    } finally {
      updatingAll = false;
    }
  }

  function formatRelativeTime(isoDate: string): string {
    if (!isoDate || isoDate.startsWith('0001')) return $t('rules.time_never');
    try {
      const date = new Date(isoDate);
      const now = new Date();
      const diffMs = now.getTime() - date.getTime();
      const diffMin = Math.floor(diffMs / 60000);
      if (diffMin < 1) return $t('rules.time_just_now');
      if (diffMin < 60) return $t('rules.time_min_ago', { n: diffMin });
      const diffHours = Math.floor(diffMin / 60);
      if (diffHours < 24) return $t('rules.time_h_ago', { n: diffHours });
      const diffDays = Math.floor(diffHours / 24);
      return $t('rules.time_d_ago', { n: diffDays });
    } catch {
      return isoDate;
    }
  }
</script>

<div class="providers-tab">
  <div class="tab-header">
    <div class="header-info">
      <span class="header-title">{$t('rules.tab_providers')}</span>
      <span class="header-badge">{providers.length}</span>
      {#if emptyCount > 0}
        <span class="header-warning" role="status">
          {$t('rules.providers_empty_warning', { n: String(emptyCount) })}
        </span>
      {/if}
    </div>
    {#if providers.length > 0}
      <Button
        variant="secondary"
        class="btn-sm"
        onclick={handleUpdateAll}
        loading={updatingAll}
        disabled={updatingAll || !!updatingProvider}
      >
        <svg
          width="14"
          height="14"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          stroke-width="2"
          stroke-linecap="round"
          stroke-linejoin="round"
          class="btn-icon"
          class:spinning={updatingAll}
        >
          <path d="M21 12a9 9 0 1 1-3-6.7L21 8" />
          <path d="M21 3v5h-5" />
        </svg>
        <span>{updatingAll ? $t('rules.updating') : $t('rules.update_all')}</span>
      </Button>
    {/if}
  </div>

  {#if loading}
    <div class="loading-state">
      <span class="spinner"></span>
    </div>
  {:else if providers.length === 0}
    <EmptyState
      title={$t('rules.no_providers')}
      description={$t('rules.providers_subtitle')}
      icon={DatIcon}
    />
  {:else}
    <div class="providers-grid">
      {#each providers as provider (provider.name)}
        <div class="provider-card">
          <div class="provider-body">
            <div class="provider-top">
              <span class="provider-name" title={provider.name}>{provider.name}</span>
              <div class="badges">
                <span class="badge badge-type"
                  >{provider.vehicleType || provider.type || 'HTTP'}</span
                >
                {#if provider.behavior}
                  <span class="badge badge-behavior">{provider.behavior}</span>
                {/if}
                {#if isEmpty(provider)}
                  <span class="badge badge-empty">{$t('rules.provider_status_empty')}</span>
                {/if}
                {#if details[provider.name] && !details[provider.name].file_exists}
                  <span class="badge badge-empty">{$t('rules.provider_status_no_file')}</span>
                {/if}
              </div>
            </div>

            {#if details[provider.name]?.url}
              <span class="provider-url font-mono" title={details[provider.name].url}>
                {details[provider.name].url}
              </span>
            {/if}
            {#if urlChecks[provider.name]}
              <span
                class="provider-url-check"
                class:is-ok={urlChecks[provider.name].ok}
                role="status"
              >
                {urlCheckText(urlChecks[provider.name])}
              </span>
            {/if}

            <div class="provider-meta">
              <div class="meta-item">
                <span class="meta-label">{$t('rules.provider_rules_count')}:</span>
                <span class="meta-value mono">{provider.ruleCount.toLocaleString()}</span>
              </div>
              <div class="meta-item">
                <span class="meta-label">{$t('rules.provider_updated')}:</span>
                <span class="meta-value updated-time">{formatRelativeTime(provider.updatedAt)}</span
                >
              </div>
            </div>
          </div>

          <div class="provider-actions">
            <Button
              variant="secondary"
              class="btn-sm"
              disabled={isEmpty(provider)}
              title={$t('rules.provider_content_btn_title')}
              onclick={() => (contentName = provider.name)}
            >
              <span>{$t('rules.provider_content_btn')}</span>
            </Button>
            {#if details[provider.name]?.url}
              <Button
                variant="secondary"
                class="btn-sm"
                loading={checkingURL === provider.name}
                disabled={!!checkingURL}
                title={$t('rules.provider_url_check_title')}
                onclick={() => handleCheckURL(provider.name)}
              >
                <span>{$t('rules.provider_url_check')}</span>
              </Button>
            {/if}
            <Button
              variant="secondary"
              class="btn-sm"
              onclick={() => handleUpdate(provider.name)}
              loading={updatingProvider === provider.name}
              disabled={updatingAll || updatingProvider === provider.name}
            >
              <svg
                width="13"
                height="13"
                viewBox="0 0 24 24"
                fill="none"
                stroke="currentColor"
                stroke-width="2"
                stroke-linecap="round"
                stroke-linejoin="round"
                class="btn-icon"
                class:spinning={updatingProvider === provider.name}
              >
                <path d="M21 12a9 9 0 1 1-3-6.7L21 8" />
                <path d="M21 3v5h-5" />
              </svg>
              <span>{$t('rules.update')}</span>
            </Button>
          </div>
        </div>
      {/each}
    </div>
  {/if}
</div>

{#if contentName}
  {#key contentName}
    <RuleProviderContentModal name={contentName} onclose={() => (contentName = null)} />
  {/key}
{/if}

<style>
  .header-warning {
    font-size: var(--font-size-xs);
    font-weight: 600;
    color: var(--warning);
  }

  .badge-empty {
    background: var(--warning-soft);
    color: var(--warning);
  }

  .provider-url {
    display: block;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-size: var(--font-size-xs);
    color: var(--fg-muted);
  }

  .provider-url-check {
    font-size: var(--font-size-xs);
    color: var(--danger);
  }

  .provider-url-check.is-ok {
    color: var(--success);
  }

  .providers-tab {
    display: flex;
    flex-direction: column;
    gap: 16px;
  }

  .tab-header {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
  }

  .header-info {
    display: flex;
    align-items: center;
    gap: 8px;
  }

  .header-title {
    font-size: var(--font-size-base);
    font-weight: 600;
    color: var(--fg-primary);
  }

  .header-badge {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    min-width: 20px;
    height: 20px;
    padding: 0 6px;
    font-size: var(--font-size-xs);
    font-weight: 600;
    background: var(--bg-elevated);
    color: var(--fg-secondary);
    border: 1px solid var(--border);
    border-radius: var(--radius-full, 9999px);
  }

  .loading-state {
    display: flex;
    justify-content: center;
    align-items: center;
    padding: 48px;
  }

  .spinner {
    width: 28px;
    height: 28px;
    border: 2px solid var(--border);
    border-top-color: var(--accent);
    border-radius: 50%;
    animation: spin 0.8s linear infinite;
  }

  .providers-grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(min(320px, 100%), 1fr));
    gap: 12px;
  }

  .provider-card {
    min-width: 0;
    display: flex;
    flex-direction: column;
    justify-content: space-between;
    background: var(--bg-card);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    padding: 14px 16px;
    transition: border-color 0.15s ease;
  }

  .provider-card:hover {
    border-color: var(--border-hover, var(--border));
  }

  .provider-body {
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: 10px;
  }

  .provider-top {
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    gap: 6px;
  }

  .provider-name {
    font-size: var(--font-size-base);
    font-weight: 600;
    color: var(--fg-primary);
    overflow-wrap: anywhere;
  }

  .badges {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 4px;
  }

  .badge {
    font-size: var(--font-size-xs);
    font-weight: 600;
    padding: 2px 6px;
    border-radius: 4px;
    letter-spacing: 0.03em;
    text-transform: uppercase;
  }

  .badge-type {
    background: color-mix(in srgb, var(--accent) 12%, transparent);
    color: var(--accent);
    border: 1px solid color-mix(in srgb, var(--accent) 25%, transparent);
  }

  .badge-behavior {
    background: color-mix(in srgb, var(--fg-dim) 12%, transparent);
    color: var(--fg-secondary);
    border: 1px solid color-mix(in srgb, var(--fg-dim) 22%, transparent);
  }

  .provider-meta {
    display: flex;
    flex-direction: column;
    gap: 4px;
    font-size: var(--font-size-xs);
  }

  .meta-item {
    display: flex;
    align-items: center;
    gap: 6px;
  }

  .meta-label {
    color: var(--fg-muted);
  }

  .meta-value {
    color: var(--fg-secondary);
  }

  .mono {
    font-family: var(--font-family-mono, monospace);
  }

  .updated-time {
    color: var(--fg-faint, var(--fg-dim));
  }

  .provider-actions {
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
    justify-content: flex-end;
    margin-top: 12px;
    padding-top: 10px;
    border-top: 1px solid var(--border-light, var(--border));
  }

  .provider-actions :global(.btn) {
    flex: 1 1 auto;
  }

  .btn-icon {
    margin-right: 4px;
    flex-shrink: 0;
  }

  .spinning {
    animation: spin 0.8s linear infinite;
  }

  @keyframes spin {
    to {
      transform: rotate(360deg);
    }
  }

  @media (max-width: 640px) {
    .providers-grid {
      grid-template-columns: 1fr;
    }
  }
</style>
