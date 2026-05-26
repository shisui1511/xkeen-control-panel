<script lang="ts">
  import { onMount } from 'svelte';
  import { t, currentLang } from './i18n';
  import { showConfirm, capabilities, fetchCapabilities, showToast, devMode, fetchDevMode } from './stores';
  import SubscriptionHeader from './components/SubscriptionHeader.svelte';
  import SubscriptionChips from './components/SubscriptionChips.svelte';

  let { onSwitchTab = () => {} } = $props<{ onSwitchTab?: (tab: string) => void }>();

  interface Subscription {
    id: string;
    name: string;
    url: string;
    tag_prefix: string;
    interval: number;
    last_update: string;
    enabled: boolean;
    type?: string;
    filter_name?: string;
    filter_type?: string;
    filter_transport?: string;
    proxy_count?: number;
    last_error?: string;
    upload?: number;
    download?: number;
    total?: number;
    rule_count?: number;
    detected_format?: string;
    provider_type?: string;
    support_url?: string;
    profile_update_hours?: number;
    profile_title?: string;
    last_count?: number;
    last_skipped?: number;
    last_changed?: boolean;
    expire?: number;
    hwid_locked?: boolean;
    use_provider_interval?: boolean;
  }

  interface SubscriptionNode {
    tag: string;
    name: string;
    country: string;
    flag: string;
    use_case: string;
    speed: string;
    is_new: boolean;
    protocol: string;
    transport: string;
    security: string;
    server: string;
    active: boolean;
  }

  interface NodeHealth {
    alive: boolean;
    latency_ms: number;
    checked: string;
  }

  function getFormatBadge(sub: Subscription): string {
    if (sub.type === 'mihomo') return 'clash · YAML';
    if (sub.detected_format === 'sing-box') return 'xray · sing-box';
    if (sub.detected_format === 'clash-meta') return 'xray · clash';
    if (sub.detected_format === 'base64') return 'xray · base64';
    if (sub.detected_format === 'share-links') return 'xray · links';
    if (sub.detected_format === 'xray-json') return 'xray · JSON';
    return 'xray · JSON';
  }

  function getProviderBadge(sub: Subscription): string | null {
    if (!sub.provider_type || sub.provider_type === 'custom') return null;
    const labels: Record<string, string> = {
      remnawave: 'Remnawave',
      marzban: 'Marzban',
      '3x-ui': '3X-UI'
    };
    return labels[sub.provider_type] ?? null;
  }

  function formatTraffic(bytes: number): string {
    const gb = bytes / (1024 * 1024 * 1024);
    if (gb >= 1) return `${gb.toFixed(1)} GB`;
    const mb = bytes / (1024 * 1024);
    return `${mb.toFixed(1)} MB`;
  }

  function formatTrafficUsage(upload?: number, download?: number, total?: number): string {
    const used = (upload || 0) + (download || 0);
    if (used === 0 && (!total || total === 0)) return '—';
    const usedStr = formatTraffic(used);
    if (total && total > 0) {
      return `${usedStr} / ${formatTraffic(total)}`;
    }
    return usedStr;
  }

  let subscriptions = $state<Subscription[]>([]);
  let loading = $state(false);
  let refreshLoading = $state<Record<string, boolean>>({});
  let showAddModal = $state(false);
  let editingSub = $state<Subscription | null>(null);
  let activeDropdownId = $state<string | null>(null);

  // Состояние для inline раскрытия узлов
  let expandedSubs = $state<Record<string, boolean>>({});
  let subNodes = $state<Record<string, SubscriptionNode[]>>({});
  let subNodesLoading = $state<Record<string, boolean>>({});
  let subHealth = $state<Record<string, Record<string, NodeHealth>>>({});
  let checkingNodes = $state<Record<string, Record<string, boolean>>>({});
  let activatingNode = $state<Record<string, string | null>>({}); // subId -> nodeTag
  let flagsSupported = $state(true);

  // Diagnostic Modal State
  let showDiagnosticModal = $state(false);
  let diagnosticSub = $state<Subscription | null>(null);
  let diagnosticLoading = $state(false);
  let diagnosticTab = $state<'report' | 'headers' | 'raw'>('report');
  let rawResponseData = $state<{ body: string; headers: Record<string, string[]> } | null>(null);
  let parseReportData = $state<{
    parsed_count: number;
    skipped_count: number;
    skipped: { line: number; reason: string; snippet: string }[];
    timestamp: string;
  } | null>(null);

  async function openDiagnosticModal(sub: Subscription) {
    diagnosticSub = sub;
    showDiagnosticModal = true;
    diagnosticLoading = true;
    diagnosticTab = 'report';
    rawResponseData = null;
    parseReportData = null;

    try {
      const [resReport, resRaw] = await Promise.all([
        fetch(`/api/subscriptions/parse-report?id=${sub.id}`),
        fetch(`/api/subscriptions/raw?id=${sub.id}`)
      ]);

      if (resReport.ok) {
        parseReportData = await resReport.json();
      }
      if (resRaw.ok) {
        rawResponseData = await resRaw.json();
      }
    } catch (e: any) {
      showToast('error', e.message || $t('app.error'));
    } finally {
      diagnosticLoading = false;
    }
  }

  function closeDiagnosticModal() {
    showDiagnosticModal = false;
    diagnosticSub = null;
  }

  // Form fields
  let formName = $state('');
  let formURL = $state('');
  let formTagPrefix = $state('');
  let formInterval = $state(24);
  let formFilterName = $state('');
  let formFilterType = $state('');
  let formFilterTransport = $state('');
  let formEnabled = $state(true);
  let formUseProviderInterval = $state(false);

  async function loadSubscriptions() {
    loading = true;
    try {
      const res = await fetch('/api/subscriptions');
      if (res.ok) {
        const envelope = await res.json();
        subscriptions = Array.isArray(envelope) ? envelope : (envelope.data ?? []);
      } else {
        showToast('error', await res.text());
      }
    } catch (e: any) {
      showToast('error', e.message || $t('app.error'));
    } finally {
      loading = false;
    }
  }

  async function refreshSubscription(id: string) {
    refreshLoading[id] = true;
    try {
      const csrfToken = localStorage.getItem('csrf_token');
      const res = await fetch(`/api/subscriptions/refresh?id=${id}`, {
        method: 'POST',
        headers: { 'X-CSRF-Token': csrfToken || '' }
      });
      if (!res.ok) {
        showToast('error', await res.text());
      }
      await loadSubscriptions();
    } catch (e: any) {
      showToast('error', e.message || $t('app.error'));
    } finally {
      refreshLoading[id] = false;
    }
  }

  async function refreshAll() {
    loading = true;
    try {
      const csrfToken = localStorage.getItem('csrf_token');
      const res = await fetch('/api/subscriptions/refresh-all', {
        method: 'POST',
        headers: { 'X-CSRF-Token': csrfToken || '' }
      });
      if (!res.ok) {
        showToast('error', await res.text());
      }
      await loadSubscriptions();
    } catch (e: any) {
      showToast('error', e.message || $t('app.error'));
    } finally {
      loading = false;
    }
  }

  async function saveSubscription() {
    const csrfToken = localStorage.getItem('csrf_token');
    const sub = {
      name: formName,
      url: formURL,
      tag_prefix: formTagPrefix,
      interval: formInterval,
      enabled: formEnabled,
      filter_name: formFilterName || undefined,
      filter_type: formFilterType || undefined,
      filter_transport: formFilterTransport || undefined,
      use_provider_interval: formUseProviderInterval
    };

    try {
      let res: Response;
      if (editingSub) {
        res = await fetch(`/api/subscriptions/update?id=${editingSub.id}`, {
          method: 'POST',
          headers: {
            'Content-Type': 'application/json',
            'X-CSRF-Token': csrfToken || ''
          },
          body: JSON.stringify(sub)
        });
      } else {
        res = await fetch('/api/subscriptions/add', {
          method: 'POST',
          headers: {
            'Content-Type': 'application/json',
            'X-CSRF-Token': csrfToken || ''
          },
          body: JSON.stringify(sub)
        });
      }
      if (!res.ok) {
        showToast('error', await res.text());
        return;
      }
      closeModal();
      await loadSubscriptions();
    } catch (e: any) {
      showToast('error', e.message || $t('app.error'));
    }
  }

  async function deleteSubscription(id: string) {
    if (!(await showConfirm($t('app.confirm'), $t('subscr.delete_confirm')))) return;
    try {
      const csrfToken = localStorage.getItem('csrf_token');
      const res = await fetch(`/api/subscriptions/delete?id=${id}`, {
        method: 'POST',
        headers: { 'X-CSRF-Token': csrfToken || '' }
      });
      if (!res.ok) {
        showToast('error', await res.text());
        return;
      }
      await loadSubscriptions();
    } catch (e: any) {
      showToast('error', e.message || $t('app.error'));
    }
  }

  function openAddModal() {
    editingSub = null;
    formName = '';
    formURL = '';
    formTagPrefix = '';
    formInterval = 24;
    formFilterName = '';
    formFilterType = '';
    formFilterTransport = '';
    formEnabled = true;
    formUseProviderInterval = false;
    showAddModal = true;
  }

  function openEditModal(sub: Subscription) {
    editingSub = sub;
    formName = sub.name;
    formURL = sub.url;
    formTagPrefix = sub.tag_prefix || '';
    formInterval = sub.interval;
    formFilterName = sub.filter_name || '';
    formFilterType = sub.filter_type || '';
    formFilterTransport = sub.filter_transport || '';
    formEnabled = sub.enabled;
    formUseProviderInterval = !!sub.use_provider_interval;
    showAddModal = true;
  }

  function closeModal() {
    showAddModal = false;
    editingSub = null;
  }

  function formatDate(dateStr: string): string {
    if (!dateStr || dateStr.startsWith('0001')) return '—';
    const d = new Date(dateStr);
    return d.toLocaleString($currentLang === 'ru' ? 'ru-RU' : 'en-US', {
      month: 'short',
      day: 'numeric',
      hour: '2-digit',
      minute: '2-digit'
    });
  }

  function toggleDropdown(id: string) {
    activeDropdownId = activeDropdownId === id ? null : id;
  }

  function handleKeydown(e: KeyboardEvent) {
    if (e.key === 'Escape') {
      closeModal();
      closeDiagnosticModal();
      activeDropdownId = null;
    }
  }

  function handleClickOutside(e: MouseEvent) {
    const target = e.target as HTMLElement;
    if (!target.closest('.dropdown-container')) {
      activeDropdownId = null;
    }
  }

  function latencyClass(h: NodeHealth | undefined): string {
    if (!h || !h.alive || h.latency_ms < 0) return 'latency-unknown';
    if (h.latency_ms < 300) return 'latency-good';
    if (h.latency_ms < 1000) return 'latency-ok';
    return 'latency-bad';
  }

  function latencyLabel(h: NodeHealth | undefined): string {
    if (!h) return '?';
    if (!h.alive) return '✕';
    if (h.latency_ms < 0) return '?';
    return `${h.latency_ms}ms`;
  }

  type Token = 
    | { type: 'text'; value: string }
    | { type: 'bold'; value: string }
    | { type: 'italic'; value: string }
    | { type: 'link'; text: string; url: string };

  function parseSimpleMarkdown(text: string): Token[] {
    if (!text) return [];
    
    const tokens: Token[] = [];
    const regex = /(\*\*.*?\*\*|\*.*?\*|\[.*?\]\(.*?\))/g;
    const parts = text.split(regex);
    
    for (const part of parts) {
      if (!part) continue;
      
      if (part.startsWith('**') && part.endsWith('**')) {
        tokens.push({ type: 'bold', value: part.slice(2, -2) });
      } else if (part.startsWith('*') && part.endsWith('*')) {
        tokens.push({ type: 'italic', value: part.slice(1, -1) });
      } else if (part.startsWith('[') && part.includes('](') && part.endsWith(')')) {
        const closeBracketIdx = part.indexOf('](');
        const linkText = part.slice(1, closeBracketIdx);
        const linkUrl = part.slice(closeBracketIdx + 2, -1);
        tokens.push({ type: 'link', text: linkText, url: linkUrl });
      } else {
        tokens.push({ type: 'text', value: part });
      }
    }
    return tokens;
  }

  async function toggleExpand(subId: string) {
    if (expandedSubs[subId]) {
      expandedSubs[subId] = false;
      return;
    }

    expandedSubs[subId] = true;

    if (!subNodes[subId]) {
      subNodesLoading[subId] = true;
      try {
        const [resNodes, resHealth] = await Promise.all([
          fetch(`/api/subscriptions/nodes?id=${subId}`),
          fetch(`/api/subscriptions/health?id=${subId}`)
        ]);

        if (resNodes.ok) {
          subNodes[subId] = await resNodes.json();
        }
        if (resHealth.ok) {
          subHealth[subId] = await resHealth.json();
        }
      } catch (e: any) {
        showToast('error', e.message || $t('app.error'));
      } finally {
        subNodesLoading[subId] = false;
      }
    }
  }

  async function checkNodeHealth(subId: string, nodeTag: string) {
    if (!checkingNodes[subId]) checkingNodes[subId] = {};
    if (checkingNodes[subId][nodeTag]) return;

    checkingNodes[subId][nodeTag] = true;
    try {
      const url = `/api/subscriptions/health?id=${subId}&force=true&node_tag=${nodeTag}`;
      const res = await fetch(url);
      if (!res.ok) return;
      const data = await res.json();
      if (data && data[nodeTag]) {
        if (!subHealth[subId]) subHealth[subId] = {};
        subHealth[subId][nodeTag] = data[nodeTag];
      }
    } catch (e: any) {
      showToast('error', e.message || $t('app.error'));
    } finally {
      checkingNodes[subId][nodeTag] = false;
    }
  }

  async function setActiveNode(subId: string, nodeTag: string, subType?: string) {
    if (subType === 'mihomo') return;
    const csrfToken = localStorage.getItem('csrf_token');
    if (!activatingNode[subId]) activatingNode[subId] = null;
    activatingNode[subId] = nodeTag;
    try {
      const res = await fetch(`/api/subscriptions/active?id=${subId}`, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          'X-CSRF-Token': csrfToken || ''
        },
        body: JSON.stringify({ node_tag: nodeTag })
      });
      if (res.ok) {
        showToast($t('subscr.detail.activated'), 'success');
        if (subNodes[subId]) {
          subNodes[subId] = subNodes[subId].map((n) => ({ ...n, active: n.tag === nodeTag }));
        }
      } else if (res.status === 409) {
        showToast($t('subscr.detail.auto_routing_conflict'), 'warning');
      } else {
        const err = await res.json().catch(() => ({}));
        showToast(err.error || $t('subscr.detail.activate_error'), 'error');
      }
    } catch (e: any) {
      showToast('error', e.message || $t('app.error'));
    } finally {
      activatingNode[subId] = null;
    }
  }

  onMount(() => {
    // Проверяем поддержку эмодзи флагов в ОС
    try {
      const canvas = document.createElement('canvas');
      const ctx = canvas.getContext('2d');
      if (ctx) {
        ctx.fillStyle = '#000';
        ctx.textBaseline = 'top';
        ctx.font = '32px Arial';
        ctx.fillText('🇺🇸', 0, 0);
        const widthFlag = ctx.measureText('🇺🇸').width;
        const widthLetters = ctx.measureText('US').width;
        flagsSupported = widthFlag < widthLetters;
      }
    } catch (e) {
      flagsSupported = false;
    }

    loadSubscriptions();
    fetchCapabilities();
    fetchDevMode();
    window.addEventListener('click', handleClickOutside);
    window.addEventListener('keydown', handleKeydown);
    return () => {
      window.removeEventListener('click', handleClickOutside);
      window.removeEventListener('keydown', handleKeydown);
    };
  });

  let stats = $derived((() => {
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
  })());

