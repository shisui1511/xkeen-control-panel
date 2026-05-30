import { test, expect } from '@playwright/test';

// Используем русский язык для тестов интерфейса
test.use({ locale: 'ru-RU' });

// Фикстура групп прокси для тестирования
const MOCK_PROXIES_RESPONSE = {
  proxies: {
    // Группа с 12 прокси (>8 — должна сворачиваться по умолчанию)
    LargeGroup: {
      name: 'LargeGroup',
      type: 'Selector',
      now: 'proxy-03',
      all: [
        'proxy-01', 'proxy-02', 'proxy-03', 'proxy-04',
        'proxy-05', 'proxy-06', 'proxy-07', 'proxy-08',
        'proxy-09', 'proxy-10', 'proxy-11', 'proxy-12'
      ],
      alive: true,
      history: [{ delay: 120, time: '2024-01-01T00:00:00Z' }]
    },
    // Группа с 4 прокси (<=8 — не сворачивается)
    SmallGroup: {
      name: 'SmallGroup',
      type: 'URLTest',
      now: 'fast-01',
      all: ['fast-01', 'fast-02', 'fast-03', 'fast-04'],
      alive: true,
      history: [{ delay: 60, time: '2024-01-01T00:00:00Z' }]
    },
    // Отдельные прокси для LargeGroup (разные задержки для сортировки)
    'proxy-01': {
      name: 'proxy-01',
      type: 'Shadowsocks',
      alive: true,
      history: [{ delay: 200, time: '2024-01-01T00:00:00Z' }]
    },
    'proxy-02': {
      name: 'proxy-02',
      type: 'Shadowsocks',
      alive: true,
      history: [{ delay: 350, time: '2024-01-01T00:00:00Z' }]
    },
    'proxy-03': {
      name: 'proxy-03',
      type: 'Shadowsocks',
      alive: true,
      history: [{ delay: 45, time: '2024-01-01T00:00:00Z' }]
    },
    'proxy-04': {
      name: 'proxy-04',
      type: 'Shadowsocks',
      alive: true,
      history: [{ delay: 500, time: '2024-01-01T00:00:00Z' }]
    },
    'proxy-05': {
      name: 'proxy-05',
      type: 'Shadowsocks',
      alive: true,
      history: [{ delay: 150, time: '2024-01-01T00:00:00Z' }]
    },
    'proxy-06': {
      name: 'proxy-06',
      type: 'Shadowsocks',
      alive: false,
      history: [{ delay: 800, time: '2024-01-01T00:00:00Z' }]
    },
    'proxy-07': {
      name: 'proxy-07',
      type: 'Shadowsocks',
      alive: true,
      history: [{ delay: 620, time: '2024-01-01T00:00:00Z' }]
    },
    'proxy-08': {
      name: 'proxy-08',
      type: 'Shadowsocks',
      alive: true,
      history: [{ delay: 300, time: '2024-01-01T00:00:00Z' }]
    },
    'proxy-09': {
      name: 'proxy-09',
      type: 'Shadowsocks',
      alive: true,
      history: [{ delay: 410, time: '2024-01-01T00:00:00Z' }]
    },
    'proxy-10': {
      name: 'proxy-10',
      type: 'Shadowsocks',
      alive: true,
      history: [{ delay: 555, time: '2024-01-01T00:00:00Z' }]
    },
    'proxy-11': {
      name: 'proxy-11',
      type: 'Shadowsocks',
      alive: true,
      history: [{ delay: 720, time: '2024-01-01T00:00:00Z' }]
    },
    'proxy-12': {
      name: 'proxy-12',
      type: 'Shadowsocks',
      alive: false,
      history: [{ delay: 0, time: '2024-01-01T00:00:00Z' }]
    },
    // Прокси для SmallGroup
    'fast-01': {
      name: 'fast-01',
      type: 'Shadowsocks',
      alive: true,
      history: [{ delay: 30, time: '2024-01-01T00:00:00Z' }]
    },
    'fast-02': {
      name: 'fast-02',
      type: 'Shadowsocks',
      alive: true,
      history: [{ delay: 55, time: '2024-01-01T00:00:00Z' }]
    },
    'fast-03': {
      name: 'fast-03',
      type: 'Shadowsocks',
      alive: true,
      history: [{ delay: 80, time: '2024-01-01T00:00:00Z' }]
    },
    'fast-04': {
      name: 'fast-04',
      type: 'Shadowsocks',
      alive: true,
      history: [{ delay: 110, time: '2024-01-01T00:00:00Z' }]
    }
  }
};

