// e2e-pages: config settings
import { test, expect } from '@playwright/test';
import AxeBuilder from '@axe-core/playwright';
import { visitPage } from './helpers/api-mocks';
import { mockConfigLayer, defaultSnapshot } from './helpers/config-layer-mocks';
import { LAZY_LOAD_TIMEOUT } from './helpers/timeouts';

test.use({ locale: 'ru-RU' });

test.describe('Слой «Конфигурация»: черновик с сервера (tracer)', () => {
  test('SSE draft обновляет полосу черновика, значение переживает перезагрузку', async ({
    page
  }) => {
    const mock = await mockConfigLayer(page);
    await visitPage(page, '/#/config');

    const bar = page.getByTestId('config-draftbar');
    await expect(bar).toContainText('Черновик пуст', { timeout: LAZY_LOAD_TIMEOUT });

    await mock.waitConnected();
    await mock.emit('draft', { draft_revision: 2, draft_changes: 3 });
    await expect(bar).toContainText('Неприменённых изменений: 3');

    // Сервер помнит черновик: после F5 состояние приходит из GET /state
    mock.snapshot.draft_revision = 2;
    mock.snapshot.draft_changes = 3;
    await page.reload();
    await expect(page.getByTestId('config-draftbar')).toContainText('Неприменённых изменений: 3', {
      timeout: LAZY_LOAD_TIMEOUT
    });
    expect(mock.callsTo('GET', '/state').length).toBeGreaterThanOrEqual(2);
  });
});

test.describe('Слой «Конфигурация»: сбой чтения настроек', () => {
  test('GET /api/settings упал: вместо вечного скелетона — «Повторить», повтор открывает раздел', async ({
    page
  }) => {
    await mockConfigLayer(page);
    let failures = 1;
    // Позже зарегистрированный маршрут срабатывает первым: первый ответ — 500, дальше мок слоя
    await page.route('**/api/settings', async (route) => {
      if (route.request().method() === 'GET' && failures > 0) {
        failures -= 1;
        await route.fulfill({
          status: 500,
          contentType: 'application/json',
          body: JSON.stringify({ error: 'boom' })
        });
        return;
      }
      await route.fallback();
    });
    await visitPage(page, '/#/config');

    const retry = page.getByRole('button', { name: 'Повторить' });
    await expect(retry).toBeVisible({ timeout: LAZY_LOAD_TIMEOUT });
    await expect(page.getByText('Не удалось загрузить настройки панели')).toBeVisible();

    await retry.click();
    await expect(page.getByTestId('config-draftbar')).toBeVisible({ timeout: LAZY_LOAD_TIMEOUT });
  });
});

