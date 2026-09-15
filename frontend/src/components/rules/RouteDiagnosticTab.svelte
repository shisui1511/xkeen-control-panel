<script lang="ts">
  import { t } from '../../i18n';
  import { testRoute, type RouteTraceResult } from '../../lib/api';
  import Button from '../Button.svelte';
  import Icon from '../../lib/components/Icon.svelte';

  let inputTarget = $state('');
  let testing = $state(false);
  let error = $state('');
  let result = $state<RouteTraceResult | null>(null);

  async function handleTest(e?: Event) {
    if (e) e.preventDefault();
    const cleanTarget = inputTarget.trim();
    if (!cleanTarget) return;

    testing = true;
    error = '';
    result = null;

    try {
      // Parse optional port if formatted as host:port
      let target = cleanTarget;
      let port: number | undefined;

      // Handle IPv6 [::1]:port or host:port
      if (cleanTarget.startsWith('[') && cleanTarget.includes(']:')) {
        const parts = cleanTarget.split(']:');
        target = parts[0].replace('[', '');
        port = parseInt(parts[1], 10) || undefined;
      } else if (!cleanTarget.includes('::') && cleanTarget.includes(':')) {
        const parts = cleanTarget.split(':');
        target = parts[0];
        port = parseInt(parts[1], 10) || undefined;
      }

      const res = await testRoute(target, port);
      result = res;
    } catch (err: any) {
      error = err.message || $t('rules.diagnostic_error');
    } finally {
      testing = false;
    }
  }

  function getActionBadgeClass(action: string): string {
    const act = (action || '').toUpperCase();
    if (act === 'DIRECT') return 'badge-direct';
    if (act === 'REJECT') return 'badge-reject';
    return 'badge-proxy';
  }

  function getSourceBadge(source: string): { label: string; className: string } {
    if (source === 'user') {
      return { label: $t('rules.diagnostic_source_user'), className: 'source-user' };
    }
    if (source === 'fallback') {
      return { label: $t('rules.diagnostic_source_fallback'), className: 'source-fallback' };
    }
    return { label: $t('rules.diagnostic_source_kernel'), className: 'source-kernel' };
  }
</script>

