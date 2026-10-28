<script lang="ts">
  import { get } from 'svelte/store';
  import { t, tp, currentLang } from '../../../i18n';
  import { showConfirm, showToast } from '../../../stores';
  import Button from '../../Button.svelte';
  import Icon from '../../../lib/components/Icon.svelte';
  import { applyRunning, geodataHolder } from '../../../lib/configLayer';
  import {
    geodataView,
    runUpdate,
    geodataErrorText,
    type FileResultView,
    type LastRunView
  } from '../../../lib/configGeodata';
  import OutputLog from './OutputLog.svelte';

  // Карточка «Обновление баз»: запуск xkeen -ug по кнопке, статус, итог последнего запуска и
  // живой вывод. Итог по файлам здесь не дублируется: он живёт в строках источников.
  const uid = $props.id();

  const view = $derived($geodataView);
  const run = $derived(view?.run);
  const ugRunning = $derived(run?.running === true && run.job === 'ug');
  const removeRunning = $derived(run?.running === true && run.job === 'remove');

  // Причина, по которой «Обновить сейчас» неактивна: видимым текстом под кнопкой и в title
  const busyReason = $derived.by(() => {
    if ($applyRunning || $geodataHolder === 'geodata_remove' || removeRunning) {
      return $t('cfg.routing.apply_busy_hint');
    }
    if ($geodataHolder !== '' || run?.running === true || run?.waiting_apply === true) {
      return $t('cfg.geo.ug_busy');
    }
    return '';
  });

  let starting = $state(false);
  // Блок вывода раскрыт кнопкой или сам, когда идёт запуск
  let showOutput = $state(false);
  const outputOpen = $derived(showOutput);

  // Идёт запуск: блок вывода раскрывается сам, после этого его можно свернуть
  $effect(() => {
    if (ugRunning) showOutput = true;
  });

  /** Время RFC3339 как «число, время» по часам браузера. */
  function localWhen(iso: string | undefined): string {
    if (iso === undefined || iso === '') return '';
    const d = new Date(iso);
    if (Number.isNaN(d.getTime())) return iso;
    return d.toLocaleString($currentLang, { dateStyle: 'short', timeStyle: 'short' });
  }

  /** Часы и минуты из RFC3339 как есть: показывается время роутера, а не часового пояса браузера. */
  function routerClock(iso: string | undefined): string {
    const m = /T(\d{2}:\d{2})/.exec(iso ?? '');
    return m !== null ? m[1] : (iso ?? '');
  }

  /** Текст причины: известный код словаря либо сам код. */
  function reasonText(code: string): string {
    const key = `cfg.geo.reason_${code}`;
    const text = $t(key);
    return text === key ? code : text;
  }

  function lastRunResult(r: LastRunView): string {
    if (r.outcome === 'ok') return $t('cfg.geo.outcome_ok');
    if (r.outcome === 'partial') return $tp('cfg.geo.outcome_partial', r.not_updated);
    if (r.outcome === 'failed') {
      const base = $t('cfg.geo.outcome_failed');
      return r.reason !== undefined && r.reason !== '' ? `${base}: ${reasonText(r.reason)}` : base;
    }
    return $t('cfg.geo.parse_unknown');
  }

  function resultText(r: FileResultView): string {
    if (r.status === 'updated') return $t('cfg.geo.res_updated');
    if (r.status === 'unchanged') return $t('cfg.geo.res_same');
    if (r.status === 'kept') {
      return $t('cfg.geo.res_kept', { reason: reasonText(r.reason ?? '') });
    }
    return $t('cfg.geo.outcome_failed');
  }

  async function runNow() {
    if (starting || busyReason !== '') return;
    const ok = await showConfirm({
      variant: 'warning',
      title: get(t)('cfg.geo.run_confirm_title'),
      consequence: get(t)('cfg.geo.run_confirm_body'),
      confirmLabel: get(t)('cfg.geo.run_confirm_ok')
    });
    if (!ok) return;
    starting = true;
    try {
      await runUpdate();
      showOutput = true;
    } catch (e) {
      showToast('error', get(t)('cfg.geo.run_failed', { reason: geodataErrorText(e) }));
    } finally {
      starting = false;
    }
  }

  const lastRun = $derived(view?.last_run);
</script>