test.describe('Слой «Конфигурация»: состояния полосы черновика', () => {
  test('dirty + drift: применение заблокировано, сброс доступен', async ({ page }) => {
    await mockConfigLayer(page, { snapshot: { draft_changes: 3, drift_count: 2 } });
    await visitPage(page, '/#/config');
    const bar = page.getByTestId('config-draftbar');
    await expect(bar).toContainText('Неприменённых изменений: 3', { timeout: LAZY_LOAD_TIMEOUT });
    await expect(bar).toContainText('Применение заблокировано: есть расхождения файлов (2)');
    const apply = page.getByTestId('config-apply');
    await expect(apply).toBeDisabled();
    await expect(apply).toHaveAttribute('title', 'Сначала решите расхождения в списке файлов');
    await expect(bar.getByRole('button', { name: 'Сбросить' })).toBeEnabled();
  });

  test('clean: обе кнопки недоступны', async ({ page }) => {
    await mockConfigLayer(page);
    await visitPage(page, '/#/config');
    const bar = page.getByTestId('config-draftbar');
    await expect(bar).toContainText('Черновик пуст — всё применено', {
      timeout: LAZY_LOAD_TIMEOUT
    });
    await expect(page.getByTestId('config-apply')).toBeDisabled();
    await expect(bar.getByRole('button', { name: 'Сбросить' })).toBeDisabled();
  });

  test('до загрузки состояния кнопки недоступны', async ({ page }) => {
    const mock = await mockConfigLayer(page, { snapshot: { draft_changes: 3 } });
    let release: () => void = () => {};
    const gate = new Promise<void>((resolve) => (release = resolve));
    await page.route('**/api/configlayer/state', async (route) => {
      await gate;
      await route.fallback();
    });
    await visitPage(page, '/#/config');
    await expect(page.getByTestId('config-draftbar')).toBeVisible({ timeout: LAZY_LOAD_TIMEOUT });
    await expect(page.getByTestId('config-apply')).toBeDisabled();
    release();
    await expect(page.getByTestId('config-apply')).toBeEnabled();
    expect(mock.callsTo('GET', '/state').length).toBeGreaterThanOrEqual(1);
  });

  test('сбой первой загрузки: заглушка с кнопкой «Повторить»', async ({ page }) => {
    const mock = await mockConfigLayer(page, { failState: true });
    await visitPage(page, '/#/config');
    await expect(page.getByText('Не удалось загрузить состояние')).toBeVisible({
      timeout: LAZY_LOAD_TIMEOUT
    });
    mock.failState = false;
    await page.getByRole('button', { name: 'Повторить' }).click();
    await expect(page.getByTestId('config-draftbar')).toContainText('Черновик пуст');
  });

  test('применение из этой вкладки: POST apply, затем «Применяется…»', async ({ page }) => {
    const mock = await mockConfigLayer(page, { snapshot: { draft_changes: 3 } });
    await visitPage(page, '/#/config');
    const apply = page.getByTestId('config-apply');
    await expect(apply).toBeEnabled({ timeout: LAZY_LOAD_TIMEOUT });
    await mock.waitConnected();
    await apply.click();
    await expect.poll(() => mock.callsTo('POST', '/configlayer/apply').length).toBe(1);
    await mock.emit('apply_step', { id: 'build', state: 'running' });
    const bar = page.getByTestId('config-draftbar');
    await expect(bar).toContainText('Применяется…');
    await expect(apply).toBeDisabled();
    await expect(bar.getByRole('button', { name: 'Сбросить' })).toBeDisabled();
  });

  test('неудачное применение: полоса снова «есть изменения», черновик цел', async ({ page }) => {
    const mock = await mockConfigLayer(page, { snapshot: { draft_changes: 3 } });
    await visitPage(page, '/#/config');
    await expect(page.getByTestId('config-apply')).toBeEnabled({ timeout: LAZY_LOAD_TIMEOUT });
    await mock.waitConnected();
    await page.getByTestId('config-apply').click();
    await mock.emit('apply_step', { id: 'build', state: 'running' });
    await mock.emit('apply_done', {
      running: false,
      steps: [{ id: 'build', state: 'failed', message: 'boom' }],
      result: { ok: false, code: 'build_failed', written: 0 }
    });
    const bar = page.getByTestId('config-draftbar');
    await expect(bar).toContainText('Неприменённых изменений: 3');
    await expect(page.getByTestId('config-apply')).toBeEnabled();
  });

  test('применение из другой вкладки: подпись и недоступные кнопки', async ({ page }) => {
    await mockConfigLayer(page, {
      snapshot: {
        draft_changes: 3,
        apply: { running: true, steps: [{ id: 'build', state: 'running' }] }
      }
    });
    await visitPage(page, '/#/config');
    const bar = page.getByTestId('config-draftbar');
    await expect(bar).toContainText('Применение запущено в другой вкладке', {
      timeout: LAZY_LOAD_TIMEOUT
    });
    await expect(page.getByTestId('config-apply')).toBeDisabled();
    await expect(bar.getByRole('button', { name: 'Сбросить' })).toBeDisabled();
  });

  test('обрыв SSE: «Нет связи…», после восстановления состояние читается заново', async ({
    page
  }) => {
    const mock = await mockConfigLayer(page, { snapshot: { draft_changes: 3 } });
    await visitPage(page, '/#/config');
    const bar = page.getByTestId('config-draftbar');
    await expect(bar).toContainText('Неприменённых изменений: 3', { timeout: LAZY_LOAD_TIMEOUT });
    await mock.waitConnected();
    const before = mock.callsTo('GET', '/state').length;
    await mock.dropConnection();
    await expect(bar).toContainText('Нет связи с панелью. Переподключаемся…');
    await mock.reopen();
    await expect
      .poll(() => mock.callsTo('GET', '/state').length, { timeout: 10_000 })
      .toBeGreaterThan(before);
    await expect(bar).not.toContainText('Нет связи с панелью');
  });

  test('«Сбросить»: подтверждение и POST draft/reset с ревизией', async ({ page }) => {
    const mock = await mockConfigLayer(page, { snapshot: { draft_changes: 3, draft_revision: 5 } });
    await visitPage(page, '/#/config');
    const bar = page.getByTestId('config-draftbar');
    await expect(bar.getByRole('button', { name: 'Сбросить' })).toBeEnabled({
      timeout: LAZY_LOAD_TIMEOUT
    });
    await bar.getByRole('button', { name: 'Сбросить' }).click();
    const dialog = page.getByRole('dialog');
    await expect(dialog).toContainText('Сбросить черновик?');
    await expect(dialog).toContainText('Неприменённые изменения (3) будут потеряны.');
    await expect(dialog).toContainText('Файлы на диске не изменятся.');
    await dialog.getByRole('button', { name: 'Сбросить' }).click();
    await expect.poll(() => mock.callsTo('POST', '/draft/reset').length).toBe(1);
    expect(mock.callsTo('POST', '/draft/reset')[0].body).toEqual({ revision: 5 });
    await expect(bar).toContainText('Черновик пуст — всё применено');
  });

  test('«Применить» не просит подтверждения', async ({ page }) => {
    const mock = await mockConfigLayer(page, { snapshot: { draft_changes: 1 } });
    await visitPage(page, '/#/config');
    await expect(page.getByTestId('config-apply')).toBeEnabled({ timeout: LAZY_LOAD_TIMEOUT });
    await page.getByTestId('config-apply').click();
    await expect.poll(() => mock.callsTo('POST', '/configlayer/apply').length).toBe(1);
    await expect(page.getByRole('dialog')).toHaveCount(0);
  });

  test('409 draft_conflict на сбросе: тост и повторный GET state', async ({ page }) => {
    const mock = await mockConfigLayer(page, { snapshot: { draft_changes: 3 } });
    await visitPage(page, '/#/config');
    const bar = page.getByTestId('config-draftbar');
    await expect(bar.getByRole('button', { name: 'Сбросить' })).toBeEnabled({
      timeout: LAZY_LOAD_TIMEOUT
    });
    mock.draftConflict = true;
    const before = mock.callsTo('GET', '/state').length;
    await bar.getByRole('button', { name: 'Сбросить' }).click();
    await page.getByRole('dialog').getByRole('button', { name: 'Сбросить' }).click();
    await expect(
      page.getByText('Черновик изменён в другой вкладке. Показана свежая версия')
    ).toBeVisible();
    await expect.poll(() => mock.callsTo('GET', '/state').length).toBeGreaterThan(before);
  });

  test('409 apply_busy на применении: тост «Применение уже идёт»', async ({ page }) => {
    const mock = await mockConfigLayer(page, { snapshot: { draft_changes: 3 }, applyBusy: true });
    await visitPage(page, '/#/config');
    await expect(page.getByTestId('config-apply')).toBeEnabled({ timeout: LAZY_LOAD_TIMEOUT });
    await page.getByTestId('config-apply').click();
    await expect(page.getByText('Применение уже идёт')).toBeVisible();
    await expect(page.getByTestId('config-draftbar')).toContainText('Неприменённых изменений: 3');
    expect(mock.callsTo('POST', '/configlayer/apply')).toHaveLength(1);
  });
});

