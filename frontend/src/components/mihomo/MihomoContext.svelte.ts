import { getContext, setContext } from 'svelte';
import {
  generateYAML as generateMihomoYAML,
  type Proxy,
  type ProxyGroup,
  type Rule,
  type DNSConfig,
  type TUNConfig,
  type SnifferConfig,
  type Listener,
  type RuleProvider,
  ZKEEN_RULE_PROVIDERS
} from '../../lib/mihomoYaml';
import { ZKEEN_16_GROUPS_TEMPLATE } from '../../lib/constructors/presets/mihomoPresets';

export type ActiveMihomoSection =
  'proxies' | 'groups' | 'rules' | 'dns' | 'tun' | 'rulesets' | 'listeners';

export class MihomoContext {
  activeSection = $state<ActiveMihomoSection>('proxies');
  proxies = $state<Proxy[]>([]);
  groups = $state<ProxyGroup[]>([]);
  rules = $state<Rule[]>([]);
  listeners = $state<Listener[]>([]);
  listenersRaw = $state<string | null>(null);
  listenersReadOnly = $state(false);

  dns = $state<DNSConfig>({
    enabled: false,
    nameservers: ['https://doh.pub/dns-query', '223.5.5.5'],
    fallback: ['https://8.8.8.8/dns-query', '1.1.1.1'],
    enhancedMode: 'fake-ip',
    fakeIPRange: '198.18.0.1/16'
  });

  tun = $state<TUNConfig>({
    enabled: false,
    stack: 'mixed',
    autoRoute: true,
    autoDetectInterface: true,
    dnsHijack: ['any:53']
  });

  sniffer = $state<SnifferConfig>({
    enabled: false,
    sniffHttp: true,
    sniffTls: true,
    sniffQuic: true
  });

  activePreset = $state<string>('');
  activeRuleProvider = $state<'none' | 'zkeen' | 'metacubex'>('none');
  ruleProviders = $state<RuleProvider[]>([]);
  externalControllerType = $state<'unix' | 'tcp'>('unix');
  externalControllerTarget = $state<string>('127.0.0.1:9090');

  subscriptions = $state<any[]>([]);
  mihomoProviders = $state<any[]>([]);
  lastParsedProviders = $state<any[]>([]);

  preservedKeys = $state<string[]>([]);
  safeMergeEnabled = $state(true);
  safeMergeExpanded = $state(false);

  hasZkeenGeodata = $state(false);
  existingTproxyPort = $state<number | null>(null);
  existingRedirPort = $state<number | null>(null);

  isDirty = $state(false);
  previewWidth = $state(440);
  showPreviewPane = $state(true);

  markDirty(): void {
    this.isDirty = true;
  }

  resetDirty(): void {
    this.isDirty = false;
  }

  // Proxies
  addProxy(proxy: Proxy): void {
    this.proxies = [...this.proxies, proxy];
    this.markDirty();
  }

  updateProxy(id: string, partial: Partial<Proxy>): void {
    this.proxies = this.proxies.map((p) => (p.id === id ? ({ ...p, ...partial } as Proxy) : p));
    this.markDirty();
  }

  removeProxy(id: string): void {
    const target = this.proxies.find((p) => p.id === id);
    const targetName = target ? target.name : null;

    this.proxies = this.proxies.filter((p) => p.id !== id);

    if (targetName) {
      this.groups = this.groups.map((g) => ({
        ...g,
        proxies: (g.proxies || []).filter((name) => name !== targetName)
      }));
    }
    this.markDirty();
  }

  duplicateProxy(id: string): Proxy | null {
    const target = this.proxies.find((p) => p.id === id);
    if (!target) return null;

    const copy: Proxy = {
      ...target,
      id: crypto.randomUUID(),
      name: `${target.name} (copy)`
    };
    this.proxies = [...this.proxies, copy];
    this.markDirty();
    return copy;
  }

  toggleProxy(id: string): void {
    this.proxies = this.proxies.map((p) =>
      p.id === id ? { ...p, enabled: !(p.enabled ?? true) } : p
    );
    this.markDirty();
  }

  // Groups
  addGroup(group: ProxyGroup): void {
    this.groups = [...this.groups, group];
    this.markDirty();
  }

  updateGroup(id: string, partial: Partial<ProxyGroup>): void {
    this.groups = this.groups.map((g) => (g.id === id ? ({ ...g, ...partial } as ProxyGroup) : g));
    this.markDirty();
  }

