// e2e-pages: config
import { test, expect, type Page } from '@playwright/test';
import AxeBuilder from '@axe-core/playwright';
import { visitPage } from './helpers/api-mocks';
import { mockConfigLayer, defaultSnapshot } from './helpers/config-layer-mocks';
import { LAZY_LOAD_TIMEOUT } from './helpers/timeouts';

test.use({ locale: 'ru-RU' });

const okFile = {
  key: 'xray/xcp-routing.json',
  kernel: 'xray',
  path: 'xcp-routing.json',
  owner: 'panel',
  state: 'ok'
};

test.describe('Файлы панели: дрейф вживую (tracer)', () => {
  test('событие files с дрейфом → строка и предупреждение → diff → «Принять правку» → «Отпущен»', async ({
    page
  }) => {
    const mock = await mockConfigLayer(page, { snapshot: { files: [okFile], drift_count: 0 } });
    await visitPage(page, '/#/config');

    const files = page.getByTestId('config-files');
    await expect(files).toContainText('xcp-routing.json', { timeout: LAZY_LOAD_TIMEOUT });
    await expect(files).toContainText('Совпадает');
    await expect(page.getByTestId('config-drift')).toHaveCount(0);

    // Файл изменили снаружи: сервер шлёт files с дрейфом
    await mock.waitConnected();
    const drifted = { ...okFile, state: 'drift_modified' };
    mock.snapshot.files = [drifted];
    mock.snapshot.drift_count = 1;
    await mock.emit('files', { files: [drifted], drift_count: 1 });

    const alert = page.getByTestId('config-drift');
    await expect(alert).toBeVisible();
    await expect(alert).toHaveAttribute('role', 'alert');
    await expect(alert).toContainText('Файлы изменены вне панели');
    await expect(files).toContainText('Изменён вручную');

    // Показать отличия → GET /files/diff, строки «−» и «+»
    const toggle = files.getByRole('button', { name: 'Показать отличия' });
    await expect(toggle).toHaveAttribute('aria-expanded', 'false');
    await toggle.click();
    await expect(files.getByRole('button', { name: 'Скрыть отличия' })).toHaveAttribute(
      'aria-expanded',
      'true'
    );
    await expect(files.locator('.diff-line-removed').first()).toContainText('"a": 2');
    await expect(files.locator('.diff-line-added').first()).toContainText('"a": 1');
    expect(mock.callsTo('GET', '/files/diff')).toHaveLength(1);

    // Принять правку → подтверждение → POST /files/release
    await files.getByRole('button', { name: 'Принять правку' }).click();
    const dialog = page.getByRole('dialog');
    await expect(dialog).toContainText('Принять правку?');
    await dialog.getByRole('button', { name: 'Принять', exact: true }).click();

    await expect(files).toContainText('Отпущен');
    await expect(files).toContainText('Вручную');
    await expect(page.getByTestId('config-drift')).toHaveCount(0);
    await expect(
      files.getByRole('button', { name: 'Вернуть под управление панели' })
    ).toBeVisible();
    await expect(files.getByRole('button', { name: 'Принять правку' })).toHaveCount(0);
    await expect(files.getByRole('button', { name: 'Пересобрать', exact: true })).toHaveCount(0);
    expect(mock.callsTo('POST', '/files/release')[0].body).toEqual({ key: okFile.key });
  });
});

// --- Общие данные и помощники ---

type F = Record<string, unknown>;
const file = (over: F): F => ({ kernel: 'xray', owner: 'panel', state: 'ok', ...over });

