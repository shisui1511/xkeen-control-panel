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
        'proxy-01',
        'proxy-02',
        'proxy-03',
        'proxy-04',
        'proxy-05',
        'proxy-06',
        'proxy-07',
        'proxy-08',
        'proxy-09',
        'proxy-10',
        'proxy-11',
        'proxy-12'
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
    },
    // Core routing (D-01/D-02/D-03/D-04) fixture groups
    GLOBAL: {
      name: 'GLOBAL',
      type: 'Selector',
      now: 'YouTube',
      all: ['YouTube', 'QUIC', 'Заблок. сервисы'],
      alive: true,
      history: [{ delay: 50, time: '2024-01-01T00:00:00Z' }]
    },
    'Заблок. сервисы': {
      name: 'Заблок. сервисы',
      type: 'Selector',
      now: 'sp',
      all: ['sp', 'sp2'],
      alive: true,
      history: [{ delay: 60, time: '2024-01-01T00:00:00Z' }]
    },
    YouTube: {
      name: 'YouTube',
      type: 'Selector',
      now: 'Заблок. сервисы',
      all: ['Заблок. сервисы', 'sp'],
      alive: true,
      history: [{ delay: 70, time: '2024-01-01T00:00:00Z' }]
    },
    QUIC: {
      name: 'QUIC',
      type: 'Selector',
      now: 'REJECT',
      all: ['REJECT'],
      alive: true,
      history: [{ delay: 0, time: '2024-01-01T00:00:00Z' }]
    },
    sp: {
      name: 'sp',
      type: 'Shadowsocks',
      alive: true,
      history: [{ delay: 80, time: '2024-01-01T00:00:00Z' }]
    },
    sp2: {
      name: 'sp2',
      type: 'Shadowsocks',
      alive: true,
      history: [{ delay: 90, time: '2024-01-01T00:00:00Z' }]
    },
    REJECT: {
      name: 'REJECT',
      type: 'Reject',
      alive: true,
      history: [{ delay: 0, time: '2024-01-01T00:00:00Z' }]
    }
  }
};

