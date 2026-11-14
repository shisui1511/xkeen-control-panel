<script lang="ts">
  import { onMount } from 'svelte';
  import { t } from '../../../i18n';
  import PageHeader from '../../../PageHeader.svelte';
  import Button from '../../Button.svelte';
  import EmptyState from '../../EmptyState.svelte';
  import Skeleton from '../../Skeleton.svelte';
  import Icon from '../../../lib/components/Icon.svelte';
  import { showToast } from '../../../stores';
  import { layerStatus, layerConnection, refetchLayerState } from '../../../lib/configLayer';
  import {
    WIZARD_STEP_IDS,
    closeWizard,
    loadWizard,
    startWizard,
    stopWizard,
    wizardLoadError,
    wizardView
  } from '../../../lib/configWizard';

  // Страница мастера первого запуска (#/config/setup, D-01…D-03, D-10): шапка с «Закрыть»,
  // семь шагов списком и «Готово». Статусы шагов 1–6 считает сервер (GET
  // /api/configlayer/wizard), седьмой шаг (проверка IP) клиентский: он заперт, пока не
  // выполнены «Применение» и «Запуск ядра» (D-09). Вкладок раздела, полосы черновика и
  // общих уведомлений здесь нет (D-03).

  type StepStatus =
    'pending' | 'current' | 'done' | 'partial' | 'running' | 'error' | 'blocked' | 'locked';

  interface Row {
    n: number;
    id: string;
    status: StepStatus;
  }

  const uid = $props.id();

  onMount(() => {
    startWizard();
    return () => stopWizard();
  });

  const failed = $derived(
    $layerStatus === 'error' || $layerConnection === 'lost' || $wizardLoadError
  );

  const rows = $derived.by((): Row[] => {
    const v = $wizardView;
    if (!v) return [];
    const byId = new Map(v.steps.map((s) => [s.id, s]));
    const out: Row[] = WIZARD_STEP_IDS.map((id, i) => {
      const n = i + 1;
      const step = byId.get(id);
      let status: StepStatus = 'pending';
      if (step?.done) status = 'done';
      else if (v.current === n) status = 'current';
      return { n, id, status };
    });
    // Шаг 7 открывается, когда применение завершено и ядро запущено (D-09)
    const ipOpen = byId.get('apply')?.done === true && byId.get('start')?.done === true;
    out.push({
      n: 7,
      id: 'ip',
      status: !ipOpen ? 'locked' : v.current === 0 ? 'current' : 'pending'
    });
    return out;
  });

  // Шаги 1–6 выполнены: «Готово» — главная кнопка
  const allDone = $derived($wizardView !== null && $wizardView.current === 0);

  // Номер раскрытого по умолчанию шага: текущий (первый невыполненный), а когда шаги 1–6
  // выполнены — проверка IP
  const currentN = $derived(rows.find((r) => r.status === 'current')?.n ?? null);

  // Выбор пользователя: какой шаг раскрыт (null — все свёрнуты) и какой шаг был текущим
  // в момент выбора
  let manual = $state<{ n: number | null; at: number | null } | null>(null);
  const openN = $derived(manual !== null && manual.n !== manual.at ? manual.n : currentN);

  function toggle(n: number) {
    manual = { n: openN === n ? null : n, at: currentN };
  }

  function statusText(r: Row): string {
    if (r.status === 'done') return $t('cfg.setup.status.done');
    return $t('cfg.setup.status.pending');
  }

  function bodyText(r: Row): string {
    if (r.id === 'ip' && r.status === 'locked') return $t('cfg.setup.ip.locked');
    return $t(`cfg.setup.step.${r.id}.hint`);
  }

  let closing = $state(false);

  // «Закрыть» отменяет автооткрытие (D-10) и уводит в «Узлы» даже при сбое запроса;
  // черновик не трогается
  async function close() {
    closing = true;
    try {
      await closeWizard();
    } catch (err) {
      const reason = err instanceof Error ? err.message : String(err);
      showToast('warning', $t('cfg.setup.close_failed', { reason }));
    } finally {
      closing = false;
    }
    window.location.hash = '#/config/nodes';
  }

  function retry() {
    void refetchLayerState();
    void loadWizard();
  }