const SEVEN: F[] = [
  file({
    key: 'mihomo/xcp-released.yaml',
    kernel: 'mihomo',
    path: 'xcp-released.yaml',
    owner: 'manual',
    state: 'released'
  }),
  file({ key: 'xray/xcp-ok.json', path: 'xcp-ok.json' }),
  file({ key: 'xray/xcp-pending.json', path: 'xcp-pending.json', state: 'pending' }),
  file({ key: 'xray/xcp-modified.json', path: 'xcp-modified.json', state: 'drift_modified' }),
  file({ key: 'xray/xcp-missing.json', path: 'xcp-missing.json', state: 'drift_missing' }),
  file({
    key: 'xray/xcp-renamed.json',
    path: 'xcp-renamed.json',
    state: 'drift_renamed',
    obsolete_name: 'xcp-renamed.json.obsolete'
  }),
  file({
    key: 'mihomo/xcp-empty.yaml',
    kernel: 'mihomo',
    path: 'xcp-empty.yaml',
    state: 'drift_empty'
  })
];

const rows = (page: Page) => page.getByTestId('config-files').locator('li.file-row');
const row = (page: Page, path: string) => rows(page).filter({ hasText: path });

async function openConfig(page: Page, snapshot: Parameters<typeof mockConfigLayer>[1] = {}) {
  const mock = await mockConfigLayer(page, snapshot);
  await visitPage(page, '/#/config');
  await expect(page.getByTestId('config-draftbar')).toBeVisible({ timeout: LAZY_LOAD_TIMEOUT });
  return mock;
}

const driftOf = (files: F[]) => files.filter((f) => String(f.state).startsWith('drift_')).length;

