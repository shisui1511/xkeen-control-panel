import { test, expect } from '@playwright/test';

test.use({ locale: 'ru-RU' });

// Фикстура для регрессии плашки «Не проверено» как четвёртого фильтра пульта
// (Phase 90 gap closure, G-90-4). Обе группы попадают в ведро 'service': их
// имена не совпадают ни с одним CORE_GROUP_PATTERNS и не ссылаются друг на
// друга через all[]. Категория «Не проверено» содержит ровно один узел
// (Fresh-Node-01), принадлежащий только группе «Медиа». Ни один узел не
// попадает в ведро средней задержки — фильтр «С задержкой» гарантированно
// не оставляет ни одной группы, что используется для проверки пустого
// состояния (Тест 3).
const MOCK_PROXIES_OBS = {
  proxies: {
    Медиа: {
      name: 'Медиа',
      type: 'Selector',
      now: 'Fresh-Node-01',
      all: ['Fresh-Node-01', 'Quick-Node-02'],
      alive: true
    },
    Игры: {
      name: 'Игры',
      type: 'Selector',
      now: 'Quick-Node-02',
      all: ['Quick-Node-02', 'Dead-Node-03'],
      alive: true
    },
    // Непроверенный живой узел: alive: true, но без history и без delay.
    'Fresh-Node-01': {
      name: 'Fresh-Node-01',
      type: 'Vless',
      alive: true
    },
    'Quick-Node-02': {
      name: 'Quick-Node-02',
      type: 'Vless',
      alive: true,
      history: [{ delay: 120, time: '' }]
    },
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

// Вариант ответа для Теста 4: Fresh-Node-01 уже измерен (замеры пришли),
// категория «Не проверено» падает до нуля, но фильтр остаётся активным.
const MOCK_PROXIES_OBS_MEASURED = {
  proxies: {
    ...MOCK_PROXIES_OBS.proxies,
    'Fresh-Node-01': {
      name: 'Fresh-Node-01',
      type: 'Vless',
      alive: true,
      history: [{ delay: 90, time: '' }]
    }
  }
};

test.describe('Плашка «Не проверено» как фильтр пульта (Phase 90 gap closure, G-90-4)', () => {
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

    let proxiesResponse: unknown = MOCK_PROXIES_OBS;

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
          body: JSON.stringify(proxiesResponse)
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

    // Хук для Теста 4: заменяет ответ на "уже измерено" после первого клика.
    (page as unknown as { __setMeasured: () => void }).__setMeasured = () => {
      proxiesResponse = MOCK_PROXIES_OBS_MEASURED;
    };

    await page.goto('/#/proxies');
    await page.waitForSelector('.group-card', { timeout: 10000 });
  });

  test('плашка «Не проверено» кликабельна и фильтрует список групп', async ({ page }) => {
    await expect(page.locator('.group-card[data-group="Медиа"]')).toBeVisible();
    await expect(page.locator('.group-card[data-group="Игры"]')).toBeVisible();

    const uncheckedBox = page.locator('.obs-stat-box').filter({ hasText: 'Не проверено' }).first();
    await expect(uncheckedBox.locator('.stat-value')).toHaveText('1');
    const tagName = await uncheckedBox.evaluate((el) => el.tagName);
    expect(tagName).toBe('BUTTON');
    await expect(uncheckedBox).toHaveAttribute('aria-pressed', 'false');

    await uncheckedBox.click();
    await expect(uncheckedBox).toHaveAttribute('aria-pressed', 'true');
    await expect(page.locator('.group-card[data-group="Медиа"]')).toBeVisible();
    await expect(page.locator('.group-card[data-group="Игры"]')).toHaveCount(0);

    await uncheckedBox.click();
    await expect(uncheckedBox).toHaveAttribute('aria-pressed', 'false');
    await expect(page.locator('.group-card[data-group="Медиа"]')).toBeVisible();
    await expect(page.locator('.group-card[data-group="Игры"]')).toBeVisible();
  });

  test('фильтры пульта взаимно исключают друг друга', async ({ page }) => {
    const uncheckedBox = page.locator('.obs-stat-box').filter({ hasText: 'Не проверено' }).first();
    const downBox = page.locator('.obs-stat-box').filter({ hasText: 'Недоступны' }).first();

    await uncheckedBox.click();
    await downBox.click();

    await expect(uncheckedBox).toHaveAttribute('aria-pressed', 'false');
    await expect(downBox).toHaveAttribute('aria-pressed', 'true');
    await expect(page.locator('.group-card[data-group="Игры"]')).toBeVisible();
    await expect(page.locator('.group-card[data-group="Медиа"]')).toHaveCount(0);
  });

  test('фильтр без совпадений показывает текстовое сообщение вместо пустой области', async ({
    page
  }) => {
    const degradedBox = page.locator('.obs-stat-box').filter({ hasText: 'Высокий пинг' }).first();

    await degradedBox.click();
    await expect(page.locator('.group-card')).toHaveCount(0);
    const emptyState = page.locator('.search-empty-state');
    await expect(emptyState).toBeVisible();
    await expect(emptyState).not.toBeEmpty();

    await degradedBox.click();
    await expect(page.locator('.group-card[data-group="Медиа"]')).toBeVisible();
    await expect(page.locator('.group-card[data-group="Игры"]')).toBeVisible();
    await expect(emptyState).toHaveCount(0);
  });

  test('плашка не исчезает, пока её фильтр активен', async ({ page }) => {
    test.setTimeout(60000);

    const uncheckedBox = page.locator('.obs-stat-box').filter({ hasText: 'Не проверено' }).first();
    await uncheckedBox.click();
    await expect(uncheckedBox).toHaveAttribute('aria-pressed', 'true');

    (page as unknown as { __setMeasured: () => void }).__setMeasured();

    // Интервал фонового опроса страницы — 10 секунд; таймаут poll даёт запас
    // на случай замедления под общей нагрузкой при параллельном прогоне.
    await expect
      .poll(
        async () => {
          const box = page.locator('.obs-stat-box').filter({ hasText: 'Не проверено' }).first();
          if ((await box.count()) === 0) return null;
          return box.locator('.stat-value').textContent();
        },
        { timeout: 30000, intervals: [500] }
      )
      .toBe('0');

    const uncheckedBoxAfter = page
      .locator('.obs-stat-box')
      .filter({ hasText: 'Не проверено' })
      .first();
    await expect(uncheckedBoxAfter).toBeVisible();
    await expect(uncheckedBoxAfter).toHaveAttribute('aria-pressed', 'true');

    await uncheckedBoxAfter.click();
    await expect(page.locator('.group-card[data-group="Медиа"]')).toBeVisible();
    await expect(page.locator('.group-card[data-group="Игры"]')).toBeVisible();
  });
});
