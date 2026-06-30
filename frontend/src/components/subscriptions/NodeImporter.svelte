<script lang="ts">
  import { t } from '../../i18n';

  interface Subscription {
    id: string;
    name: string;
  }

  interface ParseReport {
    timestamp: string;
    parsed_count: number;
    skipped_count: number;
    skipped: { line: number; reason: string; snippet: string }[];
  }

  interface RawResponse {
    headers: Record<string, string[]>;
    body: string;
  }

  let {
    diagnosticSub = null,
    diagnosticTab = 'report',
    diagnosticLoading = false,
    parseReportData = null,
    rawResponseData = null,
    onClose,
    onTabChange
  }: {
    diagnosticSub: Subscription | null;
    diagnosticTab: 'report' | 'headers' | 'raw';
    diagnosticLoading: boolean;
    parseReportData: ParseReport | null;
    rawResponseData: RawResponse | null;
    onClose: () => void;
    onTabChange: (tab: 'report' | 'headers' | 'raw') => void;
  } = $props();

  function formatDate(dateStr: string): string {
    if (!dateStr || dateStr.startsWith('0001')) return '—';
    try {
      const d = new Date(dateStr);
      return d.toLocaleString();
    } catch {
      return dateStr;
    }
  }

  function handleKeydown(e: KeyboardEvent) {
    if (e.key === 'Escape') {
      onClose();
    }
  }
</script>

<div
  class="modal-overlay"
  role="button"
  tabindex="0"
  onclick={onClose}
  onkeydown={handleKeydown}