</script>

<div class="setup-page" data-testid="config-setup">
  <PageHeader title={$t('cfg.setup.title')} subtitle={$t('cfg.setup.subtitle')}>
    {#snippet actions()}
      <Button variant="ghost" loading={closing} data-testid="setup-close" onclick={close}>
        <Icon name="cross" size={16} />
        {$t('cfg.setup.close')}
      </Button>
    {/snippet}
  </PageHeader>

  {#if failed}
    <EmptyState
      title={$t('cfg.load_error_title')}
      description={$t('cfg.load_error_body')}
      ctaText={$t('cfg.load_error_retry')}
      plain
      oncta={retry}
    />
  {:else if $wizardView === null}
    <div class="setup-skeleton" aria-busy="true">
      {#each [1, 2, 3, 4, 5, 6, 7] as n (n)}
        <Skeleton type="rect" height="48px" />
      {/each}
    </div>
  {:else}
    <ol class="setup-steps" data-testid="setup-steps" aria-label={$t('cfg.setup.steps_aria')}>
      {#each rows as r (r.n)}
        <li
          class="setup-step"
          data-testid="setup-step-{r.n}"
          data-step-id={r.id}
          data-status={r.status}
        >
          <h3 class="setup-step-heading">
            <button
              type="button"
              class="setup-step-toggle"
              aria-expanded={openN === r.n}
              aria-controls="{uid}-body-{r.n}"
              data-testid="setup-step-{r.n}-toggle"
              onclick={() => toggle(r.n)}
            >
              <span class="setup-step-num">{r.n}</span>
              <span class="setup-step-head">
                <span class="setup-step-title" id="{uid}-title-{r.n}">
                  {$t(`cfg.setup.step.${r.id}.title`)}
                </span>
                <span class="setup-step-status">{statusText(r)}</span>
              </span>
            </button>
          </h3>
          <div
            id="{uid}-body-{r.n}"
            class="setup-step-body"
            role="region"
            aria-labelledby="{uid}-title-{r.n}"
            hidden={openN !== r.n}
            data-testid="setup-step-{r.n}-body"
          >
            <p class="setup-step-hint">{bodyText(r)}</p>
          </div>
        </li>
      {/each}
    </ol>

    <div class="setup-footer">
      <Button
        variant={allDone ? 'primary' : 'secondary'}
        data-testid="setup-finish"
        onclick={() => (window.location.hash = '#/dashboard')}
      >
        {$t('cfg.setup.finish')}
      </Button>
    </div>
  {/if}
</div>

<style>
  .setup-page {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-4);
    min-width: 0;
  }

  @media (min-width: 1024px) {
    .setup-page {
      max-width: 720px;
    }
  }

  .setup-skeleton,
  .setup-steps {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-2);
  }

  .setup-steps {
    list-style: none;
    margin: 0;
    padding: 0;
  }

  .setup-step {
    min-width: 0;
    background: var(--bg-card);
    border: 1px solid var(--border);
    border-radius: var(--radius-lg);
  }

  .setup-step-heading {
    margin: 0;
    font-size: var(--font-size-base);
  }

  .setup-step-toggle {
    display: flex;
    align-items: center;
    gap: var(--spacing-4);
    width: 100%;
    padding: var(--spacing-4);
    background: none;
    border: 0;
    color: inherit;
    font: inherit;
    text-align: left;
    cursor: pointer;
  }

  .setup-step-head {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-1);
    min-width: 0;
  }

  .setup-step-title {
    font-weight: 600;
    overflow-wrap: anywhere;
  }

  .setup-step-status {
    font-size: var(--font-size-xs);
    color: var(--fg-dim);
  }

  .setup-step-body {
    padding: 0 var(--spacing-4) var(--spacing-4);
  }

  .setup-step-body[hidden] {
    display: none;
  }

  .setup-step-hint {
    margin: 0;
    color: var(--fg-secondary);
    overflow-wrap: anywhere;
  }

  .setup-footer {
    display: flex;
    flex-wrap: wrap;
    gap: var(--spacing-2);
  }
</style>
