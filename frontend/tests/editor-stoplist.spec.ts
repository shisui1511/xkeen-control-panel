import { test, expect, type Page } from '@playwright/test';

// Стоп-список XKeen в Редакторе: подсказка под полем имени, подтверждение перед
// созданием и переименованием, дубль без стоп-слов, значок в дереве файлов.
// Все API замоканы; служб на ПК разработчика тест не трогает.

test.use({ locale: 'ru-RU' });

const XRAY_DIR = '/opt/etc/xray/configs';
const MIHOMO_DIR = '/opt/etc/mihomo';

const xrayList = [
  { name: '04_outbounds.json', path: `${XRAY_DIR}/04_outbounds.json`, size: 900 },
  { name: 'old_config.json', path: `${XRAY_DIR}/old_config.json`, size: 400 }
];
const mihomoList = [{ name: 'config.yaml', path: `${MIHOMO_DIR}/config.yaml`, size: 1500 }];

interface Recorded {
  create: URL[];
  rename: URL[];
  save: URL[];
}

let rec: Recorded;

async function mockApi(page: Page) {
  rec = { create: [], rename: [], save: [] };

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

  const json = (body: unknown, status = 200) => ({
    status,
    contentType: 'application/json',
    body: JSON.stringify(body)
  });

  await page.route('**/api/**', async (route) => {
    const url = new URL(route.request().url());
    const p = url.pathname;

    if (p === '/api/auth/me') {
      await route.fulfill(
        json({ authenticated: true, setup_required: false, csrf_token: 'mock-csrf-token' })
      );
    } else if (p === '/api/capabilities') {
      await route.fulfill(
        json({
          success: true,
          data: {
            kernels: {
              xray: { installed: true, version: '1.8.4', channel: 'stable' },
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
      );
    } else if (p === '/api/config/list') {
      const dir = url.searchParams.get('dir') || '';
      await route.fulfill(json(dir.includes('mihomo') ? mihomoList : xrayList));
    } else if (p === '/api/config/read') {
      await route.fulfill({ status: 200, contentType: 'text/plain', body: '{}' });
    } else if (p === '/api/config/backups') {
      await route.fulfill(json([]));
    } else if (p === '/api/config/create') {
      rec.create.push(url);
      const path = url.searchParams.get('path') || '';
      const confirmed = url.searchParams.get('confirm_stoplist') === '1';
      // Имя weird-*.json зеркало не ловит, а «сервер» — ловит.
      if (path.includes('/weird-') && !confirmed) {
        await route.fulfill(
          json(
            {
              success: false,
              code: 'xkeen_stoplist_name',
              error: 'Серверный текст: имя weird будет отвергнуто XKeen'
            },
            409
          )
        );
      } else {
        await route.fulfill(json({ success: true }));
      }
    } else if (p === '/api/config/rename') {
      rec.rename.push(url);
      await route.fulfill(json({ success: true }));
    } else if (p === '/api/config/save') {
      rec.save.push(url);
      await route.fulfill(json({ success: true }));
    } else if (p === '/api/service/status') {
      await route.fulfill(
        json({ success: true, data: { is_running: true, active_kernel: 'mihomo' } })
      );
    } else if (p === '/api/templates/list') {
      await route.fulfill(json([]));
    } else {
      await route.fulfill(json({ success: true, data: {} }));
    }
  });
}

async function openCreateModal(page: Page) {
  await page.getByRole('button', { name: 'Новый файл' }).click();
  await expect(page.locator('#new-file-name')).toBeVisible();
}

test.describe('Editor: стоп-список XKeen', () => {
  test.beforeEach(async ({ page }) => {
    await mockApi(page);
    await page.goto('/#/editor');
    await expect(page.locator('.file-row:has-text("04_outbounds.json")')).toBeVisible();
  });

  test('создание стоп-имени в корне Xray: отмена не шлёт запрос, подтверждение шлёт флаг', async ({
    page
  }) => {
    await openCreateModal(page);
    await page.locator('#new-file-name').fill('04_outbounds.bak.json');
    await page.getByRole('button', { name: 'Создать', exact: true }).click();

    const dialog = page.getByText('Имя из стоп-списка XKeen');
    await expect(dialog).toBeVisible();
    await expect(
      page.getByText('XKeen отменит запуск Xray', { exact: false }).last()
    ).toBeVisible();

    await page.getByRole('button', { name: 'Отмена' }).last().click();
    await expect(dialog).toBeHidden();
    expect(rec.create).toHaveLength(0);
    // Модалка создания осталась открытой с введённым именем
    await expect(page.locator('#new-file-name')).toHaveValue('04_outbounds.bak.json');

    await page.getByRole('button', { name: 'Создать', exact: true }).click();
    await page.getByRole('button', { name: 'Всё равно создать', exact: true }).click();

    await expect.poll(() => rec.create.length).toBe(1);
    expect(rec.create[0].searchParams.get('confirm_stoplist')).toBe('1');
    expect(rec.create[0].searchParams.get('path')).toBe(`${XRAY_DIR}/04_outbounds.bak.json`);
  });

  test('обычное имя создаётся без диалога и без флага', async ({ page }) => {
    await openCreateModal(page);
    await page.locator('#new-file-name').fill('routing.json');
    await page.getByRole('button', { name: 'Создать', exact: true }).click();

    await expect.poll(() => rec.create.length).toBe(1);
    expect(rec.create[0].searchParams.get('confirm_stoplist')).toBeNull();
    await expect(page.getByText('Имя из стоп-списка XKeen')).toHaveCount(0);
  });

  test('409 xkeen_stoplist_name от сервера: диалог с текстом сервера и повтор с флагом', async ({
    page
  }) => {
    await openCreateModal(page);
    await page.locator('#new-file-name').fill('weird-1.json');
    await page.getByRole('button', { name: 'Создать', exact: true }).click();

    await expect(page.getByText('Имя из стоп-списка XKeen')).toBeVisible();
    await expect(page.getByText('Серверный текст: имя weird будет отвергнуто XKeen')).toBeVisible();
    await page.getByRole('button', { name: 'Всё равно создать', exact: true }).click();

    await expect.poll(() => rec.create.length).toBe(2);
    expect(rec.create[0].searchParams.get('confirm_stoplist')).toBeNull();
    expect(rec.create[1].searchParams.get('confirm_stoplist')).toBe('1');
  });

  test('пустое имя не отправляет запрос', async ({ page }) => {
    await openCreateModal(page);
    await page.getByRole('button', { name: 'Создать', exact: true }).click();
    await expect(page.getByText('Имя из стоп-списка XKeen')).toHaveCount(0);
    expect(rec.create).toHaveLength(0);
    await expect(page.locator('#new-file-hint')).toHaveCount(0);
  });

  test('подсказка под полем имени: видна для стоп-слова, нет для обычного имени', async ({
    page
  }) => {
    await openCreateModal(page);
    const hint = page.locator('#new-file-hint');

    await page.locator('#new-file-name').fill('04_outbounds.bak.json');
    await expect(hint).toBeVisible();
    await expect(hint).toContainText('XKeen отменит запуск Xray');
    await expect(hint).toContainText('«bak»');
    await expect(page.locator('#new-file-name')).toHaveAttribute(
      'aria-describedby',
      'new-file-hint'
    );

    await page.locator('#new-file-name').fill('routing.json');
    await expect(hint).toHaveCount(0);
  });

  test('каталог Mihomo: подсказки и диалога нет даже для x.bak.json', async ({ page }) => {
    await page.locator('.file-row:has-text("config.yaml")').click();
    await openCreateModal(page);
    await page.locator('#new-file-name').fill('x.bak.json');
    await expect(page.locator('#new-file-hint')).toHaveCount(0);

    await page.getByRole('button', { name: 'Создать', exact: true }).click();
    await expect.poll(() => rec.create.length).toBe(1);
    expect(rec.create[0].searchParams.get('confirm_stoplist')).toBeNull();
    expect(rec.create[0].searchParams.get('path')).toBe(`${MIHOMO_DIR}/x.bak.json`);
    await expect(page.getByText('Имя из стоп-списка XKeen')).toHaveCount(0);
  });

  test('переименование в стоп-имя: подсказка, диалог, запрос только после подтверждения', async ({
    page
  }) => {
    await page.locator('.file-row:has-text("04_outbounds.json")').click({ button: 'right' });
    await page.getByRole('menuitem', { name: 'Переименовать файл' }).click();

    const input = page.locator('#rename-target');
    await expect(input).toBeVisible();
    await input.fill('04_outbounds.old.json');
    const hint = page.locator('#rename-file-hint');
    await expect(hint).toBeVisible();
    await expect(hint).toContainText('«old»');

    await page.getByRole('button', { name: 'Переименовать', exact: true }).click();
    await expect(page.getByText('Имя из стоп-списка XKeen')).toBeVisible();
    expect(rec.rename).toHaveLength(0);

    await page.getByRole('button', { name: 'Отмена' }).last().click();
    expect(rec.rename).toHaveLength(0);

    await page.getByRole('button', { name: 'Переименовать', exact: true }).click();
    await page.getByRole('button', { name: 'Всё равно переименовать', exact: true }).click();

    await expect.poll(() => rec.rename.length).toBe(1);
    expect(rec.rename[0].searchParams.get('confirm_stoplist')).toBe('1');
    expect(rec.rename[0].searchParams.get('new')).toBe(`${XRAY_DIR}/04_outbounds.old.json`);
  });

  test('«Создать копию» называет файл <имя>-2.json без стоп-слов и без флага', async ({ page }) => {
    await page.locator('.file-row:has-text("04_outbounds.json")').click({ button: 'right' });
    await page.getByRole('menuitem', { name: 'Создать копию' }).click();

    await expect.poll(() => rec.save.length).toBe(1);
    expect(rec.save[0].searchParams.get('path')).toBe(`${XRAY_DIR}/04_outbounds-2.json`);
    expect(rec.save[0].searchParams.get('path')).not.toContain('_copy');
    expect(rec.save[0].searchParams.get('confirm_stoplist')).toBeNull();
    await expect(page.getByText('Имя из стоп-списка XKeen')).toHaveCount(0);
  });

  test('копия файла, уже совпадающего со стоп-списком, требует подтверждения', async ({ page }) => {
    await page.locator('.file-row:has-text("old_config.json")').click({ button: 'right' });
    await page.getByRole('menuitem', { name: 'Создать копию' }).click();

    await expect(page.getByText('Имя из стоп-списка XKeen')).toBeVisible();
    expect(rec.save).toHaveLength(0);

    await page.getByRole('button', { name: 'Всё равно создать копию', exact: true }).click();
    await expect.poll(() => rec.save.length).toBe(1);
    expect(rec.save[0].searchParams.get('confirm_stoplist')).toBe('1');
    expect(rec.save[0].searchParams.get('path')).toBe(`${XRAY_DIR}/old_config-2.json`);
  });
  test('значок в дереве: у old_config.json есть, у 04_outbounds.json и Mihomo нет', async ({
    page
  }) => {
    const stopRow = page.locator('.file-row:has-text("old_config.json")');
    const mark = stopRow.locator('.stoplist-mark');
    await expect(mark).toBeVisible();
    await expect(mark).toHaveAttribute('title', /«old»/);
    await expect(mark).toHaveAttribute('title', /отменит запуск Xray/);
    await expect(mark).toHaveAttribute('aria-label', /«old»/);

    await expect(
      page.locator('.file-row:has-text("04_outbounds.json") .stoplist-mark')
    ).toHaveCount(0);
    await expect(page.locator('.file-row:has-text("config.yaml") .stoplist-mark')).toHaveCount(0);

    // Строка остаётся кликабельной, контекстное меню на месте
    await stopRow.click({ button: 'right' });
    await expect(page.getByRole('menuitem', { name: 'Переименовать файл' })).toBeVisible();
    await expect(page.getByRole('menuitem', { name: 'Удалить' })).toBeVisible();
  });
});
