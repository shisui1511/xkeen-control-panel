<script lang="ts">
  import { onMount } from 'svelte';
  import Modal from './components/Modal.svelte';
  import { t, setLang, currentLang, getAvailableLangs, type Lang } from './i18n';
  import Icon from './lib/components/Icon.svelte';
  import StorageCard from './lib/components/StorageCard.svelte';
  import {
    capabilities,
    fetchCapabilities,
    showToast,
    devMode,
    fetchDevMode,
    setDevMode,
    showConfirm,
    type ThemeDensity,
    applyDensity
  } from './stores';
  import { apiFetch, apiFetchJSON } from './lib/api';
  import MihomoSocketMigrateModal from './components/mihomo/MihomoSocketMigrateModal.svelte';
  import { capsuleConfigStore, updateCapsuleConfig } from './lib/capsuleSettings';
  import PingTargetSettingsCard from './components/PingTargetSettingsCard.svelte';
  import PageHeader from './PageHeader.svelte';
  import UpdatesPanel from './components/settings/UpdatesPanel.svelte';
  import Tabs, { type TabItem } from './components/Tabs.svelte';
  import SegmentedControl, { type SegmentItem } from './components/SegmentedControl.svelte';
  import Select from './components/Select.svelte';
  import Skeleton from './components/Skeleton.svelte';
  import PasswordField from './components/PasswordField.svelte';
  import PasswordStrengthMeter from './components/PasswordStrengthMeter.svelte';
  import { validatePasswordPolicy, policyErrorKey } from './lib/passwordPolicy';

  let { onSwitchTab }: { onSwitchTab?: (tab: string) => void } = $props();

  let showMihomoMigrateModal = $state(false);
  let checkingConnection = $state(false);
  let secretVisible = $state(false);

  async function recheckConnection() {
    checkingConnection = true;
    try {
      await fetchCapabilities();
    } finally {
      checkingConnection = false;
    }
  }

  let version = $state('...');
  let langs = getAvailableLangs();
  type SettingsTab = 'general' | 'updates' | 'security' | 'connection' | 'backups' | 'about';
  const SETTINGS_TABS: SettingsTab[] = [
    'general',
    'updates',
    'security',
    'connection',
    'backups',
    'about'
  ];

  // #/settings?tab=updates — ссылка из уведомления об обновлении
  function tabFromHash(): SettingsTab {
    const query = window.location.hash.split('?')[1] ?? '';
    const tab = new URLSearchParams(query).get('tab') as SettingsTab | null;
    return tab && SETTINGS_TABS.includes(tab) ? tab : 'general';
  }

  let activeTab = $state<SettingsTab>(tabFromHash());

  const settingsTabItems = $derived<TabItem[]>([
    { value: 'general', label: $t('settings.tab_general') },
    { value: 'updates', label: $t('settings.tab_updates') },
    { value: 'security', label: $t('settings.tab_security') },
    { value: 'connection', label: $t('settings.tab_connection') },
    { value: 'backups', label: $t('settings.tab_backups') },
    { value: 'about', label: $t('settings.tab_about') }
  ]);

  const themeItems = $derived<SegmentItem[]>([
    { value: 'light', label: $t('settings.theme_light_btn') },
    { value: 'dark', label: $t('settings.theme_dark_btn') },
    { value: 'auto', label: $t('settings.theme_auto_btn') }
  ]);

  type AccentChoice = 'blue' | 'indigo' | 'steel' | 'graphite';
  const accentItems: { value: AccentChoice; labelKey: string }[] = [
    { value: 'blue', labelKey: 'settings.accent_blue_btn' },
    { value: 'indigo', labelKey: 'settings.accent_indigo_btn' },
    { value: 'steel', labelKey: 'settings.accent_steel_btn' },
    { value: 'graphite', labelKey: 'settings.accent_graphite_btn' }
  ];

  const densityItems = $derived<SegmentItem[]>([
    { value: 'comfortable', label: $t('settings.density_comfortable_btn') },
    { value: 'compact', label: $t('settings.density_compact_btn') },
    { value: 'auto', label: $t('settings.density_auto_btn') }
  ]);

  // Backups state variables
  let configFiles = $state<string[]>([]);
  let selectedFile = $state('');
  let backups = $state<string[]>([]);
  let loadingBackups = $state(false);
  let backupsLoaded = $state(false);

  // Snapshots state
  interface SnapshotMeta {
    id: string;
    label: string;
    created_at: number;
    size_bytes: number;
  }
  let snapshots = $state<SnapshotMeta[]>([]);
  let snapshotLabel = $state('');
  let creatingSnapshot = $state(false);
  let restoringSnapshot = $state('');
  let uploading = $state(false);
  let isDragOver = $state(false);

  async function uploadBackup(file: File) {
    if (!file) return;
    if (!file.name.endsWith('.tar.gz')) {
      showToast('error', 'Invalid file format, only .tar.gz is allowed');
      return;
    }
    uploading = true;
    try {
      const formData = new FormData();
      formData.append('backup', file);

      const res = await apiFetch('/api/snapshots/upload', {
        method: 'POST',
        body: formData
      });

      if (res.ok) {
        showToast('success', $t('settings.snapshot_uploaded'));
        fetchSnapshots();
      } else {
        const errMsg = await res.text();
        showToast('error', $t('settings.snapshot_restore_error', { error: errMsg }));
      }
    } catch (e: any) {
      if (e?.status === 401) return;
      showToast('error', $t('settings.snapshot_restore_error', { error: e.message }));
    } finally {
      uploading = false;
    }
  }

  function handleDragOver(e: DragEvent) {
    e.preventDefault();
    isDragOver = true;
  }

  function handleDragLeave() {
    isDragOver = false;
  }

  function handleDrop(e: DragEvent) {
    e.preventDefault();
    isDragOver = false;
    if (e.dataTransfer?.files && e.dataTransfer.files.length > 0) {
      uploadBackup(e.dataTransfer.files[0]);
    }
  }

  function handleFileSelect(e: Event) {
    const target = e.target as HTMLInputElement;
    if (target.files && target.files.length > 0) {
      uploadBackup(target.files[0]);
    }
  }

  let downloadingDiagnostics = $state(false);

  async function downloadDiagnostics() {
    downloadingDiagnostics = true;
    try {
      const res = await apiFetch('/api/system/diagnostics');
      if (!res.ok) {
        showToast('error', await res.text());
        return;
      }
      const blob = await res.blob();
      const url = URL.createObjectURL(blob);
      const a = document.createElement('a');
      a.href = url;
      a.download = `xcp-diagnostics-${new Date().toISOString().slice(0, 10)}.tar.gz`;
      a.click();
      URL.revokeObjectURL(url);
    } catch (e: any) {
      if (e?.status === 401) return;
      showToast('error', e instanceof Error ? e.message : String(e));
    } finally {
      downloadingDiagnostics = false;
    }
  }

  async function fetchSnapshots() {
    try {
      snapshots = (await apiFetchJSON<any[]>('/api/snapshots/list')) ?? [];
    } catch (e: any) {
      if (e?.status === 401) return;
      showToast('error', e instanceof Error ? e.message : String(e));
    }
  }

  async function createSnapshot() {
    creatingSnapshot = true;
    try {
      await apiFetchJSON('/api/snapshots/create', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ label: snapshotLabel })
      });
      snapshotLabel = '';
      showToast('success', $t('settings.snapshot_created'));
      fetchSnapshots();
    } catch (e: any) {
      if (e?.status === 401) return;
      showToast('error', e.message);
    } finally {
      creatingSnapshot = false;
    }
  }

  async function restoreSnapshot(id: string) {
    const snap = snapshots.find((s) => s.id === id);
    if (
      !(await showConfirm({
        title: $t('settings.snapshot_restore_title'),
        objectName: snap ? snap.label || snap.id : id,
        consequence: $t('settings.snapshot_restore_confirm'),
        variant: 'warning',
        confirmLabel: $t('settings.restore')
      }))
    )
      return;
    restoringSnapshot = id;
    try {
      await apiFetchJSON(`/api/snapshots/${id}/restore`, {
        method: 'POST'
      });
      showToast('success', $t('settings.snapshot_restored'));
    } catch (e: any) {
      if (e?.status === 401) return;
      showToast('error', e.message);
    } finally {
      restoringSnapshot = '';
    }
  }

  async function deleteSnapshot(id: string) {
    const snap = snapshots.find((s) => s.id === id);
    if (
      !(await showConfirm({
        title: $t('settings.snapshot_delete_title'),
        objectName: snap ? snap.label || snap.id : id,
        consequence: $t('settings.snapshot_delete_confirm'),
        variant: 'danger',
        confirmLabel: $t('app.delete')
      }))
    )
      return;
    try {
      await apiFetchJSON(`/api/snapshots/${id}/delete`, {
        method: 'POST'
      });
      showToast('success', $t('settings.snapshot_deleted'));
      fetchSnapshots();
    } catch (e: any) {
      if (e?.status === 401) return;
      showToast('error', e.message);
    }
  }

  function formatSnapshotSize(bytes: number): string {
    if (bytes < 1024) return bytes + ' B';
    if (bytes < 1024 * 1024) return (bytes / 1024).toFixed(1) + ' KB';
    return (bytes / (1024 * 1024)).toFixed(1) + ' MB';
  }

  async function loadConfigFiles() {
    try {
      const xrayRes = await apiFetch('/api/config/list?dir=/opt/etc/xray/configs');
      const mihomoRes = await apiFetch('/api/config/list?dir=/opt/etc/mihomo');

      let files: string[] = [];
      if (xrayRes.ok) {
        const data = await xrayRes.json();
        files = [...files, ...data.map((f: any) => f.path)];
      }
      if (mihomoRes.ok) {
        const data = await mihomoRes.json();
        files = [...files, ...data.map((f: any) => f.path)];
      }

      files.push('/opt/etc/xcp/config.json');

      configFiles = Array.from(new Set(files)).sort();
      if (configFiles.length > 0 && !selectedFile) {
        selectedFile = configFiles[0];
        fetchBackups();
      }
    } catch (e: any) {
      if (e?.status === 401) return;
      configFiles = [];
      showToast(
        'error',
        `${$t('settings.backup_list_error')}: ${e instanceof Error ? e.message : String(e)}`
      );
      console.error(e);
    }
  }

  async function fetchBackups() {
    if (!selectedFile) return;
    loadingBackups = true;
    try {
      const res = await apiFetch(`/api/config/backups?path=${encodeURIComponent(selectedFile)}`);
      if (res.ok) {
        backups = (await res.json()) ?? [];
      } else {
        backups = [];
        const txt = await res.text();
        showToast('error', `${$t('settings.backups_fetch_error')}: ${txt}`);
        console.error(new Error(`Failed to fetch backups: ${txt}`));
      }
    } catch (e: any) {
      if (e?.status === 401) return;
      backups = [];
      showToast(
        'error',
        `${$t('settings.backups_fetch_error')}: ${e instanceof Error ? e.message : String(e)}`
      );
      console.error(e);
    } finally {
      loadingBackups = false;
    }
  }

  async function restoreBackup(backupPath: string) {
    const filename = backupPath.split('/').pop() || backupPath;
    if (
      !(await showConfirm({
        title: $t('settings.backup_restore_title'),
        objectName: filename,
        consequence: $t('settings.backup_restore_confirm'),
        variant: 'warning',
        confirmLabel: $t('settings.restore')
      }))
    )
      return;
    const targetFile = selectedFile; // capture before any await to prevent TOCTOU race
    if (!targetFile) return;
    try {
      const readRes = await apiFetch(`/api/config/read?path=${encodeURIComponent(backupPath)}`);
      if (!readRes.ok) {
        const txt = await readRes.text();
        showToast('error', `${$t('settings.backup_read_error')}: ${txt}`);
        return;
      }
      const data = await readRes.text();

      const saveRes = await apiFetch(`/api/config/save?path=${encodeURIComponent(targetFile)}`, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json'
        },
        body: data
      });
      if (saveRes.ok) {
        showToast('success', $t('settings.backup_restore_success'));
        fetchBackups();
      } else {
        const txt = await saveRes.text();
        showToast('error', `${$t('settings.backup_restore_error')}: ${txt}`);
      }
    } catch (e: any) {
      if (e?.status === 401) return;
      showToast('error', e.message);
    }
  }

  async function deleteBackup(backupPath: string) {
    const filename = backupPath.split('/').pop() || backupPath;
    if (
      !(await showConfirm({
        title: $t('settings.backup_delete_title'),
        objectName: filename,
        consequence: $t('settings.backup_delete_confirm'),
        variant: 'danger',
        confirmLabel: $t('app.delete')
      }))
    )
      return;
    try {
      const res = await apiFetch(`/api/config/delete?path=${encodeURIComponent(backupPath)}`, {
        method: 'POST'
      });
      if (res.ok) {
        showToast('success', $t('settings.backup_delete_success'));
        fetchBackups();
      } else {
        const txt = await res.text();
        showToast('error', `${$t('settings.backup_delete_error')}: ${txt}`);
      }
    } catch (e: any) {
      if (e?.status === 401) return;
      showToast('error', e.message);
    }
  }

  async function createBackup() {
    if (!selectedFile) return;
    try {
      const readRes = await apiFetch(`/api/config/read?path=${encodeURIComponent(selectedFile)}`);
      if (!readRes.ok) {
        showToast('error', await readRes.text());
        return;
      }
      const data = await readRes.text();
      const saveRes = await apiFetch(`/api/config/save?path=${encodeURIComponent(selectedFile)}`, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json'
        },
        body: data
      });
      if (saveRes.ok) {
        showToast('success', $t('settings.backup_created'));
        await fetchBackups();
      } else {
        showToast('error', await saveRes.text());
      }
    } catch (e: any) {
      if (e?.status === 401) return;
      showToast('error', e.message);
    }
  }

  $effect(() => {
    if (activeTab === 'backups' && !backupsLoaded) {
      backupsLoaded = true;
      loadConfigFiles();
      fetchSnapshots();
    } else if (activeTab !== 'backups') {
      backupsLoaded = false;
    }
  });

  // Appearance & Behavior settings (persisted in localStorage)
  let selectedTheme = $state<'light' | 'dark' | 'auto'>('auto');
  let selectedAccent = $state<AccentChoice>('blue');
  let selectedDensity = $state<ThemeDensity>('auto');
  let systemTimezone = $state('—');
  let animationsEnabled = $state(true);
  let autoRefresh = $state(true);
  let confirmDangerous = $state(true);
  let notificationSound = $state(false);

  async function loadSystemTimezone() {
    try {
      const stats = await apiFetchJSON<{ timezone?: string }>('/api/system/stats');
      if (stats?.timezone) {
        systemTimezone = stats.timezone;
      }
    } catch {}
  }

  function loadAppearanceSettings() {
    try {
      const saved = localStorage.getItem('theme') || '';
      selectedTheme = saved === 'light' || saved === 'dark' ? saved : 'auto';
      const savedAccent = localStorage.getItem('accent') || '';
      selectedAccent =
        savedAccent === 'indigo' || savedAccent === 'steel' || savedAccent === 'graphite'
          ? savedAccent
          : 'blue';
      const savedDensity = localStorage.getItem('theme_density');
      selectedDensity =
        savedDensity === 'comfortable' || savedDensity === 'compact' ? savedDensity : 'auto';
      animationsEnabled = localStorage.getItem('animations') !== 'false';
      autoRefresh = localStorage.getItem('autoRefresh') !== 'false';
      confirmDangerous = localStorage.getItem('confirmDangerous') !== 'false';
      notificationSound = localStorage.getItem('notificationSound') === 'true';
    } catch {}
  }

  function setTheme(t: 'light' | 'dark' | 'auto') {
    selectedTheme = t;
    try {
      if (t === 'auto') {
        localStorage.removeItem('theme');
        const prefersDark = window.matchMedia('(prefers-color-scheme: dark)').matches;
        document.documentElement.setAttribute('data-theme', prefersDark ? 'dark' : 'light');
      } else {
        localStorage.setItem('theme', t);
        document.documentElement.setAttribute('data-theme', t);
      }
    } catch {}
  }

  function setAccent(a: AccentChoice) {
    selectedAccent = a;
    try {
      if (a === 'blue') {
        localStorage.removeItem('accent');
        document.documentElement.removeAttribute('data-accent');
      } else {
        localStorage.setItem('accent', a);
        document.documentElement.setAttribute('data-accent', a);
      }
    } catch {}
  }

  function setDensity(d: ThemeDensity) {
    selectedDensity = d;
    applyDensity(d);
  }

  function saveSetting(key: string, value: string) {
    try {
      localStorage.setItem(key, value);
    } catch {}
  }

  // Change password
  let currentPassword = $state('');
  let newPassword = $state('');
  let confirmPassword = $state('');
  let passwordChanging = $state(false);
  let passwordError = $state('');
  let passwordSuccess = $state(false);

  async function changePassword() {
    passwordError = '';
    passwordSuccess = false;
    if (newPassword !== confirmPassword) {
      passwordError = $t('settings.password_mismatch');
      return;
    }
    const policyCode = validatePasswordPolicy(newPassword, { current: currentPassword });
    if (policyCode) {
      const key = policyErrorKey(policyCode);
      passwordError = key ? $t(key) : $t('settings.password_error');
      return;
    }
    passwordChanging = true;
    try {
      const res = await apiFetch('/api/auth/change-password', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ current_password: currentPassword, new_password: newPassword })
      });
      let payload: any = null;
      try {
        payload = await res.json();
      } catch {
        // тело ответа не JSON
      }
      if (res.ok) {
        if (payload?.csrf_token) {
          localStorage.setItem('csrf_token', payload.csrf_token);
        }
        passwordSuccess = true;
        currentPassword = '';
        newPassword = '';
        confirmPassword = '';
        loadSessions();
      } else {
        const key = policyErrorKey(payload?.code);
        passwordError = (key ? $t(key) : payload?.error) || $t('settings.password_error');
      }
    } catch (e: any) {
      if (e?.status === 401) return;
      passwordError = e?.message || $t('settings.password_error');
    } finally {
      passwordChanging = false;
    }
  }

  // Active sessions — «Активные сессии» (D-10, SESS-02)
  interface SessionInfo {
    id: string;
    browser: string;
    os: string;
    ip: string;
    created_at: string;
    last_seen: string;
    current: boolean;
  }

  let sessions = $state<SessionInfo[]>([]);
  let sessionsLoading = $state(false);
  let sessionsError = $state('');
  let terminatingSessionId = $state('');
  let terminatingOthers = $state(false);

  function sessionDeviceLabel(s: SessionInfo): string {
    const parts = [s.browser, s.os].filter((p) => p && p.trim() !== '');
    return parts.length > 0 ? parts.join(' · ') : $t('settings.sessions_unknown_device');
  }

  function formatSessionTime(value: string): string {
    const date = new Date(value);
    if (Number.isNaN(date.getTime())) return value;
    return date.toLocaleString($currentLang, { dateStyle: 'short', timeStyle: 'short' });
  }

  async function loadSessions() {
    sessionsLoading = true;
    sessionsError = '';
    try {
      sessions = (await apiFetchJSON<SessionInfo[]>('/api/auth/sessions')) ?? [];
    } catch (e: any) {
      if (e?.status === 401) return;
      sessionsError = $t('settings.sessions_load_error');
    } finally {
      sessionsLoading = false;
    }
  }

  async function terminateSession(s: SessionInfo) {
    const ok = await showConfirm({
      title: $t('settings.sessions_terminate_confirm_title'),
      objectName: `${sessionDeviceLabel(s)} · ${s.ip}`,
      consequence: $t('settings.sessions_terminate_consequence'),
      confirmLabel: $t('settings.sessions_terminate_confirm'),
      variant: 'danger'
    });
    if (!ok) return;
    terminatingSessionId = s.id;
    try {
      await apiFetchJSON('/api/auth/sessions/terminate', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ id: s.id })
      });
      showToast('success', $t('settings.sessions_terminated'));
      await loadSessions();
    } catch (e: any) {
      if (e?.status === 401) return;
      showToast('error', e instanceof Error ? e.message : String(e));
    } finally {
      terminatingSessionId = '';
    }
  }

  async function terminateOtherSessions() {
    const ok = await showConfirm({
      title: $t('settings.sessions_terminate_all_confirm_title'),
      message: $t('settings.sessions_terminate_all_message'),
      consequence: $t('settings.sessions_terminate_all_consequence'),
      confirmLabel: $t('settings.sessions_terminate_all_confirm'),
      variant: 'danger'
    });
    if (!ok) return;
    terminatingOthers = true;
    try {
      await apiFetchJSON('/api/auth/sessions/terminate-others', { method: 'POST' });
      showToast('success', $t('settings.sessions_terminated_all'));
      await loadSessions();
    } catch (e: any) {
      if (e?.status === 401) return;
      showToast('error', e instanceof Error ? e.message : String(e));
    } finally {
      terminatingOthers = false;
    }
  }

  // Время жизни сессии — «Время жизни сессии» (D-06)
  interface SessionTTLSettings {
    idle_ttl_hours: number;
    absolute_ttl_days: number;
    idle_ttl_min: number;
    idle_ttl_max: number;
    absolute_ttl_min: number;
    absolute_ttl_max: number;
  }

  let sessionTTL = $state<SessionTTLSettings | null>(null);
  let ttlLoading = $state(false);
  let ttlSaving = $state(false);
  let ttlError = $state('');
  // <input type="number" bind:value> в Svelte 5 биндит число, а не строку;
  // пустое поле отдаёт '' (не 0) — используем это же для «поле очищено».
  let idleTtlInput = $state<number | ''>('');
  let absoluteTtlInput = $state<number | ''>('');

  function parseTtlField(raw: number | ''): number | null {
    if (raw === '' || raw === null || raw === undefined) return null;
    if (!Number.isInteger(raw)) return null;
    return raw;
  }

  const idleTtlValue = $derived(parseTtlField(idleTtlInput));
  const absoluteTtlValue = $derived(parseTtlField(absoluteTtlInput));

  const idleTtlInvalid = $derived(
    sessionTTL !== null &&
      (idleTtlValue === null ||
        idleTtlValue < sessionTTL.idle_ttl_min ||
        idleTtlValue > sessionTTL.idle_ttl_max)
  );
  const absoluteTtlInvalid = $derived(
    sessionTTL !== null &&
      (absoluteTtlValue === null ||
        absoluteTtlValue < sessionTTL.absolute_ttl_min ||
        absoluteTtlValue > sessionTTL.absolute_ttl_max)
  );
  const ttlUnchanged = $derived(
    sessionTTL !== null &&
      idleTtlValue === sessionTTL.idle_ttl_hours &&
      absoluteTtlValue === sessionTTL.absolute_ttl_days
  );
  const ttlSaveDisabled = $derived(
    sessionTTL === null ||
      ttlLoading ||
      ttlSaving ||
      idleTtlInvalid ||
      absoluteTtlInvalid ||
      ttlUnchanged
  );

  async function loadSessionTTL() {
    ttlLoading = true;
    ttlError = '';
    try {
      const data = await apiFetchJSON<SessionTTLSettings>('/api/settings/session');
      sessionTTL = data;
      idleTtlInput = data.idle_ttl_hours;
      absoluteTtlInput = data.absolute_ttl_days;
    } catch (e: any) {
      if (e?.status === 401) return;
      showToast('error', e instanceof Error ? e.message : String(e));
    } finally {
      ttlLoading = false;
    }
  }

  async function saveSessionTTL() {
    if (!sessionTTL || idleTtlValue === null || absoluteTtlValue === null) return;
    ttlSaving = true;
    ttlError = '';
    try {
      const data = await apiFetchJSON<SessionTTLSettings>('/api/settings/session', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          idle_ttl_hours: idleTtlValue,
          absolute_ttl_days: absoluteTtlValue
        })
      });
      sessionTTL = data;
      idleTtlInput = data.idle_ttl_hours;
      absoluteTtlInput = data.absolute_ttl_days;
      showToast('success', $t('settings.session_ttl_saved'));
    } catch (e: any) {
      if (e?.status === 401) return;
      ttlError = e?.message || $t('settings.session_ttl_range_error');
    } finally {
      ttlSaving = false;
    }
  }

  let securityLoaded = $state(false);
  $effect(() => {
    if (activeTab === 'security' && !securityLoaded) {
      securityLoaded = true;
      loadSessions();
      loadSessionTTL();
    } else if (activeTab !== 'security') {
      securityLoaded = false;
    }
  });

  async function fetchVersion() {
    try {
      const res = await apiFetch('/api/version');
      if (!res.ok) {
        version = $t('app.unavailable');
        return;
      }
      const data = await res.json();
      version = data.panel_version || data.version || $t('app.unavailable');
    } catch (e: any) {
      if (e?.status === 401) return;
      version = $t('app.unavailable');
    }
  }

  function handleLangChange(e: Event) {
    const select = e.target as HTMLSelectElement;
    setLang(select.value as Lang);
  }

  onMount(async () => {
    fetchVersion();
    fetchCapabilities();
    fetchDevMode();
    loadAppearanceSettings();
    loadSystemTimezone();
  });
