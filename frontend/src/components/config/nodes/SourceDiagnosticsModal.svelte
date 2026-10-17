<script lang="ts">
  import { untrack } from 'svelte';
  import { t } from '../../../i18n';
  import { showToast } from '../../../stores';
  import Modal from '../../Modal.svelte';
  import Tabs from '../../Tabs.svelte';
  import SegmentedControl from '../../SegmentedControl.svelte';
  import Icon from '../../../lib/components/Icon.svelte';
  import {
    fetchDiagnostics,
    type DiagView,
    type KernelFlags,
    type KernelName
  } from '../../../lib/configSources';
  import { sourceErrorText } from '../../../lib/sourceErrors';
  import { PAGE_FIRST, PAGE_STEP, formatDateTime } from '../../../lib/nodesView';

  // Диагностика ответа провайдера по каждому установленному ядру (D-07). Заголовки, тело и
  // отчёт приходят с сервера уже замаскированными (D-33, D-34): модаль показывает их как есть,
  // а «Скопировать» копирует ровно показанный текст.
  let { open, name, sourceId, installed, initialKernel, onclose } = $props<{
    open: boolean;
    name: string;
    sourceId: string;
    installed: KernelFlags;
    initialKernel: KernelName;
    onclose: () => void;
  }>();

  type Tab = 'report' | 'headers' | 'raw';

  const KERNELS: { id: KernelName; name: string }[] = [
    { id: 'xray', name: 'Xray' },
    { id: 'mihomo', name: 'Mihomo' }
  ];

  let kernel = $state<KernelName>('xray');
  let tab = $state<Tab>('report');
  let status = $state<'idle' | 'loading' | 'ready' | 'error'>('idle');
  let diag = $state<DiagView | null>(null);
  let limit = $state(PAGE_FIRST);
  // Номер последнего запроса: ответ устаревшего ядра или закрытой модали игнорируется
  let seq = 0;

  const kernels = $derived(KERNELS.filter((k) => installed[k.id]));
  const segmentItems = $derived(kernels.map((k) => ({ value: k.id, label: k.name })));
  const tabItems = $derived([
    { value: 'report', label: $t('cfg.nodes.tab_report'), testId: 'nodes-diag-tab-report' },
    { value: 'headers', label: $t('cfg.nodes.tab_headers'), testId: 'nodes-diag-tab-headers' },
    { value: 'raw', label: $t('cfg.nodes.tab_raw'), testId: 'nodes-diag-tab-raw' }
  ]);

  const skipped = $derived(diag?.report.skipped ?? []);
  const shownSkipped = $derived(skipped.slice(0, limit));
  const headersText = $derived(
    (diag?.headers ?? []).map((h) => `${h.name}: ${h.value}`).join('\n')
  );
  const bodyText = $derived(diag?.body ?? '');
  // Статуса нет, когда запрос не дошёл до провайдера (DNS, обрыв связи): ответа не было
  const responded = $derived((diag?.http_status ?? 0) > 0);
  // Провайдер ответил, но отказом (4xx/5xx): «все строки разобраны» тут неверно, разбирать нечего
  const httpFailed = $derived((diag?.http_status ?? 0) >= 400);
  // Дата в том же формате, что и в карточке источника
  const fetchedAt = $derived(formatDateTime(diag?.fetched_at) || '—');

  async function load() {
    const mine = ++seq;
    status = 'loading';
    try {
      const res = await fetchDiagnostics(sourceId, kernel);
      if (mine !== seq) return;
      diag = res;
      limit = PAGE_FIRST;
      status = 'ready';
    } catch {
      if (mine !== seq) return;
      diag = null;
      status = 'error';
    }
  }

  function initialize() {
    kernel = kernels.some((k) => k.id === initialKernel)
      ? initialKernel
      : (kernels[0]?.id ?? 'xray');
    tab = 'report';
    diag = null;
    void load();
  }

  function wipe() {
    seq++;
    status = 'idle';
    diag = null;
  }

  $effect(() => {
    if (open) untrack(initialize);
    else untrack(wipe);
  });

  function onKernel(next: string) {
    kernel = next as KernelName;
    void load();
  }

  async function copy(text: string) {
    try {
      await navigator.clipboard.writeText(text);
      showToast('success', $t('cfg.nodes.diag_copied'));
    } catch (err) {
      showToast('error', err instanceof Error ? err.message : String(err));
    }
  }
