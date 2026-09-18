import { t, currentLang } from '../../i18n';
import { apiFetch, apiFetchJSON } from '../../lib/api';
import { parseValidationError } from '../../lib/errorParser';
import { capabilities, showToast, showConfirm } from '../../stores';
import { get } from 'svelte/store';

function isSafeKey(key: unknown): key is string {
  return (
    typeof key === 'string' &&
    key.length > 0 &&
    key !== '__proto__' &&
    key !== 'constructor' &&
    key !== 'prototype'
  );
}

export interface Subscription {
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
  exclude_filter?: string;
  exclude_type?: string;
  mihomo_groups?: string[];
  routing_mode?: 'manual' | 'auto';
  mihomo_provider?: {
    name: string;
    vehicle_type: string;
    updated_at: string;
    node_count: number;
  } | null;
}

export interface Node {
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

export interface NodeHealth {
  alive: boolean;
  delay?: number;
  http_code?: number;
  tested?: boolean;
}

export class ProvidersState {
  subscriptions = $state<Subscription[]>([]);
  expandedSubs = $state<Record<string, boolean>>({});
  subNodes = $state<Record<string, Node[]>>({});
  subNodesLoading = $state<Record<string, boolean>>({});
  subHealth = $state<Record<string, Record<string, NodeHealth>>>({});
  checkingNodes = $state<Record<string, Record<string, boolean>>>({});
  refreshLoading = $state<Record<string, boolean>>({});
  subNodesError = $state<Record<string, boolean>>({});
  activeDropdownId = $state<string | null>(null);
  dialerProxyTargets = $state<Record<string, any[]>>({});
  loading = $state(false);

  // Modal & form states
  showAddModal = $state(false);
  editingSub = $state<Subscription | null>(null);
  formName = $state('');
  formEnableXray = $state(false);
  formEnableMihomo = $state(false);
  formURL = $state('');
  formInterval = $state(24);
  formRoutingMode = $state<'manual' | 'auto'>('manual');
  formTagPrefix = $state('');
  formFilterName = $state('');
  formFilterType = $state('');
  formFilterTransport = $state('');
  formExcludeFilter = $state('');
  formExcludeType = $state('');
  formMihomoGroups = $state<string[]>([]);
  formEnabled = $state(true);
  formUseProviderInterval = $state(false);
  availableMihomoGroups = $state<string[]>([]);
  formSockoptMark = $state<number | null>(null);
  formSockoptFastOpen = $state(false);
  formSockoptMptcp = $state(false);

  stats = $derived.by(() => {
    let totalNodes = 0;
    let nextUpdate: Date | null = null;

    for (const sub of this.subscriptions) {
      if (!sub.enabled) continue;
      totalNodes += sub.proxy_count || 0;

      if (sub.last_update && sub.interval > 0) {
        const last = new Date(sub.last_update);
        if (!isNaN(last.getTime())) {
          const next = new Date(last.getTime() + sub.interval * 3600 * 1000);
          if (!nextUpdate || next < nextUpdate) {
            nextUpdate = next;
          }
        }
      }
    }

    let nextStr = '—';
    if (nextUpdate) {
      const now = new Date();
      const diffMs = (nextUpdate as Date).getTime() - now.getTime();
      if (diffMs <= 0) {
        nextStr = get(t)('subscr.stats.soon');
      } else {
        const hours = Math.floor(diffMs / (3600 * 1000));
        const mins = Math.floor((diffMs % (3600 * 1000)) / (60 * 1000));
        if (hours > 0) {
          nextStr = `${hours} ${get(t)('subscr.stats.hours')} ${mins} ${get(t)('subscr.stats.mins')}`;
        } else {
          nextStr = `${mins} ${get(t)('subscr.stats.mins')}`;
        }
      }
    }
    return {
      total: this.subscriptions.length,
      nodes: totalNodes,
      next: nextStr
    };
  });

  async loadAvailableMihomoGroups() {
    try {
      const res = await apiFetchJSON<{ groups: string[] }>('/api/mihomo/groups');
      this.availableMihomoGroups = res?.groups || [];
    } catch {
      this.availableMihomoGroups = [];
    }
  }

