<script lang="ts">
  import { onMount, onDestroy } from 'svelte'
  import { t } from './i18n'
  import { EditorView, keymap, lineNumbers, highlightActiveLineGutter, highlightSpecialChars, drawSelection, dropCursor, rectangularSelection, crosshairCursor, highlightActiveLine } from '@codemirror/view'
  import { EditorState } from '@codemirror/state'
  import { defaultKeymap, history, historyKeymap } from '@codemirror/commands'
  import { searchKeymap, highlightSelectionMatches } from '@codemirror/search'
  import { autocompletion, completionKeymap, closeBrackets, closeBracketsKeymap } from '@codemirror/autocomplete'
  import { foldGutter, indentOnInput, syntaxHighlighting, defaultHighlightStyle, bracketMatching, foldKeymap } from '@codemirror/language'
  import { lintKeymap, linter } from '@codemirror/lint'
  import { json, jsonParseLinter, jsonLanguage } from '@codemirror/lang-json'
  import { yaml, yamlLanguage } from '@codemirror/lang-yaml'
  import { hoverTooltip } from '@codemirror/view'

  // Schema support
  import {
    jsonSchemaLinter,
    jsonSchemaHover,
    jsonCompletion,
    stateExtensions,
    handleRefresh
  } from 'codemirror-json-schema'
  import {
    yamlSchemaLinter,
    yamlSchemaHover,
    yamlCompletion
  } from 'codemirror-json-schema/yaml'

  // Schema definitions
  import { xraySchema } from './schemas/xray'
  import { mihomoSchema } from './schemas/mihomo'

  export let onSwitchTab: (tab: string) => void = () => {}

  let editorContainer: HTMLDivElement
  let editorView: EditorView | null = null
  let files: string[] = []
  let selectedFile = ''
  let loading = false
  let saving = false
  let message = ''
  let backups: string[] = []

  // Schema assist mode
  let schemaEnabled = true
  let expertMode = false

  // CRUD modals
  let showCreateModal = false
  let showRenameModal = false
  let newFileName = ''
  let renameTarget = ''

  async function loadFiles() {
    try {
      const res = await fetch('/api/config/list')
      if (!res.ok) throw new Error('Failed to load files')
      files = await res.json()
    } catch (e: any) {
      message = $t('editor.load_error') + ': ' + e.message
    }
  }

  function getSchemaExtensions(path: string) {
    if (!schemaEnabled) return []

    const isYaml = path.endsWith('.yaml') || path.endsWith('.yml')
    const isJson = path.endsWith('.json')

    // Determine which schema to use
    let schema: any = null
    if (path.includes('xray') || path.includes('/opt/etc/xray')) {
      schema = xraySchema
    } else if (path.includes('mihomo') || path.includes('config.yaml')) {
      schema = mihomoSchema
    }

    if (!schema) return []

    if (isJson) {
      return [
        linter(jsonParseLinter(), { delay: 300 }),
        linter(jsonSchemaLinter(), { needsRefresh: handleRefresh }),
        jsonLanguage.data.of({ autocomplete: jsonCompletion() }),
        hoverTooltip(jsonSchemaHover()),
        stateExtensions(schema)
      ]
    }

    if (isYaml) {
      return [
        linter(yamlSchemaLinter(), { needsRefresh: handleRefresh }),
        yamlLanguage.data.of({ autocomplete: yamlCompletion() }),
        hoverTooltip(yamlSchemaHover()),
        stateExtensions(schema)
      ]
    }

    return []
  }

  async function loadFile(path: string) {
    if (!path) return
    
    loading = true
    message = ''
    
    try {
      const res = await fetch(`/api/config/read?path=${encodeURIComponent(path)}`)
      if (!res.ok) throw new Error('Failed to load file')
      
      const content = await res.text()
      
      const lang = path.endsWith('.yaml') || path.endsWith('.yml') ? yaml() : json()
      const schemaExts = getSchemaExtensions(path)
      
      const state = EditorState.create({
        doc: content,
        extensions: [
          lineNumbers(),
          highlightActiveLineGutter(),
          highlightSpecialChars(),
          history(),
          foldGutter(),
          drawSelection(),
          dropCursor(),
          EditorState.allowMultipleSelections.of(true),
          indentOnInput(),
          syntaxHighlighting(defaultHighlightStyle, { fallback: true }),
          bracketMatching(),
          closeBrackets(),
          autocompletion(),
          rectangularSelection(),
          crosshairCursor(),
          highlightActiveLine(),
          highlightSelectionMatches(),
          keymap.of([
            ...closeBracketsKeymap,
            ...defaultKeymap,
            ...searchKeymap,
            ...historyKeymap,
            ...foldKeymap,
            ...completionKeymap,
            ...lintKeymap
          ]),
          lang,
          EditorView.lineWrapping,
          ...schemaExts
        ]
      })
      
      if (editorView) {
        editorView.setState(state)
      } else {
        editorView = new EditorView({
          state,
          parent: editorContainer
        })
      }
      
      selectedFile = path
      await loadBackups(path)
    } catch (e) {
      message = $t('editor.file_load_error') + ': ' + e.message
    } finally {
      loading = false
    }
  }

  async function loadBackups(path: string) {
    try {
      const res = await fetch(`/api/config/backups?path=${encodeURIComponent(path)}`)
      if (res.ok) {
        backups = await res.json()
      }
    } catch (e) {
      // Backups are optional
    }
  }

  async function saveFile() {
    if (!selectedFile || !editorView) return
    
    saving = true
    message = ''
    
    try {
      const content = editorView.state.doc.toString()
      
      const csrfToken = localStorage.getItem('csrf_token')
      const res = await fetch(`/api/config/save?path=${encodeURIComponent(selectedFile)}`, {
        method: 'POST',
        headers: { 
          'Content-Type': 'application/json',
          'X-CSRF-Token': csrfToken || ''
        },
        body: content
      })
      
      if (!res.ok) throw new Error('Failed to save file')
      
      message = $t('editor.file_saved')
      await loadBackups(selectedFile)
      setTimeout(() => message = '', 3000)
    } catch (e) {
      message = $t('editor.save_error') + ': ' + e.message
    } finally {
      saving = false
    }
  }

  async function restoreBackup(backupPath: string) {
    if (!confirm($t('editor.backup_restored').replace('✓ ', ''))) return
    
    try {
      const res = await fetch(`/api/config/read?path=${encodeURIComponent(backupPath)}`)
      if (!res.ok) throw new Error('Failed to load backup')
      
      const content = await res.text()
      
      if (editorView) {
        editorView.dispatch({
          changes: {
            from: 0,
            to: editorView.state.doc.length,
            insert: content
          }
        })
      }
      
      message = $t('editor.backup_restored')
    } catch (e) {
      message = $t('editor.restore_error') + ': ' + e.message
    }
  }

  async function createFile() {
    if (!newFileName) return
    
    const csrfToken = localStorage.getItem('csrf_token')
    const path = selectedFile ? selectedFile.substring(0, selectedFile.lastIndexOf('/') + 1) + newFileName : '/opt/etc/xray/configs/' + newFileName
    
    try {
      const res = await fetch(`/api/config/create?path=${encodeURIComponent(path)}`, {
        method: 'POST',
        headers: { 'X-CSRF-Token': csrfToken || '' }
      })
      
      if (!res.ok) throw new Error(await res.text())
      
      message = '✓ ' + $t('editor.create_file')
      showCreateModal = false
      newFileName = ''
      await loadFiles()
      await loadFile(path)
    } catch (e) {
      message = $t('editor.create_error') + ': ' + e.message
    }
  }

  async function deleteFile() {
    if (!selectedFile) return
    if (!confirm($t('app.delete') + ' ' + selectedFile.split('/').pop() + '?')) return
    
    const csrfToken = localStorage.getItem('csrf_token')
    
    try {
      const res = await fetch(`/api/config/delete?path=${encodeURIComponent(selectedFile)}`, {
        method: 'POST',
        headers: { 'X-CSRF-Token': csrfToken || '' }
      })
      
      if (!res.ok) throw new Error(await res.text())
      
      message = '✓ ' + $t('app.delete')
      selectedFile = ''
      backups = []
      await loadFiles()
    } catch (e) {
      message = $t('editor.delete_error') + ': ' + e.message
    }
  }

  async function renameFile() {
    if (!renameTarget || !selectedFile) return
    
    const csrfToken = localStorage.getItem('csrf_token')
    const newPath = selectedFile.substring(0, selectedFile.lastIndexOf('/') + 1) + renameTarget
    
    try {
      const res = await fetch(`/api/config/rename?old=${encodeURIComponent(selectedFile)}&new=${encodeURIComponent(newPath)}`, {
        method: 'POST',
        headers: { 'X-CSRF-Token': csrfToken || '' }
      })
      
      if (!res.ok) throw new Error(await res.text())
      
      message = '✓ ' + $t('app.rename')
      showRenameModal = false
      renameTarget = ''
      await loadFiles()
      await loadFile(newPath)
    } catch (e) {
      message = $t('editor.rename_error') + ': ' + e.message
    }
  }

  function toggleSchema() {
    schemaEnabled = !schemaEnabled
    if (selectedFile) {
      // Reload current file to apply/remove schema extensions
      const content = editorView ? editorView.state.doc.toString() : ''
      loadFile(selectedFile)
    }
  }

  function toggleExpertMode() {
    expertMode = !expertMode
    // Expert mode currently just disables schema validation visual noise
    if (selectedFile) {
      loadFile(selectedFile)
    }
  }

  function applyQuickFixes() {
    if (!editorView || !selectedFile) return

    const content = editorView.state.doc.toString()
    const isYaml = selectedFile.endsWith('.yaml') || selectedFile.endsWith('.yml')
    const isXray = selectedFile.includes('xray')
    const isMihomo = selectedFile.includes('mihomo') || selectedFile.includes('config.yaml')

    let fixed = content
    let fixesApplied = 0

    try {
      if (isYaml) {
        // Simple YAML fixes
        if (isMihomo) {
          if (!fixed.includes('proxies:') && !fixed.includes('proxy-providers:')) {
            fixed = 'proxies:\n' + fixed
            fixesApplied++
          }
          if (!fixed.includes('proxy-groups:')) {
            fixed = fixed + '\nproxy-groups:\n  - name: 🚀 Выбор прокси\n    type: select\n    proxies:\n      - DIRECT\n'
            fixesApplied++
          }
        }
      } else {
        // JSON fixes
        const data = JSON.parse(fixed)
        if (isXray) {
          if (!data.inbounds) {
            data.inbounds = []
            fixesApplied++
          }
          if (!data.outbounds) {
            data.outbounds = [{ protocol: 'freedom', tag: 'direct' }]
            fixesApplied++
          }
          if (!data.routing) {
            data.routing = { rules: [] }
            fixesApplied++
          }
        }
        fixed = JSON.stringify(data, null, 2)
      }

      if (fixesApplied > 0) {
        editorView.dispatch({
          changes: { from: 0, to: editorView.state.doc.length, insert: fixed }
        })
        message = `✓ Quick fixes applied: ${fixesApplied}`
      } else {
        message = '✓ No quick fixes needed'
      }
      setTimeout(() => message = '', 3000)
    } catch (e) {
      message = 'Quick fix error: ' + e.message
    }
  }

  onMount(() => {
    loadFiles()
  })

  onDestroy(() => {
    if (editorView) {
      editorView.destroy()
    }
  })
