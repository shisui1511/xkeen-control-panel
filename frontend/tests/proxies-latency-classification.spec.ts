import { test, expect } from '@playwright/test';

test.use({ locale: 'ru-RU' });

// Фикстура для регрессии единства классификации задержки (Phase 90 gap closure).
// Группа `Untested` намеренно не совпадает ни с одним CORE_GROUP_PATTERNS и не
// ссылается на неё другая группа, поэтому classifyGroupRole() относит её к
// 'service' — карточка рендерится обычной (с .gc-lat-box и .gc-now-pill-trigger),
// а не мини-карточкой системного выхода.
const MOCK_PROXIES_MIXED = {
  proxies: {
    Untested: {
      name: 'Untested',
      type: 'Selector',
      now: 'Fresh-Node-01',
      all: ['Fresh-Node-01', 'Quick-Node-02', 'Dead-Node-03', 'DIRECT'],
      alive: true
    },
    // Непроверенный живой узел: alive: true, но без history и без delay —
    // ровно то состояние, в котором узел оказывается сразу после перезапуска
    // Mihomo или свежего импорта подписки.
    'Fresh-Node-01': {
      name: 'Fresh-Node-01',
      type: 'Vless',
      alive: true
    },
    // Контрольный узел с реальной быстрой задержкой.
    'Quick-Node-02': {
      name: 'Quick-Node-02',
      type: 'Vless',
      alive: true,
      history: [{ delay: 120, time: '' }]
    },
    // Контрольный узел: недоступен, задержка 0.
    'Dead-Node-03': {
      name: 'Dead-Node-03',
      type: 'Vless',
      alive: false,
      history: [{ delay: 0, time: '' }]
    },
    DIRECT: {
      name: 'DIRECT',
      type: 'Direct'
    },
    REJECT: {
      name: 'REJECT',
      type: 'Reject'
    }
  }
};

test.describe('Единая классификация задержки прокси (Phase 90 gap closure)', () => {
  test.beforeEach(async ({ page }) => {
    // Mock Service Worker
    await page.addInitScript(() => {
      Object.defineProperty(window.navigator, 'serviceWorker', {
        value: {
          register: () => Promise.resolve({}),
          addEventListener: () => {},
          removeEventListener: () => {},
          getRegistrations: () => Promise.resolve([])
        },
        writable: false,
        configurable: true
      });
    });

    await page.route('**/api/**', async (route) => {
      const request = route.request();
      const url = request.url();
      const method = request.method();

      if (url.includes('/api/auth/me')) {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({
            authenticated: true,
            setup_required: false,
            csrf_token: 'mock-csrf-token'
          })
        });
      } else if (url.includes('/api/capabilities')) {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({
            success: true,
            data: {
              kernels: {
                xray: { installed: false, version: '', channel: 'stable' },
                mihomo: { installed: true, version: '1.18.0', channel: 'stable' }
              },
              active_kernel: 'mihomo',
              mihomo: {
                reachable: true,
                process_running: true,
                api_reachable: true,
                api_authenticated: true
              }
            }
          })
        });
      } else if (url.includes('/api/mihomo/proxy/proxies') && method === 'GET') {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify(MOCK_PROXIES_MIXED)
        });
      } else if (url.includes('/api/mihomo/proxy/connections')) {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({ connections: [], total: 0 })
        });
      } else {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({ success: true })
        });
      }
    });

    await page.goto('/#/proxies');
    await page.waitForSelector('.group-card', { timeout: 10000 });
  });

  test('единая классификация: непроверенный живой узел не выглядит таймаутом в бейдже группы', async ({
    page
  }) => {
    const card = page.locator('.group-card').filter({ hasText: 'Untested' }).first();

    const latBox = card.locator('.gc-lat-box').first();
    await expect(latBox).toBeVisible();
    const classAttr = (await latBox.getAttribute('class')) || '';
    expect(classAttr).toMatch(/\bdim\b/);
    expect(classAttr).not.toMatch(/\bbad\b/);
    await expect(latBox).toHaveText('—');

    const pillTrigger = card.locator('.gc-now-pill-trigger').first();
    const pillClass = (await pillTrigger.getAttribute('class')) || '';
    expect(pillClass).not.toMatch(/\blat-bad\b/);
  });
});
