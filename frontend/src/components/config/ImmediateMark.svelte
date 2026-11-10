<script lang="ts">
  import { t } from '../../i18n';
  import Badge from '../Badge.svelte';
  import Icon from '../../lib/components/Icon.svelte';

  // Пометка немедленного действия раздела «Конфигурация» (D-14): действие меняет
  // диск или ядро сразу, мимо черновика, и «Сбросить» его не отменяет. Ставится у
  // заголовка действия или карточки, рядом с кнопкой. Корень .immediate-row — элемент
  // flex-строки родителя: на телефоне он занимает всю ширину и уходит под заголовок.
  // Пояснение раскрывается нажатием (details/summary): title виден только мышью, а на
  // сенсорном экране и с клавиатуры подсказку иначе не получить.
  let { title }: { title?: string } = $props();

  let open = $state(false);
  const explanation = $derived(title ?? $t('cfg.immediate.title'));

  // Escape закрывает подсказку и не закрывает окно, в котором стоит пометка
  function onKeydown(e: KeyboardEvent) {
    if (e.key === 'Escape' && open) {
      e.preventDefault();
      e.stopPropagation();
      open = false;
    }
  }
</script>

<span class="immediate-row">
  <details class="immediate-details" bind:open>
    <summary class="immediate-summary" onkeydown={onKeydown}>
      <Badge variant="soft" tone="neutral" testid="immediate-mark" title={explanation}>
        <span class="immediate-icon" aria-hidden="true"><Icon name="info" size={12} /></span>
        {$t('cfg.immediate.label')}
      </Badge>
    </summary>
    <span class="immediate-tip" data-testid="immediate-mark-tip">{explanation}</span>
  </details>
</span>

<style>
  .immediate-row {
    display: inline-flex;
    flex-wrap: wrap;
    align-items: center;
    min-width: 0;
    max-width: 100%;
  }

  .immediate-details {
    min-width: 0;
    max-width: 100%;
  }

  .immediate-summary {
    display: inline-flex;
    max-width: 100%;
    list-style: none;
    cursor: help;
    border-radius: var(--radius-sm);
  }

  .immediate-summary::-webkit-details-marker {
    display: none;
  }

  /* summary не входит в общий список :focus-visible из global.css */
  .immediate-summary:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: 2px;
  }

  .immediate-icon {
    display: inline-flex;
    flex-shrink: 0;
    color: var(--fg-dim);
  }

  .immediate-tip {
    display: block;
    max-width: 40ch;
    margin-top: var(--spacing-1);
    font-size: var(--font-size-xs);
    line-height: 1.5;
    color: var(--fg-secondary);
    white-space: normal;
    overflow-wrap: anywhere;
  }

  @media (max-width: 480px) {
    .immediate-row {
      flex-basis: 100%;
    }

    .immediate-row :global(.badge) {
      max-width: 100%;
      white-space: normal;
    }

    .immediate-tip {
      max-width: 100%;
    }
  }
</style>
