// e2e-pages: traffic proxies
// router-area: routing
import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { test, expect, type Browser, type Page } from '@playwright/test';
import { axeSevere } from './lib/axe';
import { attachConsoleGuard } from './lib/console';
import { RT, T } from './lib/env';
import { CORE_TAG } from './lib/known';
import { escapeRe } from './lib/regex';
import {
  addToDraft,
  api,
  nodeLink,
  randomPort,
  resetDraft,
  waitApplyIdle,
  withPanel
} from './lib/nodes-api';
import {
  myEntryIds,
  restoreRouting,
  routingInfo,
  takeAndApply,
  type RoutingStand
} from './lib/routing-api';

// Вкладка «Лимиты» на «Трафике», меню и прежние адреса (фаза 152, SPX-03, SPX-04, D-13, D-15):
// «Трафик» в меню под обоими ядрами, пунктов Smart Proxy и квот нет; вкладка по адресу ?tab=,
// перенаправления #/smartproxy и #/trafficquotas; создание, правка, сброс счётчика и удаление
// лимита «Весь трафик» с подтверждениями; в форме нет throttle и log_only, у цели «авто»
// блокировка и «напрямую» недоступны; axe вкладки и модалки в обеих темах. Цели формы — группы
// применённой маршрутизации, поэтому подготовка заводит два узла «Моих узлов» на портах без
// сервера и группу «авто», берёт маршрутизацию и применяет. Лимиты теста — с префиксом RTTL-,
// в конце удаляются через API; группы и узлы убираются, маршрутизация отпускается, если не была
// взята, черновик сбрасывается. Ответы сети не подменяются.

const PREFIX = 'RTTL-';
const LIMIT_NAME = `${PREFIX}весь трафик`;
const GB = 1024 ** 3;

const stand: RoutingStand = { groupIds: [], myBefore: [], wasManaged: false };
let autoId = '';
let standRead = false;

test.describe.configure({ mode: 'serial' });

interface LimitRow {
  id: string;
  name: string;
  target_type: string;
  target_id: string;
  limit_bytes: number;
  action: string;
}

interface LimitsList {
  limits: LimitRow[];
  data: string;
  kernel: string;
}

const LOCALES = ['ru', 'en'].map(
  (lang) =>
    JSON.parse(
      readFileSync(
        fileURLToPath(new URL(`../../src/locales/${lang}.json`, import.meta.url)),
        'utf8'
      )
    ) as Record<string, string>
);

/**
 * Текст ключа на обоих языках (язык панели стенда заранее неизвестен); параметры {…} —
 * любой текст. exact — строка целиком.
 */
function say(key: string, exact = false): RegExp {
  const parts = LOCALES.map((l) => {
    const text = l[key];
    if (!text) throw new Error(`ключа ${key} нет в словаре`);
    return escapeRe(text).replace(/\\\{\w+\\\}/g, '.*?');
  });
  const body = `(${parts.join('|')})`;
  return new RegExp(exact ? `^${body}$` : body);
}

async function limitsList(page: Page): Promise<LimitsList> {
  const res = await api(page, 'GET', '/api/traffic/limits');
  expect(res.status, 'GET /api/traffic/limits').toBe(200);
  return {
    limits: (res.data?.limits ?? []) as LimitRow[],
    data: String(res.data?.data ?? ''),
    kernel: String(res.data?.kernel ?? '')
  };
}

/** Удаляет лимиты теста (по префиксу имени); ошибки уборки не роняют тест. */
async function dropLimits(page: Page): Promise<void> {
  const list = await limitsList(page).catch(() => null);
  for (const l of list?.limits ?? []) {
    if (!l.name.startsWith(PREFIX)) continue;
    await api(page, 'POST', '/api/traffic/limits/delete', { id: l.id }).catch(() => null);
  }
}

async function openTraffic(page: Page, hash: string, width = 1280): Promise<void> {
  await page.setViewportSize({ width, height: width <= 480 ? 844 : 900 });
  await page.goto(`/${hash}`, { timeout: T.page, waitUntil: 'domcontentloaded' });
  await expect(page.locator('.main-content .page-header h1').first()).toBeVisible({
    timeout: T.page
  });
}

/** Вкладка «Лимиты» загружена: список пришёл (скелетона нет). */
async function limitsReady(page: Page): Promise<void> {
  const tab = page.getByTestId('traffic-limits');
  await expect(tab).toBeVisible({ timeout: T.page });
  await expect(tab.locator('[aria-busy="true"]')).toHaveCount(0, { timeout: T.page });
}