  async loadSubscriptions(signal?: AbortSignal, hasProxies = false) {
    const reqSignal = signal instanceof AbortSignal ? signal : undefined;
    if (this.subscriptions.length === 0 && !hasProxies) {
      this.loading = true;
    }
    try {
      this.loadAvailableMihomoGroups();
      this.subscriptions = await apiFetchJSON<Subscription[]>('/api/proxy-providers', {
        signal: reqSignal
      });
    } catch (e: any) {
      if (e?.name !== 'AbortError' && e?.status !== 401) {
        showToast('error', get(t)('subscr.load_error'));
      }
    } finally {
      this.loading = false;
    }
  }

  async refreshSubscription(id: string) {
    const sub = this.subscriptions.find((s) => s.id === id);
    if (!sub) return;

    this.refreshLoading[id] = true;
    try {
      const tasks: Promise<
        | { kernel: 'xray' | 'mihomo'; status: 'fulfilled'; value?: any }
        | { kernel: 'xray' | 'mihomo'; status: 'rejected'; reason: any }
      >[] = [];

      if (sub.enable_xray) {
        tasks.push(
          (async () => {
            const res = await apiFetch(`/api/subscriptions/refresh?id=${id}`, {
              method: 'POST'
            });
            if (res.ok) {
              return { kernel: 'xray' as const, status: 'fulfilled' as const };
            } else {
              const text = await res.text();
              const parsedErr = parseValidationError(text, get(currentLang) === 'ru' ? 'ru' : 'en');
              throw { kernel: 'xray', reason: parsedErr || get(t)('app.error') };
            }
          })()
        );
      }

      const providerName = sub.mihomo_provider?.name;
      if (sub.enable_mihomo && providerName) {
        tasks.push(
          (async () => {
            const res = await apiFetch(`/api/proxy-providers/${providerName}/refresh`, {
              method: 'PUT'
            });
            if (res.ok) {
              return { kernel: 'mihomo' as const, status: 'fulfilled' as const };
            } else {
              const text = await res.text();
              const parsedErr = parseValidationError(text, get(currentLang) === 'ru' ? 'ru' : 'en');
              throw { kernel: 'mihomo', reason: parsedErr || get(t)('app.error') };
            }
          })()
        );
      }

      if (tasks.length === 0) {
        if (sub.enable_mihomo && !sub.mihomo_provider?.name) {
          showToast(
            'error',
            get(t)('subscr.refresh.mihomo_failed').replace('{message}', get(t)('app.unavailable'))
          );
        }
        this.refreshLoading[id] = false;
        return;
      }

      const results = await Promise.allSettled(tasks);

      for (const res of results) {
        if (res.status === 'fulfilled') {
          const val = res.value;
          if (val.kernel === 'xray') {
            showToast('success', get(t)('subscr.refresh.xray_started'));
          } else {
            showToast('success', get(t)('subscr.refresh.mihomo_started'));
          }
        } else {
          const err = res.reason;
          if (err && err.kernel === 'xray') {
            showToast(
              'error',
              get(t)('subscr.refresh.xray_failed').replace('{message}', err.reason)
            );
          } else if (err && err.kernel === 'mihomo') {
            showToast(
              'error',
              get(t)('subscr.refresh.mihomo_failed').replace('{message}', err.reason)
            );
          } else {
            showToast('error', get(t)('app.error'));
          }
        }
      }

      await this.loadSubscriptions();
      if (this.expandedSubs[id]) {
        await this.loadNodesBySource(id);
      }
    } catch (e: any) {
      if (e?.status === 401) return;
      showToast('error', get(t)('app.error'));
    } finally {
      this.refreshLoading[id] = false;
    }
  }

  async refreshAll() {
    this.loading = true;
    try {
      const res = await apiFetch('/api/subscriptions/refresh-all', {
        method: 'POST'
      });
      if (res.ok) {
        showToast('success', get(t)('app.success'));
        await this.loadSubscriptions();
        for (const id of Object.keys(this.expandedSubs)) {
          if (this.expandedSubs[id]) {
            await this.loadNodesBySource(id);
          }
        }
      } else {
        showToast('error', get(t)('app.error'));
      }
    } catch (e: any) {
      if (e?.status === 401) return;
      showToast('error', get(t)('app.error'));
    } finally {
      this.loading = false;
    }
  }

