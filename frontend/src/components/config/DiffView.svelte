<script lang="ts">
  import { t } from '../../i18n';
  import { getDiffGroups } from '../editor/diff';

  let {
    id,
    expected,
    actual,
    missing = false,
    truncated = false
  }: {
    id?: string;
    /** Что запишет панель (добавленные строки «+»). */
    expected: string;
    /** Что лежит на диске сейчас (удалённые строки «−»). */
    actual: string;
    /** Файла на диске нет: все строки ожидаемого содержимого — добавленные. */
    missing?: boolean;
    truncated?: boolean;
  } = $props();

  // Строки выводятся текстовыми узлами Svelte: содержимое файла не исполняется (T-144-46)
  const missingLines = $derived.by(() => {
    const lines = expected.split('\n');
    if (lines.length > 0 && lines[lines.length - 1] === '') lines.pop();
    return lines;
  });
  const groups = $derived(missing ? [] : getDiffGroups(actual, expected));
  const caption = $derived(missing ? $t('cfg.diff.missing') : $t('cfg.diff.caption'));
</script>

<div class="diff-view" {id} data-testid="config-diff">
  <p class="diff-caption">{caption}</p>
  <!-- Блок прокручивается сам: фокус нужен, чтобы листать его клавиатурой (WCAG 2.1.1) -->
  <!-- svelte-ignore a11y_no_noninteractive_tabindex -->
  <div class="diff-body" role="region" aria-label={caption} tabindex="0">
    {#if missing}
      {#each missingLines as line, li (li)}
        <div class="diff-line diff-line-added">+ {line}</div>
      {/each}
    {:else}
      {#each groups as group, gi (gi)}
        {#if group.type === 'added'}
          {#each group.lines as line, li (li)}
            <div class="diff-line diff-line-added">+ {line}</div>
          {/each}
        {:else if group.type === 'removed'}
          {#each group.lines as line, li (li)}
            <div class="diff-line diff-line-removed">- {line}</div>
          {/each}
        {:else if group.type === 'collapsed'}
          <div class="diff-line diff-line-collapsed">{group.lines[0]}</div>
        {:else}
          {#each group.lines as line, li (li)}
            <div class="diff-line diff-line-unchanged">{line}</div>
          {/each}
        {/if}
      {/each}
    {/if}
  </div>
  {#if truncated}
    <p class="diff-note">{$t('cfg.diff.truncated')}</p>
  {/if}
</div>

<style>
  .diff-view {
    display: flex;
    flex-direction: column;
    gap: 8px;
    min-width: 0;
  }

  .diff-caption,
  .diff-note {
    margin: 0;
    font-size: var(--font-size-xs);
    line-height: 1.4;
    color: var(--fg-dim);
  }

  .diff-body {
    max-height: 320px;
    overflow: auto;
    padding: 10px 14px;
    background: var(--bg-page);
    border: 1px solid var(--border-light);
    border-radius: var(--radius-md);
    font-family: var(--font-family-mono);
    font-size: 12px;
    line-height: 1.5;
    scrollbar-width: thin;
  }

  .diff-body::-webkit-scrollbar {
    width: 4px;
    height: 4px;
  }
  .diff-body::-webkit-scrollbar-track {
    background: transparent;
  }
  .diff-body::-webkit-scrollbar-thumb {
    background: var(--border);
    border-radius: var(--radius-sm);
  }

  .diff-line {
    white-space: pre-wrap;
    word-break: break-all;
  }

  .diff-line-added {
    background: color-mix(in srgb, var(--success) 12%, transparent);
    /* Чистый --success на подложке 12% в светлой теме даёт 3.96:1; смесь с основным текстом проходит AA */
    color: color-mix(in srgb, var(--success) 70%, var(--fg-primary));
    border-left: 3px solid var(--success);
    padding-left: 6px;
  }

  .diff-line-removed {
    background: color-mix(in srgb, var(--danger) 12%, transparent);
    color: color-mix(in srgb, var(--danger) 70%, var(--fg-primary));
    border-left: 3px solid var(--danger);
    padding-left: 6px;
  }

  .diff-line-collapsed {
    background: color-mix(in srgb, var(--fg-primary) 3%, transparent);
    /* --fg-faint не проходит контраст для текста; свёрнутая строка — текст */
    color: var(--fg-dim);
    text-align: center;
    font-style: italic;
    padding: 4px 0;
    border-top: 1px dashed var(--border);
    border-bottom: 1px dashed var(--border);
    margin: 4px 0;
  }

  .diff-line-unchanged {
    color: var(--fg-dim);
    padding-left: 9px;
  }
</style>
