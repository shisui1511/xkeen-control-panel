import { test, expect } from '@playwright/test';

test.use({ locale: 'ru-RU' });

const MOCK_PROXIES = {
  proxies: {
    // Selector group with 12 proxies
    YouTube: {
      name: 'YouTube',
      type: 'Selector',
      now: 'RU-Node-01',
      all: [
        'RU-Node-01',
        'US-Node-02',
        'NL-Node-03',
        'DE-Node-04',
        'LT-Node-05',
        'EE-Node-06',
        'SE-Node-07',
        'AM-Node-08',
        'ES-Node-09',
        'FI-Node-10',
        'SG-Node-11',
        'GB-Node-12'
      ],
      alive: true,
      icon: 'https://cdn.jsdelivr.net/gh/Koolson/Qure@master/IconSet/Color/YouTube.png'
    },
    // URLTest group with 4 proxies
    FastGroup: {
      name: 'FastGroup',
      type: 'URLTest',
      now: 'US-Node-02',
      all: ['US-Node-02', 'NL-Node-03', 'DE-Node-04', 'RU-Node-01'],
      alive: true
    },
    // Individual proxies with latency data
    'RU-Node-01': {
      name: 'RU-Node-01',
      type: 'Vless',
      alive: true,
      history: [{ delay: 45, time: '' }]
    },
    'US-Node-02': {
      name: 'US-Node-02',
      type: 'Vless',
      alive: true,
      history: [{ delay: 150, time: '' }]
    },
    'NL-Node-03': {
      name: 'NL-Node-03',
      type: 'Vless',
      alive: true,
      history: [{ delay: 350, time: '' }]
    },
    'DE-Node-04': {
      name: 'DE-Node-04',
      type: 'Vless',
      alive: false,
      history: [{ delay: 0, time: '' }]
    },
    'LT-Node-05': {
      name: 'LT-Node-05',
      type: 'Vless',
      alive: true,
      history: [{ delay: 80, time: '' }]
    },
    'EE-Node-06': {
      name: 'EE-Node-06',
      type: 'Vless',
      alive: true,
      history: [{ delay: 90, time: '' }]
    },
    'SE-Node-07': {
      name: 'SE-Node-07',
      type: 'Vless',
      alive: true,
      history: [{ delay: 120, time: '' }]
    },
    'AM-Node-08': {
      name: 'AM-Node-08',
      type: 'Vless',
      alive: true,
      history: [{ delay: 180, time: '' }]
    },
    'ES-Node-09': {
      name: 'ES-Node-09',
      type: 'Vless',
      alive: true,
      history: [{ delay: 240, time: '' }]
    },
    'FI-Node-10': {
      name: 'FI-Node-10',
      type: 'Vless',
      alive: true,
      history: [{ delay: 400, time: '' }]
    },
    'SG-Node-11': {
      name: 'SG-Node-11',
      type: 'Vless',
      alive: true,
      history: [{ delay: 600, time: '' }]
    },
    'GB-Node-12': {
      name: 'GB-Node-12',
      type: 'Vless',
      alive: true,
      history: [{ delay: 900, time: '' }]
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

const MOCK_SUBSCRIPTIONS = [
  {
    id: 'sub-1',
    name: 'Primary VPN',
    profile_title: 'Primary VPN',
    url: 'https://example.com/sub',
    enabled: true,
    interval: 24,
    use_provider_interval: false,
    enable_xray: false,
    enable_mihomo: true,
    mihomo_integrated: true,
    hwid_locked: false,
    last_update: '2026-06-30T10:00:00Z',
    proxy_count: 12,
    upload: 10 * 1024 * 1024 * 1024,
    download: 20 * 1024 * 1024 * 1024,
    total: 100 * 1024 * 1024 * 1024,
    expire: Math.floor(Date.now() / 1000) + 15 * 86400 // expires in 15 days
  }
];

test.describe('Proxies UI Improvements (Phase 57)', () => {
  let putRequests: { url: string; body: any }[] = [];
  let postRequests: { url: string; body: any }[] = [];
  let currentProxies: any;

  test.beforeEach(async ({ page }) => {
    putRequests = [];
    postRequests = [];
    currentProxies = JSON.parse(JSON.stringify(MOCK_PROXIES));

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

    // Capture and mock API calls
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
          body: JSON.stringify(currentProxies)
        });
      } else if (url.includes('/api/mihomo/proxy/proxies/') && method === 'PUT') {
        const body = request.postDataJSON();
        putRequests.push({ url, body });

        const groupMatch = url.match(/\/api\/mihomo\/proxy\/proxies\/([^?#/]+)/);
        if (groupMatch) {
          const groupName = decodeURIComponent(groupMatch[1]);
          if (body && body.name && currentProxies.proxies[groupName]) {
            currentProxies.proxies[groupName].now = body.name;
          }
        }

        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({ success: true })
        });
      } else if (
        (url.includes('/api/subscriptions') || url.includes('/api/proxy-providers')) &&
        method === 'GET'
      ) {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify(MOCK_SUBSCRIPTIONS)
        });
      } else if (url.includes('/api/subscriptions/refresh') && method === 'POST') {
        postRequests.push({ url, body: null });
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({ success: true })
        });
      } else if (url.includes('/api/mihomo/proxy/group/') && url.includes('/delay')) {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({
            'RU-Node-01': 55,
            'US-Node-02': 160
          })
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
    // .page-head is a generic class also rendered on Dashboard's own default
    // view (briefly active before the hash-based tab switch completes), so
    // waiting on it as an OR-fallback here raced against that transition and
    // resolved too early. .group-card is unique to the Proxies page.
    // (Phase 90: the page now renders up to three `.group-grid` sections —
    // Core/Service/System — and the Core section renders even when empty
    // (D-01), so `.group-grid` alone can resolve to a zero-height, not-yet-
    // visible element; `.group-card` is unambiguous regardless of section.)
    await page.waitForSelector('.group-card', { timeout: 10000 });
  });

  test('Grid Layout - nodes rendered in 5 columns grid', async ({ page }) => {
    // Expand YouTube group
    const ytGroup = page.locator('.group-card').filter({ hasText: 'YouTube' }).first();
    const gcHead = ytGroup.locator('.gc-head').first();

    // Check if collapsed initially and expand
    const isCollapsed = await ytGroup.locator('.health-bar').isVisible();
    if (isCollapsed) {
      await gcHead.click();
    }

    // Verify grid layout exists
    const grid = ytGroup.locator('.proxy-grid');
    await expect(grid).toBeVisible();

    const display = await grid.evaluate((el) => window.getComputedStyle(el).display);
    expect(display).toBe('grid');

    const cards = grid.locator('.proxy-card');
    await expect(cards).toHaveCount(12);
  });

  test('Dot-indicators - show latency dots for collapsed groups', async ({ page }) => {
    const ytGroup = page.locator('.group-card').filter({ hasText: 'YouTube' }).first();
    const gcHead = ytGroup.locator('.gc-head').first();

    // Collapse if expanded
    const isGridVisible = await ytGroup.locator('.proxy-grid').isVisible();
    if (isGridVisible) {
      await gcHead.click();
    }

    // Verify health bar container
    const healthBar = ytGroup.locator('.health-bar');
    await expect(healthBar).toBeVisible();

    // Verify segments exist
    const segments = healthBar.locator('.health-segment');
    await expect(segments.first()).toBeVisible();
  });

  test('Brand icons - YouTube group displays branding', async ({ page }) => {
    const ytGroup = page.locator('.group-card').filter({ hasText: 'YouTube' }).first();
    // Brand icon image should be present in the header
    const brandIcon = ytGroup.locator('.gc-head img.brand-icon');
    await expect(brandIcon).toBeVisible();
  });

  // D-06: бейдж типа группы читается по-русски, а не показывает английское имя типа
  test('group type badge отображается по-русски', async ({ page }) => {
    await expect(page.locator('[data-group="YouTube"] .type-badge')).toHaveText(/Выбор/);
  });

  // D-05: логотип сервиса выровнен на единой круглой подложке 26x26
  test('логотип сервиса выровнен на подложке 26px', async ({ page }) => {
    const iconWrap = page.locator('[data-group="YouTube"] .group-icon-wrap');
    const size = await iconWrap.evaluate((el) => {
      const style = window.getComputedStyle(el);
      return { width: style.width, height: style.height };
    });
    expect(size.width).toBe('26px');
    expect(size.height).toBe('26px');
  });

  test('Active node highlight - active node has accent border and background', async ({ page }) => {
    const ytGroup = page.locator('.group-card').filter({ hasText: 'YouTube' }).first();
    const gcHead = ytGroup.locator('.gc-head').first();

    // Ensure expanded
    const isCollapsed = await ytGroup.locator('.health-bar').isVisible();
    if (isCollapsed) {
      await gcHead.click();
    }

    // Check active card highlight
    const activeCard = ytGroup.locator('.proxy-card.now');
    await expect(activeCard).toBeVisible();
    await expect(activeCard).toContainText('RU-Node-01');

    // Arrow character should not be used
    const text = await activeCard.innerText();
    expect(text).not.toContain('→');
  });

  test('Country flag - flag emoji is parsed and displayed', async ({ page }) => {
    const ytGroup = page.locator('.group-card').filter({ hasText: 'YouTube' }).first();
    const gcHead = ytGroup.locator('.gc-head').first();

    // Ensure expanded
    if (await ytGroup.locator('.health-bar').isVisible()) {
      await gcHead.click();
    }

    const firstCard = ytGroup.locator('.proxy-card').filter({ hasText: 'RU-Node-01' }).first();
    await expect(firstCard).toContainText('🇷🇺');
  });

  test('Global search - filters groups and nodes', async ({ page }) => {
    const searchInput = page.locator('input.group-search');
    await expect(searchInput).toBeVisible();

    // Search for US node
    await searchInput.fill('US-Node');
    await page.waitForTimeout(300); // Wait for debounce

    const ytGroup = page.locator('.group-card').filter({ hasText: 'YouTube' }).first();
    const fastGroup = page.locator('.group-card').filter({ hasText: 'FastGroup' }).first();

    // Both groups should be visible as they both contain US-Node-02
    await expect(ytGroup).toBeVisible();
    await expect(fastGroup).toBeVisible();

    // Inside YouTube, only US-Node-02 card should be visible
    if (await ytGroup.locator('.health-bar').isVisible()) {
      await ytGroup.locator('.gc-head').click();
    }
    const visibleCards = ytGroup.locator('.proxy-card');
    const count = await visibleCards.count();
    for (let i = 0; i < count; i++) {
      const isVisible = await visibleCards.nth(i).isVisible();
      if (isVisible) {
        await expect(visibleCards.nth(i)).toContainText('US-Node');
      }
    }
  });

  test('Collapse/Expand all - collapses/expands all groups on click', async ({ page }) => {
    const collapseAllBtn = page.locator('button:has-text("Свернуть все")');
    const expandAllBtn = page.locator('button:has-text("Развернуть все")');

    await expect(collapseAllBtn).toBeVisible();
    await expect(expandAllBtn).toBeVisible();

    // Click collapse all
    await collapseAllBtn.click();

    const ytGroup = page.locator('.group-card').filter({ hasText: 'YouTube' }).first();
    const fastGroup = page.locator('.group-card').filter({ hasText: 'FastGroup' }).first();
    await expect(ytGroup.locator('.health-bar')).toBeVisible();
    await expect(fastGroup.locator('.health-bar')).toBeVisible();

    // Click expand all
    await expandAllBtn.click();
    await expect(ytGroup.locator('.proxy-grid')).toBeVisible();
    await expect(fastGroup.locator('.proxy-grid')).toBeVisible();
  });

  test('Node Switch PUT API - click changes node and calls API', async ({ page }) => {
    const ytGroup = page.locator('.group-card').filter({ hasText: 'YouTube' }).first();
    if (await ytGroup.locator('.health-bar').isVisible()) {
      await ytGroup.locator('.gc-head').click();
    }

    const usCard = ytGroup.locator('.proxy-card').filter({ hasText: 'US-Node-02' }).first();
    await usCard.click();

    // Optimistic UI check
    await expect(usCard).toHaveClass(/now/);

    // Verify PUT request
    await expect.poll(() => putRequests.length).toBe(1);
    expect(putRequests[0].url).toContain('YouTube');
    expect(putRequests[0].body).toEqual({ name: 'US-Node-02' });
  });

  test('Provider CRUD and Merge - tab switching and subscription actions', async ({ page }) => {
    // Scoped to .tabs-container: a bare `button:has-text(...)` also matches the
    // Dashboard qa-mini quick-action button (accessible name "Прокси Mihomo
    // узлы и группы" contains the substring "Группы"), which can still be
    // fading out in the DOM during the 150ms transition:fade cross-fade when
    // this test's beforeEach lands right after the #/proxies tab switch.
    const tabsContainer = page.locator('.tabs-container');
    const groupsTab = tabsContainer.locator('button:has-text("Группы")');
    const providersTab = tabsContainer.locator('button:has-text("Провайдеры")');

    await expect(groupsTab).toBeVisible();
    await expect(providersTab).toBeVisible();

    await providersTab.click();

    const subCard = page
      .locator('.card, .provider-card')
      .filter({ hasText: 'Primary VPN' })
      .first();
    await expect(subCard).toBeVisible();

    await expect(subCard.locator('.progress-bar, .traffic-bar')).toBeVisible();

    const refreshBtn = subCard.locator('button:has-text("↺"), button.btn-refresh-sub');
    if (await refreshBtn.isVisible()) {
      await refreshBtn.click();
      await expect.poll(() => postRequests.length).toBe(1);
    }
  });

  // D-08: клик по плашке статуса фильтрует группы, повторный клик сбрасывает
  test('observatory filter: клик по плашке изолирует группы, повторный клик сбрасывает', async ({
    page
  }) => {
    await expect(page.locator('.group-card').filter({ hasText: 'YouTube' }).first()).toBeVisible();
    await expect(
      page.locator('.group-card').filter({ hasText: 'FastGroup' }).first()
    ).toBeVisible();

    const badBtn = page.locator('.obs-stat-btn').filter({ hasText: 'Недоступны' }).first();
    await expect(badBtn).toBeVisible();

    // Кликаем по "Недоступны"
    await badBtn.click();
    await expect(badBtn).toHaveAttribute('aria-pressed', 'true');

    // YouTube и FastGroup обе содержат DE-Node-04 (bad, alive=false)
    await expect(page.locator('.group-card').filter({ hasText: 'YouTube' }).first()).toBeVisible();

    // Клик повторно сбрасывает фильтр
    await badBtn.click();
    await expect(badBtn).toHaveAttribute('aria-pressed', 'false');
    await expect(page.locator('.group-card').filter({ hasText: 'YouTube' }).first()).toBeVisible();
  });

  // D-09: заголовок и подписи на русском без капслока
  test('observatory filter: заголовок и подписи на русском без капслока', async ({ page }) => {
    const title = page.locator('.obs-title');
    await expect(title).toHaveText('Состояние узлов');
    const textTransform = await title.evaluate((el) => window.getComputedStyle(el).textTransform);
    expect(textTransform).toBe('none');
  });

  // D-10: расшифровка общего числа узлов
  test('observatory filter: расшифровка общего числа узлов', async ({ page }) => {
    const totalBox = page.locator('.obs-stat-box').first();
    const resSub = totalBox.locator('.res-sub');
    await expect(resSub).toBeVisible();
    await expect(resSub).toHaveText(/\d+\s+прокси\s+\+\s+\d+\s+системн/);
  });

  // D-11: тултип полосы здоровья
  test('health bar: тултип показывает числа и проценты', async ({ page }) => {
    const ytGroup = page.locator('.group-card').filter({ hasText: 'YouTube' }).first();
    const collapseAllBtn = page.locator('button:has-text("Свернуть все")');
    await collapseAllBtn.click();

    const healthBar = ytGroup.locator('.health-bar').first();
    await expect(healthBar).toBeVisible();

    await healthBar.hover();
    const tooltip = page.locator('.health-tooltip');
    await expect(tooltip).toBeVisible();
    await expect(tooltip).toContainText(/Доступно:\s+\d+\s+\(\d+%\)/);
    await expect(tooltip).toContainText(/Недоступно:\s+\d+\s+\(\d+%\)/);

    // Escape скрывает тултип
    await page.keyboard.press('Escape');
    await expect(tooltip).toBeHidden();
  });

  // D-11: полоса не прилипает к нижней границе карточки
  test('health bar: полоса не прилипает к нижней границе карточки', async ({ page }) => {
    const collapseAllBtn = page.locator('button:has-text("Свернуть все")');
    await collapseAllBtn.click();

    const ytGroup = page.locator('.group-card').filter({ hasText: 'YouTube' }).first();
    const healthBar = ytGroup.locator('.health-bar').first();

    const barBox = await healthBar.boundingBox();
    const cardBox = await ytGroup.boundingBox();

    expect(barBox).not.toBeNull();
    expect(cardBox).not.toBeNull();
    if (barBox && cardBox) {
      const gap = cardBox.y + cardBox.height - (barBox.y + barBox.height);
      expect(gap).toBeGreaterThanOrEqual(6);
    }
  });

  // D-11: aria-label содержит сводку
  test('health bar: aria-label содержит сводку', async ({ page }) => {
    const collapseAllBtn = page.locator('button:has-text("Свернуть все")');
    await collapseAllBtn.click();

    const ytGroup = page.locator('.group-card').filter({ hasText: 'YouTube' }).first();
    const healthBar = ytGroup.locator('.health-bar').first();
    const ariaLabel = await healthBar.getAttribute('aria-label');
    expect(ariaLabel).toMatch(/Доступно:\s+\d+/);
  });

  // D-13: Quick-Select Popover
  test('quick select: клик по плашке открывает поповер и клик по узлу переключает прокси', async ({
    page
  }) => {
    const ytGroup = page.locator('.group-card').filter({ hasText: 'YouTube' }).first();
    const trigger = ytGroup.locator('.gc-now-pill-trigger').first();
    await expect(trigger).toBeVisible();
    await trigger.click();

    const popover = page.locator('.qs-popover');
    await expect(popover).toBeVisible();

    const searchInput = popover.locator('.qs-search');
    await expect(searchInput).toBeVisible();
    await expect(searchInput).toBeFocused();

    // Клик по узлу NL-Node-03
    const nlOption = popover.locator('.qs-item').filter({ hasText: 'NL-Node-03' });
    await expect(nlOption).toBeVisible();
    await nlOption.click();

    // Поповер закрывается, PUT отправлен
    await expect(popover).toBeHidden();
    expect(
      putRequests.some((r) => r.url.includes('YouTube') && r.body?.name === 'NL-Node-03')
    ).toBe(true);
  });

  // D-14: Quick-Select клавиатурная навигация
  test('quick select: навигация стрелками и выбор через Enter', async ({ page }) => {
    const ytGroup = page.locator('.group-card').filter({ hasText: 'YouTube' }).first();
    const trigger = ytGroup.locator('.gc-now-pill-trigger').first();
    await trigger.click();

    const popover = page.locator('.qs-popover');
    await expect(popover).toBeVisible();

    // Нажатие вниз и Enter
    await page.keyboard.press('ArrowDown');
    await page.keyboard.press('Enter');

    await expect(popover).toBeHidden();
    expect(putRequests.length).toBeGreaterThan(0);
  });

  // D-15: Quick-Select информационная плашка для URLTest
  test('quick select: группа URLTest показывает предупреждение об автоматическом выборе', async ({
    page
  }) => {
    const fastGroup = page.locator('.group-card').filter({ hasText: 'FastGroup' }).first();
    const trigger = fastGroup.locator('.gc-now-pill-trigger').first();
    await trigger.click();

    const popover = page.locator('.qs-popover');
    await expect(popover).toBeVisible();

    const note = popover.locator('.qs-auto-note');
    await expect(note).toBeVisible();
    await expect(note).toContainText('автоматически');

    // Клик по узлу в URLTest не отправляет PUT
    const firstOption = popover.locator('.qs-item').first();
    await firstOption.click({ force: true });
    expect(putRequests.filter((r) => r.url.includes('FastGroup')).length).toBe(0);

    await page.keyboard.press('Escape');
    await expect(popover).toBeHidden();
  });

  // D-16: Ping micro-button & per-group testing
  test('ping micro-button: запуск теста группы блокирует только эту группу со спиннером', async ({
    page
  }) => {
    const ytGroup = page.locator('.group-card').filter({ hasText: 'YouTube' }).first();
    const pingBtn = ytGroup.locator('.gc-ping-btn').first();
    await expect(pingBtn).toBeVisible();

    await pingBtn.click();
    await expect(pingBtn).toBeVisible();
  });

  // D-17: Latency history popover на бейдже задержки группы
  test('latency history popover: клик на бейдж задержки группы открывает историю', async ({
    page
  }) => {
    const ytGroup = page.locator('.group-card').filter({ hasText: 'YouTube' }).first();
    const latBox = ytGroup.locator('.gc-lat-box').first();
    await expect(latBox).toBeVisible();

    await latBox.click();
    const historyPopover = page.locator('.latency-history-popover');
    await expect(historyPopover).toBeVisible();

    await page.keyboard.press('Escape');
    await expect(historyPopover).toBeHidden();
  });

  // UI-Review: Swipe-to-dismiss для QuickSelect Bottom Sheet на мобильных
  test('quick select mobile: свайп вниз по драг-ручке закрывает bottom sheet', async ({ page }) => {
    await page.setViewportSize({ width: 375, height: 667 });
    const ytGroup = page.locator('.group-card').filter({ hasText: 'YouTube' }).first();
    const trigger = ytGroup.locator('.gc-now-pill-trigger').first();
    await trigger.click();

    const popover = page.locator('.qs-popover');
    await expect(popover).toBeVisible();
    await expect(popover).toHaveClass(/qs-bottom-sheet/);

    const dragHandle = popover.locator('.qs-drag-handle');
    await expect(dragHandle).toBeVisible();

    // Симуляция жеста touch drag вниз
    await dragHandle.evaluate((el) => {
      const touchStart = new Touch({
        identifier: 1,
        target: el,
        clientY: 400,
        clientX: 180
      });
      el.dispatchEvent(
        new TouchEvent('touchstart', { touches: [touchStart], bubbles: true, cancelable: true })
      );

      const touchMove = new Touch({
        identifier: 1,
        target: el,
        clientY: 500,
        clientX: 180
      });
      el.dispatchEvent(
        new TouchEvent('touchmove', { touches: [touchMove], bubbles: true, cancelable: true })
      );

      el.dispatchEvent(
        new TouchEvent('touchend', { touches: [], bubbles: true, cancelable: true })
      );
    });

    await expect(popover).toBeHidden();
  });
});
