import { writable } from 'svelte/store';

/**
 * AuthStatus — single source of truth for which screen App.svelte renders.
 * 'error' is set by App.svelte's checkAuth() on a network failure (distinct
 * from the anonymous 'login'/'setup' states returned by a successful
 * /api/auth/me call).
 */
export type AuthStatus = 'loading' | 'authenticated' | 'login' | 'setup' | 'error';

export const authStatus = writable<AuthStatus>('loading');

// Module-level de-dup guard (D-19): several concurrent requests can all hit
// 401 at once (e.g. multiple usePoller instances). Only the first caller
// since the last markAuthenticated() should trigger the toast + login
// switch — claimUnauthorized() lets api.ts express exactly that without
// duplicating the guard logic at every call site.
let unauthorizedClaimed = false;

/**
 * claimUnauthorized — returns true only for the first 401 since the last
 * markAuthenticated() call. Subsequent concurrent/duplicate 401s (before a
 * fresh login) return false so the caller skips its side effects.
 */
export function claimUnauthorized(): boolean {
  if (unauthorizedClaimed) return false;
  unauthorizedClaimed = true;
  return true;
}

/**
 * markAuthenticated — called after a successful login. Resets the 401 guard
 * so a session that expires again after this point produces a fresh toast.
 */
export function markAuthenticated(): void {
  unauthorizedClaimed = false;
  authStatus.set('authenticated');
}

/**
 * markLoggedOut — switches the app to the login screen in place, without any
 * navigation/reload (D-19: the current #/route and any dirty-editor drafts
 * must survive).
 */
export function markLoggedOut(): void {
  authStatus.set('login');
}
