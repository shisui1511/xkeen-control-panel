<script lang="ts">
  import Modal from '../Modal.svelte';
  import Tabs, { type TabItem } from '../Tabs.svelte';
  import { t } from '../../i18n';
  import { apiFetchJSON } from '../../lib/api';

  export interface Template {
    name: string;
    type: 'xray' | 'mihomo';
    description?: string;
    content?: string;
  }

  interface Props {
    isOpen: boolean;
    selectedFile?: string;
    hasEditorView?: boolean;
    onApplyTemplate: (template: Template) => void | Promise<void>;
    onClose: () => void;
  }

  let {
    isOpen,
    selectedFile = '',
    hasEditorView = false,
    onApplyTemplate,
    onClose
  }: Props = $props();

  let templates = $state<Template[]>([]);
  let templateTab = $state<'xray' | 'mihomo'>('xray');
  let selectedTemplate = $state<Template | null>(null);
  let templatePreview = $state('');
  let templateLoading = $state(false);

  let filteredTemplates = $derived(templates.filter((t) => t.type === templateTab));

  const templateTabItems = $derived<TabItem[]>([
    { value: 'xray', label: $t('editor.templates_tab_xray') },
    { value: 'mihomo', label: $t('editor.templates_tab_mihomo') }
  ]);

  async function loadTemplates() {
    try {
      const data = await apiFetchJSON<Template[]>('/api/templates/list');
      templates = Array.isArray(data) ? data : [];
    } catch (e: any) {
      if (e?.status === 401) return;
      templates = [];
    }
  }

  function loadTemplatePreview(template: Template) {
    selectedTemplate = template;
    templatePreview = (template.content || '').split('\n').slice(0, 50).join('\n');
  }

  let prevOpen = false;

  $effect(() => {
    if (isOpen && !prevOpen) {
      prevOpen = true;
      const isMihomo =
        selectedFile &&
        (selectedFile.endsWith('.yaml') ||
          selectedFile.endsWith('.yml') ||
          selectedFile.includes('mihomo'));
      templateTab = isMihomo ? 'mihomo' : 'xray';
      selectedTemplate = null;
      templatePreview = '';
      if (templates.length === 0) {
        loadTemplates().then(() => {
          const first = templates.filter((t) => t.type === templateTab)[0];
          if (first && !selectedTemplate) {
            loadTemplatePreview(first);
          }
        });
      } else {
        const first = templates.filter((t) => t.type === templateTab)[0];
        if (first) {
          loadTemplatePreview(first);
        }
      }
    } else if (!isOpen) {
      prevOpen = false;
    }
  });

  async function handleApply() {
    if (!selectedTemplate) return;
    templateLoading = true;
    try {
      await onApplyTemplate(selectedTemplate);
    } finally {
      templateLoading = false;
    }
  }
</script>

<Modal
  {isOpen}
  title={$t('editor.templates')}
  maxWidth="900px"
  class="templates-wide-modal"
  onclose={onClose}