</script>

<div class="container">
  <PageHeader
    title={$t('settings.h1')}
    subtitle={$t('settings.h1_sub')}
    breadcrumbs={[{ label: $t('nav.group_system') }, { label: $t('settings.h1') }]}
    {onSwitchTab}
  />

  <Tabs bind:value={activeTab} items={settingsTabItems} ariaLabel={$t('settings.h1')} />

  <!-- General tab -->
  {#if activeTab === 'general'}
    <div class="card mb-2">
      <div class="card-label">{$t('settings.section_locale')}</div>
      <div class="field-group">
        <div class="field-row">
          <span class="field-row-name">{$t('settings.language')}</span>
          <Select
            class="field-select"
            value={$currentLang}
            onchange={handleLangChange}
            title={$t('settings.language')}
          >
            {#each langs as lang (lang.code)}
              <option value={lang.code}>{lang.name}</option>
            {/each}
          </Select>
        </div>
        <div class="field-row">
          <div>
            <span class="field-row-name">{$t('settings.timezone')}</span>
            <div class="field-row-desc">{$t('settings.timezone_desc')}</div>
          </div>
          <div class="field-value-badge" title={$t('settings.timezone')}>
            {systemTimezone}
          </div>
        </div>
      </div>
    </div>

    <StorageCard />

    <div class="card mb-2">
      <div class="card-label">{$t('settings.section_appearance')}</div>
      <div class="field-group">
        <div class="field-row">
          <div>
            <span class="field-row-name">{$t('settings.theme')}</span>
            <div class="field-row-desc">{$t('settings.theme_desc')}</div>
          </div>
          <SegmentedControl
            items={themeItems}
            value={selectedTheme}
            ariaLabel={$t('settings.theme')}
            onchange={(val) => setTheme(val as 'light' | 'dark' | 'auto')}
          />
        </div>
        <div class="field-row">
          <div>
            <span class="field-row-name">{$t('settings.accent')}</span>
            <div class="field-row-desc">{$t('settings.accent_desc')}</div>
          </div>
          <div class="accent-picker" role="group" aria-label={$t('settings.accent')}>
            {#each accentItems as item (item.value)}
              <button
                type="button"
                class="accent-swatch accent-swatch--{item.value}"
                class:is-active={selectedAccent === item.value}
                aria-pressed={selectedAccent === item.value}
                title={$t(item.labelKey)}
                onclick={() => setAccent(item.value)}
              >
                {#if selectedAccent === item.value}
                  <Icon name="check" size={13} />
                {/if}
              </button>
            {/each}
          </div>
        </div>
        <div class="field-row">
          <div>
            <span class="field-row-name">{$t('settings.density')}</span>
            <div class="field-row-desc">{$t('settings.density_desc')}</div>
          </div>
          <SegmentedControl
            items={densityItems}
            value={selectedDensity}
            ariaLabel={$t('settings.density')}
            onchange={(val) => setDensity(val as ThemeDensity)}
          />
        </div>
        <div class="field-row">
          <div>
            <span class="field-row-name">{$t('settings.animations')}</span>
            <div class="field-row-desc">{$t('settings.animations_desc')}</div>
          </div>
          <label class="toggle-switch">
            <input
              type="checkbox"
              aria-label={$t('settings.animations')}
              bind:checked={animationsEnabled}
              onchange={() => saveSetting('animations', String(animationsEnabled))}
            />
            <span class="toggle-slider"></span>
          </label>
        </div>
      </div>
    </div>

    <div class="card mb-2">
      <div class="card-label">{$t('settings.section_capsule')}</div>
      <div class="field-group">
        <div class="field-row">
          <div>
            <span class="field-row-name">{$t('settings.capsule_visible')}</span>
            <div class="field-row-desc">{$t('settings.capsule_visible_desc')}</div>
          </div>
          <label class="toggle-switch">
            <input
              type="checkbox"
              aria-label={$t('settings.capsule_visible')}
              checked={$capsuleConfigStore.visible}
              onchange={(e) =>
                updateCapsuleConfig({ visible: (e.target as HTMLInputElement).checked })}
            />
            <span class="toggle-slider"></span>
          </label>
        </div>
        {#if $capsuleConfigStore.visible}
          <div class="field-row">
            <div>
              <span class="field-row-name">{$t('settings.capsule_traffic')}</span>
              <div class="field-row-desc">{$t('settings.capsule_traffic_desc')}</div>
            </div>
            <label class="toggle-switch">
              <input
                type="checkbox"
                aria-label={$t('settings.capsule_traffic')}
                checked={$capsuleConfigStore.showTraffic}
                onchange={(e) =>
                  updateCapsuleConfig({ showTraffic: (e.target as HTMLInputElement).checked })}
              />
              <span class="toggle-slider"></span>
            </label>
          </div>
          <div class="field-row">
            <div>
              <span class="field-row-name">{$t('settings.capsule_resources')}</span>
              <div class="field-row-desc">{$t('settings.capsule_resources_desc')}</div>
            </div>
            <label class="toggle-switch">
              <input
                type="checkbox"
                aria-label={$t('settings.capsule_resources')}
                checked={$capsuleConfigStore.showResources}
                onchange={(e) =>
                  updateCapsuleConfig({ showResources: (e.target as HTMLInputElement).checked })}
              />
              <span class="toggle-slider"></span>
            </label>
          </div>
        {/if}
      </div>
    </div>

    <div class="card mb-2">
      <div class="card-label">{$t('settings.section_behavior')}</div>
      <div class="field-group">
        <div class="field-row">
          <div>
            <span class="field-row-name">{$t('settings.auto_refresh')}</span>
            <div class="field-row-desc">{$t('settings.auto_refresh_desc')}</div>
          </div>
          <label class="toggle-switch">
            <input
              type="checkbox"
              aria-label={$t('settings.auto_refresh')}
              bind:checked={autoRefresh}
              onchange={() => saveSetting('autoRefresh', String(autoRefresh))}
            />
            <span class="toggle-slider"></span>
          </label>
        </div>
        <div class="field-row">
          <div>
            <span class="field-row-name">{$t('settings.confirm_dangerous')}</span>
            <div class="field-row-desc">{$t('settings.confirm_dangerous_desc')}</div>
          </div>
          <label class="toggle-switch">
            <input
              type="checkbox"
              aria-label={$t('settings.confirm_dangerous')}
              bind:checked={confirmDangerous}
              onchange={() => saveSetting('confirmDangerous', String(confirmDangerous))}
            />
            <span class="toggle-slider"></span>
          </label>
        </div>
        <div class="field-row">
          <div>
            <span class="field-row-name">{$t('settings.notification_sound')}</span>
            <div class="field-row-desc">{$t('settings.notification_sound_desc')}</div>
          </div>
          <label class="toggle-switch">
            <input
              type="checkbox"
              aria-label={$t('settings.notification_sound')}
              bind:checked={notificationSound}
              onchange={() => saveSetting('notificationSound', String(notificationSound))}
            />
            <span class="toggle-slider"></span>
          </label>
        </div>
        <div class="field-row">
          <div>
            <span class="field-row-name">{$t('settings.dev_mode')}</span>
            <div class="field-row-desc">{$t('settings.dev_mode_desc')}</div>
          </div>
          <label class="toggle-switch">
            <input
              type="checkbox"
              aria-label={$t('settings.dev_mode')}
              checked={$devMode}
              onchange={(e) => setDevMode((e.target as HTMLInputElement).checked)}
            />
            <span class="toggle-slider"></span>
          </label>
        </div>
      </div>
    </div>

    <PingTargetSettingsCard />
  {/if}

  <!-- Updates tab -->
  {#if activeTab === 'updates'}
    <UpdatesPanel {version} />
  {/if}

  <!-- Backups tab -->
  {#if activeTab === 'backups'}
    <div class="card settings-card" style="margin-bottom:18px;">
      <div class="card-label">{$t('settings.section_file_backups')}</div>
      <div class="field-group">
        <div class="field-row">
          <div>
            <div class="lbl">{$t('settings.backup_file')}</div>
            <div class="desc">{$t('settings.backups_desc')}</div>
          </div>
          <div class="ctrl">
            <Select
              class="input"
              style="min-width: 250px;"
              bind:value={selectedFile}
              onchange={fetchBackups}
              title={$t('settings.backup_file')}
            >
              {#each configFiles as file (file)}
                <option value={file}>{file}</option>
              {:else}
                <option value="">{$t('settings.no_files')}</option>
              {/each}
            </Select>
            <button class="btn btn-primary btn-sm" onclick={createBackup} disabled={!selectedFile}>
              {$t('settings.backup_create_btn')}
            </button>
          </div>
        </div>
      </div>

      <!-- Backups table -->
      <div class="field-group" style="border:0;">
        {#if !selectedFile}
          <div style="padding:20px;text-align:center;color:var(--fg-dim);font-style:italic;">
            {$t('settings.backup_select_file_hint')}
          </div>
        {:else if loadingBackups}
          <div style="padding:20px;text-align:center;color:var(--fg-dim);">{$t('app.loading')}</div>
        {:else if backups.length === 0}
          <div style="padding:20px;text-align:center;color:var(--fg-dim);font-style:italic;">
            {$t('settings.backups_empty')}
          </div>
        {:else}
          {#each backups as backup (backup)}
            <div class="field-row">
              <div>
                <div class="lbl mono">{backup.split('/').pop()}</div>
                <div class="desc mono" style="font-size: 12px; color: var(--fg-dim);">{backup}</div>
              </div>
              <div class="ctrl">
                <button
                  class="btn btn-secondary btn-sm"
                  onclick={() => restoreBackup(backup)}
                  title={$t('settings.backup_restore_btn')}
                >
                  {$t('settings.backup_restore_btn')}
                </button>
                <button
                  class="btn btn-danger btn-sm"
                  onclick={() => deleteBackup(backup)}
                  title={$t('app.delete')}
                >
                  {$t('app.delete')}
                </button>
              </div>
            </div>
          {/each}
        {/if}
      </div>
    </div>

    <!-- Divider -->
    <div style="border-top: 1px solid var(--border); margin: 24px 0;"></div>

    <!-- Section 2: Snapshots -->
    <div
      class="card premium-backup-card"
      style="margin-top: 0; background: var(--bg-card); border: 1px solid var(--border); transition: all 0.3s ease;"
    >
      <div
        class="card-title-row"
        style="display: flex; align-items: center; justify-content: space-between; margin-bottom: 20px;"
      >
        <div style="display: flex; align-items: center; gap: 10px;">
          <Icon name="archive" size={20} color="var(--accent)" />
          <h2
            class="card-title"
            style="margin: 0; font-size: 1.1rem; color: var(--fg-primary); font-weight: 600;"
          >
            {$t('settings.section_snapshots')}
          </h2>
        </div>
      </div>

      <!-- Control panel to create a backup -->
      <div
        class="field-row select-row"
        style="margin-bottom: 20px; gap: 12px; align-items: center; background: var(--surface-tint); border: 1px solid var(--border-light); padding: 12px; border-radius: var(--radius-md);"
      >
        <div style="flex: 1; display: flex; flex-direction: column; gap: 4px;">
          <input
            class="input"
            type="text"
            placeholder={$t('settings.snapshot_label_placeholder')}
            bind:value={snapshotLabel}
            disabled={creatingSnapshot || uploading || restoringSnapshot !== ''}
          />
        </div>
        <button
          class="btn btn-primary"
          style="min-width: 150px;"
          onclick={createSnapshot}
          disabled={creatingSnapshot || uploading || restoringSnapshot !== ''}
        >
          {#if creatingSnapshot}
            <span
              class="spinner"
              style="--spinner-size: 14px; --spinner-track: color-mix(in srgb, currentColor 30%, transparent); --spinner-color: currentColor;"
            ></span>
            <span>{$t('app.loading')}</span>
          {:else}
            <Icon name="plus" size={16} />
            <span>{$t('settings.snapshot_create_btn')}</span>
          {/if}
        </button>
      </div>

      <!-- Interactive backup table -->
      <div class="table-container" style="margin-bottom: 20px;">
        {#if snapshots.length === 0}
          <div
            style="display: flex; flex-direction: column; align-items: center; justify-content: center; padding: 40px 20px; text-align: center; gap: 12px;"
          >
            <Icon name="database" size={40} color="var(--fg-dim)" />
            <div style="font-weight: 500; font-size: 15px; color: var(--fg-primary);">
              {$t('settings.snapshots_empty')}
            </div>
            <div style="color: var(--fg-dim); font-size: 12px; max-width: 380px;">
              {$t('settings.snapshots_empty_desc')}
            </div>
          </div>
        {:else}
          <table style="width: 100%; border-collapse: collapse; text-align: left; font-size: 13px;">
            <thead>
              <tr style="background: rgba(0, 0, 0, 0.18); border-bottom: 1px solid var(--border);">
                <th style="padding: 12px 16px; color: var(--fg-dim); font-weight: 500; width: 18%;"
                  >ID</th
                >
                <th style="padding: 12px 16px; color: var(--fg-dim); font-weight: 500;"
                  >{$t('settings.comment')}</th
                >
                <th style="padding: 12px 16px; color: var(--fg-dim); font-weight: 500; width: 12%;"
                  >{$t('settings.size')}</th
                >
                <th style="padding: 12px 16px; color: var(--fg-dim); font-weight: 500; width: 22%;"
                  >{$t('settings.created_at')}</th
                >
                <th
                  style="padding: 12px 16px; color: var(--fg-dim); font-weight: 500; width: 25%; text-align: right;"
                  >{$t('settings.actions')}</th
                >
              </tr>
            </thead>
            <tbody>
              {#each snapshots as snap (snap.id)}
                <tr
                  class="backup-tr"
                  style="border-bottom: 1px solid var(--border-light); transition: background 0.2s ease;"
                >
                  <td
                    style="padding: 12px 16px; font-family: var(--font-mono); color: var(--fg-primary);"
                    >{snap.id}</td
                  >
                  <td style="padding: 12px 16px; color: var(--fg-primary); font-weight: 500;"
                    >{snap.label || '—'}</td
                  >
                  <td style="padding: 12px 16px; color: var(--fg-secondary);"
                    >{formatSnapshotSize(snap.size_bytes)}</td
                  >
                  <td style="padding: 12px 16px; color: var(--fg-secondary);"
                    >{new Date(snap.created_at * 1000).toLocaleString()}</td
                  >
                  <td style="padding: 12px 16px; text-align: right;">
                    <div style="display: flex; gap: 8px; justify-content: flex-end;">
                      <a
                        class="btn btn-secondary btn-sm"
                        style="display: inline-flex; align-items: center; gap: 4px; padding: 6px 12px;"
                        href="/api/snapshots/{snap.id}/download"
                        download
                        title={$t('settings.snapshot_download_btn')}
                      >
                        <Icon name="download" size={14} />
                        <span>{$t('settings.snapshot_download_btn')}</span>
                      </a>
                      <button
                        class="btn btn-secondary btn-sm"
                        style="display: inline-flex; align-items: center; gap: 4px; padding: 6px 12px;"
                        onclick={() => restoreSnapshot(snap.id)}
                        disabled={creatingSnapshot || uploading || restoringSnapshot !== ''}
                      >
                        {#if restoringSnapshot === snap.id}
                          <span
                            class="spinner"
                            style="--spinner-size: 12px; --spinner-track: color-mix(in srgb, currentColor 30%, transparent); --spinner-color: currentColor;"
                          ></span>
                          <span>{$t('app.loading')}</span>
                        {:else}
                          <Icon name="refresh" size={14} />
                          <span>{$t('settings.snapshot_restore_btn')}</span>
                        {/if}
                      </button>
                      <button
                        class="btn btn-danger btn-sm"
                        style="display: inline-flex; align-items: center; gap: 4px; padding: 6px 12px;"
                        onclick={() => deleteSnapshot(snap.id)}
                        disabled={creatingSnapshot || uploading || restoringSnapshot !== ''}
                      >
                        <Icon name="trash" size={14} />
                        <span>{$t('settings.snapshot_delete_btn')}</span>
                      </button>
                    </div>
                  </td>
                </tr>
              {/each}
            </tbody>
          </table>
        {/if}
      </div>

      <!-- Drag-and-Drop Dropzone -->
      <button
        type="button"
        class="backup-dropzone {isDragOver ? 'drag-over' : ''} {uploading ? 'uploading' : ''}"
        disabled={uploading}
        style="width: 100%; border-radius: var(--radius-md); padding: 30px 20px; text-align: center; display: flex; flex-direction: column; align-items: center; justify-content: center; border: 2px dashed {isDragOver
          ? 'var(--accent)'
          : 'var(--border)'}; background: {isDragOver
          ? 'var(--accent-soft)'
          : 'var(--bg-elevated)'}; font: inherit; color: inherit; gap: 8px; transition: all 0.3s ease; position: relative;"
        ondragover={handleDragOver}
        ondragleave={handleDragLeave}
        ondrop={handleDrop}
        onclick={() => {
          if (!uploading) document.getElementById('backup-upload-input')?.click();
        }}
      >
        {#if uploading}
          <span class="spinner" style="--spinner-size: 30px; --spinner-w: 3px; margin-bottom: 8px;"
          ></span>
          <div style="font-weight: 500; color: var(--fg-primary); font-size: 14px;">
            {$t('settings.snapshot_uploading')}
          </div>
        {:else}
          <span style="transition: color 0.3s; margin-bottom: 4px; display: inline-flex;">
            <Icon name="upload" size={32} color={isDragOver ? 'var(--accent)' : 'var(--fg-dim)'} />
          </span>
          <div style="font-weight: 500; color: var(--fg-primary); font-size: 14px;">
            {isDragOver ? $t('settings.drop_file_to_upload') : $t('settings.select_or_drag_file')}
          </div>
          <div style="color: var(--fg-dim); font-size: 12px;">
            {$t('settings.supported_file_types')}
          </div>
        {/if}
      </button>
      <input
        id="backup-upload-input"
        type="file"
        accept=".tar.gz"
        style="display: none;"
        onchange={handleFileSelect}
        disabled={uploading || creatingSnapshot || restoringSnapshot !== ''}
      />
    </div>
  {/if}

  <!-- Connection tab -->
  {#if activeTab === 'connection'}
    <div class="card mb-2">
      <div class="card-label">{$t('settings.mihomo_api')}</div>
      <div class="field-group">
        {#if $capabilities?.mihomo.api_url}
          <div class="field-row">
            <span class="field-row-name">{$t('settings.mihomo_api_url')}</span>
            <span class="field-row-val mono">{$capabilities.mihomo.api_url}</span>
          </div>
        {/if}
        <div class="field-row">
          <span class="field-row-name">{$t('mihomo.controller_type')}</span>
          <span class="field-row-val" style="display:flex;align-items:center;gap:8px;">
            {#if $capabilities?.mihomo?.controller_type === 'unix'}
              <span class="status-ok">● {$t('mihomo.controller_mode_unix')}</span>
            {:else if $capabilities?.mihomo?.is_insecure_lan}
              <span class="status-err"
                >▲ {$capabilities?.mihomo?.controller_target || '0.0.0.0:9090'} ({$t(
                  'mihomo.controller_mode_insecure'
                )})</span
              >
              <button
                class="btn btn-warning btn-sm"
                onclick={() => (showMihomoMigrateModal = true)}
              >
                {$t('mihomo.migrate_btn')}
              </button>
            {:else}
              <span class="status-ok">● {$capabilities?.mihomo?.controller_target || 'TCP'}</span>
            {/if}
          </span>
        </div>
        <div class="field-row">
          <span class="field-row-name">{$t('settings.mihomo_status')}</span>
          <span class="field-row-val">
            {#if $capabilities?.mihomo.process_running}
              <span class="status-ok">● {$t('settings.mihomo_running')}</span>
            {:else}
              <span class="status-err">○ {$t('settings.mihomo_stopped')}</span>
            {/if}
          </span>
        </div>
        <div class="field-row">
          <span class="field-row-name">{$t('settings.mihomo_api_reachable')}</span>
          <span class="field-row-val">
            {#if $capabilities?.mihomo.api_reachable}
              <span class="status-ok">{$t('settings.mihomo_yes')}</span>
            {:else}
              <span class="status-err">{$t('settings.mihomo_no')}</span>
            {/if}
          </span>
        </div>
        <div class="field-row">
          <span class="field-row-name">{$t('settings.mihomo_api_auth')}</span>
          <span class="field-row-val">
            {#if $capabilities?.mihomo.api_authenticated}
              <span class="status-ok">{$t('settings.mihomo_yes')}</span>
            {:else if $capabilities?.mihomo.api_reachable}
              <span class="status-err">{$t('settings.mihomo_auth_error')}</span>
            {:else}
              <span style="color: var(--fg-secondary)">—</span>
            {/if}
          </span>
        </div>
        {#if $capabilities?.mihomo.discovered_secret}
          <div class="field-row">
            <span class="field-row-name">{$t('settings.mihomo_secret_discovered')}</span>
            <span class="field-row-val mono" style="display:flex;align-items:center;gap:6px;">
              {secretVisible ? $capabilities.mihomo.discovered_secret : '••••••••'}
              <button
                class="btn btn-secondary btn-sm"
                onclick={() => (secretVisible = !secretVisible)}
              >
                {secretVisible ? $t('app.hide') : $t('app.show')}
              </button>
            </span>
          </div>
        {/if}
      </div>
      <div class="card-actions">
        <button
          class="btn btn-secondary"
          onclick={recheckConnection}
          disabled={checkingConnection}
          title={$t('settings.recheck_title')}
        >
          {checkingConnection ? $t('settings.checking') : $t('settings.recheck_btn')}
        </button>
      </div>
    </div>
  {/if}

  <!-- Security tab -->
  {#if activeTab === 'security'}
    <div class="card mb-2">
      <div class="card-label">{$t('settings.change_password')}</div>
      <div class="field-group">
        <div class="field-row password-row">
          <label class="field-row-name" for="curr-pwd">{$t('settings.current_password')}</label>
          <div class="password-field-wrap">
            <PasswordField
              id="curr-pwd"
              bind:value={currentPassword}
              autocomplete="current-password"
              inputClass="input"
              placeholder="••••••••"
            />
          </div>
        </div>
        <div class="field-row password-row">
          <label class="field-row-name" for="new-pwd">{$t('settings.new_password')}</label>
          <div class="password-field-wrap password-field-with-meter">
            <PasswordField
              id="new-pwd"
              bind:value={newPassword}
              autocomplete="new-password"
              inputClass="input"
              placeholder="••••••••"
            />
            <PasswordStrengthMeter password={newPassword} userInputs={[currentPassword]} />
          </div>
        </div>
        <div class="field-row password-row">
          <label class="field-row-name" for="conf-pwd">{$t('settings.confirm_password')}</label>
          <div class="password-field-wrap">
            <PasswordField
              id="conf-pwd"
              bind:value={confirmPassword}
              autocomplete="new-password"
              inputClass="input"
              placeholder="••••••••"
            />
          </div>
        </div>
      </div>
      {#if passwordError}
        <div class="field-error">{passwordError}</div>
      {/if}
      {#if passwordSuccess}
        <div class="field-success">{$t('settings.password_changed')}</div>
      {/if}
      <div class="field-row-desc">{$t('settings.password_change_side_effect')}</div>
      <div class="card-actions">
        <button
          class="btn btn-primary"
          onclick={changePassword}
          disabled={passwordChanging || !currentPassword || !newPassword || !confirmPassword}
        >
          {passwordChanging ? $t('app.loading') : $t('settings.save_password')}
        </button>
      </div>
    </div>

    <div class="card mb-2">
      <div class="card-label">{$t('settings.session_ttl_title')}</div>
      <div class="field-group">
        <div class="field-row">
          <div>
            <label class="field-row-name" for="idle-ttl">{$t('settings.session_idle_ttl')}</label>
            <div class="field-row-desc">{$t('settings.session_idle_ttl_desc')}</div>
            {#if sessionTTL}
              <div class="field-row-desc">
                {$t('settings.session_ttl_range_hint', {
                  min: sessionTTL.idle_ttl_min,
                  max: sessionTTL.idle_ttl_max
                })}
              </div>
            {/if}
          </div>
          <div class="ttl-input-group">
            <input
              id="idle-ttl"
              type="number"
              class="input"
              class:input-error={idleTtlInvalid}
              aria-invalid={idleTtlInvalid}
              bind:value={idleTtlInput}
              disabled={ttlLoading || sessionTTL === null}
            />
            <span class="ttl-unit">{$t('settings.session_ttl_unit_hours')}</span>
          </div>
        </div>
        {#if idleTtlInvalid}
          <div class="field-error">{$t('settings.session_ttl_range_error')}</div>
        {/if}
        <div class="field-row">
          <div>
            <label class="field-row-name" for="absolute-ttl"
              >{$t('settings.session_absolute_ttl')}</label
            >
            <div class="field-row-desc">{$t('settings.session_absolute_ttl_desc')}</div>
            {#if sessionTTL}
              <div class="field-row-desc">
                {$t('settings.session_ttl_range_hint', {
                  min: sessionTTL.absolute_ttl_min,
                  max: sessionTTL.absolute_ttl_max
                })}
              </div>
            {/if}
          </div>
          <div class="ttl-input-group">
            <input
              id="absolute-ttl"
              type="number"
              class="input"
              class:input-error={absoluteTtlInvalid}
              aria-invalid={absoluteTtlInvalid}
              bind:value={absoluteTtlInput}
              disabled={ttlLoading || sessionTTL === null}
            />
            <span class="ttl-unit">{$t('settings.session_ttl_unit_days')}</span>
          </div>
        </div>
        {#if absoluteTtlInvalid}
          <div class="field-error">{$t('settings.session_ttl_range_error')}</div>
        {/if}
      </div>
      {#if ttlError}
        <div class="field-error">{ttlError}</div>
      {/if}
      <div class="card-actions">
        <button class="btn btn-primary" onclick={saveSessionTTL} disabled={ttlSaveDisabled}>
          {ttlSaving ? $t('app.loading') : $t('app.save')}
        </button>
      </div>
    </div>

    <div class="card mb-2">
      <div class="card-label">{$t('settings.sessions_title')}</div>
      <div class="field-group sessions-list">
        {#if sessionsLoading}
          {#each { length: 3 } as _, i (i)}
            <div class="field-row session-skeleton-row">
              <div class="session-skeleton-lines">
                <Skeleton type="text-line" width="55%" height="14px" />
                <Skeleton type="text-line" width="75%" height="12px" />
              </div>
            </div>
          {/each}
        {:else if sessionsError}
          <div class="field-row-desc">{sessionsError}</div>
          <button class="btn btn-secondary btn-sm" onclick={loadSessions}>{$t('app.retry')}</button>
        {:else}
          {#each sessions as s (s.id)}
            <div class="field-row" data-testid="session-row">
              <div class="session-info">
                <div class="session-name-row">
                  <span class="field-row-name session-name-text" title={sessionDeviceLabel(s)}>
                    {sessionDeviceLabel(s)}
                  </span>
                  {#if s.current}
                    <span class="badge badge-info">{$t('settings.sessions_current_badge')}</span>
                  {/if}
                </div>
                <div class="field-row-desc">
                  IP {s.ip} · {$t('settings.sessions_login_at', {
                    time: formatSessionTime(s.created_at)
                  })} · {$t('settings.sessions_last_seen', {
                    time: formatSessionTime(s.last_seen)
                  })}
                </div>
              </div>
              {#if !s.current}
                <button
                  class="btn btn-secondary btn-sm"
                  onclick={() => terminateSession(s)}
                  disabled={terminatingSessionId === s.id}
                >
                  {$t('settings.sessions_terminate')}
                </button>
              {/if}
            </div>
          {/each}
        {/if}
      </div>
      <div class="card-actions">
        <button
          class="btn btn-danger"
          onclick={terminateOtherSessions}
          disabled={sessions.length <= 1 || terminatingOthers || sessionsLoading}
        >
          {$t('settings.sessions_terminate_all_others')}
        </button>
      </div>
    </div>

    <div class="card mb-2">
      <div class="card-label">{$t('settings.security')}</div>
      <div class="field-group">
        <div class="field-row-info">
          <span class="feature-check"><Icon name="check" size={14} /></span><span
            >{$t('settings.auth_bcrypt')}</span
          >
        </div>
        <div class="field-row-info">
          <span class="feature-check"><Icon name="check" size={14} /></span><span
            >{$t('settings.csrf')}</span
          >
        </div>
        <div class="field-row-info">
          <span class="feature-check"><Icon name="check" size={14} /></span><span
            >{$t('settings.rate_limit')}</span
          >
        </div>
        <div class="field-row-info">
          <span class="feature-check"><Icon name="check" size={14} /></span><span
            >{$t('settings.security_headers')}</span
          >
        </div>
        <div class="field-row-info">
          <span class="feature-check"><Icon name="check" size={14} /></span><span
            >{$t('settings.security_https_only')}</span
          >
        </div>
        <div class="field-row-info">
          <span class="feature-check"><Icon name="check" size={14} /></span><span
            >{$t('settings.security_cookie_flags')}</span
          >
        </div>
      </div>
    </div>
  {/if}

  <!-- About tab -->
  {#if activeTab === 'about'}
    <div class="card mb-2">
      <div class="card-label">{$t('settings.about')}</div>
      <div class="field-group">
        <div class="field-row">
          <span class="field-row-name">{$t('settings.version')}</span>
          <span class="field-row-val mono">{version}</span>
        </div>
        <div class="field-row">
          <span class="field-row-name">{$t('settings.frontend')}</span>
          <span class="field-row-val mono">Svelte 5 + TypeScript + Vite</span>
        </div>
        <div class="field-row">
          <span class="field-row-name">{$t('settings.backend')}</span>
          <span class="field-row-val mono">Go + net/http</span>
        </div>
      </div>
      <div class="card-actions">
        <button
          class="btn btn-secondary"
          onclick={downloadDiagnostics}
          disabled={downloadingDiagnostics}
          title={$t('settings.diagnostics_download')}
        >
          {downloadingDiagnostics
            ? $t('settings.diagnostics_downloading')
            : $t('settings.diagnostics_download')}
        </button>
      </div>
    </div>
  {/if}
</div>

<MihomoSocketMigrateModal
  bind:open={showMihomoMigrateModal}
  onclose={() => (showMihomoMigrateModal = false)}
  onsuccess={recheckConnection}
/>

<style>
  /* card label */
  .card-label {
    font-size: 12px;
    font-weight: 600;
    letter-spacing: 0.04em;
    color: var(--fg-dim);
    margin-bottom: 14px;
  }

  /* field-group / field-row */
  .field-group {
    display: flex;
    flex-direction: column;
  }

  .field-row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 10px 0;
    border-bottom: 1px solid var(--border);
    gap: 16px;
  }

  .field-row:last-child {
    border-bottom: none;
    padding-bottom: 0;
  }

  .field-row:first-child {
    padding-top: 0;
  }

  .field-row-name {
    font-size: 14px;
    font-weight: 500;
    color: var(--fg-primary);
    flex-shrink: 0;
  }

  .field-row-val {
    font-size: 13px;
    color: var(--fg-secondary);
    text-align: right;
  }

  /* accent color picker */
  .accent-picker {
    display: flex;
    gap: 10px;
  }

  .accent-swatch {
    width: 26px;
    height: 26px;
    border-radius: var(--radius-full);
    border: 2px solid transparent;
    display: flex;
    align-items: center;
    justify-content: center;
    color: var(--swatch-check);
    cursor: pointer;
    padding: 0;
    transition:
      transform var(--transition-fast),
      border-color var(--transition-fast);
  }

  .accent-swatch:hover {
    transform: scale(1.08);
  }

  .accent-swatch.is-active {
    border-color: var(--fg-primary);
  }

  .accent-swatch--blue {
    background: var(--swatch-blue);
  }

  .accent-swatch--indigo {
    background: var(--swatch-indigo);
  }

  .accent-swatch--steel {
    background: var(--swatch-steel);
  }

  .accent-swatch--graphite {
    background: var(--swatch-graphite);
  }

  .field-row-val.mono {
    font-family: var(--font-mono, monospace);
    font-size: 12px;
  }

  .mono {
    font-family: var(--font-mono, monospace);
    font-size: 12px;
  }

  .field-value-badge {
    font-size: 13px;
    padding: 5px 10px;
    border: 1px solid var(--border);
    border-radius: 6px;
    background: var(--bg-card);
    color: var(--fg-primary);
    font-family: var(--font-mono, monospace);
    display: inline-flex;
    align-items: center;
  }

  .field-row-info {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 8px 0;
    font-size: 13px;
    color: var(--fg-secondary);
    border-bottom: 1px solid var(--border);
  }

  .field-row-info:last-child {
    border-bottom: none;
  }

  .card-actions {
    display: flex;
    gap: 8px;
    margin-top: 14px;
    flex-wrap: wrap;
  }

  .status-ok {
    color: var(--success);
  }

  .status-err {
    color: var(--danger);
  }

  .field-error {
    margin-top: 8px;
    font-size: 13px;
    color: var(--danger);
    padding: 6px 10px;
    background: color-mix(in srgb, var(--danger) 8%, transparent);
    border-radius: 6px;
    border: 1px solid color-mix(in srgb, var(--danger) 25%, transparent);
  }

  .field-success {
    margin-top: 8px;
    font-size: 13px;
    color: var(--success);
    padding: 6px 10px;
    background: color-mix(in srgb, var(--success) 8%, transparent);
    border-radius: 6px;
    border: 1px solid color-mix(in srgb, var(--success) 25%, transparent);
  }

  .mb-2 {
    margin-bottom: 12px;
  }

  .field-row-desc {
    font-size: 12px;
    color: var(--fg-dim);
    margin-top: 2px;
    overflow-wrap: anywhere;
  }

  .btn-sm {
    padding: 6px 12px;
    font-size: 12px;
  }

  .backup-dropzone:hover {
    border-color: var(--accent);
    background: var(--accent-soft);
  }

  /* Активные сессии (D-10) */
  .sessions-list {
    display: flex;
    flex-direction: column;
    max-height: 480px;
    overflow-y: auto;
    scrollbar-width: thin;
    scrollbar-color: var(--border) transparent;
  }

  .sessions-list::-webkit-scrollbar {
    width: 8px;
  }

  .sessions-list::-webkit-scrollbar-thumb {
    background: var(--border);
    border-radius: var(--radius-full);
  }

  .sessions-list::-webkit-scrollbar-track {
    background: transparent;
  }

  .session-skeleton-row {
    display: block;
  }

  .session-skeleton-lines {
    display: flex;
    flex-direction: column;
    gap: 6px;
    width: 100%;
  }

  .session-info {
    display: flex;
    flex-direction: column;
    gap: 2px;
    min-width: 0;
    flex: 1;
  }

  .session-name-row {
    display: flex;
    align-items: center;
    gap: 8px;
    min-width: 0;
  }

  .field-row-name.session-name-text {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    min-width: 0;
    flex-shrink: 1;
  }

  /* Смена пароля — индикатор надёжности под новым паролем (D-18) */
  .password-field-wrap {
    flex: 1;
    min-width: 0;
    max-width: 260px;
  }

  .password-field-wrap :global(.password-field) {
    width: 100%;
  }

  .password-field-wrap :global(.password-field input) {
    width: 100%;
    min-width: 0;
  }

  /* На узком экране подпись над полем: иначе ширина поля зависит от длины подписи */
  @media (max-width: 480px) {
    .password-row {
      flex-direction: column;
      align-items: stretch;
      gap: 6px;
    }

    .password-row .password-field-wrap {
      max-width: none;
    }
  }

  .password-field-with-meter {
    display: flex;
    flex-direction: column;
    gap: 4px;
  }

  /* Время жизни сессии (D-06) */
  .ttl-input-group {
    display: flex;
    align-items: center;
    gap: 8px;
  }

  .ttl-input-group .input {
    width: 96px;
  }

  /* Единицы разной длины («ч» / «дн.») не должны сдвигать поля */
  .ttl-unit {
    min-width: 24px;
    font-size: 12px;
    color: var(--fg-dim);
  }

  .feature-check {
    display: inline-flex;
    color: var(--success);
  }
</style>