test.describe('Слой «Конфигурация»: пункт меню и бейдж', () => {
  test('пункт «Конфигурация» стоит сразу после «Дашборда» в группе «Обзор»', async ({ page }) => {
    await mockConfigLayer(page);
    await visitPage(page, '/#/dashboard');
    const group = page.locator('details.nav-group').first();
    const items = group.locator('a.nav-item');
    await expect(items.nth(1)).toHaveAttribute('href', '#/config', { timeout: LAZY_LOAD_TIMEOUT });
    await expect(items.nth(0)).toHaveAttribute('href', '#/dashboard');
    await expect(items.nth(1)).toContainText('Конфигурация');
  });

  for (const [count, label] of [
    [3, '3'],
    [120, '99+']
  ] as const) {
    test(`бейдж меню: ${count} → «${label}», с aria-label`, async ({ page }) => {
      await mockConfigLayer(page, { snapshot: { draft_changes: count } });
      await visitPage(page, '/#/dashboard');
      const badge = page.locator('.nav-badge-config');
      await expect(badge).toHaveText(label, { timeout: LAZY_LOAD_TIMEOUT });
      await expect(badge).toHaveAttribute('role', 'img');
      await expect(badge).toHaveAttribute('aria-label', `Неприменённых изменений: ${count}`);
    });
  }

  test('бейдж скрыт при нуле и обновляется событием draft на другой странице', async ({ page }) => {
    const mock = await mockConfigLayer(page, { snapshot: defaultSnapshot() });
    await visitPage(page, '/#/dashboard');
    await expect(page.locator('a.nav-item[href="#/config"]')).toBeVisible({
      timeout: LAZY_LOAD_TIMEOUT
    });
    await expect(page.locator('.nav-badge-config')).toHaveCount(0);
    await mock.waitConnected();
    await mock.emit('draft', { draft_revision: 2, draft_changes: 4 });
    await expect(page.locator('.nav-badge-config')).toHaveText('4');
  });
});

