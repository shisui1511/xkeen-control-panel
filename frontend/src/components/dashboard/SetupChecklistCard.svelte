<script lang="ts">
  import { t } from '../../i18n';
  import Card from '../Card.svelte';
  import Badge from '../Badge.svelte';
  import Button from '../Button.svelte';
  import Icon from '../../lib/components/Icon.svelte';
  import { showConfirm, showToast } from '../../stores';
  import {
    hideWizard,
    wizardView,
    WIZARD_STEP_IDS,
    type WizardStepId
  } from '../../lib/configWizard';

  // Карточка «Настроить VPN» (фаза 149.1, CFG-04): те же шесть шагов и тот же прогресс, что в
  // мастере, — из общего ответа GET /api/configlayer/wizard (149 D-27, D-01). Пропсов нет:
  // когда показывать (первый ответ сервера, card_visible), решает дашборд. Шаг 7
  // (проверка IP) в карточку не входит (D-05).

  interface Row {
    n: number;
    id: WizardStepId;
    done: boolean;
    current: boolean;
    partial: { done: number; total: number } | null;
  }

  const rows = $derived.by((): Row[] => {
    const view = $wizardView;
    if (!view) return [];
    return WIZARD_STEP_IDS.map((id, i) => {
      const step = view.steps.find((s) => s.id === id);
      const p = step?.partial;
      return {
        n: i + 1,
        id,
        done: step?.done === true,
        current: view.current === i + 1,
        // «Частично, N из M» — только у шага секций и только пока взято не всё
        partial: id === 'sections' && p && p.done < p.total ? p : null
      };
    });
  });

  const doneCount = $derived($wizardView?.done_count ?? 0);
  const total = $derived($wizardView?.total ?? WIZARD_STEP_IDS.length);

  // Мастер сам раскроет первый невыполненный шаг того же ответа
  function openWizard() {
    window.location.hash = '#/config/setup';
  }

  // «Скрыть» навсегда (D-11): только после подтверждения. Признак ставит сервер, карточка
  // исчезает по его ответу и по событию wizard во всех открытых вкладках и браузерах
  let hiding = $state(false);

  async function hideCard() {
    const ok = await showConfirm({
      variant: 'warning',
      title: $t('dash.setup.hide_title'),
      message: $t('dash.setup.hide_message'),
      confirmLabel: $t('dash.setup.hide_confirm'),
      cancelLabel: $t('app.cancel')
    });
    if (!ok) return;
    hiding = true;
    try {
      await hideWizard();
    } catch (err) {
      const reason = err instanceof Error ? err.message : String(err);
      showToast('error', $t('dash.setup.hide_failed', { reason }));
    } finally {
      hiding = false;
    }
  }
</script>

{#if $wizardView}
  <div class="setup-card" data-testid="setup-checklist-card">
    <Card title={$t('dash.setup.title')}>
      {#snippet actions()}
        <span class="sc-progress" data-testid="setup-card-progress">
          {$t('dash.setup.progress', { done: String(doneCount), total: String(total) })}
        </span>
      {/snippet}
      <ol class="sc-list">
        {#each rows as row (row.id)}
          <li
            class="sc-step"
            class:sc-step--done={row.done}
            class:sc-step--current={row.current}
            data-testid="setup-card-step-{row.n}"
            data-done={row.done ? 'true' : 'false'}
            data-current={row.current ? 'true' : 'false'}
          >
            <span
              class="sc-icon"
              role="img"
              aria-label={row.done
                ? $t('dash.quickstart.step_done')
                : $t('dash.quickstart.step_pending')}
            >
              {#if row.done}
                <!-- Частичный шаг — тем же цветом, что маркер мастера -->
                <Icon
                  name="check"
                  size={16}
                  color={row.partial ? 'var(--warning)' : 'var(--success)'}
                />
              {:else}
                <span class="sc-pending" aria-hidden="true"></span>
              {/if}
            </span>
            <span class="sc-text">{$t(`dash.setup.step.${row.id}`)}</span>
            {#if row.partial}
              <Badge tone="warning" testid="setup-card-partial">
                {$t('cfg.setup.status.partial', {
                  done: String(row.partial.done),
                  total: String(row.partial.total)
                })}
              </Badge>
            {/if}
          </li>
        {/each}
      </ol>
      <div class="sc-footer">
        <Button variant="primary" size="sm" testid="setup-card-continue" onclick={openWizard}>
          {doneCount > 0 ? $t('dash.setup.continue') : $t('dash.setup.begin')}
        </Button>
        <Button
          variant="ghost"
          size="sm"
          testid="setup-card-hide"
          loading={hiding}
          onclick={hideCard}
        >
          {$t('dash.setup.hide')}
        </Button>
      </div>
    </Card>
  </div>
{/if}

<style>
  .setup-card {
    margin-bottom: var(--spacing-4);
  }

  /* Цель касания 44 px: обе кнопки карточки в любой плотности (токен --btn-h) */
  @media (max-width: 768px) {
    .setup-card {
      --btn-h: 44px;
    }
  }

  .sc-progress {
    font-size: var(--font-size-xs);
    font-weight: 400;
    color: var(--fg-dim);
    font-family: var(--font-family-mono);
    white-space: nowrap;
  }

  .sc-list {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: var(--spacing-2);
  }

  .sc-step {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: var(--spacing-2);
    padding: var(--spacing-1) 0;
    min-width: 0;
  }

  .sc-icon {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 16px;
    height: 16px;
    flex-shrink: 0;
  }

  /* Невыполненный шаг — пустое кольцо того же размера, что в мастере; текущий — кольцо
     акцента */
  .sc-pending {
    width: 16px;
    height: 16px;
    border: 1.5px solid var(--fg-dim);
    border-radius: var(--radius-full);
  }

  .sc-step--current .sc-pending {
    border-color: var(--accent-line);
    background: var(--accent-soft);
  }

  .sc-text {
    flex: 1;
    min-width: 0;
    font-size: var(--font-size-base);
    color: var(--fg-secondary);
    overflow-wrap: anywhere;
  }

  .sc-step--current .sc-text {
    color: var(--fg-primary);
    font-weight: 600;
  }

  .sc-footer {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    justify-content: space-between;
    gap: var(--spacing-2);
    margin-top: var(--spacing-4);
  }

  /* Телефон: кнопки подвала друг под другом на всю ширину (растягивает колонка),
     счётчик остаётся в заголовке */
  @media (max-width: 480px) {
    .sc-footer {
      flex-direction: column;
      align-items: stretch;
    }
  }
</style>
