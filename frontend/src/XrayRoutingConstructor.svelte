<script lang="ts">
  import { onMount, onDestroy, tick } from 'svelte';
  import Modal from './components/Modal.svelte';
  import Select from './components/Select.svelte';
  import Button from './components/Button.svelte';
  import DraftRestoreBanner from './components/DraftRestoreBanner.svelte';
  import { registerDirtySource, getDraft, clearDraft, type DraftRecord } from './lib/dirtyRegistry';
  import { activateRestartGrace } from './lib/serviceGrace';
  import { currentLang, t, tp } from './i18n';
  import { capabilities, showToast, fetchCapabilities, showConfirm } from './stores';
  import { mergeXrayFile, syncDnsPipeline, substituteProxyTag } from './lib/xrayMerge';
  import { parseValidationError } from './lib/errorParser';
  import { findPortCollisions, parseMihomoPorts, type PortAllocation } from './lib/portChecker';
  import { apiFetch, apiFetchJSON } from './lib/api';
  import PreflightWarnings, {
    type PreflightWarning
  } from './components/editor/PreflightWarnings.svelte';
  import ConstructorPreview, { type PreviewTab } from './components/ConstructorPreview.svelte';
  import XraySectionRouting from './components/xray/XraySectionRouting.svelte';
  import XraySectionInbounds from './components/xray/XraySectionInbounds.svelte';
  import XraySectionDns from './components/xray/XraySectionDns.svelte';
  import XraySectionOutbounds from './components/xray/XraySectionOutbounds.svelte';
  import XraySectionLog from './components/xray/XraySectionLog.svelte';
  import XraySectionPolicy from './components/xray/XraySectionPolicy.svelte';
  import {
    type XrayRoutingRule,
    type DNSServer,
    type XrayInbound,
    type OutboundDetail,
    type XraySectionName
  } from './components/xray/XrayContext.svelte';
  import {
    XRAY_DEFAULT_PRESETS,
    type XrayRoutingPreset
  } from './lib/constructors/presets/xrayPresets';

  let {
    onSwitchTab = () => {},
    selectedFile = '',
    onInsertIntoEditor = () => {},
    embedded = false
  } = $props<{
    onSwitchTab?: (tab: string) => void;
    selectedFile?: string;
    onInsertIntoEditor?: (content: string) => void;
    embedded?: boolean;
  }>();

  // Runes State (Svelte 5)
  let activeSection = $state<XraySectionName>('routing');
  let logConfig = $state({ loglevel: 'warning', dnsLog: false });
  let dnsConfig = $state<{
    tag: string;
    servers: (string | DNSServer)[];
    queryStrategy: string;
    hosts: Record<string, string>;
  }>({
    tag: 'dns-in',
    servers: [],
    queryStrategy: 'UseIP',
    hosts: {}
  });
  let routingConfig = $state<{ domainStrategy: string }>({
    domainStrategy: 'IPIfNonMatch'
  });
  let inbounds = $state<XrayInbound[]>([]);

  // Outbound Management & Reactivity
  let customOutbounds = $state<any[]>([]);
  let subscriptionOutbounds = $state<any[]>([]);
  let outboundTagsLoading = $state(false);

  let outboundTags = $derived([
    'direct',
    'block',
    'dns-out',
    ...customOutbounds.map((o) => o.tag).filter(Boolean),
    ...subscriptionOutbounds.map((o) => o.tag).filter(Boolean)
  ]);

  let outboundDetails = $derived.by<OutboundDetail[]>(() => {
    const list: OutboundDetail[] = [
      { tag: 'direct', protocol: 'freedom' },
      { tag: 'block', protocol: 'blackhole' },
      { tag: 'dns-out', protocol: 'dns' }
    ];
    const seen = new Set<string>(['direct', 'block', 'dns-out']);

    for (const o of customOutbounds) {
      if (o.tag && !seen.has(o.tag)) {
        seen.add(o.tag);
        let server = '';
        if (o.settings?.vnext?.[0]?.address) {
          server = o.settings.vnext[0].address;
        } else if (o.settings?.servers?.[0]?.address) {
          server = o.settings.servers[0].address;
        } else if (o.settings?.peers?.[0]?.endpoint) {
          server = o.settings.peers[0].endpoint;
        }
        list.push({
          tag: o.tag,
          protocol: o.protocol || 'unknown',
          server: server || undefined
        });
      }
    }

    for (const o of subscriptionOutbounds) {
      if (o.tag && !seen.has(o.tag)) {
        seen.add(o.tag);
        let server = '';
        if (o.settings?.vnext?.[0]?.address) {
          server = o.settings.vnext[0].address;
        } else if (o.settings?.servers?.[0]?.address) {
          server = o.settings.servers[0].address;
        } else if (o.settings?.peers?.[0]?.endpoint) {
          server = o.settings.peers[0].endpoint;
        }
        list.push({
          tag: o.tag,
          protocol: o.protocol || 'unknown',
          server: server || undefined
        });
      }
    }
    return list;
  });

  let routingRules = $state<XrayRoutingRule[]>([]);
  let balancers = $state<any[]>([]);
  let proxyTag = $state<string>('');
  let dnsOverVless = $state<boolean>(false);

  let policyConfig = $state<{ levels: Record<string, any>; system: Record<string, any> }>({
    levels: {
      '0': {
        handshake: 4,
        connIdle: 300,
        uplinkOnly: 2,
        downlinkOnly: 5
      }
    },
    system: {}
  });

  let showPreviewPane = $state(true);
  let activePreviewTab = $state<string>('05_routing.json');
  let isDirty = $state(false);
  let applyLoading = $state(false);
  let validationError = $state<string | null>(null);
  let dnsRedirectLoading = $state(false);

  let showApplyConfirm = $state(false);
  let filesToModify = $state<Array<{ name: string; changesCount: number }>>([]);
  let saveWarnings = $state<PreflightWarning[]>([]);

  // Test Route & Logger States
  let testRouteForm = $state({
    domain: '',
    ip: '',
    port: '',
    protocol: '',
    inboundTag: ''
  });
  let testRouteRunning = $state(false);
  let testRouteResult = $state<any>(null);
  let testRouteError = $state<string>('');
  let restartingLogger = $state(false);

  // Scenario Bar
  let schema = $state<any>(null);
  let schemaLoading = $state(true);
  let schemaError = $state<string | null>(null);
  let lastAppliedPreset = $state<string | null>(null);
  let isPresetModified = $state(false);

  // Raw file contents loaded from backend
  let xrayFiles = $state<Record<string, any>>({});
  let rawFilesOnLoad = $state<Record<string, string>>({});
  let undoHistory = $state<Array<Record<string, any>>>([]);

  // Draft handling
  let detectedDraft = $state<DraftRecord | null>(null);

  const previewTabs: PreviewTab[] = [
    { id: '05_routing.json', title: '05_routing.json' },
    { id: '04_outbounds.json', title: '04_outbounds.json' },
    { id: '02_dns.json', title: '02_dns.json' },
    { id: '01_log.json', title: '01_log.json' },
    { id: '03_inbounds.json', title: '03_inbounds.json' },
    { id: '06_policy.json', title: '06_policy.json' },
    { id: 'all', title: $t('xray.all_files') }
  ];

  let activePreviewText = $derived.by(() => {
    const cfgs = generateFileConfigs();
    if (activePreviewTab === 'all') {
      const combined: Record<string, any> = {};
      for (const [_, content] of Object.entries(cfgs)) {
        Object.assign(combined, content);
      }
      return JSON.stringify(combined, null, 2);
    }
    return JSON.stringify(cfgs[activePreviewTab] || {}, null, 2);
  });

  function generateFileConfigs(): Record<string, any> {
    const rulesToExport = routingRules.map((r) => {
      const copy: any = { type: 'field' };
      if (r.outboundTag) copy.outboundTag = r.outboundTag;
      if (r.domain && r.domain.length > 0) copy.domain = r.domain;
      if (r.ip && r.ip.length > 0) copy.ip = r.ip;
      if (r.port) copy.port = r.port;
      if (r.network) copy.network = r.network;
      if (r.protocol && r.protocol.length > 0) copy.protocol = r.protocol;
      if (r.inboundTag && r.inboundTag.length > 0) copy.inboundTag = r.inboundTag;
      return copy;
    });

    const routingObj: any = {
      routing: {
        domainStrategy: routingConfig.domainStrategy,
        rules: rulesToExport
      }
    };
    if (balancers.length > 0) {
      routingObj.routing.balancers = balancers;
    }

    const inboundsObj = {
      inbounds: inbounds.map((ib) => ({
        tag: ib.tag,
        port: ib.port,
        protocol: ib.protocol,
        listen: ib.listen || undefined,
        settings: ib.settings || {},
        sniffing: ib.sniffing || undefined,
        streamSettings: ib.streamSettings || undefined
      }))
    };

    const outboundsObj = {
      outbounds: customOutbounds
    };

    const dnsObj = {
      dns: {
        tag: dnsConfig.tag,
        queryStrategy: dnsConfig.queryStrategy,
        servers: dnsConfig.servers,
        hosts: Object.keys(dnsConfig.hosts).length > 0 ? dnsConfig.hosts : undefined
      }
    };

    const logObj = {
      log: {
        loglevel: logConfig.loglevel,
        dnsLog: logConfig.dnsLog
      }
    };

    const policyObj = {
      policy: {
        levels: policyConfig.levels,
        system: policyConfig.system
      }
    };

    return {
      '01_log.json': logObj,
      '02_dns.json': dnsObj,
      '03_inbounds.json': inboundsObj,
      '04_outbounds.json': outboundsObj,
      '05_routing.json': routingObj,
      '06_policy.json': policyObj
    };
  }

  async function loadXrayConfig() {
    try {
      const res = await apiFetch('/api/xray/config');
      if (res.status === 401) return;
      const data = await res.json();
      if (data && typeof data === 'object') {
        xrayFiles = data.files || {};
        parseXrayFiles(xrayFiles);
      }
    } catch {
      // Ignored
    }
  }

  function parseXrayFiles(files: Record<string, any>) {
    if (files['01_log.json']?.log) {
      const l = files['01_log.json'].log;
      logConfig.loglevel = l.loglevel || 'warning';
      logConfig.dnsLog = !!l.dnsLog;
    }

    if (files['02_dns.json']?.dns) {
      const d = files['02_dns.json'].dns;
      dnsConfig.tag = d.tag || 'dns-in';
      dnsConfig.servers = Array.isArray(d.servers) ? d.servers : [];
      dnsConfig.queryStrategy = d.queryStrategy || 'UseIP';
      dnsConfig.hosts = d.hosts || {};
    }

    if (files['03_inbounds.json']?.inbounds) {
      inbounds = files['03_inbounds.json'].inbounds;
    }

    if (files['04_outbounds.json']?.outbounds) {
      customOutbounds = files['04_outbounds.json'].outbounds;
    }

    if (files['05_routing.json']?.routing) {
      const r = files['05_routing.json'].routing;
      routingConfig.domainStrategy = r.domainStrategy || 'IPIfNonMatch';
      if (Array.isArray(r.rules)) {
        routingRules = r.rules.map((rule: any) => ({
          ...rule,
          id: typeof crypto !== 'undefined' ? crypto.randomUUID() : 'r-' + Math.random(),
          enabled: rule.enabled !== false
        }));
      }
      if (Array.isArray(r.balancers)) {
        balancers = r.balancers;
      }
    }

    if (files['06_policy.json']?.policy) {
      const p = files['06_policy.json'].policy;
      policyConfig.levels = p.levels || {
        '0': { handshake: 4, connIdle: 300, uplinkOnly: 2, downlinkOnly: 5 }
      };
      policyConfig.system = p.system || {};
    }
  }

  async function loadXrayOutboundTags() {
    outboundTagsLoading = true;
    try {
      const res = await apiFetch('/api/xray/outbounds');
      if (res.status === 401) return;
      const data = await res.json();
      if (data && Array.isArray(data.outbounds)) {
        subscriptionOutbounds = data.outbounds;
      }
    } catch {
      // Ignore
    } finally {
      outboundTagsLoading = false;
    }
  }

  async function loadSchema() {
    schemaLoading = true;
    schemaError = null;
    try {
      const res = await apiFetch('/api/constructor/schema');
      if (res.status === 401) return;
      const data = await res.json();
      schema = data?.data || data;
    } catch (e: any) {
      schemaError = e?.message || 'Failed to load schema';
    } finally {
      schemaLoading = false;
    }
  }

  function applyPreset(presetId: string) {
    const preset = XRAY_DEFAULT_PRESETS.find((p: XrayRoutingPreset) => p.id === presetId);
    if (!preset) return;

    routingRules = preset.rules.map((r) => ({
      ...r,
      id: typeof crypto !== 'undefined' ? crypto.randomUUID() : 'r-' + Math.random(),
      enabled: true
    }));

    if (preset.dnsServers && preset.dnsServers.length > 0) {
      dnsConfig.servers = [...preset.dnsServers] as (string | DNSServer)[];
    }
    dnsOverVless = preset.dnsOverVless;
    lastAppliedPreset = presetId;
    isPresetModified = false;
    isDirty = true;
  }

  async function runTestRoute() {
    if (!testRouteForm.domain.trim() && !testRouteForm.ip.trim()) return;
    testRouteRunning = true;
    testRouteError = '';
    testRouteResult = null;
    try {
      const res = await apiFetchJSON<any>('/api/xray/test-route', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          domain: testRouteForm.domain.trim() || undefined,
          ip: testRouteForm.ip.trim() || undefined,
          port: Number(testRouteForm.port) || undefined,
          protocol: testRouteForm.protocol || undefined,
          inbound_tag: testRouteForm.inboundTag.trim() || undefined
        })
      });
      testRouteResult = res?.data || res;
    } catch (e: any) {
      testRouteError = e?.message || $t('xray.test_route.error');
    } finally {
      testRouteRunning = false;
    }
  }

  async function restartLogger() {
    restartingLogger = true;
    try {
      await apiFetchJSON('/api/xray/restart-logger', { method: 'POST' });
      showToast('success', $t('xray.restart_logger.success'));
    } catch (e: any) {
      showToast('error', e?.message || $t('xray.restart_logger.error'));
    } finally {
      restartingLogger = false;
    }
  }

  async function enableDNSRedirect() {
    dnsRedirectLoading = true;
    try {
      await apiFetchJSON('/api/xkeen/dns-redirect/enable', { method: 'POST' });
      showToast('success', $t('editor.dns_intercept_enabled'));
      await fetchCapabilities();
    } catch (e: any) {
      showToast('error', e?.message || $t('editor.dns_intercept_error'));
    } finally {
      dnsRedirectLoading = false;
    }
  }

  function promptApplyChanges() {
    filesToModify = [
      { name: '05_routing.json', changesCount: routingRules.length },
      { name: '04_outbounds.json', changesCount: customOutbounds.length },
      { name: '02_dns.json', changesCount: dnsConfig.servers.length },
      { name: '01_log.json', changesCount: 1 },
      { name: '03_inbounds.json', changesCount: inbounds.length },
      { name: '06_policy.json', changesCount: 1 }
    ];
    showApplyConfirm = true;
  }

  async function handleApplyChanges() {
    applyLoading = true;
    validationError = null;
    try {
      const generated = generateFileConfigs();
      for (const [filename, content] of Object.entries(generated)) {
        await apiFetchJSON('/api/xray/config/file', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ filename, content })
        });
      }
      try {
        await apiFetchJSON('/api/service/control?action=restart', { method: 'POST' });
      } catch {
        // Ignored
      }
      showToast('success', $t('app.saved'));
      isDirty = false;
      showApplyConfirm = false;
      activateRestartGrace();
    } catch (e: any) {
      const errMsg = e?.message || 'Save error';
      validationError = errMsg;
      showToast('error', errMsg);
    } finally {
      applyLoading = false;
    }
  }

  function handleUndo() {
    if (undoHistory.length > 0) {
      const prev = undoHistory.pop();
      if (prev) {
        parseXrayFiles(prev);
        isDirty = true;
      }
    }
  }

  function openInEditor() {
    onSwitchTab('editor');
    if (onInsertIntoEditor) {
      onInsertIntoEditor(activePreviewText);
    }
  }

  function handleRestoreDraft() {
    if (detectedDraft?.data) {
      try {
        const parsed =
          typeof detectedDraft.data === 'string'
            ? JSON.parse(detectedDraft.data)
            : detectedDraft.data;
        parseXrayFiles(parsed);
        isDirty = true;
      } catch {
        // Ignored
      }
    }
    detectedDraft = null;
  }

  function handleDiscardDraft() {
    clearDraft('xray_constructor');
    detectedDraft = null;
  }

  onMount(async () => {
    registerDirtySource('xray_constructor', {
      name: 'Xray Constructor',
      isDirty: () => isDirty,
      getDraft: () => generateFileConfigs(),
      onSave: async () => {
        await handleApplyChanges();
        return !validationError;
      }
    });

    const draft = getDraft('xray_constructor');
    if (draft) {
      detectedDraft = draft;
    }

    await Promise.all([loadXrayConfig(), loadXrayOutboundTags(), loadSchema()]);
  });
