<script lang="ts">
  import { onMount, onDestroy, tick } from 'svelte';
  import { t, currentLang } from './i18n';
  import { showToast, capabilities, devMode } from './stores';
  import { parseValidationError } from './lib/errorParser';

  // Subcomponents
  import SubscriptionList from './components/subscriptions/SubscriptionList.svelte';
  import SubscriptionFormModal from './components/subscriptions/SubscriptionFormModal.svelte';
  import NodeImporter from './components/subscriptions/NodeImporter.svelte';

  interface Subscription {
    id: string;
    name: string;
    profile_title?: string;
    url: string;
    enabled: boolean;
    interval: number;
    use_provider_interval: boolean;
    enable_xray: boolean;
    enable_mihomo: boolean;
    mihomo_integrated: boolean;
    hwid_locked: boolean;
    last_update: string;
    last_error?: string;
    proxy_count?: number;
    upload?: number;
    download?: number;
    total?: number;
    expire?: number;
    support_url?: string;
    announcement?: string;
    profile_update_hours?: number;
    tag_prefix?: string;
    filter_name?: string;
    filter_type?: string;
    filter_transport?: string;
    mihomo_groups?: string[];
    xray_routing_mode?: 'manual' | 'auto';
  }

  interface Node {
    tag: string;
    name?: string;
    country?: string;
    flag?: string;
    active: boolean;
    use_case?: string;
    speed?: string;
    protocol?: string;
    transport?: string;
    security?: string;
    is_new?: boolean;
  }

  interface NodeHealth {
    alive: boolean;
    delay?: number;
    http_code?: number;
  }

  // Reactive state using runes
  let subscriptions = $state<Subscription[]>([]);
  let expandedSubs = $state<Record<string, boolean>>({});
  let subNodes = $state<Record<string, Node[]>>({});
  let subNodesLoading = $state<Record<string, boolean>>({});
  let subHealth = $state<Record<string, Record<string, NodeHealth>>>({});
  let checkingNodes = $state<Record<string, Record<string, boolean>>>({});
  let refreshLoading = $state<Record<string, boolean>>({});
  let activeDropdownId = $state<string | null>(null);
  let loading = $state(false);

  // Form modal states
  let showAddModal = $state(false);
  let editingSub = $state<Subscription | null>(null);
  let formName = $state('');
  let formEnableXray = $state(false);
  let formEnableMihomo = $state(false);
  let formURL = $state('');
  let formInterval = $state(24);
  let formRoutingMode = $state<'manual' | 'auto'>('manual');
  let formTagPrefix = $state('');
  let formFilterName = $state('');
  let formFilterType = $state('');
  let formFilterTransport = $state('');
  let formMihomoGroups = $state<string[]>([]);
  let formEnabled = $state(true);
  let formUseProviderInterval = $state(false);
  let availableMihomoGroups = $state<string[]>([]);

  // Diagnostic states
  let showDiagnosticModal = $state(false);
  let diagnosticSub = $state<Subscription | null>(null);
  let diagnosticTab = $state<'report' | 'headers' | 'raw'>('report');
  let diagnosticLoading = $state(false);
  let parseReportData = $state<any>(null);
  let rawResponseData = $state<any>(null);

  async function openDiagnosticModal(sub: Subscription) {
    diagnosticSub = sub;
    showDiagnosticModal = true;
    diagnosticTab = 'report';
    diagnosticLoading = true;
    parseReportData = null;
    rawResponseData = null;

    try {
      const resReport = await fetch(`/api/subscription/parse-report?id=${sub.id}`);
      if (resReport.ok) {
        parseReportData = await resReport.json();
      }
      const resRaw = await fetch(`/api/subscription/raw-response?id=${sub.id}`);
      if (resRaw.ok) {
        rawResponseData = await resRaw.json();
      }
    } catch (e) {
      // Ignored, data stays null
    } finally {
      diagnosticLoading = false;
    }
  }

  function closeDiagnosticModal() {
    showDiagnosticModal = false;
    diagnosticSub = null;
  }

  async function loadAvailableMihomoGroups() {
    try {
      const res = await fetch('/api/subscription/mihomo-groups');
      if (res.ok) {
        const data = await res.json();
        availableMihomoGroups = Array.isArray(data) ? data : [];
      } else {
        availableMihomoGroups = [];
      }
    } catch (e) {
      availableMihomoGroups = [];
    }
  }

  async function loadSubscriptions() {
    loading = true;
    try {
      const res = await fetch('/api/subscription/list');
      if (res.ok) {
        subscriptions = await res.json();
      }
    } catch (e) {
      showToast('error', $t('subscr.load_error'));
    } finally {
      loading = false;
    }
  }

  async function refreshSubscription(id: string) {
    refreshLoading[id] = true;
    try {
      const csrfToken = localStorage.getItem('csrf_token');
      const res = await fetch(`/api/subscription/refresh?id=${id}`, {
        method: 'POST',
        headers: { 'X-CSRF-Token': csrfToken || '' }
      });
      if (res.ok) {
        showToast('success', $t('app.success'));
        await loadSubscriptions();
        if (expandedSubs[id]) {
          await loadNodes(id);
        }
      } else {
        const text = await res.text();
        const parsedErr = parseValidationError(text, $currentLang === 'ru' ? 'ru' : 'en');
        showToast('error', parsedErr || $t('app.error'));
        await loadSubscriptions();
      }
    } catch (e) {
      showToast('error', $t('app.error'));
    } finally {
      refreshLoading[id] = false;
    }
  }

  async function refreshAll() {
    loading = true;
    try {
      const csrfToken = localStorage.getItem('csrf_token');
      const res = await fetch('/api/subscription/refresh-all', {
        method: 'POST',
        headers: { 'X-CSRF-Token': csrfToken || '' }
      });
      if (res.ok) {
        showToast('success', $t('app.success'));
        await loadSubscriptions();
        for (const id of Object.keys(expandedSubs)) {
          if (expandedSubs[id]) {
            await loadNodes(id);
          }
        }
      } else {
        showToast('error', $t('app.error'));
      }
    } catch (e) {
      showToast('error', $t('app.error'));
    } finally {
      loading = false;
    }
  }

  async function saveSubscription() {
    if (!formURL.trim()) {
      showToast('error', $t('subscr.fill_url') || 'Please fill in the URL field');
      return;
    }

    const csrfToken = localStorage.getItem('csrf_token');
    const payload = {
      id: editingSub ? editingSub.id : '',
      name: formName,
      url: formURL,
      enabled: formEnabled,
      interval: formInterval,
      use_provider_interval: formUseProviderInterval,
      enable_xray: formEnableXray,
      enable_mihomo: formEnableMihomo,
      tag_prefix: formTagPrefix,
      filter_name: formFilterName,
      filter_type: formFilterType,
      filter_transport: formFilterTransport,
      mihomo_groups: formMihomoGroups,
      xray_routing_mode: formRoutingMode
    };

    try {
      const url = editingSub ? '/api/subscription/update' : '/api/subscription/create';
      const res = await fetch(url, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          'X-CSRF-Token': csrfToken || ''
        },
        body: JSON.stringify(payload)
      });

      if (res.ok) {
        showToast('success', $t('app.success'));
        showAddModal = false;
        await loadSubscriptions();
      } else {
        const text = await res.text();
        const parsedErr = parseValidationError(text, $currentLang === 'ru' ? 'ru' : 'en');
        showToast('error', parsedErr || $t('app.error'));
      }
    } catch (e) {
      showToast('error', $t('app.error'));
    }
  }

  async function deleteSubscription(id: string) {
    const sub = subscriptions.find((s) => s.id === id);
    if (!sub) return;
    const confirmMsg = $t('subscr.delete_confirm')
      ? $t('subscr.delete_confirm').replace('{name}', sub.profile_title || sub.name)
      : `Удалить подписку: Вы уверены, что хотите безвозвратно удалить подписку '${sub.profile_title || sub.name}'?`;

    if (!confirm(confirmMsg)) return;

    const csrfToken = localStorage.getItem('csrf_token');
    try {
      const res = await fetch(`/api/subscription/delete?id=${id}`, {
        method: 'POST',
        headers: { 'X-CSRF-Token': csrfToken || '' }
      });
      if (res.ok) {
        showToast('success', $t('app.success'));
        await loadSubscriptions();
      } else {
        showToast('error', $t('app.error'));
      }
    } catch (e) {
      showToast('error', $t('app.error'));
    }
  }

  function openAddModal() {
    editingSub = null;
    formName = '';
    formURL = '';
    formInterval = 24;
    formEnabled = true;
    formUseProviderInterval = false;
    formEnableXray = true;
    formEnableMihomo = false;
    formRoutingMode = 'manual';
    formTagPrefix = '';
    formFilterName = '';
    formFilterType = '';
    formFilterTransport = '';
    formMihomoGroups = [];
    showAddModal = true;
    loadAvailableMihomoGroups();
  }

  function openEditModal(sub: Subscription) {
    editingSub = sub;
    formName = sub.name;
    formURL = sub.url;
    formInterval = sub.interval;
    formEnabled = sub.enabled;
    formUseProviderInterval = sub.use_provider_interval;
    formEnableXray = sub.enable_xray;
    formEnableMihomo = sub.enable_mihomo;
    formRoutingMode = sub.xray_routing_mode || 'manual';
    formTagPrefix = sub.tag_prefix || '';
    formFilterName = sub.filter_name || '';
    formFilterType = sub.filter_type || '';
    formFilterTransport = sub.filter_transport || '';
    formMihomoGroups = sub.mihomo_groups || [];
    showAddModal = true;
    loadAvailableMihomoGroups();
  }

  function closeModal() {
    showAddModal = false;
    editingSub = null;
  }

  function toggleDropdown(id: string) {
    if (activeDropdownId === id) {
      activeDropdownId = null;
    } else {
      activeDropdownId = id;
    }
  }

  function handleKeydown(e: KeyboardEvent) {
    if (e.key === 'Escape') {
      closeModal();
      closeDiagnosticModal();
    }
  }

  function handleClickOutside(e: MouseEvent) {
    if (activeDropdownId) {
      const target = e.target as HTMLElement;
      if (!target.closest('.dropdown-container')) {
        activeDropdownId = null;
      }
    }
  }

  async function loadNodes(subId: string) {
    subNodesLoading[subId] = true;
    try {
      const res = await fetch(`/api/subscription/nodes?id=${subId}`);
      if (res.ok) {
        subNodes[subId] = await res.json();
      }
    } catch (e) {
      // Node list error is local
    } finally {
      subNodesLoading[subId] = false;
    }
  }

  async function toggleExpand(subId: string) {
    expandedSubs[subId] = !expandedSubs[subId];
    if (expandedSubs[subId]) {
      await loadNodes(subId);
    }
  }

  async function checkNodeHealth(subId: string, nodeTag: string) {
    if (!checkingNodes[subId]) checkingNodes[subId] = {};
    checkingNodes[subId][nodeTag] = true;
    try {
      const res = await fetch(
        `/api/subscription/node-health?id=${subId}&tag=${encodeURIComponent(nodeTag)}`
      );
      if (res.ok) {
        const health = await res.json();
        if (!subHealth[subId]) subHealth[subId] = {};
        subHealth[subId][nodeTag] = health;
      }
    } catch (e) {
      // Health check error is local
    } finally {
      checkingNodes[subId][nodeTag] = false;
    }
  }

  async function setActiveNode(subId: string, nodeTag: string) {
    const csrfToken = localStorage.getItem('csrf_token');
    try {
      const res = await fetch(
        `/api/subscription/set-active-node?id=${subId}&tag=${encodeURIComponent(nodeTag)}`,
        {
          method: 'POST',
          headers: { 'X-CSRF-Token': csrfToken || '' }
        }
      );
      if (res.ok) {
        showToast('success', $t('app.success'));
        await loadNodes(subId);
      } else {
        const text = await res.text();
        showToast('error', text || $t('app.error'));
      }
    } catch (e) {
      showToast('error', $t('app.error'));
    }
  }

  function checkAutoExpand() {
    const hash = window.location.hash;
    const regex = /#\/subscriptions\?expand=(.+)/;
    const match = hash.match(regex);
    if (match && match[1]) {
      const subId = match[1];
      expandedSubs[subId] = true;
      loadNodes(subId).then(() => {
        setTimeout(() => {
          const el = document.getElementById(`sub-card-${subId}`);
          if (el) {
            el.scrollIntoView({ behavior: 'smooth', block: 'start' });
          }
        }, 100);
      });
    }
  }

  let stats = $derived(
    (() => {
      const totalNodes = subscriptions.reduce((sum, s) => sum + (s.proxy_count || 0), 0);
      let minNext = Infinity;
      subscriptions.forEach((s) => {
        if (s.enabled && s.last_update && !s.last_update.startsWith('0001')) {
          const next = new Date(s.last_update).getTime() + s.interval * 3600 * 1000;
          const diff = next - Date.now();
          if (diff > 0 && diff < minNext) {
            minNext = diff;
          }
        }
      });
      let nextStr = '—';
      if (minNext !== Infinity) {
        const diffHours = Math.floor(minNext / (3600 * 1000));
        const diffMins = Math.floor((minNext % (3600 * 1000)) / (60 * 1000));
        nextStr = `${diffHours}ч ${diffMins}м`;
      }
      return {
        total: subscriptions.length,
        nodes: totalNodes,
        next: nextStr
      };
    })()
  );

  onMount(() => {
    loadSubscriptions().then(() => {
      checkAutoExpand();
    });
    const handleHashChange = () => {
      checkAutoExpand();
    };
    window.addEventListener('hashchange', handleHashChange);
    window.addEventListener('click', handleClickOutside);
    window.addEventListener('keydown', handleKeydown);

    return () => {
      window.removeEventListener('hashchange', handleHashChange);
      window.removeEventListener('click', handleClickOutside);
      window.removeEventListener('keydown', handleKeydown);
    };
  });