test.describe('Proxies layout (Phase 9.2) — D-03, D-05, D-07, D-08, D-11/D-12', () => {

  test.beforeEach(async ({ page }) => {
    // Логируем консоль браузера для отладки
    page.on('console', msg => {
      console.log(`BROWSER [${msg.type()}]: ${msg.text()}`);
    });
    page.on('pageerror', err => {
      console.log(`BROWSER ERROR: ${err.message}`);
    });

    // Мокаем Service Worker, чтобы избежать JS-ошибок
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

    // Перехватываем все API-запросы
    await page.route('**/api/**', async (route) => {
      const url = route.request().url();

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
      } else if (url.includes('/api/mihomo/proxy/proxies') && !url.includes('/delay')) {
        // Возвращаем фикстуру с группами
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify(MOCK_PROXIES_RESPONSE)
        });
      } else if (url.includes('/delay')) {
        // Мокаем тест задержки для отдельного прокси
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({ delay: 100 })
        });
      } else if (url.includes('/api/mihomo/proxy/connections')) {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({ connections: [], total: 0 })
        });
      } else {
        // Все остальные API-запросы возвращают 200 с пустым результатом
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({ success: true })
        });
      }
    });

    // Переходим на страницу прокси
    await page.goto('/#/proxies');
    // Ждём появления первой группы или контейнера прокси
    await page.waitForSelector('.group-card, .proxies-page, .ph-actions', { timeout: 10000 });
  });

  // D-03: Collapse-by-default — группа с >8 прокси свёрнута по умолчанию
  test('D-03: большая группа (>8 прокси) свёрнута по умолчанию', async ({ page }) => {
    // Группа LargeGroup имеет 12 прокси и должна быть свёрнута
    // В свёрнутом состоянии видно не более 4 строк прокси (active + top-3 по задержке)
    const largeGroup = page.locator('.group-card').filter({ hasText: 'LargeGroup' }).first();
    await expect(largeGroup).toBeVisible();

    const proxyRows = largeGroup.locator('.proxy-row');
    const count = await proxyRows.count();
    // Свёрнутая группа показывает <= 4 прокси (active + top-3)
    expect(count).toBeLessThanOrEqual(4);

    // Должен присутствовать элемент .more-hint с подсказкой «...ещё N»
    const moreHint = largeGroup.locator('.more-hint');
    await expect(moreHint).toBeVisible();
  });

  // D-05: Toggle — клик по gc-head разворачивает/сворачивает группу
  test('D-05: клик по gc-head разворачивает и сворачивает LargeGroup', async ({ page }) => {
    const largeGroup = page.locator('.group-card').filter({ hasText: 'LargeGroup' }).first();
    const gcHead = largeGroup.locator('.gc-head').first();

    // Изначально свёрнуто — <= 4 строк
    const initialCount = await largeGroup.locator('.proxy-row').count();
    expect(initialCount).toBeLessThanOrEqual(4);

    // Кликаем по заголовку — группа должна развернуться
    await gcHead.click();
    await page.waitForTimeout(300); // ждём CSS-transition

    const expandedCount = await largeGroup.locator('.proxy-row').count();
    // После разворачивания видны все 12 прокси
    expect(expandedCount).toBe(12);

    // Кликаем снова — группа сворачивается
    await gcHead.click();
    await page.waitForTimeout(300);

    const collapsedCount = await largeGroup.locator('.proxy-row').count();
    expect(collapsedCount).toBeLessThanOrEqual(4);
  });

  // D-07: Клик по .more-hint разворачивает группу
  test('D-07: клик по .more-hint разворачивает свёрнутую группу', async ({ page }) => {
    const largeGroup = page.locator('.group-card').filter({ hasText: 'LargeGroup' }).first();

    // Изначально свёрнуто
    const moreHint = largeGroup.locator('.more-hint');
    await expect(moreHint).toBeVisible();

    // Кликаем по .more-hint
    await moreHint.click();
    await page.waitForTimeout(300); // ждём раскрытия

    // После клика группа должна развернуться — все 12 прокси видны
    const expandedCount = await largeGroup.locator('.proxy-row').count();
    expect(expandedCount).toBe(12);
  });

  // D-08: Compact padding — .proxy-row имеет padding-top: 4px
  test('D-08: .proxy-row имеет компактный padding-top 4px', async ({ page }) => {
    const largeGroup = page.locator('.group-card').filter({ hasText: 'LargeGroup' }).first();

    // Разворачиваем группу, чтобы получить доступ к строкам прокси
    await largeGroup.locator('.gc-head').first().click();
    await page.waitForTimeout(300);

    // Получаем computed style первой строки прокси
    const paddingTop = await largeGroup.locator('.proxy-row').first().evaluate((el) => {
      return window.getComputedStyle(el).paddingTop;
    });

    expect(paddingTop).toBe('4px');
  });

  // D-11/D-12: Поиск по имени группы — фильтрация и скрытие несовпавших
  test('D-11/D-12: поиск по имени группы скрывает несовпадающие группы', async ({ page }) => {
    // Оба group-card должны быть видны изначально
    const allCards = page.locator('.group-card');
    await expect(allCards).toHaveCount(2);

    const largeCard = page.locator('.group-card').filter({ hasText: 'LargeGroup' }).first();
    const smallCard = page.locator('.group-card').filter({ hasText: 'SmallGroup' }).first();

    await expect(largeCard).toBeVisible();
    await expect(smallCard).toBeVisible();

    // Вводим "Large" в поле поиска
    const searchInput = page.locator('input.group-search');
    await searchInput.fill('Large');

    // LargeGroup должна остаться видимой, SmallGroup — скрыться
    await expect(largeCard).toBeVisible();
    const smallDisplay = await smallCard.evaluate((el) => window.getComputedStyle(el).display);
    expect(smallDisplay).toBe('none');

    // Очищаем поле поиска — обе группы снова видны
    await searchInput.fill('');
    await expect(largeCard).toBeVisible();
    await expect(smallCard).toBeVisible();
  });

});