test.describe('Слой «Конфигурация»: флаг в настройках', () => {
  const row = (page: import('@playwright/test').Page) => page.getByTestId('config-layer-row');

  test('флаг выключен: пункта меню нет, #/config открывает дашборд', async ({ page }) => {
    await mockConfigLayer(page, { flag: false });
    await visitPage(page, '/#/config');
    await expect(page).toHaveURL(/#\/dashboard$/, { timeout: LAZY_LOAD_TIMEOUT });
    await expect(page.locator('a.nav-item[href="#/config"]')).toHaveCount(0);
    await expect(page.getByTestId('config-draftbar')).toHaveCount(0);
  });

  test('включение из настроек: POST {enabled:true}, тост, пункт меню появился', async ({
    page
  }) => {
    const mock = await mockConfigLayer(page, { flag: false });
    await visitPage(page, '/#/settings');
    await expect(row(page)).toBeVisible({ timeout: LAZY_LOAD_TIMEOUT });
    await expect(page.locator('a.nav-item[href="#/config"]')).toHaveCount(0);
    await row(page).locator('.toggle-slider').click();
    await expect.poll(() => mock.callsTo('POST', '/settings/config-layer').length).toBe(1);
    expect(mock.callsTo('POST', '/settings/config-layer')[0].body).toEqual({ enabled: true });
    await expect(page.getByText('Слой «Конфигурация» включён')).toBeVisible();
    await expect(page.locator('a.nav-item[href="#/config"]')).toBeVisible();
    await expect(row(page).getByRole('checkbox')).toBeChecked();
  });

  test('выключение: диалог, POST {enabled:false}, пункт пропал, с #/config — на дашборд', async ({
    page
  }) => {
    const mock = await mockConfigLayer(page, { flag: true });
    await visitPage(page, '/#/settings');
    await expect(row(page).getByRole('checkbox')).toBeChecked({ timeout: LAZY_LOAD_TIMEOUT });
    await row(page).locator('.toggle-slider').click();
    const dialog = page.getByRole('dialog');
    await expect(dialog).toContainText('Выключить слой «Конфигурация»?');
    await expect(dialog).toContainText(
      'Файлы панели xcp-* будут перенесены в резервную копию. Запущенное ядро перезапустится.'
    );
    await expect(dialog).toContainText('Панель вернётся к работе как до включения слоя.');
    await dialog.getByRole('button', { name: 'Выключить' }).click();
    await expect.poll(() => mock.callsTo('POST', '/settings/config-layer').length).toBe(1);
    expect(mock.callsTo('POST', '/settings/config-layer')[0].body).toEqual({ enabled: false });
    await expect(
      page.getByText('Слой «Конфигурация» выключен, файлы панели перенесены в резервную копию')
    ).toBeVisible();
    await expect(page.locator('a.nav-item[href="#/config"]')).toHaveCount(0);
    await page.evaluate(() => {
      window.location.hash = '#/config';
    });
    await expect(page).toHaveURL(/#\/dashboard$/);
  });

  test('отказ в диалоге выключения оставляет переключатель включённым', async ({ page }) => {
    const mock = await mockConfigLayer(page, { flag: true });
    await visitPage(page, '/#/settings');
    await expect(row(page).getByRole('checkbox')).toBeChecked({ timeout: LAZY_LOAD_TIMEOUT });
    await row(page).locator('.toggle-slider').click();
    await page.getByRole('dialog').getByRole('button', { name: 'Отмена' }).click();
    await expect(row(page).getByRole('checkbox')).toBeChecked();
    expect(mock.callsTo('POST', '/settings/config-layer')).toHaveLength(0);
  });

  test('ошибка выключения: переключатель снова включён, тост с текстом сервера', async ({
    page
  }) => {
    await mockConfigLayer(page, {
      flag: true,
      settingsError: {
        status: 500,
        code: 'config_layer_disable_failed',
        message: 'Не удалось перенести файлы панели'
      }
    });
    await visitPage(page, '/#/settings');
    await expect(row(page).getByRole('checkbox')).toBeChecked({ timeout: LAZY_LOAD_TIMEOUT });
    await row(page).locator('.toggle-slider').click();
    await page.getByRole('dialog').getByRole('button', { name: 'Выключить' }).click();
    await expect(page.getByText('Не удалось перенести файлы панели')).toBeVisible();
    await expect(row(page).getByRole('checkbox')).toBeChecked();
    await expect(row(page).getByRole('checkbox')).toBeEnabled();
    await expect(page.locator('a.nav-item[href="#/config"]')).toBeVisible();
  });

  test('во время запроса переключатель заблокирован со спиннером', async ({ page }) => {
    await mockConfigLayer(page, { flag: false, settingsDelayMs: 1200 });
    await visitPage(page, '/#/settings');
    await expect(row(page)).toBeVisible({ timeout: LAZY_LOAD_TIMEOUT });
    await row(page).locator('.toggle-slider').click();
    await expect(row(page).getByRole('checkbox')).toBeDisabled();
    await expect(row(page).locator('.spinner')).toBeVisible();
    await expect(row(page).getByRole('checkbox')).toBeEnabled({ timeout: 5000 });
    await expect(row(page).locator('.spinner')).toHaveCount(0);
  });

  test('во время применения переключатель недоступен с подсказкой', async ({ page }) => {
    await mockConfigLayer(page, {
      flag: true,
      snapshot: { apply: { running: true, steps: [{ id: 'build', state: 'running' }] } }
    });
    await visitPage(page, '/#/settings');
    await expect(row(page).getByRole('checkbox')).toBeDisabled({ timeout: LAZY_LOAD_TIMEOUT });
    await expect(row(page).locator('label.toggle-switch')).toHaveAttribute(
      'title',
      'Дождитесь окончания применения'
    );
  });
});

test.describe('Слой «Конфигурация»: узкий экран и доступность', () => {
  const THEMES = ['dark', 'light'] as const;

  for (const theme of THEMES) {
    test(`390 px: #/config и #/settings без горизонтальной прокрутки (${theme})`, async ({
      page
    }) => {
      await page.setViewportSize({ width: 390, height: 844 });
      await page.addInitScript((t) => localStorage.setItem('theme', t), theme);
      await mockConfigLayer(page, { snapshot: { draft_changes: 3, drift_count: 2 } });

      await visitPage(page, '/#/config');
      await expect(page.getByTestId('config-draftbar')).toContainText('Неприменённых изменений', {
        timeout: LAZY_LOAD_TIMEOUT
      });
      const overflowConfig = await page.evaluate(
        () => document.scrollingElement!.scrollWidth - document.scrollingElement!.clientWidth
      );
      expect(overflowConfig, '#/config не шире окна').toBeLessThanOrEqual(0);

      // Кнопки полосы — второй строкой на всю ширину, не ниже 44 px
      const reset = page.getByTestId('config-draftbar').getByRole('button', { name: 'Сбросить' });
      const apply = page.getByTestId('config-apply');
      const resetBox = (await reset.boundingBox())!;
      const applyBox = (await apply.boundingBox())!;
      expect(resetBox.height).toBeGreaterThanOrEqual(43.5);
      expect(applyBox.height).toBeGreaterThanOrEqual(43.5);
      expect(Math.abs(resetBox.y - applyBox.y)).toBeLessThan(2);

      await page.evaluate(() => {
        window.location.hash = '#/settings';
      });
      await expect(page.getByTestId('config-layer-row')).toBeVisible({
        timeout: LAZY_LOAD_TIMEOUT
      });
      const overflowSettings = await page.evaluate(
        () => document.scrollingElement!.scrollWidth - document.scrollingElement!.clientWidth
      );
      expect(overflowSettings, '#/settings не шире окна').toBeLessThanOrEqual(0);
    });

    test(`390 px: липкая полоса не уходит под мобильную шапку (${theme})`, async ({ page }) => {
      await page.setViewportSize({ width: 390, height: 844 });
      await page.addInitScript((t) => localStorage.setItem('theme', t), theme);
      await mockConfigLayer(page, { snapshot: { draft_changes: 3 } });
      await visitPage(page, '/#/config');
      await expect(page.getByTestId('config-apply')).toBeEnabled({ timeout: LAZY_LOAD_TIMEOUT });
      // Остальные секции раздела добавляют следующие планы; их высоту заменяет распорка в стеке
      await page.evaluate(() => {
        const spacer = document.createElement('div');
        spacer.style.height = '2000px';
        document.querySelector('.config-stack')!.appendChild(spacer);
        window.scrollTo(0, 500);
      });
      await page.waitForTimeout(200);
      const header = (await page.locator('.mobile-header').boundingBox())!;
      const bar = (await page.getByTestId('config-draftbar').boundingBox())!;
      expect(bar.y).toBeGreaterThanOrEqual(header.y + header.height - 0.5);
      expect(bar.y).toBeLessThan(header.y + header.height + 8);
    });

    test(`axe: #/config и #/settings без серьёзных нарушений (${theme})`, async ({ page }) => {
      await page.addInitScript((t) => localStorage.setItem('theme', t), theme);
      await mockConfigLayer(page, { snapshot: { draft_changes: 3, drift_count: 1 } });

      for (const route of ['/#/config', '/#/settings']) {
        await visitPage(page, route);
        await expect(page.locator('.main-content .page-header h1').first()).toBeVisible({
          timeout: LAZY_LOAD_TIMEOUT
        });
        if (route.endsWith('config')) {
          await expect(page.getByTestId('config-draftbar')).toContainText('Неприменённых');
          await expect(page.locator('.nav-badge-config')).toHaveAttribute(
            'aria-label',
            'Неприменённых изменений: 3'
          );
        } else {
          await expect(page.getByTestId('config-layer-row')).toBeVisible();
        }
        await page.evaluate((t) => {
          document.documentElement.setAttribute('data-theme', t);
        }, theme);
        await page.waitForLoadState('networkidle');
        // Появление страницы идёт с fade: contrast считается по полупрозрачным цветам, пока анимация не кончилась
        await page.waitForFunction(() =>
          document.getAnimations().every((a) => a.effect?.getTiming().iterations === Infinity)
        );
        const results = await new AxeBuilder({ page })
          .withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa'])
          .exclude('.cm-editor')
          .exclude('.xterm')
          .analyze();
        const severe = results.violations.filter(
          (v) => v.impact === 'critical' || v.impact === 'serious'
        );
        expect(
          severe.map((v) => ({ id: v.id, nodes: v.nodes.map((n) => n.target) })),
          `${route} (${theme})`
        ).toEqual([]);
      }
    });
  }
});
