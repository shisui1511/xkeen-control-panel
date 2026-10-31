// e2e-pages: config/cores
import { test, expect, type Page } from '@playwright/test';
import { attachConsoleGuard } from './lib/console';
import { T } from './lib/env';
import { api, layerFlag, randomPort, resetDraft } from './lib/nodes-api';

// Формы секций Mihomo на настоящей панели (фаза 147, план 23): «Основное», DNS,
// «Входящие/TUN» и Sniffer правят профиль панели через общие блоки форм, ключи вне формы
// сохраняются, секции чужого профиля читаются только для чтения. Ответы сети не
// подменяются. Флаг слоя включается на время прогона и возвращается прежним. Правки идут
// только в черновик и сбрасываются; «Применить» не вызывается, профиль стенда не меняется.

const layer = layerFlag();

test.describe.configure({ mode: 'serial' });

test.beforeAll(async ({ browser }) => {
  await layer.enable(browser);
});

test.afterAll(async ({ browser }) => {
  await layer.restore(browser);
});

async function openConfig(page: Page, hash: string): Promise<void> {
  await page.goto(`/${hash}`, { timeout: T.page, waitUntil: 'domcontentloaded' });
  await expect(page.getByTestId('config-page')).toBeVisible({ timeout: T.page });
}

/** Страница, на которой API панели доступно из браузера (сессия и токен CSRF). */
async function startPage(page: Page): Promise<void> {
  await page.goto('/', { timeout: T.page, waitUntil: 'domcontentloaded' });
  await page.locator('.main-content').first().waitFor({ state: 'visible', timeout: T.page });
}

/** Вкладка загружена: она видна и скелетона больше нет. */
async function tabReady(page: Page, testId: string): Promise<void> {
  const tab = page.getByTestId(testId);
  await expect(tab).toBeVisible({ timeout: T.page });
  await expect(tab.locator('.skeleton')).toHaveCount(0, { timeout: T.page });
}

interface MihomoSection {
  id: string;
  owner: string;
  value: unknown;
  form: unknown;
  advanced: string;
}

interface MihomoView {
  installed: boolean;
  profileOwner: string;
  sections: MihomoSection[];
}

/** Вид секций Mihomo по настоящему API. */
async function mihomoView(page: Page): Promise<MihomoView> {
  const view = await api(page, 'GET', '/api/configlayer/cores?kernel=mihomo');
  expect(view.status, 'GET /api/configlayer/cores?kernel=mihomo').toBe(200);
  const profile = (view.data?.profile ?? {}) as { owner?: string };
  return {
    installed: view.data?.installed === true,
    profileOwner: profile.owner ?? '',
    sections: (view.data?.sections ?? []) as MihomoSection[]
  };
}

function sectionOf(view: MihomoView, id: string): MihomoSection {
  const sec = view.sections.find((s) => s.id === id);
  expect(sec, `секция ${id} в виде Mihomo`).toBeTruthy();
  return sec as MihomoSection;
}

/** Ревизия черновика из снимка слоя. */
async function draftRevision(page: Page): Promise<number> {
  const st = await api(page, 'GET', '/api/configlayer/state');
  expect(st.status, 'GET /api/configlayer/state').toBe(200);
  return Number(st.data?.draft_revision ?? 0);
}

/**
 * Профиль панели в черновике: если маршрутизацией Mihomo не управляет панель и «взять»
 * ещё не просили, желание «взять» уходит в черновик настоящим маршрутом (файлы меняются
 * только при «Применить», которого тест не вызывает).
 */
async function ensureProfileInDraft(page: Page): Promise<MihomoView> {
  let view = await mihomoView(page);
  if (view.profileOwner !== 'panel' && view.profileOwner !== 'pending') {
    const take = await api(page, 'POST', '/api/configlayer/routing/take', {
      revision: await draftRevision(page)
    });
    expect(take.status, 'желание «взять маршрутизацию» через API').toBe(200);
    view = await mihomoView(page);
  }
  expect(['panel', 'pending'], 'профиль Mihomo у панели или ждёт «Применить»').toContain(
    view.profileOwner
  );
  return view;
}

