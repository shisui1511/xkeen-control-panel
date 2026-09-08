<script lang="ts">
  import { t } from '../../i18n';

  export interface PreflightWarning {
    code?: string;
    message?: string;
    params?: Record<string, string | number>;
  }

  let {
    warnings = [],
    title,
    onDismiss
  }: {
    warnings?: PreflightWarning[];
    title?: string;
    onDismiss?: () => void;
  } = $props();

  function getWarningText(w: PreflightWarning): string {
    if (w.message) return w.message;
    if (w.code) {
      const translated = $t(w.code, w.params);
      if (translated && translated !== w.code) {
        return translated;
      }
    }
    return w.code || '';
  }

  function formatWarningParts(text: string): { text: string; isCode: boolean }[] {
    const parts: { text: string; isCode: boolean }[] = [];
    const regex = /'([^']+)'/g;
    let lastIndex = 0;
    let match: RegExpExecArray | null;
    while ((match = regex.exec(text)) !== null) {
      if (match.index > lastIndex) {
        parts.push({ text: text.slice(lastIndex, match.index), isCode: false });
      }
      parts.push({ text: match[1], isCode: true });
      lastIndex = regex.lastIndex;
    }
    if (lastIndex < text.length) {
      parts.push({ text: text.slice(lastIndex), isCode: false });
    }
    return parts.length > 0 ? parts : [{ text, isCode: false }];
  }

  let activeWarnings = $derived.by(() => {
    const seen = new Set<string>();
    const result: PreflightWarning[] = [];
    for (const w of warnings || []) {
      const text = getWarningText(w).trim();
      if (!text) continue;
      const key = (w.code || '') + '::' + text;
      if (!seen.has(key)) {
        seen.add(key);
        result.push(w);
      }
    }
    return result;
  });
</script>

{#if activeWarnings.length > 0}
  <div class="alert alert-warning alert-dismissible preflight-warnings" role="alert">
    <div class="preflight-warnings-content">
      <strong>{title || $t('editor.save_warnings_title')}</strong>
      <ul class="preflight-warnings-list">
        {#each activeWarnings as warning, i (i)}
          <li>
            {#each formatWarningParts(getWarningText(warning)) as part}
              {#if part.isCode}
                <code class="warning-code-token">{part.text}</code>
              {:else}
                {part.text}
              {/if}
            {/each}
          </li>
        {/each}
      </ul>
    </div>
    {#if onDismiss}
      <button
        type="button"
        class="alert-close-btn"
        onclick={onDismiss}
        aria-label={$t('app.close')}
      >
        &times;
      </button>
    {/if}
  </div>
{/if}

<style>
  .preflight-warnings {
    margin-bottom: var(--spacing-sm, 8px);
  }

  .preflight-warnings-content {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-xs, 4px);
    width: 100%;
  }

  .preflight-warnings-list {
    margin: 0;
    padding-left: 20px;
    display: flex;
    flex-direction: column;
    gap: var(--spacing-xs, 4px);
  }

  .preflight-warnings-list li {
    line-height: 1.4;
  }

  .warning-code-token {
    font-family: var(--font-mono, monospace);
    font-size: 0.85em;
    padding: 1px 4px;
    border-radius: var(--radius-xs, 3px);
    background: color-mix(in srgb, var(--warning) 18%, transparent);
    color: var(--warning);
  }
</style>
