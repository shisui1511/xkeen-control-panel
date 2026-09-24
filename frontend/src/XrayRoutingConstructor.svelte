<script lang="ts">
  import { onMount, tick } from 'svelte';
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
  import { apiFetch, apiFetchJSON, setDNSRedirect } from './lib/api';
  import PreflightWarnings, {
    type PreflightWarning
  } from './components/editor/PreflightWarnings.svelte';
  import ConstructorPreview, { type PreviewTab } from './components/ConstructorPreview.svelte';
  import {
    rulesFromConfig,
    rulesToConfig,
    cleanBalancer,
    observatoryFor,
    parseJsonc,
    lineDiff,
    hasJsonComments,
    dnsOverProxyRules,
    takeDnsOverProxyRules,
    DEFAULT_OBSERVATORY,
    type DiffLine,
    type XrayBalancer,
    type ObservatorySettings
  } from './lib/constructors/xrayRouting';
  import ScenarioChips from './components/ScenarioChips.svelte';
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
  let balancers = $state<XrayBalancer[]>([]);
  let observatorySettings = $state<ObservatorySettings>({ ...DEFAULT_OBSERVATORY });
  // routing keys the constructor does not edit (domainMatcher, …) and other
  // top-level keys of 05_routing.json are written back unchanged.
  let routingExtra = $state<Record<string, unknown>>({});
  let routingFileExtra = $state<Record<string, unknown>>({});
  // Original file texts, for the apply diff and comment warnings.
  let xrayRawFiles = $state<Record<string, string>>({});
  // Files that exist but could not be parsed: never auto-replace them.
  let unparsedFiles = $state<string[]>([]);
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
  interface FilePlan {
    name: string;
    changed: boolean;
    added: number;
    removed: number;
    hasComments: boolean;
    /** Changed lines with two lines of context; null marks a skipped gap. */
    diff: (DiffLine | null)[];
    content: string;
  }
  let filesToModify = $state<FilePlan[]>([]);
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
    const routingObj: any = {
      ...routingFileExtra,
      routing: {
        ...routingExtra,
        domainStrategy: routingConfig.domainStrategy,
        ...rulesToConfig(routingRules)
      }
    };
    // "DNS over proxy": the DNS module's queries go through the proxy
    // outbound; the managed rules come first so nothing else catches them.
    if (dnsOverVless && proxyTag) {
      routingObj.routing.rules = [
        ...dnsOverProxyRules(dnsConfig.tag || 'dns-in', proxyTag),
        ...routingObj.routing.rules
      ];
    }
    const cleanBalancers = balancers.filter((b) => b.tag.trim()).map(cleanBalancer);
    if (cleanBalancers.length > 0) {
      routingObj.routing.balancers = cleanBalancers;
    }
    const observatory = observatoryFor(cleanBalancers, observatorySettings);
    if (observatory) {
      routingObj.observatory = observatory;
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
        tag: dnsConfig.tag || (dnsOverVless ? 'dns-in' : undefined),
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

  const XRAY_DIR = '/opt/etc/xray/configs';
  const XRAY_FILES = [
    '01_log.json',
    '02_dns.json',
    '03_inbounds.json',
    '04_outbounds.json',
    '05_routing.json',
    '06_policy.json'
  ];

  async function loadXrayConfig() {
    const promises = XRAY_FILES.map(async (name) => {
      try {
        const path = `${XRAY_DIR}/${name}`;
        const res = await apiFetch(`/api/config/read?path=${encodeURIComponent(path)}`);
        if (!res.ok) return;
        const text = await res.text();
        xrayRawFiles[name] = text;
        const data = parseJsonc(text);
        if (data === undefined && text.trim()) {
          unparsedFiles = [...unparsedFiles, name];
        }
        xrayFiles[name] = data ?? {};
      } catch (e: any) {
        if (e?.status === 401) return;
        xrayFiles[name] = {};
      }
    });

    await Promise.allSettled(promises);
    parseXrayFiles(xrayFiles);

    // Auto-initialize if stub config (CONSTR-06 / D-08)
    const routingFile = xrayFiles['05_routing.json'] || {};
    const isRoutingStub = !routingFile.routing?.rules || routingFile.routing.rules.length === 0;
    const outboundsFile = xrayFiles['04_outbounds.json'] || {};
    const isOutboundsStub = !outboundsFile.outbounds || outboundsFile.outbounds.length === 0;
    // A file that exists but does not parse is the user's to fix, not a stub.
    if ((isRoutingStub || isOutboundsStub) && unparsedFiles.length === 0) {
      if (!applyLoading) {
        applyTemplateFiles('selective-routing', false);
      }
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
      const { routing: r, observatory, ...fileRest } = files['05_routing.json'];
      const {
        domainStrategy,
        rules: _rules,
        xcpDisabledRules: _disabled,
        balancers: loadedBalancers,
        ...routingRest
      } = r;
      void _rules;
      void _disabled;
      routingConfig.domainStrategy = domainStrategy || 'IPIfNonMatch';
      const { enabled: managedDns, rest: userRules } = takeDnsOverProxyRules(rulesFromConfig(r));
      routingRules = userRules;
      dnsOverVless = managedDns;
      routingExtra = routingRest;
      routingFileExtra = fileRest;
      const proxyRule = routingRules.find(
        (rule) =>
          rule.outboundTag &&
          rule.outboundTag !== 'direct' &&
          rule.outboundTag !== 'block' &&
          rule.outboundTag !== 'dns-out'
      );
      if (proxyRule?.outboundTag) {
        proxyTag = proxyRule.outboundTag;
      }
      balancers = Array.isArray(loadedBalancers)
        ? loadedBalancers.map((b: any) => ({
            ...b,
            tag: String(b.tag ?? ''),
            selector: Array.isArray(b.selector) ? b.selector : []
          }))
        : [];
      if (observatory) {
        observatorySettings = {
          probeUrl: observatory.probeUrl || DEFAULT_OBSERVATORY.probeUrl,
          probeInterval: observatory.probeInterval || DEFAULT_OBSERVATORY.probeInterval
        };
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
    const custom: any[] = [];
    const subs: any[] = [];

    try {
      const listRes = await apiFetch(`/api/config/list?dir=${encodeURIComponent(XRAY_DIR)}`);
      if (listRes.ok) {
        const files: { name: string; path: string; size: number }[] = await listRes.json();
        const outboundFiles = files.filter(
          (f) => f.name.startsWith('04_outbounds') && f.name.endsWith('.json')
        );
        for (const f of outboundFiles) {
          try {
            const res = await apiFetch(`/api/config/read?path=${encodeURIComponent(f.path)}`);
            if (!res.ok) continue;
            const json = await res.json();
            const fileOutbounds = (json.outbounds ?? []) as any[];

            if (f.name === '04_outbounds.json') {
              for (const o of fileOutbounds) {
                if (o && o.tag && o.tag !== 'direct' && o.tag !== 'block' && o.tag !== 'dns-out') {
                  custom.push(o);
                }
              }
            } else {
              for (const o of fileOutbounds) {
                if (o && o.tag) {
                  subs.push(o);
                }
              }
            }
          } catch {
            /* skip missing/corrupted file */
          }
        }
      }
    } catch {
      /* fallback */
    }

    // Deduplicate custom by tag
    const seenCustom = new Set<string>();
    const uniqueCustom: any[] = [];
    for (const o of custom) {
      if (o.tag && !seenCustom.has(o.tag)) {
        seenCustom.add(o.tag);
        uniqueCustom.push(o);
      }
    }

    // Deduplicate subs by tag
    const seenSubs = new Set<string>();
    const uniqueSubs: any[] = [];
    for (const o of subs) {
      if (o.tag && !seenSubs.has(o.tag)) {
        seenSubs.add(o.tag);
        uniqueSubs.push(o);
      }
    }

    if (uniqueCustom.length > 0) {
      customOutbounds = uniqueCustom;
    }
    subscriptionOutbounds = uniqueSubs;
    outboundTagsLoading = false;

    if (!proxyTag) {
      const allTags = [
        ...uniqueCustom.map((o) => o.tag).filter(Boolean),
        ...uniqueSubs.map((o) => o.tag).filter(Boolean)
      ];
      const systemTags = ['direct', 'block', 'dns-out'];
      const customTag = allTags.find((t) => !systemTags.includes(t));
      if (customTag) {
        proxyTag = customTag;
      }
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
      await setDNSRedirect(true);
      showToast('success', $t('editor.dns_intercept_enabled'));
      await fetchCapabilities();
    } catch (e: any) {
      showToast('error', e?.message || $t('editor.dns_intercept_error'));
    } finally {
      dnsRedirectLoading = false;
    }
  }

  /** Compares every generated file with what was loaded from the router. */
  function computeFilePlans(): FilePlan[] {
    const generated = generateFileConfigs();
    return Object.entries(generated).map(([name, content]) => {
      const after = typeof content === 'string' ? content : JSON.stringify(content, null, 2);
      const loaded = xrayFiles[name];
      const before =
        loaded && Object.keys(loaded).length > 0 ? JSON.stringify(loaded, null, 2) : '';
      const changed = before !== after;
      const full = changed ? lineDiff(before, after) : [];
      const diff: (DiffLine | null)[] = [];
      let lastShown = -1;
      full.forEach((line, i) => {
        const near = full.slice(Math.max(0, i - 2), i + 3).some((l) => l.kind !== 'same');
        if (!near) return;
        if (lastShown !== -1 && i > lastShown + 1) diff.push(null);
        diff.push(line);
        lastShown = i;
      });
      return {
        name,
        changed,
        added: full.filter((l) => l.kind === 'add').length,
        removed: full.filter((l) => l.kind === 'del').length,
        hasComments: changed && hasJsonComments(xrayRawFiles[name] ?? ''),
        diff,
        content: after
      };
    });
  }

  function promptApplyChanges() {
    filesToModify = computeFilePlans();
    showApplyConfirm = true;
  }

  function getOutboundsForTemplate(
    _id: 'minimal-routing' | 'selective-routing' | 'all-proxy-routing'
  ): object {
    return {
      outbounds: [
        { tag: 'direct', protocol: 'freedom' },
        { tag: 'block', protocol: 'blackhole' }
      ]
    };
  }

  function getRoutingForTemplate(
    id: 'minimal-routing' | 'selective-routing' | 'all-proxy-routing',
    tag: string
  ): object {
    let rules: any[] = [];
    if (id === 'minimal-routing') {
      rules = [
        { type: 'field', ip: ['geoip:private'], outboundTag: 'direct' },
        { type: 'field', port: '0-65535', outboundTag: 'direct' }
      ];
    } else if (id === 'selective-routing') {
      rules = [
        { type: 'field', ip: ['geoip:private'], outboundTag: 'direct' },
        { type: 'field', domain: ['geosite:category-ads-all'], outboundTag: 'block' },
        { type: 'field', domain: ['geosite:geolocation-!cn'], outboundTag: 'PROXY_TAG' }
      ];
    } else {
      // all-proxy-routing
      rules = [
        { type: 'field', ip: ['geoip:private'], outboundTag: 'direct' },
        { type: 'field', domain: ['geosite:category-ads-all'], outboundTag: 'block' },
        { type: 'field', port: '0-65535', outboundTag: 'PROXY_TAG' }
      ];
    }
    return {
      routing: {
        domainStrategy: 'IPIfNonMatch',
        rules: substituteProxyTag(rules, tag)
      }
    };
  }

  async function applyTemplateFiles(
    templateId: 'minimal-routing' | 'selective-routing' | 'all-proxy-routing',
    silent = false
  ) {
    const tag = proxyTag && outboundTags.includes(proxyTag) ? proxyTag : 'direct';

    applyLoading = true;
    saveWarnings = [];
    try {
      const outboundsPath = `${XRAY_DIR}/04_outbounds.json`;
      const existingOutbounds = (xrayFiles['04_outbounds.json']?.outbounds || []) as any[];
      const custom = existingOutbounds.filter(
        (o: any) => o && o.tag !== 'direct' && o.tag !== 'block'
      );
      const templateOutbounds = (getOutboundsForTemplate(templateId) as any).outbounds || [];
      const mergedOutbounds = {
        outbounds: [...templateOutbounds, ...custom]
      };

      const saveOutboundsRes = await apiFetch(
        `/api/config/save?path=${encodeURIComponent(outboundsPath)}`,
        {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify(mergedOutbounds, null, 2)
        }
      );
      if (!saveOutboundsRes.ok) throw new Error('Failed to save 04_outbounds.json');

      const templateRouting = getRoutingForTemplate(templateId, 'PROXY_TAG');
      const templateContent = JSON.stringify(templateRouting, null, 2);

      const existingRouting = xrayFiles['05_routing.json'];
      const existingContent = existingRouting ? JSON.stringify(existingRouting, null, 2) : '';

      let mergeRes: { content: string; stats?: { user_rules?: number; rules?: number } };
      try {
        mergeRes = await apiFetchJSON<{
          content: string;
          stats?: { user_rules?: number; rules?: number };
        }>('/api/config/smart-merge', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({
            type: 'xray',
            existing_content: existingContent,
            template_content: templateContent,
            target_file: '05_routing.json',
            active_outbound_tag: tag
          })
        });
      } catch (mergeErr: any) {
        if (mergeErr?.status === 401) return;
        console.error('Smart merge failed for 05_routing.json:', mergeErr);
        if (!silent) {
          showToast('error', $t('editor.smart_merge_failed'));
        }
        return;
      }

      if (!mergeRes || !mergeRes.content) {
        if (!silent) {
          showToast('error', $t('editor.smart_merge_failed'));
        }
        return;
      }

      const routingPath = `${XRAY_DIR}/05_routing.json`;
      const saveRoutingRes = await apiFetch(
        `/api/config/save?path=${encodeURIComponent(routingPath)}`,
        {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: mergeRes.content
        }
      );
      if (!saveRoutingRes.ok) {
        if (saveRoutingRes.status === 422) {
          const resData = await saveRoutingRes.json().catch(() => ({}));
          validationError = resData.error || 'Unknown validation error';
          if (!silent) {
            showToast('error', $t('editor.validation_failed'));
          }
          return;
        }
        throw new Error('Failed to save 05_routing.json');
      }

      const collected: PreflightWarning[] = [];
      const outJson = await saveOutboundsRes.json().catch(() => null);
      const outData = outJson?.data ?? outJson;
      if (Array.isArray(outData?.warnings)) collected.push(...outData.warnings);

      const routJson = await saveRoutingRes.json().catch(() => null);
      const routData = routJson?.data ?? routJson;
      if (Array.isArray(routData?.warnings)) {
        collected.push(...routData.warnings);
      } else if (Array.isArray((mergeRes as any)?.warnings)) {
        collected.push(...(mergeRes as any).warnings);
      }
      saveWarnings = collected;

      if (!silent) {
        const stats = mergeRes.stats || {};
        showToast(
          'success',
          $t('editor.smart_merge_applied', {
            nodes: $tp('editor.smart_merge_applied_nodes', custom.length),
            providers: $tp('editor.smart_merge_applied_providers', 0),
            rules: $tp('editor.smart_merge_applied_rules', stats.rules ?? 0),
            userRules: $tp('editor.smart_merge_applied_user_rules', stats.user_rules ?? 0)
          })
        );
      }
      await loadXrayConfig();
    } catch (e: any) {
      if (e?.status === 401) return;
      if (!silent) {
        showToast('error', $t('editor.save_error') + ': ' + e.message);
      }
    } finally {
      applyLoading = false;
    }
  }

  async function handleApplyChanges() {
    applyLoading = true;
    validationError = null;
    saveWarnings = [];
    const collectedWarnings: PreflightWarning[] = [];

    // Мягкая валидация proxyTag
    if (proxyTag && !outboundTags.includes(proxyTag)) {
      showToast('warning', $t('editor.proxy_tag_warning'));
    }
    try {
      // Only changed files are written: unchanged ones keep their formatting
      // and comments, and the router flash is spared needless writes.
      const changedPlans = computeFilePlans().filter((p) => p.changed);
      if (changedPlans.length === 0) {
        showToast('info', $t('xray.no_changes_to_apply'));
        isDirty = false;
        showApplyConfirm = false;
        applyLoading = false;
        return;
      }
      for (const plan of changedPlans) {
        const filename = plan.name;
        const filePath = `${XRAY_DIR}/${filename}`;
        const saveRes = await apiFetch(`/api/config/save?path=${encodeURIComponent(filePath)}`, {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: plan.content
        });
        if (!saveRes.ok) {
          if (saveRes.status === 422) {
            const data = await saveRes.json().catch(() => ({}));
            validationError = data.error || 'Unknown validation error';
            showToast('error', $t('editor.validation_failed'));
            applyLoading = false;
            return;
          }
          throw new Error(`Failed to save ${filename}`);
        }
        const saveJson = await saveRes.json().catch(() => null);
        const data = saveJson?.data ?? saveJson;
        if (Array.isArray(data?.warnings) && data.warnings.length > 0) {
          collectedWarnings.push(...data.warnings);
        }
      }
      saveWarnings = collectedWarnings;

      try {
        await apiFetch('/api/service/control?action=restart', { method: 'POST' });
      } catch {
        // Ignored
      }
      showToast('success', $t('app.saved'));
      isDirty = false;
      showApplyConfirm = false;
      activateRestartGrace();
      await loadXrayConfig();
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

    await loadXrayOutboundTags();
    await loadXrayConfig();
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
          <svg
            width="13"
            height="13"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            stroke-width="2"
            style="margin-right: 4px;"
          >
            <rect x="3" y="3" width="18" height="18" rx="2" ry="2" />
            <line x1="15" y1="3" x2="15" y2="21" />
          </svg>
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
      <ScenarioChips
        label={$t('editor.constructor_scenario')}
        options={XRAY_DEFAULT_PRESETS.map((p) => ({
          id: p.id,
          label: $t(p.nameKey),
          description: $t(p.descKey)
        }))}
        active={lastAppliedPreset || ''}
        modifiedBadge={isPresetModified ? $t('xray.preset_modified') : undefined}
        onSelect={applyPreset}
      />

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
          bind:balancers
          bind:observatorySettings
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
          {proxyTag}
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
</div>

<Modal
  isOpen={showApplyConfirm}
  title={$t('editor.apply_confirm_title')}
  dataTestid="apply-confirm-dialog"
  onclose={() => (showApplyConfirm = false)}
>
  <p>{$t('editor.apply_confirm_body')}</p>
  <div class="apply-plan">
    <strong>{$t('xray.files_to_modify')}</strong>
    <ul class="apply-plan-list">
      {#each filesToModify as file (file.name)}
        <li>
          {#if file.changed}
            {#if file.hasComments}
              <p class="apply-plan-warn">{$t('xray.comments_will_be_lost')}</p>
            {/if}
            <details>
              <summary>
                <code>{file.name}</code>
                <span class="diff-stat add">+{file.added}</span>
                <span class="diff-stat del">−{file.removed}</span>
              </summary>

              <pre class="apply-diff">{#each file.diff as line, i (i)}{#if line === null}<span
                      class="diff-gap">…</span
                    >{:else}<span class="diff-{line.kind}"
                      >{line.kind === 'add'
                        ? '+ '
                        : line.kind === 'del'
                          ? '- '
                          : '  '}{line.text}</span
                    >{/if}{/each}</pre>
            </details>
          {:else}
            <code>{file.name}</code>: {$t('xray.no_changes')}
          {/if}
        </li>
      {/each}
    </ul>
    <p class="apply-plan-note">{$t('mihomo.backup_notice')}</p>
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
    gap: 0;
    align-items: stretch;
    position: relative;
    min-height: 520px;
  }

  .gen-left {
    flex: 1;
    min-width: 320px;
    padding-right: var(--spacing-3, 12px);
    display: flex;
    flex-direction: column;
  }

  @media (max-width: 1024px) {
    .gen-layout {
      flex-direction: column;
    }
  }

  .rule-providers-row {
    display: flex;
    align-items: center;
    gap: 12px;
    margin-bottom: 16px;
  }

  .rule-providers-row :global(.xcp-select) {
    width: auto;
    min-width: 200px;
    max-width: 360px;
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

  .apply-plan {
    margin-top: var(--spacing-3);
  }

  .apply-plan-list {
    margin: var(--spacing-2) 0 0;
    padding-left: 0;
    list-style: none;
    display: flex;
    flex-direction: column;
    gap: var(--spacing-2);
  }

  .apply-plan-list summary {
    cursor: pointer;
    display: flex;
    align-items: center;
    gap: var(--spacing-2);
  }

  .diff-stat {
    font-family: var(--font-family-mono);
    font-size: var(--font-size-xs);
  }

  .diff-stat.add,
  .apply-diff :global(.diff-add) {
    color: var(--success);
  }

  .diff-stat.del,
  .apply-diff :global(.diff-del) {
    color: var(--danger);
  }

  .apply-diff {
    margin: var(--spacing-2) 0 0;
    max-height: 40vh;
    overflow: auto;
    padding: var(--spacing-2);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    background: var(--code-bg);
    font-family: var(--font-family-mono);
    font-size: var(--font-size-xs);
    line-height: 1.45;
    white-space: pre;
    scrollbar-width: thin;
    scrollbar-color: var(--scrollbar-thumb) transparent;
  }

  .apply-diff :global(span) {
    display: block;
  }

  .apply-diff :global(.diff-same),
  .apply-diff :global(.diff-gap) {
    color: var(--fg-muted);
  }

  .apply-plan-warn {
    margin: var(--spacing-2) 0 0;
    font-size: var(--font-size-xs);
    color: var(--warning);
  }

  .apply-plan-note {
    margin-top: var(--spacing-3);
    font-size: var(--font-size-sm);
    color: var(--fg-secondary);
  }
</style>
