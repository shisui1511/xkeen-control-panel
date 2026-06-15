<script lang="ts">
  import { onMount, tick } from 'svelte';
  import { currentLang, t } from './i18n';
  import { capabilities, showToast, fetchCapabilities } from './stores';
  import { mergeXrayFile, syncDnsPipeline, substituteProxyTag } from './lib/xrayMerge';
  import { parseValidationError } from './lib/errorParser';

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

  interface XrayRoutingRule {
    id: string;
    type: 'field';
    outboundTag: string;
    domain?: string[];
    ip?: string[];
    port?: string;
    network?: string;
    protocol?: string[];
    inboundTag?: string[];
  }

  interface DNSServer {
    address: string;
    port?: number;
    tag?: string;
    domains?: string[];
    skipFallback?: boolean;
    inboundPort?: number;
  }

  interface XrayInbound {
    tag: string;
    port: number;
    listen?: string;
    protocol: string;
    settings?: Record<string, any>;
    sniffing?: Record<string, any>;
    streamSettings?: Record<string, any>;
  }

  interface OutboundDetail {
    tag: string;
    protocol: string;
    server?: string;
  }

  // Runes State (Svelte 5)
  let activeSection = $state<'log' | 'dns' | 'inbounds' | 'outbounds' | 'routing' | 'policy'>(
    'routing'
  );
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
  let outboundTags = $state<string[]>(['direct', 'block', 'dns-out']);
  let outboundTagsLoading = $state(false);
  let outboundDetails = $state<OutboundDetail[]>([]);
  let proxyTag = $state<string>('');
  let routingRules = $state<XrayRoutingRule[]>([]);
  let policyConfig = $state<{ levels: Record<string, any>; system: Record<string, any> }>({
    levels: { '0': { handshake: 4, connIdle: 300, uplinkOnly: 2, downlinkOnly: 5 } },
    system: {}
  });

  let schema = $state<any>(null);
  let schemaLoading = $state(true);
  let schemaError = $state('');
  let validationError = $state('');

  let dnsOverVless = $state(false);
  let dnsRedirectLoading = $state(false);

  async function enableDNSRedirect() {
    dnsRedirectLoading = true;
    try {
      const csrfToken = localStorage.getItem('csrf_token');
      const res = await fetch('/api/service/dns-redirect', {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          'X-CSRF-Token': csrfToken || ''
        },
        body: JSON.stringify({ enabled: true })
      });
      if (res.ok) {
        showToast('success', ru ? 'Перехват DNS успешно включен' : 'DNS Interception enabled successfully');
        await fetchCapabilities();
      } else {
        const text = await res.text();
        showToast('error', text || (ru ? 'Не удалось включить перехват DNS' : 'Failed to enable DNS Interception'));
      }
    } catch (err: any) {
      showToast('error', err.message || String(err));
    } finally {
      dnsRedirectLoading = false;
    }
  }

  async function loadSchema() {
    schemaLoading = true;
    schemaError = '';
    try {
      const res = await fetch('/api/assets/definition');
      if (!res.ok) throw new Error(`HTTP ${res.status}`);
      schema = await res.json();
    } catch (e: any) {
      schemaError = e.message || 'Unknown error';
    } finally {
      schemaLoading = false;
    }
  }

  let isDirty = $state(false);
  let applyLoading = $state(false);
  let showApplyConfirm = $state(false);
  let loadErrors = $state<Record<string, string>>({});
  let xrayFiles = $state<Record<string, any>>({});

  // Import Node states (runes)
  let showImportModal = $state(false);
  let importLink = $state('');
  let importTag = $state('');
  let importStep = $state(1); // 1: Input link, 2: Preview & Confirm tag
  let importLoading = $state(false);
  let importNodes = $state<
    { link: string; outbound: any; tag: string; rowError?: string | null }[]
  >([]);
  let importErrorMsg = $state('');

  // Form states
  let showRuleForm = $state(false);
  let newRule = $state({
    outboundTag: 'direct',
    domainRaw: '',
    ipRaw: '',
    port: '',
    network: 'tcp,udp',
    inboundTagRaw: ''
  });

  let showDnsForm = $state(false);
  let newDns = $state({
    address: '',
    port: 53,
    tag: '',
    domainsRaw: '',
    skipFallback: false,
    inboundPort: 1053
  });

  let showHostForm = $state(false);
  let newHost = $state({
    domain: '',
    ip: ''
  });

  let showInboundForm = $state(false);
  let newInbound = $state({
    tag: '',
    port: 10808,
    listen: '127.0.0.1',
    protocol: 'socks',
    udp: true
  });

  let ruleFilterTag = $state<string>('');

  const XRAY_DIR = '/opt/etc/xray/configs';
  const XRAY_FILES = [
    '01_log.json',
    '02_dns.json',
    '03_inbounds.json',
    '04_outbounds.json',
    '05_routing.json',
    '06_policy.json'
  ];

  onMount(async () => {
    await loadSchema();
    await loadAllConfigs();
  });

  async function loadAllConfigs() {
    loadErrors = {};
    const promises = XRAY_FILES.map(async (name) => {
      try {
        const path = `${XRAY_DIR}/${name}`;
        const res = await fetch(`/api/config/read?path=${encodeURIComponent(path)}`);
        if (!res.ok) throw new Error(`HTTP ${res.status}`);
        const data = await res.json();
        xrayFiles[name] = data;
      } catch (e: any) {
        loadErrors[name] = e.message;
        xrayFiles[name] = {};
      }
    });

    await Promise.allSettled(promises);
    populateFromFiles();
    outboundTags = await loadXrayOutboundTags();
    if (outboundTags.length > 0) {
      if (!proxyTag) {
        const systemTags = ['direct', 'block', 'dns-out'];
        const custom = outboundTags.find((t) => !systemTags.includes(t));
        proxyTag = custom || outboundTags[0];
      }
      if (!newRule.outboundTag) {
        newRule.outboundTag = outboundTags[0];
      }
    }
    isDirty = false;

    // Auto-initialize if stub config (CONSTR-06 / D-08)
    const routingFile = xrayFiles['05_routing.json'] || {};
    const isRoutingStub = !routingFile.routing?.rules || routingFile.routing.rules.length === 0;
    const outboundsFile = xrayFiles['04_outbounds.json'] || {};
    const isOutboundsStub = !outboundsFile.outbounds || outboundsFile.outbounds.length === 0;
    if (isRoutingStub || isOutboundsStub) {
      if (!applyLoading) {
        applyTemplateFiles('selective-routing', true);
      }
    }
  }

  function populateFromFiles() {
    // 01_log.json
    const logFile = xrayFiles['01_log.json'] || {};
    logConfig = {
      loglevel: logFile.log?.loglevel || 'warning',
      dnsLog: logFile.log?.dnsLog ?? false
    };

    // 02_dns.json
    const dnsFile = xrayFiles['02_dns.json'] || {};
    dnsConfig = {
      tag: dnsFile.dns?.tag || 'dns-in',
      servers: dnsFile.dns?.servers || [],
      queryStrategy: dnsFile.dns?.queryStrategy || 'UseIP',
      hosts: dnsFile.dns?.hosts || {}
    };

    // 03_inbounds.json
    const inboundsFile = xrayFiles['03_inbounds.json'] || {};
    inbounds = inboundsFile.inbounds || [];

    // 05_routing.json
    const routingFile = xrayFiles['05_routing.json'] || {};
    routingConfig = {
      domainStrategy: routingFile.routing?.domainStrategy || 'IPIfNonMatch'
    };
    const rawRules = routingFile.routing?.rules || [];
    const hasDnsInRule = rawRules.some((r: any) => r.inboundTag && r.inboundTag.includes('dns-in'));
    const hasPort53Rule = rawRules.some((r: any) => (r.port === 53 || r.port === '53') && r.outboundTag === 'dns-out');
    dnsOverVless = hasDnsInRule && hasPort53Rule;

    const filteredRules = rawRules.filter((r: any) => {
      const isDnsInRule = r.inboundTag && r.inboundTag.includes('dns-in');
      const isPort53Rule = (r.port === 53 || r.port === '53') && r.outboundTag === 'dns-out';
      return !isDnsInRule && !isPort53Rule;
    });

    routingRules = filteredRules.map((r: any) => ({
      id: r.id || crypto.randomUUID(),
      type: r.type || 'field',
      outboundTag: r.outboundTag || 'direct',
      domain: r.domain,
      ip: r.ip,
      port: r.port,
      network: r.network,
      protocol: r.protocol,
      inboundTag: r.inboundTag
    }));

    const proxyRule = routingRules.find(
      (r: any) =>
        r.outboundTag !== 'direct' && r.outboundTag !== 'block' && r.outboundTag !== 'dns-out'
    );
    proxyTag = proxyRule ? proxyRule.outboundTag : '';

    // Stub detection removed (CONSTR-06)

    // 06_policy.json
    const policyFile = xrayFiles['06_policy.json'] || {};
    policyConfig = {
      levels: policyFile.policy?.levels || {
        '0': { handshake: 4, connIdle: 300, uplinkOnly: 2, downlinkOnly: 5 }
      },
      system: policyFile.policy?.system || {}
    };
  }

  async function loadXrayOutboundTags(): Promise<string[]> {
    outboundTagsLoading = true;
    const tags: string[] = [];
    const details: OutboundDetail[] = [];

    details.push({ tag: 'direct', protocol: 'freedom' });
    details.push({ tag: 'block', protocol: 'blackhole' });
    details.push({ tag: 'dns-out', protocol: 'dns' });

    try {
      const listRes = await fetch(`/api/config/list?dir=${encodeURIComponent(XRAY_DIR)}`);
      if (listRes.ok) {
        const files: { name: string; path: string; size: number }[] = await listRes.json();
        const outboundFiles = files.filter(
          (f) => f.name.startsWith('04_outbounds') && f.name.endsWith('.json')
        );
        for (const f of outboundFiles) {
          try {
            const res = await fetch(`/api/config/read?path=${encodeURIComponent(f.path)}`);
            if (!res.ok) continue;
            const json = await res.json();
            const fileOutbounds = (json.outbounds ?? []) as any[];
            for (const o of fileOutbounds) {
              if (o.tag) {
                tags.push(o.tag);
                let server = '';
                if (o.settings?.vnext?.[0]?.address) {
                  server = o.settings.vnext[0].address;
                } else if (o.settings?.servers?.[0]?.address) {
                  server = o.settings.servers[0].address;
                }
                details.push({
                  tag: o.tag,
                  protocol: o.protocol || 'unknown',
                  server: server || undefined
                });
              }
            }
          } catch {
            /* skip missing/corrupted file */
          }
        }
      } else {
        // Fallback to static array read if /api/config/list fails
        for (const name of ['04_outbounds.json', '04_outbounds.manual.json']) {
          try {
            const path = `${XRAY_DIR}/${name}`;
            const res = await fetch(`/api/config/read?path=${encodeURIComponent(path)}`);
            if (!res.ok) continue;
            const json = await res.json();
            const fileOutbounds = (json.outbounds ?? []) as any[];
            for (const o of fileOutbounds) {
              if (o.tag) {
                tags.push(o.tag);
                let server = '';
                if (o.settings?.vnext?.[0]?.address) {
                  server = o.settings.vnext[0].address;
                } else if (o.settings?.servers?.[0]?.address) {
                  server = o.settings.servers[0].address;
                }
                details.push({
                  tag: o.tag,
                  protocol: o.protocol || 'unknown',
                  server: server || undefined
                });
              }
            }
          } catch {
            /* skip missing */
          }
        }
      }
    } catch {
      /* fallback */
    }

    const uniqueDetails: OutboundDetail[] = [];
    const seen = new Set<string>();
    for (const d of details) {
      if (!seen.has(d.tag)) {
        seen.add(d.tag);
        uniqueDetails.push(d);
      }
    }
    outboundDetails = uniqueDetails;
    outboundTagsLoading = false;

    return [...new Set([...tags, 'direct', 'block', 'dns-out'])];
  }

  function getChangedFiles(): Array<[string, any]> {
    const list: Array<[string, any]> = [];

    // 01_log.json
    list.push(['01_log.json', { loglevel: logConfig.loglevel, dnsLog: logConfig.dnsLog }]);

    // 02_dns.json
    list.push([
      '02_dns.json',
      { servers: dnsConfig.servers, queryStrategy: dnsConfig.queryStrategy, hosts: dnsConfig.hosts, tag: 'dns-in' }
    ]);

    // Синхронизируем DNS c inbounds и routing
    const { dnsInbounds, routingRules: generatedRules } = syncDnsPipeline(
      dnsConfig.servers,
      proxyTag
    );

    // 03_inbounds.json
    list.push(['03_inbounds.json', { dnsInbounds }]);

    // 05_routing.json
    const rules = [...routingRules];
    for (const r of generatedRules) {
      const exists = rules.some((ex) => ex.inboundTag && ex.inboundTag.includes(r.inboundTag[0]));
      if (!exists) {
        rules.unshift({
          id: crypto.randomUUID(),
          ...r
        });
      }
    }
    if (dnsOverVless) {
      const activeProxy = proxyTag || outboundTags.find((t) => !['direct', 'block', 'dns-out'].includes(t)) || 'direct';
      rules.unshift({
        id: crypto.randomUUID(),
        type: 'field',
        port: 53,
        outboundTag: 'dns-out'
      });
      rules.unshift({
        id: crypto.randomUUID(),
        type: 'field',
        inboundTag: ['dns-in'],
        outboundTag: activeProxy
      });
    }
    list.push([
      '05_routing.json',
      { rules, proxyTag, domainStrategy: routingConfig.domainStrategy }
    ]);

    // 06_policy.json
    const lvl0 = policyConfig.levels?.['0'] || {
      handshake: 4,
      connIdle: 300,
      uplinkOnly: 2,
      downlinkOnly: 5
    };
    list.push(['06_policy.json', { level0: lvl0, system: policyConfig.system }]);

    return list;
  }

  async function handleApplyChanges() {
    if (!showApplyConfirm) {
      showApplyConfirm = true;
      return;
    }
    showApplyConfirm = false;
    applyLoading = true;
    await tick();

    // Мягкая валидация proxyTag
    if (proxyTag && !outboundTags.includes(proxyTag)) {
      showToast('warning', $t('editor.proxy_tag_warning'));
    }

    try {
      const csrfToken = localStorage.getItem('csrf_token');

      const changed = filesToModify.filter((f) => f.changesCount > 0);
      if (changed.length === 0) {
        showToast('info', $t('editor.no_changes'));
        applyLoading = false;
        return;
      }

      validationError = '';

      // 1. Сохранить изменённые файлы
      for (const file of changed) {
        const managedPair = getChangedFiles().find(([n]) => n === file.name);
        if (!managedPair) continue;
        const [, managed] = managedPair;
        const existing = xrayFiles[file.name] ?? {};
        const merged = mergeXrayFile(file.name, existing, managed);
        const saveRes = await fetch(`/api/config/save?path=${encodeURIComponent(file.path)}`, {
          method: 'POST',
          headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrfToken || '' },
          body: JSON.stringify(merged, null, 2)
        });
        if (!saveRes.ok) {
          if (saveRes.status === 422) {
            const data = await saveRes.json();
            validationError = data.error || 'Unknown validation error';
            showToast('error', $t('editor.validation_failed'));
            applyLoading = false;
            return;
          }
          throw new Error(`Failed to save ${file.name}`);
        }
      }

      // 2. Рестарт XKeen
      const restartRes = await fetch('/api/service/control?action=restart', {
        method: 'POST',
        headers: { 'X-CSRF-Token': csrfToken || '' }
      });
      if (!restartRes.ok) throw new Error('Failed to restart service');

      isDirty = false;
      showToast('success', $t('editor.file_saved'));
      await loadAllConfigs();
    } catch (e: any) {
      showToast('error', $t('editor.save_error') + ': ' + e.message);
    } finally {
      applyLoading = false;
    }
  }

  // CRUD для правил
  function addRule() {
    const domains = newRule.domainRaw.trim()
      ? newRule.domainRaw.split(/[\s,]+/).filter(Boolean)
      : undefined;
    const ips = newRule.ipRaw.trim() ? newRule.ipRaw.split(/[\s,]+/).filter(Boolean) : undefined;
    const inbounds = newRule.inboundTagRaw.trim()
      ? newRule.inboundTagRaw.split(/[\s,]+/).filter(Boolean)
      : undefined;

    routingRules = [
      ...routingRules,
      {
        id: crypto.randomUUID(),
        type: 'field',
        outboundTag: newRule.outboundTag,
        domain: domains,
        ip: ips,
        port: newRule.port.trim() || undefined,
        network: newRule.network !== 'tcp,udp' ? newRule.network : undefined,
        inboundTag: inbounds
      }
    ];

    showRuleForm = false;
    newRule.domainRaw = '';
    newRule.ipRaw = '';
    newRule.port = '';
    newRule.network = 'tcp,udp';
    newRule.inboundTagRaw = '';
    isDirty = true;
  }

  function removeRule(id: string) {
    routingRules = routingRules.filter((r) => r.id !== id);
    isDirty = true;
  }

  function moveRule(id: string, dir: -1 | 1) {
    const idx = routingRules.findIndex((r) => r.id === id);
    if (idx < 0) return;
    const next = idx + dir;
    if (next < 0 || next >= routingRules.length) return;
    const arr = [...routingRules];
    [arr[idx], arr[next]] = [arr[next], arr[idx]];
    routingRules = arr;
    isDirty = true;
  }

  // CRUD для DNS серверов
  function addDNSServer() {
    if (!newDns.address.trim()) return;
    if (newDns.tag.trim()) {
      const serverObj: DNSServer = {
        address: newDns.address.trim(),
        port: Number(newDns.port) || 53,
        tag: newDns.tag.trim(),
        domains: newDns.domainsRaw.trim()
          ? newDns.domainsRaw.split(/[\s,]+/).filter(Boolean)
          : undefined,
        skipFallback: newDns.skipFallback
      };
      dnsConfig.servers = [...dnsConfig.servers, serverObj];
    } else {
      dnsConfig.servers = [...dnsConfig.servers, newDns.address.trim()];
    }

    newDns.address = '';
    newDns.port = 53;
    newDns.tag = '';
    newDns.domainsRaw = '';
    newDns.skipFallback = false;
    newDns.inboundPort = 1053;
    showDnsForm = false;
    isDirty = true;
  }

  function removeDNSServer(index: number) {
    dnsConfig.servers = dnsConfig.servers.filter((_, idx) => idx !== index);
    isDirty = true;
  }

  // Hosts CRUD
  function addHost() {
    if (!newHost.domain.trim() || !newHost.ip.trim()) return;
    dnsConfig.hosts = {
      ...dnsConfig.hosts,
      [newHost.domain.trim()]: newHost.ip.trim()
    };
    newHost.domain = '';
    newHost.ip = '';
    showHostForm = false;
    isDirty = true;
  }

  function removeHost(domain: string) {
    const updated = { ...dnsConfig.hosts };
    delete updated[domain];
    dnsConfig.hosts = updated;
    isDirty = true;
  }

  // CRUD для Inbounds
  function addInbound() {
    if (!newInbound.tag.trim()) return;
    inbounds = [
      ...inbounds,
      {
        tag: newInbound.tag.trim(),
        port: Number(newInbound.port),
        listen: newInbound.listen.trim(),
        protocol: newInbound.protocol,
        settings: newInbound.protocol === 'socks' ? { auth: 'noauth', udp: newInbound.udp } : {}
      }
    ];
    newInbound.tag = '';
    newInbound.port = 10808;
    newInbound.listen = '127.0.0.1';
    showInboundForm = false;
    isDirty = true;
  }

  function removeInbound(tag: string) {
    inbounds = inbounds.filter((ib) => ib.tag !== tag);
    isDirty = true;
  }

  // Пресеты
  function applyPreset(presetId: string) {
    validationError = '';
    if (schema && schema.xray && schema.xray.presets) {
      const p = schema.xray.presets.find((x: any) => x.id === presetId);
      if (p) {
        dnsConfig.servers = (p.dns_servers || []).map((s: any) => {
          if (typeof s === 'string') return s;
          return {
            address: s.address,
            port: s.port,
            tag: s.tag,
            domains: s.domains ? [...s.domains] : undefined,
            skipFallback: s.skipFallback
          };
        });
        routingRules = (p.routing_rules || []).map((r: any) => ({
          id: crypto.randomUUID(),
          type: r.type || 'field',
          outboundTag: r.outboundTag,
          domain: r.domain ? [...r.domain] : undefined,
          ip: r.ip ? [...r.ip] : undefined,
          port: r.port,
          network: r.network,
          protocol: r.protocol ? [...r.protocol] : undefined,
          inboundTag: r.inboundTag ? [...r.inboundTag] : undefined
        }));
        dnsOverVless = p.dns_over_vless ?? false;
        isDirty = true;
        showToast('success', $t('editor.preset_applied'));
        return;
      }
    }

    if (presetId === 'selective-routing') {
      dnsConfig.servers = [
        '1.1.1.1',
        {
          address: '8.8.8.8',
          port: 53,
          tag: 'dns-in-ytb',
          domains: ['geosite:youtube', 'geosite:google'],
          skipFallback: true
        },
        {
          address: '77.88.8.8',
          port: 53,
          tag: 'dns-in-direct',
          domains: ['geosite:tld-ru'],
          skipFallback: false
        }
      ];
      routingRules = [
        {
          id: crypto.randomUUID(),
          type: 'field',
          outboundTag: 'direct',
          ip: ['geoip:private']
        },
        {
          id: crypto.randomUUID(),
          type: 'field',
          outboundTag: 'block',
          domain: ['geosite:category-ads-all']
        },
        {
          id: crypto.randomUUID(),
          type: 'field',
          outboundTag: 'PROXY_TAG',
          network: 'tcp,udp'
        }
      ];
      dnsOverVless = true;
    } else if (presetId === 'all-proxy-routing') {
      dnsConfig.servers = ['1.1.1.1', '8.8.8.8'];
      routingRules = [
        {
          id: crypto.randomUUID(),
          type: 'field',
          outboundTag: 'direct',
          ip: ['geoip:private']
        },
        {
          id: crypto.randomUUID(),
          type: 'field',
          outboundTag: 'PROXY_TAG',
          network: 'tcp,udp'
        }
      ];
      dnsOverVless = true;
    } else if (presetId === 'selective-no-quic') {
      dnsConfig.servers = [
        '1.1.1.1',
        {
          address: '8.8.8.8',
          port: 53,
          tag: 'dns-in-ytb',
          domains: ['geosite:youtube', 'geosite:google'],
          skipFallback: true
        }
      ];
      routingRules = [
        {
          id: crypto.randomUUID(),
          type: 'field',
          outboundTag: 'block',
          network: 'udp',
          port: '443'
        },
        {
          id: crypto.randomUUID(),
          type: 'field',
          outboundTag: 'direct',
          ip: ['geoip:private']
        },
        {
          id: crypto.randomUUID(),
          type: 'field',
          outboundTag: 'block',
          domain: ['geosite:category-ads-all']
        },
        {
          id: crypto.randomUUID(),
          type: 'field',
          outboundTag: 'PROXY_TAG',
          network: 'tcp,udp'
        }
      ];
      dnsOverVless = true;
    } else if (presetId === 'only-blocked-routing') {
      dnsConfig.servers = [
        '1.1.1.1',
        {
          address: '8.8.8.8',
          port: 53,
          tag: 'dns-in-ytb',
          domains: ['geosite:category-anticensorship', 'geosite:refilter'],
          skipFallback: true
        }
      ];
      routingRules = [
        {
          id: crypto.randomUUID(),
          type: 'field',
          outboundTag: 'direct',
          ip: ['geoip:private']
        },
        {
          id: crypto.randomUUID(),
          type: 'field',
          outboundTag: 'PROXY_TAG',
          domain: ['geosite:category-anticensorship', 'geosite:refilter']
        },
        {
          id: crypto.randomUUID(),
          type: 'field',
          outboundTag: 'direct',
          port: '0-65535'
        }
      ];
      dnsOverVless = true;
    }
    isDirty = true;
    showToast('success', $t('editor.preset_applied'));
  }

  // Template data helpers (Bug C / D-06)
  // Outbounds identical for all three templates: direct(freedom), block(blackhole)
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

  // Apply template files: writes 04_outbounds.json + 05_routing.json (Bug C / D-06, D-07)
  async function applyTemplateFiles(
    templateId: 'minimal-routing' | 'selective-routing' | 'all-proxy-routing',
    silent = false
  ) {
    const tag = proxyTag && outboundTags.includes(proxyTag) ? proxyTag : 'direct';

    applyLoading = true;
    try {
      const csrfToken = localStorage.getItem('csrf_token');

      // Write 04_outbounds.json
      const outboundsPath = `${XRAY_DIR}/04_outbounds.json`;
      const saveOutboundsRes = await fetch(
        `/api/config/save?path=${encodeURIComponent(outboundsPath)}`,
        {
          method: 'POST',
          headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrfToken || '' },
          body: JSON.stringify(getOutboundsForTemplate(templateId), null, 2)
        }
      );
      if (!saveOutboundsRes.ok) throw new Error('Failed to save 04_outbounds.json');

      // Write 05_routing.json
      const routingPath = `${XRAY_DIR}/05_routing.json`;
      const saveRoutingRes = await fetch(
        `/api/config/save?path=${encodeURIComponent(routingPath)}`,
        {
          method: 'POST',
          headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrfToken || '' },
          body: JSON.stringify(getRoutingForTemplate(templateId, tag), null, 2)
        }
      );
      if (!saveRoutingRes.ok) throw new Error('Failed to save 05_routing.json');

      if (!silent) {
        showToast('success', $t('editor.preset_applied'));
      }
      await loadAllConfigs();
    } catch (e: any) {
      if (!silent) {
        showToast('error', $t('editor.save_error') + ': ' + e.message);
      }
    } finally {
      applyLoading = false;
    }
  }

  function openInEditor() {
    if (onInsertIntoEditor) {
      onInsertIntoEditor(previewJson);
    } else {
      onSwitchTab('editor');
    }
  }

  function countDiffKeys(existing: any, merged: any): number {
    let count = 0;
    const allKeys = new Set([...Object.keys(existing || {}), ...Object.keys(merged || {})]);
    for (const k of allKeys) {
      if (JSON.stringify(existing?.[k]) !== JSON.stringify(merged?.[k])) {
        count++;
      }
    }
    return count;
  }

  interface FileChangeInfo {
    name: string;
    path: string;
    changesCount: number;
  }

  let filesToModify = $derived.by<FileChangeInfo[]>(() => {
    const list = getChangedFiles();
    return list.map(([name, managed]) => {
      const existing = xrayFiles[name] ?? {};
      const merged = mergeXrayFile(name, existing, managed);
      return {
        name,
        path: `${XRAY_DIR}/${name}`,
        changesCount: countDiffKeys(existing, merged)
      };
    });
  });

  let filteredRules = $derived(
    routingRules.filter((r) => !ruleFilterTag || r.outboundTag === ruleFilterTag)
  );

  // Превью
  let previewJson = $derived.by(() => {
    const list = getChangedFiles();
    const result: Record<string, any> = {};
    for (const [name, managed] of list) {
      const existing = xrayFiles[name] ?? {};
      result[name] = mergeXrayFile(name, existing, managed);
    }
    return JSON.stringify(result, null, 2);
  });

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

  function generateUniqueTag(baseTag: string, existing: string[]): string {
    let tag = baseTag.trim() || 'node';
    if (!existing.includes(tag)) {
      return tag;
    }
    let counter = 1;
    while (existing.includes(`${tag}-${counter}`)) {
      counter++;
    }
    return `${tag}-${counter}`;
  }

  function openImportModal() {
    showImportModal = true;
    importLink = '';
    importTag = '';
    importStep = 1;
    importLoading = false;
    importNodes = [];
    importErrorMsg = '';
  }

  function closeImportModal() {
    showImportModal = false;
  }

  async function parseImportLink() {
    const trimmed = importLink.trim();
    if (!trimmed) {
      importErrorMsg = $t('subscr.import_error_empty');
      return;
    }

    const lines = trimmed
      .split('\n')
      .map((l) => l.trim())
      .filter((l) => l.length > 0);

    importErrorMsg = '';
    importLoading = true;

    try {
      const csrfToken = localStorage.getItem('csrf_token');
      const res = await fetch('/api/outbound/parse', {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          'X-CSRF-Token': csrfToken || ''
        },
        body: JSON.stringify({ links: lines })
      });

      const data = await res.json();
      if (!res.ok) {
        importErrorMsg = data.error || $t('subscr.import_error_invalid');
        return;
      }

      if (data.data && data.data.length > 0) {
        const newImportNodes = [];
        const existingTags = [...outboundTags];

        for (let i = 0; i < data.data.length; i++) {
          const result = data.data[i];
          if (result.outbound) {
            const baseTag = result.outbound.tag || 'node';
            const uniqueTag = generateUniqueTag(baseTag, existingTags);
            existingTags.push(uniqueTag);
            newImportNodes.push({
              link: lines[i],
              outbound: result.outbound,
              tag: uniqueTag,
              rowError: result.error || null
            });
          } else {
            newImportNodes.push({
              link: lines[i],
              outbound: null,
              tag: '',
              rowError: result.error || $t('subscr.import_error_invalid')
            });
          }
        }

        importNodes = newImportNodes;
        importStep = 2;
      } else {
        importErrorMsg = $t('subscr.import_error_invalid');
      }
    } catch (e: any) {
      importErrorMsg = e.message || $t('subscr.import_error_invalid');
    } finally {
      importLoading = false;
    }
  }

  async function confirmImportNode() {
    importErrorMsg = '';
    importLoading = true;

    try {
      const csrfToken = localStorage.getItem('csrf_token');
      const items = importNodes.map((item) => ({
        link: item.link,
        tag: item.tag.trim()
      }));

      const res = await fetch('/api/outbound/import-bulk', {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          'X-CSRF-Token': csrfToken || ''
        },
        body: JSON.stringify({ items })
      });

      const data = await res.json();
      if (!res.ok) {
        importErrorMsg = data.error || $t('subscr.import_error');
        return;
      }

      showToast('success', $t('subscr.import_success', { count: importNodes.length }));
      showImportModal = false;
      outboundTags = await loadXrayOutboundTags();
    } catch (e: any) {
      importErrorMsg = e.message || $t('subscr.import_error');
    } finally {
      importLoading = false;
    }
  }

  const ru = $derived($currentLang === 'ru');