test.describe('Файлы панели: список и состояния', () => {
  test('сортировка drift → ожидает → совпадает → отпущен, внутри по алфавиту', async ({ page }) => {
    await openConfig(page, { snapshot: { files: SEVEN, drift_count: driftOf(SEVEN) } });
    await expect(rows(page)).toHaveCount(7);
    const order = await rows(page).evaluateAll((els) =>
      els.map((e) => e.getAttribute('data-state'))
    );
    expect(order).toEqual([
      'drift_empty',
      'drift_missing',
      'drift_modified',
      'drift_renamed',
      'pending',
      'ok',
      'released'
    ]);
    const paths = await rows(page).locator('.file-path').allTextContents();
    expect(paths.slice(0, 4)).toEqual([
      'xcp-empty.yaml',
      'xcp-missing.json',
      'xcp-modified.json',
      'xcp-renamed.json'
    ]);
  });

  test('семь подписей состояний, владельцы, тег ядра и пояснение переименования', async ({
    page
  }) => {
    await openConfig(page, { snapshot: { files: SEVEN, drift_count: driftOf(SEVEN) } });
    const expected: [string, string, string, string][] = [
      ['xcp-ok.json', 'Совпадает', 'Панель', 'xray'],
      ['xcp-pending.json', 'Ожидает применения', 'Панель', 'xray'],
      ['xcp-modified.json', 'Изменён вручную', 'Панель', 'xray'],
      ['xcp-missing.json', 'Файл пропал', 'Панель', 'xray'],
      ['xcp-renamed.json', 'Переименован снаружи', 'Панель', 'xray'],
      ['xcp-empty.yaml', 'Пустой файл', 'Панель', 'mihomo'],
      ['xcp-released.yaml', 'Отпущен', 'Вручную', 'mihomo']
    ];
    for (const [path, state, owner, kernel] of expected) {
      const r = row(page, path);
      await expect(r.locator('.status-badge')).toHaveText(state);
      await expect(r.locator('.file-owner')).toHaveText(owner);
      await expect(r.locator('.badge')).toHaveText(kernel);
    }
    await expect(row(page, 'xcp-renamed.json').locator('.file-detail')).toHaveText(
      'Найден xcp-renamed.json.obsolete вместо xcp-renamed.json'
    );
    // Полное имя доступно в title
    await expect(row(page, 'xcp-ok.json').locator('.file-path')).toHaveAttribute(
      'title',
      'xcp-ok.json'
    );
  });

  test('смешанные состояния: полоса дрейфа слева, у отпущенного одно действие и нет diff', async ({
    page
  }) => {
    await openConfig(page, { snapshot: { files: SEVEN, drift_count: driftOf(SEVEN) } });
    const drift = row(page, 'xcp-modified.json');
    const barWidth = await drift.evaluate((e) => getComputedStyle(e).borderLeftWidth);
    expect(barWidth).toBe('3px');
    const okWidth = await row(page, 'xcp-ok.json').evaluate(
      (e) => getComputedStyle(e).borderLeftWidth
    );
    expect(okWidth).toBe('0px');

    const released = row(page, 'xcp-released.yaml');
    await expect(released.getByRole('button')).toHaveCount(1);
    await expect(
      released.getByRole('button', { name: 'Вернуть под управление панели' })
    ).toBeVisible();
    await expect(released.getByRole('button', { name: 'Показать отличия' })).toHaveCount(0);

    await expect(drift.getByRole('button')).toHaveCount(3);
    // У совпадающих и ожидающих действий нет
    await expect(row(page, 'xcp-ok.json').getByRole('button')).toHaveCount(0);
    await expect(row(page, 'xcp-pending.json').getByRole('button')).toHaveCount(0);
  });

  test('ноль файлов — EmptyState без кнопки, один файл — список без пагинации', async ({
    page
  }) => {
    const mock = await openConfig(page);
    const files = page.getByTestId('config-files');
    await expect(files).toContainText('Файлов панели пока нет');
    await expect(files).toContainText('Файлы xcp-* появятся здесь после первого применения');
    await expect(files.getByRole('button')).toHaveCount(0);
    await expect(rows(page)).toHaveCount(0);

    await mock.waitConnected();
    const one = file({ key: 'xray/xcp-a.json', path: 'xcp-a.json' });
    await mock.emit('files', { files: [one], drift_count: 0 });
    await expect(rows(page)).toHaveCount(1);
    await expect(files).not.toContainText('Файлов панели пока нет');
  });

  test('первая загрузка: по три Skeleton-строки в файлах и ядрах, затем данные', async ({
    page
  }) => {
    const mock = await mockConfigLayer(page, {
      snapshot: { files: SEVEN.slice(0, 2), drift_count: 0 }
    });
    let release: () => void = () => {};
    const gate = new Promise<void>((resolve) => (release = resolve));
    await page.route('**/api/configlayer/state', async (route) => {
      await gate;
      await route.fallback();
    });
    await visitPage(page, '/#/config');
    const filesLoading = page.getByTestId('config-files-loading');
    await expect(filesLoading).toBeVisible({ timeout: LAZY_LOAD_TIMEOUT });
    await expect(filesLoading.locator('.skeleton')).toHaveCount(3);
    const kernelsLoading = page.getByTestId('config-kernels-loading');
    await expect(kernelsLoading.locator('.skeleton')).toHaveCount(3);
    await expect(page.getByTestId('config-apply')).toBeDisabled();
    release();
    await expect(rows(page)).toHaveCount(2);
    await expect(filesLoading).toHaveCount(0);
    await expect(kernelsLoading).toHaveCount(0);
    expect(mock.callsTo('GET', '/state').length).toBeGreaterThanOrEqual(1);
  });

  test('предупреждение о дрейфе появляется и исчезает вживую; «Пересобрать всё» только при дрейфе', async ({
    page
  }) => {
    const mock = await openConfig(page, {
      snapshot: { files: [file({ key: 'xray/xcp-a.json', path: 'xcp-a.json' })], drift_count: 0 }
    });
    const files = page.getByTestId('config-files');
    await expect(page.getByTestId('config-drift')).toHaveCount(0);
    await expect(files.getByRole('button', { name: 'Пересобрать всё' })).toHaveCount(0);

    await mock.waitConnected();
    const drifted = file({ key: 'xray/xcp-a.json', path: 'xcp-a.json', state: 'drift_modified' });
    await mock.emit('files', { files: [drifted], drift_count: 1 });
    const alert = page.getByTestId('config-drift');
    await expect(alert).toBeVisible();
    await expect(alert).toContainText('«Применить» заблокировано, пока расхождения не решены');
    // Предупреждение не закрывается
    await expect(alert.getByRole('button')).toHaveCount(0);
    await expect(files.getByRole('button', { name: 'Пересобрать всё' })).toBeVisible();

    await mock.emit('files', {
      files: [file({ key: 'xray/xcp-a.json', path: 'xcp-a.json' })],
      drift_count: 0
    });
    await expect(page.getByTestId('config-drift')).toHaveCount(0);
    await expect(files.getByRole('button', { name: 'Пересобрать всё' })).toHaveCount(0);
  });
});

