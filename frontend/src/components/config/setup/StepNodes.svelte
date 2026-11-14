<script lang="ts">
  import { t } from '../../../i18n';
  import AddSourceField from '../nodes/AddSourceField.svelte';
  import ConfigNotices from '../ConfigNotices.svelte';
  import type { WizardStep } from '../../../lib/configWizard';

  // Тело шага 3 «Узлы» (D-02, D-04.3): поле «Добавить» встроено прямо в шаг, под ним —
  // уведомления источников (не скачался, отклонён, пуст): ошибка видна в самом шаге, и
  // шаг остаётся невыполненным, пока в черновике нет рабочего узла. Счёт узлов и
  // источников даёт сервер (GET /api/configlayer/wizard).

  interface Props {
    step: WizardStep | undefined;
  }

  let { step }: Props = $props();

  const nodes = $derived(step?.nodes ?? 0);
  const sources = $derived(step?.sources ?? 0);
</script>

<div class="step-nodes">
  <AddSourceField />

  <div class="step-nodes-errors" data-testid="setup-nodes-errors">
    <ConfigNotices prefixes={['source_']} />
  </div>

  {#if nodes > 0}
    <p class="step-nodes-count" data-testid="setup-nodes-count">
      {$t('cfg.setup.nodes.count', { n: String(nodes) })}
    </p>
  {:else if sources > 0}
    <p class="step-nodes-note" data-testid="setup-nodes-no-usable">
      {$t('cfg.setup.nodes.no_usable')}
    </p>
  {/if}
</div>

<style>
  .step-nodes {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-4);
    min-width: 0;
  }

  .step-nodes-errors {
    min-width: 0;
    overflow-wrap: anywhere;
  }

  .step-nodes-errors:empty {
    display: none;
  }

  .step-nodes-count,
  .step-nodes-note {
    margin: 0;
    line-height: 1.5;
    overflow-wrap: anywhere;
  }

  .step-nodes-count {
    color: var(--fg-primary);
  }

  .step-nodes-note {
    color: var(--fg-secondary);
  }
</style>