</script>

<div class="container">
  <div class="page-head">
    <div>
      <div class="crumbs">
        {$t('nav.group_proxy')} <span style="color:var(--fg-faint);margin:0 6px;">/</span>
        {$t('nav.subscriptions')}
      </div>
      <h1>{$t('subscr.title') || 'Подписки'}</h1>
      <p class="sub">{$t('subscr.subtitle') || 'Управление подписками на прокси-серверы'}</p>
    </div>
    <div class="ph-actions">
      <button class="btn btn-secondary" onclick={refreshAll} disabled={loading}>
        <svg
          width="14"
          height="14"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          stroke-width="2"
          style="margin-right: 6px;"><path d="M21 12a9 9 0 1 1-3-6.7L21 8M21 3v5h-5" /></svg
        >
        {$t('subscr.refresh_all') || 'Обновить всё'}
      </button>

      <button class="btn btn-primary" onclick={openAddModal}>
        <svg
          width="14"
          height="14"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          stroke-width="2"
          style="margin-right: 6px;"><path d="M12 5v14M5 12h14" /></svg
        >
        {$t('subscr.add') || 'Добавить подписку'}
      </button>
    </div>
  </div>

  {#if $capabilities?.xray && !$capabilities.xray.conf_dir_exists && $capabilities.active_kernel === 'xray'}
    <div class="confdir-warning">
      <svg
        width="16"
        height="16"
        viewBox="0 0 24 24"
        fill="none"
        stroke="currentColor"
        stroke-width="2"
        style="flex-shrink:0"
        ><path
          d="M10.29 3.86L1.82 18a2 2 0 0 0 1.71 3h16.94a2 2 0 0 0 1.71-3L13.71 3.86a2 2 0 0 0-3.42 0z"
        /><line x1="12" y1="9" x2="12" y2="13" /><line x1="12" y1="17" x2="12.01" y2="17" /></svg
      >
      <span>{$t('subscr.confdir_warning').replace('{dir}', $capabilities.xray.conf_dir)}</span>
    </div>
  {/if}

  {#if subscriptions.length === 0}
    <div
      class="card text-center"
      style="padding: 3rem; display: flex; flex-direction: column; align-items: center; justify-content: center; gap: 1rem;"
    >
      <p style="color: var(--fg-secondary); margin: 0;">{$t('subscr.empty') || 'Список подписок пуст'}</p>
      <button class="btn btn-primary" onclick={openAddModal}>
        <svg
          width="14"
          height="14"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          stroke-width="2"
          style="margin-right: 6px;"><path d="M12 5v14M5 12h14" /></svg
        >
        {$t('subscr.add_first') || 'Добавить первую подписку'}
      </button>
    </div>
  {:else}
    <SubscriptionList
      {subscriptions}
      {expandedSubs}
      {refreshLoading}
      {activeDropdownId}
      {subNodesLoading}
      {subNodes}
      {subHealth}
      {checkingNodes}
      devMode={$devMode}
      {stats}
      onToggleExpand={toggleExpand}
      onRefreshSub={refreshSubscription}
      onEditSub={openEditModal}
      onDeleteSub={deleteSubscription}
      onOpenDiagnostic={openDiagnosticModal}
      onSetActiveNode={setActiveNode}
      onCheckNodeHealth={checkNodeHealth}
      onToggleDropdown={toggleDropdown}
    />
  {/if}
</div>

{#if showAddModal}
  <SubscriptionFormModal
    {editingSub}
    bind:formName
    bind:formEnableXray
    bind:formEnableMihomo
    bind:formURL
    bind:formInterval
    bind:formRoutingMode
    bind:formTagPrefix
    bind:formFilterName
    bind:formFilterType
    bind:formFilterTransport
    bind:formMihomoGroups
    bind:formEnabled
    bind:formUseProviderInterval
    {availableMihomoGroups}
    onClose={closeModal}
    onSave={saveSubscription}
  />
{/if}

{#if showDiagnosticModal && diagnosticSub}
  <NodeImporter
    {diagnosticSub}
    diagnosticTab={diagnosticTab}
    diagnosticLoading={diagnosticLoading}
    parseReportData={parseReportData}
    rawResponseData={rawResponseData}
    onClose={closeDiagnosticModal}
    onTabChange={(tab) => (diagnosticTab = tab)}
  />
{/if}

<style>
  .confdir-warning {
    display: flex;
    align-items: flex-start;
    gap: 8px;
    padding: 10px 14px;
    margin-bottom: 16px;
    background: color-mix(in srgb, var(--color-warning, #f59e0b) 12%, transparent);
    border: 1px solid color-mix(in srgb, var(--color-warning, #f59e0b) 40%, transparent);
    border-radius: var(--radius-sm, 6px);
    color: var(--fg);
    font-size: 13px;
    line-height: 1.5;
  }

  .confdir-warning svg {
    color: var(--color-warning, #f59e0b);
    margin-top: 1px;
  }
</style>
