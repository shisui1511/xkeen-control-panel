<script lang="ts">
  import { t } from '../../i18n';
  import Card from '../Card.svelte';
  import Icon from '../../lib/components/Icon.svelte';
  import { mihomoApiAvailable, mihomoApiState, mihomoOfflineReason } from '../../stores';

  // Чек-лист первого запуска Mihomo: ядро выбрано → подписка → конфиг применён → ядро
  // запущено. Шаги — настоящая последовательность, поэтому список упорядоченный. Когда
  // карточку показывать (Mihomo активен, данные первого опроса пришли, не всё сделано),
  // решает дашборд.

  interface Props {
    hasSubscription: boolean;
    mihomoRunning: boolean;
    onSwitchTab: (tab: string) => void;
  }

  let { hasSubscription, mihomoRunning, onSwitchTab }: Props = $props();

  interface Step {
    key: string;
    done: boolean;
    label: string;
    reason?: string;
    cta?: { label: string; href: string; run?: () => void };
  }

  const steps = $derived.by((): Step[] => [
    // Шаг 1 выполнен всегда: карточка видна только при активном Mihomo
    { key: 'kernel', done: true, label: $t('dash.quickstart.step1_label') },
    {
      key: 'subscription',
      done: hasSubscription,
      label: hasSubscription ? $t('dash.quickstart.step2_done') : $t('dash.quickstart.step2_label'),
      cta: { label: $t('dash.quickstart.step2_cta'), href: '#/proxies?tab=providers' }
    },
    {
      key: 'config',
      done: $mihomoApiAvailable,
      label: $mihomoApiAvailable
        ? $t('dash.quickstart.step3_done')
        : $t('dash.quickstart.step3_label'),
      reason:
        $mihomoApiState === 'down' && $mihomoOfflineReason
          ? $t(`dash.mihomo_offline_${$mihomoOfflineReason}`)
          : undefined,
      cta: { label: $t('dash.quickstart.step3_cta'), href: '#/constructor' }
    },
    {
      key: 'running',
      done: mihomoRunning,
      label: mihomoRunning ? $t('dash.quickstart.step4_done') : $t('dash.quickstart.step4_label'),
      cta: {
        label: $t('dash.quickstart.step4_cta'),
        href: '#/services',
        run: () => onSwitchTab('services')
      }
    }
  ]);

  const doneCount = $derived(steps.filter((s) => s.done).length);
</script>

<div class="quickstart">
  <Card title={$t('dash.quickstart.title')}>
    {#snippet actions()}
      <span class="qs-progress">
        {$t('dash.quickstart.progress', { done: String(doneCount), total: String(steps.length) })}
      </span>
    {/snippet}
    <ol class="quickstart-list">
      {#each steps as step (step.key)}
        <li class="qs-step" class:qs-step--done={step.done}>
          <span
            class="qs-icon"
            role="img"
            aria-label={step.done
              ? $t('dash.quickstart.step_done')
              : $t('dash.quickstart.step_pending')}
          >
            {#if step.done}
              <Icon name="check" size={16} color="var(--success)" />
            {:else}
              <span class="qs-pending" aria-hidden="true"></span>
            {/if}
          </span>
          <span class="qs-text">
            {step.label}
            {#if step.reason && !step.done}
              <span class="qs-reason" data-testid="qs-step3-reason">{step.reason}</span>
            {/if}
          </span>
          {#if step.cta && !step.done}
            <a class="btn btn-secondary qs-cta" href={step.cta.href} onclick={step.cta.run}>
              {step.cta.label}
            </a>
          {/if}
        </li>
      {/each}
    </ol>
  </Card>
</div>

<style>
  .quickstart {
    margin-bottom: 18px;
  }

  .qs-progress {
    font-size: var(--font-size-xs);
    font-weight: 400;
    color: var(--fg-dim);
    font-family: var(--font-family-mono);
  }

  .quickstart-list {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: var(--spacing-2, 8px);
  }

  .qs-step {
    display: flex;
    align-items: center;
    gap: var(--spacing-2, 8px);
    padding: var(--spacing-1, 4px) 0;
  }

  .qs-icon {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 16px;
    height: 16px;
    flex-shrink: 0;
  }

  /* Невыполненный шаг — пустое кольцо */
  .qs-pending {
    width: 13px;
    height: 13px;
    border: 1.5px solid var(--fg-dim);
    border-radius: 50%;
  }

  .qs-text {
    font-size: var(--font-size-sm);
    color: var(--fg-primary);
    flex: 1;
  }

  .qs-reason {
    display: block;
    font-size: var(--font-size-xs);
    color: var(--fg-secondary);
  }

  .qs-step--done .qs-text {
    color: var(--fg-secondary);
  }

  .qs-cta {
    font-size: var(--font-size-xs);
    padding: 4px 8px;
    margin-left: auto;
    flex-shrink: 0;
  }
</style>