test.describe('Файлы панели: diff и действия', () => {
  test('пропавший файл: подпись «Файла нет на диске…», все строки добавленные', async ({
    page
  }) => {
    const missing = file({
      key: 'xray/xcp-missing.json',
      path: 'xcp-missing.json',
      state: 'drift_missing'
    });
    await openConfig(page, {
      snapshot: { files: [missing], drift_count: 1 },
      diffs: {
        'xray/xcp-missing.json': { expected: '{\n  "a": 1\n}\n', actual: '', missing: true }
      }
    });
    const r = row(page, 'xcp-missing.json');
    await r.getByRole('button', { name: 'Показать отличия' }).click();
    const diff = r.getByTestId('config-diff');
    await expect(diff).toContainText('Файла нет на диске. Ожидаемое содержимое:');
    await expect(diff.locator('.diff-line-added')).toHaveCount(3);
    await expect(diff.locator('.diff-line-removed')).toHaveCount(0);
  });

  test('подпись направления diff всегда сверху; повторный клик скрывает отличия', async ({
    page
  }) => {
    const modified = file({ key: 'xray/xcp-m.json', path: 'xcp-m.json', state: 'drift_modified' });
    await openConfig(page, { snapshot: { files: [modified], drift_count: 1 } });
    const r = row(page, 'xcp-m.json');
    await r.getByRole('button', { name: 'Показать отличия' }).click();
    await expect(r.getByTestId('config-diff')).toContainText(
      '«−» на диске сейчас, «+» запишет панель'
    );
    await r.getByRole('button', { name: 'Скрыть отличия' }).click();
    await expect(r.getByTestId('config-diff')).toHaveCount(0);
  });

  test('«Пересобрать» одного файла: подтверждение и POST /files/rebuild {keys}', async ({
    page
  }) => {
    const modified = file({ key: 'xray/xcp-m.json', path: 'xcp-m.json', state: 'drift_modified' });
    const mock = await openConfig(page, { snapshot: { files: [modified], drift_count: 1 } });
    await row(page, 'xcp-m.json').getByRole('button', { name: 'Пересобрать' }).click();
    const dialog = page.getByRole('dialog');
    await expect(dialog).toContainText('Пересобрать файл из панели?');
    await expect(dialog).toContainText('xcp-m.json');
    await expect(dialog).toContainText('Ваши ручные правки будут заменены содержимым из панели.');
    await expect(dialog).toContainText(
      'Прежняя версия сохранится в резервной копии. Запущенное ядро перезапустится.'
    );
    await dialog.getByRole('button', { name: 'Пересобрать', exact: true }).click();
    await expect.poll(() => mock.callsTo('POST', '/files/rebuild').length).toBe(1);
    expect(mock.callsTo('POST', '/files/rebuild')[0].body).toEqual({ keys: ['xray/xcp-m.json'] });
  });

  test('отмена подтверждения не отправляет запрос', async ({ page }) => {
    const modified = file({ key: 'xray/xcp-m.json', path: 'xcp-m.json', state: 'drift_modified' });
    const mock = await openConfig(page, { snapshot: { files: [modified], drift_count: 1 } });
    await row(page, 'xcp-m.json').getByRole('button', { name: 'Принять правку' }).click();
    await page.getByRole('dialog').getByRole('button', { name: 'Отмена' }).click();
    await expect(page.getByRole('dialog')).toHaveCount(0);
    expect(mock.callsTo('POST', '/files/release')).toHaveLength(0);
    await expect(row(page, 'xcp-m.json')).toContainText('Изменён вручную');
  });

  test('«Пересобрать всё»: подтверждение и POST /files/rebuild {all:true}', async ({ page }) => {
    const mock = await openConfig(page, {
      snapshot: { files: SEVEN, drift_count: driftOf(SEVEN) }
    });
    await page.getByTestId('config-files').getByRole('button', { name: 'Пересобрать всё' }).click();
    const dialog = page.getByRole('dialog');
    await expect(dialog).toContainText('Пересобрать все файлы с расхождениями?');
    await expect(dialog).toContainText(
      'Ручные правки во всех файлах с расхождениями будут заменены.'
    );
    await dialog.getByRole('button', { name: 'Пересобрать всё' }).click();
    await expect.poll(() => mock.callsTo('POST', '/files/rebuild').length).toBe(1);
    expect(mock.callsTo('POST', '/files/rebuild')[0].body).toEqual({ all: true });
  });

  test('«Вернуть под управление панели» отправляет тот же rebuild по ключу файла', async ({
    page
  }) => {
    const mock = await openConfig(page, {
      snapshot: { files: SEVEN, drift_count: driftOf(SEVEN) }
    });
    await row(page, 'xcp-released.yaml')
      .getByRole('button', { name: 'Вернуть под управление панели' })
      .click();
    const dialog = page.getByRole('dialog');
    await expect(dialog).toContainText('Пересобрать файл из панели?');
    await dialog.getByRole('button', { name: 'Пересобрать', exact: true }).click();
    await expect.poll(() => mock.callsTo('POST', '/files/rebuild').length).toBe(1);
    expect(mock.callsTo('POST', '/files/rebuild')[0].body).toEqual({
      keys: ['mihomo/xcp-released.yaml']
    });
  });

  test('пока идёт применение, действия над файлами недоступны', async ({ page }) => {
    await openConfig(page, {
      snapshot: {
        files: SEVEN,
        drift_count: driftOf(SEVEN),
        apply: { running: true, steps: [{ id: 'build', state: 'running' }] }
      }
    });
    const files = page.getByTestId('config-files');
    await expect(files.getByRole('button', { name: 'Пересобрать всё' })).toBeDisabled();
    for (const name of ['Пересобрать:', 'Принять правку:', 'Вернуть под управление панели:']) {
      const buttons = files.getByRole('button', { name });
      const n = await buttons.count();
      expect(n, name).toBeGreaterThan(0);
      for (let i = 0; i < n; i++) await expect(buttons.nth(i)).toBeDisabled();
    }
    // Просмотр отличий остаётся доступным: он ничего не меняет
    await expect(files.getByRole('button', { name: 'Показать отличия:' }).first()).toBeEnabled();
  });
});

