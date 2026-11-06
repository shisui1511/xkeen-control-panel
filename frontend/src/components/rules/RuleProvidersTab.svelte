<script lang="ts">
  import { t } from '../../i18n';
  import {
    apiFetchJSON,
    checkRuleProviderURL,
    type RuleProvider,
    type RuleProviderDetails,
    type RuleProviderURLCheck
  } from '../../lib/api';
  import { formatDateTime } from '../../lib/nodesView';
  import RuleProviderContentModal from './RuleProviderContentModal.svelte';
  import ProviderUrlResult from './ProviderUrlResult.svelte';
  import Icon from '../../lib/components/Icon.svelte';
  import EmptyState from '../EmptyState.svelte';
  import Button from '../Button.svelte';
  import DatIcon from '../../lib/components/icons/Dat.svelte';

  interface Props {
    providers: RuleProvider[];
    /** Config-level details by provider name (file state, URL). */
    details?: Record<string, RuleProviderDetails>;
    loading?: boolean;
    /** Причина, по которой API Mihomo недоступен; null — API работает. */
    offline?: 'xray' | 'not_running' | 'api_down' | null;
    onUpdate: (name: string) => Promise<void>;
    onUpdateAll: () => Promise<void>;
  }

  let {
    providers = [],
    details = {},
    loading = false,
    offline = null,
    onUpdate,
    onUpdateAll
  }: Props = $props();

  let updatingProvider = $state<string | null>(null);
  let updatingAll = $state(false);
  let contentName = $state<string | null>(null);
  let checkingURL = $state<string | null>(null);
  let urlChecks = $state<Record<string, RuleProviderURLCheck>>({});
  // Сбой самого запроса проверки (не результат диагностики ссылки): отдельная строка.
  let urlFailures = $state<Record<string, string>>({});
  // Имя активного профиля для подписи без API; сбой чтения — config.yaml.
  let profileName = $state('config.yaml');

  // Состояние файла провайдера по данным профиля (используется без API ядра).
  type FileState = 'ok' | 'none_http' | 'none_file' | 'empty';

  function fileState(p: RuleProvider): FileState {
    const d = details[p.name];
    if (!d || isInline(p)) return 'ok';
    if (!d.file_exists) return (d.type || '').toLowerCase() === 'http' ? 'none_http' : 'none_file';
    return d.file_size ? 'ok' : 'empty';
  }

  const emptyCount = $derived(providers.filter((p) => isEmpty(p)).length);

  function isInline(p: RuleProvider): boolean {
    return (details[p.name]?.type || p.vehicleType || '').toLowerCase() === 'inline';
  }

  // Без API числа записей нет: пустыми считаются file-провайдеры без файла и файлы нулевого размера.
  function isEmpty(p: RuleProvider): boolean {
    if (offline) {
      const fs = fileState(p);
      return fs === 'none_file' || fs === 'empty';
    }
    return !isInline(p) && p.ruleCount === 0;
  }

  // «Содержимое» без файла читать нечего (inline лежит в самом профиле).
  function noContent(p: RuleProvider): boolean {
    if (!offline) return isEmpty(p);
    return fileState(p) !== 'ok';
  }

  function offlineLabel(reason: NonNullable<Props['offline']>): string {
    if (reason === 'xray') return $t('rules.offline_xray');
    return reason === 'api_down' ? $t('rules.offline_api') : $t('rules.offline_stopped');
  }

  function fileSizeText(bytes: number): string {
    return $t('rules.provider_file_size', { size: (bytes / 1024).toFixed(1) });
  }

  $effect(() => {
    if (!offline) return;
    let cancelled = false;
    apiFetchJSON<{ active?: string }>('/api/mihomo/profiles')
      .then((state) => {
        if (!cancelled && state?.active) profileName = state.active;
      })
      .catch(() => {
        // имя не прочиталось — в подписи остаётся config.yaml
      });
    return () => {
      cancelled = true;
    };
  });

  async function handleCheckURL(name: string) {
    if (checkingURL) return;
    checkingURL = name;
    delete urlFailures[name];
    try {
      urlChecks[name] = await checkRuleProviderURL(name);
    } catch (e: any) {
      if (e?.status === 401) return;
      delete urlChecks[name];
      urlFailures[name] = e?.message || String(e);
    } finally {
      checkingURL = null;
    }
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
      {#if offline}
        <span class="badge badge-info offline-badge" data-testid="rules-offline-badge"
          >{offlineLabel(offline)}</span
        >
      {/if}
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
        disabled={!!offline || updatingAll || !!updatingProvider}
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

  {#if offline}
    <p class="offline-note" data-testid="rules-offline-note">
      {$t('rules.offline_note', { profile: profileName })}
    </p>
  {/if}

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
        {@const d = details[provider.name]}
        {@const fs = offline ? fileState(provider) : 'ok'}
        <div class="provider-card" data-testid="provider-card-{provider.name}">
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
                {#if offline}
                  {#if fs === 'none_file'}
                    <span class="badge badge-error">{$t('rules.provider_badge_nofile')}</span>
                  {:else if fs === 'empty'}
                    <span class="badge badge-error">{$t('rules.provider_badge_empty_file')}</span>
                  {/if}
                {:else}
                  {#if isEmpty(provider)}
                    <span class="badge badge-empty">{$t('rules.provider_status_empty')}</span>
                  {/if}
                  {#if d && !d.file_exists}
                    <span class="badge badge-empty">{$t('rules.provider_status_no_file')}</span>
                  {/if}
                {/if}
              </div>
            </div>

            {#if d?.url}
              <span class="provider-url font-mono" class:url-full={!!offline} title={d.url}>
                {d.url}
              </span>
            {/if}
            {#if offline && d?.proxy}
              <span class="provider-via font-mono">
                {$t('rules.provider_via_proxy', { proxy: d.proxy })}
              </span>
            {/if}

            {#if offline}
              {#if fs === 'none_http'}
                <p class="provider-status" data-testid="provider-status-{provider.name}">
                  <Icon name="info" size={14} />
                  <span>{$t('rules.provider_no_file_http')}</span>
                </p>
              {:else if fs === 'none_file'}
                <p
                  class="error-text provider-wrap"
                  role="alert"
                  data-testid="provider-status-{provider.name}"
                >
                  {$t('rules.provider_no_file_local', { path: d?.path ?? '' })}
                </p>
              {/if}
            {/if}

            {#if urlChecks[provider.name] || urlFailures[provider.name]}
              <ProviderUrlResult
                name={provider.name}
                result={urlChecks[provider.name]}
                failure={urlFailures[provider.name]}
              />
            {/if}
            {#if checkingURL === provider.name}
              <span class="provider-checking" aria-live="polite">
                {$t('rules.provider_url_checking')}
              </span>
            {/if}

            <div class="provider-meta">
              {#if offline}
                {#if d?.file_exists && d.file_size}
                  <div class="meta-item">
                    <span class="meta-value mono">{fileSizeText(d.file_size)}</span>
                  </div>
                {/if}
                {#if d?.file_exists && d.file_mtime}
                  <div class="meta-item">
                    <span class="meta-value"
                      >{$t('rules.provider_file_date', {
                        date: formatDateTime(d.file_mtime * 1000)
                      })}</span
                    >
                  </div>
                {/if}
                {#if d && isInline(provider) && d.inline_count}
                  <div class="meta-item">
                    <span class="meta-label">{$t('rules.provider_rules_count')}:</span>
                    <span class="meta-value mono">{d.inline_count.toLocaleString()}</span>
                  </div>
                {/if}
              {:else}
                <div class="meta-item">
                  <span class="meta-label">{$t('rules.provider_rules_count')}:</span>
                  <span class="meta-value mono">{provider.ruleCount.toLocaleString()}</span>
                </div>
                <div class="meta-item">
                  <span class="meta-label">{$t('rules.provider_updated')}:</span>
                  <span class="meta-value updated-time"
                    >{formatRelativeTime(provider.updatedAt)}</span
                  >
                </div>
              {/if}
            </div>
          </div>

          <div class="provider-actions">
            <Button
              variant="secondary"
              class="btn-sm"
              disabled={noContent(provider)}
              title={$t('rules.provider_content_btn_title')}
              onclick={() => (contentName = provider.name)}
            >
              <span>{$t('rules.provider_content_btn')}</span>
            </Button>
            {#if d?.url}
              <Button
                variant="secondary"
                class="btn-sm"
                loading={checkingURL === provider.name}
                disabled={!!checkingURL}
                title={$t('rules.provider_url_check_title')}
                data-testid="provider-check-{provider.name}"
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
              disabled={!!offline || updatingAll || updatingProvider === provider.name}
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
  /* Светлая тема: акцентный текст на подложке бейджа даёт 4,0:1, нужно 4,5:1. */
  .offline-badge {
    color: color-mix(in srgb, var(--accent-text, var(--accent)) 65%, var(--fg-primary));
  }

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
    color: var(--fg-dim);
  }

  .provider-url.url-full {
    overflow: visible;
    text-overflow: clip;
    white-space: normal;
    overflow-wrap: anywhere;
  }

  .provider-via {
    font-size: var(--font-size-xs);
    color: var(--fg-secondary);
    overflow-wrap: anywhere;
  }

  .provider-status {
    display: flex;
    align-items: flex-start;
    gap: 6px;
    margin: 0;
    font-size: var(--font-size-xs);
    color: var(--fg-secondary);
    overflow-wrap: anywhere;
  }

  .provider-wrap {
    margin: 0;
    overflow-wrap: anywhere;
  }

  .provider-checking {
    font-size: var(--font-size-xs);
    color: var(--fg-secondary);
  }

  .offline-note {
    margin: 0;
    font-size: var(--font-size-xs);
    color: var(--fg-secondary);
    overflow-wrap: anywhere;
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
    transition: border-color var(--transition-fast);
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
    letter-spacing: 0;
  }

  .badge-type {
    background: color-mix(in srgb, var(--accent) 12%, transparent);
    color: var(--accent);
    border: 1px solid color-mix(in srgb, var(--accent) 25%, transparent);
  }

  .badge-behavior {
    background: color-mix(in srgb, var(--fg-dim) 12%, transparent);
    color: var(--fg-primary);
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
    color: var(--fg-dim);
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

  .provider-status :global(svg) {
    flex-shrink: 0;
    margin-top: 1px;
  }

  @media (max-width: 768px) {
    .providers-grid {
      grid-template-columns: 1fr;
    }

    .provider-actions :global(.btn),
    .tab-header :global(.btn) {
      min-height: 44px;
    }
  }
</style>