<div class="diagnostic-container">
  <!-- Query Form Card -->
  <div class="card diagnostic-card">
    <div class="diagnostic-header">
      <div>
        <h3 class="diagnostic-title">{$t('rules.diagnostic_title')}</h3>
        <p class="diagnostic-subtitle">{$t('rules.diagnostic_subtitle')}</p>
      </div>
    </div>

    <form class="diagnostic-form" onsubmit={handleTest}>
      <div class="input-wrapper">
        <input
          type="text"
          class="input font-mono diagnostic-input"
          placeholder={$t('rules.diagnostic_input_placeholder')}
          bind:value={inputTarget}
          disabled={testing}
          required
        />
      </div>
      <Button
        variant="primary"
        type="submit"
        loading={testing}
        disabled={testing || !inputTarget.trim()}
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
        >
          <circle cx="11" cy="11" r="8" />
          <line x1="21" y1="21" x2="16.65" y2="16.65" />
        </svg>
        <span>{testing ? $t('rules.diagnostic_testing') : $t('rules.diagnostic_run')}</span>
      </Button>
    </form>
  </div>

  <!-- Error Alert -->
  {#if error}
    <div class="alert alert-danger" role="alert">
      <Icon name="warning" size={16} />
      <span>{error}</span>
    </div>
  {/if}

  <!-- Diagnostic Result Flow -->
  {#if result}
    <div class="card result-card">
      <div class="result-header">
        <span class="result-title">
          {$t('rules.diagnostic_step_request')}:
          <span class="mono target-highlight">{result.target}</span>
        </span>
        <span class="trace-latency">
          {$t('rules.diagnostic_latency', { time: result.trace_time_ms.toFixed(2) })}
        </span>
      </div>

      <div class="flow-container">
        <!-- Step 1: Request -->
        <div class="flow-step">
          <div class="step-badge">1</div>
          <div class="step-card">
            <span class="step-label">{$t('rules.diagnostic_step_request')}</span>
            <span class="step-value mono text-truncate" title={result.target}>{result.target}</span>
          </div>
        </div>

        <div class="flow-arrow" aria-hidden="true">
          <svg
            width="20"
            height="20"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            stroke-width="2"
          >
            <polyline points="9 18 15 12 9 6" />
          </svg>
        </div>

        <!-- Step 2: Matched Rule -->
        <div class="flow-step">
          <div class="step-badge">2</div>
          <div class="step-card">
            <div class="step-header-row">
              <span class="step-label">{$t('rules.diagnostic_step_rule')}</span>
              {#if result.source}
                {@const sBadge = getSourceBadge(result.source)}
                <span class="badge {sBadge.className}">{sBadge.label}</span>
              {/if}
            </div>
            <div class="rule-details">
              <span class="badge rule-type-badge">{result.rule_type}</span>
              <span class="step-value mono text-truncate" title={result.rule_payload}>
                {result.rule_payload || '—'}
              </span>
            </div>
          </div>
        </div>

        <div class="flow-arrow" aria-hidden="true">
          <svg
            width="20"
            height="20"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            stroke-width="2"
          >
            <polyline points="9 18 15 12 9 6" />
          </svg>
        </div>

        <!-- Step 3: Target Group / Action -->
        <div class="flow-step">
          <div class="step-badge">3</div>
          <div class="step-card">
            <span class="step-label">{$t('rules.diagnostic_step_group')}</span>
            <div class="group-details">
              <span class="badge {getActionBadgeClass(result.target_action)}">
                {result.target_action}
              </span>
              {#if result.target_group && result.target_group !== result.target_action}
                <span class="step-value text-truncate" title={result.target_group}>
                  {result.target_group}
                </span>
              {/if}
            </div>
          </div>
        </div>

        <div class="flow-arrow" aria-hidden="true">
          <svg
            width="20"
            height="20"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            stroke-width="2"
          >
            <polyline points="9 18 15 12 9 6" />
          </svg>
        </div>

        <!-- Step 4: Selected Node -->
        <div class="flow-step">
          <div class="step-badge">4</div>
          <div class="step-card">
            <span class="step-label">{$t('rules.diagnostic_step_node')}</span>
            <div class="node-details">
              <span
                class="step-value text-truncate node-name"
                title={result.selected_proxy || result.target_action}
              >
                {result.selected_proxy || result.target_action}
              </span>
              {#if result.proxy_type}
                <span class="badge proxy-type-badge">{result.proxy_type}</span>
              {/if}
            </div>
          </div>
        </div>
      </div>
    </div>
  {:else if !testing}
    <!-- Empty / Initial Guide Card -->
    <div class="card empty-guide-card">
      <div class="guide-icon">
        <svg
          width="32"
          height="32"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          stroke-width="1.75"
        >
          <circle cx="12" cy="12" r="10" />
          <line x1="2" y1="12" x2="22" y2="12" />
          <path
            d="M12 2a15.3 15.3 0 0 1 4 10 15.3 15.3 0 0 1-4 10 15.3 15.3 0 0 1-4-10 15.3 15.3 0 0 1 4-10z"
          />
        </svg>
      </div>
      <h4 class="guide-title">{$t('rules.diagnostic_title')}</h4>
      <p class="guide-desc">{$t('rules.diagnostic_empty_hint')}</p>
    </div>
  {/if}
</div>

<style>
  .diagnostic-container {
    display: flex;
    flex-direction: column;
    gap: 16px;
  }

  .diagnostic-card {
    background: var(--bg-card);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    padding: 16px 20px;
  }

  .diagnostic-header {
    margin-bottom: 12px;
  }

  .diagnostic-title {
    font-size: var(--font-size-base);
    font-weight: 600;
    color: var(--fg-primary);
    margin: 0 0 4px 0;
  }

  .diagnostic-subtitle {
    font-size: var(--font-size-sm);
    color: var(--fg-secondary);
    margin: 0;
  }

  .diagnostic-form {
    display: flex;
    gap: 10px;
    align-items: center;
  }

  .input-wrapper {
    flex: 1;
    min-width: 0;
  }

  .diagnostic-input {
    width: 100%;
    padding: 8px 12px;
    background: var(--bg-surface);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    color: var(--fg-primary);
    font-size: var(--font-size-base);
    box-sizing: border-box;
    transition: border-color var(--transition-fast, 0.15s ease);
  }

  .diagnostic-input:focus {
    outline: none;
    border-color: var(--accent);
    box-shadow: 0 0 0 2px var(--accent-soft);
  }

  .btn-icon {
    margin-right: 6px;
    flex-shrink: 0;
  }

  .alert {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 12px 16px;
    border-radius: var(--radius-md);
    font-size: var(--font-size-sm);
  }

  .alert-danger {
    background: color-mix(in srgb, var(--danger) 12%, transparent);
    border: 1px solid color-mix(in srgb, var(--danger) 30%, transparent);
    color: var(--danger);
  }

  .result-card {
    background: var(--bg-card);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    padding: 20px;
  }

  .result-header {
    display: flex;
    justify-content: space-between;
    align-items: center;
    padding-bottom: 14px;
    margin-bottom: 16px;
    border-bottom: 1px solid var(--border);
    flex-wrap: wrap;
    gap: 8px;
  }

  .result-title {
    font-size: var(--font-size-base);
    font-weight: 600;
    color: var(--fg-primary);
  }

  .target-highlight {
    color: var(--accent);
    padding: 2px 6px;
    background: var(--accent-soft);
    border-radius: 4px;
  }

  .trace-latency {
    font-size: var(--font-size-xs);
    font-family: var(--font-family-mono, monospace);
    color: var(--fg-secondary);
    background: var(--surface-2);
    padding: 3px 8px;
    border-radius: var(--radius-sm);
    border: 1px solid var(--border-light, var(--border));
  }

  .flow-container {
    display: grid;
    grid-template-columns: 1fr auto 1fr auto 1fr auto 1fr;
    align-items: center;
    gap: 8px;
  }

  .flow-step {
    display: flex;
    flex-direction: column;
    gap: 6px;
    min-width: 0;
  }

  .step-badge {
    align-self: flex-start;
    width: 20px;
    height: 20px;
    border-radius: 50%;
    background: var(--surface-2);
    color: var(--fg-secondary);
    font-size: var(--font-size-xs);
    font-weight: 700;
    display: flex;
    align-items: center;
    justify-content: center;
    border: 1px solid var(--border);
  }

  .step-card {
    background: var(--bg-surface);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    padding: 12px;
    display: flex;
    flex-direction: column;
    gap: 6px;
    min-height: 72px;
    justify-content: center;
  }

  .step-label {
    font-size: var(--font-size-xs);
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.04em;
    color: var(--fg-muted);
  }

  .step-header-row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 6px;
  }

  .step-value {
    font-size: var(--font-size-sm);
    font-weight: 500;
    color: var(--fg-primary);
  }

  .node-name {
    font-weight: 600;
    color: var(--accent);
  }

  .rule-details,
  .group-details,
  .node-details {
    display: flex;
    align-items: center;
    gap: 6px;
    min-width: 0;
    flex-wrap: wrap;
  }

  .flow-arrow {
    display: flex;
    align-items: center;
    justify-content: center;
    color: var(--fg-dim);
  }

  .badge {
    font-size: 10px;
    font-weight: 600;
    padding: 2px 6px;
    border-radius: 4px;
    letter-spacing: 0.02em;
    text-transform: uppercase;
    white-space: nowrap;
  }

  .source-user {
    background: color-mix(in srgb, var(--accent) 15%, transparent);
    color: var(--accent);
    border: 1px solid color-mix(in srgb, var(--accent) 30%, transparent);
  }

  .source-kernel {
    background: color-mix(in srgb, var(--fg-dim) 15%, transparent);
    color: var(--fg-secondary);
    border: 1px solid var(--border);
  }

  .source-fallback {
    background: color-mix(in srgb, var(--warning) 15%, transparent);
    color: var(--warning);
    border: 1px solid color-mix(in srgb, var(--warning) 30%, transparent);
  }

  .rule-type-badge {
    background: var(--surface-2);
    color: var(--fg-secondary);
    border: 1px solid var(--border);
  }

  .badge-direct {
    background: color-mix(in srgb, var(--success) 15%, transparent);
    color: var(--success);
    border: 1px solid color-mix(in srgb, var(--success) 30%, transparent);
  }

  .badge-reject {
    background: color-mix(in srgb, var(--danger) 15%, transparent);
    color: var(--danger);
    border: 1px solid color-mix(in srgb, var(--danger) 30%, transparent);
  }

  .badge-proxy {
    background: color-mix(in srgb, var(--accent) 15%, transparent);
    color: var(--accent);
    border: 1px solid color-mix(in srgb, var(--accent) 30%, transparent);
  }

  .proxy-type-badge {
    background: var(--surface-2);
    color: var(--fg-dim);
    border: 1px solid var(--border);
  }

  .mono {
    font-family: var(--font-family-mono, monospace);
  }

  .text-truncate {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .empty-guide-card {
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    text-align: center;
    padding: 40px 20px;
    background: var(--bg-card);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
  }

  .guide-icon {
    width: 56px;
    height: 56px;
    border-radius: 50%;
    display: grid;
    place-items: center;
    color: var(--accent);
    background: var(--accent-soft);
    border: 1px solid var(--accent-line);
    margin-bottom: 12px;
  }

  .guide-title {
    font-size: var(--font-size-base);
    font-weight: 600;
    color: var(--fg-primary);
    margin: 0 0 6px 0;
  }

  .guide-desc {
    font-size: var(--font-size-sm);
    color: var(--fg-secondary);
    max-width: 440px;
    margin: 0;
    line-height: 1.5;
  }

  @media (max-width: 860px) {
    .flow-container {
      grid-template-columns: 1fr;
      gap: 12px;
    }

    .flow-arrow {
      transform: rotate(90deg);
      padding: 4px 0;
    }
  }

  @media (max-width: 640px) {
    .diagnostic-form {
      flex-direction: column;
      align-items: stretch;
    }
  }
</style>