</script>

<div class="container">
  <div class="page-head">
    <div>
      <div class="crumbs">
        {$t('nav.group_proxy')} <span style="color:var(--fg-faint);margin:0 6px;">/</span>
        {$t('nav.subscriptions')}
      </div>
      <h1>{$t('subscr.title')}</h1>
      <p class="sub">{$t('subscr.subtitle')}</p>
    </div>
    <div class="ph-actions">
      <button class="btn btn-secondary" on:click={refreshAll} disabled={loading}>
        <svg
          width="14"
          height="14"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          stroke-width="2"
          style="margin-right: 6px;"><path d="M21 12a9 9 0 1 1-3-6.7L21 8M21 3v5h-5" /></svg
        >
        {$t('subscr.refresh_all')}
      </button>
      <button class="btn btn-primary" on:click={openAddModal}>
        <svg
          width="14"
          height="14"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          stroke-width="2"
          style="margin-right: 6px;"><path d="M12 5v14M5 12h14" /></svg
        >
        {$t('subscr.add')}
      </button>
    </div>
  </div>

  {#if $capabilities?.xray && !$capabilities.xray.conf_dir_exists}
    <div class="confdir-warning">
      <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" style="flex-shrink:0"><path d="M10.29 3.86L1.82 18a2 2 0 0 0 1.71 3h16.94a2 2 0 0 0 1.71-3L13.71 3.86a2 2 0 0 0-3.42 0z"/><line x1="12" y1="9" x2="12" y2="13"/><line x1="12" y1="17" x2="12.01" y2="17"/></svg>
      <span>{$t('subscr.confdir_warning').replace('{dir}', $capabilities.xray.conf_dir)}</span>
    </div>
  {/if}

  {#if subscriptions.length === 0}
    <div
      class="card text-center"
      style="padding: 3rem; display: flex; flex-direction: column; align-items: center; justify-content: center; gap: 1rem;"
    >
      <p style="color: var(--fg-secondary); margin: 0;">{$t('subscr.empty')}</p>
      <button class="btn btn-primary" on:click={openAddModal}>
        <svg
          width="14"
          height="14"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          stroke-width="2"
          style="margin-right: 6px;"><path d="M12 5v14M5 12h14" /></svg
        >
        {$t('subscr.add_first')}
      </button>
    </div>
  {:else}
    <div class="stats-chips-row mb-3">
      <span class="chip chip-default">
        <b>{stats.total}</b> {$currentLang === 'ru' ? 'подписки' : 'subscriptions'}
      </span>
      <span class="chip chip-default">
        <b>{stats.nodes}</b> {$currentLang === 'ru' ? 'узлов суммарно' : 'nodes total'}
      </span>
      {#if stats.next !== '—'}
        <span class="chip chip-default chip--icon">
          <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" class="timer-icon">
            <circle cx="12" cy="12" r="10"/><polyline points="12 6 12 12 16 14"/>
          </svg>
          <span>
            {$currentLang === 'ru' ? 'след. обновление через' : 'next update in'} <b>{stats.next}</b>
          </span>
        </span>
      {/if}
    </div>

    <div class="subscriptions-list">
      {#each subscriptions as sub}
        <div class="card sub-card">
          <div class="sub-card-layout">
            <div class="sub-card-left">
              <div class="type-dot-wrapper">
                <div class="type-dot" class:mihomo={sub.type === 'mihomo'} title={sub.type === 'mihomo' ? 'Mihomo' : 'XRay'}></div>
              </div>
              <div class="sub-card-content">
                <SubscriptionHeader sub={sub} onEdit={() => openEditModal(sub)} />
                <div class="sub-url-row">
                  <span class="sub-url-text" title={sub.url}>{sub.url}</span>
                </div>
                <div class="sub-updated-row">
                  {$t('subscr.updated_at').replace('{date}', formatDate(sub.last_update))}
                </div>
                <div class="sub-chips-wrapper">
                  <SubscriptionChips sub={sub} />
                </div>
                {#if sub.hwid_locked}
                  <div class="hwid-locked-warning">
                    ⚠ {$t('subscr.hwid_locked_warning')}
                  </div>
                {/if}
              </div>
            </div>

            <div class="sub-card-right">
              <div class="sub-actions-wrapper">
                <button
                  class="btn {sub.last_error ? 'btn-danger-outline' : 'btn-secondary'} btn-sm"
                  on:click={() => refreshSubscription(sub.id)}
                  disabled={refreshLoading[sub.id]}
                >
                  {#if refreshLoading[sub.id]}
                    <span class="spinner" style="margin-right: 4px;">...</span>
                    {$t('app.loading')}
                  {:else if sub.last_error}
                    {$currentLang === 'ru' ? 'Повторить' : 'Retry'}
                  {:else}
                    {$t('subscr.refresh')}
                  {/if}
                </button>

                {#if $devMode}
                <button
                  class="btn btn-secondary btn-sm"
                  on:click={() => openDiagnosticModal(sub)}
                >
                  🔍 {$t('subscr.diag_btn')}
                </button>
                {/if}

                <div class="dropdown-container">
                  <button
                    class="btn btn-secondary action-btn-dots"
                    on:click={() => toggleDropdown(sub.id)}>⋯</button
                  >
                  {#if activeDropdownId === sub.id}
                    <div class="dropdown-menu">
                      <button
                        on:click={() => {
                          openEditModal(sub);
                          activeDropdownId = null;
                        }}>{$t('app.edit')}</button
                      >
                      <button
                        on:click={() => {
                          deleteSubscription(sub.id);
                          activeDropdownId = null;
                        }}
                        class="delete-action">{$t('app.delete')}</button
                      >
                    </div>
                  {/if}
                </div>
              </div>
            </div>
          </div>

          <!-- Nodes link & Inline content -->
          <div class="nodes-preview-section">
            <button
              class="nodes-preview-toggle-btn"
              class:expanded={expandedSubs[sub.id]}
              on:click={() => toggleExpand(sub.id)}
            >
              <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" class="arrow-icon" class:rotated={expandedSubs[sub.id]}>
                <polyline points="9 18 15 12 9 6"/>
              </svg>
              <span>{$t('subscr.detail.open')} ({sub.proxy_count || 0})</span>
            </button>

            {#if expandedSubs[sub.id]}
              <div class="nodes-preview-content">
                {#if subNodesLoading[sub.id]}
                  <div class="loading-nodes">
                    <span class="spinner-xs"></span>
                    <span style="margin-left: 8px;">{$t('app.loading')}</span>
                  </div>
                {:else}
                  <!-- Announcement -->
                  {#if sub.announcement}
                    <div class="inline-announcement-warn">
                      <span class="inline-warn-icon">!</span>
                      <span class="inline-warn-text">
                        {#each parseSimpleMarkdown(sub.announcement) as token}
                          {#if token.type === 'text'}
                            {token.value}
                          {:else if token.type === 'bold'}
                            <strong>{token.value}</strong>
                          {:else if token.type === 'italic'}
                            <em>{token.value}</em>
                          {:else if token.type === 'link'}
                            <a href={token.url} target="_blank" rel="noopener noreferrer">{token.text}</a>
                          {/if}
                        {/each}
                      </span>
                    </div>
                  {/if}

                  <!-- Nodes list -->
                  {#if !subNodes[sub.id] || subNodes[sub.id].length === 0}
                    <div class="empty-nodes">
                      {$t('subscr.detail.no_nodes')}
                    </div>
                  {:else}
                    <div class="inline-nodes-list">
                      {#each subNodes[sub.id] as node}
                        {@const h = subHealth[sub.id]?.[node.tag]}
                        {@const isNodeActive = node.active}
                        <!-- svelte-ignore a11y_click_events_have_key_events -->
                        <!-- svelte-ignore a11y_no_static_element_interactions -->
                        <div
                          class="sub-node-row"
                          class:active={isNodeActive}
                          on:click={() => {
                            if (sub.type !== 'mihomo') {
                              setActiveNode(sub.id, node.tag, sub.type);
                            }
                          }}
                        >
                          {#if isNodeActive}
                            <div class="sub-node-active-bar"></div>
                          {/if}

                          <!-- Flag Avatar -->
                          <div class="sub-node-avatar-container" class:active={isNodeActive}>
                            {#if flagsSupported && node.flag}
                              <span class="sub-node-flag">{node.flag}</span>
                            {:else}
                              {#if node.country}
                                <span class="sub-node-avatar-text">{node.country}</span>
                              {:else}
                                <span class="sub-node-flag-fallback">🌐</span>
                              {/if}
                            {/if}
                          </div>

                          <!-- Text Info -->
                          <div class="sub-node-info">
                            <div class="sub-node-name-row">
                              <span class="sub-node-name">{node.name || $t('country.' + node.country) || node.tag}</span>
                              {#if node.is_new}
                                <span class="sub-node-badge-new">NEW</span>
                              {/if}
                            </div>
                            <div class="sub-node-meta-row">
                              {#if node.use_case || node.speed}
                                <span class="sub-node-meta-text">
                                  {node.use_case || ''}
                                  {#if node.use_case && node.speed} — {/if}
                                  {node.speed || ''}
                                </span>
                              {:else}
                                <span class="sub-node-meta-text">
                                  {node.protocol || ''}
                                  {#if node.protocol && node.transport} — {/if}
                                  {node.transport || ''}
                                </span>
                              {/if}
                            </div>
                          </div>

                          <!-- Status / Ping right -->
                          <div class="sub-node-status-container">
                            <button
                              class="sub-node-ping-btn"
                              on:click={(e) => {
                                e.stopPropagation();
                                checkNodeHealth(sub.id, node.tag);
                              }}
                              disabled={checkingNodes[sub.id]?.[node.tag]}
                              title="Проверить пинг"
                            >
                              {#if checkingNodes[sub.id]?.[node.tag]}
                                <span class="spinner-xs"></span>
                              {:else}
                                {#if h}
                                  <span class="sub-node-ping-val {latencyClass(h)}">{latencyLabel(h)}</span>
                                  {#if h.alive}
                                    <div class="sub-node-status-icon success">
                                      <svg width="8" height="8" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="4">
                                        <polyline points="20 6 9 17 4 12"/>
                                      </svg>
                                    </div>
                                  {:else}
                                    <div class="sub-node-status-icon danger">
                                      <svg width="8" height="8" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="4">
                                        <line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/>
                                      </svg>
                                    </div>
                                  {/if}
                                {:else}
                                  <div class="sub-node-status-icon default-ok">
                                    <svg width="8" height="8" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="4">
                                      <polyline points="20 6 9 17 4 12"/>
                                    </svg>
                                  </div>
                                {/if}
                              {/if}
                            </button>
                          </div>
                        </div>
                      {/each}
                    </div>
                  {/if}
                {/if}
              </div>
            {/if}
          </div>
        </div>
      {/each}
    </div>
  {/if}
</div>

{#if showAddModal}
  <div
    class="modal-overlay"
    role="button"
    tabindex="0"
    on:click={closeModal}
    on:keydown={handleKeydown}
  >
    <div class="modal-card" role="presentation" on:click|stopPropagation>
      <div class="modal-card-header">
        <h2>{editingSub ? $t('subscr.edit_title') : $t('subscr.add_title')}</h2>
        <button class="modal-close-btn" on:click={closeModal}>&times;</button>
      </div>
      <div class="modal-card-body">
        <div class="form-group">
          <label for="form-name" class="form-label">{$t('subscr.name')}</label>
          <input
            id="form-name"
            type="text"
            class="input"
            bind:value={formName}
            placeholder={$t('subscr.name_placeholder')}
          />
        </div>

        <div class="form-group">
          <label for="form-url" class="form-label">{$t('subscr.url')}</label>
          <input
            id="form-url"
            type="text"
            class="input"
            bind:value={formURL}
            placeholder="https://..."
          />
        </div>

        <div class="form-group">
          <label for="form-tag-prefix" class="form-label">{$t('subscr.tag_prefix')}</label>
          <input
            id="form-tag-prefix"
            type="text"
            class="input"
            bind:value={formTagPrefix}
            placeholder={$t('subscr.tag_prefix_placeholder')}
          />
        </div>

        <div class="form-group">
          <label for="form-interval" class="form-label"
            >{$t('subscr.interval')} ({$currentLang === 'ru' ? 'часов' : 'hours'})</label
          >
          <input
            id="form-interval"
            type="number"
            class="input"
            bind:value={formInterval}
            min="1"
            max="168"
          />
        </div>

        <div class="form-group">
          <label for="form-filter-name" class="form-label">{$t('subscr.filter_name')}</label>
          <input
            id="form-filter-name"
            type="text"
            class="input"
            bind:value={formFilterName}
            placeholder={$t('subscr.filter_placeholder')}
          />
        </div>

        <div class="form-group">
          <label for="form-filter-type" class="form-label">{$t('subscr.filter_type')}</label>
          <input
            id="form-filter-type"
            type="text"
            class="input"
            bind:value={formFilterType}
            placeholder="vmess, vless, trojan..."
          />
        </div>

        <div class="form-group">
          <label for="form-filter-transport" class="form-label">{$t('subscr.filter_transport')}</label>
          <input
            id="form-filter-transport"
            type="text"
            class="input"
            bind:value={formFilterTransport}
            placeholder="ws, grpc, tcp..."
          />
        </div>

        <div class="form-group-checkbox">
          <label class="toggle-switch">
            <input type="checkbox" id="enabled" bind:checked={formEnabled} />
            <span class="toggle-slider"></span>
          </label>
          <label for="enabled" class="checkbox-label">{$t('subscr.enabled')}</label>
        </div>

        <div class="form-group-checkbox">
          <label class="toggle-switch">
            <input type="checkbox" id="use-provider-interval" bind:checked={formUseProviderInterval} />
            <span class="toggle-slider"></span>
          </label>
          <label for="use-provider-interval" class="checkbox-label">
            {$t('subscr.use_provider_interval')}
            {#if editingSub && editingSub.profile_update_hours && editingSub.profile_update_hours > 0}
              <span style="color: var(--accent); font-size: 11px; margin-left: 4px;">
                ({$t('subscr.provider_dictates').replace('{hours}', String(editingSub.profile_update_hours))})
              </span>
            {/if}
          </label>
        </div>
      </div>
      <div class="modal-card-footer">
        <button class="btn btn-secondary" on:click={closeModal}>{$t('app.cancel')}</button>
        <button class="btn btn-primary" on:click={saveSubscription}>{$t('app.save')}</button>
      </div>
    </div>
  </div>
{/if}

{#if showDiagnosticModal && diagnosticSub}
  <div
    class="modal-overlay"
    role="button"
    tabindex="0"
    on:click={closeDiagnosticModal}
    on:keydown={handleKeydown}
  >
    <div class="modal-card modal-large" role="presentation" on:click|stopPropagation>
      <div class="modal-card-header">
        <h2>{$t('subscr.diag_title').replace('{name}', diagnosticSub.name)}</h2>
        <button class="modal-close-btn" on:click={closeDiagnosticModal}>&times;</button>
      </div>

      <div class="diag-tabs">
        <button
          class="diag-tab-btn"
          class:active={diagnosticTab === 'report'}
          on:click={() => diagnosticTab = 'report'}
        >
          {$t('subscr.tab_report')}
        </button>
        <button
          class="diag-tab-btn"
          class:active={diagnosticTab === 'headers'}
          on:click={() => diagnosticTab = 'headers'}
        >
          {$t('subscr.tab_headers')}
        </button>
        <button
          class="diag-tab-btn"
          class:active={diagnosticTab === 'raw'}
          on:click={() => diagnosticTab = 'raw'}
        >
          {$t('subscr.tab_raw')}
        </button>
      </div>

      <div class="modal-card-body diag-body">
        {#if diagnosticLoading}
          <div class="text-center" style="padding: 2rem 0; color: var(--fg-dim);">
            <span class="spinner" style="margin-right: 8px;">...</span>
            {$t('subscr.loading_diag')}
          </div>
        {:else}
          {#if diagnosticTab === 'report'}
            <div class="tab-content">
              <div class="diag-summary-cards">
                <div class="diag-sum-card success">
                  <div class="title">{$t('subscr.diag_parsed')}</div>
                  <div class="val">{parseReportData?.parsed_count ?? 0}</div>
                </div>
                <div class="diag-sum-card warning">
                  <div class="title">{$t('subscr.diag_skipped')}</div>
                  <div class="val">{parseReportData?.skipped_count ?? 0}</div>
                </div>
                <div class="diag-sum-card">
                  <div class="title">{$t('subscr.diag_time')}</div>
                  <div class="val">{formatDate(parseReportData?.timestamp || '')}</div>
                </div>
              </div>

              <div class="diag-table-wrapper">
                {#if parseReportData && parseReportData.skipped && parseReportData.skipped.length > 0}
                  <table class="diag-table">
                    <thead>
                      <tr>
                        <th style="width: 80px;">{$t('subscr.table_line')}</th>
                        <th>{$t('subscr.table_reason')}</th>
                        <th>{$t('subscr.table_snippet')}</th>
                      </tr>
                    </thead>
                    <tbody>
                      {#each parseReportData.skipped as item}
                        <tr>
                          <td class="line-num">{item.line}</td>
                          <td class="reason">{item.reason}</td>
                          <td class="snippet"><code>{item.snippet}</code></td>
                        </tr>
                      {/each}
                    </tbody>
                  </table>
                {:else}
                  <div class="text-center" style="padding: 1.5rem; color: var(--fg-secondary);">
                    {$t('subscr.no_skips')}
                  </div>
                {/if}
              </div>
            </div>
          {:else if diagnosticTab === 'headers'}
            <div class="tab-content">
              {#if rawResponseData && rawResponseData.headers}
                <div class="diag-headers-list">
                  {#each Object.entries(rawResponseData.headers) as [key, val]}
                    <div class="hdr-item">
                      <span class="hdr-key">{key}</span>
                      <span class="hdr-val">{val.join(', ')}</span>
                    </div>
                  {/each}
                </div>
              {:else}
                <div class="text-center" style="padding: 1.5rem; color: var(--fg-secondary);">
                  —
                </div>
              {/if}
            </div>
          {:else if diagnosticTab === 'raw'}
            <div class="tab-content height-100">
              {#if rawResponseData && rawResponseData.body}
                <pre class="raw-body-pre"><code>{rawResponseData.body}</code></pre>
              {:else}
                <div class="text-center" style="padding: 1.5rem; color: var(--fg-secondary);">
                  —
                </div>
              {/if}
            </div>
          {/if}
        {/if}
      </div>

      <div class="modal-card-footer">
        <button class="btn btn-secondary" on:click={closeDiagnosticModal}>{$t('app.close')}</button>
      </div>
    </div>
  </div>
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

  .subscriptions-list {
    display: grid;
    grid-template-columns: 1fr;
    gap: 14px;
  }

  .sub-card {
    padding: 20px 24px;
    display: flex;
    flex-direction: column;
    gap: 16px;
    position: relative;
    overflow: hidden;
  }

  .sub-card-layout {
    display: flex;
    justify-content: space-between;
    align-items: flex-start;
    gap: 20px;
  }

  .sub-card-left {
    display: flex;
    gap: 14px;
    flex: 1;
    min-width: 0;
  }

  .type-dot-wrapper {
    padding-top: 6px;
    display: flex;
    align-items: center;
    justify-content: center;
  }

  .type-dot {
    width: 8px;
    height: 8px;
    border-radius: 50%;
    background: var(--accent);
    box-shadow: 0 0 6px var(--accent);
    flex-shrink: 0;
  }
  .type-dot.mihomo {
    background: #8b5cf6;
    box-shadow: 0 0 6px #8b5cf6;
  }

  .sub-card-content {
    display: flex;
    flex-direction: column;
    gap: 10px;
    flex: 1;
    min-width: 0;
  }

  .sub-url-row {
    font-family: var(--font-family-mono);
    font-size: 12px;
    color: var(--fg-secondary);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
    max-width: 100%;
  }

  .sub-url-text {
    cursor: pointer;
  }

  .hwid-locked-warning {
    margin-top: 6px;
    font-size: 11px;
    color: var(--color-warning, #f59e0b);
    opacity: 0.9;
  }

  .sub-updated-row {
    font-size: 11.5px;
    color: var(--fg-dim);
    margin-top: -4px;
  }

  .sub-card-right {
    display: flex;
    align-items: flex-start;
    flex-shrink: 0;
  }

  .sub-actions-wrapper {
    display: flex;
    align-items: center;
    gap: 8px;
  }

  .sub-card .btn-sm {
    height: 32px;
    padding: 0 12px;
    font-size: 12px;
    border-radius: var(--radius-md);
    display: inline-flex;
    align-items: center;
    justify-content: center;
  }

  /* Раздел предпросмотра узлов */
  .nodes-preview-section {
    border-top: 1px solid var(--border);
    margin: 0 -24px -20px;
  }

  .nodes-preview-toggle-btn {
    width: 100%;
    background: rgba(0, 0, 0, 0.08);
    border: none;
    padding: 10px 24px;
    display: flex;
    align-items: center;
    gap: 8px;
    font-size: 12px;
    font-weight: 600;
    color: var(--fg-secondary);
    cursor: pointer;
    text-align: left;
    transition: background var(--transition-fast), color var(--transition-fast);
  }

  .nodes-preview-toggle-btn:hover {
    background: rgba(255, 255, 255, 0.02);
    color: var(--fg-primary);
  }

  .arrow-icon {
    transition: transform var(--transition-fast);
    color: var(--fg-dim);
  }

  .arrow-icon.rotated {
    transform: rotate(90deg);
  }

  .nodes-preview-content {
    padding: 16px 24px 20px;
    background: rgba(0, 0, 0, 0.12);
    display: flex;
    flex-direction: column;
    gap: 12px;
    border-bottom-left-radius: var(--radius-lg);
    border-bottom-right-radius: var(--radius-lg);
  }

  .nodes-preview-info {
    font-size: 11.5px;
    color: var(--warning);
    background: rgba(240, 180, 80, 0.05);
    border: 1px solid rgba(240, 180, 80, 0.15);
    padding: 8px 12px;
    border-radius: var(--radius-md);
    line-height: 1.4;
  }

  .stats-chips-row {
    display: flex;
    flex-wrap: wrap;
    gap: 8px;
  }

  .timer-icon {
    color: var(--fg-dim);
  }

  .timer-icon :global(polyline) {
    animation: clockRotate 4s linear infinite;
    transform-origin: 12px 12px;
  }

  @keyframes clockRotate {
    from {
      transform: rotate(0deg);
    }
    to {
      transform: rotate(360deg);
    }
  }

  /* Dropdown Styles */
  .dropdown-container {
    position: relative;
    display: inline-block;
  }

  .action-btn-dots {
    height: 32px;
    width: 32px;
    padding: 0;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    border-radius: var(--radius-md);
    background: transparent !important;
    border: none !important;
    color: var(--fg-secondary) !important;
    font-size: 14px;
    cursor: pointer;
  }
  .action-btn-dots:hover {
    color: var(--accent) !important;
    background: var(--hover) !important;
  }

  .dropdown-menu {
    position: absolute;
    right: 0;
    top: 100%;
    margin-top: 6px;
    background: var(--bg-card);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    box-shadow: 0 10px 25px rgba(0, 0, 0, 0.3);
    z-index: 100;
    min-width: 140px;
    overflow: hidden;
    display: flex;
    flex-direction: column;
  }

  .dropdown-menu button {
    background: none;
    border: none;
    padding: 10px 14px;
    text-align: left;
    font-size: 13px;
    color: var(--fg-primary);
    cursor: pointer;
    width: 100%;
    transition: background var(--transition-fast);
  }

  .dropdown-menu button:hover {
    background: var(--hover);
  }

  .dropdown-menu button.delete-action {
    color: var(--danger);
  }

  .dropdown-menu button.delete-action:hover {
    background: rgba(235, 94, 85, 0.1);
  }

  /* Modal Styles */
  .modal-overlay {
    position: fixed;
    top: 0;
    left: 0;
    right: 0;
    bottom: 0;
    background: rgba(0, 0, 0, 0.6);
    backdrop-filter: blur(4px);
    display: flex;
    align-items: center;
    justify-content: center;
    z-index: 1000;
    padding: 20px;
  }

  .modal-card {
    background: var(--bg-card);
    border: 1px solid var(--border);
    border-radius: var(--radius-lg);
    width: 100%;
    max-width: 520px;
    box-shadow: 0 20px 40px rgba(0, 0, 0, 0.5);
    overflow: hidden;
    display: flex;
    flex-direction: column;
    max-height: 90vh;
    animation: modal-anim 0.2s cubic-bezier(0.16, 1, 0.3, 1);
  }

  @keyframes modal-anim {
    from {
      transform: scale(0.95) translateY(10px);
      opacity: 0;
    }
    to {
      transform: scale(1) translateY(0);
      opacity: 1;
    }
  }

  .modal-card-header {
    padding: 16px 24px;
    border-bottom: 1px solid var(--border);
    display: flex;
    justify-content: space-between;
    align-items: center;
  }

  .modal-card-header h2 {
    margin: 0;
    font-size: 16px;
    font-weight: 700;
    color: var(--fg-primary);
  }

  .modal-close-btn {
    background: none;
    border: none;
    color: var(--fg-dim);
    font-size: 24px;
    cursor: pointer;
    line-height: 1;
    padding: 4px;
  }

  .modal-close-btn:hover {
    color: var(--fg-primary);
  }

  .modal-card-body {
    padding: 24px;
    overflow-y: auto;
    display: flex;
    flex-direction: column;
    gap: 16px;
  }

  .form-group-checkbox {
    display: flex;
    align-items: center;
    gap: 12px;
    margin-top: 4px;
  }

  .checkbox-label {
    font-size: 13px;
    color: var(--fg-primary);
    cursor: pointer;
    user-select: none;
  }

  .modal-card-footer {
    padding: 16px 24px;
    border-top: 1px solid var(--border);
    display: flex;
    justify-content: flex-end;
    gap: 12px;
  }

  :global(.error-badge) {
    background: rgba(239, 68, 68, 0.12);
    color: #ef4444;
    border: 1px solid rgba(239, 68, 68, 0.3);
    font-size: 11px;
    font-weight: 600;
    padding: 2px 7px;
    border-radius: 4px;
  }

  :global(.btn-danger-outline) {
    background: rgba(239, 68, 68, 0.08);
    color: #ef4444;
    border: 1px solid rgba(239, 68, 68, 0.35);
  }
  :global(.btn-danger-outline:hover) {
    background: rgba(239, 68, 68, 0.16);
  }

  .modal-large {
    max-width: 800px;
    width: 100%;
  }

  .diag-tabs {
    display: flex;
    border-bottom: 1px solid var(--border);
    background: rgba(0, 0, 0, 0.08);
  }

  .diag-tab-btn {
    flex: 1;
    background: none;
    border: none;
    border-bottom: 2px solid transparent;
    padding: 12px;
    color: var(--fg-dim);
    font-size: 13px;
    font-weight: 600;
    cursor: pointer;
    transition: all var(--transition-fast);
  }

  .diag-tab-btn:hover {
    color: var(--fg-primary);
    background: rgba(255, 255, 255, 0.02);
  }

  .diag-tab-btn.active {
    color: var(--accent);
    border-bottom-color: var(--accent);
    background: rgba(255, 255, 255, 0.04);
  }

  .diag-body {
    padding: 20px;
    max-height: 60vh;
    overflow-y: auto;
  }

  .diag-summary-cards {
    display: grid;
    grid-template-columns: repeat(3, 1fr);
    gap: 12px;
    margin-bottom: 20px;
  }

  .diag-sum-card {
    background: var(--accent-soft);
    border: 1px solid var(--accent-line);
    border-radius: var(--radius-md);
    padding: 12px 16px;
  }

  .diag-sum-card.success {
    background: rgba(16, 185, 129, 0.06);
    border-color: rgba(16, 185, 129, 0.2);
    color: #10b981;
  }

  .diag-sum-card.warning {
    background: rgba(245, 158, 11, 0.06);
    border-color: rgba(245, 158, 11, 0.2);
    color: #f59e0b;
  }

  .diag-sum-card .title {
    font-size: 11px;
    text-transform: uppercase;
    letter-spacing: 0.05em;
    color: var(--fg-dim);
  }

  .diag-sum-card .val {
    font-size: 20px;
    font-weight: 700;
    margin-top: 4px;
    font-family: var(--font-family-mono);
  }

  .diag-table-wrapper {
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    overflow: hidden;
  }

  .diag-table {
    width: 100%;
    border-collapse: collapse;
    font-size: 12px;
  }

  .diag-table th, .diag-table td {
    padding: 10px 12px;
    text-align: left;
    border-bottom: 1px solid var(--border);
  }

  .diag-table th {
    background: rgba(0, 0, 0, 0.15);
    color: var(--fg-primary);
    font-weight: 600;
  }

  .diag-table tr:last-child td {
    border-bottom: none;
  }

  .diag-table .line-num {
    font-family: var(--font-family-mono);
    color: var(--fg-dim);
  }

  .diag-table .reason {
    color: var(--danger);
  }

  .diag-table .snippet code {
    font-family: var(--font-family-mono);
    background: rgba(0, 0, 0, 0.2);
    padding: 2px 4px;
    border-radius: 3px;
    word-break: break-all;
  }

  .diag-headers-list {
    display: flex;
    flex-direction: column;
    gap: 8px;
  }

  .hdr-item {
    display: flex;
    flex-direction: column;
    padding: 8px 12px;
    background: rgba(0, 0, 0, 0.1);
    border-radius: var(--radius-sm);
    border: 1px solid var(--border);
  }

  .hdr-key {
    font-weight: 700;
    font-size: 12px;
    color: var(--accent);
    font-family: var(--font-family-mono);
  }

  .hdr-val {
    font-size: 12px;
    font-family: var(--font-family-mono);
    margin-top: 4px;
    word-break: break-all;
    color: var(--fg-primary);
  }

  .raw-body-pre {
    margin: 0;
    padding: 16px;
    background: rgba(0, 0, 0, 0.2);
    border-radius: var(--radius-md);
    border: 1px solid var(--border);
    overflow: auto;
    max-height: 45vh;
    font-family: var(--font-family-mono);
    font-size: 12px;
    line-height: 1.5;
    white-space: pre-wrap;
    word-break: break-all;
  }

  /* Стили для встроенного списка узлов */
  .loading-nodes, .empty-nodes {
    padding: 16px;
    text-align: center;
    color: var(--fg-dim);
    font-size: 13px;
    display: flex;
    align-items: center;
    justify-content: center;
  }

  .inline-announcement-warn {
    display: flex;
    align-items: flex-start;
    gap: 8px;
    background: rgba(239, 68, 68, 0.05);
    border: 1px solid rgba(239, 68, 68, 0.15);
    border-radius: var(--radius-sm, 4px);
    padding: 8px 12px;
    margin-bottom: 12px;
    text-align: left;
  }

  .inline-warn-icon {
    color: var(--danger);
    font-weight: bold;
    font-size: 14px;
    line-height: 1;
    margin-top: 1px;
  }

  .inline-warn-text {
    font-size: 11.5px;
    color: var(--fg-secondary);
    line-height: 1.4;
    white-space: pre-wrap;
  }

  .inline-warn-text a {
    color: var(--accent);
    text-decoration: underline;
  }

  .inline-warn-text a:hover {
    text-decoration: none;
  }

  .inline-nodes-list {
    display: flex;
    flex-direction: column;
    border: 1px solid var(--border);
    border-radius: var(--radius-md, 6px);
    overflow: hidden;
    background: var(--bg-card);
  }

  .sub-node-row {
    position: relative;
    display: flex;
    align-items: center;
    padding: 8px 12px;
    border-bottom: 1px solid var(--border);
    cursor: pointer;
    transition: background var(--transition-fast);
  }

  .sub-node-row:last-child {
    border-bottom: none;
  }

  .sub-node-row:hover {
    background: rgba(255, 255, 255, 0.02);
  }

  .sub-node-row.active {
    background: rgba(25, 118, 210, 0.05);
  }

  .sub-node-active-bar {
    position: absolute;
    left: 0;
    top: 0;
    bottom: 0;
    width: 3px;
    background: var(--accent);
  }

  .sub-node-avatar-container {
    width: 28px;
    height: 28px;
    border-radius: 50%;
    background: rgba(255, 255, 255, 0.05);
    border: 1px solid var(--border);
    display: flex;
    align-items: center;
    justify-content: center;
    margin-right: 10px;
    flex-shrink: 0;
    font-size: 15px;
    transition: all var(--transition-fast);
  }

  .sub-node-avatar-container.active {
    background: var(--accent);
    border-color: var(--accent);
    color: white;
  }

  .sub-node-avatar-text {
    font-size: 9px;
    font-weight: 700;
    text-transform: uppercase;
    color: var(--fg-secondary);
  }

  .sub-node-avatar-container.active .sub-node-avatar-text {
    color: white;
  }

  .sub-node-flag-fallback {
    font-size: 13px;
  }

  .sub-node-info {
    flex: 1;
    min-width: 0;
  }

  .sub-node-name-row {
    display: flex;
    align-items: center;
    gap: 6px;
    margin-bottom: 1px;
  }

  .sub-node-name {
    font-size: 12.5px;
    font-weight: 500;
    color: var(--fg-primary);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }

  .sub-node-badge-new {
    font-size: 8px;
    font-weight: 700;
    color: #f59e0b;
    background: rgba(245, 158, 11, 0.15);
    border: 1px solid rgba(245, 158, 11, 0.3);
    border-radius: 3px;
    padding: 0px 3px;
    letter-spacing: 0.05em;
  }

  .sub-node-meta-row {
    display: flex;
    align-items: center;
  }

  .sub-node-meta-text {
    font-size: 10.5px;
    color: var(--fg-dim);
  }

  .sub-node-status-container {
    display: flex;
    align-items: center;
    gap: 6px;
    flex-shrink: 0;
    margin-left: 10px;
  }

  .sub-node-ping-btn {
    background: none;
    border: none;
    padding: 0;
    margin: 0;
    cursor: pointer;
    display: flex;
    align-items: center;
    gap: 6px;
    transition: opacity var(--transition-fast);
  }

  .sub-node-ping-btn:hover {
    opacity: 0.8;
  }

  .sub-node-ping-btn:disabled {
    cursor: not-allowed;
    opacity: 0.5;
  }

  .sub-node-ping-val {
    font-size: 10.5px;
    font-family: var(--font-family-mono);
    color: var(--fg-dim);
  }

  .sub-node-status-icon {
    width: 14px;
    height: 14px;
    border-radius: 50%;
    display: flex;
    align-items: center;
    justify-content: center;
    flex-shrink: 0;
  }

  .sub-node-status-icon.success {
    background: rgba(34, 197, 94, 0.15);
    border: 1px solid rgba(34, 197, 94, 0.3);
    color: #22c55e;
  }

  .sub-node-status-icon.danger {
    background: rgba(239, 68, 68, 0.15);
    border: 1px solid rgba(239, 68, 68, 0.3);
    color: var(--danger);
  }

  .sub-node-status-icon.default-ok {
    background: rgba(34, 197, 94, 0.15);
    border: 1px solid rgba(34, 197, 94, 0.3);
    color: #22c55e;
  }

  .latency-good { color: #22c55e; }
  .latency-ok { color: #f59e0b; }
  .latency-bad { color: var(--danger); }
  .latency-unknown { color: var(--fg-faint); }

  @media (max-width: 768px) {
    .sub-card-layout {
      flex-direction: column;
      gap: 16px;
    }
    .sub-card-right {
      width: 100%;
      justify-content: flex-end;
    }
  }
</style>