  async saveSubscription() {
    if (!this.formURL.trim()) {
      showToast('error', get(t)('subscr.fill_url'));
      return;
    }

    const payload = {
      id: this.editingSub ? this.editingSub.id : '',
      name: this.formName,
      url: this.formURL,
      enabled: this.formEnabled,
      interval: this.formInterval,
      use_provider_interval: this.formUseProviderInterval,
      enable_xray: this.formEnableXray,
      enable_mihomo: this.formEnableMihomo,
      tag_prefix: this.formTagPrefix,
      filter_name: this.formFilterName,
      filter_type: this.formFilterType,
      filter_transport: this.formFilterTransport,
      exclude_filter: this.formExcludeFilter,
      exclude_type: this.formExcludeType,
      mihomo_groups: this.formMihomoGroups,
      routing_mode: this.formRoutingMode,
      sockopt_mark:
        this.formSockoptMark !== null && this.formSockoptMark !== undefined
          ? Number(this.formSockoptMark)
          : 0,
      sockopt_fast_open: this.formSockoptFastOpen,
      sockopt_mptcp: this.formSockoptMptcp
    };

    try {
      const url = this.editingSub
        ? `/api/subscriptions/update?id=${encodeURIComponent(this.editingSub.id)}`
        : '/api/subscriptions/add';
      const res = await apiFetch(url, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json'
        },
        body: JSON.stringify(payload)
      });

