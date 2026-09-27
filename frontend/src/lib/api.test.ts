import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { get } from 'svelte/store';

// The module under test holds a module-level 401 de-dup guard (authState.ts's
// unauthorizedClaimed) that is only reset by markAuthenticated(). To keep
// each scenario independent, we reset the module registry via
// vi.resetModules() and dynamically re-import api.ts (plus its ../stores,
// ../i18n and ./authState deps) INSIDE every test — this guarantees a fresh
// guard flag AND that the toastStore/t/authStatus instances we assert
// against are the exact same instances api.ts's showToast/get(t)/
// claimUnauthorized calls operate on (a static top-level import of ../stores
// would resolve to a stale module instance after resetModules(), silently
// observing a different toastStore than the one api.ts writes to).
async function loadFreshApi() {
  const api = await import('./api');
  const stores = await import('../stores');
  const i18n = await import('../i18n');
  const authState = await import('./authState');
  return {
    ...api,
    toastStore: stores.toastStore,
    t: i18n.t,
    authStatus: authState.authStatus,
    markAuthenticated: authState.markAuthenticated
  };
}

function makeResponse(status: number, body: unknown): Response {
  const response = {
    status,
    ok: status >= 200 && status < 300,
    json: () => Promise.resolve(body),
    text: () => Promise.resolve(typeof body === 'string' ? body : JSON.stringify(body)),
    clone: () => makeResponse(status, body)
  } as unknown as Response;
  return response;
}

let localStorageMock: {
  getItem: ReturnType<typeof vi.fn>;
  setItem: ReturnType<typeof vi.fn>;
  removeItem: ReturnType<typeof vi.fn>;
};
let windowMock: { location: { href: string } };
let fetchMock: ReturnType<typeof vi.fn>;

beforeEach(() => {
  vi.resetModules();

  localStorageMock = {
    getItem: vi.fn().mockReturnValue('test-csrf-token'),
    setItem: vi.fn(),
    removeItem: vi.fn()
  };
  windowMock = { location: { href: '' } };
  fetchMock = vi.fn();

  vi.stubGlobal('localStorage', localStorageMock);
  vi.stubGlobal('window', windowMock);
  vi.stubGlobal('fetch', fetchMock);
});

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

