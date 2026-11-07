<script lang="ts">
  import { MediaQuery } from 'svelte/reactivity';
  import { t } from '../../../i18n';
  import type { CoreKernel, PinnedValue } from '../../../lib/configCores';

  // Закреплённые значения секции (D-10): их панель дописывает сама, на них держатся
  // «Трафик», XKeen и API ядра. Строки «ключ → значение» — текст, не поля ввода. Набор и
  // порядок задаёт сервер. Больше трёх строк, а на ширине до 480 px всегда — внутри details.
  let {
    kernel,
    id,
    items
  }: {
    kernel: CoreKernel;
    id: string;
    items: PinnedValue[];
  } = $props();

  const FOLD_AFTER = 3;

  const narrow = new MediaQuery('max-width: 480px');

  const folded = $derived(items.length > FOLD_AFTER || narrow.current);

  function show(value: unknown): string {
    if (typeof value === 'string') return value;
    const json = JSON.stringify(value);
    return json === undefined ? String(value) : json;
  }
</script>

{#snippet rows()}
  <p class="pv-note">{$t('cfg.sec.pinned_note')}</p>
  <ul class="pv-list">
    {#each items as item (item.key)}
      <li class="pv-row">
        <span class="pv-key">{item.key}</span>
        <span class="pv-arrow" aria-hidden="true">→</span>
        <span class="pv-value">{show(item.value)}</span>
        <span class="badge badge--tag pv-badge">{$t('cfg.sec.pinned')}</span>
      </li>
    {/each}
  </ul>
{/snippet}

{#if items.length > 0}
  <div class="pv" data-testid="section-pinned-{kernel}-{id}">
    {#if folded}
      <details class="pv-details">
        <summary class="pv-summary">
          {$t('cfg.sec.pinned')} ({items.length})
        </summary>
        {@render rows()}
      </details>
    {:else}
      {@render rows()}
    {/if}
  </div>
{/if}

<style>
  .pv {
    min-width: 0;
    padding: var(--spacing-3);
    background: var(--bg-elevated);
    border: 1px solid var(--border-light);
    border-radius: var(--radius-md);
  }

  .pv-summary {
    display: flex;
    align-items: center;
    min-height: 32px;
    font-size: var(--font-size-sm);
    font-weight: 600;
    color: var(--fg-secondary);
    cursor: pointer;
  }

  .pv-note {
    margin: 0 0 var(--spacing-2);
    font-size: var(--font-size-xs);
    line-height: 1.5;
    color: var(--fg-secondary);
    overflow-wrap: anywhere;
  }

  .pv-details .pv-note {
    margin-top: var(--spacing-2);
  }

  .pv-list {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-2);
    margin: 0;
    padding: 0;
    list-style: none;
  }

  .pv-row {
    display: flex;
    flex-wrap: wrap;
    align-items: baseline;
    gap: var(--spacing-1) var(--spacing-2);
    min-width: 0;
    font-family: var(--font-family-mono);
    font-size: var(--font-size-sm);
    line-height: 1.5;
    color: var(--fg-primary);
  }

  .pv-key,
  .pv-value {
    min-width: 0;
    overflow-wrap: anywhere;
  }

  .pv-arrow {
    color: var(--fg-secondary);
  }

  /* --fg-secondary общего .badge на --surface-tint поверх --bg-elevated в тёмной теме даёт
     4.3:1 (ниже 4.5:1 для 12px); --fg-primary — не ниже 7:1 во всех темах */
  .pv-badge {
    flex-shrink: 0;
    color: var(--fg-primary);
  }

  @media (pointer: coarse), (max-width: 768px) {
    .pv-summary {
      min-height: 44px;
    }
  }
</style>