      if (res.ok) {
        showToast('success', get(t)('app.success'));
        this.showAddModal = false;
        await this.loadSubscriptions();
      } else {
        const text = await res.text();
        const parsedErr = parseValidationError(text, get(currentLang) === 'ru' ? 'ru' : 'en');
        showToast('error', parsedErr || get(t)('app.error'));
      }
    } catch (e: any) {
      if (e?.status === 401) return;
      showToast('error', get(t)('app.error'));
    }
  }

  async deleteSubscription(id: string) {
    const sub = this.subscriptions.find((s) => s.id === id);
    if (!sub) return;
    const subName = sub.profile_title || sub.name || id;

    if (
      !(await showConfirm({
        title: get(t)('subscr.delete_title'),
        objectName: subName,
        consequence: get(t)('subscr.delete_consequence'),
        variant: 'danger',
        confirmLabel: get(t)('app.delete')
      }))
    )
      return;

    try {
      const res = await apiFetch(`/api/subscriptions/delete?id=${id}`, {
        method: 'POST'
      });
      if (res.ok) {
        showToast('success', get(t)('app.success'));
        await this.loadSubscriptions();
      } else {
        showToast('error', get(t)('app.error'));
      }
    } catch (e: any) {
      if (e?.status === 401) return;
      showToast('error', get(t)('app.error'));
    }
  }

  openAddModal() {
    this.editingSub = null;
    this.formName = '';
    this.formURL = '';
    this.formInterval = 24;
    this.formEnabled = true;
    this.formUseProviderInterval = false;
    this.formEnableXray = true;
    this.formEnableMihomo = false;
    this.formRoutingMode = 'manual';
    this.formTagPrefix = '';
    this.formFilterName = '';
    this.formFilterType = '';
    this.formFilterTransport = '';
    this.formExcludeFilter = '';
    this.formExcludeType = '';
    this.formMihomoGroups = [];
    this.formSockoptMark = null;
    this.formSockoptFastOpen = false;
    this.formSockoptMptcp = false;
    this.showAddModal = true;
    this.loadAvailableMihomoGroups();
  }

  openEditModal(sub: Subscription) {
    this.editingSub = sub;
    this.formName = sub.name;
    this.formURL = sub.url;
    this.formInterval = sub.interval;
    this.formEnabled = sub.enabled;
    this.formUseProviderInterval = sub.use_provider_interval ?? false;
    this.formEnableXray = sub.enable_xray ?? false;
    this.formEnableMihomo = sub.enable_mihomo ?? false;
    this.formRoutingMode = sub.routing_mode ?? 'manual';
    this.formTagPrefix = sub.tag_prefix ?? '';
    this.formFilterName = sub.filter_name ?? '';
    this.formFilterType = sub.filter_type ?? '';
    this.formFilterTransport = sub.filter_transport ?? '';
    this.formExcludeFilter = sub.exclude_filter ?? '';
    this.formExcludeType = sub.exclude_type ?? '';
    this.formMihomoGroups = sub.mihomo_groups ?? [];
    this.formSockoptMark =
      (sub as any).sockopt_mark !== undefined && (sub as any).sockopt_mark !== 0
        ? (sub as any).sockopt_mark
        : null;
    this.formSockoptFastOpen = !!(sub as any).sockopt_fast_open;
    this.formSockoptMptcp = !!(sub as any).sockopt_mptcp;
    this.showAddModal = true;
    this.loadAvailableMihomoGroups();
  }

  closeModal() {
    this.showAddModal = false;
    this.editingSub = null;
  }

  toggleDropdown(id: string) {
    if (this.activeDropdownId === id) {
      this.activeDropdownId = null;
    } else {
      this.activeDropdownId = id;
    }
  }

  handleClickOutside = (e: MouseEvent) => {
    if (this.activeDropdownId) {
      const target = e.target as HTMLElement;
      if (!target.closest('.dropdown-container')) {
        this.activeDropdownId = null;
      }
    }
  };

  getNodeSource(sub: Subscription): 'mihomo' | 'xray' {
    if (sub.enable_mihomo && !sub.enable_xray) {
      return 'mihomo';
    }
    if (sub.enable_xray && !sub.enable_mihomo) {
      return 'xray';
    }
    if (sub.enable_mihomo && sub.enable_xray) {
      const active = get(capabilities)?.active_kernel;
      return active === 'mihomo' ? 'mihomo' : 'xray';
    }
    return 'xray';
  }

  async loadNodes(subId: string) {
    if (
      !isSafeKey(subId) ||
      subId === '__proto__' ||
      subId === 'constructor' ||
      subId === 'prototype'
    ) {
      return;
    }
    this.subNodesLoading[subId] = true;
    try {
      const res = await apiFetch(`/api/subscriptions/nodes?id=${encodeURIComponent(subId)}`);
      if (res.ok) {
        this.subNodes[subId] = await res.json();
      }
    } catch (e: any) {
      if (e?.status === 401) return;
    } finally {
      this.subNodesLoading[subId] = false;
    }
  }

  async loadMihomoNodes(subId: string) {
    if (
      !isSafeKey(subId) ||
      subId === '__proto__' ||
      subId === 'constructor' ||
      subId === 'prototype'
    ) {
      return;
    }
    const sub = this.subscriptions.find((s) => s.id === subId);
    if (!sub || !sub.mihomo_provider?.name) {
      this.subNodesError[subId] = true;
      return;
    }

    this.subNodesLoading[subId] = true;
    this.subNodesError[subId] = false;
    try {
      const res = await apiFetch(
        `/api/proxy-providers/${encodeURIComponent(sub.mihomo_provider.name)}/nodes`
      );
      if (res.ok) {
        const data: {
          tag: string;
          name: string;
          alive: boolean;
          tested: boolean;
          delay_ms: number;
        }[] = await res.json();
        this.subNodes[subId] = (data as any[]).map((n) => ({
          ...n,
          tag: n.tag,
          name: n.name,
          active: false,
          is_new: false
        }));
        if (!this.subHealth[subId]) this.subHealth[subId] = {};
        data.forEach((n) => {
          if (!n.tag || n.tag === '__proto__' || n.tag === 'constructor' || n.tag === 'prototype') {
            return;
          }
          this.subHealth[subId][n.tag] = {
            alive: n.alive,
            delay: n.tested ? n.delay_ms : undefined,
            tested: n.tested
          };
        });
      } else {
        this.subNodesError[subId] = true;
      }
    } catch (e: any) {
      if (e?.status === 401) return;
      this.subNodesError[subId] = true;
    } finally {
      this.subNodesLoading[subId] = false;
    }
  }

  async loadNodesBySource(subId: string) {
    const sub = this.subscriptions.find((s) => s.id === subId);
    if (!sub) return;
    if (this.getNodeSource(sub) === 'mihomo') {
      await this.loadMihomoNodes(subId);
    } else {
      await this.loadNodes(subId);
    }
  }

  async loadDialerProxyTargets(subId: string) {
    if (
      !isSafeKey(subId) ||
      subId === '__proto__' ||
      subId === 'constructor' ||
      subId === 'prototype'
    ) {
      return;
    }
    const sub = this.subscriptions.find((s) => s.id === subId);
    if (!sub || !sub.enable_xray) return;
    try {
      const res = await apiFetch(
        `/api/subscriptions/dialer-proxy-targets?id=${encodeURIComponent(subId)}&node_tag=_`
      );
      if (res.ok) {
        const json = await res.json();
        this.dialerProxyTargets[subId] = json?.data || json || [];
      }
    } catch (e: any) {
      if (e?.status === 401) return;
    }
  }

  async handleSetDialerProxy(subId: string, nodeTag: string, targetTag: string) {
    try {
      const res = await apiFetch(`/api/subscriptions/node-dialer-proxy?id=${subId}`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          node_tag: nodeTag,
          target_tag: targetTag
        })
      });
      if (res.status === 401) return;
      const data = await res.json().catch(() => null);
      if (res.status === 409) {
        if (data?.error === 'cannot cascade node that is already used as a proxy target') {
          showToast('error', get(t)('subscr.dialer_proxy.already_target'));
        } else {
          showToast('error', get(t)('subscr.dialer_proxy.chain_limit'));
        }
        return;
      }
      if (!res.ok) {
        showToast('error', data?.error || get(t)('subscr.dialer_proxy.error'));
        return;
      }
      showToast('success', get(t)('subscr.dialer_proxy.saved'));
      await this.loadNodesBySource(subId);
      await this.loadDialerProxyTargets(subId);
    } catch (e: any) {
      if (e?.status === 401) return;
      showToast('error', e.message || get(t)('subscr.dialer_proxy.error'));
    }
  }

  async toggleExpand(subId: string) {
    if (
      !isSafeKey(subId) ||
      subId === '__proto__' ||
      subId === 'constructor' ||
      subId === 'prototype'
    ) {
      return;
    }
    this.expandedSubs[subId] = !this.expandedSubs[subId];
    if (this.expandedSubs[subId]) {
      await this.loadNodesBySource(subId);
      await this.loadDialerProxyTargets(subId);
    }
  }

  async checkMihomoNodeHealth(subId: string, providerName: string, nodeTag: string) {
    if (
      !isSafeKey(subId) ||
      subId === '__proto__' ||
      subId === 'constructor' ||
      subId === 'prototype'
    ) {
      return;
    }
    if (
      !isSafeKey(nodeTag) ||
      nodeTag === '__proto__' ||
      nodeTag === 'constructor' ||
      nodeTag === 'prototype'
    ) {
      return;
    }
    if (!this.checkingNodes[subId]) this.checkingNodes[subId] = {};
    this.checkingNodes[subId][nodeTag] = true;

    const setNodeHealthFailed = () => {
      if (!this.subHealth[subId]) this.subHealth[subId] = {};
      this.subHealth[subId][nodeTag] = {
        alive: false,
        delay: 0,
        tested: true
      };
    };

    try {
      const targetURL = `/api/mihomo/proxy/proxies/${encodeURIComponent(nodeTag)}/delay?url=http://www.gstatic.com/generate_204&timeout=5000`;
      const res = await apiFetch(targetURL, {
        method: 'GET'
      });
      if (res.ok) {
        const health = await res.json();
        if (!this.subHealth[subId]) this.subHealth[subId] = {};
        this.subHealth[subId][nodeTag] = {
          alive: health.delay > 0,
          delay: health.delay,
          tested: true
        };
      } else {
        if (res.status === 404) {
          const hcRes = await apiFetch(
            `/api/mihomo/proxy/providers/proxies/${encodeURIComponent(providerName)}/healthcheck`,
            {
              method: 'GET'
            }
          );
          if (hcRes.ok || hcRes.status === 204) {
            await new Promise((resolve) => setTimeout(resolve, 800));
            const nodesRes = await apiFetch(
              `/api/proxy-providers/${encodeURIComponent(providerName)}/nodes`
            );
            if (nodesRes.ok) {
              const nodesData = await nodesRes.json();
              if (Array.isArray(nodesData)) {
                if (!this.subHealth[subId]) this.subHealth[subId] = {};
                nodesData.forEach((n: any) => {
                  if (
                    !n.tag ||
                    n.tag === '__proto__' ||
                    n.tag === 'constructor' ||
                    n.tag === 'prototype'
                  ) {
                    return;
                  }
                  this.subHealth[subId][n.tag] = {
                    alive: n.alive,
                    delay: n.tested ? n.delay_ms : undefined,
                    tested: n.tested
                  };
                });
              } else {
                setNodeHealthFailed();
              }
            } else {
              setNodeHealthFailed();
            }
          } else {
            setNodeHealthFailed();
          }
        } else {
          setNodeHealthFailed();
        }
      }
    } catch (e: any) {
      if (e?.status === 401) return;
      setNodeHealthFailed();
    } finally {
      this.checkingNodes[subId][nodeTag] = false;
    }
  }

  async checkNodeHealth(subId: string, nodeTag: string) {
    if (
      !isSafeKey(subId) ||
      subId === '__proto__' ||
      subId === 'constructor' ||
      subId === 'prototype'
    ) {
      return;
    }
    if (
      !isSafeKey(nodeTag) ||
      nodeTag === '__proto__' ||
      nodeTag === 'constructor' ||
      nodeTag === 'prototype'
    ) {
      return;
    }
    const sub = this.subscriptions.find((s) => s.id === subId);
    if (!sub) return;

    const source = this.getNodeSource(sub);
    if (source === 'mihomo' && sub.mihomo_provider?.name) {
      await this.checkMihomoNodeHealth(subId, sub.mihomo_provider.name, nodeTag);
      return;
    }

    if (!this.checkingNodes[subId]) this.checkingNodes[subId] = {};
    this.checkingNodes[subId][nodeTag] = true;
    try {
      const res = await apiFetch(
        `/api/subscriptions/health?id=${encodeURIComponent(subId)}&tag=${encodeURIComponent(nodeTag)}`
      );
      if (res.ok) {
        const health = await res.json();
        if (!this.subHealth[subId]) this.subHealth[subId] = {};
        this.subHealth[subId][nodeTag] = health;
      }
    } catch (e: any) {
      if (e?.status === 401) return;
    } finally {
      this.checkingNodes[subId][nodeTag] = false;
    }
  }

  async setActiveNode(subId: string, nodeTag: string) {
    try {
      const res = await apiFetch(
        `/api/subscriptions/active?id=${subId}&tag=${encodeURIComponent(nodeTag)}`,
        {
          method: 'POST'
        }
      );
      if (res.status === 401) return;
      if (res.ok) {
        showToast('success', get(t)('app.success'));
        await this.loadNodesBySource(subId);
      } else {
        const text = await res.text().catch(() => '');
        showToast('error', text || get(t)('app.error'));
      }
    } catch (e: any) {
      if (e?.status === 401) return;
      showToast('error', get(t)('app.error'));
    }
  }

  checkAutoExpand() {
    const hash = window.location.hash;
    const regex = /#\/proxies\?expand=([a-zA-Z0-9_.-]+)/;
    const match = hash.match(regex);
    if (match && match[1]) {
      const rawId = match[1];
      if (
        !isSafeKey(rawId) ||
        rawId === '__proto__' ||
        rawId === 'constructor' ||
        rawId === 'prototype'
      ) {
        return;
      }
      const matched = this.subscriptions.find((s) => s.id === rawId);
      if (!matched) return;
      const subId = matched.id;
      if (
        !isSafeKey(subId) ||
        subId === '__proto__' ||
        subId === 'constructor' ||
        subId === 'prototype'
      ) {
        return;
      }
      this.expandedSubs[subId] = true;
      this.loadNodesBySource(subId).then(() => {
        setTimeout(() => {
          const el = document.getElementById(`sub-card-${subId}`);
          if (el) {
            el.scrollIntoView({ behavior: 'smooth', block: 'start' });
          }
        }, 100);
      });
    }
  }
}
