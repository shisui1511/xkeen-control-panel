import { get } from 'svelte/store';
import { showToast } from '../stores';
import { t } from '../i18n';

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
  const csrfToken = localStorage.getItem('csrf_token') ?? '';
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
  const envelope: APIResponse<T> = await res.json();
  if (!envelope.success) {
    throw new Error(envelope.error ?? `HTTP ${res.status}`);
  }
  return envelope.data as T;
}
