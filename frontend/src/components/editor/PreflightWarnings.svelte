<script lang="ts">
  import { t } from '../../i18n';

  export interface PreflightWarning {
    code?: string;
    message?: string;
    params?: Record<string, string | number>;
  }

  let {
    warnings = [],
    onDismiss
  }: {
    warnings?: PreflightWarning[];
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
      <strong>{$t('editor.save_warnings_title')}</strong>
      <ul class="preflight-warnings-list">
        {#each activeWarnings as warning, i (i)}
          <li>{getWarningText(warning)}</li>
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
</style>