test.describe('Ядра и версии', () => {
  test('статусы версий: ниже минимума, не определена, не установлено; актуальная без пометки', async ({
    page
  }) => {
    await openConfig(page, {
      snapshot: {
        kernels: [
          {
            name: 'xkeen',
            installed: false,
            version: '',
            status: 'not_installed',
            min_version: ''
          },
          {
            name: 'xray',
            installed: true,
            version: '1.8.11',
            status: 'below_min',
            min_version: '1.8.13'
          },
          {
            name: 'mihomo',
            installed: true,
            version: 'alpha',
            status: 'undetermined',
            min_version: '1.18.0'
          }
        ]
      }
    });
    const kernels = page.getByTestId('config-kernels');
    await expect(kernels).toContainText('Ядра и версии');
    const order = await kernels
      .locator('li')
      .evaluateAll((els) => els.map((e) => e.getAttribute('data-kernel')));
    expect(order).toEqual(['xkeen', 'xray', 'mihomo']);

    const xkeen = kernels.locator('[data-kernel="xkeen"]');
    await expect(xkeen).toContainText('не установлено');
    await expect(xkeen.locator('.version-tag')).toHaveCount(0);

    const xray = kernels.locator('[data-kernel="xray"]');
    await expect(xray.locator('.version-tag')).toHaveText('1.8.11');
    await expect(xray).toContainText('ниже минимума (нужна 1.8.13)');
    await expect(xray.locator('[title]')).toHaveAttribute(
      'title',
      'Часть функций панели выключена. «Применить» работает.'
    );

    const mihomo = kernels.locator('[data-kernel="mihomo"]');
    await expect(mihomo.locator('.version-tag')).toHaveText('alpha');
    await expect(mihomo).toContainText('версия не определена');
  });

  test('актуальная версия — только .version-tag, без бейджа', async ({ page }) => {
    await openConfig(page);
    const kernels = page.getByTestId('config-kernels');
    await expect(kernels.locator('li')).toHaveCount(3);
    for (const name of ['xkeen', 'xray', 'mihomo']) {
      const r = kernels.locator(`[data-kernel="${name}"]`);
      await expect(r.locator('.version-tag')).toBeVisible();
      await expect(r.locator('.status-badge')).toHaveCount(0);
    }
  });

  test('строки Xray, Mihomo и XKeen есть всегда, даже если сервер прислал пустой список', async ({
    page
  }) => {
    await openConfig(page, { snapshot: { kernels: [] } });
    const kernels = page.getByTestId('config-kernels');
    await expect(kernels.locator('li')).toHaveCount(3);
    await expect(kernels.locator('.status-badge')).toHaveText([
      'не установлено',
      'не установлено',
      'не установлено'
    ]);
  });
});

