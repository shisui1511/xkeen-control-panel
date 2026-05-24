<script lang="ts">
  import { onMount, onDestroy, tick } from 'svelte';
  import { t } from './i18n';
  import { showToast } from './stores';
  import Icon from './lib/components/Icon.svelte';
  import {
    EditorView,
    keymap,
    lineNumbers,
    highlightActiveLineGutter,
    highlightSpecialChars,
    drawSelection,
    dropCursor,
    rectangularSelection,
    crosshairCursor,
    highlightActiveLine
  } from '@codemirror/view';
  import { EditorState, Compartment } from '@codemirror/state';
  import { defaultKeymap, history, historyKeymap } from '@codemirror/commands';
  import { searchKeymap, highlightSelectionMatches } from '@codemirror/search';
  import {
    autocompletion,
    completionKeymap,
    closeBrackets,
    closeBracketsKeymap
  } from '@codemirror/autocomplete';
  import {
    foldGutter,
    indentOnInput,
    syntaxHighlighting,
    defaultHighlightStyle,
    bracketMatching,
    foldKeymap
  } from '@codemirror/language';
  import { lintKeymap, linter } from '@codemirror/lint';
  import { json, jsonParseLinter, jsonLanguage } from '@codemirror/lang-json';
  import { yaml, yamlLanguage } from '@codemirror/lang-yaml';
  import { hoverTooltip } from '@codemirror/view';

  // Schema support
  import {
    jsonSchemaLinter,
    jsonSchemaHover,
    jsonCompletion,
    stateExtensions,
    handleRefresh
  } from 'codemirror-json-schema';
  import { yamlSchemaLinter, yamlSchemaHover, yamlCompletion } from 'codemirror-json-schema/yaml';

  // Schema definitions
  import { xraySchema } from './schemas/xray';
  import { mihomoSchema } from './schemas/mihomo';

  // Snippets
  import { xraySnippetSource, mihomoSnippetSource } from './lib/snippets';

  export let onSwitchTab: (tab: string) => void = () => {};

  let editorContainer: HTMLDivElement;
  let editorView: EditorView | null = null;
  const schemaCompartment = new Compartment();
  interface Template {
    name: string;
    description: string;
    type: string;
    url: string;
  }

  interface ConfigFileInfo {
    name: string;
    path: string;
    size: number;
  }

  let files: ConfigFileInfo[] = [];
  let selectedFile = '';
  let loading = false;
  let saving = false;
  let backups: string[] = [];

  // Directory management
  const xrayDir = '/opt/etc/xray/configs';
  const mihomoDir = '/opt/etc/mihomo';
  let currentDir = xrayDir;

  // Dual-panel sidebar file lists
  let xrayFiles: ConfigFileInfo[] = [];
  let mihomoFiles: ConfigFileInfo[] = [];
  let fileSearchQuery = '';
  $: filteredXrayFiles = xrayFiles.filter(file => file.name.toLowerCase().includes(fileSearchQuery.toLowerCase()));
  $: filteredMihomoFiles = mihomoFiles.filter(file => file.name.toLowerCase().includes(fileSearchQuery.toLowerCase()));
  let showSidebar = true;

  // Status bar cursor position
  let cursorLine = 1;
  let cursorCol = 1;

  // Schema assist mode
  let schemaEnabled = true;
  let expertMode = false;

  // CRUD modals
  let showCreateModal = false;
  let showRenameModal = false;
  let showTemplatesModal = false;
  let newFileName = '';
  let renameTarget = '';
  let templates: Template[] = [];

  // Generator state
  let showGeneratorModal = false;
  let genProtocol = 'vless';
  let genAddress = '';
  let genPort = 443;
  let genUUID = crypto.randomUUID();
  let genSNI = '';
  let genFlow = 'xtls-rprx-vision';
  let genSecurity = 'reality';
  let genPublicKey = '';
  let genShortId = '';
  let genSpiderDomain = '';

  // Dirty state tracking
  let originalContent = '';
  let isDirty = false;

  // Draft state tracking
  let hasDraft = false;
  let draftContent = '';

  function restoreDraft() {
    if (!editorView || !draftContent) return;
    editorView.dispatch({
      changes: { from: 0, to: editorView.state.doc.length, insert: draftContent }
    });
    isDirty = true;
    hasDraft = false;
    showToast('success', $t('editor.draft_restored') || 'Draft restored');
  }

  function discardDraft() {
    if (selectedFile) {
      localStorage.removeItem('editor.draft.' + selectedFile);
      hasDraft = false;
      draftContent = '';
      showToast('info', $t('editor.draft_discarded') || 'Draft discarded');
    }
  }

  function checkDirty(): boolean {
    if (!editorView) return false;
    return editorView.state.doc.toString() !== originalContent;
  }

  function confirmUnsaved(): boolean {
    if (!checkDirty()) return true;
    return confirm($t('editor.unsaved_warning') || 'You have unsaved changes. Discard them?');
  }

  async function loadFiles(dir?: string) {
    if (dir) currentDir = dir;
    try {
      const [xRes, mRes] = await Promise.all([
        fetch(`/api/config/list?dir=${encodeURIComponent(xrayDir)}`),
        fetch(`/api/config/list?dir=${encodeURIComponent(mihomoDir)}`)
      ]);
      xrayFiles = xRes.ok ? await xRes.json() : [];
      mihomoFiles = mRes.ok ? await mRes.json() : [];
      files = [...xrayFiles, ...mihomoFiles];
    } catch (e: any) {
      showToast('error', $t('editor.load_error') + ': ' + e.message);
    }
  }

  function switchDir(dir: string) {
    if (currentDir === dir) return;
    if (!confirmUnsaved()) return;
    currentDir = dir;
    selectedFile = '';
    backups = [];
    originalContent = '';
    isDirty = false;
    if (editorView) {
      editorView.setState(EditorState.create({ doc: '' }));
    }
    loadFiles();
  }

  function getSchemaExtensions(path: string, expert: boolean = false) {
    if (!schemaEnabled) return [];

    const isYaml = path.endsWith('.yaml') || path.endsWith('.yml');
    const isJson = path.endsWith('.json');

    // Determine which schema to use
    let schema: any = null;
    if (path.includes('xray') || path.includes('/opt/etc/xray')) {
      schema = xraySchema;
    } else if (path.includes('mihomo') || path.includes('config.yaml')) {
      schema = mihomoSchema;
    }

    if (!schema) return [];

    const isXray = path.includes('xray') || path.includes('/opt/etc/xray');
    const snippetSource = isXray ? xraySnippetSource : mihomoSnippetSource;

    if (isJson) {
      // In expert mode, skip strict schema linting but keep autocomplete and hover
      if (expert) {
        return [
          linter(jsonParseLinter(), { delay: 300 }),
          jsonLanguage.data.of({ autocomplete: jsonCompletion() }),
          jsonLanguage.data.of({ autocomplete: snippetSource }),
          hoverTooltip(jsonSchemaHover()),
          stateExtensions(schema)
        ];
      }
      return [
        linter(jsonParseLinter(), { delay: 300 }),
        linter(jsonSchemaLinter(), { needsRefresh: handleRefresh }),
        jsonLanguage.data.of({ autocomplete: jsonCompletion() }),
        jsonLanguage.data.of({ autocomplete: snippetSource }),
        hoverTooltip(jsonSchemaHover()),
        stateExtensions(schema)
      ];
    }

    if (isYaml) {
      // In expert mode, skip strict schema linting but keep autocomplete and hover
      if (expert) {
        return [
          yamlLanguage.data.of({ autocomplete: yamlCompletion() }),
          yamlLanguage.data.of({ autocomplete: snippetSource }),
          hoverTooltip(yamlSchemaHover()),
          stateExtensions(schema)
        ];
      }
      return [
        linter(yamlSchemaLinter(), { needsRefresh: handleRefresh }),
        yamlLanguage.data.of({ autocomplete: yamlCompletion() }),
        yamlLanguage.data.of({ autocomplete: snippetSource }),
        hoverTooltip(yamlSchemaHover()),
        stateExtensions(schema)
      ];
    }

    return [];
  }

  async function loadFile(path: string) {
    if (!path) return;
    if (selectedFile && path !== selectedFile && !confirmUnsaved()) return;

    loading = true;
    isDirty = false;

    try {
      const res = await fetch(`/api/config/read?path=${encodeURIComponent(path)}`);
      if (!res.ok) throw new Error('Failed to load file');

      const content = await res.text();

      const lang = path.endsWith('.yaml') || path.endsWith('.yml') ? yaml() : json();
      const schemaExts = getSchemaExtensions(path, expertMode);

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
            {
              key: 'Mod-s',
              run: () => {
                checkBeforeSave();
                return true;
              }
            },
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
          EditorView.updateListener.of((update) => {
            if (update.docChanged) {
              const currentContent = update.state.doc.toString();
              isDirty = currentContent !== originalContent;
              if (selectedFile) {
                if (isDirty) {
                  localStorage.setItem(`editor.draft.${selectedFile}`, currentContent);
                } else {
                  localStorage.removeItem(`editor.draft.${selectedFile}`);
                }
              }
            }
            if (update.selectionSet || update.docChanged) {
              const pos = update.state.selection.main.head;
              const line = update.state.doc.lineAt(pos);
              cursorLine = line.number;
              cursorCol = pos - line.from + 1;
            }
          }),
          schemaCompartment.of(schemaExts)
        ]
      });

      originalContent = content;
      isDirty = false;

      // Check draft
      const draft = localStorage.getItem(`editor.draft.${path}`);
      if (draft && draft !== content) {
        hasDraft = true;
        draftContent = draft;
      } else {
        hasDraft = false;
        draftContent = '';
      }

      // Снять loading и установить selectedFile — Svelte отрендерит editorContainer
      loading = false;
      selectedFile = path;

      // Ждём рендера editorContainer в DOM
      await tick();

      if (editorView) {
        if (editorView.dom.isConnected) {
          // Same container — just swap state
          editorView.setState(state);
        } else {
          // Container was recreated (was destroyed while loading=true) — remount
          editorView.destroy();
          editorView = null;
          if (editorContainer) {
            editorView = new EditorView({ state, parent: editorContainer });
          }
        }
      } else if (editorContainer) {
        editorView = new EditorView({
          state,
          parent: editorContainer
        });
      }

      await loadBackups(path);
    } catch (e: any) {
      showToast('error', $t('editor.file_load_error') + ': ' + (e?.message || e));
      loading = false;
    }
  }

  async function loadBackups(path: string) {
    try {
      const res = await fetch(`/api/config/backups?path=${encodeURIComponent(path)}`);
      if (res.ok) {
        backups = await res.json();
      }
    } catch (e) {
      // Backups are optional
    }
  }

  let showSaveConfirmModal = false;
  let validationResult: { valid: boolean; error: string } | null = null;
  let validationLoading = false;
  let diffChanges: any[] = [];

  // Kebab menu for destructive actions (Delete)
  let showKebabMenu = false;
  function toggleKebab(e: MouseEvent) {
    e.stopPropagation();
    showKebabMenu = !showKebabMenu;
    if (showKebabMenu) {
      const close = () => {
        showKebabMenu = false;
        window.removeEventListener('click', close);
      };
      setTimeout(() => window.addEventListener('click', close), 0);
    }
  }

  interface DiffChange {
    type: 'added' | 'removed' | 'unchanged';
    value: string;
  }

  interface DiffGroup {
    type: 'added' | 'removed' | 'unchanged' | 'collapsed';
    lines: string[];
  }

  function getDiff(oldStr: string, newStr: string): DiffChange[] {
    const oldLines = oldStr.split('\n');
    const newLines = newStr.split('\n');

    const m = oldLines.length;
    const n = newLines.length;

    if (m + n > 2000) {
      return [
        {
          type: 'removed',
          value: 'File is too large for visual diff. Old version content hidden.'
        },
        {
          type: 'added',
          value: 'File is too large for visual diff. New version content will be saved.'
        }
      ];
    }

    const dp: number[][] = Array.from({ length: m + 1 }, () => new Array(n + 1).fill(0));

    for (let i = 1; i <= m; i++) {
      for (let j = 1; j <= n; j++) {
        if (oldLines[i - 1] === newLines[j - 1]) {
          dp[i][j] = dp[i - 1][j - 1] + 1;
        } else {
          dp[i][j] = Math.max(dp[i - 1][j], dp[i][j - 1]);
        }
      }
    }

    const diff: DiffChange[] = [];
    let i = m,
      j = n;
    while (i > 0 || j > 0) {
      if (i > 0 && j > 0 && oldLines[i - 1] === newLines[j - 1]) {
        diff.unshift({ type: 'unchanged', value: oldLines[i - 1] });
        i--;
        j--;
      } else if (j > 0 && (i === 0 || dp[i][j - 1] >= dp[i - 1][j])) {
        diff.unshift({ type: 'added', value: newLines[j - 1] });
        j--;
      } else if (i > 0 && (j === 0 || dp[i - 1][j] > dp[i][j - 1])) {
        diff.unshift({ type: 'removed', value: oldLines[i - 1] });
        i--;
      }
    }
    return diff;
  }

  function getDiffGroups(oldStr: string, newStr: string): DiffGroup[] {
    const changes = getDiff(oldStr, newStr);
    const groups: DiffGroup[] = [];

    let currentType = changes[0]?.type;
    let currentLines: string[] = [];

    for (const change of changes) {
      if (change.type === currentType) {
        currentLines.push(change.value);
      } else {
        if (currentLines.length > 0) {
          groups.push({ type: currentType, lines: currentLines });
        }
        currentType = change.type;
        currentLines = [change.value];
      }
    }
    if (currentLines.length > 0) {
      groups.push({ type: currentType, lines: currentLines });
    }

    const processedGroups: DiffGroup[] = [];
    for (const g of groups) {
      if (g.type === 'unchanged' && g.lines.length > 10) {
        const head = g.lines.slice(0, 3);
        const tail = g.lines.slice(-3);
        const collapsedCount = g.lines.length - 6;

        processedGroups.push({ type: 'unchanged', lines: head });
        processedGroups.push({
          type: 'collapsed',
          lines: [`... (${collapsedCount} lines hidden) ...`]
        });
        processedGroups.push({ type: 'unchanged', lines: tail });
      } else {
        processedGroups.push(g);
      }
    }

    return processedGroups;
  }

  function downloadFile() {
    if (!selectedFile || !editorView) return;
    const content = editorView.state.doc.toString();
    const blob = new Blob([content], { type: 'text/plain;charset=utf-8' });
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = selectedFile.split('/').pop() || 'config';
    document.body.appendChild(a);
    a.click();
    document.body.removeChild(a);
    URL.revokeObjectURL(url);
  }

  async function checkBeforeSave() {
    if (!selectedFile || !editorView) return;
    const content = editorView.state.doc.toString();

    diffChanges = getDiff(originalContent, content);

    if (diffChanges.filter((c) => c.type !== 'unchanged').length === 0) {
      showToast('info', $t('editor.no_changes') || 'No changes to save');
      return;
    }

    showSaveConfirmModal = true;
    validationLoading = true;
    validationResult = null;

    try {
      const csrfToken = localStorage.getItem('csrf_token');
      const res = await fetch('/api/config/validate', {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          'X-CSRF-Token': csrfToken || ''
        },
        body: JSON.stringify({
          path: selectedFile,
          content: content
        })
      });
      if (res.ok) {
        validationResult = await res.json();
      } else {
        const text = await res.text();
        validationResult = { valid: false, error: text || 'Validation endpoint failed' };
      }
    } catch (e: any) {
      validationResult = { valid: false, error: e.message };
    } finally {
      validationLoading = false;
    }
  }

  async function confirmSave() {
    if (!selectedFile || !editorView) return;

    saving = true;
    showSaveConfirmModal = false;

    try {
      const content = editorView.state.doc.toString();

      const csrfToken = localStorage.getItem('csrf_token');
      const res = await fetch(`/api/config/save?path=${encodeURIComponent(selectedFile)}`, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          'X-CSRF-Token': csrfToken || ''
        },
        body: content
      });

      if (!res.ok) throw new Error('Failed to save file');

      showToast('success', $t('editor.file_saved'));
      originalContent = content;
      isDirty = false;
      localStorage.removeItem(`editor.draft.${selectedFile}`);
      hasDraft = false;
      draftContent = '';
      await loadBackups(selectedFile);
    } catch (e: any) {
      showToast('error', $t('editor.save_error') + ': ' + e.message);
    } finally {
      saving = false;
    }
  }

  async function restoreBackup(backupPath: string) {
    if (!confirm($t('editor.restore_confirm'))) return;

    try {
      const res = await fetch(`/api/config/read?path=${encodeURIComponent(backupPath)}`);
      if (!res.ok) throw new Error('Failed to load backup');

      const content = await res.text();

      if (editorView) {
        editorView.dispatch({
          changes: {
            from: 0,
            to: editorView.state.doc.length,
            insert: content
          }
        });
      }

      showToast('success', $t('editor.backup_restored'));
    } catch (e) {
      showToast('error', $t('editor.restore_error') + ': ' + e.message);
    }
  }

  async function createFile() {
    if (!newFileName) return;

    const csrfToken = localStorage.getItem('csrf_token');
    const path = selectedFile
      ? selectedFile.substring(0, selectedFile.lastIndexOf('/') + 1) + newFileName
      : '/opt/etc/xray/configs/' + newFileName;

    try {
      const res = await fetch(`/api/config/create?path=${encodeURIComponent(path)}`, {
        method: 'POST',
        headers: { 'X-CSRF-Token': csrfToken || '' }
      });

      if (!res.ok) throw new Error(await res.text());

      showToast('success', $t('editor.create_file'));
      showCreateModal = false;
      newFileName = '';
      await loadFiles();
      await loadFile(path);
    } catch (e) {
      showToast('error', $t('editor.create_error') + ': ' + e.message);
    }
  }

  async function deleteFile() {
    if (!selectedFile) return;
    if (!confirm($t('app.delete') + ' ' + selectedFile.split('/').pop() + '?')) return;

    const csrfToken = localStorage.getItem('csrf_token');

    try {
      const res = await fetch(`/api/config/delete?path=${encodeURIComponent(selectedFile)}`, {
        method: 'POST',
        headers: { 'X-CSRF-Token': csrfToken || '' }
      });

      if (!res.ok) throw new Error(await res.text());

      showToast('success', $t('app.delete'));
      selectedFile = '';
      backups = [];
      await loadFiles();
    } catch (e) {
      showToast('error', $t('editor.delete_error') + ': ' + e.message);
    }
  }

  async function renameFile() {
    if (!renameTarget || !selectedFile) return;

    const csrfToken = localStorage.getItem('csrf_token');
    const newPath = selectedFile.substring(0, selectedFile.lastIndexOf('/') + 1) + renameTarget;

    try {
      const res = await fetch(
        `/api/config/rename?old=${encodeURIComponent(selectedFile)}&new=${encodeURIComponent(newPath)}`,
        {
          method: 'POST',
          headers: { 'X-CSRF-Token': csrfToken || '' }
        }
      );

      if (!res.ok) throw new Error(await res.text());

      showToast('success', $t('app.rename'));
      showRenameModal = false;
      renameTarget = '';
      await loadFiles();
      await loadFile(newPath);
    } catch (e) {
      showToast('error', $t('editor.rename_error') + ': ' + e.message);
    }
  }

  function toggleSchema() {
    schemaEnabled = !schemaEnabled;
    if (editorView && selectedFile) {
      const newExts = getSchemaExtensions(selectedFile, expertMode);
      editorView.dispatch({
        effects: schemaCompartment.reconfigure(newExts)
      });
    }
  }

  function toggleExpertMode() {
    expertMode = !expertMode;
    if (editorView && selectedFile) {
      const newExts = getSchemaExtensions(selectedFile, expertMode);
      editorView.dispatch({
        effects: schemaCompartment.reconfigure(newExts)
      });
    }
  }

  function applyQuickFixes() {
    if (!editorView || !selectedFile) return;

    const content = editorView.state.doc.toString();
    const isYaml = selectedFile.endsWith('.yaml') || selectedFile.endsWith('.yml');
    const isXray = selectedFile.includes('xray');
    const isMihomo = selectedFile.includes('mihomo') || selectedFile.includes('config.yaml');

    let fixed = content;
    let fixesApplied = 0;

    try {
      if (isYaml) {
        // Simple YAML fixes
        if (isMihomo) {
          if (!fixed.includes('proxies:') && !fixed.includes('proxy-providers:')) {
            fixed = 'proxies:\n' + fixed;
            fixesApplied++;
          }
          if (!fixed.includes('proxy-groups:')) {
            fixed =
              fixed +
              '\nproxy-groups:\n  - name: Выбор прокси\n    type: select\n    proxies:\n      - DIRECT\n';
            fixesApplied++;
          }
        }
      } else {
        // JSON fixes
        const data = JSON.parse(fixed);
        if (isXray) {
          if (!data.inbounds) {
            data.inbounds = [];
            fixesApplied++;
          }
          if (!data.outbounds) {
            data.outbounds = [{ protocol: 'freedom', tag: 'direct' }];
            fixesApplied++;
          }
          if (!data.routing) {
            data.routing = { rules: [] };
            fixesApplied++;
          }
        }
        fixed = JSON.stringify(data, null, 2);
      }

      if (fixesApplied > 0) {
        editorView.dispatch({
          changes: { from: 0, to: editorView.state.doc.length, insert: fixed }
        });
        showToast('success', `Quick fixes applied: ${fixesApplied}`);
      } else {
        showToast('info', 'No quick fixes needed');
      }
    } catch (e) {
      showToast('error', 'Quick fix error: ' + e.message);
    }
  }

  async function loadTemplates() {
    try {
      const res = await fetch('/api/templates/list');
      if (res.ok) {
        const data = await res.json();
        templates = Array.isArray(data) ? data : [];
      } else {
        templates = [];
      }
    } catch (e) {
      templates = [];
    }
  }

  async function applyTemplate(template: Template) {
    if (!editorView) return;
    if (isDirty && !confirmUnsaved()) return;
    if (
      !confirm(
        $t('editor.confirm_template') ||
          'Apply this template? Current unsaved changes will be lost.'
      )
    )
      return;

    const backupContent = editorView.state.doc.toString();
    loading = true;
    try {
      const res = await fetch(`/api/templates/fetch?name=${encodeURIComponent(template.name)}`);
      if (!res.ok) throw new Error((await res.text()) || 'Failed to fetch template');
      const data = await res.json();

      if (!data.content) throw new Error('Template is empty');

      editorView.dispatch({
        changes: { from: 0, to: editorView.state.doc.length, insert: data.content }
      });
      isDirty = true;
      showTemplatesModal = false;
      showToast('success', $t('editor.template_applied') || 'Template applied successfully');
    } catch (e: any) {
      showToast(
        'error',
        ($t('editor.template_error') || 'Failed to apply template') + ': ' + e.message
      );
      if (editorView) {
        editorView.dispatch({
          changes: { from: 0, to: editorView.state.doc.length, insert: backupContent }
        });
      }
    } finally {
      loading = false;
    }
  }

  function generateOutbound() {
    if (!editorView) return;

    let config: any = {};
    if (genProtocol === 'vless') {
      config = {
        protocol: 'vless',
        settings: {
          vnext: [
            {
              address: genAddress,
              port: genPort,
              users: [{ id: genUUID, encryption: 'none', flow: genFlow }]
            }
          ]
        },
        streamSettings: {
          network: 'tcp',
          security: genSecurity,
          realitySettings:
            genSecurity === 'reality'
              ? {
                  show: false,
                  dest: genSpiderDomain + ':443',
                  xver: 0,
                  serverNames: [genSNI],
                  privateKey: '', // User must fill
                  shortIds: [genShortId]
                }
              : undefined
        }
      };
    } else if (genProtocol === 'shadowsocks') {
      config = {
        protocol: 'shadowsocks',
        settings: {
          servers: [
            {
              address: genAddress,
              port: genPort,
              method: '256-gcm',
              password: genUUID
            }
          ]
        }
      };
    }

    const content = JSON.stringify(config, null, 2);
    const cursor = editorView.state.selection.main.head;
    editorView.dispatch({
      changes: { from: cursor, insert: content }
    });
    showGeneratorModal = false;
  }

  // Reactive file info
  $: fileSize = formatBytes(originalContent ? new Blob([originalContent]).size : 0);
  $: fileType = selectedFile
    ? selectedFile.endsWith('.yaml') || selectedFile.endsWith('.yml')
      ? 'YAML'
      : 'JSON'
    : '';
  $: fileLineEndings = originalContent?.includes('\r\n') ? 'CRLF' : 'LF';

  function formatBytes(bytes: number): string {
    if (bytes === 0) return '0 B';
    const k = 1024;
    const sizes = ['B', 'KB', 'MB'];
    const i = Math.floor(Math.log(bytes) / Math.log(k));
    return parseFloat((bytes / Math.pow(k, i)).toFixed(1)) + ' ' + sizes[i];
  }

  onMount(() => {
    loadFiles();
    loadTemplates();
  });

  onDestroy(() => {
    if (editorView) {
      editorView.destroy();
    }
  });
