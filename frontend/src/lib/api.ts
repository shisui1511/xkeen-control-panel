import { get } from 'svelte/store';
import { showToast } from '../stores';
import { t } from '../i18n';
import { saveDraftsToSessionStorage } from './dirtyRegistry';

/**
 * APIResponse — standard envelope returned by migrated backend handlers.
 * Handlers using JSONSuccess/JSONError in response.go return this shape.
 */
export interface APIResponse<T = unknown> {
  success: boolean;
  data?: T;
  error?: string;
}

/**
 * ApiFetchOptions — RequestInit plus apiFetch-specific control fields.
 *
 * skip401Redirect: when true, a 401 response does NOT trigger the centralized
 * logout/toast/redirect sequence (used for expected-anonymous 401s, e.g.
 * App.svelte's checkAuth() against /api/auth/me). The function still throws
 * an Error with status === 401 in both branches — only the side effects are
 * suppressed.
 */
export interface ApiFetchOptions extends RequestInit {
  skip401Redirect?: boolean;
}

// Module-level de-dup guard: prevents duplicate logout/toast/redirect when
// multiple concurrent requests (e.g. several usePoller instances) hit 401 at
// once. First 401 wins; it is never reset back to false — a full page
// navigation follows the redirect, so a manual reset would only open a
// window for repeated toasts.
let loggingOut = false;

/**
 * handleUnauthorized — centralized session-expiry side effects (D-01).
 * Not exported: only apiFetch's 401 branch is allowed to trigger this.
 */
function handleUnauthorized(): void {
  try {
    saveDraftsToSessionStorage();
  } catch (e) {
    console.error('[api] Failed to auto-save drafts on 401:', e);
  }
  localStorage.removeItem('csrf_token');
  showToast('error', get(t)('auth.session_expired'));
  window.location.href = '/';
}

/**
 * apiFetch — drop-in wrapper for fetch() that automatically injects
 * the X-CSRF-Token header from localStorage and centrally handles session
 * expiry (401) via logout + toast + redirect (D-01), with an opt-out via
 * skip401Redirect (D-02).
 *
 * Scope note: this file provides infrastructure for new code (US5+).
 * Migrating existing fetch() calls in other components is a separate task.
 */
export async function apiFetch(url: string, options: ApiFetchOptions = {}): Promise<Response> {
  const { skip401Redirect, ...init } = options;
  const csrfToken =
    typeof localStorage !== 'undefined' ? (localStorage.getItem('csrf_token') ?? '') : '';
  const headers = new Headers(init.headers);
  if (csrfToken) {
    headers.set('X-CSRF-Token', csrfToken);
  }
  const res = await fetch(url, { ...init, headers });
  if (res.status === 401) {
    if (!skip401Redirect && !loggingOut) {
      loggingOut = true;
      handleUnauthorized();
    }
    const err: any = new Error('Unauthorized');
    err.status = 401;
    throw err;
  }
  return res;
}

/**
 * apiFetchJSON — like apiFetch but automatically parses the JSON envelope.
 * Returns envelope.data on success, throws with envelope.error message on failure.
 * Use this for endpoints that return {success, data?, error?} from JSONSuccess/JSONError.
 */
export async function apiFetchJSON<T = unknown>(
  url: string,
  options: ApiFetchOptions = {}
): Promise<T> {
  const res = await apiFetch(url, options);
  let payload: any;
  try {
    payload = await res.json();
  } catch {
    if (!res.ok) {
      throw new Error(`HTTP ${res.status}`);
    }
    throw new Error('Invalid JSON response');
  }

  if (!res.ok) {
    const errorMsg =
      payload && typeof payload === 'object' && payload.error
        ? payload.error
        : `HTTP ${res.status}`;
    throw new Error(errorMsg);
  }

  if (payload && typeof payload === 'object' && 'success' in payload) {
    if (!payload.success) {
      throw new Error(payload.error ?? `HTTP ${res.status}`);
    }
    return (payload.data !== undefined ? payload.data : payload) as T;
  }

  return payload as T;
}

export interface UserRule {
  id: string;
  type: string; // 'domain' | 'domain_suffix' | 'domain_keyword' | 'ip_cidr' | 'port'
  value: string;
  target: string; // 'direct' | 'proxy' | 'reject'
  group?: string;
  comment?: string;
  enabled: boolean;
}

