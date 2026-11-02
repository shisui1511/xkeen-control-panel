<script lang="ts">
  import { t } from '../i18n';
  import Icon from '../lib/components/Icon.svelte';

  // Навигация конструкторов (Xray, Mihomo) на телефоне. Вместо лестницы
  // «вкладки → вкладки разделов → содержимое → превью» — три экрана:
  // список разделов со сводкой, отдельный раздел и превью. «Превью» и
  // «Применить» закреплены над нижней панелью, их не нужно искать под
  // длинной формой. Какие блоки страницы видны на каком экране, решают
  // правила [data-mview] в global.css.
  interface SectionItem {
    id: string;
    label: string;
    summary?: string;
  }

  let {
    view,
    sections,
    activeId,
    onOpenSection,
    onBack,
    onPreview,
    onApply,
    applyDisabled = false,
    applyLabel
  }: {
    view: 'list' | 'section' | 'preview';
    sections: SectionItem[];
    activeId: string;
    onOpenSection: (id: string) => void;
    onBack: () => void;
    onPreview: () => void;
    onApply: () => void;
    applyDisabled?: boolean;
    applyLabel: string;
  } = $props();

  const title = $derived(
    view === 'preview'
      ? $t('constructor.m_preview')
      : (sections.find((s) => s.id === activeId)?.label ?? '')
  );

  function go(fn: () => void) {
    fn();
    window.scrollTo({ top: 0 });
  }
</script>

{#if view === 'list'}
  <nav class="cmn-list" aria-label={$t('constructor.m_sections')}>
    {#each sections as s (s.id)}
      <button type="button" class="cmn-item" onclick={() => go(() => onOpenSection(s.id))}>
        <span class="cmn-label">{s.label}</span>
        {#if s.summary}<span class="cmn-summary">{s.summary}</span>{/if}
        <Icon name="chevron-right" size={16} />
      </button>
    {/each}
  </nav>
{:else}
  <div class="cmn-back-row">
    <button type="button" class="cmn-back" onclick={() => go(onBack)}>
      <svg
        width="18"
        height="18"
        viewBox="0 0 24 24"
        fill="none"
        stroke="currentColor"
        stroke-width="2.2"
        aria-hidden="true"><path d="M15 18l-6-6 6-6" /></svg
      >
      {$t('constructor.m_sections')}
    </button>
    <h3 class="cmn-title">{title}</h3>
  </div>
{/if}

{#if view !== 'preview'}
  <div class="cmn-bar">
    <button type="button" class="btn btn-secondary" onclick={() => go(onPreview)}>
      {$t('constructor.m_preview')}
    </button>
    <button type="button" class="btn btn-primary" disabled={applyDisabled} onclick={onApply}>
      {applyLabel}
    </button>
  </div>
{/if}

<style>
  .cmn-list {
    display: flex;
    flex-direction: column;
    margin-bottom: 16px;
    border: 1px solid var(--border);
    border-radius: var(--radius-lg);
    background: var(--bg-card);
    overflow: hidden;
  }

  .cmn-item {
    display: flex;
    align-items: center;
    gap: 10px;
    min-height: 52px;
    padding: 8px 12px 8px 16px;
    border: 0;
    border-bottom: 1px solid var(--border-light);
    background: none;
    color: var(--fg-dim);
    font: inherit;
    text-align: left;
    cursor: pointer;
  }

  .cmn-item:last-child {
    border-bottom: 0;
  }

  .cmn-item:active {
    background: var(--hover);
  }

  .cmn-label {
    flex: 1;
    min-width: 0;
    color: var(--fg-primary);
    font-weight: 600;
  }

  .cmn-summary {
    flex-shrink: 0;
    font-size: var(--font-size-sm);
    color: var(--fg-secondary);
    font-variant-numeric: tabular-nums;
  }

  .cmn-back-row {
    margin: 0 0 12px;
  }

  .cmn-back {
    display: inline-flex;
    align-items: center;
    gap: 2px;
    min-height: 40px;
    margin-left: -8px;
    padding: 0 10px 0 4px;
    border: 0;
    border-radius: var(--radius-md);
    background: none;
    color: var(--accent-text);
    font: inherit;
    font-weight: 600;
    cursor: pointer;
  }

  .cmn-title {
    margin: 2px 0 0;
    font-size: var(--font-size-xl);
  }

  /* Плавающая панель действий над нижней панелью разделов */
  .cmn-bar {
    position: fixed;
    left: max(12px, env(safe-area-inset-left));
    right: max(12px, env(safe-area-inset-right));
    bottom: calc(var(--dock-space) + 10px);
    z-index: 80;
    display: grid;
    grid-template-columns: auto 1fr;
    gap: 8px;
    padding: 8px;
    border: 1px solid var(--border);
    border-radius: var(--radius-lg);
    background: var(--bg-card);
    box-shadow: var(--shadow-md);
  }

  .cmn-bar :global(.btn) {
    min-height: 44px;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
</style>
