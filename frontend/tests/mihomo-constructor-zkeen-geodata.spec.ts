import { test, expect, type Page } from '@playwright/test';

test.use({ locale: 'ru-RU' });

// Запись B25 этапа 10: конструктор Mihomo проверяет базу zkeen по реальному
// имени файла из списка баз и не обращается к тегам файла, которого нет
// (запрос geosite.dat давал 400 и ошибку в консоли браузера).

const CONFIG_PATH = '/opt/etc/mihomo/config.yaml';
const ZKEEN_TAGS = [
  { tag: 'domains', count: 12 },
  { tag: 'other', count: 5 },
  { tag: 'politic', count: 7 }
];

async function mockPanel(page: Page, datFiles: { name: string; exists: boolean }[]) {
  const tagRequests: string[] = [];
  const badResponses: string[] = [];
  page.on('response', (res) => {
    if (res.url().includes('/api/') && res.status() >= 400) badResponses.push(res.url());
  });

  await page.addInitScript(() => {
    Object.defineProperty(window.navigator, 'serviceWorker', {
      value: undefined,
      writable: false,
      configurable: true
    });
  });

  await page.route('**/api/**', async (route) => {
    const url = route.request().url();
    const json = (body: unknown, status = 200) =>
      route.fulfill({ status, contentType: 'application/json', body: JSON.stringify(body) });

    if (url.includes('/api/auth/me')) {
      await json({ authenticated: true, setup_required: false, csrf_token: 'mock-csrf-token' });
    } else if (url.includes('/api/capabilities')) {
      await json({
        success: true,
        data: {
          kernels: {
            xray: { installed: true, version: '1.8.4', channel: 'stable' },
            mihomo: { installed: true, version: '1.18.0', channel: 'stable' }
          },
          active_kernel: 'mihomo'
        }
      });
    } else if (url.includes('/api/dat/list')) {
      await json(
        datFiles.map((f) => ({ ...f, path: `/opt/etc/xray/dat/${f.name}`, type: 'xray' }))
      );
    } else if (url.includes('/api/dat/tags')) {
      const name = new URL(url).searchParams.get('name') ?? '';
      tagRequests.push(name);
      if (name === 'geosite_zkeen.dat') {
        await json({ success: true, data: ZKEEN_TAGS });
      } else {
        await json({ success: false, error: 'file not found' }, 400);
      }
    } else if (url.includes('/api/config/list')) {
      await json(
        url.includes('mihomo') ? [{ name: 'config.yaml', path: CONFIG_PATH, size: 1500 }] : []
      );
    } else if (url.includes('/api/config/read')) {
      await route.fulfill({
        status: 200,
        contentType: 'text/plain',
        body: 'proxies: []\nproxy-groups: []\nrules: []\n'
      });
    } else if (url.includes('/api/assets/definition')) {
      await json({});
    } else {
      await json({ success: true, data: {} });
    }
  });

  return { tagRequests, badResponses };
}

test.describe('конструктор Mihomo: база zkeen', () => {
  test('на роутере geosite_zkeen.dat: теги читаются из него, запроса geosite.dat нет (B25)', async ({
    page
  }) => {
    const seen = await mockPanel(page, [
      { name: 'geosite_zkeen.dat', exists: true },
      { name: 'geosite_v2fly.dat', exists: true }
    ]);
    await page.goto('/#/constructor');
    await page.waitForSelector('.gen-layout', { timeout: 8000 });
    await expect.poll(() => seen.tagRequests.length).toBeGreaterThan(0);

    expect(seen.tagRequests).toEqual(['geosite_zkeen.dat']);
    expect(seen.badResponses).toEqual([]);
  });

  test('баз zkeen нет: запросов тегов нет совсем (B25)', async ({ page }) => {
    const seen = await mockPanel(page, [{ name: 'geosite_v2fly.dat', exists: true }]);
    await page.goto('/#/constructor');
    await page.waitForSelector('.gen-layout', { timeout: 8000 });
    await page.waitForTimeout(1500);

    expect(seen.tagRequests).toEqual([]);
    expect(seen.badResponses).toEqual([]);
  });

  test('файл есть в списке, но не существует на диске: теги не запрашиваются (B25)', async ({
    page
  }) => {
    const seen = await mockPanel(page, [{ name: 'geosite_zkeen.dat', exists: false }]);
    await page.goto('/#/constructor');
    await page.waitForSelector('.gen-layout', { timeout: 8000 });
    await page.waitForTimeout(1500);

    expect(seen.tagRequests).toEqual([]);
  });
});
