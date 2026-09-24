/**
 * Xray routing model for the constructor.
 *
 * Xray has no "disabled" flag for rules, but it ignores unknown keys. Rules
 * switched off in the UI are therefore kept in `routing.xcpDisabledRules`
 * together with their position, so they survive a save and come back at the
 * same place. Every other field of a rule (balancerTag, source, user, attrs,
 * ruleTag, …) is preserved as is.
 */

export interface UIRoutingRule {
  id: string;
  enabled: boolean;
  outboundTag?: string;
  balancerTag?: string;
  domain?: string[];
  ip?: string[];
  source?: string[];
  sourcePort?: string;
  port?: string;
  network?: string;
  protocol?: string[];
  inboundTag?: string[];
  ruleTag?: string;
  [key: string]: unknown;
}

export interface XrayBalancer {
  tag: string;
  selector: string[];
  strategy?: { type?: string; settings?: Record<string, unknown> };
  fallbackTag?: string;
  [key: string]: unknown;
}

export interface ObservatorySettings {
  probeUrl: string;
  probeInterval: string;
}

/** Keys that only exist in the UI and must never reach the config file. */
const UI_ONLY_KEYS = new Set(['id', 'enabled', 'xcpPosition']);

/** Strategies that need an observatory to rank outbounds. */
export const PROBING_STRATEGIES = new Set(['leastPing', 'leastLoad']);

export const DEFAULT_OBSERVATORY: ObservatorySettings = {
  probeUrl: 'https://www.google.com/generate_204',
  probeInterval: '1m'
};

function newId(): string {
  return typeof crypto !== 'undefined' && 'randomUUID' in crypto
    ? crypto.randomUUID()
    : 'r-' + Math.random().toString(36).slice(2);
}

/** Builds UI rules from a `routing` object, restoring disabled rules. */
export function rulesFromConfig(routing: {
  rules?: unknown[];
  xcpDisabledRules?: unknown[];
}): UIRoutingRule[] {
  const active = (Array.isArray(routing?.rules) ? routing.rules : []).map((r) => ({
    ...(r as Record<string, unknown>),
    id: newId(),
    enabled: true
  })) as UIRoutingRule[];

  const disabled = (Array.isArray(routing?.xcpDisabledRules) ? routing.xcpDisabledRules : [])
    .map((r) => r as Record<string, unknown>)
    .map((r) => ({ rule: r, pos: typeof r.xcpPosition === 'number' ? r.xcpPosition : Infinity }))
    .sort((a, b) => a.pos - b.pos);

  const result = [...active];
  for (const { rule, pos } of disabled) {
    const { xcpPosition: _drop, ...rest } = rule;
    void _drop;
    const ui = { ...rest, id: newId(), enabled: false } as UIRoutingRule;
    const at = Number.isFinite(pos) ? Math.min(pos, result.length) : result.length;
    result.splice(at, 0, ui);
  }
  return result;
}

/** Strips UI-only keys and empty values, keeping every other field. */
export function cleanRule(rule: UIRoutingRule): Record<string, unknown> {
  const out: Record<string, unknown> = {};
  for (const [key, value] of Object.entries(rule)) {
    if (UI_ONLY_KEYS.has(key) || key.endsWith('Raw')) continue;
    if (value === undefined || value === null || value === '') continue;
    if (Array.isArray(value) && value.length === 0) continue;
    out[key] = value;
  }
  // A rule targets either an outbound or a balancer, never both.
  if (out.balancerTag) delete out.outboundTag;
  return out;
}

/** Splits UI rules into active `rules` and positioned `xcpDisabledRules`. */
export function rulesToConfig(rules: UIRoutingRule[]): {
  rules: Record<string, unknown>[];
  xcpDisabledRules?: Record<string, unknown>[];
} {
  const active: Record<string, unknown>[] = [];
  const disabled: Record<string, unknown>[] = [];
  rules.forEach((rule, position) => {
    const clean = cleanRule(rule);
    if (rule.enabled === false) disabled.push({ ...clean, xcpPosition: position });
    else active.push(clean);
  });
  return disabled.length > 0 ? { rules: active, xcpDisabledRules: disabled } : { rules: active };
}

/**
 * Returns the observatory config the balancers need, or undefined when no
 * balancer ranks outbounds by probing.
 */
export function observatoryFor(
  balancers: XrayBalancer[],
  settings: ObservatorySettings = DEFAULT_OBSERVATORY
): Record<string, unknown> | undefined {
  const selectors = new Set<string>();
  for (const b of balancers) {
    if (!PROBING_STRATEGIES.has(b.strategy?.type ?? '')) continue;
    for (const s of b.selector ?? []) if (s) selectors.add(s);
  }
  if (selectors.size === 0) return undefined;
  return {
    subjectSelector: [...selectors].sort(),
    probeUrl: settings.probeUrl || DEFAULT_OBSERVATORY.probeUrl,
    probeInterval: settings.probeInterval || DEFAULT_OBSERVATORY.probeInterval,
    enableConcurrency: true
  };
}