const hashOf = (page: Page) => page.evaluate(() => window.location.hash);

async function prepare(browser: Browser): Promise<void> {
  await withPanel(browser, async (page) => {
    await resetDraft(page);
    stand.wasManaged = (await routingInfo(page)).desired;
    stand.myBefore = await myEntryIds(page);
    standRead = true;
    await dropLimits(page);
    const port = randomPort();
    await addToDraft(
      page,
      [
        nodeLink('vless_ws.txt', port, `${PREFIX}a`),
        nodeLink('trojan_grpc.txt', port + 1, `${PREFIX}b`)
      ].join('\n')
    );
    const res = await api(page, 'POST', '/api/configlayer/routing/groups/save', {
      revision: (await routingInfo(page)).revision,
      group: {
        id: '',
        name: 'RT Лимиты авто',
        mode: 'auto',
        sources: { all: false, ids: ['my'] },
        include: [PREFIX],
        exclude: [],
        regex: false,
        picks: [],
        members: [],
        reserve: ''
      }
    });
    expect(res.status, 'сохранение группы').toBe(200);
    autoId = String((res.data?.group as { id?: string } | undefined)?.id ?? '');
    expect(autoId, 'идентификатор группы').not.toBe('');
    stand.groupIds = [autoId];
    await takeAndApply(page);
    // признак «применена» бэкенд ставит только ручным группам: для «авто» достаточно записи в списке
    const created = (await routingInfo(page)).groups.find((g) => g.id === autoId);
    expect(created?.mode, 'группа «авто» теста есть в списке').toBe('auto');
  });
}

test.beforeAll(async ({ browser }) => {
  // сброс черновика, узлы, группа «авто», взятие маршрутизации и «Применить» с перезапуском ядра
  test.setTimeout(T.save * 8);
  await withPanel(browser, (page) => resetDraft(page));
  await prepare(browser);
});

test.afterAll(async ({ browser }) => {
  test.setTimeout(T.save * 6);
  try {
    if (standRead) {
      await withPanel(browser, async (page) => {
        await dropLimits(page);
        await restoreRouting(page, stand);
      });
    }
  } finally {
    await withPanel(browser, async (page) => {
      await waitApplyIdle(page, 'перед сбросом черновика');
      await resetDraft(page);
    });
  }
});

test('traffic-limits: меню, вкладки и прежние адреса', { tag: CORE_TAG }, async ({ page }) => {
  const guard = attachConsoleGuard(page);
  await openTraffic(page, '#/traffic');

  // «Трафик» в меню под любым ядром; пунктов Smart Proxy и квот больше нет (SPX-03)
  await expect(page.locator('.sidebar-nav a[href="#/traffic"]')).toHaveCount(1, {
    timeout: T.page
  });
  await expect(page.locator('a[href="#/smartproxy"]')).toHaveCount(0);
  await expect(page.locator('a[href="#/trafficquotas"]')).toHaveCount(0);

  // Без tab — «Статистика»
  await expect(page.getByTestId('traffic-tab-stats')).toHaveAttribute('aria-selected', 'true', {
    timeout: T.page
  });
  await expect(page.getByTestId('traffic-panel-stats')).toBeVisible();
  // Под Xray живого потока Mihomo нет: вместо графиков — пояснение
  const mihomoOnly = page.getByTestId('traffic-xray-mihomo-only');
  if (RT.core === 'xray') {
    await expect(mihomoOnly).toBeVisible({ timeout: T.page });
    await expect(mihomoOnly).toHaveClass(/alert/);
  } else if (RT.core === 'mihomo') {
    await expect(mihomoOnly).toHaveCount(0);
  }

  // ?tab=limits — «Лимиты»
  await openTraffic(page, '#/traffic?tab=limits');
  await expect(page.getByTestId('traffic-tab-limits')).toHaveAttribute('aria-selected', 'true', {
    timeout: T.page
  });
  await expect(page.getByTestId('traffic-tab-stats')).toHaveAttribute('aria-selected', 'false');
  await limitsReady(page);

  // Неизвестное значение tab — «Статистика»
  await openTraffic(page, '#/traffic?tab=bogus');
  await expect(page.getByTestId('traffic-tab-stats')).toHaveAttribute('aria-selected', 'true', {
    timeout: T.page
  });
  await expect(page.getByTestId('traffic-limits')).toHaveCount(0);

  // Прежние адреса: Smart Proxy — «Прокси», квоты — «Трафик → Лимиты»
  await page.goto('/#/smartproxy', { timeout: T.page, waitUntil: 'domcontentloaded' });
  await expect.poll(() => hashOf(page), { timeout: T.page }).toBe('#/proxies');
  await expect(page.getByTestId('proxies-layer-page')).toBeVisible({ timeout: T.page });

  await page.goto('/#/trafficquotas', { timeout: T.page, waitUntil: 'domcontentloaded' });
  await expect.poll(() => hashOf(page), { timeout: T.page }).toBe('#/traffic?tab=limits');
  await expect(page.getByTestId('traffic-tab-limits')).toHaveAttribute('aria-selected', 'true', {
    timeout: T.page
  });
  await limitsReady(page);
  await guard.assertClean();
});