</script>

<div class="editor-page">
  <div class="sidebar">
    <div class="sidebar-header">
      <div style="display: flex; align-items: center; gap: 0.5rem;">
        <button class="btn-icon-small" on:click={() => onSwitchTab('dashboard')} title="Назад">
          ←
        </button>
        <h3>{$t('editor.configs')}</h3>
      </div>
      <button class="btn-icon-small" on:click={() => { showCreateModal = true; newFileName = '' }} title={$t('editor.create_file')}>
        +
      </button>
    </div>
    <div class="file-list">
      {#each files as file}
        <button 
          class="file-item" 
          class:active={file === selectedFile}
          on:click={() => loadFile(file)}
        >
          {file.split('/').pop()}
        </button>
      {/each}
    </div>
    
    {#if backups.length > 0}
      <h3>{$t('editor.backups')}</h3>
      <div class="backup-list">
        {#each backups as backup}
          <button 
            class="backup-item"
            on:click={() => restoreBackup(backup)}
          >
            {backup.split('.backup-')[1] || backup}
          </button>
        {/each}
      </div>
    {/if}
  </div>

  <div class="editor-main">
    <div class="toolbar">
      <span class="file-name">{selectedFile ? selectedFile.split('/').pop() : $t('editor.select_file')}</span>
      <div class="toolbar-actions">
        {#if selectedFile}
          <label class="toggle-label" title="Enable schema validation, autocomplete and hover tooltips">
            <input type="checkbox" bind:checked={schemaEnabled} on:change={toggleSchema} />
            {$t('editor.schema')}
          </label>
          <label class="toggle-label" title="Expert mode: full schema assist / Beginner: simplified">
            <input type="checkbox" bind:checked={expertMode} on:change={toggleExpertMode} />
            {$t('editor.expert')}
          </label>
          <button on:click={applyQuickFixes} class="btn-secondary" title="Apply common fixes">
            🔧 {$t('editor.quick_fix')}
          </button>
          <button on:click={() => { showRenameModal = true; renameTarget = selectedFile.split('/').pop() || '' }} class="btn-secondary">
            {$t('app.rename')}
          </button>
          <button on:click={deleteFile} class="btn-danger">
            {$t('app.delete')}
          </button>
        {/if}
        <button on:click={saveFile} disabled={!selectedFile || saving} class="btn-primary">
          {saving ? $t('app.loading') : $t('app.save')}
        </button>
      </div>
    </div>

    {#if message}
      <div class="message" class:error={message.includes($t('app.error'))}>
        {message}
      </div>
    {/if}

    {#if loading}
      <div class="loading">{$t('app.loading')}</div>
    {:else if !selectedFile}
      <div class="empty-state">
        <p>{$t('editor.select_file')}</p>
      </div>
    {:else}
      <div class="editor-container" bind:this={editorContainer}></div>
    {/if}
  </div>
</div>

{#if showCreateModal}
  <div class="modal-overlay" on:click={() => showCreateModal = false}>
    <div class="modal" on:click|stopPropagation>
      <h3>{$t('editor.create_file')}</h3>
      <input 
        type="text" 
        bind:value={newFileName}
        placeholder={$t('editor.file_name')}
        class="input"
        on:keydown={(e) => e.key === 'Enter' && createFile()}
      />
      <div class="modal-actions">
        <button on:click={() => showCreateModal = false} class="btn btn-secondary">{$t('app.cancel')}</button>
        <button on:click={createFile} class="btn btn-primary">{$t('app.create')}</button>
      </div>
    </div>
  </div>
{/if}

{#if showRenameModal}
  <div class="modal-overlay" on:click={() => showRenameModal = false}>
    <div class="modal" on:click|stopPropagation>
      <h3>{$t('editor.rename_file')}</h3>
      <input 
        type="text" 
        bind:value={renameTarget}
        placeholder={$t('editor.new_name')}
        class="input"
        on:keydown={(e) => e.key === 'Enter' && renameFile()}
      />
      <div class="modal-actions">
        <button on:click={() => showRenameModal = false} class="btn btn-secondary">{$t('app.cancel')}</button>
        <button on:click={renameFile} class="btn btn-primary">{$t('app.rename')}</button>
      </div>
    </div>
  </div>
{/if}

<style>
  .editor-page {
    display: flex;
    height: 100vh;
    background: var(--bg);
  }

  .sidebar {
    width: 250px;
    background: var(--card-bg);
    border-right: 1px solid var(--border);
    padding: 1rem;
    overflow-y: auto;
  }

  .sidebar-header {
    display: flex;
    justify-content: space-between;
    align-items: center;
    margin-bottom: 0.5rem;
  }

  .sidebar h3 {
    margin: 0;
    font-size: 0.875rem;
    font-weight: 600;
    color: var(--text-secondary);
    text-transform: uppercase;
  }

  .file-list, .backup-list {
    display: flex;
    flex-direction: column;
    gap: 0.25rem;
    margin-bottom: 1.5rem;
  }

  .file-item, .backup-item {
    padding: 0.5rem;
    background: transparent;
    border: none;
    border-radius: 4px;
    text-align: left;
    cursor: pointer;
    color: var(--text);
    font-size: 0.875rem;
    transition: background 0.2s;
  }

  .file-item:hover, .backup-item:hover {
    background: var(--hover);
  }

  .file-item.active {
    background: var(--primary);
    color: white;
  }

  .backup-item {
    font-size: 0.75rem;
    color: var(--text-secondary);
  }

  .editor-main {
    flex: 1;
    display: flex;
    flex-direction: column;
    overflow: hidden;
  }

  .toolbar {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 0.75rem 1rem;
    background: var(--card-bg);
    border-bottom: 1px solid var(--border);
  }

  .file-name {
    font-weight: 500;
    color: var(--text);
  }

  .toolbar-actions {
    display: flex;
    gap: 0.5rem;
  }

  .toolbar-actions button {
    padding: 0.5rem 1rem;
    border: none;
    border-radius: 4px;
    cursor: pointer;
    font-size: 0.875rem;
    transition: opacity 0.2s;
  }

  .toolbar-actions button:hover:not(:disabled) {
    opacity: 0.9;
  }

  .toolbar-actions button:disabled {
    opacity: 0.5;
    cursor: not-allowed;
  }

  .btn-primary {
    background: var(--primary);
    color: white;
  }

  .btn-secondary {
    background: transparent;
    border: 1px solid var(--border);
    color: var(--text);
  }

  .btn-danger {
    background: var(--danger);
    color: white;
  }

  .btn-icon-small {
    padding: 0.25rem 0.5rem;
    background: transparent;
    border: 1px solid var(--border);
    border-radius: 4px;
    cursor: pointer;
    font-size: 1rem;
    color: var(--text);
  }

  .toggle-label {
    display: flex;
    align-items: center;
    gap: 0.25rem;
    font-size: 0.8rem;
    color: var(--text-secondary);
    cursor: pointer;
    user-select: none;
  }

  .toggle-label input[type="checkbox"] {
    cursor: pointer;
  }

  .message {
    padding: 0.75rem 1rem;
    background: var(--success-bg, #d4edda);
    color: var(--success-text, #155724);
    border-bottom: 1px solid var(--border);
  }

  .message.error {
    background: var(--error-bg, #f8d7da);
    color: var(--error-text, #721c24);
  }

  .loading, .empty-state {
    display: flex;
    align-items: center;
    justify-content: center;
    height: 100%;
    color: var(--text-secondary);
  }

  .editor-container {
    flex: 1;
    overflow: auto;
  }

  .modal-overlay {
    position: fixed;
    top: 0;
    left: 0;
    right: 0;
    bottom: 0;
    background: rgba(0,0,0,0.5);
    display: flex;
    align-items: center;
    justify-content: center;
    z-index: 1000;
  }

  .modal {
    background: var(--card-bg);
    border: 1px solid var(--border);
    border-radius: var(--radius);
    padding: 1.5rem;
    width: 100%;
    max-width: 400px;
    box-shadow: var(--shadow);
  }

  .modal h3 {
    margin: 0 0 1rem 0;
  }

  .modal-actions {
    display: flex;
    justify-content: flex-end;
    gap: 0.5rem;
    margin-top: 1rem;
  }

  .input {
    width: 100%;
    padding: 0.5rem;
    border: 1px solid var(--border);
    border-radius: 4px;
    background: var(--bg);
    color: var(--text);
    font-size: 0.875rem;
  }

  :global(.cm-editor) {
    height: 100%;
    font-size: 14px;
  }

  :global(.cm-scroller) {
    overflow: auto;
  }
</style>