</script>

<div class="container">
  <div class="page-head">
    <div>
      <div class="crumbs">
        {$t('nav.group_core')} <span style="color:var(--fg-faint);margin:0 6px;">/</span>
        {$t('nav.editor')}
      </div>
      <h1>{$t('editor.h1')}</h1>
      <p class="sub">{$t('editor.h1_sub')}</p>
    </div>
    <div class="ph-actions">
      <button
        class="btn btn-primary"
        on:click={() => {
          showCreateModal = true;
          newFileName = '';
        }}
        title={$t('editor.create_file')}
      >
        <svg
          width="14"
          height="14"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          stroke-width="2.5"
          stroke-linecap="round"
          ><line x1="12" y1="5" x2="12" y2="19" /><line x1="5" y1="12" x2="19" y2="12" /></svg
        >
        {$t('editor.new_file')}
      </button>
      {#if selectedFile}
        <button
          class="btn btn-secondary"
          on:click={() => loadFile(selectedFile)}
          disabled={loading}
          title={$t('editor.reload')}
        >
          <svg
            width="14"
            height="14"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            stroke-width="2"
            stroke-linecap="round"
            stroke-linejoin="round"
            ><path d="M21 12a9 9 0 1 1-3-6.7L21 8" /><path d="M21 3v5h-5" /></svg
          >
          {$t('editor.reload')}
        </button>
        <button
          class="btn btn-secondary"
          on:click={checkBeforeSave}
          disabled={saving}
          title={$t('app.save')}
        >
          <svg
            width="14"
            height="14"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            stroke-width="2"
            stroke-linecap="round"
            stroke-linejoin="round"
            ><path d="M19 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h11l5 5v11a2 2 0 0 1-2 2Z" /><polyline
              points="17 21 17 13 7 13 7 21"
            /><polyline points="7 3 7 8 15 8" /></svg
          >
          {saving ? $t('app.loading') : $t('app.save')}
        </button>
      {/if}
    </div>
  </div>

  <div class="editor-grid" style={showSidebar ? '' : 'grid-template-columns: 1fr;'}>
    {#if showSidebar}
      <div>
        <!-- File Search -->
      <div style="margin-bottom: 12px; position: relative;">
        <input
          type="text"
          class="input"
          style="width: 100%; padding: 8px 12px; font-size: 13px;"
          placeholder={$t('app.search') || 'Поиск файлов...'}
          bind:value={fileSearchQuery}
        />
        {#if fileSearchQuery}
          <button
            on:click={() => fileSearchQuery = ''}
            style="position: absolute; right: 10px; top: 50%; transform: translateY(-50%); background: none; border: none; color: var(--fg-dim); cursor: pointer; font-size: 16px; padding: 0 4px;"
            title="Очистить"
          >
            ×
          </button>
        {/if}
      </div>

      <!-- Xray Section -->
      <div class="editor-files" style="margin-bottom:12px;">
        <div
          class="editor-files-head"
          style="display:flex;align-items:center;justify-content:space-between;"
        >
          <span>Xray</span>
          <span
            style="color:var(--accent);font-family:var(--font-family-mono);text-transform:none;letter-spacing:0;font-weight:500;font-size:11px;"
            >{xrayDir}</span
          >
        </div>
        <div class="file-list">
          {#each filteredXrayFiles as file}
            <button
              class="file-row"
              class:active={file.path === selectedFile}
              on:click={() => loadFile(file.path)}
            >
              <span class="fr-name">{file.name}</span>
              <span class="fr-meta">{formatBytes(file.size)}</span>
            </button>
          {:else}
            <span
              class="sb-empty"
              style="padding:10px 14px;display:block;color:var(--fg-faint);font-size:12px;">—</span
            >
          {/each}
        </div>
      </div>

      <!-- Mihomo Section -->
      <div class="editor-files">
        <div
          class="editor-files-head"
          style="display:flex;align-items:center;justify-content:space-between;"
        >
          <span>Mihomo</span>
          <span
            style="color:var(--accent);font-family:var(--font-family-mono);text-transform:none;letter-spacing:0;font-weight:500;font-size:11px;"
            >{mihomoDir}</span
          >
        </div>
        <div class="file-list">
          {#each filteredMihomoFiles as file}
            <button
              class="file-row"
              class:active={file.path === selectedFile}
              on:click={() => loadFile(file.path)}
            >
              <span class="fr-name">{file.name}</span>
              <span class="fr-meta">{formatBytes(file.size)}</span>
            </button>
          {:else}
            <span
              class="sb-empty"
              style="padding:10px 14px;display:block;color:var(--fg-faint);font-size:12px;">—</span
            >
          {/each}
        </div>
      </div>

      <!-- Backups Section -->
      {#if backups.length > 0}
        <div class="editor-files" style="margin-top:12px;">
          <div class="editor-files-head">
            <span>{$t('editor.backups')}</span>
          </div>
          <div class="file-list">
            {#each backups as backup}
              <button class="file-row" on:click={() => restoreBackup(backup)}>
                <span class="fr-name">{backup.split('.backup-')[1] || backup}</span>
              </button>
            {/each}
          </div>
        </div>
      {/if}
    </div>
    {/if}

    <!-- Main Editor Card -->
    <div class="editor-main-card">
      <div class="editor-toolbar">
        <button
          class="btn btn-secondary"
          style="padding: 6px 10px; margin-right: 8px;"
          on:click={() => showSidebar = !showSidebar}
          title={showSidebar ? "Скрыть сайдбар" : "Показать сайдбар"}
          aria-label={showSidebar ? "Скрыть сайдбар" : "Показать сайдбар"}
        >
          {#if showSidebar}
            <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="15 18 9 12 15 6"/></svg>
          {:else}
            <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="9 18 15 12 9 6"/></svg>
          {/if}
        </button>
        <span class="file-name"
          >{selectedFile ? selectedFile.split('/').pop() : $t('editor.select_file')}</span
        >
        {#if selectedFile}
          <span class="file-meta" style="margin-left:8px;"
            >{fileSize} · {fileType} · UTF‑8 · {fileLineEndings}</span
          >
        {/if}

        {#if hasDraft}
          <div
            class="editor-draft-bar"
            style="margin-left: 12px; display: inline-flex; align-items: center; gap: 6px;"
          >
            <span>{$t('editor.has_draft') || 'Есть черновик'}</span>
            <button on:click={restoreDraft} class="btn btn-sm btn-warning">
              {$t('editor.restore_draft') || 'Восстановить'}
            </button>
            <button
              on:click={discardDraft}
              class="btn btn-sm btn-secondary"
              style="padding: 2px 8px;"
            >
              {$t('editor.discard_draft') || 'Сбросить'}
            </button>
          </div>
        {/if}

        <span style="margin-left:auto;display:flex;gap:8px;align-items:center;">
          {#if selectedFile}
            <label
              class="toggle-label"
              for="schema-toggle"
              title="Enable schema validation, autocomplete and hover tooltips"
            >
              <label class="toggle-switch">
                <input
                  id="schema-toggle"
                  type="checkbox"
                  bind:checked={schemaEnabled}
                  on:change={toggleSchema}
                />
                <span class="toggle-slider"></span>
              </label>
              {$t('editor.schema')}
            </label>

            <label
              class="toggle-label"
              for="expert-toggle"
              title="Expert mode: full schema assist / Beginner: simplified"
            >
              <label class="toggle-switch">
                <input
                  id="expert-toggle"
                  type="checkbox"
                  bind:checked={expertMode}
                  on:change={toggleExpertMode}
                />
                <span class="toggle-slider"></span>
              </label>
              {$t('editor.expert')}
            </label>

            <!-- Kebab actions -->
            <div class="kebab-wrap">
              <button
                class="btn btn-secondary"
                style="padding:6px 10px;"
                on:click={toggleKebab}
                title="Дополнительные действия"
                aria-label="Дополнительные действия"
              >
                <svg width="13" height="13" viewBox="0 0 24 24" fill="currentColor">
                  <circle cx="12" cy="5" r="2" /><circle cx="12" cy="12" r="2" /><circle
                    cx="12"
                    cy="19"
                    r="2"
                  />
                </svg>
              </button>
              {#if showKebabMenu}
                <!-- svelte-ignore a11y-click-events-have-key-events -->
                <!-- svelte-ignore a11y-no-static-element-interactions -->
                <div
                  class="kebab-dropdown"
                  style="right:0;top:calc(100% + 4px);"
                  on:click|stopPropagation
                >
                  <button
                    class="kebab-item"
                    on:click={() => {
                      showKebabMenu = false;
                      applyQuickFixes();
                    }}
                  >
                    <Icon name="settings" size={14} />
                    {$t('editor.quick_fix')}
                  </button>
                  <button
                    class="kebab-item"
                    on:click={() => {
                      showKebabMenu = false;
                      showTemplatesModal = true;
                      loadTemplates();
                    }}
                  >
                    <Icon name="editor" size={14} />
                    {$t('editor.templates')}
                  </button>
                  <button
                    class="kebab-item"
                    on:click={() => {
                      showKebabMenu = false;
                      showGeneratorModal = true;
                    }}
                  >
                    <Icon name="add" size={14} />
                    {$t('editor.generator')}
                  </button>
                  <button
                    class="kebab-item"
                    on:click={() => {
                      showKebabMenu = false;
                      showRenameModal = true;
                      renameTarget = selectedFile.split('/').pop() || '';
                    }}
                  >
                    {$t('app.rename')}
                  </button>
                  <div class="kebab-divider"></div>
                  <button
                    class="kebab-item danger"
                    on:click={() => {
                      showKebabMenu = false;
                      deleteFile();
                    }}
                  >
                    <Icon name="trash" size={14} />
                    {$t('app.delete')}
                  </button>
                </div>
              {/if}
            </div>

            <button
              on:click={downloadFile}
              class="btn btn-secondary"
              style="padding: 6px 10px;"
              title="Скачать файл"
              aria-label="Скачать файл"
            >
              <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                <path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4M7 10l5 5 5-5M12 15V3"/>
              </svg>
            </button>
          {/if}
        </span>
      </div>

      {#if loading}
        <div class="loading" style="min-height: 420px; display: grid; place-items: center;">
          <div class="spinner"></div>
        </div>
      {:else if !selectedFile}
        <div
          class="empty-state"
          style="min-height: 420px; display: grid; place-items: center; color: var(--fg-dim);"
        >
          <p>{$t('editor.select_file')}</p>
        </div>
      {:else}
        <div class="editor-pane" style="display: block; padding: 0;">
          <div class="editor-container" bind:this={editorContainer} style="height: 520px;"></div>
        </div>
      {/if}

      <!-- Status Bar -->
      {#if selectedFile}
        <div
          style="padding:8px 14px;border-top:1px solid var(--border);display:flex;gap:14px;font-family:var(--font-family-mono);font-size:11px;color:var(--fg-dim);"
        >
          <span class:status-dirty={isDirty}>
            <span style="color: {isDirty ? 'var(--warning)' : 'var(--success)'};">●</span>
            {isDirty ? $t('editor.unsaved') || 'Изменён' : $t('editor.saved') || 'Сохранён'}
          </span>
          <span>schema: {selectedFile.includes('xray') ? 'xray@latest' : 'mihomo@latest'}</span>
          <span>{cursorLine}:{cursorCol}</span>
          <span style="margin-left:auto;">Ctrl+S — сохранить · Ctrl+Z — отменить</span>
        </div>
      {/if}
    </div>
  </div>
</div>

<!-- Modals with Hopper styles -->
{#if showCreateModal}
  <div
    class="confirm-modal-backdrop"
    role="button"
    tabindex="0"
    on:click={() => (showCreateModal = false)}
    on:keydown={(e) => e.key === 'Escape' && (showCreateModal = false)}
  >
    <div
      class="confirm-modal"
      role="presentation"
      on:click|stopPropagation
      on:keydown|stopPropagation
    >
      <h3 style="color: var(--fg-primary); font-size: 16px; font-weight: 700; margin-bottom: 12px;">
        {$t('editor.create_file')}
      </h3>
      <label for="new-file-name" class="sr-only">{$t('editor.file_name')}</label>
      <input
        id="new-file-name"
        type="text"
        bind:value={newFileName}
        placeholder={$t('editor.file_name')}
        class="input"
        style="margin-bottom: 16px;"
        on:keydown={(e) => e.key === 'Enter' && createFile()}
      />
      <div class="confirm-modal-actions">
        <button on:click={() => (showCreateModal = false)} class="btn btn-secondary">
          {$t('app.cancel')}
        </button>
        <button on:click={createFile} class="btn btn-primary">
          {$t('app.create')}
        </button>
      </div>
    </div>
  </div>
{/if}

{#if showRenameModal}
  <div
    class="confirm-modal-backdrop"
    role="button"
    tabindex="0"
    on:click={() => (showRenameModal = false)}
    on:keydown={(e) => e.key === 'Escape' && (showRenameModal = false)}
  >
    <div
      class="confirm-modal"
      role="presentation"
      on:click|stopPropagation
      on:keydown|stopPropagation
    >
      <h3 style="color: var(--fg-primary); font-size: 16px; font-weight: 700; margin-bottom: 12px;">
        {$t('editor.rename_file')}
      </h3>
      <label for="rename-target" class="sr-only">{$t('editor.new_name')}</label>
      <input
        id="rename-target"
        type="text"
        bind:value={renameTarget}
        placeholder={$t('editor.new_name')}
        class="input"
        style="margin-bottom: 16px;"
        on:keydown={(e) => e.key === 'Enter' && renameFile()}
      />
      <div class="confirm-modal-actions">
        <button on:click={() => (showRenameModal = false)} class="btn btn-secondary">
          {$t('app.cancel')}
        </button>
        <button on:click={renameFile} class="btn btn-primary">
          {$t('app.rename')}
        </button>
      </div>
    </div>
  </div>
{/if}

{#if showTemplatesModal}
  <div
    class="confirm-modal-backdrop"
    role="button"
    tabindex="0"
    on:click={() => (showTemplatesModal = false)}
    on:keydown={(e) => e.key === 'Escape' && (showTemplatesModal = false)}
  >
    <div
      class="confirm-modal"
      style="max-width: 600px;"
      role="presentation"
      on:click|stopPropagation
      on:keydown|stopPropagation
    >
      <div class="modal-header">
        <h3 style="color: var(--fg-primary); font-size: 16px; font-weight: 700; margin: 0;">
          {$t('editor.templates')}
        </h3>
        <button class="btn-close" on:click={() => (showTemplatesModal = false)}>
          <Icon name="cross" size={14} />
        </button>
      </div>
      <p style="margin: 8px 0 16px; color: var(--fg-dim); font-size: 13px;">
        {$t('editor.templates_desc')}
      </p>

      <div class="template-list">
        {#each templates as template}
          <button class="template-item" on:click={() => applyTemplate(template)}>
            <div class="template-info">
              <span class="template-name">{template.name}</span>
              <span class="template-desc">{template.description}</span>
            </div>
            <span class="template-type">{template.type}</span>
          </button>
        {:else}
          <p class="text-center p-3" style="color: var(--fg-dim);">{$t('editor.no_templates')}</p>
        {/each}
      </div>
    </div>
  </div>
{/if}

{#if showGeneratorModal}
  <div
    class="confirm-modal-backdrop"
    role="button"
    tabindex="0"
    on:click={() => (showGeneratorModal = false)}
    on:keydown={(e) => e.key === 'Escape' && (showGeneratorModal = false)}
  >
    <div
      class="confirm-modal"
      style="max-width: 500px;"
      role="presentation"
      on:click|stopPropagation
      on:keydown|stopPropagation
    >
      <div class="modal-header">
        <h3 style="color: var(--fg-primary); font-size: 16px; font-weight: 700; margin: 0;">
          {$t('editor.generator')}
        </h3>
        <button class="btn-close" on:click={() => (showGeneratorModal = false)}>
          <Icon name="cross" size={14} />
        </button>
      </div>

      <div class="form-group" style="margin-bottom: 12px; margin-top: 12px;">
        <label
          for="gen-protocol"
          style="display: block; font-size: 12px; color: var(--fg-dim); margin-bottom: 4px;"
          >{$t('editor.protocol')}</label
        >
        <select id="gen-protocol" bind:value={genProtocol} class="input" style="width: 100%;">
          <option value="vless">VLESS</option>
          <option value="shadowsocks">Shadowsocks</option>
        </select>
      </div>

      <div
        class="form-grid"
        style="margin-bottom: 12px; display: grid; grid-template-columns: 2fr 1fr; gap: 12px;"
      >
        <div class="form-group">
          <label
            for="gen-address"
            style="display: block; font-size: 12px; color: var(--fg-dim); margin-bottom: 4px;"
            >{$t('editor.address')}</label
          >
          <input
            id="gen-address"
            type="text"
            bind:value={genAddress}
            placeholder="example.com"
            class="input"
          />
        </div>
        <div class="form-group">
          <label
            for="gen-port"
            style="display: block; font-size: 12px; color: var(--fg-dim); margin-bottom: 4px;"
            >{$t('editor.port')}</label
          >
          <input id="gen-port" type="number" bind:value={genPort} class="input" />
        </div>
      </div>

      <div class="form-group" style="margin-bottom: 12px;">
        <label
          for="gen-uuid"
          style="display: block; font-size: 12px; color: var(--fg-dim); margin-bottom: 4px;"
          >{genProtocol === 'vless' ? 'UUID' : 'Password'}</label
        >
        <div class="input-group" style="display: flex; gap: 8px;">
          <input id="gen-uuid" type="text" bind:value={genUUID} class="input" style="flex: 1;" />
          <button
            class="btn btn-secondary"
            style="padding: 0 12px;"
            on:click={() => (genUUID = crypto.randomUUID())}
            title={$t('editor.generate_uuid')}
          >
            <Icon name="refresh" size={14} />
          </button>
        </div>
      </div>

      {#if genProtocol === 'vless'}
        <div class="form-group" style="margin-bottom: 12px;">
          <label
            for="gen-sni"
            style="display: block; font-size: 12px; color: var(--fg-dim); margin-bottom: 4px;"
            >SNI</label
          >
          <input
            id="gen-sni"
            type="text"
            bind:value={genSNI}
            placeholder="sni.example.com"
            class="input"
          />
        </div>

        <div
          class="form-grid"
          style="margin-bottom: 16px; display: grid; grid-template-columns: 1fr 1fr; gap: 12px;"
        >
          <div class="form-group">
            <label
              for="gen-security"
              style="display: block; font-size: 12px; color: var(--fg-dim); margin-bottom: 4px;"
              >Security</label
            >
            <select id="gen-security" bind:value={genSecurity} class="input" style="width: 100%;">
              <option value="reality">Reality</option>
              <option value="tls">TLS</option>
              <option value="none">None</option>
            </select>
          </div>
          {#if genSecurity === 'reality'}
            <div class="form-group">
              <label
                for="gen-shortid"
                style="display: block; font-size: 12px; color: var(--fg-dim); margin-bottom: 4px;"
                >Short ID</label
              >
              <input
                id="gen-shortid"
                type="text"
                bind:value={genShortId}
                placeholder="hex string"
                class="input"
              />
            </div>
          {/if}
        </div>
      {/if}

      <div class="confirm-modal-actions">
        <button on:click={() => (showGeneratorModal = false)} class="btn btn-secondary">
          {$t('app.cancel')}
        </button>
        <button on:click={generateOutbound} class="btn btn-primary">
          {$t('app.generate')}
        </button>
      </div>
    </div>
  </div>
{/if}

{#if showSaveConfirmModal}
  <div
    class="confirm-modal-backdrop"
    role="button"
    tabindex="0"
    on:click={() => (showSaveConfirmModal = false)}
    on:keydown={(e) => e.key === 'Escape' && (showSaveConfirmModal = false)}
  >
    <div
      class="confirm-modal"
      style="max-width: 700px; width: 90%; display: flex; flex-direction: column; max-height: 85vh;"
      role="presentation"
      on:click|stopPropagation
      on:keydown|stopPropagation
    >
      <div class="modal-header">
        <h3 style="color: var(--fg-primary); font-size: 16px; font-weight: 700; margin: 0;">
          {$t('editor.confirm_save_title') || 'Confirm Save'}
        </h3>
        <button
          class="btn-close"
          on:click={() => (showSaveConfirmModal = false)}
          aria-label="Close modal"
        >
          <Icon name="cross" size={14} />
        </button>
      </div>

      <!-- Validation status -->
      {#if validationLoading}
        <div class="validation-result validation-loading" style="margin-top: 12px;">
          <svg
            width="16"
            height="16"
            viewBox="0 0 38 38"
            stroke="var(--accent)"
            style="flex-shrink:0"
          >
            <g fill="none" fill-rule="evenodd">
              <g transform="translate(1 1)" stroke-width="2">
                <circle stroke-opacity=".5" cx="18" cy="18" r="18" />
                <path d="M36 18c0-9.94-8.06-18-18-18">
                  <animateTransform
                    attributeName="transform"
                    type="rotate"
                    from="0 18 18"
                    to="360 18 18"
                    dur="1s"
                    repeatCount="indefinite"
                  />
                </path>
              </g>
            </g>
          </svg>
          <span>{$t('editor.validating') || 'Проверка конфигурации...'}</span>
        </div>
      {:else if validationResult}
        {#if validationResult.valid}
          <div class="validation-result validation-ok" style="margin-top: 12px;">
            <span class="v-icon">✓</span>
            <span>{$t('editor.validation_valid') || 'Конфигурация корректна.'}</span>
          </div>
        {:else}
          <div class="validation-result validation-err" style="margin-top: 12px;">
            <div class="validation-err-head">
              <span class="v-icon">✗</span>
              <span>{$t('editor.validation_invalid') || 'Конфигурация содержит ошибки:'}</span>
            </div>
            <pre>{validationResult.error}</pre>
          </div>
        {/if}
      {/if}

      <!-- Diff Preview -->
      <div class="diff-preview" style="margin-top: 12px;">
        <div class="diff-preview-title">
          {$t('editor.diff_preview') || 'Предпросмотр изменений'}
        </div>
        <div class="diff-preview-body">
          {#each getDiffGroups(originalContent, editorView ? editorView.state.doc.toString() : '') as group}
            {#if group.type === 'added'}
              {#each group.lines as line}
                <div class="diff-line diff-line-added">+ {line}</div>
              {/each}
            {:else if group.type === 'removed'}
              {#each group.lines as line}
                <div class="diff-line diff-line-removed">- {line}</div>
              {/each}
            {:else if group.type === 'collapsed'}
              <div class="diff-line diff-line-collapsed">{group.lines[0]}</div>
            {:else}
              {#each group.lines as line}
                <div class="diff-line diff-line-unchanged">{line}</div>
              {/each}
            {/if}
          {/each}
        </div>
      </div>

      <div class="confirm-modal-actions" style="margin-top: 16px;">
        <button on:click={() => (showSaveConfirmModal = false)} class="btn btn-secondary">
          {$t('app.cancel')}
        </button>
        <button
          on:click={confirmSave}
          class="btn btn-primary"
          disabled={saving || (validationResult && !validationResult.valid && !expertMode)}
        >
          {saving ? $t('app.loading') : $t('app.save')}
        </button>
      </div>
    </div>
  </div>
{/if}

<style>
  /* hot-fix layout, требует визуального ревью Claude Design */
  .editor-grid {
    display: grid;
    grid-template-columns: 260px 1fr;
    gap: 14px;
    align-items: start;
  }
  .editor-files {
    background: var(--bg-card);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    overflow: hidden;
  }
  .editor-files-head {
    padding: 12px 14px;
    border-bottom: 1px solid var(--border);
    font-size: 11px;
    letter-spacing: .18em;
    text-transform: uppercase;
    color: var(--fg-dim);
    font-weight: 700;
  }
  .editor-main-card {
    background: var(--bg-card);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    overflow: hidden;
    display: flex;
    flex-direction: column;
  }
  .editor-toolbar {
    padding: 10px 14px;
    border-bottom: 1px solid var(--border);
    display: flex;
    align-items: center;
    gap: 10px;
  }
  .editor-toolbar .file-name {
    font-family: var(--font-family-mono);
    font-size: 13px;
    color: var(--fg-primary);
    font-weight: 600;
  }
  .editor-toolbar .file-meta {
    font-family: var(--font-family-mono);
    font-size: 11px;
    color: var(--fg-dim);
  }

  :global(.cm-editor) {
    height: 100% !important;
    font-size: 13.5px;
    background: #050d16 !important;
  }
  :global(.cm-scroller) {
    overflow: auto !important;
  }
  :global(.cm-gutter) {
    background: #050d16 !important;
    border-right: 1px solid #0e2034 !important;
    color: var(--fg-faint) !important;
  }
  :global(.cm-content) {
    font-family: var(--font-family-mono) !important;
  }

  .template-list {
    display: flex;
    flex-direction: column;
    gap: 8px;
    max-height: 360px;
    overflow-y: auto;
  }

  .template-item {
    display: flex;
    justify-content: space-between;
    align-items: center;
    padding: 12px;
    background: var(--bg-deep);
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
    background: var(--hover);
  }

  .template-info {
    display: flex;
    flex-direction: column;
    gap: 4px;
  }

  .template-name {
    font-weight: 600;
    font-size: 13px;
    color: var(--fg-primary);
  }

  .template-desc {
    font-size: 11.5px;
    color: var(--fg-dim);
  }

  .template-type {
    font-size: 10px;
    text-transform: uppercase;
    background: var(--bg-card);
    padding: 2px 6px;
    border-radius: 4px;
    border: 1px solid var(--border);
    color: var(--fg-dim);
    font-family: var(--font-family-mono);
  }

  .modal-header {
    display: flex;
    justify-content: space-between;
    align-items: center;
  }

  .btn-close {
    background: none;
    border: none;
    cursor: pointer;
    color: var(--fg-dim);
    display: flex;
    align-items: center;
    padding: 4px;
    border-radius: 4px;
  }
  .btn-close:hover {
    background: var(--hover);
    color: var(--fg-primary);
  }

  /* status bar */
  .status-dirty {
    color: var(--warning) !important;
  }

  /* kebab menu */
  .kebab-wrap {
    position: relative;
    display: inline-block;
  }

  .kebab-dropdown {
    position: absolute;
    right: 0;
    top: calc(100% + 4px);
    background: var(--bg-card);
    border: 1px solid var(--border);
    border-radius: 8px;
    box-shadow: 0 10px 30px rgba(0, 0, 0, 0.5);
    min-width: 180px;
    z-index: 100;
    overflow: hidden;
    padding: 4px;
  }

  .kebab-item {
    display: flex;
    align-items: center;
    gap: 8px;
    width: 100%;
    padding: 8px 12px;
    background: transparent;
    border: none;
    border-radius: 6px;
    cursor: pointer;
    font-size: 12.5px;
    color: var(--fg-primary);
    text-align: left;
    transition: background 0.15s;
  }

  .kebab-item:hover {
    background: var(--hover);
  }

  .kebab-item.danger {
    color: var(--danger);
  }

  .kebab-divider {
    height: 1px;
    background: var(--border);
    margin: 4px 0;
  }

  /* validation / diff */
  .validation-result {
    display: flex;
    align-items: flex-start;
    gap: 8px;
    padding: 10px 14px;
    border-radius: 6px;
    font-size: 12.5px;
  }

  .validation-loading {
    background: rgba(41, 194, 240, 0.08);
    color: var(--accent);
    border: 1px solid rgba(41, 194, 240, 0.2);
  }

  .validation-ok {
    background: rgba(16, 185, 129, 0.08);
    color: var(--success);
    border: 1px solid rgba(16, 185, 129, 0.2);
  }

  .v-icon {
    font-weight: 700;
    flex-shrink: 0;
  }

  .validation-err {
    background: rgba(239, 68, 68, 0.08);
    color: var(--danger);
    border: 1px solid rgba(239, 68, 68, 0.2);
    flex-direction: column;
    width: 100%;
  }

  .validation-err pre {
    margin: 6px 0 0;
    font-size: 11px;
    white-space: pre-wrap;
    word-break: break-all;
    color: var(--fg-dim);
    font-family: var(--font-family-mono);
    width: 100%;
  }

  .diff-preview {
    flex: 1;
    overflow: hidden;
    display: flex;
    flex-direction: column;
    min-height: 0;
  }

  .diff-preview-title {
    font-size: 11px;
    font-weight: 600;
    color: var(--fg-dim);
    text-transform: uppercase;
    letter-spacing: 0.08em;
    margin-bottom: 6px;
    flex-shrink: 0;
  }

  .diff-preview-body {
    flex: 1;
    overflow-y: auto;
    background: #050d16;
    border: 1px solid var(--border);
    border-radius: 6px;
    padding: 10px;
    font-family: var(--font-family-mono);
    font-size: 12px;
    line-height: 1.5;
  }

  .diff-line {
    white-space: pre-wrap;
    word-break: break-all;
  }

  .diff-line-added {
    color: #a3e9b6;
    background: rgba(163, 233, 182, 0.04);
  }

  .diff-line-removed {
    color: #f87171;
    background: rgba(248, 113, 113, 0.04);
  }

  .diff-line-collapsed {
    color: var(--fg-faint);
    font-style: italic;
  }

  .diff-line-unchanged {
    color: var(--fg-secondary);
  }

  .spinner {
    width: 24px;
    height: 24px;
    border: 2px solid var(--border);
    border-top-color: var(--accent);
    border-radius: 50%;
    animation: spin 0.8s linear infinite;
  }

  @keyframes spin {
    to {
      transform: rotate(360deg);
    }
  }
</style>
