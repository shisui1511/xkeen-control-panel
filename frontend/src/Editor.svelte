<script lang="ts">
  import { onMount, onDestroy, tick } from 'svelte';
  import { fade, slide } from 'svelte/transition';
  import { t, currentLang } from './i18n';
  import { showToast, capabilities, showConfirm } from './stores';
  import { apiFetch, apiFetchJSON } from './lib/api';
  import { parseValidationError } from './lib/errorParser';
  import Icon from './lib/components/Icon.svelte';
  import EmptyState from './components/EmptyState.svelte';
  import EditorIcon from './lib/components/icons/Editor.svelte';
  import Skeleton from './components/Skeleton.svelte';
  import { EditorState } from '@codemirror/state';
  import { EditorView } from '@codemirror/view';

  import { buildPathAtCursor, type PathSegment } from './lib/editor-utils';

  // Subcomponents
  import EditorSidebar from './components/editor/EditorSidebar.svelte';
  import EditorHeaderActions from './components/editor/EditorHeaderActions.svelte';
  import CodeMirrorEditor from './components/editor/CodeMirrorEditor.svelte';
  import BackupSidebar from './components/editor/BackupSidebar.svelte';
  import FileActionModals from './components/editor/FileActionModals.svelte';
  import TemplatesModal, { type Template } from './components/editor/TemplatesModal.svelte';
  import SaveConfirmModal from './components/editor/SaveConfirmModal.svelte';
  import OutboundGeneratorModal from './components/editor/OutboundGeneratorModal.svelte';
  import DeleteFileModal from './components/editor/DeleteFileModal.svelte';
  import EditorToolbar from './components/editor/EditorToolbar.svelte';
  import EditorStatusBar from './components/editor/EditorStatusBar.svelte';
  import EditorBreadcrumbs from './components/editor/EditorBreadcrumbs.svelte';
  import PageHeader from './PageHeader.svelte';
  import Tabs, { type TabItem } from './components/Tabs.svelte';
  import DraftRestoreBanner from './components/DraftRestoreBanner.svelte';
  import { registerDirtySource, getDraft, clearDraft, type DraftRecord } from './lib/dirtyRegistry';
  import { activateRestartGrace } from './lib/serviceGrace';
  import PreflightWarnings, {
    type PreflightWarning
  } from './components/editor/PreflightWarnings.svelte';
  import { getDiff, getDiffGroups, type DiffGroup } from './components/editor/diff';
  import { computeQuickFixes } from './components/editor/quickFixes';
  import { startServiceStatusPolling } from './components/editor/serviceChecker';
  import {
    duplicateConfigFile,
    downloadConfigFile,
    downloadContent,
    fetchBackupsList,
    fetchBackupContent,
    readConfigFile,
    createConfigFile,
    deleteConfigFile,
    renameConfigFile,
    listConfigFiles,
    formatBytes,
    type ConfigFileInfo
  } from './components/editor/fileOps';

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

  const editorModeTabItems = $derived<TabItem[]>([
    { value: 'files', label: $t('editor.tab_files'), testId: 'tab-files' },
    { value: 'constructor', label: $t('editor.tab_constructor'), testId: 'tab-constructor' }
  ]);

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
  let stopPollingStatus: (() => void) | null = null;

  // Drawer states
  let drawerOpen = $state(false);
  let selectedBackup = $state('');
  let diffGroups = $state<DiffGroup[]>([]);
  let backupLoading = $state(false);
  let saveWarnings = $state<PreflightWarning[]>([]);

  // Directory management
  const xrayDir = '/opt/etc/xray/configs';
  const mihomoDir = '/opt/etc/mihomo';
  let currentDir = $state(xrayDir);

  // Dual-panel sidebar file lists
  let xrayFiles = $state<ConfigFileInfo[]>([]);
  let mihomoFiles = $state<ConfigFileInfo[]>([]);
  let showSidebar = $state(true);

  let isMac = $derived(
    typeof navigator !== 'undefined' && /Mac|iPod|iPhone|iPad/.test(navigator.platform)
  );

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
  let showGeneratorModal = $state(false);
  let newFileName = $state('');
  let renameTarget = $state('');

  // Dirty state tracking
  let originalContent = $state('');
  let isDirty = $state(false);
  let saveError = $state(false);

  type SaveBadgeVariant = 'running' | 'warning' | 'stopped';

  const saveStatusState = $derived.by(() => {
    if (saving || applyLoading) {
      return { kind: 'live' as const, label: backgroundStatusText || $t('editor.saving') };
    }
    if (saveError) {
      return {
        kind: 'badge' as const,
        variant: 'stopped' as SaveBadgeVariant,
        label: $t('editor.save_error')
      };
    }
    if (isDirty) {
      return {
        kind: 'badge' as const,
        variant: 'warning' as SaveBadgeVariant,
        label: $t('editor.unsaved')
      };
    }
    return {
      kind: 'badge' as const,
      variant: 'running' as SaveBadgeVariant,
      label: $t('editor.saved')
    };
  });

  // Local active tab: 'files' | 'constructor'
  let activeTab = $state<'files' | 'constructor'>('files');

  // Constructor.svelte lazy-chunk boundary (Pitfall 1, RESEARCH.md): without
  // this, XrayRoutingConstructor.svelte (3 473 lines) rides statically inside
  // this file's own lazy chunk. `constructorReloadKey` keys the {#key} block
  // wrapping the {#await import(...)} below; the actual recovery mechanism is
  // a full reload — see Dashboard.svelte's retryChunkLoad() for the same
  // reasoning (a failed dynamic import() specifier is permanently cached by
  // the browser's module map for the document's lifetime).
  let constructorReloadKey = $state(0);
  let constructorChunkErrorShown = false;

  function retryConstructorChunkLoad() {
    constructorChunkErrorShown = false;
    constructorReloadKey++;
    window.location.reload();
  }

  function reportConstructorChunkError(err: unknown): void {
    console.error('Failed to load Constructor lazy chunk', err);
    if (constructorChunkErrorShown) return;
    constructorChunkErrorShown = true;
    showToast('error', $t('app.chunk_load_failed'), 0, {
      label: $t('app.retry'),
      onClick: retryConstructorChunkLoad
    });
  }

  function reportConstructorChunkErrorAction(_node: HTMLElement, err: unknown): void {
    reportConstructorChunkError(err);
  }

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
    constructorChunkErrorShown = false;
    if (tab === 'constructor') {
      window.location.hash = '#/constructor';
    } else {
      window.location.hash = '#/editor';
    }
    window.dispatchEvent(new Event('hashchange'));
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
      showToast('success', $t('editor.yaml_inserted'));
    } else {
      activeTab = 'files';
      window.location.hash = '#/editor';
      showToast('info', $t('editor.select_file_for_yaml'));
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
    showToast('success', $t('editor.draft_restored'));
  }

  function discardDraft() {
    if (selectedFile) {
      localStorage.removeItem('editor.draft.' + selectedFile);
      hasDraft = false;
      draftContent = '';
      showToast('info', $t('editor.draft_discarded'));
    }
  }

  function checkDirty(): boolean {
    const currentTab = tabs.find((t) => t.path === activeTabPath);
    return currentTab ? currentTab.isDirty : false;
  }

  async function confirmUnsaved(): Promise<boolean> {
    return await showConfirm({
      title: $t('editor.unsaved_changes_title'),
      message: $t('editor.unsaved_warning'),
      variant: 'warning',
      confirmLabel: $t('app.continue')
    });
  }

  async function loadFiles(dir?: string) {
    if (dir) currentDir = dir;
    try {
      xrayFiles = await listConfigFiles(xrayDir);
      mihomoFiles = await listConfigFiles(mihomoDir);
    } catch (e: any) {
      if (e?.status === 401) return;
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

  async function closeTab(path: string, force = false) {
    const tabIndex = tabs.findIndex((t) => t.path === path);
    if (tabIndex === -1) return;

    const tabToClose = tabs[tabIndex];

    if (tabToClose.isDirty && !force) {
      if (activeTabPath !== path) {
        await switchTab(path);
      }
      if (!(await confirmUnsaved())) return;
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
    saveError = false;

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
      const content = await readConfigFile(path);

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
            if (!(await confirmUnsaved())) {
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
      backups = await fetchBackupsList(path);
    } catch (e: any) {
      if (e?.status === 401) return;
    }
  }

  async function selectBackup(backupPath: string) {
    if (selectedBackup === backupPath) return;
    selectedBackup = backupPath;
    backupLoading = true;
    diffGroups = [];
    try {
      const backupContent = await fetchBackupContent(backupPath);
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

  function downloadFile() {
    if (!selectedFile || !editorView) return;
    downloadContent(selectedFile.split('/').pop() || 'config', editorView.state.doc.toString());
  }

  async function checkBeforeSave() {
    if (!selectedFile || !editorView) return;
    const content = editorView.state.doc.toString();

    diffChanges = getDiff(originalContent, content);

    if (diffChanges.filter((c) => c.type !== 'unchanged').length === 0) {
      showToast('info', $t('editor.no_changes'));
      return;
    }

    showSaveConfirmModal = true;
  }

  async function confirmSave() {
    if (!selectedFile || !editorView) return;

    saving = true;
    saveError = false;
    saveWarnings = [];

    try {
      const content = editorView.state.doc.toString();

      const res = await apiFetch(`/api/config/save?path=${encodeURIComponent(selectedFile)}`, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json'
        },
        body: content
      });

      if (!res.ok) {
        const text = await res.text();
        const parsedErr = parseValidationError(text, ru ? 'ru' : 'en');
        throw new Error(parsedErr || 'Failed to save file');
      }

      const saveJson = await res.json().catch(() => null);
      const data = saveJson?.data ?? saveJson;
      saveWarnings = Array.isArray(data?.warnings) ? data.warnings : [];

      showSaveConfirmModal = false;
      showToast('success', $t('editor.file_saved'));
      originalContent = content;
      isDirty = false;
      saveError = false;

      // Update tab state
      const activeT = tabs.find((t) => t.path === selectedFile);
      if (activeT) {
        activeT.isDirty = false;
        activeT.originalContent = content;
        tabs = [...tabs];
      }

      localStorage.removeItem(`editor.draft.${selectedFile}`);
      hasDraft = false;
      draftContent = '';
      await loadBackups(selectedFile);
    } catch (e: any) {
      if (e?.status === 401) return;
      saveError = true;
      showToast('error', $t('editor.save_error') + ': ' + e.message);
    } finally {
      saving = false;
    }
  }

  async function handleSaveAndApply() {
    if (!selectedFile || !editorView) return;
    applyLoading = true;
    saveError = false;
    saveWarnings = [];
    await tick();
    backgroundStatusText = $t('editor.saving');

    try {
      const content = editorView.state.doc.toString();

      // 1. POST /api/config/save
      const saveRes = await apiFetch(`/api/config/save?path=${encodeURIComponent(selectedFile)}`, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json'
        },
        body: content
      });

      if (!saveRes.ok) {
        const text = await saveRes.text();
        const parsedErr = parseValidationError(text, ru ? 'ru' : 'en');
        throw new Error(parsedErr || 'Failed to save file');
      }

      const saveJson = await saveRes.json().catch(() => null);
      const data = saveJson?.data ?? saveJson;
      saveWarnings = Array.isArray(data?.warnings) ? data.warnings : [];

      originalContent = content;
      isDirty = false;
      saveError = false;
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
      activateRestartGrace(6000);
      backgroundStatusText = $t('editor.restarting');
      const restartRes = await apiFetch('/api/service/control?action=restart', {
        method: 'POST'
      });

      const restartText = await restartRes.text();
      if (!restartRes.ok) throw new Error(restartText || 'Failed to restart service');

      // 3. Status polling
      startBackgroundStatusCheck();
    } catch (e: any) {
      if (e?.status === 401) return;
      console.error('handleSaveAndApply error:', e);
      saveError = true;
      showToast('error', $t('editor.save_error') + ': ' + e.message);
      applyLoading = false;
      backgroundStatusText = '';
    }
  }

  function startBackgroundStatusCheck() {
    if (stopPollingStatus) {
      stopPollingStatus();
      stopPollingStatus = null;
    }

    stopPollingStatus = startServiceStatusPolling({
      onStatusChange: (text) => {
        backgroundStatusText = text;
      },
      onSuccess: () => {
        applyLoading = false;
        backgroundStatusText = '';
        stopPollingStatus = null;
      },
      onError: () => {
        applyLoading = false;
        backgroundStatusText = '';
        stopPollingStatus = null;
      }
    });
  }

  async function restoreBackup(backupPath: string) {
    const filename = backupPath.split('/').pop() || backupPath;
    if (
      !(await showConfirm({
        title: $t('editor.restore_backup_title'),
        objectName: filename,
        consequence: $t('editor.restore_confirm'),
        variant: 'warning',
        confirmLabel: $t('editor.restore')
      }))
    )
      return;

    try {
      const content = await fetchBackupContent(backupPath);

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

  async function createFile(fileName?: string) {
    const name = fileName || newFileName;
    if (!name) return;

    const path = selectedFile
      ? selectedFile.substring(0, selectedFile.lastIndexOf('/') + 1) + name
      : '/opt/etc/xray/configs/' + name;

    try {
      await createConfigFile(path);
      showToast('success', $t('editor.create_file'));
      showCreateModal = false;
      newFileName = '';
      await loadFiles();
      await loadFile(path);
    } catch (e: any) {
      if (e?.status === 401) return;
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

    try {
      await deleteConfigFile(selectedFile);
      showToast('success', $t('app.delete'));
      const fileToDelete = selectedFile;
      await closeTab(fileToDelete, true);
      await loadFiles();
    } catch (e: any) {
      if (e?.status === 401) return;
      showToast('error', $t('editor.delete_error') + ': ' + (e as any)?.message);
    }
  }

  async function renameFile(newName?: string) {
    const target = newName || renameTarget;
    if (!target || !selectedFile) return;

    const newPath = selectedFile.substring(0, selectedFile.lastIndexOf('/') + 1) + target;

    try {
      await renameConfigFile(selectedFile, newPath);
      showToast('success', $t('app.rename'));
      showRenameModal = false;
      renameTarget = '';
      await loadFiles();
      await loadFile(newPath);
    } catch (e: any) {
      if (e?.status === 401) return;
      showToast('error', $t('editor.rename_error') + ': ' + (e as any)?.message);
    }
  }

  async function duplicateFile(file: ConfigFileInfo) {
    try {
      const newPath = await duplicateConfigFile(file);
      showToast('success', $t('editor.duplicate_file'));
      await loadFiles();
      await loadFile(newPath);
    } catch (e: any) {
      showToast('error', e?.message || 'Failed to duplicate file');
    }
  }

  async function downloadFileByName(file: ConfigFileInfo) {
    if (file.path === selectedFile && editorView) {
      downloadFile();
      return;
    }
    try {
      await downloadConfigFile(file);
    } catch (err: any) {
      showToast('error', err?.message || 'Download failed');
    }
  }

  function deleteFileByInfo(file: ConfigFileInfo) {
    selectedFile = file.path;
    deleteFile();
  }

  function openBackupsForFile(file: ConfigFileInfo) {
    selectedFile = file.path;
    loadBackups(file.path);
    drawerOpen = true;
  }

  function toggleSchema() {
    schemaEnabled = !schemaEnabled;
  }

  function toggleExpertMode() {
    expertMode = !expertMode;
  }

  function applyQuickFixes() {
    if (!editorView || !selectedFile) return;

    try {
      const content = editorView.state.doc.toString();
      const { fixed, fixesApplied } = computeQuickFixes(content, selectedFile);

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

  function openTemplatesModal() {
    showTemplatesModal = true;
  }

  async function applyTemplate(template: Template) {
    if (!editorView) return;
    if (isDirty && !(await confirmUnsaved())) return;
    if (
      !(await showConfirm({
        title: $t('editor.template_apply_title'),
        objectName: template.name,
        consequence: $t('editor.confirm_template'),
        variant: 'warning',
        confirmLabel: $t('editor.apply')
      }))
    )
      return;

    templateLoading = true;
    saveWarnings = [];
    try {
      if (!template.content) throw new Error('Template is empty');

      let finalContent = template.content;
      try {
        const currentContent = editorView.state.doc.toString();
        const mergeRes = await apiFetchJSON<{
          content: string;
          warnings?: PreflightWarning[];
        }>('/api/config/smart-merge', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({
            type: template.type,
            existing_content: currentContent,
            template_content: template.content,
            target_file: selectedFile
          })
        });
        if (mergeRes && mergeRes.content) {
          finalContent = mergeRes.content;
          const mergeData: any = mergeRes;
          const w = mergeData?.data?.warnings ?? mergeData?.warnings;
          saveWarnings = Array.isArray(w) ? w : [];
        } else {
          throw new Error('empty merge result');
        }
      } catch (mergeErr: any) {
        if (mergeErr?.status === 401) return;
        console.error('Smart merge failed:', mergeErr);
        showToast('error', $t('editor.smart_merge_failed'));
        return;
      }

      editorView.dispatch({
        changes: { from: 0, to: editorView.state.doc.length, insert: finalContent }
      });
      isDirty = true;
      showTemplatesModal = false;
      showToast('success', $t('editor.template_applied'));
    } catch (e: any) {
      if (e?.status === 401) return;
      showToast('error', $t('editor.template_error') + ': ' + e.message);
    } finally {
      templateLoading = false;
    }
  }

  function handleGeneratedOutbound(content: string) {
    if (!editorView) return;
    const cursor = editorView.state.selection.main.head;
    editorView.dispatch({
      changes: { from: cursor, insert: content }
    });
    showGeneratorModal = false;
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

  let detectedDraft = $state<DraftRecord | null>(null);
  let unregisterDirty: (() => void) | null = null;

  function handleRestoreSessionDraft() {
    if (!detectedDraft?.data) return;
    const draftData = detectedDraft.data;
    if (Array.isArray(draftData.tabs) && draftData.tabs.length > 0) {
      tabs = draftData.tabs.map((t: any) => ({
        path: t.path,
        name: t.name || t.path.split('/').pop() || '',
        isDirty: t.isDirty !== undefined ? t.isDirty : true,
        isPreview: false,
        originalContent: t.originalContent || '',
        currentContent: t.currentContent || ''
      }));
      if (draftData.selectedFile) {
        selectedFile = draftData.selectedFile;
        activeTabPath = draftData.activeTabPath || draftData.selectedFile;
        const cur = tabs.find((t) => t.path === selectedFile);
        if (cur && editorView) {
          editorView.dispatch({
            changes: { from: 0, to: editorView.state.doc.length, insert: cur.currentContent }
          });
        }
      }
      isDirty = tabs.some((t) => t.isDirty);
    }
    clearDraft('editor');
    detectedDraft = null;
    showToast('success', $t('draft.restored_toast'));
  }

  function handleDiscardSessionDraft() {
    clearDraft('editor');
    detectedDraft = null;
    showToast('info', $t('draft.discarded_toast'));
  }

  onMount(() => {
    loadFiles();
    checkHashTab();
    window.addEventListener('hashchange', checkHashTab);

    const draft = getDraft('editor');
    if (draft) {
      detectedDraft = draft;
    }

    unregisterDirty = registerDirtySource('editor', {
      name: $t('nav.editor') || 'Editor',
      isDirty: () => isDirty || tabs.some((t) => t.isDirty),
      onSave: async () => {
        if (selectedFile && editorView) {
          await confirmSave();
          return !isDirty;
        }
        return true;
      },
      getDraft: () => ({
        selectedFile,
        activeTabPath,
        tabs: tabs.map((t) => ({
          path: t.path,
          name: t.name,
          currentContent:
            t.path === selectedFile && editorView
              ? editorView.state.doc.toString()
              : t.currentContent,
          originalContent: t.originalContent,
          isDirty: t.isDirty
        }))
      }),
      restoreDraft: (draftRecord) => {
        if (draftRecord?.data) {
          detectedDraft = draftRecord;
          handleRestoreSessionDraft();
        }
      }
    });
  });

  onDestroy(() => {
    window.removeEventListener('hashchange', checkHashTab);
    if (unregisterDirty) {
      unregisterDirty();
      unregisterDirty = null;
    }
    if (stopPollingStatus) {
      stopPollingStatus();
      stopPollingStatus = null;
    }
  });
</script>

<svelte:window onkeydown={handleGlobalKeydown} />

<div class="editor-page-container" class:constructor-mode={activeTab === 'constructor'}>
  <!-- Level 1 Header (EDIT-01) -->
  <PageHeader
    title={$t('editor.h1')}
    subtitle={$t('editor.h1_sub')}
    breadcrumbs={[
      { label: $t('nav.group_tools') },
      { label: $t('nav.editor') },
      ...(activeTab === 'constructor' ? [{ label: $t('editor.tab_constructor') }] : [])
    ]}
    {onSwitchTab}
    hideHome={true}
  >
    <Tabs
      items={editorModeTabItems}
      value={activeTab}
      onchange={(val) => setTab(val as 'files' | 'constructor')}
      ariaLabel={$t('editor.h1')}
      variant="pill"
    />

    {#if activeTab === 'files'}
      <EditorHeaderActions
        {saveStatusState}
        {selectedFile}
        {loading}
        {saving}
        {applyLoading}
        onReloadFile={() => loadFile(selectedFile)}
        onSaveFile={checkBeforeSave}
        onSaveAndApply={handleSaveAndApply}
      />
    {/if}
  </PageHeader>

  {#if detectedDraft}
    <DraftRestoreBanner
      timestamp={detectedDraft.timestamp}
      onRestore={handleRestoreSessionDraft}
      onDiscard={handleDiscardSessionDraft}
    />
  {/if}

  {#if activeTab === 'files'}
    <!-- Workspace with Resizable Splitter (EDIT-02) -->
    <div class="editor-workspace">
      <EditorSidebar
        show={showSidebar}
        {xrayFiles}
        {mihomoFiles}
        {selectedFile}
        activeKernel={$capabilities?.active_kernel || ''}
        onLoadFile={loadFile}
        onCreateFile={() => {
          showCreateModal = true;
          newFileName = '';
        }}
        onRenameFile={(f) => {
          showRenameModal = true;
          renameTarget = f.name;
          selectedFile = f.path;
        }}
        onDuplicateFile={duplicateFile}
        onDownloadFile={downloadFileByName}
        onDeleteFile={deleteFileByInfo}
        onViewBackups={openBackupsForFile}
      />

      <!-- Main Editor Card -->
      {#if tabs.length === 0}
        <div class="editor-empty-card">
          <div class="editor-empty-content">
            <EmptyState
              title={$t('editor.select_file')}
              description={$t('editor.empty_state_body')}
              icon={EditorIcon}
              plain={true}
            />
            <div class="editor-empty-actions">
              {#if !showSidebar}
                <button class="btn btn-secondary" onclick={() => (showSidebar = true)}>
                  <svg
                    width="14"
                    height="14"
                    viewBox="0 0 24 24"
                    fill="none"
                    stroke="currentColor"
                    stroke-width="2"
                    ><path
                      d="M22 19a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h5l2 3h9a2 2 0 0 1 2 2z"
                    /></svg
                  >
                  {$t('editor.show_files')}
                </button>
              {/if}
              <button
                class="btn btn-primary"
                onclick={() => {
                  showCreateModal = true;
                  newFileName = '';
                }}
              >
                <svg
                  width="14"
                  height="14"
                  viewBox="0 0 24 24"
                  fill="none"
                  stroke="currentColor"
                  stroke-width="2"
                >
                  <line x1="12" y1="5" x2="12" y2="19" />
                  <line x1="5" y1="12" x2="19" y2="12" />
                </svg>
                {$t('editor.create_file')}
              </button>
            </div>

            <div class="editor-empty-shortcuts">
              <span class="shortcut-item"
                ><kbd>{isMac ? '⌘' : 'Ctrl'}+S</kbd> <span>{$t('editor.to_save')}</span></span
              >
              <span class="shortcut-dot">•</span>
              <span class="shortcut-item"
                ><kbd>{isMac ? '⌘' : 'Ctrl'}+F</kbd>
                <span>{$t('editor.shortcut_search')}</span></span
              >
              <span class="shortcut-dot">•</span>
              <span class="shortcut-item"
                ><kbd>{isMac ? '⌘' : 'Ctrl'}+Z</kbd> <span>{$t('editor.shortcut_undo')}</span></span
              >
            </div>
          </div>
        </div>
      {:else}
        <div class="editor-main-card">
          <!-- Level 2 Subhead Bar (EDIT-01) -->
          <EditorToolbar
            {showSidebar}
            {tabs}
            {activeTabPath}
            {selectedFile}
            {hasDraft}
            {fileType}
            activeKernel={$capabilities?.active_kernel}
            onToggleSidebar={() => (showSidebar = !showSidebar)}
            onSwitchTab={switchTab}
            onPinTab={pinTab}
            onCloseTab={closeTab}
            onRestoreDraft={restoreDraft}
            onDiscardDraft={discardDraft}
            onDownloadFile={downloadFile}
            onRenameFile={() => {
              showRenameModal = true;
              renameTarget = selectedFile.split('/').pop() || '';
            }}
            onOpenTemplates={openTemplatesModal}
            onOpenGenerator={() => (showGeneratorModal = true)}
            onApplyQuickFixes={applyQuickFixes}
            onDeleteFile={deleteFile}
          />

          <EditorBreadcrumbs {breadcrumbs} onJump={jumpToSegment} />

          <PreflightWarnings
            warnings={saveWarnings}
            onDismiss={() => {
              saveWarnings = [];
            }}
          />

          <!-- CodeMirror editor component -->
          <div style="flex: 1; min-height: 0; position:relative; background: var(--cm-bg);">
            {#if loading}
              <div
                style="display:grid;place-items:center;height:100%;position:absolute;inset:0;background:color-mix(in srgb, var(--bg-card) 75%, transparent);z-index:10;"
              >
                <div class="spinner" style="--spinner-size: 24px;"></div>
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
                    saveError = false;

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

          <!-- Status Bar (EDIT-05, EDIT-06) -->
          <EditorStatusBar
            {cursorLine}
            {cursorCol}
            {isMac}
            {schemaEnabled}
            {expertMode}
            {applyLoading}
            {backgroundStatusText}
            backupCount={backups.length}
            {drawerOpen}
            onToggleSchema={toggleSchema}
            onToggleExpertMode={toggleExpertMode}
            onToggleDrawer={() => (drawerOpen = !drawerOpen)}
          />

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
    {#key constructorReloadKey}
      {#await import('./Constructor.svelte')}
        <Skeleton type="card" height="60vh" />
      {:then { default: Constructor }}
        <div transition:fade={{ duration: 150 }} style="margin-top: 16px;">
          <Constructor
            {onSwitchTab}
            onInsertIntoEditor={handleInsertIntoEditor}
            {selectedFile}
            embedded={true}
            invalidateCache={activeTab === 'constructor'}
          />
        </div>
      {:catch err}
        <div use:reportConstructorChunkErrorAction={err}>
          <EmptyState
            title={$t('app.chunk_load_failed')}
            description=""
            ctaText={$t('app.retry')}
            oncta={retryConstructorChunkLoad}
          />
        </div>
      {/await}
    {/key}
  {/if}
</div>

<!-- CRUD Modals -->
<FileActionModals
  createOpen={showCreateModal}
  renameOpen={showRenameModal}
  initialRenameValue={renameTarget}
  onCreate={createFile}
  onRename={renameFile}
  onCloseCreate={() => (showCreateModal = false)}
  onCloseRename={() => (showRenameModal = false)}
/>

<!-- Templates Modal -->
<TemplatesModal
  isOpen={showTemplatesModal}
  {selectedFile}
  hasEditorView={!!editorView}
  onApplyTemplate={applyTemplate}
  onClose={() => (showTemplatesModal = false)}
/>

<OutboundGeneratorModal
  isOpen={showGeneratorModal}
  onGenerate={handleGeneratedOutbound}
  onClose={() => (showGeneratorModal = false)}
/>

<SaveConfirmModal
  isOpen={showSaveConfirmModal}
  {originalContent}
  currentContent={editorView ? editorView.state.doc.toString() : ''}
  {saving}
  onConfirm={confirmSave}
  onClose={() => (showSaveConfirmModal = false)}
/>

<DeleteFileModal
  isOpen={showDeleteConfirmModal}
  filePath={selectedFile}
  onConfirm={confirmDeleteFile}
  onClose={() => (showDeleteConfirmModal = false)}
/>

<style>
  .editor-page-container {
    display: flex;
    flex-direction: column;
    height: calc(100vh - 76px);
    min-height: 500px;
    gap: 0;
  }

  .editor-page-container.constructor-mode {
    height: auto;
    min-height: 100%;
    padding-bottom: 60px;
  }

  :global(.page-header-actions .tabs) {
    margin-bottom: 0;
    border-bottom: none;
  }

  .editor-workspace {
    display: flex;
    flex-direction: row;
    flex: 1;
    min-height: 0;
    gap: 0;
    overflow: hidden;
    position: relative;
  }

  .editor-main-card {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
    height: 100%;
    min-height: 0;
    background: var(--bg-card);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    overflow: hidden;
  }

  .editor-empty-card {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    height: 100%;
    min-height: 0;
    background: var(--bg-card);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    overflow: hidden;
    position: relative;
    padding: 32px 24px;
    box-sizing: border-box;
  }

  .editor-empty-content {
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    text-align: center;
    max-width: 520px;
    width: 100%;
  }

  .editor-empty-card :global(.empty-state) {
    justify-content: center;
    background: transparent;
    border: none;
    box-shadow: none;
    padding: 0;
    max-width: 480px;
  }

  .editor-empty-actions {
    display: flex;
    align-items: center;
    justify-content: center;
    gap: 12px;
    margin-top: 20px;
    flex-wrap: wrap;
  }

  .editor-empty-actions .btn {
    display: inline-flex;
    align-items: center;
    gap: 8px;
    padding: 8px 18px;
    font-size: 13px;
    font-weight: 500;
  }

  .editor-empty-shortcuts {
    display: flex;
    align-items: center;
    justify-content: center;
    gap: 10px;
    margin-top: 32px;
    padding-top: 18px;
    border-top: 1px solid color-mix(in srgb, var(--border) 65%, transparent);
    color: var(--fg-muted);
    font-size: 12px;
  }

  .editor-empty-shortcuts .shortcut-item {
    display: inline-flex;
    align-items: center;
    gap: 6px;
  }

  .editor-empty-shortcuts kbd {
    background: var(--surface-tint);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm, 4px);
    padding: 2px 6px;
    font-size: 12px;
    font-family: var(--font-family-mono);
    color: var(--fg-secondary);
    line-height: 1.3;
    box-shadow: var(--shadow-sm);
  }

  .editor-empty-shortcuts .shortcut-dot {
    color: var(--border);
    user-select: none;
  }
</style>
