import { describe, it, expect } from 'vitest';
import checkBundleSize from '../scripts/check-bundle-size.cjs';
import injectSwVersion from '../scripts/inject-sw-version.cjs';

const {
  BUDGET_BYTES,
  findEntryChunk,
  gzipSize,
  formatKb,
  checkNoExternalResources,
  checkWoff2Only,
  checkSwCacheVersioned,
  checkApiBranchUncached
} = checkBundleSize;

const { injectVersion } = injectSwVersion;

describe('Bundle Size Gate', () => {
  it('findEntryChunk returns the record with isEntry: true, ignoring async chunks', () => {
    const manifest = {
      'src/main.ts': { file: 'assets/index-abc123.js', isEntry: true },
      'src/Logs.svelte': { file: 'assets/Logs-def456.js' },
      'src/Proxies.svelte': { file: 'assets/Proxies-ghi789.js', isEntry: false }
    };
    const entry = findEntryChunk(manifest);
    expect(entry).not.toBeNull();
    expect(entry.file).toBe('assets/index-abc123.js');
  });

  it('findEntryChunk returns null when no record has isEntry: true', () => {
    const manifest = {
      'src/Logs.svelte': { file: 'assets/Logs-def456.js' },
      'src/Proxies.svelte': { file: 'assets/Proxies-ghi789.js', isEntry: false }
    };
    expect(findEntryChunk(manifest)).toBeNull();
  });

  it('gzipSize returns a positive number smaller than the input for compressible data', () => {
    const buffer = Buffer.alloc(10000, 'a');
    const size = gzipSize(buffer);
    expect(size).toBeGreaterThan(0);
    expect(size).toBeLessThan(buffer.length);
  });

  it('gzipSize is deterministic across repeated calls on the same buffer', () => {
    const buffer = Buffer.alloc(10000, 'a');
    expect(gzipSize(buffer)).toBe(gzipSize(buffer));
  });

  it('formatKb formats bytes as KB with two decimal places', () => {
    expect(formatKb(204800)).toBe('200.00');
  });

  it('BUDGET_BYTES equals 204800 (200 KB)', () => {
    expect(BUDGET_BYTES).toBe(204800);
  });

  it('checkNoExternalResources finds a violation for an absolute https:// link href', () => {
    const violations = checkNoExternalResources('<link href="https://example.com/x.css" />');
    expect(violations).toEqual(['https://example.com/x.css']);
  });

  it('checkNoExternalResources finds a violation for a scheme-relative // script src', () => {
    const violations = checkNoExternalResources('<script src="//example.com/x.js"></script>');
    expect(violations).toEqual(['//example.com/x.js']);
  });

  it('checkNoExternalResources ignores relative and data: URIs', () => {
    const html = `
      <link rel="manifest" href="./manifest.json" />
      <link rel="stylesheet" href="/assets/index.css" />
      <link rel="icon" href="data:image/svg+xml,%3Csvg%3E%3C/svg%3E" />
    `;
    expect(checkNoExternalResources(html)).toEqual([]);
  });

  it('checkWoff2Only finds a violation for a truetype src inside @font-face', () => {
    const css = `
      @font-face {
        font-family: 'Manrope';
        src: url('/fonts/a.ttf') format('truetype');
      }
    `;
    expect(checkWoff2Only(css)).toEqual(['/fonts/a.ttf']);
  });

  it('checkWoff2Only returns an empty array for a woff2-only src', () => {
    const css = `
      @font-face {
        font-family: 'Manrope';
        src: url('/fonts/a.woff2') format('woff2');
      }
    `;
    expect(checkWoff2Only(css)).toEqual([]);
  });

  it('checkApiBranchUncached finds a violation when the /api/ branch caches the response', () => {
    const sw = `
      self.addEventListener('fetch', (event) => {
        const url = new URL(event.request.url);
        if (url.pathname.startsWith('/api/')) {
          event.respondWith(
            fetch(event.request).then((response) => {
              caches.open(CACHE_NAME).then((cache) => cache.put(event.request, response.clone()));
              return response;
            })
          );
          return;
        }
      });
    `;
    const violations = checkApiBranchUncached(sw);
    expect(violations.length).toBeGreaterThan(0);
  });

  it('checkApiBranchUncached returns an empty array for a plain network passthrough branch', () => {
    const sw = `
      self.addEventListener('fetch', (event) => {
        const url = new URL(event.request.url);
        if (url.pathname.startsWith('/api/')) {
          event.respondWith(fetch(event.request));
          return;
        }
      });
    `;
    expect(checkApiBranchUncached(sw)).toEqual([]);
  });

  it('checkApiBranchUncached returns an empty array when Cache Storage is used only outside the /api/ branch (region-scoped)', () => {
    const sw = `
      self.addEventListener('install', (event) => {
        event.waitUntil(caches.open(CACHE_NAME).then((cache) => cache.addAll(['/'])));
      });
      self.addEventListener('fetch', (event) => {
        const url = new URL(event.request.url);
        if (url.pathname.startsWith('/api/')) {
          event.respondWith(fetch(event.request));
          return;
        }
        event.respondWith(
          caches.match(event.request).then((cached) => cached || fetch(event.request))
        );
      });
    `;
    expect(checkApiBranchUncached(sw)).toEqual([]);
  });

  it('checkSwCacheVersioned returns a violation when the build placeholder was not substituted', () => {
    const sw = "const CACHE_NAME = 'xcp-v__BUILD_VERSION__';";
    const violations = checkSwCacheVersioned(sw, '0.21.0');
    expect(violations.length).toBeGreaterThan(0);
  });

  it('checkSwCacheVersioned returns a violation when the version does not match package.json', () => {
    const sw = "const CACHE_NAME = 'xcp-v0.20.0';";
    const violations = checkSwCacheVersioned(sw, '0.21.0');
    expect(violations.length).toBeGreaterThan(0);
  });

  it('checkSwCacheVersioned returns an empty array when the version is correctly substituted', () => {
    const sw = "const CACHE_NAME = 'xcp-v0.21.0';";
    expect(checkSwCacheVersioned(sw, '0.21.0')).toEqual([]);
  });

  it('injectVersion substitutes the version into the CACHE_NAME declaration', () => {
    const out = injectVersion("const CACHE_NAME = 'xcp-vX';", '9.9.9');
    expect(out).toBe("const CACHE_NAME = 'xcp-v9.9.9';");
  });

  it('injectVersion throws when the CACHE_NAME declaration is missing', () => {
    expect(() => injectVersion('// no cache name here', '9.9.9')).toThrow();
  });
});