test(
  'traffic-limits: создать, изменить, сбросить и удалить лимит',
  { tag: CORE_TAG },
  async ({ page }) => {
    test.setTimeout(T.page * 4);
    const guard = attachConsoleGuard(page);
    try {
      await openTraffic(page, '#/traffic?tab=limits');
      await limitsReady(page);
      const tab = page.getByTestId('traffic-limits');
      const before = await limitsList(page);
      const empty = tab.getByText(say('traffic.limits.empty_title', true));
      if (before.limits.length === 0) {
        await expect(empty).toBeVisible({ timeout: T.action });
      } else {
        test.info().annotations.push({
          type: 'skip-part',
          description: `на стенде ${before.limits.length} лимитов: пустое состояние не проверено`
        });
      }

      // Форма: действий throttle и log_only нет (D-15)
      await page.getByTestId('traffic-limit-add').click();
      const modal = page.getByTestId('traffic-limit-modal');
      await expect(modal).toBeVisible({ timeout: T.action });
      const action = page.getByTestId('traffic-limit-field-action');
      expect(
        await action
          .locator('option')
          .evaluateAll((els) => els.map((e) => (e as HTMLOptionElement).value)),
        'действия формы лимита'
      ).toEqual(['notify', 'block', 'direct']);

      // Цель — группа «авто»: блокировка и «напрямую» недоступны, подсказка (D-13)
      const target = page.getByTestId('traffic-limit-field-target');
      await target.selectOption(autoId);
      await expect(action.locator('option[value="block"]')).toBeDisabled();
      await expect(action.locator('option[value="direct"]')).toBeDisabled();
      await expect(action).toHaveValue('notify');
      await expect(modal).toContainText(say('traffic.limits.hint_auto_notify'));

      // «Весь трафик» с блокировкой: подсказка о ручных группах; под Mihomo — о перечитывании
      await target.selectOption('all');
      await expect(action.locator('option[value="block"]')).toBeEnabled();
      await action.selectOption('block');
      await expect(modal).toContainText(say('traffic.limits.hint_all_block'));
      const reload = page.getByTestId('traffic-limit-hint-reload');
      if (before.kernel === 'mihomo') await expect(reload).toBeVisible();
      else await expect(reload).toHaveCount(0);

      // Лимит «Весь трафик», 1 ГБ, «Уведомить»
      await action.selectOption('notify');
      await page.getByTestId('traffic-limit-field-name').fill(LIMIT_NAME);
      await page.getByTestId('traffic-limit-field-limit').fill('1');
      await page.getByTestId('traffic-limit-field-unit').selectOption('GB');
      await page.getByTestId('traffic-limit-save').click();
      await expect(modal).toHaveCount(0, { timeout: T.save });
      await expect(
        page.locator('.toast').filter({ hasText: say('traffic.limits.toast_saved') })
      ).toBeVisible({ timeout: T.action });

      const created = (await limitsList(page)).limits.find((l) => l.name === LIMIT_NAME);
      expect(created, 'лимит теста в списке').toBeTruthy();
      expect(created!.target_type, 'цель — весь трафик').toBe('all');
      expect(created!.action, 'действие — уведомить').toBe('notify');
      expect(created!.limit_bytes, 'лимит 1 ГБ').toBe(GB);
      const id = created!.id;
      const card = page.getByTestId(`traffic-limit-${id}`);
      await expect(card).toBeVisible({ timeout: T.action });
      // перед названием цели стоит иконка: текст сравнивается без крайних пробелов
      await expect
        .poll(
          async () =>
            ((await page.getByTestId(`traffic-limit-target-${id}`).textContent()) ?? '').trim(),
          { timeout: T.action }
        )
        .toMatch(say('traffic.limits.target_all', true));
      // Расход «X из Y», когда счётчики ядра доступны; иначе «нет данных»
      const data = (await limitsList(page)).data;
      await expect(card).toContainText(
        data === 'ok' ? say('traffic.limits.of') : say('traffic.limits.no_data'),
        { timeout: T.action }
      );
      await expect(card.getByRole('progressbar')).toBeVisible();
      await expect(empty).toHaveCount(0);

      // Правка: 2 ГБ
      await page.getByTestId(`traffic-limit-edit-${id}`).click();
      await expect(modal).toBeVisible({ timeout: T.action });
      await expect(page.getByTestId('traffic-limit-field-name')).toHaveValue(LIMIT_NAME);
      await page.getByTestId('traffic-limit-field-limit').fill('2');
      await page.getByTestId('traffic-limit-save').click();
      await expect(modal).toHaveCount(0, { timeout: T.save });
      await expect
        .poll(async () => (await limitsList(page)).limits.find((l) => l.id === id)?.limit_bytes, {
          message: 'лимит изменён',
          timeout: T.action
        })
        .toBe(2 * GB);

      // Сброс счётчика: «Не сбрасывать» ничего не делает, «Сбросить счётчик» — сбрасывает
      const dialog = page.getByRole('dialog');
      const resetToast = page
        .locator('.toast')
        .filter({ hasText: say('traffic.limits.toast_reset') });
      await page.getByTestId(`traffic-limit-reset-${id}`).click();
      await expect(
        dialog.getByRole('button', { name: say('traffic.limits.reset_confirm', true) })
      ).toBeVisible({ timeout: T.action });
      await dialog.getByRole('button', { name: say('traffic.limits.reset_cancel', true) }).click();
      await expect(dialog).toHaveCount(0, { timeout: T.action });
      await expect(resetToast).toHaveCount(0);
      await page.getByTestId(`traffic-limit-reset-${id}`).click();
      await dialog.getByRole('button', { name: say('traffic.limits.reset_confirm', true) }).click();
      await expect(resetToast).toBeVisible({ timeout: T.save });

      // Удаление: «Оставить лимит» и «Удалить лимит»
      await page.getByTestId(`traffic-limit-delete-${id}`).click();
      await expect(
        dialog.getByRole('button', { name: say('traffic.limits.delete_cancel', true) })
      ).toBeVisible({ timeout: T.action });
      await dialog
        .getByRole('button', { name: say('traffic.limits.delete_confirm', true) })
        .click();
      await expect(
        page.locator('.toast').filter({ hasText: say('traffic.limits.toast_deleted') })
      ).toBeVisible({ timeout: T.save });
      await expect(card).toHaveCount(0, { timeout: T.action });
      expect(
        (await limitsList(page)).limits.some((l) => l.id === id),
        'лимит удалён на сервере'
      ).toBe(false);
      if (before.limits.length === 0) await expect(empty).toBeVisible({ timeout: T.action });
      await guard.assertClean();
    } finally {
      await dropLimits(page);
    }
  }
);