</script>

<Modal
  isOpen={open}
  title={$t('cfg.nodes.diag_title', { name })}
  maxWidth="760px"
  dataTestid="nodes-diag-modal"
  {onclose}
>
  <div class="dg-body">
    <div class="dg-controls">
      {#if kernels.length > 1}
        <div data-testid="nodes-diag-kernel">
          <SegmentedControl
            items={segmentItems}
            bind:value={kernel}
            ariaLabel={$t('cfg.nodes.diag_kernel_aria')}
            onchange={onKernel}
          />
        </div>
      {:else if kernels.length === 1}
        <span class="chip chip-default" data-testid="nodes-diag-kernel">{kernels[0].name}</span>
      {/if}
      <Tabs
        items={tabItems}
        bind:value={tab}
        variant="pill"
        ariaLabel={$t('cfg.nodes.diag_tabs_aria')}
      />
    </div>

    {#if status === 'loading' || status === 'idle'}
      <div class="dg-loading" data-testid="nodes-diag-loading">
        <span class="spinner" aria-hidden="true"></span>
        <span>{$t('cfg.nodes.diag_loading')}</span>
      </div>
    {:else if status === 'error'}
      <div class="alert alert-error dg-alert" role="alert" data-testid="nodes-diag-error">
        <Icon name="warning" size={16} class="dg-alert-icon" />
        <span>{$t('cfg.nodes.diag_error')}</span>
        <button
          type="button"
          class="btn btn-secondary btn-sm dg-retry"
          data-testid="nodes-diag-retry"
          onclick={() => void load()}
        >
          {$t('cfg.nodes.retry')}
        </button>
      </div>
    {:else if diag === null || diag.empty}
      <p class="dg-empty" data-testid="nodes-diag-empty">{$t('cfg.nodes.diag_empty')}</p>
    {:else}
      <p class="dg-meta" data-testid="nodes-diag-meta">
        {#if responded}
          {$t('cfg.nodes.diag_meta', { date: fetchedAt, status: diag.http_status ?? '' })}
        {:else}
          {$t('cfg.nodes.diag_meta_none', { date: fetchedAt })}
        {/if}
      </p>

      {#if tab === 'report'}
        <div class="dg-section" data-testid="nodes-diag-report">
          <p class="dg-summary">
            {$t('cfg.nodes.diag_summary', {
              parsed: diag.report.parsed,
              stubs: diag.report.stubs,
              skipped: skipped.length
            })}
          </p>
          {#if !responded}
            <p class="dg-hint" data-testid="nodes-diag-no-response">
              {$t('cfg.nodes.diag_no_response')}
            </p>
          {:else if httpFailed}
            <p class="dg-hint" data-testid="nodes-diag-http-error">
              {$t('cfg.nodes.diag_http_error', { status: diag.http_status ?? '' })}
            </p>
          {:else if skipped.length === 0}
            <p class="dg-ok" data-testid="nodes-diag-all-ok">{$t('cfg.nodes.diag_all_ok')}</p>
          {:else}
            <table class="dg-table">
              <thead>
                <tr>
                  <th class="dg-col-line" scope="col">{$t('cfg.nodes.diag_col_line')}</th>
                  <th class="dg-col-reason" scope="col">{$t('cfg.nodes.diag_col_reason')}</th>
                  <th scope="col">{$t('cfg.nodes.diag_col_fragment')}</th>
                </tr>
              </thead>
              <tbody>
                {#each shownSkipped as row, i (i)}
                  <tr data-testid="nodes-diag-skipped">
                    <td class="dg-col-line">{row.line}</td>
                    <td class="dg-col-reason">
                      {sourceErrorText({ code: row.code, params: row.params }, $t).title}
                    </td>
                    <td><code class="dg-fragment">{row.fragment}</code></td>
                  </tr>
                {/each}
              </tbody>
            </table>
            {#if skipped.length > limit}
              <div>
                <button
                  type="button"
                  class="btn btn-secondary btn-sm"
                  data-testid="nodes-diag-more"
                  onclick={() => (limit += PAGE_STEP)}
                >
                  {$t('cfg.nodes.show_more', { n: PAGE_STEP })}
                </button>
              </div>
            {/if}
          {/if}
        </div>
      {:else if tab === 'headers'}
        <div class="dg-section" data-testid="nodes-diag-headers">
          {#if headersText === ''}
            <p class="dg-empty">{$t('cfg.nodes.diag_headers_empty')}</p>
          {:else}
            <!-- svelte-ignore a11y_no_noninteractive_tabindex -->
            <pre
              class="dg-pre"
              role="region"
              tabindex="0"
              aria-label={$t('cfg.nodes.diag_headers_label')}>{headersText}</pre>
            <div>
              <button
                type="button"
                class="btn btn-secondary btn-sm"
                data-testid="nodes-diag-copy"
                onclick={() => void copy(headersText)}
              >
                <Icon name="copy" size={14} />
                {$t('cfg.nodes.copy')}
              </button>
            </div>
          {/if}
        </div>
      {:else}
        <div class="dg-section" data-testid="nodes-diag-raw">
          {#if bodyText === ''}
            <p class="dg-empty">{$t('cfg.nodes.diag_body_empty')}</p>
          {:else}
            <!-- svelte-ignore a11y_no_noninteractive_tabindex -->
            <pre
              class="dg-pre"
              role="region"
              tabindex="0"
              aria-label={$t('cfg.nodes.diag_body_label')}>{bodyText}</pre>
            {#if diag.truncated}
              <p class="dg-hint" data-testid="nodes-diag-truncated">
                {$t('cfg.nodes.diag_truncated')}
              </p>
            {/if}
            <div>
              <button
                type="button"
                class="btn btn-secondary btn-sm"
                data-testid="nodes-diag-copy"
                onclick={() => void copy(bodyText)}
              >
                <Icon name="copy" size={14} />
                {$t('cfg.nodes.copy')}
              </button>
            </div>
          {/if}
        </div>
      {/if}
    {/if}
  </div>
</Modal>

<style>
  .dg-body {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-4);
    min-width: 0;
  }

  .dg-controls {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--spacing-2) var(--spacing-3);
    min-width: 0;
  }

  .dg-loading {
    display: flex;
    align-items: center;
    gap: var(--spacing-2);
    color: var(--fg-secondary);
  }

  .dg-alert {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--spacing-2);
    margin: 0;
    min-width: 0;
    overflow-wrap: anywhere;
  }

  .dg-alert :global(.dg-alert-icon) {
    flex-shrink: 0;
  }

  .dg-retry {
    margin-left: auto;
  }

  .dg-empty,
  .dg-hint,
  .dg-meta {
    margin: 0;
    font-size: var(--font-size-base);
    color: var(--fg-dim);
    overflow-wrap: anywhere;
  }

  .dg-meta {
    font-size: var(--font-size-xs);
  }

  .dg-section {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-3);
    min-width: 0;
  }

  .dg-summary {
    margin: 0;
    font-size: var(--font-size-base);
    color: var(--fg-secondary);
    overflow-wrap: anywhere;
  }

  .dg-ok {
    margin: 0;
    font-size: var(--font-size-base);
    color: var(--fg-primary);
  }

  .dg-table {
    width: 100%;
    table-layout: fixed;
    border-collapse: collapse;
    font-size: var(--font-size-xs);
  }

  .dg-table th,
  .dg-table td {
    padding: var(--spacing-2);
    text-align: left;
    vertical-align: top;
    border-top: 1px solid var(--border);
    overflow-wrap: anywhere;
    word-break: break-word;
  }

  .dg-table th {
    font-weight: 600;
    color: var(--fg-secondary);
    border-top: 0;
  }

  .dg-table td {
    font-weight: 400;
    color: var(--fg-primary);
  }

  .dg-col-line {
    width: 56px;
  }

  .dg-col-reason {
    width: 36%;
  }

  .dg-fragment {
    font-family: var(--font-family-mono);
    font-size: var(--font-size-xs);
    color: var(--fg-secondary);
  }

  .dg-pre {
    margin: 0;
    padding: var(--spacing-3);
    max-height: 360px;
    overflow: auto;
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    background: var(--bg-elevated);
    color: var(--fg-primary);
    font-family: var(--font-family-mono);
    font-size: var(--font-size-xs);
    font-weight: 400;
    line-height: 1.5;
    white-space: pre-wrap;
    overflow-wrap: anywhere;
    word-break: break-word;
  }

  @media (max-width: 480px) {
    .dg-col-line {
      width: 40px;
    }
  }
</style>