</script>

<div class="container">
  {#if detectedDraft}
    <DraftRestoreBanner
      timestamp={detectedDraft.timestamp}
      onRestore={handleRestoreDraft}
      onDiscard={handleDiscardDraft}
    />
  {/if}

  {#if schemaLoading}
    <div
      class="loading-state-block"
      style="padding: 48px; text-align: center; color: var(--fg-secondary);"
    >
      <div class="spinner" style="--spinner-size: 24px; margin: 0 auto 12px;"></div>
      <p>{$t('editor.loading_definition')}</p>
    </div>
  {:else if schemaError}
    <div class="error-state-block" style="padding: 48px; text-align: center;">
      <p style="color: var(--danger); margin-bottom: 16px;">
        {$t('editor.definition_load_error', { error: schemaError })}
      </p>
      <button class="btn btn-secondary" onclick={loadSchema}>{$t('app.retry')}</button>
    </div>
  {:else}
    {#if !embedded}
      <div class="constructor-header">
        <div class="constructor-header-content">
          <h2 class="constructor-title">{$t('xray.presets_h1')}</h2>
          <p class="constructor-sub">{$t('xray.presets_sub')}</p>
        </div>
        <div class="constructor-header-actions">
          <Button
            type="button"
            variant="secondary"
            onclick={() => (showPreviewPane = !showPreviewPane)}
            title={$t(showPreviewPane ? 'xray.hide_preview' : 'xray.show_preview')}
          >
            {$t(showPreviewPane ? 'xray.hide_preview' : 'xray.show_preview')}
          </Button>
          <Button type="button" variant="secondary" onclick={openInEditor}>
            {#if selectedFile}
              {$t('mihomo.insert_editor')}
            {:else}
              {$t('mihomo.open_editor')}
            {/if}
          </Button>
          {#if undoHistory.length > 0}
            <Button type="button" variant="secondary" onclick={handleUndo} disabled={applyLoading}>
              {$t('editor.undo')}
            </Button>
          {/if}
          <Button
            type="button"
            variant="primary"
            data-testid="apply-changes-btn"
            onclick={promptApplyChanges}
            disabled={applyLoading}
          >
            {applyLoading ? $t('editor.saving') : $t('mihomo.apply_changes')}
          </Button>
        </div>
      </div>
    {:else}
      <div class="embedded-head-toolbar">
        <div class="embedded-title-tag">
          <strong>{$t('xray.presets_h1')}</strong>
        </div>
        <div class="constructor-header-actions">
          <Button
            type="button"
            variant="secondary"
            onclick={() => (showPreviewPane = !showPreviewPane)}
            title={$t(showPreviewPane ? 'xray.hide_preview' : 'xray.show_preview')}
          >
            {$t(showPreviewPane ? 'xray.hide_preview' : 'xray.show_preview')}
          </Button>
        </div>
      </div>
    {/if}

    <PreflightWarnings
      warnings={saveWarnings}
      onDismiss={() => {
        saveWarnings = [];
      }}
    />

    <div class="gen-layout">
      <!-- Left Panel: Navigation and Section Content -->
      <div class="gen-left">
        <!-- Scenario chips (BUILD-04) -->
        <div class="constructor-scenario-bar">
          <span class="scenario-label">{$t('editor.constructor_scenario')}:</span>
          {#each XRAY_DEFAULT_PRESETS as p}
            <button
              class="scenario-chip"
              class:active={lastAppliedPreset === p.id}
              title={$t(p.descKey)}
              onclick={() => applyPreset(p.id)}
            >
              {$t(p.nameKey)}
              {#if lastAppliedPreset === p.id && isPresetModified}
                <span class="preset-mod-badge">{$t('xray.preset_modified')}</span>
              {/if}
            </button>
          {/each}
        </div>

        <!-- Outbound Tag selection -->
        <div class="rule-providers-row">
          <label class="form-label" for="proxy-tag-select">{$t('xray.main_proxy_outbound')}:</label>
          <Select
            id="proxy-tag-select"
            class="form-select"
            bind:value={proxyTag}
            disabled={outboundTagsLoading}
            onchange={() => (isDirty = true)}
          >
            {#if outboundTagsLoading}
              <option value="" disabled>{$t('editor.loading_tags')}</option>
            {:else if outboundTags.filter((t) => t !== 'direct' && t !== 'block' && t !== 'dns-out').length === 0}
              <option value="" disabled>{$t('editor.no_outbounds_configured')}</option>
            {:else}
              {#each outboundTags.filter((t) => t !== 'direct' && t !== 'block' && t !== 'dns-out') as tag}
                <option value={tag}>{tag}</option>
              {/each}
            {/if}
          </Select>
        </div>

        <!-- Section tabs -->
        <div class="sec-tabs" data-testid="xray-section-tabs">
          {#each [['routing', $t('xray.tab_routing')], ['inbounds', $t('xray.tab_inbounds')], ['dns', 'DNS'], ['outbounds', $t('xray.tab_outbounds')], ['log', $t('xray.tab_log')], ['policy', $t('xray.tab_policy')]] as [id, label]}
            <button
              class="sec-tab"
              class:active={activeSection === id}
              data-tab={id}
              onclick={() => {
                activeSection = id as XraySectionName;
              }}
            >
              {label}
              {#if id === 'routing' && routingRules.length > 0}
                <span class="sec-count">{routingRules.length}</span>
              {/if}
            </button>
          {/each}
        </div>

        <!-- Section Content -->
        {#if activeSection === 'routing'}
          <XraySectionRouting
            bind:routingConfig
            bind:routingRules
            {outboundTags}
            isXrayActive={$capabilities?.active_kernel === 'xray'}
            bind:testRouteForm
            {testRouteRunning}
            {testRouteResult}
            {testRouteError}
            {restartingLogger}
            onRunTestRoute={runTestRoute}
            onRestartLogger={restartLogger}
            onchange={() => (isDirty = true)}
          />
        {:else if activeSection === 'inbounds'}
          <XraySectionInbounds bind:inbounds onchange={() => (isDirty = true)} />
        {:else if activeSection === 'dns'}
          <XraySectionDns
            bind:dnsConfig
            bind:dnsOverVless
            xkeenDns={$capabilities?.xkeen_dns}
            {dnsRedirectLoading}
            onEnableDnsRedirect={enableDNSRedirect}
            onchange={() => (isDirty = true)}
          />
        {:else if activeSection === 'outbounds'}
          <XraySectionOutbounds
            bind:customOutbounds
            {subscriptionOutbounds}
            {outboundDetails}
            {outboundTags}
            onReloadTags={loadXrayOutboundTags}
            onchange={() => (isDirty = true)}
          />
        {:else if activeSection === 'log'}
          <XraySectionLog
            bind:logConfig
            accessPath={xrayFiles['01_log.json']?.log?.access}
            errorPath={xrayFiles['01_log.json']?.log?.error}
            onchange={() => (isDirty = true)}
          />
        {:else if activeSection === 'policy'}
          <XraySectionPolicy bind:policyConfig onchange={() => (isDirty = true)} />
        {/if}
      </div>

      <!-- Right Panel: ConstructorPreview with Tabs -->
      {#if showPreviewPane}
        <ConstructorPreview
          content={activePreviewText}
          language="json"
          storageKey="xray_constructor_preview_width"
          isOpen={showPreviewPane}
          onClose={() => (showPreviewPane = false)}
          tabs={previewTabs}
          activeTab={activePreviewTab}
          onTabChange={(tabId) => (activePreviewTab = tabId)}
          testId="xray-json-preview"
        >
          {#if validationError}
            <div
              class="validation-error-block"
              role="alert"
              aria-live="assertive"
              style="margin-top: 12px; padding: 12px; background: color-mix(in srgb, var(--danger) 10%, transparent); border: 1px solid var(--danger); border-radius: var(--radius-md); color: var(--danger); font-size: 13px;"
            >
              <div style="font-weight: bold; margin-bottom: 6px;">
                {$t('editor.validation_failed')}
              </div>
              <div
                style="white-space: pre-wrap; font-family: var(--font-family-mono); font-size: 13px; margin-bottom: 8px;"
              >
                {parseValidationError(validationError, $currentLang)}
              </div>
            </div>
          {/if}

          {#if embedded}
            <div class="gen-embedded-actions" style="margin-top: 12px; display: flex; gap: 8px;">
              <button class="btn btn-secondary" style="flex: 1;" onclick={openInEditor}>
                {#if selectedFile}
                  {$t('mihomo.insert_editor')}
                {:else}
                  {$t('mihomo.open_editor')}
                {/if}
              </button>
              <button
                class="btn btn-primary"
                data-testid="apply-changes-btn"
                onclick={promptApplyChanges}
                disabled={applyLoading}
                style="flex: 1;"
              >
                {applyLoading ? $t('editor.saving') : $t('mihomo.apply_changes')}
              </button>
            </div>
          {/if}
        </ConstructorPreview>
      {/if}
    </div>
  {/if}
</div>

<Modal
  isOpen={showApplyConfirm}
  title={$t('editor.apply_confirm_title')}
  dataTestid="apply-confirm-dialog"
  onclose={() => (showApplyConfirm = false)}
>
  <p>{$t('editor.apply_confirm_body')}</p>
  <div class="changed-files-list" style="margin-top: 12px;">
    <strong>{$t('xray.files_to_modify')}</strong>
    <ul style="margin: 8px 0 0 0; padding-left: 20px;">
      {#each filesToModify as file}
        {#if file.changesCount > 0}
          <li>
            <code>{file.name}</code>:
            <span
              class="badge"
              style="background-color: var(--warning-soft, color-mix(in srgb, var(--warning) 15%, transparent)); color: var(--warning);"
            >
              {$t('xray.sections_modified', { count: file.changesCount })}
            </span>
          </li>
        {:else}
          <li><code>{file.name}</code>: {$t('xray.no_changes')}</li>
        {/if}
      {/each}
    </ul>
    <p style="margin-top: 12px; font-size: 0.8125rem; color: var(--fg-secondary);">
      {$t('mihomo.backup_notice')}
    </p>
  </div>
  <div style="display: flex; justify-content: flex-end; gap: 12px; margin-top: 16px;">
    <button type="button" class="btn btn-secondary" onclick={() => (showApplyConfirm = false)}>
      {$t('app.cancel')}
    </button>
    <button
      type="button"
      class="btn btn-primary"
      onclick={handleApplyChanges}
      disabled={applyLoading}
    >
      {applyLoading ? $t('editor.saving') : $t('editor.apply_and_restart')}
    </button>
  </div>
</Modal>

<style>
  .container {
    display: flex;
    flex-direction: column;
    height: 100%;
  }

  .constructor-header {
    display: flex;
    justify-content: space-between;
    align-items: center;
    gap: 16px;
    margin-bottom: 20px;
    flex-wrap: wrap;
  }

  .constructor-title {
    font-size: var(--font-size-xl, 1.25rem);
    font-weight: 600;
    margin: 0 0 4px 0;
    color: var(--fg-primary);
  }

  .constructor-sub {
    font-size: var(--font-size-sm, 0.8125rem);
    color: var(--fg-secondary);
    margin: 0;
  }

  .embedded-head-toolbar {
    display: flex;
    justify-content: space-between;
    align-items: center;
    margin-bottom: var(--spacing-3, 12px);
    padding-bottom: var(--spacing-2, 8px);
    border-bottom: 1px solid var(--border-color);
  }

  .embedded-title-tag {
    font-size: var(--font-size-sm, 0.8125rem);
    color: var(--fg-primary);
  }

  .constructor-header-actions {
    display: flex;
    gap: var(--spacing-2, 8px);
  }

  .gen-layout {
    display: flex;
    flex-direction: row;
    gap: 16px;
    align-items: stretch;
    min-height: 520px;
  }

  .gen-left {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
  }

  @media (max-width: 1024px) {
    .gen-layout {
      flex-direction: column;
    }
  }

  .constructor-scenario-bar {
    display: flex;
    align-items: center;
    gap: 8px;
    margin-bottom: 12px;
    flex-wrap: wrap;
  }

  .scenario-label {
    font-size: 0.8125rem;
    color: var(--fg-secondary);
    font-weight: 500;
  }

  .scenario-chip {
    padding: 4px 10px;
    background: var(--bg-surface);
    border: 1px solid var(--border-color);
    border-radius: 12px;
    color: var(--fg-primary);
    font-size: 0.75rem;
    cursor: pointer;
    display: inline-flex;
    align-items: center;
    transition: all 0.15s ease;
  }

  .scenario-chip:hover {
    background: var(--bg-surface-hover);
    border-color: var(--color-primary);
  }

  .scenario-chip.active {
    background: var(--color-primary-subtle, color-mix(in srgb, var(--primary) 15%, transparent));
    border-color: var(--primary);
    color: var(--primary);
    font-weight: 600;
  }

  .preset-mod-badge {
    margin-left: 5px;
    font-size: var(--font-size-xs, 0.6875rem);
    color: var(--warning);
    opacity: 0.9;
    font-style: italic;
  }

  .rule-providers-row {
    display: flex;
    align-items: center;
    gap: 12px;
    margin-bottom: 16px;
  }

  .sec-tabs {
    display: flex;
    gap: var(--spacing-2, 8px);
    border-bottom: 1px solid var(--border-color);
    margin-bottom: var(--spacing-4, 16px);
    overflow-x: auto;
    scrollbar-width: none;
  }

  .sec-tabs::-webkit-scrollbar {
    display: none;
  }

  .sec-tab {
    padding: 8px 12px;
    background: transparent;
    border: none;
    border-bottom: 2px solid transparent;
    color: var(--fg-secondary);
    font-size: var(--font-size-sm, 0.8125rem);
    cursor: pointer;
    display: flex;
    align-items: center;
    gap: 6px;
    margin-bottom: -1px;
    min-height: 36px;
    white-space: nowrap;
  }

  .sec-tab.active {
    color: var(--color-primary);
    border-bottom-color: var(--color-primary);
    font-weight: 500;
  }

  .sec-count {
    background: var(--bg-surface-active);
    color: var(--fg-primary);
    font-size: var(--font-size-xs, 0.6875rem);
    padding: 1px 5px;
    border-radius: 10px;
    font-weight: 600;
  }
</style>