  removeGroup(id: string): void {
    this.groups = this.groups.filter((g) => g.id !== id);
    this.markDirty();
  }

  // Rules
  addRule(rule: Rule): void {
    this.rules = [...this.rules, rule];
    this.markDirty();
  }

  updateRule(id: string, partial: Partial<Rule>): void {
    this.rules = this.rules.map((r) => (r.id === id ? ({ ...r, ...partial } as Rule) : r));
    this.markDirty();
  }

  removeRule(id: string): void {
    this.rules = this.rules.filter((r) => r.id !== id);
    this.markDirty();
  }

  moveRule(fromIndex: number, toIndex: number): void {
    if (
      fromIndex < 0 ||
      fromIndex >= this.rules.length ||
      toIndex < 0 ||
      toIndex >= this.rules.length
    ) {
      return;
    }
    const item = this.rules[fromIndex];
    const newRules = [...this.rules];
    newRules.splice(fromIndex, 1);
    newRules.splice(toIndex, 0, item);
    this.rules = newRules;
    this.markDirty();
  }

  // Listeners
  addListener(listener: Listener): void {
    this.listeners = [...this.listeners, listener];
    this.markDirty();
  }

  updateListener(id: string, partial: Partial<Listener>): void {
    this.listeners = this.listeners.map((l) => (l.id === id ? { ...l, ...partial } : l));
    this.markDirty();
  }

  removeListener(id: string): void {
    this.listeners = this.listeners.filter((l) => l.id !== id);
    this.markDirty();
  }

  // Rule Providers
  addRuleProvider(provider: RuleProvider): void {
    if (!this.ruleProviders.some((p) => p.name === provider.name)) {
      this.ruleProviders = [...this.ruleProviders, provider];
      this.markDirty();
    }
  }

  removeRuleProvider(name: string): void {
    this.ruleProviders = this.ruleProviders.filter((p) => p.name !== name);
    this.markDirty();
  }

  selectedMetaRuleSets = $state<Map<string, string>>(new Map());

  // Presets
  applyPreset(presetKey: string): void {
    if (presetKey === 'zkeen') {
      this.activePreset = 'zkeen';
      this.activeRuleProvider = 'zkeen';
      this.ruleProviders = [...ZKEEN_RULE_PROVIDERS];

      // Добавляем типовые группы zkeen, если их ещё нет
      const existingNames = new Set(this.groups.map((g) => g.name));
      const groupsToAdd = (ZKEEN_16_GROUPS_TEMPLATE || [])
        .filter((g) => !existingNames.has(g.name))
        .map((g) => ({
          ...g,
          id: crypto.randomUUID()
        }));

      if (groupsToAdd.length > 0) {
        this.groups = [...this.groups, ...groupsToAdd];
      }
      this.markDirty();
    } else if (presetKey === 'none') {
      this.activePreset = '';
      this.activeRuleProvider = 'none';
      this.ruleProviders = [];
      this.markDirty();
    }
  }

  generateYaml(capabilitiesKernel?: any): string {
    return generateMihomoYAML({
      proxies: this.proxies,
      groups: this.groups,
      rules: this.rules,
      dns: this.dns,
      tun: this.tun,
      sniffer: this.sniffer,
      activeRuleProvider: this.activeRuleProvider,
      selectedMetaRuleSets: this.selectedMetaRuleSets,
      preservedKeys: this.preservedKeys,
      existingTproxyPort: this.existingTproxyPort,
      existingRedirPort: this.existingRedirPort,
      externalControllerType: this.externalControllerType,
      externalControllerTarget: this.externalControllerTarget,
      subscriptions: this.subscriptions,
      mihomoProviders: this.mihomoProviders,
      capabilities: capabilitiesKernel,
      hasZkeenGeodata: this.hasZkeenGeodata,
      ruleProviders: this.ruleProviders,
      listeners: this.listeners,
      listenersRaw: this.listenersRaw,
      listenersReadOnly: this.listenersReadOnly
    });
  }
}

export const MIHOMO_CONTEXT_KEY = Symbol('mihomo-context');

export function setMihomoContext(ctx: MihomoContext): MihomoContext {
  setContext(MIHOMO_CONTEXT_KEY, ctx);
  return ctx;
}

export function getMihomoContext(): MihomoContext {
  const ctx = getContext<MihomoContext>(MIHOMO_CONTEXT_KEY);
  if (!ctx) {
    throw new Error('MihomoContext not found in component tree');
  }
  return ctx;
}
