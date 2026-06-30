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
