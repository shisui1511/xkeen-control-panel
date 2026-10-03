import { test, expect, type Page } from '@playwright/test';
import { setupMocks, type KernelMode } from './helpers/api-mocks';

// ============================================================
// Phase 141 — регрессионный проход оставшихся страниц на моках:
// консоль (терминал и быстрые команды) в обеих темах на трёх ширинах.
// На каждую комбинацию: тема применилась, в консоли браузера нет
// error/pageerror, нет горизонтального переполнения.
// Службы на ПК не запускаются — только моки Playwright.
// Реальные дефекты помечаются test.fail с номером записи журнала аудита.
// ============================================================

const THEMES = ['light', 'dark'] as const;
const WIDTHS = [1440, 1024, 390] as const;

type Theme = (typeof THEMES)[number];

interface ConsoleRecord {
  level: string;
  text: string;
}

/** Навешивается ДО перехода, чтобы ранние ошибки не терялись. */
function attachConsoleCollectors(page: Page) {
  const errors: ConsoleRecord[] = [];
  page.on('console', (msg) => {
    if (msg.type() === 'error') errors.push({ level: 'error', text: msg.text() });
  });
  page.on('pageerror', (err) => {
    errors.push({ level: 'pageerror', text: err.message });
  });
  return { errors };
}

// Две категории: информационная команда и команда с подтверждением
const COMMANDS_FIXTURE = [
  {
    name: 'info',
    commands: [
      {
        name: 'Статус',
        description: 'Показать статус XKeen',
        command: '-status',
        dangerous: false
      },
      { name: 'Версия', description: 'Показать версию XKeen', command: '-v', dangerous: false }
    ]
  },
  {
    name: 'backup',
    commands: [
      {
        name: 'Восстановить конфигурацию',
        description: 'Восстановить конфигурацию Xray из резервной копии',
        command: '-xbr',
        dangerous: true
      }
    ]
  }
];

/** Моки консоли: список команд и молчаливый сокет терминала. */
async function mockConsole(page: Page) {
  await page.route('**/api/console/commands', async (route) => {
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify(COMMANDS_FIXTURE)
    });
  });
  // Сервер терминала не нужен: сокет принимается и молчит
  await page.routeWebSocket(/\/api\/terminal\/ws/, () => {});
}

/** Всё, что нужно перед переходом: окно, тема, язык, моки. */
async function prepare(page: Page, theme: Theme, width: number, kernel: KernelMode = 'mihomo') {
  await page.setViewportSize({ width, height: width === 390 ? 844 : 900 });
  await page.addInitScript((tm) => {
    localStorage.setItem('theme', tm);
    localStorage.setItem('lang', 'ru');
  }, theme);
  await setupMocks(page, kernel);
  await mockConsole(page);
}

async function expectCleanPage(page: Page, theme: Theme) {
  await expect(page.locator('h1').first()).toBeVisible();
  await expect(page.locator('html')).toHaveAttribute('data-theme', theme);
  // Даём отложенным запросам и подгонке терминала отработать
  await page.waitForTimeout(600);
  const overflow = await page.evaluate(
    () => document.documentElement.scrollWidth - window.innerWidth
  );
  expect(overflow, `горизонтальное переполнение: ${overflow}px`).toBeLessThanOrEqual(1);
}

for (const theme of THEMES) {
  for (const width of WIDTHS) {
    test.describe(`консоль: ${theme} ${width}px`, () => {
      test(`терминал по умолчанию: ${theme} ${width}px`, async ({ page }) => {
        await prepare(page, theme, width);
        const { errors } = attachConsoleCollectors(page);
        await page.goto('/#/console');
        await expectCleanPage(page, theme);
        expect(errors, `ошибки консоли: ${JSON.stringify(errors)}`).toHaveLength(0);
      });

      test(`быстрые команды: ${theme} ${width}px`, async ({ page }) => {
        await prepare(page, theme, width);
        const { errors } = attachConsoleCollectors(page);
        await page.goto('/#/console');
        await page.getByRole('tab', { name: 'Быстрые команды' }).click();
        await expect(page.locator('.cmd-tile').first()).toBeVisible();
        await expectCleanPage(page, theme);
        expect(errors, `ошибки консоли: ${JSON.stringify(errors)}`).toHaveLength(0);
      });
    });
  }
}

// ============================================================
// Вкладки Настроек (режим Mihomo) и режим Xray
// ============================================================

const SETTINGS_TABS = ['general', 'updates', 'security', 'connection', 'backups', 'about'] as const;

