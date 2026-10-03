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