>
  <div style="margin-top: -10px; margin-bottom: 12px;">
    <p class="templates-modal-subtitle">
      {$t('editor.templates_desc')}
    </p>
  </div>

  <!-- 2-column body -->
  <div class="templates-body-grid">
    <!-- Left column: tabs + list -->
    <div class="templates-col-list">
      <div class="templates-kernel-tabs">
        <Tabs
          items={templateTabItems}
          bind:value={templateTab}
          ariaLabel={$t('editor.templates')}
          onchange={(val) => {
            templateTab = val as 'xray' | 'mihomo';
            selectedTemplate = null;
            templatePreview = '';
            const first = filteredTemplates[0];
            if (first) loadTemplatePreview(first);
          }}
        />
      </div>

      <div class="template-list">
        {#each filteredTemplates as template (template.name)}
          <button
            class="template-item"
            class:selected={selectedTemplate?.name === template.name}
            onclick={() => loadTemplatePreview(template)}
            disabled={templateLoading}
          >
            <div class="template-info">
              <span class="template-name">{template.name}</span>
              <span class="template-desc">{template.description}</span>
            </div>
            <span class="template-type">{template.type}</span>
          </button>
        {:else}
          <div class="templates-empty-state">
            <p class="templates-empty-title">{$t('editor.no_templates')}</p>
            <p class="templates-empty-hint">{$t('editor.no_templates_hint')}</p>
          </div>
        {/each}
      </div>
    </div>

    <!-- Right column: preview -->
    <div class="templates-col-preview">
      {#if templatePreview}
        <pre class="template-preview-code">{templatePreview}</pre>
      {:else}
        <div class="templates-preview-placeholder">
          <p style="color: var(--fg-dim); font-size: 14px; text-align: center;">
            {selectedTemplate ? '' : $t('editor.select_template_preview')}
          </p>
        </div>
      {/if}
    </div>
  </div>

  <!-- Footer -->
  <div
    class="templates-modal-footer"
    style="margin-top: 16px; display: flex; justify-content: flex-end;"
  >
    <button
      class="btn btn-primary"
      disabled={!selectedTemplate || !hasEditorView || templateLoading}
      title={!hasEditorView ? $t('editor.no_file_for_template') : undefined}
      onclick={handleApply}
    >
      {$t('editor.apply_template')}
    </button>
  </div>
</Modal>

<style>
  :global(.templates-wide-modal) {
    max-width: 900px !important;
    width: 90vw !important;
    padding: 0 !important;
    overflow: hidden;
    display: flex;
    flex-direction: column;
  }

  :global(.templates-wide-modal .modal-header) {
    padding: 20px 20px 12px;
    border-bottom: 1px solid var(--border);
    display: flex;
    justify-content: space-between;
    align-items: flex-start;
    flex-shrink: 0;
  }

  .templates-modal-subtitle {
    margin: 0;
    color: var(--fg-dim);
    font-size: 13px;
  }

  .templates-body-grid {
    display: grid;
    grid-template-columns: 1fr 1fr;
    grid-template-rows: minmax(0, 1fr);
    gap: 0;
    flex: 1;
    min-height: 0;
    overflow: hidden;
    max-height: 460px;
  }

  .templates-col-list {
    display: flex;
    flex-direction: column;
    min-height: 0;
    border-right: 1px solid var(--border);
    overflow: hidden;
  }

  .templates-col-list :global(.tabs) {
    margin-bottom: 0;
    padding: 0 12px;
  }

  .templates-col-list .template-list {
    padding: 12px;
  }

  .template-list {
    display: flex;
    flex-direction: column;
    gap: 8px;
    overflow-y: auto;
    flex: 1;
    scrollbar-width: thin;
    scrollbar-color: var(--border-strong) transparent;
  }

  .template-list::-webkit-scrollbar {
    width: 4px;
  }

  .template-list::-webkit-scrollbar-thumb {
    background: var(--border-strong);
    border-radius: 2px;
  }

  .template-item {
    display: flex;
    justify-content: space-between;
    align-items: center;
    padding: 12px;
    background: var(--bg-card);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    cursor: pointer;
    text-align: left;
    transition: all 0.2s;
    width: 100%;
    color: var(--fg-primary);
  }

  .template-item:hover {
    border-color: var(--accent);
    background: var(--bg-elevated);
  }

  .template-item.selected {
    border-color: var(--accent);
    background: var(--bg-elevated);
  }

  .template-info {
    display: flex;
    flex-direction: column;
    gap: 4px;
  }

  .template-name {
    font-weight: 600;
    font-size: 14px;
    color: var(--fg-primary);
  }

  .template-desc {
    font-size: 12px;
    color: var(--fg-dim);
  }

  .template-type {
    font-size: 12px;
    font-weight: 600;
    background: var(--bg-card);
    padding: 2px 6px;
    border-radius: 4px;
    border: 1px solid var(--border);
    color: var(--fg-dim);
    font-family: var(--font-family-mono);
  }

  .templates-col-preview {
    background: var(--code-bg);
    min-height: 0;
    overflow: hidden;
    display: flex;
    flex-direction: column;
  }

  .template-preview-code {
    margin: 0;
    padding: 16px;
    font-family: var(--font-family-mono);
    font-size: 14px;
    line-height: 1.5;
    color: var(--code-fg);
    overflow-y: auto;
    overflow-x: auto;
    white-space: pre;
    height: 100%;
    scrollbar-width: thin;
    scrollbar-color: var(--border-strong) transparent;
  }

  .template-preview-code::-webkit-scrollbar {
    width: 4px;
    height: 4px;
  }

  .template-preview-code::-webkit-scrollbar-thumb {
    background: var(--border-strong);
    border-radius: 2px;
  }

  .templates-preview-placeholder {
    display: flex;
    align-items: center;
    justify-content: center;
    height: 100%;
    min-height: 200px;
    color: var(--fg-dim);
  }

  .templates-empty-state {
    padding: 24px 16px;
    text-align: center;
  }

  .templates-empty-title {
    font-size: 14px;
    font-weight: 600;
    color: var(--fg-secondary);
    margin: 0 0 6px;
  }

  .templates-empty-hint {
    font-size: 12px;
    color: var(--fg-dim);
    margin: 0;
  }

  .templates-modal-footer {
    padding: 12px 20px;
    border-top: 1px solid var(--border);
    display: flex;
    justify-content: flex-end;
    flex-shrink: 0;
  }
</style>
