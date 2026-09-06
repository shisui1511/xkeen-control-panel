import { test, expect } from '@playwright/test';

// Используем русский язык для тестов интерфейса
test.use({ locale: 'ru-RU' });

const hugeNodes: string[] = [];
const hugeProxies: Record<string, any> = {};
for (let i = 1; i <= 120; i++) {
  const name = `huge-${String(i).padStart(3, '0')}`;
  hugeNodes.push(name);
  hugeProxies[name] = {
    name,
    type: 'Shadowsocks',
    alive: true,
    history: [{ delay: 50 + (i % 200), time: '2024-01-01T00:00:00Z' }]
  };
}

// Фикстура групп прокси для тестирования
const MOCK_PROXIES_RESPONSE = {
  proxies: {
    HugeGroup: {
      name: 'HugeGroup',
      type: 'Selector',
      now: 'huge-001',
      all: hugeNodes,
      alive: true,
      history: [{ delay: 60, time: '2024-01-01T00:00:00Z' }]
    },
    ...hugeProxies,
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
      } else if (url.includes('/api/mihomo/proxy/providers/proxies')) {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({
            providers: {
              MySubscription: {
                name: 'MySubscription',
                proxies: [
                  { name: 'fast-01' },
                  { name: 'fast-02' },
                  { name: 'fast-03' },
                  { name: 'fast-04' }
                ]
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
    // 3 исходные группы (LargeGroup/SmallGroup/HugeGroup) + 4 core-routing/service фикстуры
    // (GLOBAL, Заблок. сервисы, YouTube, QUIC)
    const allCards = page.locator('.group-card');
    await expect(allCards).toHaveCount(7);

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

  // D-07: поиск фильтрует обе секции и показывает счётчики
  test('view sections: поиск фильтрует обе секции и показывает счётчики', async ({ page }) => {
    const searchInput = page.locator('input.group-search');
    await searchInput.fill('YouTube');
    await page.waitForTimeout(300);

    await expect(page.locator('.proxy-section-service [data-group="YouTube"]')).toBeVisible();
    await expect(page.locator('.proxy-section-service [data-group="Telegram"]')).toHaveCount(0);
    const serviceCount = page.locator('.proxy-section-service .proxy-section-count');
    await expect(serviceCount).toContainText(/1\s+группа/);
  });

  // D-07: пустой результат поиска показывает одно сообщение
  test('view sections: пустой результат поиска показывает одно сообщение', async ({ page }) => {
    const searchInput = page.locator('input.group-search');
    await searchInput.fill('zzzz-not-found');
    await page.waitForTimeout(300);

    const emptyState = page.locator('.search-empty-state');
    await expect(emptyState).toBeVisible();
    await expect(emptyState).toContainText('По запросу ничего не найдено');
    await expect(page.locator('.group-card')).toHaveCount(0);
  });

  // D-02: длинная цепочка маршрута сокращается с тултипом
  test('view sections: длинная цепочка маршрута сокращается', async ({ page }) => {
    const globalCard = page.locator('[data-group="GLOBAL"]');
    await expect(globalCard).toBeVisible();
    const ellipsis = globalCard.locator('.gc-chain-ellipsis');
    await expect(ellipsis).toBeVisible();
    await expect(ellipsis).toHaveAttribute('title', 'YouTube › Заблок. сервисы › sp');

    const chip = globalCard.locator('.gc-now-pill-link').first();
    await expect(chip).toBeVisible();
    const gcHead = globalCard.locator('.gc-head').first();
    const beforeClick = await gcHead.getAttribute('aria-expanded');
    await chip.click();
    expect(await gcHead.getAttribute('aria-expanded')).toBe(beforeClick);
  });

  // D-07: микро-бейдж провайдера виден для группы из подписки
  test('view sections: микро-бейдж провайдера виден для группы из подписки', async ({ page }) => {
    const smallCard = page.locator('[data-group="SmallGroup"]');
    await expect(smallCard).toBeVisible();
    const badge = smallCard.locator('.gc-provider-badge');
    await expect(badge).toBeVisible();
    await expect(badge).toContainText('MySubscription');
  });

  // D-17: переключатель режимов сетки/списка и сохранение выбора
  test('view mode: переключатель сохраняет выбор между перезагрузками', async ({ page }) => {
    const gridBtn = page.locator('.view-toggle-btn[data-view="grid"]');
    const listBtn = page.locator('.view-toggle-btn[data-view="list"]');

    await expect(gridBtn).toHaveAttribute('aria-pressed', 'true');
    await expect(listBtn).toHaveAttribute('aria-pressed', 'false');

    await listBtn.click();
    await expect(listBtn).toHaveAttribute('aria-pressed', 'true');
    await expect(gridBtn).toHaveAttribute('aria-pressed', 'false');
    await expect(page.locator('.proxy-section-service .group-grid')).toHaveClass(/group-list/);

    // Перезагрузка страницы восстанавливает сохранённый режим списка
    await page.reload();
    await page.waitForSelector('.group-card, .proxies-page, .ph-actions', { timeout: 10000 });
    await expect(page.locator('.view-toggle-btn[data-view="list"]')).toHaveAttribute(
      'aria-pressed',
      'true'
    );
    await expect(page.locator('.proxy-section-service .group-grid')).toHaveClass(/group-list/);

    // Повреждённое значение в localStorage безопасно откатывается к сетке
    await page.evaluate(() => localStorage.setItem('proxies_view_mode', '{"a":1}'));
    await page.reload();
    await page.waitForSelector('.group-card, .proxies-page, .ph-actions', { timeout: 10000 });
    await expect(page.locator('.view-toggle-btn[data-view="grid"]')).toHaveAttribute(
      'aria-pressed',
      'true'
    );
  });

  // D-18: строка списка занимает 40px на десктопе
  test('view mode: строка списка занимает 40px', async ({ page }) => {
    await page.locator('.view-toggle-btn[data-view="list"]').click();
    const head = page.locator('.group-list [data-group="YouTube"] .gc-head');
    await expect(head).toBeVisible();
    const box = await head.boundingBox();
    expect(box?.height).toBe(40);
  });

  // D-19: клик по строке списка раскрывает суб-сетку узлов
  test('view mode: клик по строке раскрывает суб-сетку узлов', async ({ page }) => {
    await page.locator('.view-toggle-btn[data-view="list"]').click();
    const groupCard = page.locator('.group-list [data-group="YouTube"]');
    const head = groupCard.locator('.gc-head');

    // Клик по шапке раскрывает суб-сетку
    await head.click();
    await expect(groupCard.locator('.proxy-grid .proxy-card').first()).toBeVisible();

    // Клик по кнопке-шеврону сворачивает суб-сетку
    const chevronBtn = groupCard.locator('.gc-chevron-btn');
    await chevronBtn.click();
    await expect(groupCard.locator('.proxy-grid')).toHaveCount(0);
  });

  // D-18 / D-19: интерактивные элементы работают в режиме списка
  test('view mode: интерактивные элементы работают в режиме списка', async ({ page }) => {
    await page.locator('.view-toggle-btn[data-view="list"]').click();
    const groupCard = page.locator('.group-list [data-group="YouTube"]');
    const head = groupCard.locator('.gc-head');

    // Quick-select по клику на плашку не меняет состояние аккордеона
    const nowPill = groupCard.locator('.gc-now-pill-trigger');
    await nowPill.click();
    await expect(page.locator('.qs-popover')).toBeVisible();
    await expect(head).toHaveAttribute('aria-expanded', 'false');
    await page.keyboard.press('Escape');
    await expect(page.locator('.qs-popover')).toHaveCount(0);

    // Микро-пинг группы отправляет запрос задержки
    let pingRequested = false;
    await page.route('**/api/mihomo/proxy/group/**/delay**', async (route) => {
      pingRequested = true;
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          'Заблок. сервисы': 45,
          sp: 80
        })
      });
    });
    const pingBtn = groupCard.locator('.gc-ping-btn');
    await pingBtn.click();
    expect(pingRequested).toBe(true);

    // Полоса здоровья показывает тултип
    const healthBar = groupCard.locator('.health-bar');
    await healthBar.hover();
    const tooltip = page.locator('.health-tooltip');
    await expect(tooltip).toBeVisible();
    await expect(tooltip).toContainText(/Доступно: \d+ \(\d+%\)/);
  });

  // D-18: мобильная высота строки списка равна 44px
  test('view mode: мобильная высота строки списка равна 44px', async ({ page }) => {
    await page.setViewportSize({ width: 375, height: 667 });
    await page.locator('.view-toggle-btn[data-view="list"]').click();
    const head = page.locator('.group-list [data-group="YouTube"] .gc-head');
    await expect(head).toBeVisible();
    const box = await head.boundingBox();
    expect(box?.height).toBe(44);
  });

  // D-20: большая группа отрисовывается порциями
  test('view mode: большая группа отрисовывается порциями', async ({ page }) => {
    const hugeCard = page.locator('[data-group="HugeGroup"]');
    await hugeCard.locator('.gc-head').click();

    const cards = hugeCard.locator('.proxy-card');
    await expect(cards).toHaveCount(50);

    const moreBtn = hugeCard.locator('.proxy-grid-more');
    await expect(moreBtn).toBeVisible();
    await expect(moreBtn).toContainText(/Показать ещё\s+\d+\s+узл/);

    await moreBtn.click();
    await expect(cards).toHaveCount(100);

    await moreBtn.click();
    await expect(cards).toHaveCount(120);
    await expect(moreBtn).toHaveCount(0);
  });

  // D-20: свёртывание группы сбрасывает лимит отрисовки
  test('view mode: свёртывание группы сбрасывает лимит отрисовки', async ({ page }) => {
    const hugeCard = page.locator('[data-group="HugeGroup"]');
    await hugeCard.locator('.gc-head').click();
    await hugeCard.locator('.proxy-grid-more').click();
    await expect(hugeCard.locator('.proxy-card')).toHaveCount(100);

    // Сворачиваем
    await hugeCard.locator('.gc-head').click();
    await expect(hugeCard.locator('.proxy-card')).toHaveCount(0);

    // Раскрываем снова — count возвращается к первой порции (50)
    await hugeCard.locator('.gc-head').click();
    await expect(hugeCard.locator('.proxy-card')).toHaveCount(50);
  });

  // D-20: «Развернуть все» не выводит всю простыню узлов сразу
  test('view mode: «Развернуть все» не выводит всю простыню узлов сразу', async ({ page }) => {
    const expandAllBtn = page.locator('.ph-actions button[title="Развернуть все"]');
    await expandAllBtn.click();

    // Даем время таймерам пачек отработать
    await page.waitForTimeout(300);

    const totalCards = await page.locator('.proxy-card').count();
    const totalGroups = await page.locator('.group-card:not(.gc-mini)').count();
    expect(totalCards).toBeLessThanOrEqual(totalGroups * 50);
  });
});