</script>

<div class="container">
  {#if schemaLoading}
    <div class="loading-state-block" style="padding: 48px; text-align: center; color: var(--fg-secondary);">
      <div class="spinner" style="width: 24px; height: 24px; border: 2px solid var(--accent); border-top-color: transparent; border-radius: 50%; animation: spin 1s linear infinite; margin: 0 auto 12px;"></div>
      <p>{$t('editor.loading_definition')}</p>
    </div>
  {:else if schemaError}
    <div class="error-state-block" style="padding: 48px; text-align: center;">
      <div class="error-icon" style="color: var(--danger); font-size: 24px; margin-bottom: 12px;">⚠</div>
      <p style="color: var(--danger); margin-bottom: 16px;">{$t('editor.definition_load_error', { error: schemaError })}</p>
      <button class="btn btn-secondary" on:click={loadSchema}>{ru ? 'Повторить попытку' : 'Retry'}</button>
    </div>
  {:else}
    {#if !embedded}
      <div class="page-head">
        <div>
          <div class="crumbs">
            {ru ? 'Сервисы' : 'Services'} <span class="crumb-sep">/</span>
            {ru ? 'Пресеты Xray' : 'Xray Presets'}
          </div>
          <h1>{ru ? 'Визуальные пресеты Xray' : 'Xray Visual Presets'}</h1>
          <p class="sub">
            {ru
              ? 'Настройка логирования, DNS, inbounds, outbounds, routing и policy для Xray.'
              : 'Configure logging, DNS, inbounds, outbounds, routing and policy for Xray.'}
          </p>
        </div>
        <div class="ph-actions">
          <button class="btn btn-secondary" on:click={openInEditor}>
            <svg
              width="13"
              height="13"
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              stroke-width="2"
              style="margin-right:5px"
              ><path d="M12 20h9" /><path
                d="M16.5 3.5a2.121 2.121 0 0 1 3 3L7 19l-4 1 1-4L16.5 3.5z"
              /></svg
            >
            {#if selectedFile}
              {ru ? 'Вставить в редактор' : 'Insert into Editor'}
            {:else}
              {ru ? 'Открыть в редакторе' : 'Open in Editor'}
            {/if}
          </button>
          <button
            class="btn btn-primary"
            data-testid="apply-changes-btn"
            on:click={handleApplyChanges}
          >
            {ru ? 'Применить изменения' : 'Apply Changes'}
          </button>
        </div>
      </div>
    {/if}

    <div class="gen-layout">
      <!-- Left Panel -->
      <div class="gen-left">
        <!-- Scenario chips -->
        <div class="constructor-scenario-bar">
          <span class="scenario-label">{$t('editor.constructor_scenario')}:</span>
          {#if schema && schema.xray && schema.xray.presets}
            {#each schema.xray.presets as p}
              <button class="scenario-chip" on:click={() => applyPreset(p.id)}>{$t(p.name)}</button>
            {/each}
          {:else}
            {#each [['selective-routing', $t('editor.scenario_rule_based')], ['all-proxy-routing', $t('editor.scenario_global_proxy')], ['selective-no-quic', ru ? 'Блокировка QUIC' : 'Block QUIC'], ['only-blocked-routing', $t('preset.only-blocked-routing')]] as [id, label]}
              <button class="scenario-chip" on:click={() => applyPreset(id as any)}>{label}</button>
            {/each}
          {/if}
        </div>

      <!-- Outbound Tag selection -->
      <div class="rule-providers-row">
        <label class="form-label" for="proxy-tag-select"
          >{ru ? 'Основной прокси-выход' : 'Main proxy outbound'}:</label
        >
        <select
          id="proxy-tag-select"
          class="form-select"
          bind:value={proxyTag}
          disabled={outboundTagsLoading}
          on:change={() => (isDirty = true)}
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
        </select>
      </div>

      <!-- Section tabs -->
      <div class="sec-tabs" data-testid="xray-section-tabs">
        {#each [['routing', ru ? 'Маршрутизация' : 'Routing'], ['inbounds', ru ? 'Входящие' : 'Inbounds'], ['dns', 'DNS'], ['outbounds', ru ? 'Исходящие' : 'Outbounds'], ['log', ru ? 'Логирование' : 'Log'], ['policy', ru ? 'Политики' : 'Policy']] as [id, label]}
          <button
            class="sec-tab"
            class:active={activeSection === id}
            data-tab={id}
            on:click={() => {
              activeSection = id as any;
              showRuleForm = false;
              showDnsForm = false;
              showInboundForm = false;
            }}
          >
            {label}
            {#if id === 'routing' && routingRules.length > 0}
              <span class="sec-count">{routingRules.length}</span>
            {/if}
          </button>
        {/each}
      </div>

      <!-- ROUTING SECTION -->
      {#if activeSection === 'routing'}
        <div class="sec-body">
          <div class="form-row">
            <label class="form-label" for="domain-strategy"
              >{$t('editor.xray_domain_strategy')}</label
            >
            <select
              id="domain-strategy"
              class="form-select"
              bind:value={routingConfig.domainStrategy}
              on:change={() => (isDirty = true)}
            >
              <option value="AsIs">AsIs</option>
              <option value="IPIfNonMatch">IPIfNonMatch</option>
              <option value="IPOnDemand">IPOnDemand</option>
            </select>
          </div>

          <!-- Filter rules -->
          <div class="form-row" style="margin-bottom: 12px;">
            <label class="form-label" for="rule-filter-select"
              >{ru ? 'Фильтр по исходящему тегу' : 'Filter by outbound tag'}:</label
            >
            <select id="rule-filter-select" class="form-select" bind:value={ruleFilterTag}>
              <option value="">{ru ? 'Все правила' : 'All rules'}</option>
              {#each outboundTags as tag}
                <option value={tag}>{tag}</option>
              {/each}
              <option value="PROXY_TAG">PROXY_TAG</option>
            </select>
          </div>

          <div class="section-title">{$t('editor.xray_routing_rules')}</div>

          <div class="routing-rules-list" data-testid="routing-rules-list">
            {#each filteredRules as rule, idx (rule.id)}
              <div class="card rule-card">
                <div class="rule-header">
                  <span class="badge badge-tag">{rule.outboundTag}</span>
                  <div class="rule-actions">
                    <button
                      class="rule-move"
                      on:click={() => moveRule(rule.id, -1)}
                      disabled={routingRules.findIndex((r) => r.id === rule.id) === 0}>▲</button
                    >
                    <button
                      class="rule-move"
                      on:click={() => moveRule(rule.id, 1)}
                      disabled={routingRules.findIndex((r) => r.id === rule.id) ===
                        routingRules.length - 1}>▼</button
                    >
                    <button class="rule-del" on:click={() => removeRule(rule.id)}>✕</button>
                  </div>
                </div>

                <div class="rule-details">
                  {#if rule.inboundTag && rule.inboundTag.length > 0}
                    <div class="rule-detail-item">
                      <strong>{ru ? 'Входящие теги' : 'Inbound Tags'}:</strong>
                      <span class="rule-chips">
                        {#each rule.inboundTag as ib}
                          <span class="chip chip-ip">{ib}</span>
                        {/each}
                      </span>
                    </div>
                  {/if}

                  {#if rule.domain && rule.domain.length > 0}
                    <div class="rule-detail-item">
                      <strong>{ru ? 'Домены' : 'Domains'}:</strong>
                      <span class="rule-chips">
                        {#each rule.domain as d}
                          <span class="chip chip-domain">{d}</span>
                        {/each}
                      </span>
                    </div>
                  {/if}

                  {#if rule.ip && rule.ip.length > 0}
                    <div class="rule-detail-item">
                      <strong>IP:</strong>
                      <span class="rule-chips">
                        {#each rule.ip as ip}
                          <span class="chip chip-ip">{ip}</span>
                        {/each}
                      </span>
                    </div>
                  {/if}

                  {#if rule.port}
                    <div class="rule-detail-item">
                      <strong>{ru ? 'Порты' : 'Ports'}:</strong> <code>{rule.port}</code>
                    </div>
                  {/if}

                  {#if rule.network}
                    <div class="rule-detail-item">
                      <strong>{ru ? 'Сеть' : 'Network'}:</strong>
                      <span class="badge">{rule.network}</span>
                    </div>
                  {/if}
                </div>
              </div>
            {/each}
          </div>

          {#if showRuleForm}
            <div class="form-card">
              <div class="form-row">
                <label class="form-label" for="rule-outbound"
                  >{$t('editor.xray_outbound_tag')}</label
                >
                <select
                  id="rule-outbound"
                  class="form-select rule-outbound-select"
                  data-testid="rule-outbound-select"
                  bind:value={newRule.outboundTag}
                >
                  {#each outboundTags as tag}
                    <option value={tag}>{tag}</option>
                  {/each}
                  <option value="PROXY_TAG">PROXY_TAG</option>
                </select>
              </div>

              <div class="form-row">
                <label class="form-label" for="rule-inbounds"
                  >{ru ? 'Входящие теги (через запятую)' : 'Inbound tags (comma separated)'}</label
                >
                <input
                  id="rule-inbounds"
                  class="form-input"
                  bind:value={newRule.inboundTagRaw}
                  placeholder="dns-in-ytb, socks"
                />
              </div>

              <div class="form-row">
                <label class="form-label" for="rule-domains"
                  >{$t('editor.xray_domain_list')} ({ru
                    ? 'через запятую'
                    : 'comma separated'})</label
                >
                <input
                  id="rule-domains"
                  class="form-input"
                  data-testid="rule-domain-input"
                  bind:value={newRule.domainRaw}
                  placeholder="geosite:youtube, google.com"
                />
              </div>

              <div class="form-row">
                <label class="form-label" for="rule-ips"
                  >{$t('editor.xray_ip_list')} ({ru ? 'через запятую' : 'comma separated'})</label
                >
                <input
                  id="rule-ips"
                  class="form-input"
                  bind:value={newRule.ipRaw}
                  placeholder="geoip:private, 1.1.1.1"
                />
              </div>

              <div class="form-row2">
                <div class="form-col">
                  <label class="form-label" for="rule-ports">{$t('editor.xray_port_range')}</label>
                  <input
                    id="rule-ports"
                    class="form-input"
                    bind:value={newRule.port}
                    placeholder="80,443,1000-2000"
                  />
                </div>
                <div class="form-col">
                  <label class="form-label" for="rule-network">{$t('editor.xray_network')}</label>
                  <select id="rule-network" class="form-select" bind:value={newRule.network}>
                    <option value="tcp,udp">tcp+udp</option>
                    <option value="tcp">tcp</option>
                    <option value="udp">udp</option>
                  </select>
                </div>
              </div>

              <div class="form-actions">
                <button class="btn btn-secondary" on:click={() => (showRuleForm = false)}
                  >{$t('app.cancel')}</button
                >
                <button class="btn btn-primary" on:click={addRule}>{$t('app.create')}</button>
              </div>
            </div>
          {:else}
            <button
              class="add-btn"
              data-testid="add-routing-rule"
              on:click={() => (showRuleForm = true)}
            >
              + {$t('editor.xray_routing_add_rule')}
            </button>
          {/if}
        </div>
      {/if}

      <!-- INBOUNDS SECTION -->
      {#if activeSection === 'inbounds'}
        <div class="sec-body">
          <div class="section-title">{$t('editor.xray_inbounds')}</div>
          {#each inbounds as inbound}
            <div class="card inbound-card">
              <div class="inbound-title">
                <span class="badge type-{inbound.protocol}">{inbound.protocol}</span>
                <strong>{inbound.tag}</strong>
                <button
                  class="item-del"
                  style="margin-left:auto"
                  on:click={() => removeInbound(inbound.tag)}>✕</button
                >
              </div>
              <div class="form-row2" style="margin-top:var(--spacing-2, 8px)">
                <div class="form-col">
                  <label class="form-label">{ru ? 'Порт входящего' : 'Inbound port'}</label>
                  <input
                    class="form-input"
                    type="number"
                    bind:value={inbound.port}
                    on:input={() => (isDirty = true)}
                    min="1"
                    max="65535"
                  />
                </div>
                <div class="form-col">
                  <label class="form-label">{ru ? 'Адрес прослушивания' : 'Listen address'}</label>
                  <input
                    class="form-input"
                    bind:value={inbound.listen}
                    on:input={() => (isDirty = true)}
                  />
                </div>
              </div>
            </div>
          {/each}

          {#if showInboundForm}
            <div class="form-card">
              <div class="form-row">
                <label class="form-label">{ru ? 'Тег' : 'Tag'}</label>
                <input class="form-input" bind:value={newInbound.tag} placeholder="socks-in" />
              </div>
              <div class="form-row2">
                <div class="form-col">
                  <label class="form-label">{ru ? 'Порт' : 'Port'}</label>
                  <input class="form-input" type="number" bind:value={newInbound.port} />
                </div>
                <div class="form-col">
                  <label class="form-label">{ru ? 'Протокол' : 'Protocol'}</label>
                  <select class="form-select" bind:value={newInbound.protocol}>
                    <option value="socks">socks</option>
                    <option value="http">http</option>
                  </select>
                </div>
              </div>
              {#if newInbound.protocol === 'socks'}
                <div class="form-row">
                  <label class="checkbox-container">
                    <input type="checkbox" bind:checked={newInbound.udp} />
                    <span class="checkmark"></span>
                    Включить UDP в Socks
                  </label>
                </div>
              {/if}
              <div class="form-actions">
                <button class="btn btn-secondary" on:click={() => (showInboundForm = false)}
                  >{$t('app.cancel')}</button
                >
                <button class="btn btn-primary" on:click={addInbound}>{$t('app.create')}</button>
              </div>
            </div>
          {:else}
            <button class="add-btn" on:click={() => (showInboundForm = true)}>
              + {ru ? 'Добавить входящее соединение' : 'Add Inbound'}
            </button>
          {/if}
        </div>
      {/if}

      <!-- DNS SECTION -->
      {#if activeSection === 'dns'}
        <div class="sec-body">
          {#if $capabilities?.xkeen_dns === false && dnsConfig.servers.length > 0}
            <div class="alert alert-warning" style="margin: 0 0 16px 0; display: flex; flex-direction: column; gap: 8px; align-items: flex-start;" role="status">
              <div style="display: flex; gap: 8px; align-items: center;">
                <span aria-hidden="true">⚠️</span>
                <span>{$t('editor.dns_intercept_warning')}</span>
              </div>
              <button class="btn btn-secondary btn-sm" style="font-size: 12px; padding: 4px 8px; display: flex; align-items: center; gap: 4px;" on:click={enableDNSRedirect} disabled={dnsRedirectLoading}>
                {#if dnsRedirectLoading}
                  <span class="spinner" style="display: inline-block; width: 12px; height: 12px; border: 2px solid currentColor; border-top-color: transparent; border-radius: 50%; animation: spin 1s linear infinite;"></span>
                {/if}
                {$t('editor.dns_intercept_enable')}
              </button>
            </div>
          {/if}

          <div class="form-row">
            <label class="form-label" for="dns-query-strategy"
              >{ru ? 'Стратегия запросов DNS' : 'DNS Query Strategy'}</label
            >
            <select
              id="dns-query-strategy"
              class="form-select"
              bind:value={dnsConfig.queryStrategy}
              on:change={() => (isDirty = true)}
            >
              <option value="UseIP">UseIP</option>
              <option value="UseIPv4">UseIPv4</option>
              <option value="UseIPv6">UseIPv6</option>
            </select>
          </div>

          <div class="card" style="margin-top: 16px; margin-bottom: 16px; padding: 12px; display: flex; flex-direction: column; gap: 4px;">
            <label class="checkbox-container" style="margin: 0;">
              <input
                type="checkbox"
                bind:checked={dnsOverVless}
                on:change={() => (isDirty = true)}
              />
              <span class="checkmark" style="top: 1px;"></span>
              <span style="font-weight: 600; color: var(--fg);">{$t('editor.dns_over_vless')}</span>
            </label>
            <div style="font-size: 0.75rem; color: var(--fg-secondary); padding-left: 28px; line-height: 1.4;">
              {$t('editor.dns_over_vless_desc')}
            </div>
          </div>

          <div class="section-title">{$t('editor.xray_dns')}</div>

          <div class="dns-servers-list">
            {#each dnsConfig.servers as srv, idx}
              <div class="item-row card" style="margin-bottom: 8px;">
                {#if typeof srv === 'string'}
                  <span class="item-name">{srv}</span>
                {:else}
                  <div style="flex: 1;">
                    <div style="font-weight: 600; color: var(--fg);">
                      {srv.address}:{srv.port || 53}
                    </div>
                    <div style="font-size: 0.75rem; color: var(--fg-secondary);">
                      Tag: <span class="badge">{srv.tag}</span> | Domains: {srv.domains?.join(
                        ', '
                      ) || 'none'}
                      {#if srv.skipFallback}
                        | <span class="badge">Skip Fallback</span>{/if}
                    </div>
                  </div>
                {/if}
                <button class="item-del" on:click={() => removeDNSServer(idx)}>✕</button>
              </div>
            {/each}
          </div>

          {#if showDnsForm}
            <div class="form-card">
              <div class="form-row">
                <label class="form-label">{ru ? 'Адрес сервера' : 'Server address'}</label>
                <input class="form-input" bind:value={newDns.address} placeholder="8.8.8.8" />
              </div>
              <div class="form-row2">
                <div class="form-col">
                  <label class="form-label">{ru ? 'Порт' : 'Port'}</label>
                  <input class="form-input" type="number" bind:value={newDns.port} />
                </div>
                <div class="form-col">
                  <label class="form-label">{ru ? 'Тег (опционально)' : 'Tag (optional)'}</label>
                  <input class="form-input" bind:value={newDns.tag} placeholder="dns-in-ytb" />
                </div>
              </div>
              {#if newDns.tag.trim()}
                <div class="form-row">
                  <label class="form-label"
                    >{ru ? 'Домены для перенаправления' : 'Domains for redirect'}</label
                  >
                  <input
                    class="form-input"
                    bind:value={newDns.domainsRaw}
                    placeholder="geosite:youtube, google.com"
                  />
                </div>
                <div class="form-row" style="margin-top: 8px;">
                  <label class="checkbox-container">
                    <input type="checkbox" bind:checked={newDns.skipFallback} />
                    <span class="checkmark"></span>
                    Skip Fallback
                  </label>
                </div>
              {/if}
              <div class="form-actions">
                <button class="btn btn-secondary" on:click={() => (showDnsForm = false)}
                  >{$t('app.cancel')}</button
                >
                <button class="btn btn-primary" on:click={addDNSServer}>{$t('app.create')}</button>
              </div>
            </div>
          {:else}
            <button class="add-btn" on:click={() => (showDnsForm = true)}>
              + {ru ? 'Добавить DNS-сервер' : 'Add DNS Server'}
            </button>
          {/if}

          <div class="section-title" style="margin-top: 16px;">Hosts</div>
          <div class="hosts-list">
            {#each Object.entries(dnsConfig.hosts) as [domain, ip]}
              <div class="item-row card" style="margin-bottom: 8px;">
                <div style="flex: 1;">
                  <code>{domain}</code> &rarr; <code>{ip}</code>
                </div>
                <button class="item-del" on:click={() => removeHost(domain)}>✕</button>
              </div>
            {/each}
          </div>

          {#if showHostForm}
            <div class="form-card" style="margin-top: 8px;">
              <div class="form-row2">
                <div class="form-col">
                  <label class="form-label">{ru ? 'Домен' : 'Domain'}</label>
                  <input class="form-input" bind:value={newHost.domain} placeholder="dns.google" />
                </div>
                <div class="form-col">
                  <label class="form-label">IP</label>
                  <input class="form-input" bind:value={newHost.ip} placeholder="8.8.8.8" />
                </div>
              </div>
              <div class="form-actions">
                <button class="btn btn-secondary" on:click={() => (showHostForm = false)}
                  >{$t('app.cancel')}</button
                >
                <button class="btn btn-primary" on:click={addHost}>{$t('app.create')}</button>
              </div>
            </div>
          {:else}
            <button class="add-btn" style="margin-top: 8px;" on:click={() => (showHostForm = true)}>
              + {ru ? 'Добавить Host' : 'Add Host'}
            </button>
          {/if}
        </div>
      {/if}

      <!-- OUTBOUNDS SECTION -->
      {#if activeSection === 'outbounds'}
        <div class="sec-body">
          <div
            style="display: flex; justify-content: space-between; align-items: center; margin-bottom: 12px;"
          >
            <div class="section-title" style="margin: 0;">
              {$t('editor.xray_section_outbounds')}
            </div>
            <button
              class="btn btn-secondary"
              on:click={openImportModal}
              style="padding: 4px 10px; font-size: 12px; display: flex; align-items: center; gap: 4px;"
            >
              <svg
                width="14"
                height="14"
                viewBox="0 0 24 24"
                fill="none"
                stroke="currentColor"
                stroke-width="2"
              >
                <path
                  d="M4 14.899A7 7 0 1 1 15.71 8h1.79a4.5 4.5 0 0 1 2.5 8.242M12 12V22M12 12L15 15M12 12L9 15"
                />
              </svg>
              {$t('subscr.import_node')}
            </button>
          </div>
          <p class="section-desc">
            {ru ? 'Доступные исходящие теги (read-only):' : 'Available outbound tags (read-only):'}
          </p>
          <div class="outbounds-list">
            {#each outboundDetails as item}
              <div
                class="card tag-card"
                style="margin-bottom: 8px; padding: 12px; display: flex; align-items: center; justify-content: space-between;"
              >
                <div>
                  <span class="badge badge-tag">{item.tag}</span>
                  {#if item.server}
                    <span style="font-size: 0.75rem; color: var(--fg-secondary); margin-left: 8px;">
                      ({item.protocol} &bull; {item.server})
                    </span>
                  {/if}
                </div>
                <span class="tag-desc" style="font-size: 0.8125rem; color: var(--fg-secondary);">
                  {item.tag === 'direct'
                    ? ru
                      ? 'Прямое подключение (freedom)'
                      : 'Direct connection (freedom)'
                    : ''}
                  {item.tag === 'block'
                    ? ru
                      ? 'Блокировка трафика (blackhole)'
                      : 'Block traffic (blackhole)'
                    : ''}
                  {item.tag === 'dns-out' ? (ru ? 'Запросы DNS' : 'DNS requests') : ''}
                  {item.tag !== 'direct' && item.tag !== 'block' && item.tag !== 'dns-out'
                    ? ru
                      ? 'Пользовательский outbound'
                      : 'Custom outbound'
                    : ''}
                </span>
              </div>
            {/each}
          </div>
        </div>
      {/if}

      <!-- LOG SECTION -->
      {#if activeSection === 'log'}
        <div class="sec-body">
          <div class="section-title">{$t('editor.xray_section_log')}</div>

          <div class="form-row">
            <label class="form-label" for="log-level"
              >{ru ? 'Уровень логирования' : 'Loglevel'}</label
            >
            <select
              id="log-level"
              class="form-select"
              bind:value={logConfig.loglevel}
              on:change={() => (isDirty = true)}
            >
              <option value="none">none</option>
              <option value="error">error</option>
              <option value="warning">warning</option>
              <option value="info">info</option>
              <option value="debug">debug</option>
            </select>
          </div>

          <div class="form-row" style="margin-top: 8px;">
            <label class="checkbox-container">
              <input
                type="checkbox"
                bind:checked={logConfig.dnsLog}
                on:change={() => (isDirty = true)}
              />
              <span class="checkmark"></span>
              {ru ? 'Включить логирование DNS' : 'Enable DNS Logging'}
            </label>
          </div>

          {#if xrayFiles['01_log.json']?.log?.access || xrayFiles['01_log.json']?.log?.error}
            <div class="logs-paths card" style="margin-top: 12px; padding: 12px;">
              <h4 style="margin: 0 0 8px 0; font-size: 0.875rem;">
                {ru ? 'Пути к логам:' : 'Logs paths:'}
              </h4>
              {#if xrayFiles['01_log.json']?.log?.access}
                <div style="font-size: 0.8125rem;">
                  <strong>Access:</strong> <code>{xrayFiles['01_log.json'].log.access}</code>
                </div>
              {/if}
              {#if xrayFiles['01_log.json']?.log?.error}
                <div style="font-size: 0.8125rem; margin-top: 4px;">
                  <strong>Error:</strong> <code>{xrayFiles['01_log.json'].log.error}</code>
                </div>
              {/if}
            </div>
          {/if}
        </div>
      {/if}

      <!-- POLICY SECTION -->
      {#if activeSection === 'policy'}
        <div class="sec-body">
          <div class="section-title">{$t('editor.xray_section_policy')}</div>

          <div class="card" style="padding: 16px;">
            <h4 style="margin: 0 0 12px 0;">Level 0 (Default)</h4>
            <div class="form-row2">
              <div class="form-col">
                <label class="form-label" for="policy-handshake">Handshake</label>
                <input
                  id="policy-handshake"
                  class="form-input"
                  type="number"
                  bind:value={policyConfig.levels['0'].handshake}
                  on:input={() => (isDirty = true)}
                />
              </div>
              <div class="form-col">
                <label class="form-label" for="policy-connidle">ConnIdle</label>
                <input
                  id="policy-connidle"
                  class="form-input"
                  type="number"
                  bind:value={policyConfig.levels['0'].connIdle}
                  on:input={() => (isDirty = true)}
                />
              </div>
            </div>
            <div class="form-row2" style="margin-top: 12px;">
              <div class="form-col">
                <label class="form-label" for="policy-uplink">UplinkOnly</label>
                <input
                  id="policy-uplink"
                  class="form-input"
                  type="number"
                  bind:value={policyConfig.levels['0'].uplinkOnly}
                  on:input={() => (isDirty = true)}
                />
              </div>
              <div class="form-col">
                <label class="form-label" for="policy-downlink">DownlinkOnly</label>
                <input
                  id="policy-downlink"
                  class="form-input"
                  type="number"
                  bind:value={policyConfig.levels['0'].downlinkOnly}
                  on:input={() => (isDirty = true)}
                />
              </div>
            </div>
          </div>

          <div class="card" style="padding: 16px; margin-top: 12px;">
            <h4 style="margin: 0 0 12px 0;">System</h4>
            <div class="form-row">
              <label class="checkbox-container">
                <input
                  type="checkbox"
                  bind:checked={policyConfig.system.statsInboundUplink}
                  on:change={() => (isDirty = true)}
                />
                <span class="checkmark"></span>
                Stats Inbound Uplink
              </label>
            </div>
            <div class="form-row" style="margin-top: 8px;">
              <label class="checkbox-container">
                <input
                  type="checkbox"
                  bind:checked={policyConfig.system.statsInboundDownlink}
                  on:change={() => (isDirty = true)}
                />
                <span class="checkmark"></span>
                Stats Inbound Downlink
              </label>
            </div>
          </div>
        </div>
      {/if}
    </div>

    <!-- Right Panel (Preview) -->
    <div class="gen-right">
      <div class="preview-header">
        <span class="preview-title">JSON {ru ? 'превью' : 'preview'}</span>
      </div>
      <pre class="constructor-preview-pane" data-testid="xray-json-preview">{previewJson}</pre>

      {#if validationError}
        <div class="validation-error-block" style="margin-top: 12px; padding: 12px; background: rgba(239, 91, 107, 0.1); border: 1px solid var(--danger); border-radius: var(--radius-md); color: var(--danger); font-size: 13px;">
          <div style="font-weight: bold; margin-bottom: 6px;">{$t('editor.validation_failed')}</div>
          <div style="white-space: pre-wrap; font-family: var(--font-family-mono); font-size: 13px; margin-bottom: 8px;">
            {parseValidationError(validationError, ru ? 'ru' : 'en')}
          </div>
          <details>
            <summary style="cursor: pointer; font-size: 12px; opacity: 0.8; user-select: none;">{$t('editor.validation_details')}</summary>
            <pre style="margin: 6px 0 0 0; white-space: pre-wrap; font-family: var(--font-family-mono); font-size: 12px; opacity: 0.9; max-height: 200px; overflow-y: auto;">{validationError}</pre>
          </details>
        </div>
      {/if}

      {#if embedded}
        <div class="gen-embedded-actions" style="margin-top: 12px; display: flex; gap: 8px;">
          <button class="btn btn-secondary" style="flex: 1;" on:click={openInEditor}>
            <svg
              width="13"
              height="13"
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              stroke-width="2"
              style="margin-right:5px"
              ><path d="M12 20h9" /><path
                d="M16.5 3.5a2.121 2.121 0 0 1 3 3L7 19l-4 1 1-4L16.5 3.5z"
              /></svg
            >
            {#if selectedFile}
              {ru ? 'Вставить в редактор' : 'Insert into Editor'}
            {:else}
              {ru ? 'Открыть в редакторе' : 'Open in Editor'}
            {/if}
          </button>
          <button
            class="btn btn-primary"
            data-testid="apply-changes-btn"
            on:click={handleApplyChanges}
            style="flex: 1;"
          >
            {ru ? 'Применить изменения' : 'Apply Changes'}
          </button>
        </div>
      {/if}
    </div>
  </div>
  {/if}
</div>

{#if showApplyConfirm}
  <div
    class="modal-overlay"
    role="button"
    tabindex="0"
    data-testid="apply-confirm-dialog"
    on:click={() => (showApplyConfirm = false)}
    on:keydown={(e) => e.key === 'Escape' && (showApplyConfirm = false)}
  >
    <div class="modal-card" role="presentation" on:click|stopPropagation>
      <div class="modal-card-header">
        <h2>{$t('editor.apply_confirm_title')}</h2>
        <button class="modal-close-btn" on:click={() => (showApplyConfirm = false)}>&times;</button>
      </div>
      <div class="modal-card-body">
        <p>{$t('editor.apply_confirm_body')}</p>
        <div class="changed-files-list" style="margin-top: 12px;">
          <strong>{ru ? 'Будут изменены файлы:' : 'Files to be modified:'}</strong>
          <ul style="margin: 8px 0 0 0; padding-left: 20px;">
            {#each filesToModify as file}
              {#if file.changesCount > 0}
                <li>
                  <code>{file.name}</code>:
                  <span
                    class="badge"
                    style="background-color: var(--color-warning-bg); color: var(--color-warning-fg);"
                  >
                    {ru
                      ? `Изменено ${file.changesCount} секций`
                      : `Modified ${file.changesCount} sections`}
                  </span>
                </li>
              {:else}
                <li><code>{file.name}</code>: {ru ? 'Без изменений' : 'No changes'}</li>
              {/if}
            {/each}
          </ul>
          <p style="margin-top: 12px; font-size: 0.8125rem; color: var(--fg-secondary);">
            {ru
              ? '* Автоматически будет создана резервная копия (хранится до 5 последних бэкапов)'
              : '* A backup will be created automatically (up to 5 copies stored)'}
          </p>
        </div>
      </div>
      <div class="modal-card-footer">
        <button class="btn btn-secondary" on:click={() => (showApplyConfirm = false)}>
          {$t('app.cancel')}
        </button>
        <button class="btn btn-primary" on:click={handleApplyChanges} disabled={applyLoading}>
          {applyLoading ? $t('editor.saving') : $t('editor.apply_and_restart')}
        </button>
      </div>
    </div>
  </div>
{/if}

{#if showImportModal}
  <div
    class="modal-overlay"
    role="button"
    tabindex="0"
    on:click={closeImportModal}
    on:keydown={(e) => e.key === 'Escape' && closeImportModal()}
  >
    <div class="modal-card" role="presentation" on:click|stopPropagation>
      <div class="modal-card-header">
        <h2>{$t('subscr.import_modal_title')}</h2>
        <button class="modal-close-btn" on:click={closeImportModal}>&times;</button>
      </div>
      <div class="modal-card-body">
        {#if importErrorMsg}
          <div
            class="error-msg"
            style="color: var(--danger); margin-bottom: 12px; font-size: 13px;"
          >
            {importErrorMsg}
          </div>
        {/if}

        {#if importStep === 1}
          <div class="form-group">
            <label for="import-link" class="form-label">{$t('subscr.import_link_label')}</label>
            <textarea
              id="import-link"
              class="input textarea-link"
              bind:value={importLink}
              placeholder={$t('subscr.import_link_placeholder')}
              rows="4"
              style="resize: none; font-family: var(--font-family-mono, monospace); font-size: 12px; width: 100%; box-sizing: border-box; background: var(--bg-surface-hover); border: 1px solid var(--border); border-radius: var(--radius-sm, 4px); padding: 8px; color: var(--fg);"
            ></textarea>
          </div>
        {:else if importStep === 2 && importNodes.length > 0}
          <div class="preview-section">
            <h3 class="preview-title" style="margin: 0 0 12px 0; font-size: 14px;">
              {$t('subscr.import_preview_title')}
            </h3>
            <div
              class="preview-list"
              style="max-height: 260px; overflow-y: auto; display: flex; flex-direction: column; gap: 10px; padding-right: 4px; scrollbar-width: thin;"
            >
              {#each importNodes as item, idx}
                {#if item.rowError}
                  <div
                    class="preview-item-card"
                    style="background: var(--bg-card); border: 1px solid var(--danger); border-radius: var(--radius-sm, 4px); padding: 10px; display: flex; flex-direction: column; gap: 8px; position: relative;"
                  >
                    <button
                      type="button"
                      on:click={() => (importNodes = importNodes.filter((_, i) => i !== idx))}
                      style="position: absolute; right: 10px; top: 10px; background: none; border: 0; color: var(--fg-secondary); cursor: pointer; font-size: 12px;"
                      aria-label="Remove">✕</button
                    >
                    <div style="font-size: 12px; color: var(--danger); padding-right: 20px;">
                      <strong>{$t('app.error')}:</strong>
                      {item.rowError}
                    </div>
                    <div
                      style="font-size: 11px; color: var(--fg-secondary); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; padding-right: 20px;"
                      title={item.link}
                    >
                      {item.link}
                    </div>
                  </div>
                {:else}
                  <div
                    class="preview-item-card"
                    style="background: var(--bg-card); border: 1px solid var(--border); border-radius: var(--radius-sm, 4px); padding: 10px; display: flex; flex-direction: column; gap: 8px; position: relative;"
                  >
                    <button
                      type="button"
                      on:click={() => (importNodes = importNodes.filter((_, i) => i !== idx))}
                      style="position: absolute; right: 10px; top: 10px; background: none; border: 0; color: var(--fg-secondary); cursor: pointer; font-size: 12px;"
                      aria-label="Remove">✕</button
                    >
                    <div
                      style="display: flex; justify-content: space-between; font-size: 12px; color: var(--fg-secondary); padding-right: 20px;"
                    >
                      <span
                        ><strong style="color: var(--fg);">{item.outbound?.protocol}</strong> · {getNodeServer(
                          item.outbound
                        )}:{getNodePort(item.outbound)}</span
                      >
                    </div>
                    <div style="display: flex; align-items: center; gap: 8px;">
                      <label
                        class="form-label"
                        style="margin: 0; font-size: 12px; flex-shrink: 0;"
                        for="import-tag-{idx}">{$t('subscr.import_tag_custom')}:</label
                      >
                      <input
                        id="import-tag-{idx}"
                        type="text"
                        class="input"
                        bind:value={item.tag}
                        style="flex-grow: 1; font-size: 12px; box-sizing: border-box; background: var(--bg-surface-hover); border: 1px solid var(--border); border-radius: var(--radius-sm, 4px); padding: 4px 8px; color: var(--fg); width: auto;"
                      />
                    </div>
                  </div>
                {/if}
              {/each}
            </div>
          </div>
        {/if}
      </div>
      <div class="modal-card-footer">
        <button class="btn btn-secondary" on:click={closeImportModal} disabled={importLoading}>
          {$t('app.cancel')}
        </button>
        {#if importStep === 1}
          <button
            class="btn btn-primary"
            on:click={parseImportLink}
            disabled={!importLink.trim() || importLoading}
          >
            {#if importLoading}
              <span class="spinner-xs" style="margin-right: 6px;"></span>
            {/if}
            {$t('subscr.import_btn_parse')}
          </button>
        {:else}
          <button
            class="btn btn-primary"
            on:click={confirmImportNode}
            disabled={importLoading ||
              importNodes.length === 0 ||
              importNodes.some((n) => n.rowError)}
          >
            {#if importLoading}
              <span class="spinner-xs" style="margin-right: 6px;"></span>
            {/if}
            {ru ? `Импортировать (${importNodes.length})` : `Import (${importNodes.length})`}
          </button>
        {/if}
      </div>
    </div>
  </div>
{/if}

<style>
  .container {
    display: flex;
    flex-direction: column;
    height: 100%;
  }

  .crumbs {
    font-size: var(--font-size-xs, 0.75rem);
    color: var(--fg-secondary);
    margin-bottom: 4px;
  }
  .crumb-sep {
    margin: 0 4px;
  }
  h1 {
    font-size: 1.5rem;
    font-weight: 600;
    margin: 0 0 4px 0;
    color: var(--fg);
  }
  .sub {
    color: var(--fg-secondary);
    font-size: var(--font-size-sm, 0.8125rem);
    margin: 0 0 20px 0;
  }

  .page-head {
    display: flex;
    justify-content: space-between;
    align-items: flex-start;
    margin-bottom: var(--spacing-4, 16px);
  }

  .ph-actions {
    display: flex;
    gap: var(--spacing-2, 8px);
  }

  .gen-layout {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: var(--spacing-4, 16px);
    align-items: start;
  }

  @media (max-width: 1024px) {
    .gen-layout {
      grid-template-columns: 1fr;
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
    background: var(--bg-surface-hover);
    border: 1px solid var(--border);
    border-radius: 12px;
    color: var(--fg);
    font-size: 0.75rem;
    cursor: pointer;
    transition: background-color var(--transition-fast);
  }

  .scenario-chip:hover {
    background: var(--bg-surface-active);
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
    border-bottom: 1px solid var(--border);
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
    color: var(--accent);
    border-bottom-color: var(--accent);
    font-weight: 500;
  }

  .sec-count {
    background: var(--bg-surface-hover, rgba(255, 255, 255, 0.1));
    color: var(--fg);
    font-size: 0.6875rem;
    padding: 1px 5px;
    border-radius: 10px;
    font-weight: 600;
  }

  .sec-body {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-4, 16px);
  }

  .section-title {
    font-size: 0.875rem;
    font-weight: 600;
    color: var(--fg);
    margin-bottom: var(--spacing-2, 8px);
  }

  .routing-rules-list {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-2, 8px);
    max-height: 400px;
    overflow-y: auto;
    scrollbar-width: thin;
  }

  .rule-card {
    padding: var(--spacing-3, 12px);
    background: var(--bg-surface);
    border: 1px solid var(--border);
    border-radius: var(--radius-md, 6px);
  }

  .rule-header {
    display: flex;
    justify-content: space-between;
    align-items: center;
    margin-bottom: 8px;
  }

  .badge-tag {
    background: var(--bg-surface-hover);
    color: var(--fg);
    font-weight: 500;
    padding: 2px 6px;
    border-radius: 4px;
    font-size: 0.75rem;
  }

  .rule-actions {
    display: flex;
    gap: 4px;
  }

  .rule-move,
  .rule-del {
    background: transparent;
    border: none;
    color: var(--fg-secondary);
    width: 20px;
    height: 20px;
    display: flex;
    align-items: center;
    justify-content: center;
    font-size: 0.6875rem;
    cursor: pointer;
    border-radius: 4px;
  }

  .rule-move:hover,
  .rule-del:hover {
    background: var(--bg-surface-hover);
    color: var(--fg);
  }

  .rule-move:disabled {
    opacity: 0.3;
    cursor: not-allowed;
  }

  .rule-details {
    display: flex;
    flex-direction: column;
    gap: 6px;
    font-size: var(--font-size-sm, 0.8125rem);
  }

  .rule-detail-item {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 6px;
  }

  .rule-chips {
    display: flex;
    flex-wrap: wrap;
    gap: 4px;
  }

  .chip {
    padding: 1px 6px;
    border-radius: 4px;
    font-size: 0.6875rem;
    font-weight: 500;
  }

  .chip-domain {
    background: rgba(13, 110, 253, 0.15);
    color: #0d6efd;
  }

  .chip-ip {
    background: rgba(25, 135, 84, 0.15);
    color: #198754;
  }

  .form-card {
    background: var(--bg-surface);
    border: 1px solid var(--border);
    border-radius: var(--radius-md, 6px);
    padding: var(--spacing-4, 16px);
    display: flex;
    flex-direction: column;
    gap: var(--spacing-3, 12px);
  }

  .form-row {
    display: flex;
    flex-direction: column;
    gap: 6px;
  }

  .form-row2 {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: 12px;
  }

  .form-col {
    display: flex;
    flex-direction: column;
    gap: 6px;
  }

  .form-label {
    font-size: var(--font-size-sm, 0.8125rem);
    color: var(--fg-secondary);
    font-weight: 500;
  }

  .form-input,
  .form-select {
    padding: 8px 12px;
    background: var(--bg-surface);
    border: 1px solid var(--border);
    border-radius: var(--radius-md, 6px);
    color: var(--fg);
    font-size: var(--font-size-sm, 0.8125rem);
    font-family: inherit;
    outline: none;
    transition: border-color var(--transition-fast);
  }

  .form-input:focus,
  .form-select:focus {
    border-color: var(--accent);
  }

  .input-with-btn {
    display: flex;
    gap: 8px;
  }
  .input-with-btn .form-input {
    flex: 1;
  }

  .form-actions {
    display: flex;
    justify-content: flex-end;
    gap: 8px;
    margin-top: 8px;
  }

  .btn {
    padding: 8px 16px;
    border-radius: var(--radius-md, 6px);
    font-size: var(--font-size-sm, 0.8125rem);
    font-weight: 500;
    cursor: pointer;
    border: none;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    transition: background-color var(--transition-fast);
  }

  .btn-primary {
    background: var(--accent);
    color: #fff;
  }
  .btn-primary:hover {
    background: var(--accent-hover, #0056b3);
  }

  .btn-secondary {
    background: var(--bg-surface-hover);
    color: var(--fg);
    border: 1px solid var(--border);
  }
  .btn-secondary:hover {
    background: var(--bg-surface-active);
  }

  .btn-secondary:disabled {
    opacity: 0.5;
    cursor: not-allowed;
  }

  .add-btn {
    width: 100%;
    padding: var(--spacing-3, 12px);
    background: transparent;
    border: 1px dashed var(--border);
    color: var(--fg-secondary);
    border-radius: var(--radius-md, 6px);
    cursor: pointer;
    transition:
      border-color var(--transition-fast),
      color var(--transition-fast);
    font-size: var(--font-size-sm, 0.8125rem);
  }

  .add-btn:hover {
    border-color: var(--accent);
    color: var(--accent);
  }

  .inbound-card {
    padding: var(--spacing-4, 16px);
    background: var(--bg-surface);
    border: 1px solid var(--border);
    border-radius: var(--radius-md, 6px);
  }

  .inbound-title {
    display: flex;
    align-items: center;
    gap: 8px;
    font-size: 0.875rem;
  }

  .type-socks {
    background: rgba(13, 110, 253, 0.15);
    color: #0d6efd;
  }

  .type-http {
    background: rgba(111, 66, 193, 0.15);
    color: #6f42c1;
  }

  .dns-servers-list {
    display: flex;
    flex-direction: column;
    gap: 4px;
  }

  .item-row {
    display: flex;
    justify-content: space-between;
    align-items: center;
    padding: 8px 12px;
    background: var(--bg-surface);
    border: 1px solid var(--border);
    border-radius: var(--radius-md, 6px);
  }

  .item-name {
    font-size: var(--font-size-sm, 0.8125rem);
    color: var(--fg);
  }

  .item-del {
    background: transparent;
    border: none;
    color: var(--fg-secondary);
    cursor: pointer;
    padding: 0 4px;
  }
  .item-del:hover {
    color: var(--fg);
  }

  .gen-right {
    display: flex;
    flex-direction: column;
    height: 100%;
    min-height: 450px;
  }

  .preview-header {
    padding: 8px 12px;
    background: var(--bg-surface);
    border: 1px solid var(--border);
    border-bottom: none;
    border-radius: var(--radius-md, 6px) var(--radius-md, 6px) 0 0;
  }

  .preview-title {
    font-size: var(--font-size-xs, 0.75rem);
    color: var(--fg-secondary);
    font-weight: 600;
    text-transform: uppercase;
  }

  .constructor-preview-pane {
    flex: 1;
    margin: 0;
    padding: var(--spacing-4, 16px);
    background: #1e1e1e;
    color: #d4d4d4;
    border: 1px solid var(--border);
    border-radius: 0 0 var(--radius-md, 6px) var(--radius-md, 6px);
    font-family: var(--font-mono, monospace);
    font-size: var(--font-size-xs, 0.75rem);
    line-height: 1.5;
    overflow: auto;
    scrollbar-width: thin;
    max-height: 500px;
  }

  .checkbox-container {
    display: block;
    position: relative;
    padding-left: 28px;
    cursor: pointer;
    font-size: var(--font-size-sm, 0.8125rem);
    user-select: none;
    color: var(--fg);
  }

  .checkbox-container input {
    position: absolute;
    opacity: 0;
    cursor: pointer;
    height: 0;
    width: 0;
  }

  .checkmark {
    position: absolute;
    top: 2px;
    left: 0;
    height: 16px;
    width: 16px;
    background-color: var(--bg-surface-hover);
    border: 1px solid var(--border);
    border-radius: 3px;
  }

  .checkbox-container:hover input ~ .checkmark {
    background-color: var(--bg-surface-active);
  }

  .checkbox-container input:checked ~ .checkmark {
    background-color: var(--accent);
    border-color: var(--accent);
  }

  .checkmark:after {
    content: '';
    position: absolute;
    display: none;
  }

  .checkbox-container input:checked ~ .checkmark:after {
    display: block;
  }

  .checkbox-container .checkmark:after {
    left: 5px;
    top: 2px;
    width: 4px;
    height: 8px;
    border: solid white;
    border-width: 0 2px 2px 0;
    transform: rotate(45deg);
  }

  /* Modal styles */
  .modal-overlay {
    position: fixed;
    top: 0;
    left: 0;
    right: 0;
    bottom: 0;
    background: rgba(0, 0, 0, 0.5);
    display: flex;
    align-items: center;
    justify-content: center;
    z-index: 1000;
  }

  .modal-card {
    background: var(--bg-surface);
    border: 1px solid var(--border);
    border-radius: var(--radius-lg, 8px);
    width: 500px;
    max-width: 90%;
    display: flex;
    flex-direction: column;
    box-shadow: 0 4px 20px rgba(0, 0, 0, 0.25);
  }

  .modal-card-header {
    padding: 16px;
    border-bottom: 1px solid var(--border);
    display: flex;
    justify-content: space-between;
    align-items: center;
  }

  .modal-card-header h2 {
    margin: 0;
    font-size: 1.125rem;
    font-weight: 600;
    color: var(--fg);
  }

  .modal-close-btn {
    background: transparent;
    border: none;
    font-size: 1.5rem;
    color: var(--fg-secondary);
    cursor: pointer;
  }

  .modal-card-body {
    padding: 16px;
    font-size: var(--font-size-sm, 0.8125rem);
    color: var(--fg);
    max-height: 400px;
    overflow-y: auto;
  }

  .modal-card-footer {
    padding: 16px;
    border-top: 1px solid var(--border);
    display: flex;
    justify-content: flex-end;
    gap: 8px;
  }
</style>