for (const theme of THEMES) {
  for (const width of WIDTHS) {
    test.describe(`настройки: ${theme} ${width}px`, () => {
      for (const tab of SETTINGS_TABS) {
        test(`вкладка ${tab}: ${theme} ${width}px`, async ({ page }) => {
          await prepare(page, theme, width);
          const { errors } = attachConsoleCollectors(page);
          await page.goto(`/#/settings?tab=${tab}`);
          await expectCleanPage(page, theme);
          expect(errors, `ошибки консоли: ${JSON.stringify(errors)}`).toHaveLength(0);
        });
      }
    });

    test.describe(`режим Xray: ${theme} ${width}px`, () => {
      test(`быстрые команды в режиме Xray: ${theme} ${width}px`, async ({ page }) => {
        await prepare(page, theme, width, 'xray');
        const { errors } = attachConsoleCollectors(page);
        await page.goto('/#/console');
        await page.getByRole('tab', { name: 'Быстрые команды' }).click();
        await expect(page.locator('.cmd-tile').first()).toBeVisible();
        await expectCleanPage(page, theme);
        expect(errors, `ошибки консоли: ${JSON.stringify(errors)}`).toHaveLength(0);
      });

      test(`настройки, подключение в режиме Xray: ${theme} ${width}px`, async ({ page }) => {
        await prepare(page, theme, width, 'xray');
        const { errors } = attachConsoleCollectors(page);
        await page.goto('/#/settings?tab=connection');
        await expectCleanPage(page, theme);
        expect(errors, `ошибки консоли: ${JSON.stringify(errors)}`).toHaveLength(0);
      });
    });
  }
}

// ============================================================
// Подтверждение опасной команды консоли: диалог виден и укладывается
// в окно, «Отмена» не отправляет POST /api/console/execute
// ============================================================

for (const theme of THEMES) {
  for (const width of WIDTHS) {
    test(`опасная команда требует подтверждения, отмена не выполняет: ${theme} ${width}px`, async ({
      page
    }) => {
      await prepare(page, theme, width);
      const { errors } = attachConsoleCollectors(page);
      const executeRequests: string[] = [];
      page.on('request', (req) => {
        if (req.url().includes('/api/console/execute')) executeRequests.push(req.method());
      });
      await page.goto('/#/console');
      await page.getByRole('tab', { name: 'Быстрые команды' }).click();
      await page.locator('.cmd-tile', { hasText: 'xkeen -xbr' }).click();

      const dialog = page.getByRole('dialog');
      await expect(dialog).toBeVisible();
      await expect(page.locator('html')).toHaveAttribute('data-theme', theme);
      for (const name of ['Отмена', 'Подтвердить']) {
        const box = await dialog.getByRole('button', { name }).boundingBox();
        expect(box, `кнопка «${name}» не найдена`).not.toBeNull();
        expect(box!.x).toBeGreaterThanOrEqual(0);
        expect(box!.x + box!.width).toBeLessThanOrEqual(width + 1);
        expect(box!.y + box!.height).toBeLessThanOrEqual(page.viewportSize()!.height + 1);
      }

      await dialog.getByRole('button', { name: 'Отмена' }).click();
      await expect(dialog).toBeHidden();
      await page.waitForTimeout(400);
      expect(executeRequests, 'после отмены не должно быть запросов выполнения').toHaveLength(0);
      expect(errors, `ошибки консоли: ${JSON.stringify(errors)}`).toHaveLength(0);
    });
  }
}

// ============================================================
// Mihomo запущен, API недоступен: плашка «API оффлайн» не должна
// давать горизонтальную прокрутку ни на одной странице поверх REST
// ============================================================

const OFFLINE_ROUTES = ['proxies', 'proxies?tab=providers', 'rules', 'connections', 'traffic'];

for (const theme of THEMES) {
  for (const width of WIDTHS) {
    for (const route of OFFLINE_ROUTES) {
      test(`API Mihomo оффлайн, ${route}: ${theme} ${width}px`, async ({ page }) => {
        await prepare(page, theme, width);
        await page.route('**/api/capabilities', async (r) => {
          await r.fulfill({
            status: 200,
            contentType: 'application/json',
            body: JSON.stringify({
              success: true,
              data: {
                kernels: {
                  xray: { installed: true, version: '1.8.4', channel: 'stable' },
                  mihomo: { installed: true, version: '1.18.0', channel: 'stable' }
                },
                active_kernel: 'mihomo',
                kernel_conflict: false,
                running_kernels: ['mihomo'],
                mihomo: {
                  reachable: false,
                  process_running: true,
                  api_reachable: false,
                  api_authenticated: false
                }
              }
            })
          });
        });
        await page.goto(`/#/${route}`);
        await expectCleanPage(page, theme);
      });
    }
  }
}

// ============================================================
// Прокси и Правила не обращаются к Mihomo, пока его API не отвечает:
// при недоступном API, активном Xray и остановленном ядре (B19, B35).
// Лишний запрос даёт 409/502 и ошибку в консоли браузера.
// ============================================================

type ApiState = 'api-down' | 'xray' | 'stopped' | 'up';

async function setupApiState(page: Page, state: ApiState) {
  await prepare(page, 'light', 1440, state === 'xray' ? 'xray' : 'mihomo');
  if (state === 'api-down' || state === 'stopped') {
    const stopped = state === 'stopped';
    await page.route('**/api/capabilities', async (r) => {
      await r.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          success: true,
          data: {
            kernels: {
              xray: { installed: true, version: '1.8.4', channel: 'stable' },
              mihomo: { installed: true, version: '1.18.0', channel: 'stable' }
            },
            active_kernel: stopped ? 'none' : 'mihomo',
            kernel_conflict: false,
            running_kernels: stopped ? [] : ['mihomo'],
            mihomo: {
              reachable: false,
              process_running: !stopped,
              api_reachable: false,
              api_authenticated: false
            }
          }
        })
      });
    });
  }
}

