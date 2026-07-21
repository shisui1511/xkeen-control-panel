<script lang="ts">
  import { onMount, onDestroy, tick } from 'svelte';
  import { fade, slide } from 'svelte/transition';
  import { t, currentLang } from './i18n';
  import { showToast, capabilities } from './stores';
  import { parseValidationError } from './lib/errorParser';
  import Icon from './lib/components/Icon.svelte';
  import EmptyState from './components/EmptyState.svelte';
  import EditorIcon from './lib/components/icons/Editor.svelte';
  import { EditorState } from '@codemirror/state';
  import { EditorView } from '@codemirror/view';

  import Constructor from './Constructor.svelte';
  import { buildPathAtCursor, type PathSegment } from './lib/editor-utils';

  // Subcomponents
  import FileTree from './components/editor/FileTree.svelte';
  import EditorTabs from './components/editor/EditorTabs.svelte';
  import CodeMirrorEditor from './components/editor/CodeMirrorEditor.svelte';
  import BackupSidebar from './components/editor/BackupSidebar.svelte';
  import Modal from './components/Modal.svelte';

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

  interface EditorTab {
    path: string;
    name: string;
    isDirty: boolean;
    isPreview: boolean;
    scrollState?: { top: number; left: number };
    cursorPos?: number;
    originalContent: string;
    currentContent: string;
  }

  let { onSwitchTab = () => {} }: { onSwitchTab?: (tab: string) => void } = $props();

  let ru = $derived($currentLang === 'ru');

  let editorView = $state<EditorView | null>(null);

  // States using runes
  let files = $state<ConfigFileInfo[]>([]);
  let selectedFile = $state('');
  let loading = $state(false);
  let loadingPath = $state<string | null>(null);
  let templateLoading = $state(false);
  const pendingPins = new Set<string>();
  let saving = $state(false);
  let backups = $state<string[]>([]);
  let tabs = $state<EditorTab[]>([]);
  let activeTabPath = $state('');
  let breadcrumbs = $state<PathSegment[]>([]);
  let applyLoading = $state(false);
  let backgroundStatusText = $state('');

  // Drawer states
  let drawerOpen = $state(false);
  let selectedBackup = $state('');
  let diffGroups = $state<any[]>([]);
  let backupLoading = $state(false);

  // Directory management
  const xrayDir = '/opt/etc/xray/configs';
  const mihomoDir = '/opt/etc/mihomo';
  let currentDir = $state(xrayDir);

  // Dual-panel sidebar file lists
  let xrayFiles = $state<ConfigFileInfo[]>([]);
  let mihomoFiles = $state<ConfigFileInfo[]>([]);
  let showSidebar = $state(true);

  // Status bar cursor position
  let cursorLine = $state(1);
  let cursorCol = $state(1);

  // Schema assist mode
  let schemaEnabled = $state(true);
  let expertMode = $state(false);

  // CRUD modals
  let showCreateModal = $state(false);
  let showRenameModal = $state(false);
  let showTemplatesModal = $state(false);
  let newFileName = $state('');
  let renameTarget = $state('');
  let templates = $state<Template[]>([]);
  let templateTab = $state<'xray' | 'mihomo'>('xray');
  let selectedTemplate = $state<Template | null>(null);
  let templatePreview = $state('');
  let updatingTemplates = $state(false);
  let loadingPreview = $state(false);
  let templateStatus = $state<any>(null);

  let filteredTemplates = $derived(templates.filter((t) => t.type === templateTab));

  // Generator state
  let showGeneratorModal = $state(false);
  let genProtocol = $state('vless');
  let genAddress = $state('');
  let genPort = $state(443);
  let genUUID = $state(crypto.randomUUID());
  let genSNI = $state('');
  let genFlow = $state('xtls-rprx-vision');
  let genSecurity = $state('reality');
  let genPublicKey = $state('');
  let genShortId = $state('');
  let genSpiderDomain = $state('');

  // Dirty state tracking
  let originalContent = $state('');
  let isDirty = $state(false);

  // Local active tab: 'files' | 'constructor'
  let activeTab = $state<'files' | 'constructor'>('files');

  function jumpToSegment(pos: number) {
    if (!editorView) return;
    editorView.focus();
    editorView.dispatch({
      selection: { anchor: pos, head: pos },
      scrollIntoView: true
    });

    const line = editorView.state.doc.lineAt(pos);
    const lineEl = editorView.dom.querySelector(`.cm-line:nth-child(${line.number})`);
    if (lineEl) {
      lineEl.classList.add('line-highlight-flash');
      setTimeout(() => {
        lineEl.classList.remove('line-highlight-flash');
      }, 1000);
    }
  }

  function checkHashTab() {
    if (window.location.hash === '#/constructor') {
      activeTab = 'constructor';
    } else if (window.location.hash === '#/mihomo-gen') {
      activeTab = 'constructor';
      window.location.hash = '#/constructor';
    } else {
      activeTab = 'files';
    }
  }

  function setTab(tab: 'files' | 'constructor') {
    activeTab = tab;
    if (tab === 'constructor') {
      window.location.hash = '#/constructor';
    } else {
      window.location.hash = '#/editor';
    }
  }

  async function handleInsertIntoEditor(yamlContent: string) {
    if (selectedFile) {
      if (editorView && editorView.dom.isConnected) {
        editorView.dispatch({
          changes: {
            from: 0,
            to: editorView.state.doc.length,
            insert: yamlContent
          }
        });
        isDirty = true;
        activeTab = 'files';
        window.location.hash = '#/editor';
      } else {
        activeTab = 'files';
        window.location.hash = '#/editor';
        await tick();
        if (editorView) {
          editorView.dispatch({
            changes: { from: 0, to: editorView.state.doc.length, insert: yamlContent }
          });
          isDirty = true;
        }
      }
      showToast(
        'success',
        $t('editor.yaml_inserted') || 'Конфигурация вставлена в редактор. Не забудьте сохранить её!'
      );
    } else {
      activeTab = 'files';
      window.location.hash = '#/editor';
      showToast(
        'info',
        $t('editor.select_file_warn') ||
          'Пожалуйста, выберите файл в панели слева для вставки YAML.'
      );
    }
  }

  // Draft state tracking
  let hasDraft = $state(false);
  let draftContent = $state('');

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
    const currentTab = tabs.find((t) => t.path === activeTabPath);
    return currentTab ? currentTab.isDirty : false;
  }

  function confirmUnsaved(): boolean {
    return confirm($t('editor.unsaved_warning') || 'Unsaved changes will be lost. Proceed?');
  }

  async function loadFiles(dir?: string) {
    if (dir) currentDir = dir;
    try {
      const resXray = await fetch(`/api/config/list?dir=${encodeURIComponent(xrayDir)}`);
      if (resXray.ok) {
        xrayFiles = (await resXray.ok) ? await resXray.json() : [];
      }
      const resMihomo = await fetch(`/api/config/list?dir=${encodeURIComponent(mihomoDir)}`);
      if (resMihomo.ok) {
        mihomoFiles = (await resMihomo.ok) ? await resMihomo.json() : [];
      }
    } catch (e) {
      showToast('error', $t('editor.load_error'));
    }
  }

  function switchDir(dir: string) {
    currentDir = dir;
    selectedFile = '';
    backups = [];
    originalContent = '';
    isDirty = false;
    loadFiles();
  }

  function pinTab(path: string) {
    const tab = tabs.find((t) => t.path === path);
    if (tab && tab.isPreview) {
      tab.isPreview = false;
      tabs = [...tabs];
    }
  }

  function handleGlobalKeydown(e: KeyboardEvent) {
    if (e.ctrlKey && e.key === 'Tab') {
      e.preventDefault();
      if (tabs.length <= 1) return;
      const currentIndex = tabs.findIndex((t) => t.path === activeTabPath);
      if (currentIndex === -1) return;
      let nextIndex = 0;
      if (e.shiftKey) {
        nextIndex = (currentIndex - 1 + tabs.length) % tabs.length;
      } else {
        nextIndex = (currentIndex + 1) % tabs.length;
      }
      switchTab(tabs[nextIndex].path);
    }

    if ((e.ctrlKey || e.metaKey) && e.key === 's') {
      e.preventDefault();
      if (selectedFile && activeTab === 'files') {
        checkBeforeSave();
      }
    }
  }

  async function switchTab(path: string) {
    if (activeTabPath === path) return;

    // Save current tab state before leaving
    if (activeTabPath && editorView) {
      const currentTab = tabs.find((t) => t.path === activeTabPath);
      if (currentTab) {
        currentTab.scrollState = {
          top: editorView.scrollDOM.scrollTop,
          left: editorView.scrollDOM.scrollLeft
        };
        currentTab.cursorPos = editorView.state.selection.main.head;
        currentTab.currentContent = editorView.state.doc.toString();
        currentTab.isDirty = currentTab.currentContent !== currentTab.originalContent;
      }
    }

    const targetTab = tabs.find((t) => t.path === path);
    if (!targetTab) return;

    activeTabPath = path;
    selectedFile = path;
    loading = true;

    try {
      originalContent = targetTab.originalContent;
      isDirty = targetTab.isDirty;

      // Check draft in localStorage
      const draft = localStorage.getItem(`editor.draft.${path}`);
      if (draft && draft !== targetTab.originalContent) {
        hasDraft = true;
        draftContent = draft;
      } else {
        hasDraft = false;
        draftContent = '';
      }

      loading = false;
      await tick();

      // Restore scroll and cursor position
      if (editorView) {
        if (targetTab.cursorPos !== undefined) {
          editorView.dispatch({
            selection: { anchor: targetTab.cursorPos, head: targetTab.cursorPos }
          });
        }
        if (targetTab.scrollState) {
          editorView.scrollDOM.scrollTop = targetTab.scrollState.top;
          editorView.scrollDOM.scrollLeft = targetTab.scrollState.left;
        }
      }

      await loadBackups(path);
      tabs = [...tabs];
    } catch (e: any) {
      showToast('error', $t('editor.file_load_error') + ': ' + (e?.message || e));
      loading = false;
    }
  }

  function closeTab(path: string, force = false) {
    const tabIndex = tabs.findIndex((t) => t.path === path);
    if (tabIndex === -1) return;

    const tabToClose = tabs[tabIndex];

    if (tabToClose.isDirty && !force) {
      if (activeTabPath !== path) {
        switchTab(path);
      }
      if (!confirmUnsaved()) return;
    }
    localStorage.removeItem('editor.draft.' + path);

    tabs.splice(tabIndex, 1);

    if (activeTabPath === path) {
      if (tabs.length > 0) {
        const nextActiveIndex = Math.min(tabIndex, tabs.length - 1);
        const nextTab = tabs[nextActiveIndex];
        activeTabPath = '';
        switchTab(nextTab.path);
      } else {
        activeTabPath = '';
        selectedFile = '';
        originalContent = '';
        isDirty = false;
        showSidebar = true;
      }
    }

    tabs = [...tabs];
    if (tabs.length === 0) {
      showSidebar = true;
    }
  }

  async function loadFile(path: string, isPreviewClick = true) {
    if (!path) return;

    const existingTab = tabs.find((t) => t.path === path);
    if (existingTab) {
      if (!isPreviewClick && existingTab.isPreview) {
        existingTab.isPreview = false;
        tabs = [...tabs];
      }
      await switchTab(path);
      return;
    }

    if (loading && loadingPath === path) {
      if (!isPreviewClick) {
        pendingPins.add(path);
      }
      return;
    }

    if (loading) return;

    loading = true;
    loadingPath = path;
    if (!isPreviewClick) {
      pendingPins.add(path);
    }

    try {
      const res = await fetch(`/api/config/read?path=${encodeURIComponent(path)}`);
      if (!res.ok) throw new Error('Failed to load file');

      const content = await res.text();

      // Save active tab state before leaving
      if (activeTabPath && editorView) {
        const currentTab = tabs.find((t) => t.path === activeTabPath);
        if (currentTab) {
          currentTab.scrollState = {
            top: editorView.scrollDOM.scrollTop,
            left: editorView.scrollDOM.scrollLeft
          };
          currentTab.cursorPos = editorView.state.selection.main.head;
          currentTab.currentContent = editorView.state.doc.toString();
          currentTab.isDirty = currentTab.currentContent !== currentTab.originalContent;
        }
      }

      const previewTab = tabs.find((t) => t.isPreview);
      const isPreview = isPreviewClick && !pendingPins.has(path);
      pendingPins.delete(path);

      if (isPreview) {
        if (previewTab) {
          if (previewTab.isDirty) {
            if (!confirmUnsaved()) {
              loading = false;
              loadingPath = null;
              return;
            }
            localStorage.removeItem('editor.draft.' + previewTab.path);
          }
          previewTab.path = path;
          previewTab.name = path.split('/').pop() || '';
          previewTab.originalContent = content;
          previewTab.currentContent = content;
          previewTab.isDirty = false;
          previewTab.isPreview = true;
          previewTab.scrollState = undefined;
          previewTab.cursorPos = undefined;
          activeTabPath = path;
          selectedFile = path;
        } else {
          const newTab: EditorTab = {
            path,
            name: path.split('/').pop() || '',
            originalContent: content,
            currentContent: content,
            isDirty: false,
            isPreview: true
          };
          tabs.push(newTab);
          activeTabPath = path;
          selectedFile = path;
        }
      } else {
        const newTab: EditorTab = {
          path,
          name: path.split('/').pop() || '',
          originalContent: content,
          currentContent: content,
          isDirty: false,
          isPreview: false
        };
        tabs.push(newTab);
        activeTabPath = path;
        selectedFile = path;
      }
      tabs = [...tabs];

      originalContent = content;
      isDirty = false;

      const draft = localStorage.getItem(`editor.draft.${path}`);
      if (draft && draft !== content) {
        hasDraft = true;
        draftContent = draft;
      } else {
        hasDraft = false;
        draftContent = '';
      }

      loading = false;
      loadingPath = null;
      await tick();

      // Restore scroll and cursor
      if (editorView) {
        if (previewTab && previewTab.cursorPos !== undefined) {
          editorView.dispatch({
            selection: { anchor: previewTab.cursorPos, head: previewTab.cursorPos }
          });
        }
      }

      await loadBackups(path);
      tabs = [...tabs];
    } catch (e: any) {
      console.error('loadFile error:', e);
      showToast('error', $t('editor.file_load_error') + ': ' + (e?.message || e));
      loading = false;
      loadingPath = null;
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

  async function selectBackup(backupPath: string) {
    if (selectedBackup === backupPath) return;
    selectedBackup = backupPath;
    backupLoading = true;
    diffGroups = [];
    try {
      const res = await fetch(`/api/config/read?path=${encodeURIComponent(backupPath)}`);
      if (!res.ok) throw new Error('Failed to load backup content');
      const backupContent = await res.text();
      const currentContent = editorView ? editorView.state.doc.toString() : '';
      diffGroups = getDiffGroups(backupContent, currentContent);
    } catch (e: any) {
      showToast('error', $t('editor.restore_error') + ': ' + e.message);
    } finally {
      backupLoading = false;
    }
  }

  let showSaveConfirmModal = $state(false);
  let diffChanges = $state<any[]>([]);

  // Kebab menu for destructive actions (Delete)
  let showKebabMenu = $state(false);
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
    if (changes.length === 0) return [];
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
  }

  async function confirmSave() {
    if (!selectedFile || !editorView) return;

    saving = true;

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

      if (!res.ok) {
        const text = await res.text();
        const parsedErr = parseValidationError(text, ru ? 'ru' : 'en');
        throw new Error(parsedErr || 'Failed to save file');
      }

      showSaveConfirmModal = false;
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

  async function handleSaveAndApply() {
    if (!selectedFile || !editorView) return;
    applyLoading = true;
    await tick();
    backgroundStatusText = $t('editor.saving') || 'Сохранение...';

    try {
      const content = editorView.state.doc.toString();
      const csrfToken = localStorage.getItem('csrf_token');

      // 1. POST /api/config/save
      const saveRes = await fetch(`/api/config/save?path=${encodeURIComponent(selectedFile)}`, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          'X-CSRF-Token': csrfToken || ''
        },
        body: content
      });

      if (!saveRes.ok) {
        const text = await saveRes.text();
        const parsedErr = parseValidationError(text, ru ? 'ru' : 'en');
        throw new Error(parsedErr || 'Failed to save file');
      }

      originalContent = content;
      isDirty = false;
      localStorage.removeItem(`editor.draft.${selectedFile}`);
      hasDraft = false;
      draftContent = '';

      // Update tab state
      const activeT = tabs.find((t) => t.path === selectedFile);
      if (activeT) {
        activeT.isDirty = false;
        activeT.originalContent = content;
        tabs = [...tabs];
      }

      await loadBackups(selectedFile);

      // 2. POST /api/service/control?action=restart
      backgroundStatusText = $t('editor.restarting') || 'Перезапуск службы...';
      const restartRes = await fetch('/api/service/control?action=restart', {
        method: 'POST',
        headers: { 'X-CSRF-Token': csrfToken || '' }
      });

      const restartText = await restartRes.text();
      if (!restartRes.ok) throw new Error(restartText || 'Failed to restart service');

      // 3. Status polling
      startBackgroundStatusCheck();
    } catch (e: any) {
      console.error('handleSaveAndApply error:', e);
      showToast('error', $t('editor.save_error') + ': ' + e.message);
      applyLoading = false;
      backgroundStatusText = '';
    }
  }

  function startBackgroundStatusCheck() {
    let attempts = 0;
    const maxAttempts = 12;
    const intervalTime = 1500;

    backgroundStatusText = `${$t('editor.checking_status') || 'Проверка статуса...'} (1/${maxAttempts})`;

    const interval = setInterval(async () => {
      attempts++;
      backgroundStatusText = `${$t('editor.checking_status') || 'Проверка статуса...'} (${attempts}/${maxAttempts})`;

      try {
        const res = await fetch('/api/service/status');
        if (res.ok) {
          const parsed = await res.json();
          if (parsed && parsed.success && parsed.data && parsed.data.is_running === true) {
            clearInterval(interval);
            showToast(
              'success',
              $t('editor.apply_success') || 'Конфигурация успешно применилась, служба запущена!'
            );
            applyLoading = false;
            backgroundStatusText = '';
            return;
          }
        }
      } catch (err) {
        // Ignore check errors and retry
      }

      if (attempts >= maxAttempts) {
        clearInterval(interval);
        showToast(
          'error',
          $t('editor.apply_timeout') || 'Служба не запустилась вовремя. Проверьте логи.'
        );
        applyLoading = false;
        backgroundStatusText = '';
      }
    }, intervalTime);
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

        // Update active tab state
        const activeT = tabs.find((t) => t.path === selectedFile);
        if (activeT) {
          activeT.currentContent = content;
          activeT.isDirty = content !== activeT.originalContent;
          isDirty = activeT.isDirty;
          tabs = [...tabs];
        }
      }

      // Close bottom backup drawer
      drawerOpen = false;
      selectedBackup = '';
      diffGroups = [];

      showToast('success', $t('editor.backup_restored'));
    } catch (e: any) {
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
      showToast('error', $t('editor.create_error') + ': ' + (e as any)?.message);
    }
  }

  let showDeleteConfirmModal = $state(false);

  function deleteFile() {
    if (!selectedFile) return;
    showDeleteConfirmModal = true;
  }

  async function confirmDeleteFile() {
    if (!selectedFile) return;
    showDeleteConfirmModal = false;

    const csrfToken = localStorage.getItem('csrf_token');

    try {
      const res = await fetch(`/api/config/delete?path=${encodeURIComponent(selectedFile)}`, {
        method: 'POST',
        headers: { 'X-CSRF-Token': csrfToken || '' }
      });

      if (!res.ok) throw new Error(await res.text());

      showToast('success', $t('app.delete'));
      const fileToDelete = selectedFile;
      closeTab(fileToDelete, true);
      await loadFiles();
    } catch (e) {
      showToast('error', $t('editor.delete_error') + ': ' + (e as any)?.message);
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
      showToast('error', $t('editor.rename_error') + ': ' + (e as any)?.message);
    }
  }

  function toggleSchema() {
    schemaEnabled = !schemaEnabled;
  }

  function toggleExpertMode() {
    expertMode = !expertMode;
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
        showToast('success', $t('editor.quick_fixes_applied', { count: fixesApplied }));
      } else {
        showToast('info', $t('editor.no_quick_fixes_needed'));
      }
    } catch (e) {
      showToast('error', $t('editor.quick_fix_error', { message: (e as any)?.message ?? '' }));
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

  async function loadTemplatePreview(template: Template) {
    selectedTemplate = template;
    templatePreview = '';
    loadingPreview = true;
    try {
      const res = await fetch(`/api/templates/fetch?name=${encodeURIComponent(template.name)}`);
      if (res.ok) {
        const data = await res.json();
        templatePreview = (data.content || '').split('\n').slice(0, 50).join('\n');
      }
    } catch (e) {
      templatePreview = '';
    } finally {
      loadingPreview = false;
    }
  }

  async function loadTemplateStatus() {
    try {
      const res = await fetch('/api/templates/status');
      if (res.ok) {
        templateStatus = await res.json();
      }
    } catch (e) {
      templateStatus = null;
    }
  }

  async function updateTemplates() {
    updatingTemplates = true;
    try {
      const csrfToken = localStorage.getItem('csrf_token');
      const res = await fetch('/api/templates/update', {
        method: 'POST',
        headers: { 'X-CSRF-Token': csrfToken || '' }
      });
      if (!res.ok) throw new Error((await res.text()) || 'Failed');
      await loadTemplates();
      const first = templates.find((t) => t.type === templateTab);
      if (first) await loadTemplatePreview(first);
      showToast('success', $t('editor.templates_updated'));
      await loadTemplateStatus();
    } catch (e: any) {
      showToast('error', $t('editor.templates_update_error'));
    } finally {
      updatingTemplates = false;
    }
  }

  function openTemplatesModal() {
    templateTab = 'xray';
    selectedTemplate = null;
    templatePreview = '';
    showTemplatesModal = true;
    loadTemplateStatus();
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

    templateLoading = true;
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
    } finally {
      templateLoading = false;
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
              method: 'aes-256-gcm',
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

  function formatBytes(bytes: number): string {
    if (bytes === 0) return '0 B';
    const k = 1024;
    const sizes = ['B', 'KB', 'MB'];
    const i = Math.floor(Math.log(bytes) / Math.log(k));
    return parseFloat((bytes / Math.pow(k, i)).toFixed(1)) + ' ' + sizes[i];
  }

  // Reactive file info using $derived
  let fileSize = $derived(formatBytes(originalContent ? new Blob([originalContent]).size : 0));
  let fileType = $derived(
    selectedFile
      ? selectedFile.endsWith('.yaml') || selectedFile.endsWith('.yml')
        ? 'YAML'
        : 'JSON'
      : ''
  );
  let fileLineEndings = $derived(originalContent?.includes('\r\n') ? 'CRLF' : 'LF');

  onMount(() => {
    loadFiles();
    loadTemplates();
    checkHashTab();
    window.addEventListener('hashchange', checkHashTab);
  });

  onDestroy(() => {
    window.removeEventListener('hashchange', checkHashTab);
  });
</script>

<svelte:window onkeydown={handleGlobalKeydown} />

<div class="editor-page-container">
  <div class="page-head">
    <div>
      <div class="crumbs">
        {$t('nav.group_core')} <span style="color:var(--fg-faint);margin:0 6px;">/</span>
        {$t('nav.editor')}
        {#if activeTab === 'constructor'}
          <span style="color:var(--fg-faint);margin:0 6px;">/</span>
          {$t('editor.tab_constructor')}
        {/if}
      </div>
      <h1>
        {activeTab === 'constructor'
          ? $t('editor.constructor_title') || 'Конструктор конфигурации'
          : $t('editor.h1') || 'Конфигурация'}
      </h1>
      <p class="sub">
        {activeTab === 'constructor'
          ? $t('editor.constructor_subtitle') || 'Пошаговое создание конфигураций'
          : $t('editor.h1_sub') ||
            'YAML / JSON конфиги с подсветкой, валидацией по схеме и автодополнением.'}
      </p>
    </div>
    {#if activeTab === 'files'}
      <div class="ph-actions">
        <button
          class="btn btn-primary"
          onclick={() => {
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
            onclick={() => loadFile(selectedFile)}
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
            onclick={checkBeforeSave}
            disabled={saving || applyLoading}
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
          <button
            class="btn btn-accent"
            onclick={handleSaveAndApply}
            disabled={saving || applyLoading}
            title={$t('editor.save_and_apply')}
          >
            {#if applyLoading}
              <span class="ks-dot-spin">
                <span class="ks-dot"></span>
                <span class="ks-dot"></span>
                <span class="ks-dot"></span>
              </span>
              {$t('app.loading')}
            {:else}
              <svg
                width="14"
                height="14"
                viewBox="0 0 24 24"
                fill="none"
                stroke="currentColor"
                stroke-width="2.5"
                stroke-linecap="round"
                stroke-linejoin="round"
                ><path
                  d="M19 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h11l5 5v11a2 2 0 0 1-2 2Z"
                /><polyline points="17 21 17 13 7 13 7 21" /><polyline points="7 3 7 8 15 8" /><path
                  d="m14 11-2 2-2-2"
                /><path d="M12 7v6" /></svg
              >
              {$t('editor.save_and_apply')}
            {/if}
          </button>
        {/if}
      </div>
    {/if}
  </div>

  <div class="editor-tabs">
    <button class="tab-btn" class:active={activeTab === 'files'} onclick={() => setTab('files')}>
      <Icon name="editor" size={14} />
      {$t('editor.tab_files')}
    </button>
    <button
      class="tab-btn"
      class:active={activeTab === 'constructor'}
      onclick={() => setTab('constructor')}
    >
      <Icon name="settings" size={14} />
      {$t('editor.tab_constructor')}
    </button>
  </div>

  {#if activeTab === 'files'}
    <div class="editor-grid" class:sidebar-collapsed={!showSidebar}>
      {#if showSidebar}
        <FileTree
          {xrayFiles}
          {mihomoFiles}
          {selectedFile}
          activeKernel={$capabilities?.active_kernel || ''}
          onLoadFile={loadFile}
        />
      {/if}

      <!-- Main Editor Card -->
      {#if tabs.length === 0}
        <div class="editor-empty-card">
          <EmptyState
            title={$t('editor.select_file')}
            description={$t('editor.empty_state_body')}
            icon={EditorIcon}
          />
          {#if !showSidebar}
            <button
              class="btn btn-primary"
              style="margin-top: 14px;"
              onclick={() => (showSidebar = true)}
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
                ><path
                  d="M22 19a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h5l2 3h9a2 2 0 0 1 2 2z"
                /></svg
              >
              {$t('editor.show_files') || 'Показать файлы'}
            </button>
          {/if}
        </div>
      {:else}
        <div class="editor-main-card">
          <EditorTabs
            {tabs}
            {activeTabPath}
            onSwitchTab={switchTab}
            onPinTab={pinTab}
            onCloseTab={closeTab}
          />

          {#if breadcrumbs.length > 0}
            <div class="editor-breadcrumbs">
              {#each breadcrumbs as segment, i}
                {#if i > 0}
                  <span class="breadcrumb-divider">&gt;</span>
                {/if}
                <button class="breadcrumb-segment" onclick={() => jumpToSegment(segment.pos)}>
                  {segment.label}
                </button>
              {/each}
            </div>
          {/if}
          <div class="editor-toolbar">
            <button
              class="btn btn-secondary"
              style="padding: 6px 10px; margin-right: 8px;"
              onclick={() => (showSidebar = !showSidebar)}
              title={showSidebar ? 'Скрыть сайдбар' : 'Показать сайдбар'}
              aria-label={showSidebar ? 'Скрыть сайдбар' : 'Показать сайдбар'}
            >
              {#if showSidebar}
                <svg
                  width="13"
                  height="13"
                  viewBox="0 0 24 24"
                  fill="none"
                  stroke="currentColor"
                  stroke-width="2"
                  stroke-linecap="round"
                  stroke-linejoin="round"><polyline points="15 18 9 12 15 6" /></svg
                >
              {:else}
                <svg
                  width="13"
                  height="13"
                  viewBox="0 0 24 24"
                  fill="none"
                  stroke="currentColor"
                  stroke-width="2"
                  stroke-linecap="round"
                  stroke-linejoin="round"><polyline points="9 18 15 12 9 6" /></svg
                >
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
                <span
                  class="badge badge-warning"
                  style="font-size: 12px; display: flex; align-items: center; gap: 4px;"
                >
                  <span class="tab-dirty-dot" style="margin:0">●</span>
                  {$t('editor.has_draft') || 'Черновик'}
                </span>
                <button class="btn btn-xs btn-primary" onclick={restoreDraft}>
                  {$t('editor.restore_draft') || 'Восстановить'}
                </button>
                <button class="btn btn-xs btn-secondary" onclick={discardDraft}>
                  {$t('editor.discard_draft') || 'Сбросить'}
                </button>
              </div>
            {/if}

            <div class="kebab-wrap" style="margin-left: auto;">
              <button
                class="btn btn-secondary"
                style="padding: 6px 10px;"
                onclick={toggleKebab}
                aria-label="Дополнительные действия"
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
                >
                  <circle cx="12" cy="12" r="1" />
                  <circle cx="12" cy="5" r="1" />
                  <circle cx="12" cy="19" r="1" />
                </svg>
              </button>
              {#if showKebabMenu}
                <div class="kebab-dropdown" transition:fade={{ duration: 100 }}>
                  <button class="kebab-item" onclick={downloadFile}>
                    <Icon name="download" size={14} />
                    Скачать файл
                  </button>
                  <button
                    class="kebab-item"
                    onclick={() => {
                      showRenameModal = true;
                      renameTarget = selectedFile.split('/').pop() || '';
                    }}
                  >
                    <Icon name="edit" size={14} />
                    {$t('app.rename') || 'Переименовать'}
                  </button>
                  <button class="kebab-item" onclick={openTemplatesModal}>
                    <Icon name="settings" size={14} />
                    Шаблоны
                  </button>
                  {#if fileType === 'JSON'}
                    <button class="kebab-item" onclick={() => (showGeneratorModal = true)}>
                      <Icon name="settings" size={14} />
                      Генератор исходящих
                    </button>
                  {/if}
                  <button class="kebab-item" onclick={applyQuickFixes}>
                    <Icon name="settings" size={14} />
                    Быстрые исправления
                  </button>
                  <div class="kebab-divider"></div>
                  <button class="kebab-item danger" onclick={deleteFile}>
                    <Icon name="trash" size={14} />
                    {$t('app.delete') || 'Удалить'}
                  </button>
                </div>
              {/if}
            </div>
          </div>

          <!-- CodeMirror editor component -->
          <div style="flex: 1; min-height: 0; position:relative; background: var(--cm-bg);">
            {#if loading}
              <div
                style="display:grid;place-items:center;height:100%;position:absolute;inset:0;background:rgba(5,13,22,0.7);z-index:10;"
              >
                <div class="spinner"></div>
              </div>
            {/if}
            {#each tabs as tab (tab.path)}
              {#if tab.path === activeTabPath}
                <CodeMirrorEditor
                  content={tab.currentContent}
                  path={tab.path}
                  {expertMode}
                  {schemaEnabled}
                  bind:view={editorView}
                  onContentChange={(newContent) => {
                    tab.currentContent = newContent;
                    tab.isDirty = newContent !== tab.originalContent;
                    isDirty = tab.isDirty;

                    if (tab.isPreview) {
                      tab.isPreview = false;
                      tabs = [...tabs];
                    }

                    if (isDirty) {
                      localStorage.setItem(`editor.draft.${tab.path}`, newContent);
                    } else {
                      localStorage.removeItem(`editor.draft.${tab.path}`);
                    }
                  }}
                  onCursorChange={(line, col, pos, state) => {
                    cursorLine = line;
                    cursorCol = col;
                    const isYaml = tab.path.endsWith('.yaml') || tab.path.endsWith('.yml');
                    breadcrumbs = buildPathAtCursor(state, pos, isYaml);
                  }}
                  onSave={checkBeforeSave}
                />
              {/if}
            {/each}
          </div>

          <!-- Status Bar / Bottom Drawer Trigger -->
          <div class="editor-statusbar">
            <span class="status-indicator" class:status-dirty={isDirty} style="margin-right: 14px;">
              <span style="color: {isDirty ? 'var(--warning)' : 'var(--success)'};">●</span>
              {isDirty ? $t('editor.unsaved') || 'Изменён' : $t('editor.saved') || 'Сохранён'}
            </span>
            <span>Ln {cursorLine}, Col {cursorCol}</span>
            <div style="margin-left: auto; display: flex; align-items: center; gap: 12px;">
              <label
                style="display:flex;align-items:center;gap:6px;cursor:pointer;user-select:none;"
              >
                <input
                  type="checkbox"
                  checked={schemaEnabled}
                  onchange={toggleSchema}
                  style="margin:0;width:12px;height:12px;"
                />
                Схема
              </label>
              <label
                style="display:flex;align-items:center;gap:6px;cursor:pointer;user-select:none;"
              >
                <input
                  type="checkbox"
                  checked={expertMode}
                  onchange={toggleExpertMode}
                  style="margin:0;width:12px;height:12px;"
                />
                Эксперт
              </label>

              {#if applyLoading && backgroundStatusText}
                <div class="status-apply-indicator">
                  <span class="ks-dot-spin">
                    <span class="ks-dot"></span>
                    <span class="ks-dot"></span>
                    <span class="ks-dot"></span>
                  </span>
                  <span>{backgroundStatusText}</span>
                </div>
              {/if}

              {#if backups.length > 0}
                <button
                  class="backups-toggle-btn"
                  onclick={() => (drawerOpen = !drawerOpen)}
                  style="margin: 0;"
                >
                  <svg
                    width="10"
                    height="10"
                    viewBox="0 0 24 24"
                    fill="none"
                    stroke="currentColor"
                    stroke-width="2.5"
                    class="chevron-icon"
                    class:rotated={drawerOpen}
                  >
                    <polyline points="18 15 12 9 6 15"></polyline>
                  </svg>
                  {$t('editor.backups') || 'Бэкапы'} ({backups.length})
                </button>
              {/if}

              <span
                class="status-tip status-shortcut-tip"
                style="border-left: 1px solid var(--border); padding-left: 12px;"
                >Ctrl+S — сохранить</span
              >
            </div>
          </div>

          <!-- Bottom Drawer -->
          {#if drawerOpen && backups.length > 0}
            <BackupSidebar
              {backups}
              {selectedBackup}
              {diffGroups}
              {backupLoading}
              onSelectBackup={selectBackup}
              onRestoreBackup={restoreBackup}
            />
          {/if}
        </div>
      {/if}
    </div>
  {:else if activeTab === 'constructor'}
    <div transition:fade={{ duration: 150 }} style="margin-top: 16px;">
      <Constructor
        {onSwitchTab}
        onInsertIntoEditor={handleInsertIntoEditor}
        {selectedFile}
        embedded={true}
        invalidateCache={activeTab === 'constructor'}
      />
    </div>
  {/if}
</div>

<!-- CRUD Modals -->
<Modal
  isOpen={showCreateModal}
  title={$t('editor.create_file')}
  onclose={() => (showCreateModal = false)}
>
  <label for="new-file-name" class="sr-only">{$t('editor.file_name')}</label>
  <input
    id="new-file-name"
    type="text"
    bind:value={newFileName}
    placeholder={$t('editor.file_name')}
    class="input"
    style="margin-bottom: 16px; width: 100%;"
    onkeydown={(e) => e.key === 'Enter' && createFile()}
  />
  <div class="confirm-modal-actions">
    <button onclick={() => (showCreateModal = false)} class="btn btn-secondary">
      {$t('app.cancel')}
    </button>
    <button onclick={createFile} class="btn btn-primary">
      {$t('app.create')}
    </button>
  </div>
</Modal>

<Modal
  isOpen={showRenameModal}
  title={$t('editor.rename_file')}
  onclose={() => (showRenameModal = false)}
>
  <label for="rename-target" class="sr-only">{$t('editor.new_name')}</label>
  <input
    id="rename-target"
    type="text"
    bind:value={renameTarget}
    placeholder={$t('editor.new_name')}
    class="input"
    style="margin-bottom: 16px; width: 100%;"
    onkeydown={(e) => e.key === 'Enter' && renameFile()}
  />
  <div class="confirm-modal-actions">
    <button onclick={() => (showRenameModal = false)} class="btn btn-secondary">
      {$t('app.cancel')}
    </button>
    <button onclick={renameFile} class="btn btn-primary">
      {$t('app.rename')}
    </button>
  </div>
</Modal>

<Modal
  isOpen={showTemplatesModal}
  title={$t('editor.templates') || 'Шаблоны'}
  maxWidth="900px"
  class="templates-wide-modal"
  onclose={() => (showTemplatesModal = false)}
>
  {#if templateStatus}
    <div
      style="margin-top: -10px; margin-bottom: 12px; display: flex; align-items: center; justify-content: space-between; gap: 8px;"
    >
      <p class="templates-modal-subtitle" style="margin: 0;">
        {$t('editor.templates_desc') || 'Шаблоны конфигураций'}
      </p>
      <div style="display: flex; align-items: center; gap: 8px;">
        {#if templateStatus.has_update}
          <span class="templates-badge update-available">
            <span class="pulse-dot"></span>
            {$t('editor.update_available') || 'Доступно обновление'} (v{templateStatus.current_version})
          </span>
        {:else if templateStatus.current_version}
          <span class="templates-badge up-to-date">
            <span class="dot"></span>
            {$t('editor.up_to_date') || 'Обновлено'} (v{templateStatus.current_version})
          </span>
        {/if}
        <button
          class="btn btn-secondary templates-update-btn"
          style="padding: 4px 8px; font-size: 12px;"
          onclick={updateTemplates}
          disabled={updatingTemplates}
          title={$t('editor.templates_update')}
        >
          <span class="templates-update-icon" class:spinning={updatingTemplates}>
            <Icon name="refresh" size={12} />
          </span>
          {$t('editor.templates_update') || 'Обновить'}
        </button>
      </div>
    </div>
  {/if}

  <!-- 2-column body -->
  <div class="templates-body-grid">
    <!-- Left column: tabs + list -->
    <div class="templates-col-list">
      <div class="templates-kernel-tabs">
        <button
          class="tab-btn"
          class:active={templateTab === 'xray'}
          aria-pressed={templateTab === 'xray'}
          onclick={async () => {
            templateTab = 'xray';
            selectedTemplate = null;
            templatePreview = '';
            const first = filteredTemplates[0];
            if (first) await loadTemplatePreview(first);
          }}
        >
          {$t('editor.templates_tab_xray') || 'Xray'}
        </button>
        <button
          class="tab-btn"
          class:active={templateTab === 'mihomo'}
          aria-pressed={templateTab === 'mihomo'}
          onclick={async () => {
            templateTab = 'mihomo';
            selectedTemplate = null;
            templatePreview = '';
            const first = filteredTemplates[0];
            if (first) await loadTemplatePreview(first);
          }}
        >
          {$t('editor.templates_tab_mihomo') || 'Mihomo'}
        </button>
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
      {#if loadingPreview}
        <div class="templates-preview-loading">
          <span class="spinning"><Icon name="refresh" size={16} /></span>
        </div>
      {:else if templatePreview}
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
      disabled={!selectedTemplate || !editorView || templateLoading}
      title={!editorView ? $t('editor.no_file_for_template') : undefined}
      onclick={() => selectedTemplate && applyTemplate(selectedTemplate)}
    >
      {$t('editor.apply_template')}
    </button>
  </div>
</Modal>

<Modal
  isOpen={showGeneratorModal}
  title={$t('editor.generator') || 'Генератор исходящих'}
  onclose={() => (showGeneratorModal = false)}
>
  <div class="form-group" style="margin-bottom: 12px;">
    <label
      for="gen-protocol"
      style="display: block; font-size: 12px; color: var(--fg-dim); margin-bottom: 4px;"
      >{$t('editor.protocol') || 'Протокол'}</label
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
        >{$t('editor.address') || 'Адрес'}</label
      >
      <input
        id="gen-address"
        type="text"
        bind:value={genAddress}
        placeholder="example.com"
        class="input"
        style="width: 100%;"
      />
    </div>
    <div class="form-group">
      <label
        for="gen-port"
        style="display: block; font-size: 12px; color: var(--fg-dim); margin-bottom: 4px;"
        >{$t('editor.port') || 'Порт'}</label
      >
      <input id="gen-port" type="number" bind:value={genPort} class="input" style="width: 100%;" />
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
        onclick={() => (genUUID = crypto.randomUUID())}
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
        style="width: 100%;"
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
            style="width: 100%;"
          />
        </div>
      {/if}
    </div>
  {/if}

  <div class="confirm-modal-actions" style="margin-top: 16px;">
    <button onclick={() => (showGeneratorModal = false)} class="btn btn-secondary">
      {$t('app.cancel')}
    </button>
    <button onclick={generateOutbound} class="btn btn-primary">
      {$t('app.generate') || 'Сгенерировать'}
    </button>
  </div>
</Modal>

<Modal
  isOpen={showSaveConfirmModal}
  title={$t('editor.confirm_save_title') || 'Confirm Save'}
  maxWidth="700px"
  onclose={() => (showSaveConfirmModal = false)}
>
  <!-- Diff Preview -->
  <div class="diff-preview" style="margin-top: 12px;">
    <div class="diff-preview-title">
      {$t('editor.diff_preview') || 'Предпросмотр изменений'}
    </div>
    <div class="diff-preview-body" style="max-height: 40vh; overflow-y: auto;">
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
    <button onclick={() => (showSaveConfirmModal = false)} class="btn btn-secondary">
      {$t('app.cancel')}
    </button>
    <button onclick={confirmSave} class="btn btn-primary" disabled={saving}>
      {saving ? $t('app.loading') : $t('app.save')}
    </button>
  </div>
</Modal>

<Modal
  isOpen={showDeleteConfirmModal}
  title={`${$t('editor.delete_file')}: ${selectedFile.split('/').pop() || ''}`}
  onclose={() => (showDeleteConfirmModal = false)}
>
  <p style="margin: 0;">
    {$t('editor.delete_confirm_body', { file: selectedFile.split('/').pop() || '' })}
  </p>
  <div class="confirm-modal-actions" style="margin-top: 16px;">
    <button onclick={() => (showDeleteConfirmModal = false)} class="btn btn-secondary">
      {$t('app.cancel')}
    </button>
    <button onclick={confirmDeleteFile} class="btn btn-danger">
      {$t('app.delete')}
    </button>
  </div>
</Modal>

<style>
  .status-dirty {
    color: var(--warning) !important;
  }

  .editor-tabs {
    display: inline-flex;
    align-self: flex-start;
    width: fit-content;
    gap: 4px;
    background: rgba(255, 255, 255, 0.03);
    border: 1px solid var(--border);
    border-radius: var(--radius);
    padding: 4px;
    margin-bottom: 16px;
  }

  .tab-btn {
    background: none;
    border: none;
    color: var(--fg-secondary);
    font-size: 14px;
    font-weight: 600;
    padding: 6px 14px;
    border-radius: var(--radius-sm);
    cursor: pointer;
    display: flex;
    align-items: center;
    gap: 6px;
    transition:
      background var(--transition-fast),
      color var(--transition-fast);
  }

  .tab-btn:hover {
    color: var(--fg-primary);
    background: rgba(255, 255, 255, 0.04);
  }

  .tab-btn.active {
    background: rgba(255, 255, 255, 0.08);
    color: var(--fg-primary);
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
    font-size: 14px;
    color: var(--fg-primary);
    font-weight: 600;
  }

  .editor-toolbar .file-meta {
    font-family: var(--font-family-mono);
    font-size: 12px;
    color: var(--fg-dim);
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
    font-size: 14px;
    color: var(--fg-primary);
  }

  .template-desc {
    font-size: 12px;
    color: var(--fg-dim);
  }

  .template-type {
    font-size: 12px;
    text-transform: uppercase;
    background: var(--bg-card);
    padding: 2px 6px;
    border-radius: 4px;
    border: 1px solid var(--border);
    color: var(--fg-dim);
    font-family: var(--font-family-mono);
  }

  .template-item.selected {
    border-color: var(--accent);
    background: var(--hover);
  }

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

  :global(.templates-modal-title-block) {
    display: flex;
    flex-direction: column;
    gap: 4px;
  }

  .templates-modal-subtitle {
    margin: 0;
    color: var(--fg-dim);
    font-size: 12px;
  }

  .templates-badge {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    font-size: 12px;
    font-weight: 600;
    padding: 3px 8px;
    border-radius: 12px;
  }
  .templates-badge.update-available {
    background-color: rgba(240, 180, 80, 0.15);
    color: var(--warning);
  }
  .templates-badge.up-to-date {
    background-color: rgba(70, 209, 138, 0.15);
    color: var(--success);
  }
  .templates-badge .dot {
    width: 6px;
    height: 6px;
    border-radius: 50%;
    background-color: var(--success);
  }
  .templates-badge .pulse-dot {
    width: 6px;
    height: 6px;
    border-radius: 50%;
    background-color: var(--warning);
    position: relative;
  }
  .templates-badge .pulse-dot::after {
    content: '';
    position: absolute;
    width: 100%;
    height: 100%;
    top: 0;
    left: 0;
    background-color: inherit;
    border-radius: 50%;
    animation: badge-pulse 1.5s infinite ease-out;
  }
  @keyframes badge-pulse {
    0% {
      transform: scale(1);
      opacity: 1;
    }
    100% {
      transform: scale(2.5);
      opacity: 0;
    }
  }

  :global(.templates-modal-header-actions) {
    display: flex;
    align-items: center;
    gap: 8px;
    flex-shrink: 0;
  }

  .templates-update-btn {
    display: flex;
    align-items: center;
    gap: 6px;
    font-size: 12px;
    padding: 6px 10px;
    height: 32px;
  }

  .templates-update-icon {
    display: flex;
    align-items: center;
  }

  .spinning {
    display: inline-flex;
    animation: spin 0.8s linear infinite;
  }

  @keyframes spin {
    from {
      transform: rotate(0deg);
    }
    to {
      transform: rotate(360deg);
    }
  }

  .templates-body-grid {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: 0;
    flex: 1;
    min-height: 0;
    overflow: hidden;
    max-height: 460px;
  }

  .templates-col-list {
    display: flex;
    flex-direction: column;
    border-right: 1px solid var(--border);
    overflow: hidden;
  }

  .templates-kernel-tabs {
    display: flex;
    gap: 4px;
    padding: 12px 16px 8px;
    border-bottom: 1px solid var(--border);
    flex-shrink: 0;
  }

  .templates-kernel-tabs .tab-btn {
    padding: 6px 12px;
    font-size: 14px;
  }

  .templates-col-list .template-list {
    padding: 12px;
  }

  .templates-col-preview {
    background: var(--bg-deep);
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
    color: var(--fg-secondary);
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

  .templates-preview-loading,
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
    font-size: 12px;
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

  .validation-result {
    display: flex;
    align-items: flex-start;
    gap: 8px;
    padding: 10px 14px;
    border-radius: 6px;
    font-size: 12px;
  }

  .validation-loading {
    background: rgba(41, 194, 240, 0.08);
    color: var(--accent);
    border: 1px solid rgba(41, 194, 240, 0.2);
  }

  .v-icon {
    font-weight: 600;
    flex-shrink: 0;
  }

  .diff-preview {
    flex: 1;
    overflow: hidden;
    display: flex;
    flex-direction: column;
    min-height: 0;
  }

  .diff-preview-title {
    font-size: 12px;
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
    background: var(--cm-bg);
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
    color: var(--success);
    background: rgba(163, 233, 182, 0.04);
  }

  .diff-line-removed {
    color: var(--danger);
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

  .btn-accent {
    background: linear-gradient(180deg, var(--accent), var(--accent-2));
    border: 1px solid var(--accent);
    color: var(--btn-primary-text, #03182a);
    font-weight: 600;
  }
  .btn-accent:hover:not(:disabled) {
    background: var(--accent-hover);
    box-shadow: 0 0 10px var(--accent-soft);
  }
  .btn-accent:disabled {
    opacity: 0.6;
    cursor: not-allowed;
  }

  .ks-dot-spin {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    margin-right: 6px;
  }
  .ks-dot {
    width: 6px;
    height: 6px;
    background-color: currentColor;
    border-radius: 50%;
    animation: ks-dot-bounce 1.4s infinite ease-in-out both;
  }
  .ks-dot:nth-child(1) {
    animation-delay: -0.32s;
  }
  .ks-dot:nth-child(2) {
    animation-delay: -0.16s;
  }

  @keyframes ks-dot-bounce {
    0%,
    80%,
    100% {
      transform: scale(0);
    }
    40% {
      transform: scale(1);
    }
  }

  .status-apply-indicator {
    display: flex;
    align-items: center;
    gap: 8px;
    font-size: 12px;
    color: var(--accent);
    padding: 0 10px;
    border-left: 1px solid var(--border);
  }

  .backups-toggle-btn {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    background: rgba(255, 255, 255, 0.03);
    border: 1px solid var(--border);
    border-radius: var(--radius);
    color: var(--fg-dim);
    font-size: 12px;
    padding: 4px 10px;
    cursor: pointer;
    font-family: var(--font-family-mono);
    transition: all 0.15s ease;
    margin-left: 10px;
  }

  .backups-toggle-btn:hover {
    background: rgba(255, 255, 255, 0.06);
    color: var(--fg-primary);
  }

  .chevron-icon {
    transition: transform 0.2s ease;
  }
  .chevron-icon.rotated {
    transform: rotate(180deg);
  }

  .editor-empty-card :global(.empty-state) {
    justify-content: center;
  }

  .editor-statusbar {
    padding: 6px 14px;
    background: rgba(0, 0, 0, 0.2);
    border-top: 1px solid var(--border);
    display: flex;
    align-items: center;
    font-family: var(--font-family-mono);
    font-size: 12px;
    color: var(--fg-dim);
    min-height: 30px;
  }
  .editor-breadcrumbs {
    display: flex;
    flex-wrap: nowrap;
    align-items: center;
    gap: 4px;
    padding: 8px 14px;
    background: rgba(0, 0, 0, 0.1);
    border-bottom: 1px solid var(--border);
    font-size: 12px;
    color: var(--fg-dim);
    overflow-x: auto;
    scrollbar-width: thin;
    scrollbar-color: var(--border) transparent;
  }
  .editor-breadcrumbs::-webkit-scrollbar {
    height: 3px;
  }
  .editor-breadcrumbs::-webkit-scrollbar-thumb {
    background: var(--border);
    border-radius: var(--radius);
  }
  .breadcrumb-segment {
    display: inline-block;
    background: transparent;
    border: none;
    padding: 0;
    color: var(--fg-dim);
    font: inherit;
    cursor: pointer;
    max-width: 160px;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .breadcrumb-divider {
    flex-shrink: 0;
  }
  @media (max-width: 768px) {
    .editor-breadcrumbs {
      display: none !important;
    }
    .status-shortcut-tip {
      display: none !important;
    }
  }
</style>
