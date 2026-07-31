import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { get } from 'svelte/store';

// The module under test holds a module-level `loggingOut` guard that is
// never reset publicly (by design — see api.ts). To keep each scenario
// independent, we reset the module registry via vi.resetModules() and
// dynamically re-import api.ts (plus its ../stores and ../i18n deps)
// INSIDE every test — this guarantees a fresh guard flag AND that the
// toastStore/t instances we assert against are the exact same instances
// api.ts's showToast/get(t) calls operate on (a static top-level import
// of ../stores would resolve to a stale module instance after
// resetModules(), silently observing a different toastStore than the one
// api.ts writes to).
async function loadFreshApi() {
  const api = await import('./api');
  const stores = await import('../stores');
  const i18n = await import('../i18n');
  return { ...api, toastStore: stores.toastStore, t: i18n.t };
}

function makeResponse(status: number, body: unknown): Response {
  return {
    status,
    ok: status >= 200 && status < 300,
    json: () => Promise.resolve(body),
    text: () => Promise.resolve(typeof body === 'string' ? body : JSON.stringify(body))
  } as unknown as Response;
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
  it('scenario 1: unexpected 401 triggers logout + single toast + redirect, and throws status 401', async () => {
    fetchMock.mockResolvedValue(makeResponse(401, { success: false }));
    const { apiFetch, toastStore, t } = await loadFreshApi();

    await expect(apiFetch('/api/settings')).rejects.toMatchObject({ status: 401 });

    expect(localStorageMock.removeItem).toHaveBeenCalledWith('csrf_token');
    const toasts = get(toastStore);
    expect(toasts).toHaveLength(1);
    expect(toasts[0].type).toBe('error');
    expect(toasts[0].message).toBe(get(t)('auth.session_expired'));
    expect(windowMock.location.href).toBe('/');
  });

  it('scenario 2: concurrent 401s from multiple callers de-dup to exactly one toast/redirect', async () => {
    fetchMock.mockResolvedValue(makeResponse(401, { success: false }));
    const { apiFetch, toastStore } = await loadFreshApi();

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
    expect(windowMock.location.href).toBe('/');
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
      text: () => Promise.resolve('<html>502 Bad Gateway</html>')
    } as unknown as Response;
    fetchMock.mockResolvedValueOnce(badGatewayResponse);
    await expect(apiFetchJSON('/api/broken')).rejects.toThrow('HTTP 502');

    // Non-ok response with JSON error message
    fetchMock.mockResolvedValueOnce(makeResponse(400, { error: 'Bad Request Parameter' }));
    await expect(apiFetchJSON('/api/bad-param')).rejects.toThrow('Bad Request Parameter');
  });
});