export interface RuleProvider {
  name: string;
  behavior: string;
  type: string;
  ruleCount: number;
  updatedAt: string;
  vehicleType: string;
}

export interface RouteTraceResult {
  target: string;
  matched: boolean;
  rule_type: string;
  rule_payload: string;
  target_action: string;
  target_group: string;
  selected_proxy: string;
  proxy_type: string;
  trace_time_ms: number;
  source: string;
  rule_index?: number;
  undetermined?: boolean;
  undetermined_rule?: string;
  undetermined_group?: string;
  undetermined_reason?: string;
}

export async function fetchCustomRules(): Promise<UserRule[]> {
  const res = await apiFetch('/api/rules/custom');
  if (!res.ok) throw new Error('Failed to load custom rules');
  const data = await res.json();
  return data.data || data.rules || data || [];
}

export interface SaveCustomRulesResult {
  applied: boolean;
  reloaded: boolean;
  count: number;
  warning?: string;
}

export async function saveCustomRules(rules: UserRule[]): Promise<SaveCustomRulesResult> {
  return apiFetchJSON<SaveCustomRulesResult>('/api/rules/custom', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ rules })
  });
}

export async function testRoute(target: string, port?: number): Promise<RouteTraceResult> {
  return apiFetchJSON<RouteTraceResult>('/api/rules/test', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ target, port })
  });
}

export async function flushFakeIP(): Promise<void> {
  const res = await apiFetch('/api/mihomo/cache/fakeip/flush', {
    method: 'POST'
  });
  if (!res.ok) throw new Error('Failed to flush Fake-IP cache');
}

export async function flushDNSCache(): Promise<void> {
  const res = await apiFetch('/api/mihomo/proxy/cache/dns/flush', {
    method: 'POST'
  });
  if (!res.ok) throw new Error('Failed to flush DNS cache');
}

export async function fetchRuleProviders(): Promise<RuleProvider[]> {
  const res = await apiFetch('/api/mihomo/proxy/providers/rules');
  if (!res.ok) throw new Error('Failed to load rule providers');
  const data = await res.json();
  const providersMap = data.providers || {};
  return Object.values(providersMap) as RuleProvider[];
}

export async function updateRuleProvider(name: string): Promise<void> {
  const res = await apiFetch(`/api/mihomo/proxy/providers/rules/${encodeURIComponent(name)}`, {
    method: 'PUT'
  });
  if (!res.ok) {
    // Mihomo explains the failure, e.g. {"message":"404 Not Found"} for a dead URL.
    let reason = `HTTP ${res.status}`;
    try {
      const body = await res.json();
      reason = body?.message || body?.error || reason;
    } catch {
      // non-JSON body: keep the status
    }
    throw new Error(`${name}: ${reason}`);
  }
}

export interface RuleProviderDetails {
  name: string;
  type: string;
  behavior: string;
  format: string;
  url?: string;
  path?: string;
  file_exists: boolean;
  file_size?: number;
  file_mtime?: number;
  inline_count?: number;
}

export interface RuleProviderPage {
  name: string;
  total: number;
  matched: number;
  offset: number;
  entries: string[];
}

export interface RuleProviderURLCheck {
  name: string;
  url: string;
  status_code?: number;
  error?: string;
  ok: boolean;
  duration_ms: number;
}

export async function fetchRuleProviderDetails(): Promise<RuleProviderDetails[]> {
  const list = await apiFetchJSON<RuleProviderDetails[]>('/api/rule-providers/info');
  return Array.isArray(list) ? list : [];
}

export async function fetchRuleProviderContent(
  name: string,
  query: string,
  offset: number,
  limit = 200
): Promise<RuleProviderPage> {
  const params = new URLSearchParams({
    name,
    q: query,
    offset: String(offset),
    limit: String(limit)
  });
  return apiFetchJSON<RuleProviderPage>(`/api/rule-providers/content?${params}`);
}

export async function checkRuleProviderURL(name: string): Promise<RuleProviderURLCheck> {
  return apiFetchJSON<RuleProviderURLCheck>(
    `/api/rule-providers/check-url?name=${encodeURIComponent(name)}`,
    { method: 'POST' }
  );
}