for (const state of ['api-down', 'xray', 'stopped'] as const) {
  for (const route of ['proxies', 'rules']) {
    test(`${route}: без запросов к Mihomo при состоянии ${state} (B19, B35)`, async ({ page }) => {
      await setupApiState(page, state);
      const mihomoRequests: string[] = [];
      page.on('request', (req) => {
        if (req.url().includes('/api/mihomo/proxy/')) mihomoRequests.push(req.url());
      });
      const { errors } = attachConsoleCollectors(page);
      await page.goto(`/#/${route}`);
      await expect(page.locator('h1').first()).toBeVisible();
      await page.waitForTimeout(2500);

      expect(mihomoRequests, `запросы к API Mihomo: ${mihomoRequests.join(', ')}`).toEqual([]);
      expect(errors, `ошибки консоли: ${JSON.stringify(errors)}`).toHaveLength(0);
    });
  }
}

for (const route of ['proxies', 'rules']) {
  test(`${route}: при работающем API запросы к Mihomo идут (B19, B35: гейт открывается)`, async ({
    page
  }) => {
    await setupApiState(page, 'up');
    const mihomoRequests: string[] = [];
    page.on('request', (req) => {
      if (req.url().includes('/api/mihomo/proxy/')) mihomoRequests.push(req.url());
    });
    await page.goto(`/#/${route}`);
    await expect.poll(() => mihomoRequests.length, { timeout: 15000 }).toBeGreaterThan(0);
  });
}

// ============================================================
// Прокси → Провайдеры: карточка подписки не шире окна (B18)
// ============================================================

function subscriptionFixture() {
  const now = Math.floor(Date.now() / 1000);
  return [
    {
      id: 'sub-1',
      name: 'C-VPN ▮ Подписка',
      url: 'https://example.invalid/sub',
      enabled: true,
      interval: 6,
      use_provider_interval: false,
      enable_xray: true,
      enable_mihomo: true,
      mihomo_integrated: false,
      hwid_locked: false,
      last_update: new Date(Date.now() - 49 * 60 * 1000).toISOString(),
      proxy_count: 2,
      upload: 120 * 1024 ** 3,
      download: 164 * 1024 ** 3,
      total: 0,
      expire: now + 243 * 86400,
      next_update: new Date(Date.now() + 10 * 60 * 1000).toISOString(),
      support_url: 'https://example.invalid/support',
      announcement: 'Объявление',
      mihomo_provider: null
    }
  ];
}

for (const theme of THEMES) {
  for (const width of WIDTHS) {
    test(`провайдеры: карточка подписки в окне, ${theme} ${width}px (B18)`, async ({ page }) => {
      await prepare(page, theme, width);
      await page.route(/\/api\/proxy-providers(\?.*)?$/, async (r) => {
        await r.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify(subscriptionFixture())
        });
      });
      await page.goto('/#/proxies?tab=providers');
      await expect(page.locator('.sub-card').first()).toBeVisible();
      await expectCleanPage(page, theme);
      const card = await page.locator('.sub-card').first().boundingBox();
      expect(card!.x + card!.width).toBeLessThanOrEqual(width + 1);
    });
  }
}

// ============================================================
// Конструктор: хлебные крошки переносятся и не выступают за окно (B24)
// ============================================================

for (const theme of THEMES) {
  for (const kernel of ['mihomo', 'xray'] as const) {
    test(`конструктор ${kernel}: крошки в окне на 390px, ${theme} (B24)`, async ({ page }) => {
      await prepare(page, theme, 390, kernel);
      await page.goto('/#/constructor');
      await expect(page.locator('.breadcrumb-current').first()).toBeVisible();
      await expectCleanPage(page, theme);
      const box = await page.locator('.breadcrumb-current').first().boundingBox();
      expect(box!.x + box!.width).toBeLessThanOrEqual(391);
    });
  }
}

// ============================================================
// Настройки → Резервные копии: строки выбора файла и копий не шире карточки (B16)
// ============================================================

for (const theme of THEMES) {
  for (const width of WIDTHS) {
    test(`резервные копии со списком копий: ${theme} ${width}px (B16)`, async ({ page }) => {
      await prepare(page, theme, width);
      await page.route('**/api/config/backups**', async (r) => {
        await r.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify([
            '/opt/etc/xcp/backups/config.json.backup-20261003-200909-with-a-rather-long-name'
          ])
        });
      });
      await page.goto('/#/settings?tab=backups');
      await expect(page.locator('.field-row .ctrl').first()).toBeVisible();
      await expectCleanPage(page, theme);
      for (const sel of ['.field-row .ctrl', '.field-row .ctrl .btn']) {
        for (const box of await page.locator(sel).evaluateAll((els) =>
          els.map((e) => {
            const r = e.getBoundingClientRect();
            return { right: r.right, width: r.width };
          })
        )) {
          if (box.width > 0) expect(box.right).toBeLessThanOrEqual(width + 1);
        }
      }
    });
  }
}
