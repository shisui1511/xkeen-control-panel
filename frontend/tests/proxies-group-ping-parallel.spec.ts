import { test, expect } from '@playwright/test';

test.use({ locale: 'ru-RU' });

// Фикстура для регрессии параллельного пинга групп (Phase 90 gap closure, G-90-7).
// «Медиа» и «Игры» намеренно не совпадают ни с одним CORE_GROUP_PATTERNS и не
// ссылаются друг на друга, поэтому classifyGroupRole() относит обе к 'service' —
// обе карточки рендерятся обычными (с .gc-ping-btn), а не мини-карточками.
// Узлы Node-A1/A2/B1/B2 намеренно без history и delay — непроверенные, чтобы
// появление реальной задержки после теста было наблюдаемо в бейдже.
const MOCK_PROXIES_PING = {
  proxies: {
    Медиа: {
      name: 'Медиа',
      type: 'Selector',
      now: 'Node-A1',
      all: ['Node-A1', 'Node-A2'],
      alive: true
    },
    Игры: {
      name: 'Игры',
      type: 'Selector',
      now: 'Node-B1',
      all: ['Node-B1', 'Node-B2'],
      alive: true
    },
    'Node-A1': { name: 'Node-A1', type: 'Vless', alive: true },
    'Node-A2': { name: 'Node-A2', type: 'Vless', alive: true },
    'Node-B1': { name: 'Node-B1', type: 'Vless', alive: true },
    'Node-B2': { name: 'Node-B2', type: 'Vless', alive: true },
    DIRECT: { name: 'DIRECT', type: 'Direct' },
    REJECT: { name: 'REJECT', type: 'Reject' }
  }
};

