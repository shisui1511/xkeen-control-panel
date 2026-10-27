import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { test, expect, type Page } from '@playwright/test';
import { attachConsoleGuard } from './lib/console';
import { T } from './lib/env';
import { api } from './lib/nodes-api';

// Каналы и тип версии на карточке «Обновление» настоящей панели (D-28, D-29, D-32): страница
// «Настройки», вкладка обновлений. Источник истины — сама панель стенда и её API; сеть не
// подменяется. Ожидаемые подписи читаются из словарей ru и en (в спеке они не дублируются);
// вид версии для ожидания спек определяет по суффиксу номера сборки сам, функции продукта
// не импортируются.
//
// Спек изменяющий (меняет канал панели): строки вида e2e-kind в шапке нет. Канал, прочитанный
// в beforeEach, возвращается в afterEach.
//
// Бюджет обращений панели к GitHub API без токена (60 в час с адреса стенда): каждая загрузка
// вкладки обновлений и каждое сохранение канала — одно обращение. Счёт за прогон всего спека:
//   update channels: — 6 (на язык: переход, сохранение канала с повторной проверкой, перезагрузка);
//   update kinds: — 1;
//   update auto-status: — 2;
//   update axe: — 4;
// итого 13. Новые загрузки вкладки в этот спек добавляются только с пересчётом счёта.

const DICT = Object.fromEntries(
  ['ru', 'en'].map((lang) => [
    lang,
    JSON.parse(
      readFileSync(
        fileURLToPath(new URL(`../../src/locales/${lang}.json`, import.meta.url)),
        'utf8'
      )
    ) as Record<string, string>
  ])
) as Record<'ru' | 'en', Record<string, string>>;

/** Подпись из словаря языка; отсутствие ключа — ошибка спека, а не повод подставить текст. */
function text(lang: 'ru' | 'en', key: string): string {
  const value = DICT[lang][key];
  if (!value) throw new Error(`в словаре ${lang} нет ключа ${key}`);
  return value;
}

type Channel = 'stable' | 'beta';

let savedChannel: Channel | null = null;

/** Вид версии по суффиксу номера: без суффикса — stable, beta и rc — beta, остальное — alpha. */
function kindOf(version: string): 'stable' | 'beta' | 'alpha' {
  const pre = version.replace(/^v/, '').split('+')[0].split('-')[1] ?? '';
  if (pre === '') return 'stable';
  if (pre.startsWith('beta') || pre.startsWith('rc')) return 'beta';
  return 'alpha';
}

async function readChannel(page: Page): Promise<Channel> {
  const res = await page.request.get('/api/update/channel');
  expect(res.ok(), `GET /api/update/channel: http ${res.status()}`).toBe(true);
  const body = (await res.json()) as { data?: { channel?: string }; channel?: string };
  const value = body.data?.channel ?? body.channel;
  return value === 'beta' ? 'beta' : 'stable';
}

test.beforeEach(async ({ page }) => {
  savedChannel = await readChannel(page);
});

test.afterEach(async ({ page }) => {
  const channel = savedChannel;
  savedChannel = null;
  if (!channel || !page.url().includes('#/settings')) return;
  // страница открыта — токен CSRF в хранилище есть; канал не менялся, если до перехода не дошло
  await api(page, 'POST', '/api/update/channel', { channel });
});

async function openUpdates(page: Page, lang: 'ru' | 'en'): Promise<void> {
  await page.addInitScript((l) => localStorage.setItem('lang', l), lang);
  await page.goto('/#/settings?tab=updates');
  await expect(page.locator('[data-testid="update-channel-row"]')).toBeVisible({
    timeout: T.page
  });
}

for (const lang of ['ru', 'en'] as const) {
  test(`update channels: подписи и выбор (${lang})`, async ({ page }) => {
    test.setTimeout(T.page * 3 + T.action * 4);
    const guard = attachConsoleGuard(page);
    await openUpdates(page, lang);

    const row = page.locator('[data-testid="update-channel-row"]');
    const main = row.getByRole('button', { name: text(lang, 'settings.channel_stable') });
    const preview = row.getByRole('button', { name: text(lang, 'settings.channel_beta') });
    await expect(main).toBeVisible({ timeout: T.action });
    await expect(preview).toBeVisible({ timeout: T.action });

    // выбираем сегмент, отличный от сохранённого, чтобы путь сохранения точно выполнялся
    const target: Channel = (savedChannel ?? 'stable') === 'beta' ? 'stable' : 'beta';
    const segment = target === 'beta' ? preview : main;
    await segment.click();

    await expect
      .poll(async () => readChannel(page), {
        timeout: T.action,
        message: 'GET /api/update/channel не вернул выбранный канал'
      })
      .toBe(target);
    await expect(row.locator('.field-row-desc')).toHaveText(
      text(lang, `settings.channel_${target}_desc`),
      { timeout: T.action }
    );

    await page.reload();
    await expect(row).toBeVisible({ timeout: T.page });
    await expect(
      row.getByRole('button', { name: text(lang, `settings.channel_${target}`) })
    ).toHaveAttribute('aria-pressed', 'true', { timeout: T.action });

    await guard.assertClean();
  });
}

test('update kinds: бейдж текущей версии', async ({ page }) => {
  test.setTimeout(T.page * 2 + T.action * 2);
  const guard = attachConsoleGuard(page);
  // номер сборки — из /api/version панели: состояние обновлений его может ещё не нести
  const res = await page.request.get('/api/version');
  expect(res.ok(), `GET /api/version: http ${res.status()}`).toBe(true);
  const body = (await res.json()) as {
    data?: { panel_version?: string };
    panel_version?: string;
  };
  const version = body.data?.panel_version ?? body.panel_version ?? '';
  expect(version, 'панель не отдала номер сборки').not.toBe('');
  const kind = kindOf(version);

  await openUpdates(page, 'ru');
  const badge = page.locator('[data-testid="update-kind-current"]');
  await expect(badge).toHaveText(text('ru', `settings.update_kind_${kind}`), {
    timeout: T.page
  });
  const cls = { stable: 'badge-success', beta: 'badge-primary', alpha: 'badge-warning' }[kind];
  await expect(badge).toHaveClass(new RegExp(`\\b${cls}\\b`));

  await guard.assertClean();
});
