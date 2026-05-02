<script lang="ts">
  import { onMount, onDestroy } from 'svelte'
  import { EditorView, keymap, lineNumbers, highlightActiveLineGutter, highlightSpecialChars, drawSelection, dropCursor, rectangularSelection, crosshairCursor, highlightActiveLine } from '@codemirror/view'
  import { EditorState } from '@codemirror/state'
  import { defaultKeymap, history, historyKeymap } from '@codemirror/commands'
  import { searchKeymap, highlightSelectionMatches } from '@codemirror/search'
  import { autocompletion, completionKeymap, closeBrackets, closeBracketsKeymap } from '@codemirror/autocomplete'
  import { foldGutter, indentOnInput, syntaxHighlighting, defaultHighlightStyle, bracketMatching, foldKeymap } from '@codemirror/language'
  import { lintKeymap } from '@codemirror/lint'
  import { json } from '@codemirror/lang-json'
  import { yaml } from '@codemirror/lang-yaml'

  let editorContainer: HTMLDivElement
  let editorView: EditorView | null = null
  let files: string[] = []
  let selectedFile = ''
  let loading = false
  let saving = false
  let message = ''
  let backups: string[] = []

  async function loadFiles() {
    try {
      const res = await fetch('/api/config/list')
      if (!res.ok) throw new Error('Failed to load files')
      files = await res.json()
    } catch (e) {
      message = 'Ошибка загрузки списка файлов: ' + e.message
    }
  }

  async function loadFile(path: string) {
    if (!path) return
    
    loading = true
    message = ''
    
    try {
      const res = await fetch(`/api/config/read?path=${encodeURIComponent(path)}`)
      if (!res.ok) throw new Error('Failed to load file')
      
      const content = await res.text()
      
      // Determine language based on file extension
      const lang = path.endsWith('.yaml') || path.endsWith('.yml') ? yaml() : json()
      
      // Create new editor state with basic setup extensions
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
      message = 'Ошибка загрузки файла: ' + e.message
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
      
      const res = await fetch(`/api/config/save?path=${encodeURIComponent(selectedFile)}`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: content
      })
      
      if (!res.ok) throw new Error('Failed to save file')
      
      message = '✓ Файл сохранён'
      await loadBackups(selectedFile)
      setTimeout(() => message = '', 3000)
    } catch (e) {
      message = 'Ошибка сохранения: ' + e.message
    } finally {
      saving = false
    }
  }

  async function restoreBackup(backupPath: string) {
    if (!confirm('Восстановить из backup?')) return
    
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
      
      message = '✓ Backup восстановлен (не забудьте сохранить)'
    } catch (e) {
      message = 'Ошибка восстановления: ' + e.message
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
    <h3>Конфиги</h3>
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
      <h3>Backups</h3>
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
      <span class="file-name">{selectedFile ? selectedFile.split('/').pop() : 'Выберите файл'}</span>
      <div class="toolbar-actions">
        <button on:click={saveFile} disabled={!selectedFile || saving}>
          {saving ? 'Сохранение...' : 'Сохранить'}
        </button>
      </div>
    </div>

    {#if message}
      <div class="message" class:error={message.includes('Ошибка')}>
        {message}
      </div>
    {/if}

    {#if loading}
      <div class="loading">Загрузка...</div>
    {:else if !selectedFile}
      <div class="empty-state">
        <p>Выберите файл для редактирования</p>
      </div>
    {:else}
      <div class="editor-container" bind:this={editorContainer}></div>
    {/if}
  </div>
</div>

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

  .sidebar h3 {
    margin: 0 0 0.5rem 0;
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

  .toolbar-actions button {
    padding: 0.5rem 1rem;
    background: var(--primary);
    color: white;
    border: none;
    border-radius: 4px;
    cursor: pointer;
    font-size: 0.875rem;
  }

  .toolbar-actions button:hover:not(:disabled) {
    opacity: 0.9;
  }

  .toolbar-actions button:disabled {
    opacity: 0.5;
    cursor: not-allowed;
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

  :global(.cm-editor) {
    height: 100%;
    font-size: 14px;
  }

  :global(.cm-scroller) {
    overflow: auto;
  }
</style>