{#if view}
  <section class="card geo-card" aria-labelledby="{uid}-title" data-testid="geo-update-card">
    <div class="geo-head">
      <h3 class="geo-title" id="{uid}-title">{$t('cfg.geo.upd_title')}</h3>
      <div class="geo-run">
        <Button
          variant="secondary"
          loading={starting || ugRunning}
          disabled={busyReason !== ''}
          title={busyReason !== '' ? busyReason : undefined}
          data-testid="geo-run-now"
          onclick={runNow}
        >
          <Icon name="refresh" size={14} />
          {$t('cfg.geo.run_now')}
        </Button>
        {#if busyReason !== ''}
          <span class="geo-hint" data-testid="geo-run-busy">{busyReason}</span>
        {/if}
      </div>
    </div>

    {#if ugRunning || removeRunning || run?.waiting_apply || view.schedule.retry_at}
      <div class="geo-status" role="status" data-testid="geo-run-status">
        {#if ugRunning}
          <span class="geo-status-line">
            <span class="spinner" aria-hidden="true"></span>
            {$t('cfg.geo.run_running')}
          </span>
        {:else if removeRunning}
          {#each view.draft as d (d.file)}
            <span class="geo-status-line">
              <span class="spinner" aria-hidden="true"></span>
              <span class="geo-mono-wrap">{$t('cfg.geo.step_remove', { file: d.file })}</span>
            </span>
          {/each}
        {/if}
        {#if run?.waiting_apply}
          <span class="geo-status-line">{$t('cfg.geo.waiting_apply')}</span>
        {/if}
        {#if view.schedule.retry_at}
          <span class="geo-status-line">
            {$t('cfg.geo.retry_planned', { time: routerClock(view.schedule.retry_at) })}
          </span>
        {/if}
      </div>
    {/if}

    <div class="geo-last">
      <p class="geo-last-text" role="status" data-testid="geo-last-run">
        {#if lastRun}
          {$t('cfg.geo.last_run', { when: localWhen(lastRun.at), result: lastRunResult(lastRun) })}
        {:else}
          {$t('cfg.geo.last_run_none')}
        {/if}
      </p>
      <button
        type="button"
        class="btn btn-secondary btn-sm"
        disabled={!lastRun && !ugRunning}
        aria-expanded={outputOpen}
        aria-controls="{uid}-output"
        data-testid="geo-output-toggle"
        onclick={() => (showOutput = !showOutput)}
      >
        <Icon name={outputOpen ? 'chevron-up' : 'chevron-down'} size={14} />
        {outputOpen ? $t('cfg.geo.output_hide') : $t('cfg.geo.output_show')}
      </button>
    </div>

    {#if outputOpen}
      <div id="{uid}-output" class="geo-output-wrap">
        <OutputLog job="ug" active={ugRunning || starting} />
      </div>
    {/if}

    {#if view.geoipset}
      <p class="geo-gips" data-testid="geo-gips">
        {$t('cfg.geo.gips_status', { status: resultText(view.geoipset) })}
      </p>
    {/if}
  </section>
{/if}

<style>
  .geo-card {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-4);
    min-width: 0;
  }

  .geo-head {
    display: flex;
    flex-wrap: wrap;
    align-items: flex-start;
    justify-content: space-between;
    gap: var(--spacing-2) var(--spacing-4);
    min-width: 0;
  }

  .geo-title {
    margin: 0;
    font-size: var(--font-size-lg);
    font-weight: 600;
    line-height: 1.2;
    color: var(--fg-primary);
  }

  .geo-run {
    display: flex;
    flex-direction: column;
    align-items: flex-end;
    gap: var(--spacing-1);
    min-width: 0;
  }

  .geo-hint {
    font-size: var(--font-size-xs);
    line-height: 1.5;
    color: var(--fg-dim);
    overflow-wrap: anywhere;
    text-align: right;
  }

  .geo-status {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-1);
    font-size: var(--font-size-base);
    line-height: 1.5;
    color: var(--fg-secondary);
  }

  .geo-status-line {
    display: inline-flex;
    align-items: center;
    gap: var(--spacing-2);
    min-width: 0;
  }

  .geo-mono-wrap {
    font-family: var(--font-family-mono);
    font-size: var(--font-size-sm);
    overflow-wrap: anywhere;
  }

  .geo-last {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    justify-content: space-between;
    gap: var(--spacing-2) var(--spacing-4);
    min-width: 0;
  }

  .geo-last-text {
    margin: 0;
    flex: 1 1 240px;
    min-width: 0;
    font-size: var(--font-size-base);
    line-height: 1.5;
    color: var(--fg-secondary);
    overflow-wrap: anywhere;
  }

  .geo-output-wrap {
    min-width: 0;
  }

  .geo-gips {
    margin: 0;
    font-size: var(--font-size-base);
    line-height: 1.5;
    color: var(--fg-secondary);
    overflow-wrap: anywhere;
  }

  @media (pointer: coarse), (max-width: 768px) {
    .geo-card :global(.btn) {
      min-height: 44px;
    }
  }
</style>