describe('apiFetch', () => {
  it('scenario 1: unexpected 401 triggers logout + single toast + in-place login switch, and throws status 401', async () => {
    fetchMock.mockResolvedValue(makeResponse(401, { success: false }));
    const { apiFetch, toastStore, t, authStatus } = await loadFreshApi();

    await expect(apiFetch('/api/settings')).rejects.toMatchObject({ status: 401 });

    expect(localStorageMock.removeItem).toHaveBeenCalledWith('csrf_token');
    const toasts = get(toastStore);
    expect(toasts).toHaveLength(1);
    expect(toasts[0].type).toBe('error');
    expect(toasts[0].message).toBe(get(t)('auth.session_expired'));
    expect(get(authStatus)).toBe('login');
    // 401 handling no longer navigates — the current #/route must survive.
    expect(windowMock.location.href).toBe('');
  });

  it('scenario 2: concurrent 401s from multiple callers de-dup to exactly one toast', async () => {
    fetchMock.mockResolvedValue(makeResponse(401, { success: false }));
    const { apiFetch, toastStore, authStatus } = await loadFreshApi();

    const results = await Promise.allSettled([
      apiFetch('/api/a'),
      apiFetch('/api/b'),
      apiFetch('/api/c')
    ]);

    for (const result of results) {
      expect(result.status).toBe('rejected');
      if (result.status === 'rejected') {
        expect(result.reason).toMatchObject({ status: 401 });
      }
    }

    expect(get(toastStore)).toHaveLength(1);
    expect(get(authStatus)).toBe('login');
  });

  it('scenario 3: skip401Redirect suppresses side effects but still throws status 401', async () => {
    fetchMock.mockResolvedValue(makeResponse(401, { success: false }));
    const { apiFetch, toastStore } = await loadFreshApi();

    await expect(apiFetch('/api/auth/me', { skip401Redirect: true })).rejects.toMatchObject({
      status: 401
    });

    expect(localStorageMock.removeItem).not.toHaveBeenCalled();
    expect(get(toastStore)).toHaveLength(0);
    expect(windowMock.location.href).toBe('');
  });

  it('scenario 4: non-401 error response resolves normally with no logout side effects', async () => {
    fetchMock.mockResolvedValue(makeResponse(500, { success: false, error: 'boom' }));
    const { apiFetch, toastStore } = await loadFreshApi();

    const res = await apiFetch('/api/settings');
    expect(res.ok).toBe(false);
    expect(res.status).toBe(500);
    expect(localStorageMock.removeItem).not.toHaveBeenCalled();
    expect(get(toastStore)).toHaveLength(0);

    // CSRF header present when token is non-empty
    const callOptions = fetchMock.mock.calls[0][1];
    const headers = new Headers(callOptions.headers);
    expect(headers.get('X-CSRF-Token')).toBe('test-csrf-token');
  });

  it('scenario 5: apiFetchJSON unwraps envelope.data on success and throws envelope.error on failure', async () => {
    const { apiFetchJSON } = await loadFreshApi();

    fetchMock.mockResolvedValueOnce(makeResponse(200, { success: true, data: { a: 1 } }));
    await expect(apiFetchJSON('/api/ok')).resolves.toEqual({ a: 1 });

    fetchMock.mockResolvedValueOnce(makeResponse(200, { success: false, error: 'boom' }));
    await expect(apiFetchJSON('/api/fail')).rejects.toThrow('boom');
  });

  it('scenario 6: apiFetchJSON handles raw non-enveloped JSON, non-JSON errors, and non-ok statuses', async () => {
    const { apiFetchJSON } = await loadFreshApi();

    // Raw array response (non-enveloped)
    fetchMock.mockResolvedValueOnce(makeResponse(200, [{ name: 'proxy-1' }]));
    await expect(apiFetchJSON('/api/proxy-providers')).resolves.toEqual([{ name: 'proxy-1' }]);

    // Non-JSON 502 Bad Gateway response
    const badGatewayResponse = {
      status: 502,
      ok: false,
      json: () => Promise.reject(new SyntaxError('Unexpected token < in JSON at position 0')),
      text: () => Promise.resolve('<html>502 Bad Gateway</html>'),
      clone() {
        return badGatewayResponse;
      }
    } as unknown as Response;
    fetchMock.mockResolvedValueOnce(badGatewayResponse);
    await expect(apiFetchJSON('/api/broken')).rejects.toThrow('HTTP 502');

    // Non-ok response with JSON error message
    fetchMock.mockResolvedValueOnce(makeResponse(400, { error: 'Bad Request Parameter' }));
    await expect(apiFetchJSON('/api/bad-param')).rejects.toThrow('Bad Request Parameter');
  });

  it('scenario 7: reason=password_changed selects the password-changed toast copy', async () => {
    fetchMock.mockResolvedValue(
      makeResponse(401, { success: false, error: 'Unauthorized', reason: 'password_changed' })
    );
    const { apiFetch, toastStore, t } = await loadFreshApi();

    await expect(apiFetch('/api/settings')).rejects.toMatchObject({ status: 401 });

    const toasts = get(toastStore);
    expect(toasts).toHaveLength(1);
    expect(toasts[0].message).toBe(get(t)('auth.session_password_changed'));
  });

  it('scenario 8: reason=terminated_elsewhere selects the terminated-elsewhere toast copy', async () => {
    fetchMock.mockResolvedValue(
      makeResponse(401, { success: false, error: 'Unauthorized', reason: 'terminated_elsewhere' })
    );
    const { apiFetch, toastStore, t } = await loadFreshApi();

    await expect(apiFetch('/api/settings')).rejects.toMatchObject({ status: 401 });

    const toasts = get(toastStore);
    expect(toasts).toHaveLength(1);
    expect(toasts[0].message).toBe(get(t)('auth.session_terminated_elsewhere'));
  });

  it('scenario 9: an unrecognized reason falls back to the generic session-expired copy', async () => {
    fetchMock.mockResolvedValue(
      makeResponse(401, { success: false, error: 'Unauthorized', reason: 'something_unknown' })
    );
    const { apiFetch, toastStore, t } = await loadFreshApi();

    await expect(apiFetch('/api/settings')).rejects.toMatchObject({ status: 401 });

    const toasts = get(toastStore);
    expect(toasts).toHaveLength(1);
    expect(toasts[0].message).toBe(get(t)('auth.session_expired'));
  });

  it('scenario 10: after markAuthenticated() resets the guard, a fresh 401 produces a new toast', async () => {
    fetchMock.mockResolvedValue(makeResponse(401, { success: false }));
    const { apiFetch, toastStore, markAuthenticated } = await loadFreshApi();

    await expect(apiFetch('/api/settings')).rejects.toMatchObject({ status: 401 });
    expect(get(toastStore)).toHaveLength(1);

    markAuthenticated();

    await expect(apiFetch('/api/settings')).rejects.toMatchObject({ status: 401 });
    expect(get(toastStore)).toHaveLength(2);
  });

  it('scenario 11: a rejected fetch (network down) reports the panel as unreachable (D-20)', async () => {
    fetchMock.mockRejectedValueOnce(new TypeError('Failed to fetch'));
    const { apiFetch } = await loadFreshApi();
    const stores = await import('../stores');
    const panelHealth = await import('./panelHealth');

    await expect(apiFetch('/api/settings')).rejects.toThrow();
    expect(get(stores.panelUnreachable)).toBe(true);

    // The failed probe schedules a real setTimeout retry — clear it so it
    // cannot fire against a torn-down fetch mock after this test ends.
    panelHealth.__resetPanelHealthForTests();
  });

  it('scenario 12: an AbortError from fetch does not report the panel as unreachable', async () => {
    const abortError = new Error('aborted');
    abortError.name = 'AbortError';
    fetchMock.mockRejectedValueOnce(abortError);
    const { apiFetch } = await loadFreshApi();
    const stores = await import('../stores');

    await expect(apiFetch('/api/settings')).rejects.toThrow();
    expect(get(stores.panelUnreachable)).toBe(false);
  });

  it('scenario 13: a 401 clears panelUnreachable before showing the reason toast (never both at once)', async () => {
    const stores = await import('../stores');
    stores.panelUnreachable.set(true);
    fetchMock.mockResolvedValue(makeResponse(401, { success: false }));
    const { apiFetch, toastStore } = await loadFreshApi();

    await expect(apiFetch('/api/settings')).rejects.toMatchObject({ status: 401 });

    expect(get(stores.panelUnreachable)).toBe(false);
    expect(get(toastStore)).toHaveLength(1);
  });
});
