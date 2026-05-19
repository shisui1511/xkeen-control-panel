/**
 * apiFetch — drop-in wrapper for fetch() that automatically injects
 * the X-CSRF-Token header from localStorage.
 *
 * Scope note: this file provides infrastructure for new code (US5+).
 * Migrating existing fetch() calls in other components is a separate task.
 */
export async function apiFetch(url: string, options: RequestInit = {}): Promise<Response> {
  const csrfToken = localStorage.getItem('csrf_token') ?? ''
  const headers = new Headers(options.headers)
  if (csrfToken) {
    headers.set('X-CSRF-Token', csrfToken)
  }
  return fetch(url, { ...options, headers })
}
