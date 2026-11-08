<script lang="ts">
  import { t } from '../../i18n';
  import Badge from '../Badge.svelte';
  import Icon from '../../lib/components/Icon.svelte';

  // Пометка немедленного действия раздела «Конфигурация» (D-14): действие меняет
  // диск или ядро сразу, мимо черновика, и «Сбросить» его не отменяет. Ставится у
  // заголовка действия или карточки, рядом с кнопкой. Корень .immediate-row — элемент
  // flex-строки родителя: на телефоне он занимает всю ширину и уходит под заголовок.
  let { title }: { title?: string } = $props();
</script>

<span class="immediate-row">
  <Badge
    variant="soft"
    tone="neutral"
    testid="immediate-mark"
    title={title ?? $t('cfg.immediate.title')}
  >
    <span class="immediate-icon" aria-hidden="true"><Icon name="info" size={12} /></span>
    {$t('cfg.immediate.label')}
  </Badge>
</span>

<style>
  .immediate-row {
    display: inline-flex;
    flex-wrap: wrap;
    align-items: center;
    min-width: 0;
    max-width: 100%;
  }

  .immediate-icon {
    display: inline-flex;
    flex-shrink: 0;
    color: var(--fg-dim);
  }

  @media (max-width: 480px) {
    .immediate-row {
      flex-basis: 100%;
    }

    .immediate-row :global(.badge) {
      max-width: 100%;
      white-space: normal;
    }
  }
</style>