test.describe('Уведомления раздела', () => {
  test('schema_reset: закрытие уходит на сервер и не возвращается', async ({ page }) => {
    const mock = await openConfig(page, {
      snapshot: { notices: [{ id: 'schema_reset', kind: 'warning' }] }
    });
    const notice = page.getByTestId('config-notices').locator('.alert-warning');
    await expect(notice).toContainText('Состояние сброшено после обновления панели');
    await expect(notice).toHaveClass(/alert-dismissible/);
    await notice.getByRole('button', { name: 'Закрыть уведомление' }).click();
    await expect(notice).toHaveCount(0);
    expect(mock.callsTo('POST', '/notices/dismiss')[0].body).toEqual({ id: 'schema_reset' });

    // Сервер помнит закрытие: после перезагрузки уведомления нет
    await page.reload();
    await expect(page.getByTestId('config-draftbar')).toBeVisible({ timeout: LAZY_LOAD_TIMEOUT });
    await expect(page.getByTestId('config-notices').locator('.alert')).toHaveCount(0);
  });

  test('build_failed:mihomo — alert-error с причиной; recovered_from_journal — предупреждение', async ({
    page
  }) => {
    const mock = await openConfig(page, {
      snapshot: {
        notices: [
          {
            id: 'build_failed:mihomo',
            kind: 'error',
            kernel: 'mihomo',
            reason: 'нет ни одного узла'
          },
          { id: 'recovered_from_journal', kind: 'warning' }
        ]
      }
    });
    const notices = page.getByTestId('config-notices');
    const failed = notices.locator('.alert-error');
    await expect(failed).toContainText(
      'Не удалось подготовить файлы для Mihomo: нет ни одного узла. Ядро не запускалось.'
    );
    await expect(notices.locator('.alert-warning')).toContainText(
      'Прошлое применение прервалось. Файлы панели возвращены из резервной копии.'
    );
    await failed.getByRole('button', { name: 'Закрыть уведомление' }).click();
    await expect(failed).toHaveCount(0);
    expect(mock.callsTo('POST', '/notices/dismiss')[0].body).toEqual({ id: 'build_failed:mihomo' });
    await expect(notices.locator('.alert-warning')).toHaveCount(1);
  });

  test('без уведомлений секция не занимает места', async ({ page }) => {
    await openConfig(page);
    const box = await page.getByTestId('config-notices').boundingBox();
    expect(box === null || box.height === 0).toBe(true);
  });
});

// --- Узкие экраны и доступность ---