for (const theme of ['light', 'dark'] as const) {
  test(`traffic-limits axe: вкладка и модалка, тема ${theme}`, async ({ page }) => {
    test.setTimeout(T.page * 3);
    const guard = attachConsoleGuard(page);
    await page.addInitScript((th) => localStorage.setItem('theme', th), theme);
    const problems: string[] = [];
    for (const width of [390, 1280]) {
      await openTraffic(page, '#/traffic?tab=limits', width);
      expect(
        await page.evaluate(() => document.documentElement.getAttribute('data-theme')),
        'применена тема'
      ).toBe(theme);
      await limitsReady(page);
      for (const v of await axeSevere(page, '[data-testid="traffic-limits"]')) {
        problems.push(`${width} px, вкладка: ${v}`);
      }
      await page.getByTestId('traffic-limit-add').click();
      const modal = page.getByTestId('traffic-limit-modal');
      await expect(modal).toBeVisible({ timeout: T.action });
      for (const v of await axeSevere(page, '.modal-container[role="dialog"]')) {
        problems.push(`${width} px, модалка: ${v}`);
      }
      await modal.getByRole('button', { name: say('traffic.limits.cancel', true) }).click();
      await expect(modal).toHaveCount(0, { timeout: T.action });
      if (width === 390) {
        const wide = await page.evaluate(() => ({
          scroll: document.documentElement.scrollWidth,
          inner: window.innerWidth
        }));
        if (wide.scroll > wide.inner + 1) {
          problems.push(`390 px: scrollWidth ${wide.scroll} шире окна ${wide.inner}`);
        }
      }
    }
    expect(problems, problems.join('\n')).toEqual([]);
    await guard.assertClean();
  });
}