/** Открывает вкладку ядер, выбирает Mihomo и раскрывает карточку секции. */
async function openSection(page: Page, id: string): Promise<void> {
  await openConfig(page, '#/config/cores');
  await tabReady(page, 'cores-tab');
  await page.getByTestId('kernel-switch-mihomo').click();
  await expect(page.getByTestId('cores-panel-mihomo')).toBeVisible({ timeout: T.page });
  const toggle = page.getByTestId(`section-toggle-mihomo-${id}`);
  await expect(toggle).toBeVisible({ timeout: T.page });
  if ((await toggle.getAttribute('aria-expanded')) !== 'true') await toggle.click();
  await expect(toggle).toHaveAttribute('aria-expanded', 'true');
}

/** Раскрывает «Дополнительно» секции и возвращает его поле. */
async function openAdvanced(page: Page, id: string) {
  const block = page.getByTestId(`section-advanced-block-mihomo-${id}`);
  await expect(block).toBeVisible({ timeout: T.action });
  if ((await block.getAttribute('open')) === null) await block.locator('summary').click();
  const area = page.getByTestId(`section-advanced-mihomo-${id}`);
  await expect(area).toBeVisible({ timeout: T.action });
  return area;
}

test('cores: Mihomo — правка формы сохраняет ключи вне формы', async ({ page }) => {
  test.setTimeout(T.save * 4);
  const guard = attachConsoleGuard(page);
  await page.setViewportSize({ width: 1440, height: 900 });
  await startPage(page);
  const before = await mihomoView(page);
  test.skip(!before.installed, 'Mihomo не установлен');
  await resetDraft(page);

  try {
    await ensureProfileInDraft(page);
    await openSection(page, 'main');
    await expect(page.getByTestId('section-owner-mihomo-main')).toHaveAttribute(
      'data-owner',
      /^(pending|panel)$/,
      { timeout: T.action }
    );

    // подключ profile вне формы: уходит в черновик из «Дополнительно» (D-26)
    const area = await openAdvanced(page, 'main');
    const current = await area.inputValue();
    const revision = await draftRevision(page);
    const withProfile = /^profile:/m.test(current)
      ? /^\s+store-fake-ip:/m.test(current)
        ? current
        : current.replace(/^profile:[^\S\n]*$/m, 'profile:\n  store-fake-ip: true')
      : `${current.trimEnd()}\nprofile:\n  store-fake-ip: true\n`.trimStart();
    await area.fill(withProfile);
    await area.blur();
    await expect(page.getByTestId('section-raw-error-mihomo-main')).toHaveCount(0);
    if (withProfile !== current) {
      await expect.poll(() => draftRevision(page), { timeout: T.action }).toBeGreaterThan(revision);
    }

    // порт формой: после записи в черновик остальные ключи секции на месте
    const port = randomPort();
    const field = page.getByTestId('section-field-mihomo-main-mixed-port');
    await expect(field).toBeVisible({ timeout: T.action });
    await expect(field).not.toHaveAttribute('readonly', /.*/);
    await field.fill(String(port));
    await field.blur();
    await expect
      .poll(
        async () => {
          const value = sectionOf(await mihomoView(page), 'main').value as {
            'mixed-port'?: unknown;
            profile?: Record<string, unknown>;
          } | null;
          return (
            Number(value?.['mixed-port']) === port && value?.profile?.['store-fake-ip'] === true
          );
        },
        { timeout: T.action }
      )
      .toBe(true);

    // закреплённое показано текстом, полей для него нет
    await expect(page.getByTestId('section-pinned-mihomo-main')).toContainText('routing-mark');

    await guard.assertClean();
  } finally {
    await resetDraft(page);
  }
});
