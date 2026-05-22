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
 * apiFetch — drop-in wrapper for fetch() that automatically injects
 * the X-CSRF-Token header from localStorage.
 *
 * Scope note: this file provides infrastructure for new code (US5+).
 * Migrating existing fetch() calls in other components is a separate task.
 */
export async function apiFetch(url: string, options: RequestInit = {}): Promise<Response> {
  const csrfToken = localStorage.getItem('csrf_token') ?? '';
  const headers = new Headers(options.headers);
  if (csrfToken) {
    headers.set('X-CSRF-Token', csrfToken);
  }
  return fetch(url, { ...options, headers });
}

/**
 * apiFetchJSON — like apiFetch but automatically parses the JSON envelope.
 * Returns envelope.data on success, throws with envelope.error message on failure.
 * Use this for endpoints that return {success, data?, error?} from JSONSuccess/JSONError.
 */
export async function apiFetchJSON<T = unknown>(
  url: string,
  options: RequestInit = {}
): Promise<T> {
  const res = await apiFetch(url, options);
  const envelope: APIResponse<T> = await res.json();
  if (!envelope.success) {
    throw new Error(envelope.error ?? `HTTP ${res.status}`);
  }
  return envelope.data as T;
}