test.describe('Proxies layout (Phase 9.2) — D-03, D-05, D-07, D-08, D-11/D-12', () => {
  test.beforeEach(async ({ page }) => {
    // Логируем консоль браузера для отладки
    page.on('console', (msg) => {
      console.log(`BROWSER [${msg.type()}]: ${msg.text()}`);
    });
    page.on('pageerror', (err) => {
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
    const largeGroup = page.locator('.group-card').filter({ hasText: 'LargeGroup' }).first();
    await expect(largeGroup).toBeVisible();

    // Свёрнутая группа показывает health-bar и не показывает proxy-grid
    const healthBar = largeGroup.locator('.health-bar');
    await expect(healthBar).toBeVisible();

    const proxyGrid = largeGroup.locator('.proxy-grid');
    await expect(proxyGrid).toBeHidden();
  });

  // D-05: Toggle — клик по gc-head разворачивает/сворачивает группу
  test('D-05: клик по gc-head разворачивает и сворачивает LargeGroup', async ({ page }) => {
    const largeGroup = page.locator('.group-card').filter({ hasText: 'LargeGroup' }).first();
    const gcHead = largeGroup.locator('.gc-head').first();

    // Изначально свёрнуто
    await expect(largeGroup.locator('.proxy-grid')).toBeHidden();

    // Кликаем по заголовку — группа должна развернуться
    await gcHead.click();

    // После разворачивания видны все 12 прокси-карточек
    await expect(largeGroup.locator('.proxy-card')).toHaveCount(12);
    await expect(largeGroup.locator('.proxy-grid')).toBeVisible();

    // Кликаем снова — группа сворачивается
    await gcHead.click();
    await expect(largeGroup.locator('.proxy-grid')).toBeHidden();
  });

  // D-07: Клик по прокси в Selector-группе переключает активный прокси
  test('D-07: клик по .dot-indicator в Selector-группе переключает активный прокси', async ({
    page
  }) => {
    const largeGroup = page.locator('.group-card').filter({ hasText: 'LargeGroup' }).first();
    const gcHead = largeGroup.locator('.gc-head').first();
    await gcHead.click();

    // Перехватываем PUT-запрос
    let putRequest: any = null;
    await page.route('**/api/mihomo/proxy/proxies/LargeGroup', async (route) => {
      if (route.request().method() === 'PUT') {
        putRequest = route.request().postDataJSON();
      }
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ success: true })
      });
    });

    // Кликаем по второй карточке (proxy-02)
    const cards = largeGroup.locator('.proxy-card');
    await cards.nth(1).click();

    expect(putRequest).not.toBeNull();
    expect(putRequest.name).toBe('proxy-02');
  });

  // D-08: Compact padding — .proxy-select-btn (содержимое .proxy-card) имеет padding-top: 10px
  test('D-08: .proxy-card имеет padding-top 10px', async ({ page }) => {
    const largeGroup = page.locator('.group-card').filter({ hasText: 'LargeGroup' }).first();

    // Разворачиваем группу
    await largeGroup.locator('.gc-head').first().click();
    await expect(largeGroup.locator('.proxy-card')).toHaveCount(12);

    // Padding теперь на .proxy-select-btn: .proxy-card — не-интерактивная обёртка
    // (padding: 0) с двумя дочерними кнопками (select + latency-test), а
    // сама компактная отбивка контента задана на кнопке выбора прокси.
    const paddingTop = await largeGroup
      .locator('.proxy-card .proxy-select-btn')
      .first()
      .evaluate((el: Element) => {
        return window.getComputedStyle(el).paddingTop;
      });

    expect(paddingTop).toBe('10px');
  });

  // D-11/D-12: Поиск по имени группы — фильтрация и скрытие несовпавших
  test('D-11/D-12: поиск по имени группы скрывает несовпадающие группы', async ({ page }) => {
    // 2 исходные группы (LargeGroup/SmallGroup) + 4 core-routing фикстуры
    // (GLOBAL, Заблок. сервисы, YouTube, QUIC) добавленные для core routing тестов
    const allCards = page.locator('.group-card');
    await expect(allCards).toHaveCount(6);

    const largeCard = page.locator('.group-card').filter({ hasText: 'LargeGroup' }).first();
    const smallCard = page.locator('.group-card').filter({ hasText: 'SmallGroup' }).first();

    await expect(largeCard).toBeVisible();
    await expect(smallCard).toBeVisible();

    // Вводим "Large" в поле поиска
    const searchInput = page.locator('input.group-search');
    await searchInput.fill('Large');

    // LargeGroup должна остаться видимой, SmallGroup — скрыться из DOM/стать скрытой
    await expect(largeCard).toBeVisible();
    await expect(smallCard).toBeHidden();

    // Очищаем поле поиска — обе группы снова видны
    await searchInput.fill('');
    await expect(largeCard).toBeVisible();
    await expect(smallCard).toBeVisible();
  });

  // D-01/D-03: группы разделены на секции Core/Service/System
  test('core routing: группы разделены на секции', async ({ page }) => {
    await expect(page.locator('.proxy-section-core [data-group="GLOBAL"]')).toBeVisible();
    await expect(page.locator('.proxy-section-core [data-group="Заблок. сервисы"]')).toBeVisible();
    await expect(page.locator('.proxy-section-service [data-group="YouTube"]')).toBeVisible();
    await expect(page.locator('.proxy-section-system [data-group="QUIC"]')).toBeVisible();
  });

  // Pitfall 1 / D-02: .gc-head — div[role=button], вложенный breadcrumb-чипс
  // кликабелен и не сворачивает карточку
  test('core routing: шапка карточки не является кнопкой', async ({ page }) => {
    const ytCard = page.locator('[data-group="YouTube"]');
    const gcHead = ytCard.locator('.gc-head').first();

    expect(await gcHead.evaluate((el) => el.tagName)).toBe('DIV');

    const initialExpanded = await gcHead.getAttribute('aria-expanded');
    await gcHead.click();
    const afterHeadClick = await gcHead.getAttribute('aria-expanded');
    expect(afterHeadClick).not.toBe(initialExpanded);

    // Возвращаем в исходное состояние для чистоты следующей проверки
    await gcHead.click();
    expect(await gcHead.getAttribute('aria-expanded')).toBe(initialExpanded);

    const breadcrumbChip = ytCard.locator('.gc-now-pill-link').first();
    const beforeChipClick = await gcHead.getAttribute('aria-expanded');
    await breadcrumbChip.click();
    expect(await gcHead.getAttribute('aria-expanded')).toBe(beforeChipClick);

    const blockedCard = page.locator('[data-group="Заблок. сервисы"]');
    await expect(blockedCard).toHaveClass(/flash-highlight/);
  });

  // D-04: закрепление группы в Core переживает перезагрузку страницы
  test('core routing: закрепление группы переживает перезагрузку', async ({ page }) => {
    // Автоопределённая Core-группа (совпала с CORE_GROUP_PATTERNS) — кнопка
    // булавки неактивна, ручное открепление для неё не предусмотрено
    await expect(page.locator('[data-group="GLOBAL"] .gc-pin-btn')).toBeDisabled();

    await page.locator('[data-group="YouTube"] .gc-pin-btn').click();
    await expect(page.locator('.proxy-section-core [data-group="YouTube"]')).toBeVisible();

    await page.reload();
    await page.waitForSelector('.group-card, .proxies-page, .ph-actions', { timeout: 10000 });
    await expect(page.locator('.proxy-section-core [data-group="YouTube"]')).toBeVisible();
  });

  // D-03: служебная группа со статическим выходом отрисована мини-карточкой
  test('core routing: служебная группа отрисована мини-карточкой', async ({ page }) => {
    const quicCard = page.locator('[data-group="QUIC"]');
    await expect(quicCard).toHaveClass(/gc-mini/);
    await expect(quicCard).toHaveClass(/out-reject/);
    await expect(quicCard.locator('.proxy-grid')).toHaveCount(0);
  });
});