test.describe('Параллельный пинг групп и непрерывный фоновый опрос (Phase 90 gap closure, G-90-7)', () => {
  // Управляется из тела теста ДО клика по кнопке-молнии: длительность ответа
  // мок-эндпоинта задержки группы.
  let groupDelayResponseMs = 50;
  // Если true — основной эндпоинт задержки группы отвечает ошибкой, вынуждая
  // testGroupLatency() уйти в batch-фолбэк (нужно Тесту 3, см. ниже).
  let forceGroupDelayFailure = false;
  // Длительность ответа мок-эндпоинта задержки ОДНОГО узла (batch-фолбэк):
  // узлы группы тестируются им последовательно, один за другим.
  let singleNodeDelayResponseMs = 50;
  // Счётчик обращений именно к списку прокси (не к эндпоинтам задержки) — на
  // нём строится доказательство непрерывности фонового опроса.
  let proxiesListCallCount = 0;
  // Счётчик обращений к эндпоинту задержки группы по имени группы — нужен для
  // проверки защиты от повторного нажатия (Тест 2).
  let groupDelayCallCounts: Record<string, number> = {};

  test.beforeEach(async ({ page }) => {
    groupDelayResponseMs = 50;
    forceGroupDelayFailure = false;
    singleNodeDelayResponseMs = 50;
    proxiesListCallCount = 0;
    groupDelayCallCounts = {};

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
      } else if (/\/api\/mihomo\/proxy\/group\/[^/]+\/delay/.test(url) && method === 'GET') {
        // Запрос задержки группы (основной путь testGroupLatency): маска до общей
        // ветки списка прокси, так как их префиксы пересекаются.
        const match = url.match(/\/api\/mihomo\/proxy\/group\/([^/?]+)\/delay/);
        const groupName = match ? decodeURIComponent(match[1]) : '';
        groupDelayCallCounts[groupName] = (groupDelayCallCounts[groupName] || 0) + 1;
        if (forceGroupDelayFailure) {
          await route.fulfill({ status: 500, contentType: 'application/json', body: '{}' });
        } else {
          await new Promise((resolve) => setTimeout(resolve, groupDelayResponseMs));
          const group = (MOCK_PROXIES_PING.proxies as Record<string, { all?: string[] }>)[
            groupName
          ];
          const nodes = group?.all ?? [];
          const body: Record<string, number> = {};
          for (const node of nodes) body[node] = 42;
          await route.fulfill({
            status: 200,
            contentType: 'application/json',
            body: JSON.stringify(body)
          });
        }
      } else if (/\/api\/mihomo\/proxy\/proxies\/[^/]+\/delay/.test(url) && method === 'GET') {
        // Запрос задержки одиночного узла (batch-фолбэк): используется Тестом 3
        // (форсированный сбой основного пути), в остальных тестах не задействуется,
        // но ветка нужна, чтобы случайный фолбэк не ушёл в общую ветку списка
        // прокси и не исказил proxiesListCallCount.
        await new Promise((resolve) => setTimeout(resolve, singleNodeDelayResponseMs));
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({ delay: 42 })
        });
      } else if (url.includes('/api/mihomo/proxy/proxies') && method === 'GET') {
        proxiesListCallCount++;
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify(MOCK_PROXIES_PING)
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

  test('пока тестируется одна группа, вторую можно тестировать параллельно', async ({ page }) => {
    groupDelayResponseMs = 3000;

    const mediaCard = page.locator('.group-card[data-group="Медиа"]');
    const gamesCard = page.locator('.group-card[data-group="Игры"]');
    const mediaPingBtn = mediaCard.locator('.gc-ping-btn');
    const gamesPingBtn = gamesCard.locator('.gc-ping-btn');

    await mediaPingBtn.click();
    await expect(mediaPingBtn).toBeDisabled();
    await expect(mediaPingBtn.locator('.spinner')).toBeVisible();

    // До завершения первого замера — кнопка соседней группы остаётся активной.
    await expect(gamesPingBtn).toBeEnabled();
    await gamesPingBtn.click();

    // Оба спиннера видны одновременно.
    await expect(page.locator('.gc-ping-btn .spinner')).toHaveCount(2);

    // Оба замера завершаются, обе кнопки снова активны, обе задержки видны.
    await expect(mediaPingBtn).toBeEnabled({ timeout: 8000 });
    await expect(gamesPingBtn).toBeEnabled({ timeout: 8000 });
    await expect(mediaCard.locator('.gc-lat-box')).toHaveText(/\d/);
    await expect(gamesCard.locator('.gc-lat-box')).toHaveText(/\d/);
  });

  test('повторный клик по своей кнопке не запускает второй прогон той же группы', async ({
    page
  }) => {
    groupDelayResponseMs = 2000;

    const mediaCard = page.locator('.group-card[data-group="Медиа"]');
    const mediaPingBtn = mediaCard.locator('.gc-ping-btn');

    await mediaPingBtn.click();
    await expect(mediaPingBtn.locator('.spinner')).toBeVisible();

    // Повторный клик по уже заблокированной кнопке своей же группы.
    await mediaPingBtn.click({ force: true });

    await expect(mediaPingBtn).toBeEnabled({ timeout: 5000 });
    expect(groupDelayCallCounts['Медиа']).toBe(1);
  });

  test('фоновый опрос не останавливается на время замера и не затирает свежий результат', async ({
    page
  }) => {
    test.setTimeout(45000);
    // Основной эндпоинт задержки группы форсированно падает — testGroupLatency()
    // уходит в batch-фолбэк, который тестирует узлы группы ПОСЛЕДОВАТЕЛЬНО,
    // один за другим. Это даёт наблюдаемое окно: Node-A1 уже измерен и записан
    // локально (history непустая), а Node-A2 — ещё нет, группа всё ещё
    // помечена тестируемой (спиннер виден). Именно в этом окне должен прийти
    // фоновый цикл опроса, чтобы по-настоящему проверить, что
    // preserveInFlightLatency() защищает уже полученный результат Node-A1 от
    // затирания статичной фикстурой списка прокси (в ней узлы всегда без
    // history/delay).
    forceGroupDelayFailure = true;
    singleNodeDelayResponseMs = 6000;

    const mediaCard = page.locator('.group-card[data-group="Медиа"]');
    const mediaPingBtn = mediaCard.locator('.gc-ping-btn');

    const initialCount = proxiesListCallCount;
    await mediaPingBtn.click();
    await expect(mediaPingBtn.locator('.spinner')).toBeVisible();

    // Node-A1 (текущий узел группы) измерен первым — бейдж группы показывает
    // задержку, пока Node-A2 ещё тестируется и спиннер всё ещё виден.
    await expect(mediaCard.locator('.gc-lat-box')).toHaveText(/42/, { timeout: 9000 });
    await expect(mediaPingBtn.locator('.spinner')).toBeVisible();

    // Фоновый опрос продолжает идти, пока Node-A2 ещё тестируется: счётчик
    // обращений к списку прокси растёт, а группа всё ещё помечена тестируемой.
    await expect
      .poll(() => proxiesListCallCount, { timeout: 10000, intervals: [500] })
      .toBeGreaterThan(initialCount);

    // Ответ этого цикла опроса не затёр уже полученную задержку Node-A1, хотя
    // фикстура списка прокси по-прежнему отдаёт его без history/delay —
    // сработала защита слияния, а не совпадение по времени: спиннер (и,
    // соответственно, пометка группы как тестируемой) всё ещё активен.
    await expect(mediaPingBtn.locator('.spinner')).toBeVisible();
    await expect(mediaCard.locator('.gc-lat-box')).toHaveText(/42/);

    // Дождаться полного завершения замера обоих узлов batch-фолбэка.
    await expect(mediaPingBtn).toBeEnabled({ timeout: 10000 });
    await expect(mediaCard.locator('.gc-lat-box')).toHaveText(/42/);
  });

  test('уход со страницы во время замера не оставляет зависших запросов и ошибок в консоли', async ({
    page
  }) => {
    groupDelayResponseMs = 5000;

    const consoleErrors: string[] = [];
    page.on('console', (msg) => {
      if (msg.type() === 'error') consoleErrors.push(msg.text());
    });

    const mediaCard = page.locator('.group-card[data-group="Медиа"]');
    const mediaPingBtn = mediaCard.locator('.gc-ping-btn');

    await mediaPingBtn.click();
    await expect(mediaPingBtn.locator('.spinner')).toBeVisible();

    await page.goto('/#/dashboard');
    await page.waitForTimeout(500);

    expect(consoleErrors).toEqual([]);
  });
});