/** Normalizes a balancer before saving (drops empty values). */
export function cleanBalancer(b: XrayBalancer): XrayBalancer {
  const out: XrayBalancer = { ...b, tag: b.tag.trim(), selector: b.selector.filter(Boolean) };
  if (!out.fallbackTag) delete out.fallbackTag;
  if (!out.strategy?.type || out.strategy.type === 'random') delete out.strategy;
  return out;
}

export type DiffLine = { kind: 'same' | 'add' | 'del'; text: string };

/** Line diff (LCS) for the apply preview; files here are small. */
export function lineDiff(before: string, after: string): DiffLine[] {
  const a = before.split('\n');
  const b = after.split('\n');
  const m = a.length;
  const n = b.length;
  const lcs: number[][] = Array.from({ length: m + 1 }, () => new Array(n + 1).fill(0));
  for (let i = m - 1; i >= 0; i--) {
    for (let j = n - 1; j >= 0; j--) {
      lcs[i][j] = a[i] === b[j] ? lcs[i + 1][j + 1] + 1 : Math.max(lcs[i + 1][j], lcs[i][j + 1]);
    }
  }
  const out: DiffLine[] = [];
  let i = 0;
  let j = 0;
  while (i < m && j < n) {
    if (a[i] === b[j]) {
      out.push({ kind: 'same', text: a[i] });
      i++;
      j++;
    } else if (lcs[i + 1][j] >= lcs[i][j + 1]) {
      out.push({ kind: 'del', text: a[i++] });
    } else {
      out.push({ kind: 'add', text: b[j++] });
    }
  }
  while (i < m) out.push({ kind: 'del', text: a[i++] });
  while (j < n) out.push({ kind: 'add', text: b[j++] });
  return out;
}

/** True when a JSON(C) text contains comments that a rewrite would drop. */
export function hasJsonComments(text: string): boolean {
  let inString = false;
  for (let i = 0; i < text.length; i++) {
    const c = text[i];
    if (inString) {
      if (c === '\\') i++;
      else if (c === '"') inString = false;
      continue;
    }
    if (c === '"') inString = true;
    else if (c === '/' && (text[i + 1] === '/' || text[i + 1] === '*')) return true;
  }
  return false;
}

/** Removes // and /* *\/ comments outside strings, keeping line breaks. */
export function stripJsonComments(text: string): string {
  let out = '';
  let inString = false;
  for (let i = 0; i < text.length; i++) {
    const c = text[i];
    if (inString) {
      out += c;
      if (c === '\\') out += text[++i] ?? '';
      else if (c === '"') inString = false;
      continue;
    }
    if (c === '"') {
      inString = true;
      out += c;
    } else if (c === '/' && text[i + 1] === '/') {
      while (i < text.length && text[i] !== '\n') i++;
      if (i < text.length) out += '\n';
    } else if (c === '/' && text[i + 1] === '*') {
      i += 2;
      while (i < text.length && !(text[i] === '*' && text[i + 1] === '/')) {
        if (text[i] === '\n') out += '\n';
        i++;
      }
      i++;
    } else {
      out += c;
    }
  }
  return out;
}

/** Parses JSON with comments; returns undefined for empty or invalid text. */
export function parseJsonc(text: string): any {
  const clean = stripJsonComments(text).trim();
  if (!clean) return undefined;
  try {
    return JSON.parse(clean);
  } catch {
    return undefined;
  }
}

/** ruleTag of the routing rules the "DNS over proxy" switch manages. */
export const DNS_OVER_PROXY_TAG = 'xcp-dns-over-proxy';

/**
 * Rules sending the Xray DNS module's own queries through the proxy:
 * private resolvers (router, LAN) stay direct, everything else goes to
 * `proxyTag`. They are placed first so no other rule catches the queries.
 */
export function dnsOverProxyRules(dnsTag: string, proxyTag: string): Record<string, unknown>[] {
  return [
    {
      type: 'field',
      inboundTag: [dnsTag],
      ip: ['geoip:private'],
      outboundTag: 'direct',
      ruleTag: DNS_OVER_PROXY_TAG
    },
    { type: 'field', inboundTag: [dnsTag], outboundTag: proxyTag, ruleTag: DNS_OVER_PROXY_TAG }
  ];
}

/** Splits managed DNS-over-proxy rules from the user's rules. */
export function takeDnsOverProxyRules(rules: UIRoutingRule[]): {
  enabled: boolean;
  rest: UIRoutingRule[];
} {
  const rest = rules.filter((r) => r.ruleTag !== DNS_OVER_PROXY_TAG);
  return { enabled: rest.length !== rules.length, rest };
}