const LONG_NAME = `xcp-${'very-long-file-name-segment-'.repeat(4)}end.json`;
const bigDiff = {
  expected: Array.from({ length: 300 }, (_, i) => `  "key_${i}": "${'v'.repeat(60)}${i}",`).join(
    '\n'
  ),
  actual: Array.from({ length: 300 }, (_, i) => `  "key_${i}": "${'x'.repeat(60)}${i}",`).join('\n')
};

const pageOverflow = (page: Page) =>
  page.evaluate(
    () => document.scrollingElement!.scrollWidth - document.scrollingElement!.clientWidth
  );

test.describe('Файлы панели: узкие экраны', () => {
  for (const width of [320, 393, 768]) {
    test(`${width} px: длинное имя и длинный diff не дают горизонтальной прокрутки`, async ({
      page
    }) => {
      expect(LONG_NAME.length).toBeGreaterThanOrEqual(120);
      await page.setViewportSize({ width, height: 900 });
      const long = file({ key: 'xray/long.json', path: LONG_NAME, state: 'drift_modified' });
      await openConfig(page, {
        snapshot: {
          files: [long, file({ key: 'xray/xcp-b.json', path: 'xcp-b.json' })],
          drift_count: 1
        },
        diffs: { 'xray/long.json': bigDiff }
      });
      const r = row(page, 'very-long-file-name');
      await expect(r.locator('.file-path')).toHaveAttribute('title', LONG_NAME);
      await r.getByRole('button', { name: 'Показать отличия' }).click();
      const body = r.locator('.diff-body');
      await expect(body).toBeVisible();

      expect(await pageOverflow(page), 'страница не шире окна').toBeLessThanOrEqual(0);
      const m = await body.evaluate((e) => ({
        client: e.clientHeight,
        scroll: e.scrollHeight,
        overflowY: getComputedStyle(e).overflowY,
        maxHeight: getComputedStyle(e).maxHeight
      }));
      expect(m.maxHeight).toBe('320px');
      expect(m.client).toBeLessThanOrEqual(322);
      expect(m.scroll, 'diff прокручивается в своём блоке').toBeGreaterThan(m.client);
      expect(['auto', 'scroll']).toContain(m.overflowY);

      const card = await page.getByTestId('config-files').locator('.files-card').boundingBox();
      expect(card!.x + card!.width).toBeLessThanOrEqual(width + 0.5);
    });
  }

  test('393 px: кнопки действий не ниже 44 px и тянутся по ширине', async ({ page }) => {
    await page.setViewportSize({ width: 393, height: 900 });
    const m = file({ key: 'xray/xcp-m.json', path: 'xcp-m.json', state: 'drift_modified' });
    await openConfig(page, { snapshot: { files: [m], drift_count: 1 } });
    const buttons = row(page, 'xcp-m.json').locator('.file-actions .btn');
    await expect(buttons).toHaveCount(3);
    for (let i = 0; i < 3; i++) {
      const box = (await buttons.nth(i).boundingBox())!;
      expect(box.height).toBeGreaterThanOrEqual(43.5);
    }
    expect(await pageOverflow(page)).toBeLessThanOrEqual(0);
  });

  for (const theme of ['dark', 'light'] as const) {
    test(`axe: список, дрейф и раскрытый diff без серьёзных нарушений (${theme})`, async ({
      page
    }) => {
      await page.addInitScript((t) => localStorage.setItem('theme', t), theme);
      await openConfig(page, {
        snapshot: {
          files: SEVEN,
          drift_count: driftOf(SEVEN),
          notices: [{ id: 'schema_reset', kind: 'warning' }],
          kernels: [
            defaultSnapshot().kernels[0],
            {
              name: 'xray',
              installed: true,
              version: '1.8.11',
              status: 'below_min',
              min_version: '1.8.13'
            },
            {
              name: 'mihomo',
              installed: false,
              version: '',
              status: 'not_installed',
              min_version: ''
            }
          ]
        }
      });
      await row(page, 'xcp-modified.json')
        .getByRole('button', { name: 'Показать отличия' })
        .click();
      await expect(page.getByTestId('config-diff')).toBeVisible();
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
        `#/config (${theme})`
      ).toEqual([]);
    });
  }
});