>
  <div class="modal-card modal-large" role="presentation" onclick={(e) => e.stopPropagation()}>
    <div class="modal-card-header">
      <h2>{diagnosticSub ? $t('subscr.diag_title').replace('{name}', diagnosticSub.name) : ''}</h2>
      <button class="modal-close-btn" onclick={onClose}>&times;</button>
    </div>

    <div class="diag-tabs">
      <button
        class="diag-tab-btn"
        class:active={diagnosticTab === 'report'}
        onclick={() => onTabChange('report')}
      >
        {$t('subscr.tab_report')}
      </button>
      <button
        class="diag-tab-btn"
        class:active={diagnosticTab === 'headers'}
        onclick={() => onTabChange('headers')}
      >
        {$t('subscr.tab_headers')}
      </button>
      <button
        class="diag-tab-btn"
        class:active={diagnosticTab === 'raw'}
        onclick={() => onTabChange('raw')}
      >
        {$t('subscr.tab_raw')}
      </button>
    </div>

    <div class="modal-card-body diag-body">
      {#if diagnosticLoading}
        <div class="text-center" style="padding: 2rem 0; color: var(--fg-dim);">
          <span class="spinner" style="margin-right: 8px;">...</span>
          {$t('subscr.loading_diag')}
        </div>
      {:else if diagnosticTab === 'report'}
        <div class="tab-content">
          <div class="diag-summary-cards">
            <div class="diag-sum-card success">
              <div class="title">{$t('subscr.diag_parsed')}</div>
              <div class="val">{parseReportData?.parsed_count ?? 0}</div>
            </div>
            <div class="diag-sum-card warning">
              <div class="title">{$t('subscr.diag_skipped')}</div>
              <div class="val">{parseReportData?.skipped_count ?? 0}</div>
            </div>
            <div class="diag-sum-card">
              <div class="title">{$t('subscr.diag_time')}</div>
              <div class="val">{formatDate(parseReportData?.timestamp || '')}</div>
            </div>
          </div>

          <div class="diag-table-wrapper">
            {#if parseReportData && parseReportData.skipped && parseReportData.skipped.length > 0}
              <table class="diag-table">
                <thead>
                  <tr>
                    <th style="width: 80px;">{$t('subscr.table_line')}</th>
                    <th>{$t('subscr.table_reason')}</th>
                    <th>{$t('subscr.table_snippet')}</th>
                  </tr>
                </thead>
                <tbody>
                  {#each parseReportData.skipped as item}
                    <tr>
                      <td class="line-num">{item.line}</td>
                      <td class="reason">{item.reason}</td>
                      <td class="snippet"><code>{item.snippet}</code></td>
                    </tr>
                  {/each}
                </tbody>
              </table>
            {:else}
              <div class="text-center" style="padding: 1.5rem; color: var(--fg-secondary);">
                {$t('subscr.no_skips')}
              </div>
            {/if}
          </div>
        </div>
      {:else if diagnosticTab === 'headers'}
        <div class="tab-content">
          {#if rawResponseData && rawResponseData.headers}
            <div class="diag-headers-list">
              {#each Object.entries(rawResponseData.headers) as [key, val]}
                <div class="hdr-item">
                  <span class="hdr-key">{key}</span>
                  <span class="hdr-val">{val.join(', ')}</span>
                </div>
              {/each}
            </div>
          {:else}
            <div class="text-center" style="padding: 1.5rem; color: var(--fg-secondary);">—</div>
          {/if}
        </div>
      {:else}
        <div class="tab-content height-100">
          {#if rawResponseData && rawResponseData.body}
            <pre class="raw-body-pre"><code>{rawResponseData.body}</code></pre>
          {:else}
            <div class="text-center" style="padding: 1.5rem; color: var(--fg-secondary);">—</div>
          {/if}
        </div>
      {/if}
    </div>

    <div class="modal-card-footer">
      <button class="btn btn-secondary" onclick={onClose}>{$t('app.close')}</button>
    </div>
  </div>
</div>

<style>
  .modal-overlay {
    position: fixed;
    top: 0;
    left: 0;
    right: 0;
    bottom: 0;
    background: rgba(0, 0, 0, 0.6);
    backdrop-filter: blur(4px);
    display: flex;
    align-items: center;
    justify-content: center;
    z-index: 1000;
    padding: 20px;
  }

  .modal-card {
    background: var(--bg-card);
    border: 1px solid var(--border);
    border-radius: var(--radius-lg);
    width: 100%;
    max-width: 520px;
    box-shadow: 0 20px 40px rgba(0, 0, 0, 0.5);
    overflow: hidden;
    display: flex;
    flex-direction: column;
    max-height: 90vh;
    animation: modal-anim 0.2s cubic-bezier(0.16, 1, 0.3, 1);
  }

  @keyframes modal-anim {
    from {
      transform: scale(0.95) translateY(10px);
      opacity: 0;
    }
    to {
      transform: scale(1) translateY(0);
      opacity: 1;
    }
  }

  .modal-card-header {
    padding: 16px 24px;
    border-bottom: 1px solid var(--border);
    display: flex;
    justify-content: space-between;
    align-items: center;
  }

  .modal-card-header h2 {
    margin: 0;
    font-size: 16px;
    font-weight: 700;
    color: var(--fg-primary);
  }

  .modal-close-btn {
    background: none;
    border: none;
    color: var(--fg-dim);
    font-size: 24px;
    cursor: pointer;
    line-height: 1;
    padding: 4px;
  }

  .modal-close-btn:hover {
    color: var(--fg-primary);
  }

  .modal-card-body {
    padding: 24px;
    overflow-y: auto;
    display: flex;
    flex-direction: column;
    gap: 16px;
    scrollbar-width: thin;
    scrollbar-color: var(--border-strong) var(--bg-card);
  }
  .modal-card-body::-webkit-scrollbar {
    width: 6px;
  }
  .modal-card-body::-webkit-scrollbar-track {
    background: var(--bg-card);
  }
  .modal-card-body::-webkit-scrollbar-thumb {
    background: var(--border-strong);
    border-radius: 4px;
  }
  .modal-card-body::-webkit-scrollbar-thumb:hover {
    background: var(--accent);
  }

  .modal-card-footer {
    padding: 16px 24px;
    border-top: 1px solid var(--border);
    display: flex;
    justify-content: flex-end;
    gap: 12px;
  }

  .modal-large {
    max-width: 800px;
    width: 100%;
  }

  .diag-tabs {
    display: flex;
    border-bottom: 1px solid var(--border);
    background: rgba(0, 0, 0, 0.08);
  }

  .diag-tab-btn {
    flex: 1;
    background: none;
    border: none;
    border-bottom: 2px solid transparent;
    padding: 12px;
    color: var(--fg-dim);
    font-size: 13px;
    font-weight: 600;
    cursor: pointer;
    transition: all var(--transition-fast);
  }

  .diag-tab-btn:hover {
    color: var(--fg-primary);
    background: rgba(255, 255, 255, 0.02);
  }

  .diag-tab-btn.active {
    color: var(--accent);
    border-bottom-color: var(--accent);
    background: rgba(255, 255, 255, 0.04);
  }

  .diag-body {
    padding: 20px;
    max-height: 60vh;
    overflow-y: auto;
  }

  .diag-summary-cards {
    display: grid;
    grid-template-columns: repeat(3, 1fr);
    gap: 12px;
    margin-bottom: 20px;
  }

  .diag-sum-card {
    background: var(--accent-soft);
    border: 1px solid var(--accent-line);
    border-radius: var(--radius-md);
    padding: 12px 16px;
  }

  .diag-sum-card.success {
    background: rgba(16, 185, 129, 0.06);
    border-color: rgba(16, 185, 129, 0.2);
    color: #10b981;
  }

  .diag-sum-card.warning {
    background: rgba(245, 158, 11, 0.06);
    border-color: rgba(245, 158, 11, 0.2);
    color: #f59e0b;
  }

  .diag-sum-card .title {
    font-size: 11px;
    text-transform: uppercase;
    letter-spacing: 0.05em;
    color: var(--fg-dim);
  }

  .diag-sum-card .val {
    font-size: 20px;
    font-weight: 700;
    margin-top: 4px;
    font-family: var(--font-family-mono);
  }

  .diag-table-wrapper {
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    overflow: hidden;
  }

  .diag-table {
    width: 100%;
    border-collapse: collapse;
    font-size: 12px;
  }

  .diag-table th,
  .diag-table td {
    padding: 10px 12px;
    text-align: left;
    border-bottom: 1px solid var(--border);
  }

  .diag-table th {
    background: rgba(0, 0, 0, 0.15);
    color: var(--fg-primary);
    font-weight: 600;
  }

  .diag-table tr:last-child td {
    border-bottom: none;
  }

  .diag-table .line-num {
    font-family: var(--font-family-mono);
    color: var(--fg-dim);
  }

  .diag-table .reason {
    color: var(--danger);
  }

  .diag-table .snippet code {
    font-family: var(--font-family-mono);
    background: rgba(0, 0, 0, 0.2);
    padding: 2px 4px;
    border-radius: 3px;
    word-break: break-all;
  }

  .diag-headers-list {
    display: flex;
    flex-direction: column;
    gap: 8px;
  }

  .hdr-item {
    display: flex;
    flex-direction: column;
    padding: 8px 12px;
    background: rgba(0, 0, 0, 0.1);
    border-radius: var(--radius-sm);
    border: 1px solid var(--border);
  }

  .hdr-key {
    font-weight: 700;
    font-size: 12px;
    color: var(--accent);
    font-family: var(--font-family-mono);
  }

  .hdr-val {
    font-size: 12px;
    font-family: var(--font-family-mono);
    margin-top: 4px;
    word-break: break-all;
    color: var(--fg-primary);
  }

  .raw-body-pre {
    margin: 0;
    padding: 16px;
    background: rgba(0, 0, 0, 0.2);
    border-radius: var(--radius-md);
    border: 1px solid var(--border);
    overflow: auto;
    max-height: 45vh;
    font-family: var(--font-family-mono);
    font-size: 12px;
    line-height: 1.5;
    white-space: pre-wrap;
    word-break: break-all;
  }
</style>
