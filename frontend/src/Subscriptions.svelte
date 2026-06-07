<script lang="ts">
  import { onMount } from 'svelte';
  import { t, currentLang, pluralize } from './i18n';
  import {
    showConfirm,
    capabilities,
    fetchCapabilities,
    showToast,
    devMode,
    fetchDevMode
  } from './stores';

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
    announcement?: string;
    mihomo_integrated?: boolean;
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
    if (gb >= 1) return `${gb.toFixed(2)} GB`;
    const mb = bytes / (1024 * 1024);
    return `${mb.toFixed(2)} MB`;
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

  function getNodeServer(node: any): string {
    if (!node || !node.settings) return '';
    if (node.settings.vnext && node.settings.vnext[0]) {
      return node.settings.vnext[0].address || '';
    }
    if (node.settings.servers && node.settings.servers[0]) {
      return node.settings.servers[0].address || '';
    }
    return '';
  }

  function getNodePort(node: any): string {
    if (!node || !node.settings) return '';
    if (node.settings.vnext && node.settings.vnext[0]) {
      return String(node.settings.vnext[0].port || '');
    }
    if (node.settings.servers && node.settings.servers[0]) {
      return String(node.settings.servers[0].port || '');
    }
    return '';
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
  let showAdvanced = $state(false);

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
    showAdvanced = false;
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
    showAdvanced = false;
    showAddModal = true;
  }

  function closeModal() {
    showAddModal = false;
    editingSub = null;
    showAdvanced = false;
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

  function formatUpdateDate(dateStr: string): string {
    if (!dateStr || dateStr.startsWith('0001')) return '—';
    const d = new Date(dateStr);
    const pad = (num: number) => String(num).padStart(2, '0');
    const day = pad(d.getDate());
    const month = pad(d.getMonth() + 1);
    const year = String(d.getFullYear()).slice(-2);
    const hours = pad(d.getHours());
    const minutes = pad(d.getMinutes());
    return `${day}.${month}.${year}, ${hours}:${minutes}`;
  }

  interface AnnouncementLine {
    isWarn: boolean;
    text: string;
  }

  function parseAnnouncementLines(text: string): AnnouncementLine[] {
    if (!text) return [];
    return text.split('\n').map((line) => {
      line = line.trim();
      let isWarn = false;
      let cleanText = line;
      if (line.startsWith('!')) {
        isWarn = true;
        cleanText = line.substring(1).trim();
      } else if (line.startsWith('⚠')) {
        isWarn = true;
        cleanText = line.substring(1).trim();
      } else if (line.startsWith('|')) {
        isWarn = true;
        cleanText = line.substring(1).trim();
      }
      return {
        isWarn,
        text: cleanText
      };
    });
  }

  interface ExpireDaysInfo {
    expired: boolean;
    days: number | null;
    text: string;
  }

  function getExpireDays(expire?: number): ExpireDaysInfo | null {
    if (!expire || expire <= 0) return null;
    const diff = expire * 1000 - Date.now();
    if (diff <= 0) return { expired: true, days: null, text: $t('subscr.expired') };
    const days = Math.ceil(diff / (24 * 3600 * 1000));
    return {
      expired: false,
      days,
      text: $t('subscr.expires_in').replace('{days}', String(days))
    };
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
        showToast('success', $t('subscr.detail.activated'));
        if (subNodes[subId]) {
          subNodes[subId] = subNodes[subId].map((n) => ({ ...n, active: n.tag === nodeTag }));
        }
      } else if (res.status === 409) {
        showToast('error', $t('subscr.detail.auto_routing_conflict'));
      } else {
        const err = await res.json().catch(() => ({}));
        showToast('error', err.error || $t('subscr.detail.activate_error'));
      }
    } catch (e: any) {
      showToast('error', e.message || $t('app.error'));
    } finally {
      activatingNode[subId] = null;
    }
  }

  function getCountryColorStyle(countryCode: string | undefined): string {
    if (!countryCode) return '';
    const code = countryCode.toUpperCase();
    const styles: Record<string, string> = {
      EU: 'background: linear-gradient(135deg, #0b3c98, #072561); color: #ffffff; text-shadow: 0 1px 2px rgba(0,0,0,0.3); border-color: rgba(41, 194, 240, 0.3);',
      RU: 'background: linear-gradient(135deg, #1e88e5, #e53935); color: #ffffff; text-shadow: 0 1px 2px rgba(0,0,0,0.3);',
      DE: 'background: linear-gradient(135deg, #ffb300, #ff3d00, #212121); color: #ffffff; text-shadow: 0 1px 2px rgba(0,0,0,0.4);',
      NL: 'background: linear-gradient(135deg, #ff7043, #d84315); color: #ffffff; text-shadow: 0 1px 2px rgba(0,0,0,0.3);',
      PL: 'background: linear-gradient(180deg, #ffffff 50%, #e91e63 50%); color: #333333; box-shadow: inset 0 0 4px rgba(0,0,0,0.15); border-color: rgba(255, 255, 255, 0.1);',
      FI: 'background: linear-gradient(135deg, #ffffff 40%, #0d47a1 40%); color: #0d47a1;',
      LT: 'background: linear-gradient(135deg, #4caf50, #ffeb3b, #f44336); color: #ffffff; text-shadow: 0 1px 2px rgba(0,0,0,0.4);',
      EE: 'background: linear-gradient(135deg, #29b6f6, #212121, #ffffff); color: #ffffff; text-shadow: 0 1px 2px rgba(0,0,0,0.4);',
      ES: 'background: linear-gradient(135deg, #e53935, #ffeb3b, #e53935); color: #ffffff; text-shadow: 0 1px 2px rgba(0,0,0,0.4);',
      US: 'background: linear-gradient(135deg, #0d47a1, #b71c1c); color: #ffffff; text-shadow: 0 1px 2px rgba(0,0,0,0.3);',
      AM: 'background: linear-gradient(135deg, #e53935, #0d47a1, #ffb300); color: #ffffff; text-shadow: 0 1px 2px rgba(0,0,0,0.4);'
    };
    return (
      styles[code] ??
      'background: linear-gradient(135deg, #424242, #212121); color: var(--fg-primary);'
    );
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
        flagsSupported = widthFlag > widthLetters;
      }
    } catch (e) {
      flagsSupported = false;
    }

    loadSubscriptions().then(() => {
      checkAutoExpand();
    });
    fetchCapabilities();
    fetchDevMode();

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

  function checkAutoExpand() {
    const expandId = sessionStorage.getItem('expand_subscription_id');
    if (expandId) {
      sessionStorage.removeItem('expand_subscription_id');
      toggleExpand(expandId).then(() => {
        setTimeout(() => {
          const el = document.getElementById(`sub-card-${expandId}`);
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
        {pluralize(
          stats.total,
          $t('subscr.total_one', { count: String(stats.total) }),
          $t('subscr.total_few', { count: String(stats.total) }),
          $t('subscr.total_many', { count: String(stats.total) })
        )}
      </span>
      <span class="chip chip-default">
        <b>{stats.nodes}</b>
        {$currentLang === 'ru' ? 'узлов суммарно' : 'nodes total'}
      </span>
      {#if stats.next !== '—'}
        <span class="chip chip-default chip--icon">
          <svg
            width="12"
            height="12"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            stroke-width="2.5"
            class="timer-icon"
          >
            <circle cx="12" cy="12" r="10" /><polyline points="12 6 12 12 16 14" />
          </svg>
          <span>
            {$currentLang === 'ru' ? 'след. обновление через' : 'next update in'}
            <b>{stats.next}</b>
          </span>
        </span>
      {/if}
    </div>

    <div class="subscriptions-list">
      {#each subscriptions as sub}
        {@const exp = getExpireDays(sub.expire)}
        <div class="card sub-card" id="sub-card-{sub.id}">
          <!-- Хедер подписки -->
          <div class="sub-header-row">
            <!-- Левая колонка хедера -->
            <div class="sub-header-left">
              <!-- Стрелочка разворачивания нод -->
              <button
                class="collapse-toggle"
                class:expanded={expandedSubs[sub.id]}
                on:click={() => toggleExpand(sub.id)}
                aria-label="Toggle node list"
              >
                <svg
                  width="14"
                  height="14"
                  viewBox="0 0 24 24"
                  fill="none"
                  stroke="currentColor"
                  stroke-width="2.5"
                >
                  <polyline points="9 18 15 12 9 6" />
                </svg>
              </button>

              <!-- LED точка статуса (активна / выключена) -->
              <div
                class="type-dot"
                class:mihomo={sub.type === 'mihomo'}
                class:disabled={!sub.enabled}
                class:has-error={!!sub.last_error}
                title={sub.last_error || (sub.enabled ? $t('app.active') : $t('app.disabled'))}
              ></div>

              <!-- Имя подписки -->
              <h2 class="sub-name" on:click={() => toggleExpand(sub.id)}>
                {sub.profile_title || sub.name}
              </h2>
            </div>

            <!-- Правая колонка хедера -->
            <div class="sub-header-right">
              <!-- Дата обновления -->
              <span
                class="sub-update-time"
                title={$t('subscr.updated_at').replace('{date}', formatDate(sub.last_update))}
              >
                {formatUpdateDate(sub.last_update)}
              </span>

              <!-- Синий чип количества нод -->
              <span
                class="nodes-count-badge"
                on:click={() => toggleExpand(sub.id)}
                title={$t('subscr.nodes_count').replace('{count}', String(sub.proxy_count || 0))}
              >
                {sub.proxy_count || 0}
              </span>

              <!-- Кнопка Обновить подписку (круговая стрелочка) -->
              <button
                class="action-icon-btn"
                on:click={() => refreshSubscription(sub.id)}
                disabled={refreshLoading[sub.id]}
                title={$t('subscr.refresh')}
              >
                <svg
                  width="14"
                  height="14"
                  viewBox="0 0 24 24"
                  fill="none"
                  stroke="currentColor"
                  stroke-width="2.5"
                  class:spinning={refreshLoading[sub.id]}
                >
                  <path d="M21.5 2v6h-6M21.34 15.57a10 10 0 1 1-.57-8.38l5.67-5.67" />
                </svg>
              </button>

              <!-- Кнопка редактирования -->
              <button
                class="action-icon-btn"
                on:click={() => openEditModal(sub)}
                title={$t('app.edit')}
              >
                <svg
                  width="14"
                  height="14"
                  viewBox="0 0 24 24"
                  fill="none"
                  stroke="currentColor"
                  stroke-width="2.5"
                >
                  <circle cx="12" cy="12" r="3" /><path
                    d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 1 1-2.83 2.83l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 0 1-4 0v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 1 1-2.83-2.83l.06-.06a1.65 1.65 0 0 0 .33-1.82 1.65 1.65 0 0 0-1.51-1H3a2 2 0 0 1 0-4h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 1 1 2.83-2.83l.06.06a1.65 1.65 0 0 0 1.82.33H9a1.65 1.65 0 0 0 1-1.51V3a2 2 0 0 1 4 0v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 1 1 2.83 2.83l-.06.06a1.65 1.65 0 0 0-.33 1.82V9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 0 1 0 4h-.09a1.65 1.65 0 0 0-1.51 1z"
                  />
                </svg>
              </button>

              <!-- Кнопка три точки -->
              <div class="dropdown-container">
                <button
                  class="action-icon-btn dots-btn"
                  on:click={() => toggleDropdown(sub.id)}
                  aria-label="More actions"
                >
                  <svg
                    width="14"
                    height="14"
                    viewBox="0 0 24 24"
                    fill="none"
                    stroke="currentColor"
                    stroke-width="2.5"
                  >
                    <circle cx="12" cy="12" r="1.5" /><circle cx="12" cy="5" r="1.5" /><circle
                      cx="12"
                      cy="19"
                      r="1.5"
                    />
                  </svg>
                </button>
                {#if activeDropdownId === sub.id}
                  <div class="dropdown-menu">
                    {#if $devMode}
                      <button
                        on:click={() => {
                          openDiagnosticModal(sub);
                          activeDropdownId = null;
                        }}>🔍 {$t('subscr.diag_btn')}</button
                      >
                    {/if}
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

          <!-- Подстрока с оставшимся временем и трафиком (Метаданные) -->
          <div class="sub-meta-row">
            <!-- Срок действия -->
            <div class="sub-meta-left">
              {#if exp}
                <span
                  class="expire-text"
                  class:expired={exp.expired}
                  class:warning={exp.days !== null && exp.days <= 5}
                >
                  {exp.text}
                </span>
                <span class="meta-divider">|</span>
              {/if}

              <span class="sub-type-label">{sub.type === 'mihomo' ? 'Mihomo' : 'XRay'}</span>

              <span class="meta-divider">|</span>
              {#if sub.mihomo_integrated}
                <span class="mihomo-integrated-badge active" title="Интегрировано в Mihomo config.yaml">Mihomo ✓</span>
              {:else}
                <span class="mihomo-integrated-badge" title="Не интегрировано в Mihomo config.yaml">Mihomo —</span>
              {/if}

              {#if sub.hwid_locked}
                <span class="meta-divider">|</span>
                <span class="hwid-locked-badge">⚠ HWID Locked</span>
              {/if}
            </div>

            <!-- Объем трафика -->
            <div class="sub-meta-right">
              <span class="traffic-text">
                {formatTraffic((sub.upload || 0) + (sub.download || 0))} / {sub.total &&
                sub.total > 0
                  ? formatTraffic(sub.total)
                  : '∞'}
              </span>
            </div>
          </div>

          <!-- Блок кнопок поддержки и объявления под метаданными -->
          {#if sub.support_url || sub.announcement}
            <div class="sub-actions-row">
              {#if sub.support_url}
                <a
                  href={sub.support_url}
                  target="_blank"
                  rel="noopener noreferrer"
                  class="btn btn-support"
                >
                  <!-- Telegram Icon (Plane) -->
                  <svg
                    width="12"
                    height="12"
                    viewBox="0 0 24 24"
                    fill="none"
                    stroke="currentColor"
                    stroke-width="2"
                    class="support-icon"
                  >
                    <line x1="22" y1="2" x2="11" y2="13"></line>
                    <polygon points="22 2 15 22 11 13 2 9 22 2"></polygon>
                  </svg>
                  <span>{$currentLang === 'ru' ? 'Поддержка' : 'Support'}</span>
                </a>
              {/if}

              {#if sub.announcement}
                <div class="announcement-wrapper">
                  <button class="btn btn-announcement">
                    <!-- Bell/Info Icon -->
                    <svg
                      width="12"
                      height="12"
                      viewBox="0 0 24 24"
                      fill="none"
                      stroke="currentColor"
                      stroke-width="2"
                      class="announce-icon"
                    >
                      <path
                        d="M18 8A6 6 0 0 0 6 8c0 7-3 9-3 9h18s-3-2-3-9M13.73 21a2 2 0 0 1-3.46 0"
                      />
                    </svg>
                    <span>{$currentLang === 'ru' ? 'Объявление' : 'Announcement'}</span>
                  </button>

                  <!-- Всплывающий popover при ховере на .announcement-wrapper -->
                  <div class="announcement-popover">
                    {#each parseAnnouncementLines(sub.announcement) as line}
                      {#if line.isWarn}
                        <div class="inline-announcement-warn">
                          <span class="inline-warn-icon">!</span>
                          <span class="inline-warn-text">
                            {#each parseSimpleMarkdown(line.text) as token}
                              {#if token.type === 'text'}
                                {token.value}
                              {:else if token.type === 'bold'}
                                <strong>{token.value}</strong>
                              {:else if token.type === 'italic'}
                                <em>{token.value}</em>
                              {:else if token.type === 'link'}
                                <a href={token.url} target="_blank" rel="noopener noreferrer"
                                  >{token.text}</a
                                >
                              {/if}
                            {/each}
                          </span>
                        </div>
                      {:else}
                        <div class="announcement-line">
                          {#each parseSimpleMarkdown(line.text) as token}
                            {#if token.type === 'text'}
                              {token.value}
                            {:else if token.type === 'bold'}
                              <strong>{token.value}</strong>
                            {:else if token.type === 'italic'}
                              <em>{token.value}</em>
                            {:else if token.type === 'link'}
                              <a href={token.url} target="_blank" rel="noopener noreferrer"
                                >{token.text}</a
                              >
                            {/if}
                          {/each}
                        </div>
                      {/if}
                    {/each}
                  </div>
                </div>
              {/if}
            </div>
          {/if}

          {#if expandedSubs[sub.id]}
            <div class="nodes-preview-content">
              {#if subNodesLoading[sub.id]}
                <div class="loading-nodes">
                  <span class="spinner-xs"></span>
                  <span style="margin-left: 8px;">{$t('app.loading')}</span>
                </div>
              {:else}
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
                      {@const metaText =
                        node.use_case || node.speed
                          ? `${node.use_case || ''}${node.use_case && node.speed ? ' - ' : ''}${node.speed || ''}`
                          : `${node.protocol || ''}${node.protocol && node.transport ? ' · ' + node.transport : ''}${node.security && node.security !== 'none' ? ' · ' + node.security : ''}`}
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
                        <div
                          class="sub-node-avatar-container"
                          class:active={isNodeActive}
                          style={(!flagsSupported || !node.flag) && node.country
                            ? getCountryColorStyle(node.country)
                            : ''}
                        >
                          {#if flagsSupported && node.flag}
                            <span class="sub-node-flag">{node.flag}</span>
                          {:else if node.country}
                            <span class="sub-node-avatar-text">{node.country}</span>
                          {:else}
                            <span class="sub-node-flag-fallback">🌐</span>
                          {/if}
                        </div>

                        <!-- Text Info -->
                        <div class="sub-node-info">
                          <div class="sub-node-name-row">
                            <span class="sub-node-name">
                              {node.name || $t('country.' + node.country) || node.tag}
                              {#if node.is_new}
                                <span class="sub-node-name-new"> [NEW]</span>
                              {/if}
                            </span>
                          </div>
                          <div class="sub-node-meta-row">
                            {#if metaText}
                              <span class="sub-node-chip-blue">{metaText}</span>
                            {/if}
                          </div>
                        </div>

                        <!-- Status / Ping right -->
                        <div class="sub-node-status-container">
                          <!-- Золотой чип формата/протокола -->
                          <span class="sub-node-chip-gold"
                            >{sub.type === 'mihomo' ? 'YAML' : 'JSON'}</span
                          >

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
                            {:else if h}
                              <span class="sub-node-ping-val {latencyClass(h)}"
                                >{latencyLabel(h)}</span
                              >
                              {#if h.alive}
                                <div class="sub-node-status-icon success">
                                  <svg
                                    width="8"
                                    height="8"
                                    viewBox="0 0 24 24"
                                    fill="none"
                                    stroke="currentColor"
                                    stroke-width="4"
                                  >
                                    <polyline points="20 6 9 17 4 12" />
                                  </svg>
                                </div>
                              {:else}
                                <div class="sub-node-status-icon danger">
                                  <svg
                                    width="8"
                                    height="8"
                                    viewBox="0 0 24 24"
                                    fill="none"
                                    stroke="currentColor"
                                    stroke-width="4"
                                  >
                                    <line x1="18" y1="6" x2="6" y2="18" /><line
                                      x1="6"
                                      y1="6"
                                      x2="18"
                                      y2="18"
                                    />
                                  </svg>
                                </div>
                              {/if}
                            {:else}
                              <div class="sub-node-status-icon default-ok">
                                <svg
                                  width="8"
                                  height="8"
                                  viewBox="0 0 24 24"
                                  fill="none"
                                  stroke="currentColor"
                                  stroke-width="4"
                                >
                                  <polyline points="20 6 9 17 4 12" />
                                </svg>
                              </div>
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

        <button
          type="button"
          class="advanced-toggle-btn"
          on:click={() => (showAdvanced = !showAdvanced)}
        >
          <span class="arrow">{showAdvanced ? '▼' : '►'}</span>
          <span>{$t('subscr.advanced_params') || 'Дополнительные параметры'}</span>
        </button>

        {#if showAdvanced}
          <div class="advanced-fields-box">
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
              <label for="form-filter-transport" class="form-label"
                >{$t('subscr.filter_transport')}</label
              >
              <input
                id="form-filter-transport"
                type="text"
                class="input"
                bind:value={formFilterTransport}
                placeholder="ws, grpc, tcp..."
              />
            </div>
          </div>
        {/if}

        <div class="form-group-checkbox">
          <label class="toggle-switch">
            <input type="checkbox" id="enabled" bind:checked={formEnabled} />
            <span class="toggle-slider"></span>
          </label>
          <label for="enabled" class="checkbox-label">{$t('subscr.enabled')}</label>
        </div>

        <div class="form-group-checkbox">
          <label class="toggle-switch">
            <input
              type="checkbox"
              id="use-provider-interval"
              bind:checked={formUseProviderInterval}
            />
            <span class="toggle-slider"></span>
          </label>
          <label for="use-provider-interval" class="checkbox-label">
            {$t('subscr.use_provider_interval')}
            {#if editingSub && editingSub.profile_update_hours && editingSub.profile_update_hours > 0}
              <span style="color: var(--accent); font-size: 11px; margin-left: 4px;">
                ({$t('subscr.provider_dictates').replace(
                  '{hours}',
                  String(editingSub.profile_update_hours)
                )})
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
          on:click={() => (diagnosticTab = 'report')}
        >
          {$t('subscr.tab_report')}
        </button>
        <button
          class="diag-tab-btn"
          class:active={diagnosticTab === 'headers'}
          on:click={() => (diagnosticTab = 'headers')}
        >
          {$t('subscr.tab_headers')}
        </button>
        <button
          class="diag-tab-btn"
          class:active={diagnosticTab === 'raw'}
          on:click={() => (diagnosticTab = 'raw')}
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
        {:else if diagnosticTab === 'report'}
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
              <div class="text-center" style="padding: 1.5rem; color: var(--fg-secondary);">—</div>
            {/if}
          </div>
        {:else if diagnosticTab === 'raw'}
          <div class="tab-content height-100">
            {#if rawResponseData && rawResponseData.body}
              <pre class="raw-body-pre"><code>{rawResponseData.body}</code></pre>
            {:else}
              <div class="text-center" style="padding: 1.5rem; color: var(--fg-secondary);">—</div>
            {/if}
          </div>
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
    padding: 24px;
    display: flex;
    flex-direction: column;
    gap: 12px;
    position: relative;
  }

  /* Хедер карточки */
  .sub-header-row {
    display: flex;
    justify-content: space-between;
    align-items: center;
    gap: 16px;
  }

  .sub-header-left {
    display: flex;
    align-items: center;
    gap: 10px;
    flex: 1;
    min-width: 0;
  }

  /* Стрелочка */
  .collapse-toggle {
    background: transparent;
    border: none;
    padding: 4px;
    color: var(--fg-dim);
    cursor: pointer;
    display: grid;
    place-items: center;
    border-radius: 4px;
    transition:
      color var(--transition-fast),
      background var(--transition-fast);
  }
  .collapse-toggle:hover {
    color: var(--accent);
    background: rgba(255, 255, 255, 0.04);
  }
  .collapse-toggle svg {
    transition: transform var(--transition-fast);
  }
  .collapse-toggle.expanded svg {
    transform: rotate(90deg);
  }

  /* LED светодиод */
  .type-dot {
    width: 8px;
    height: 8px;
    border-radius: 50%;
    background: var(--accent);
    box-shadow: 0 0 8px var(--accent);
    flex-shrink: 0;
    transition: all var(--transition-fast);
  }
  .type-dot.mihomo {
    background: #8b5cf6;
    box-shadow: 0 0 8px #8b5cf6;
  }
  .type-dot.disabled {
    background: var(--fg-faint);
    box-shadow: none;
  }
  .type-dot.has-error {
    background: var(--danger);
    box-shadow: 0 0 8px var(--danger);
  }

  /* Имя */
  .sub-name {
    margin: 0;
    font-size: 15px;
    font-weight: 600;
    color: var(--fg-primary);
    cursor: pointer;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
    font-family:
      var(--font-family-sans), 'Apple Color Emoji', 'Segoe UI Emoji', 'Segoe UI Symbol',
      'Noto Color Emoji', 'Android Emoji', EmojiSymbols, sans-serif;
  }
  .sub-name:hover {
    color: var(--accent);
  }

  /* Быстрая кнопка карандаша */
  .edit-icon-btn {
    background: transparent;
    border: none;
    padding: 4px;
    color: var(--fg-dim);
    cursor: pointer;
    border-radius: 4px;
    display: grid;
    place-items: center;
    opacity: 0;
    transition:
      opacity var(--transition-fast),
      color var(--transition-fast),
      background var(--transition-fast);
  }
  .sub-header-left:hover .edit-icon-btn,
  .edit-icon-btn:focus {
    opacity: 1;
  }
  .edit-icon-btn:hover {
    color: var(--accent);
    background: rgba(255, 255, 255, 0.04);
  }

  .sub-header-right {
    display: flex;
    align-items: center;
    gap: 12px;
    flex-shrink: 0;
  }

  .sub-update-time {
    font-size: 12px;
    color: var(--fg-dim);
  }

  /* Синий чип количества нод */
  .nodes-count-badge {
    background: rgba(41, 194, 240, 0.1);
    border: 1px solid rgba(41, 194, 240, 0.25);
    color: var(--accent);
    padding: 2px 10px;
    border-radius: 12px;
    font-size: 11.5px;
    font-weight: 700;
    cursor: pointer;
    transition: all var(--transition-fast);
  }
  .nodes-count-badge:hover {
    background: rgba(41, 194, 240, 0.18);
    border-color: rgba(41, 194, 240, 0.45);
    box-shadow: 0 0 10px rgba(41, 194, 240, 0.2);
  }

  /* action кнопки-иконки */
  .action-icon-btn {
    background: transparent;
    border: none;
    padding: 6px;
    color: var(--fg-dim);
    cursor: pointer;
    border-radius: 6px;
    display: grid;
    place-items: center;
    transition: all var(--transition-fast);
  }
  .action-icon-btn:hover {
    color: var(--accent);
    background: var(--hover);
  }
  .action-icon-btn:disabled {
    opacity: 0.4;
    cursor: not-allowed;
  }
  .action-icon-btn svg.spinning {
    animation: rotate 1.5s linear infinite;
  }

  @keyframes rotate {
    from {
      transform: rotate(0deg);
    }
    to {
      transform: rotate(360deg);
    }
  }

  /* Метаданные (Строка под заголовком) */
  .sub-meta-row {
    display: flex;
    justify-content: space-between;
    align-items: center;
    font-size: 12px;
    color: var(--fg-secondary);
    padding-bottom: 2px;
  }

  .sub-meta-left {
    display: flex;
    align-items: center;
    gap: 6px;
  }

  .meta-divider {
    color: var(--fg-faint);
    user-select: none;
  }

  .expire-text.warning {
    color: var(--warning);
  }
  .expire-text.expired {
    color: var(--danger);
  }

  .hwid-locked-badge {
    color: var(--warning);
    font-weight: 600;
  }

  .sub-type-label {
    text-transform: uppercase;
    font-size: 10px;
    letter-spacing: 0.08em;
    font-weight: 700;
    color: var(--fg-dim);
  }

  .mihomo-integrated-badge {
    text-transform: uppercase;
    font-size: 10px;
    letter-spacing: 0.08em;
    font-weight: 700;
    color: var(--fg-faint);
  }

  .mihomo-integrated-badge.active {
    color: var(--success);
  }

  .sub-meta-right {
    font-family: var(--font-family-mono);
    color: var(--fg-secondary);
  }

  /* Кнопки поддержки и объявления */
  .sub-actions-row {
    display: flex;
    gap: 10px;
    margin-top: 4px;
    align-items: center;
  }

  .btn-support {
    background: rgba(139, 92, 246, 0.12);
    border: 1px solid rgba(139, 92, 246, 0.25);
    color: #a78bfa;
    padding: 6px 14px;
    border-radius: 20px;
    font-size: 12px;
    font-weight: 600;
    text-decoration: none;
    display: inline-flex;
    align-items: center;
    gap: 6px;
    height: 28px;
    transition: all var(--transition-fast);
  }
  .btn-support:hover {
    background: rgba(139, 92, 246, 0.22);
    border-color: rgba(139, 92, 246, 0.45);
    color: #c4b5fd;
    box-shadow: 0 0 10px rgba(139, 92, 246, 0.2);
  }

  .announcement-wrapper {
    position: relative;
    display: inline-block;
  }

  .btn-announcement {
    background: rgba(240, 180, 80, 0.1);
    border: 1px solid rgba(240, 180, 80, 0.25);
    color: #f3d9a6;
    padding: 6px 14px;
    border-radius: 20px;
    font-size: 12px;
    font-weight: 600;
    display: inline-flex;
    align-items: center;
    gap: 6px;
    height: 28px;
    cursor: pointer;
    transition: all var(--transition-fast);
  }
  .btn-announcement:hover {
    background: rgba(240, 180, 80, 0.2);
    border-color: rgba(240, 180, 80, 0.45);
    color: #fff;
    box-shadow: 0 0 10px rgba(240, 180, 80, 0.2);
  }

  /* Popover при ховере на объявление */
  .announcement-popover {
    position: absolute;
    top: calc(100% + 8px);
    left: 0;
    background: var(--bg-elevated);
    border: 1px solid var(--border-strong);
    border-radius: var(--radius-lg);
    box-shadow: 0 12px 32px rgba(0, 0, 0, 0.6);
    padding: 16px;
    width: 380px;
    z-index: 250;
    opacity: 0;
    pointer-events: none;
    transform: translateY(6px);
    transition:
      opacity var(--transition-fast) ease,
      transform var(--transition-fast) ease;
    display: flex;
    flex-direction: column;
    gap: 10px;
  }
  .announcement-popover::before {
    content: '';
    position: absolute;
    bottom: 100%;
    left: 24px;
    border-width: 6px;
    border-style: solid;
    border-color: transparent transparent var(--border-strong) transparent;
  }
  .announcement-wrapper:hover .announcement-popover {
    opacity: 1;
    pointer-events: auto;
    transform: translateY(0);
  }

  .announcement-line {
    font-size: 12px;
    line-height: 1.5;
    color: var(--fg-primary);
  }
  .announcement-line a {
    color: var(--accent);
    text-decoration: none;
  }
  .announcement-line a:hover {
    text-decoration: underline;
  }

  .inline-announcement-warn {
    display: flex;
    gap: 8px;
    padding: 8px 12px;
    background: rgba(239, 91, 107, 0.08);
    border-left: 3px solid var(--danger);
    border-radius: var(--radius-sm);
    color: #f4b6be;
    font-size: 12px;
    line-height: 1.45;
  }
  .inline-warn-icon {
    color: var(--danger);
    font-weight: 700;
    user-select: none;
  }
  .inline-announcement-warn a {
    color: var(--accent);
    text-decoration: underline;
  }

  /* Раздел предпросмотра нод (Компактный инлайн-вид) */
  .nodes-preview-content.inline-mode {
    border-top: 1px solid var(--border);
    margin-top: 8px;
    padding-top: 16px;
    background: transparent;
    display: flex;
    flex-direction: column;
    gap: 10px;
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
    scrollbar-width: thin;
    scrollbar-color: var(--border-strong) var(--bg-card);
  }
  .modal-card-body::-webkit-scrollbar {
    width: 6px;
  }
  .modal-card-body::-webkit-scrollbar-track {
    background: var(--bg-card);
  }
  .modal-card-body::-webkit-scrollbar-thumb {
    background: var(--border-strong);
    border-radius: 4px;
  }
  .modal-card-body::-webkit-scrollbar-thumb:hover {
    background: var(--accent);
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

  .diag-table th,
  .diag-table td {
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
  .loading-nodes,
  .empty-nodes {
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
    padding: 10px 16px;
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
    width: 32px;
    height: 32px;
    border-radius: 8px; /* скругленный квадрат как в INCY */
    background: rgba(0, 0, 0, 0.25);
    border: 1px solid rgba(255, 255, 255, 0.05);
    display: flex;
    align-items: center;
    justify-content: center;
    margin-right: 12px;
    flex-shrink: 0;
    font-size: 16px;
    transition: all var(--transition-fast);
    color: var(--fg-secondary);
  }

  .sub-node-avatar-container.active {
    background: var(--accent);
    border-color: var(--accent);
    color: white;
  }

  .sub-node-avatar-text {
    font-size: 11px;
    font-weight: 800;
    text-transform: uppercase;
    color: inherit;
    letter-spacing: 0.02em;
  }

  .sub-node-avatar-container.active .sub-node-avatar-text {
    color: white;
  }

  .sub-node-flag-fallback {
    font-size: 14px;
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
    font-size: 13px;
    font-weight: 600;
    color: var(--fg-primary);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }

  .sub-node-name-new {
    color: #f59e0b;
    font-weight: 700;
    font-size: 11px;
    letter-spacing: 0.02em;
  }

  .sub-node-chip-blue {
    background: rgba(41, 194, 240, 0.08);
    border: 1px solid rgba(41, 194, 240, 0.2);
    color: #7dd3fc;
    padding: 2px 10px;
    border-radius: 12px;
    font-size: 11px;
    font-weight: 500;
    display: inline-flex;
    align-items: center;
    margin-top: 3px;
    max-width: 100%;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .sub-node-row.active .sub-node-chip-blue {
    background: rgba(255, 255, 255, 0.12);
    border-color: rgba(255, 255, 255, 0.25);
    color: #fff;
  }

  .sub-node-chip-gold {
    background: rgba(245, 158, 11, 0.07);
    border: 1px solid rgba(245, 158, 11, 0.2);
    color: #f59e0b;
    padding: 2px 6px;
    border-radius: 4px;
    font-size: 9.5px;
    font-weight: 700;
    letter-spacing: 0.05em;
    display: inline-block;
    text-transform: uppercase;
    flex-shrink: 0;
  }
  .sub-node-row.active .sub-node-chip-gold {
    background: rgba(255, 255, 255, 0.15);
    border-color: rgba(255, 255, 255, 0.3);
    color: #fff;
  }

  .sub-node-meta-row {
    display: flex;
    align-items: center;
  }

  .sub-node-status-container {
    display: flex;
    align-items: center;
    gap: 12px;
    flex-shrink: 0;
    margin-left: 12px;
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

  .latency-good {
    color: #22c55e;
  }
  .latency-ok {
    color: #f59e0b;
  }
  .latency-bad {
    color: var(--danger);
  }
  .latency-unknown {
    color: var(--fg-faint);
  }

  @media (max-width: 768px) {
    .sub-header-row {
      flex-wrap: wrap;
      gap: 12px;
    }
    .sub-header-left {
      width: 100%;
    }
    .sub-header-right {
      width: 100%;
      justify-content: flex-end;
    }
    .sub-meta-row {
      flex-wrap: wrap;
      gap: 8px;
    }
    .announcement-popover {
      width: calc(100vw - 64px);
      max-width: 340px;
      left: -20px;
    }
    .announcement-popover::before {
      left: 50px;
    }
  }

  /* Кнопка спойлера продвинутых настроек */
  .advanced-toggle-btn {
    background: transparent;
    border: none;
    color: var(--accent);
    cursor: pointer;
    font-size: 13px;
    font-weight: 600;
    display: inline-flex;
    align-items: center;
    gap: 8px;
    padding: 6px 0;
    margin: 12px 0 6px 0;
    width: 100%;
    text-align: left;
    outline: none;
    transition: color var(--transition-fast);
  }
  .advanced-toggle-btn:hover {
    color: var(--accent-hover, #64b5f6);
  }
  .advanced-toggle-btn .arrow {
    display: inline-block;
    transition: transform var(--transition-fast);
    font-size: 11px;
    width: 12px;
  }
  .advanced-fields-box {
    display: flex;
    flex-direction: column;
    gap: 16px;
    padding: 16px;
    background: rgba(0, 0, 0, 0.15);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    margin-top: 4px;
    margin-bottom: 12px;
  }

  .textarea-link {
    min-height: 90px;
  }
  .preview-section {
    display: flex;
    flex-direction: column;
    gap: 12px;
  }
  .preview-title {
    margin: 0 0 4px 0;
    font-size: 13px;
    font-weight: 600;
    color: var(--fg-secondary);
  }
  .preview-table {
    display: flex;
    flex-direction: column;
    gap: 8px;
    background: rgba(0, 0, 0, 0.15);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    padding: 12px 16px;
  }
  .preview-row {
    display: flex;
    justify-content: space-between;
    align-items: center;
    font-size: 13px;
  }
  .preview-label {
    color: var(--fg-secondary);
  }
  .preview-value {
    color: var(--fg-primary);
  }
  .preview-value.code {
    font-family: var(--font-family-mono, monospace);
    font-size: 12px;
  }
</style>
