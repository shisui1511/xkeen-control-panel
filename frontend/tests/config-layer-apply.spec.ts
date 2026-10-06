// e2e-pages: config
import { test, expect, type Page } from '@playwright/test';
import AxeBuilder from '@axe-core/playwright';
import { visitPage } from './helpers/api-mocks';
import {
  mockConfigLayer,
  type ConfigLayerMock,
  type ConfigLayerMockOptions
} from './helpers/config-layer-mocks';
import { LAZY_LOAD_TIMEOUT } from './helpers/timeouts';

test.use({ locale: 'ru-RU' });

const stepsDone = (over: Record<string, unknown>[] = []) => {
  const base = [
    { id: 'build', state: 'done' },
    { id: 'validate_xray', state: 'done' },
    { id: 'validate_mihomo', state: 'skipped', note_code: 'kernel_not_installed' },
    { id: 'write', state: 'done' },
    { id: 'restart', state: 'done' }
  ];
  return base.map((s) => over.find((o) => o.id === s.id) ?? s);
};

test.describe('Ход применения: шаги вживую (tracer)', () => {
  test('клик «Применить» → apply_step → шаги → apply_done → итог и перезапуск по ядрам', async ({
    page
  }) => {
    const mock = await mockConfigLayer(page, { snapshot: { draft_changes: 2 } });
    await visitPage(page, '/#/config');

    const apply = page.getByTestId('config-apply');
    await expect(apply).toBeEnabled({ timeout: LAZY_LOAD_TIMEOUT });
    await expect(page.getByTestId('config-progress')).toHaveCount(0);

    await apply.click();
    expect(mock.callsTo('POST', '/api/configlayer/apply')).toHaveLength(1);

    // Идёт сборка: карточка с пятью шагами, выполняемый шаг — спиннер
    await mock.waitConnected();
    await mock.emit('apply_step', { id: 'build', state: 'running' });
    const progress = page.getByTestId('config-progress');
    await expect(progress).toBeVisible();
    await expect(progress).toContainText('Ход применения');
    const items = progress.locator('ol li.step');
    await expect(items).toHaveCount(5);
    await expect(items.nth(0)).toContainText('Сборка');
    await expect(items.nth(1)).toContainText('Проверка Xray');
    await expect(items.nth(2)).toContainText('Проверка Mihomo');
    await expect(items.nth(3)).toContainText('Запись файлов');
    await expect(items.nth(4)).toContainText('Перезапуск');
    await expect(items.nth(0).locator('.spinner')).toBeVisible();
    await expect(items.nth(1)).toHaveAttribute('data-state', 'pending');

    await mock.emit('apply_step', { id: 'build', state: 'done' });
    await mock.emit('apply_step', { id: 'validate_xray', state: 'done' });
    await mock.emit('apply_step', {
      id: 'validate_mihomo',
      state: 'skipped',
      note_code: 'kernel_not_installed'
    });
    await mock.emit('apply_step', { id: 'write', state: 'running' });
    await expect(items.nth(2)).toContainText('Проверка Mihomo');
    await expect(items.nth(2)).toContainText('ядро не установлено');
    await expect(items.nth(3).locator('.spinner')).toBeVisible();

    // Итог: все шаги, перезапуск Xray, число записанных файлов и убранные сироты
    await mock.emit('apply_done', {
      running: false,
      steps: stepsDone(),
      restart: [{ kernel: 'xray', outcome: 'restarted' }],
      result: {
        ok: true,
        code: 'applied',
        written: 2,
        orphans_removed: ['xcp-orphan.json']
      }
    });
    await expect(items.nth(4)).toContainText('Xray перезапущен');
    await expect(progress).toContainText('Применено: записано файлов — 2');
    await expect(progress).toContainText('Убраны чужие файлы панели: xcp-orphan.json');
    await expect(progress.locator('.spinner')).toHaveCount(0);
  });
});

// --- Помощники ---

type Obj = Record<string, unknown>;

/** Страница с неприменённым черновиком; клик «Применить» и первый шаг по SSE. */
async function startApply(
  page: Page,
  opts: ConfigLayerMockOptions = {}
): Promise<{ mock: ConfigLayerMock; progress: ReturnType<Page['getByTestId']> }> {
  const mock = await mockConfigLayer(page, {
    ...opts,
    snapshot: { draft_changes: 2, ...opts.snapshot }
  });
  await visitPage(page, '/#/config');
  const apply = page.getByTestId('config-apply');
  await expect(apply).toBeEnabled({ timeout: LAZY_LOAD_TIMEOUT });
  await apply.click();
  await mock.waitConnected();
  await mock.emit('apply_step', { id: 'build', state: 'running' });
  const progress = page.getByTestId('config-progress');
  await expect(progress).toBeVisible();
  return { mock, progress };
}

const pendingSteps = (over: Obj[] = []): Obj[] =>
  ['build', 'validate_xray', 'validate_mihomo', 'write', 'restart'].map(
    (id) => over.find((o) => o.id === id) ?? { id, state: 'pending' }
  );

const failedXray = (message: string, extra: Obj = {}): Obj => ({
  running: false,
  steps: pendingSteps([
    { id: 'build', state: 'done' },
    { id: 'validate_xray', state: 'failed', message }
  ]),
  result: {
    ok: false,
    code: 'validation_failed',
    kernel: 'xray',
    message,
    hint_code: 'xray_unknown_protocol',
    written: 0,
    ...extra
  }
});

const doneWith = (restart: Obj[], result: Obj = {}): Obj => ({
  running: false,
  steps: stepsDone(),
  restart,
  result: { ok: true, code: 'applied', written: 1, ...result }
});

test.describe('Ход применения: отказ и откат', () => {
  test('проверка Xray не прошла: «Не применено», вывод ядра, подсказка, черновик цел', async ({
    page
  }) => {
    const { mock, progress } = await startApply(page);
    await mock.emit('apply_step', { id: 'build', state: 'done' });
    await mock.emit('apply_step', { id: 'validate_xray', state: 'running' });
    await mock.emit('apply_done', failedXray('unknown config id: nope'));

    const failure = progress.getByTestId('config-apply-failure');
    await expect(failure).toHaveAttribute('role', 'alert');
    await expect(failure).toContainText('Не применено');
    await expect(failure).toContainText('Xray не принял конфиг:');
    await expect(failure.locator('pre')).toHaveText('unknown config id: nope');
    await expect(failure).toContainText(
      'Что исправить: Проверьте имя протокола в узле: Xray его не знает.'
    );
    await expect(failure).toContainText('Черновик сохранён. Файлы на диске остались прежними.');
    await expect(progress.getByTestId('config-apply-success')).toHaveCount(0);

    const items = progress.locator('ol li.step');
    await expect(items.nth(1)).toHaveAttribute('data-state', 'failed');
    await expect(items.nth(1).getByRole('img', { name: 'ошибка' })).toBeVisible();
    await expect(items.nth(3)).toHaveAttribute('data-state', 'pending');
    await expect(items.nth(4)).toHaveAttribute('data-state', 'pending');
    await expect(progress.locator('.spinner')).toHaveCount(0);

    // Черновик цел: счётчик остался, «Применить» снова доступна
    await expect(page.getByTestId('config-draftbar')).toContainText('Неприменённых изменений: 2');
    await expect(page.getByTestId('config-apply')).toBeEnabled();
  });

  test('таймаут проверки — отдельное сообщение, а не «ядро не приняло конфиг»', async ({
    page
  }) => {
    const { mock, progress } = await startApply(page);
    await mock.emit(
      'apply_done',
      failedXray('', { code: 'validation_timeout', message: undefined, hint_code: undefined })
    );
    const failure = progress.getByTestId('config-apply-failure');
    await expect(failure).toContainText('Проверка Xray не уложилась во время');
    await expect(failure).not.toContainText('не принял конфиг');
    await expect(failure.locator('pre')).toHaveCount(0);
    await expect(failure).toContainText('Черновик сохранён');
  });

  test('проверка не запущена и сбой сборки с подсказками по файлам', async ({ page }) => {
    const { mock, progress } = await startApply(page);
    await mock.emit(
      'apply_done',
      failedXray('', { code: 'validation_not_run', message: undefined, hint_code: undefined })
    );
    const failure = progress.getByTestId('config-apply-failure');
    await expect(failure).toContainText('Не удалось запустить проверку Xray.');

    await mock.emit('apply_step', { id: 'build', state: 'running' });
    await mock.emit('apply_done', {
      running: false,
      steps: pendingSteps([{ id: 'build', state: 'failed' }]),
      result: {
        ok: false,
        code: 'build_failed',
        written: 0,
        issues: [
          { key: 'xray/xcp-routing.json', code: 'xray_json_syntax' },
          { key: 'mihomo/xcp-proxies.yaml', code: 'brand_new_code', detail: 'сырое описание' }
        ]
      }
    });
    await expect(failure).toContainText('Панель не смогла собрать файлы.');
    await expect(failure.locator('.issues li')).toHaveCount(2);
    await expect(failure.locator('.issues li').nth(0)).toContainText('xray/xcp-routing.json');
    await expect(failure.locator('.issues li').nth(0)).toContainText(
      'Исправьте синтаксис JSON: пропущена запятая или скобка.'
    );
    await expect(failure.locator('.issues li').nth(1)).toContainText('сырое описание');
  });

  test('откат после сбоя перезапуска: заметки фолбэка вживую и строка отката', async ({ page }) => {
    const { mock, progress } = await startApply(page);
    const note = progress.locator('li.step[data-step="restart"] .step-note');

    await mock.emit('apply_step', { id: 'restart', state: 'running' });
    await expect(note).toHaveCount(0);
    await mock.emit('apply_step', {
      id: 'restart',
      state: 'running',
      note_code: 'hot_reload_failed_restarting'
    });
    await expect(note).toHaveText('Горячая перезагрузка не прошла — перезапускаем Mihomo');
    await mock.emit('apply_step', {
      id: 'restart',
      state: 'running',
      note_code: 'restart_failed_rolling_back'
    });
    await expect(note).toHaveText('Ядро не поднялось — возвращаем прежние файлы');

    await mock.emit('apply_done', {
      running: false,
      steps: stepsDone([
        { id: 'restart', state: 'failed', note_code: 'restart_failed_rolling_back' }
      ]),
      restart: [
        {
          kernel: 'mihomo',
          outcome: 'failed_rolled_back',
          note_code: 'restart_failed_rolling_back'
        }
      ],
      result: {
        ok: false,
        code: 'restart_failed',
        kernel: 'mihomo',
        message: 'ядро mihomo не поднялось после применения: timeout',
        written: 1,
        rolled_back: true
      }
    });
    const failure = progress.getByTestId('config-apply-failure');
    await expect(failure).toContainText(
      'Не удалось применить. Файлы панели возвращены из резервной копии, ядро работает на прежнем конфиге.'
    );
    await expect(failure).not.toContainText('Файлы на диске остались прежними');
    await expect(failure.locator('pre')).toContainText('не поднялось после применения');
    await expect(progress.locator('.restart-row')).toHaveText(
      'Mihomo не поднялся — прежние файлы возвращены'
    );
    // Живые заметки фолбэка после завершения не остаются
    await expect(note).toHaveCount(0);
  });

  test('неудавшийся откат не выдаётся за возврат файлов', async ({ page }) => {
    const { mock, progress } = await startApply(page);
    await mock.emit('apply_done', {
      running: false,
      steps: stepsDone([
        { id: 'restart', state: 'failed', note_code: 'restart_failed_rolling_back' }
      ]),
      restart: [{ kernel: 'xray', outcome: 'failed_rollback_failed' }],
      result: {
        ok: false,
        code: 'rollback_failed',
        kernel: 'xray',
        message: 'ядро xray не поднялось после применения: timeout; откат файлов не удался: диск',
        written: 0
      }
    });
    const failure = progress.getByTestId('config-apply-failure');
    await expect(failure).toContainText('вернуть прежние файлы тоже не удалось');
    await expect(failure).not.toContainText('ядро работает на прежнем конфиге');
    await expect(failure).not.toContainText('Файлы на диске остались прежними');
    await expect(progress.locator('.restart-row')).toHaveText(
      'Xray не поднялся — прежние файлы вернуть не удалось'
    );
  });

  test('файлы возвращены, но ядро не поднялось: об этом сказано прямо', async ({ page }) => {
    const { mock, progress } = await startApply(page);
    await mock.emit('apply_done', {
      running: false,
      steps: stepsDone([
        { id: 'restart', state: 'failed', note_code: 'restart_failed_rolling_back' }
      ]),
      restart: [{ kernel: 'mihomo', outcome: 'failed_kernel_down' }],
      result: {
        ok: false,
        code: 'kernel_not_recovered',
        kernel: 'mihomo',
        message: 'ядро mihomo не поднялось после применения: timeout',
        written: 0,
        rolled_back: true
      }
    });
    const failure = progress.getByTestId('config-apply-failure');
    await expect(failure).toContainText('Mihomo на них не запустился');
    await expect(failure).not.toContainText('ядро работает на прежнем конфиге');
    await expect(progress.locator('.restart-row')).toHaveText(
      'Mihomo не поднялся — файлы возвращены, но ядро не запущено'
    );
  });

  test('запрет применения из-за расхождений (drift_blocked) объяснён', async ({ page }) => {
    const { mock, progress } = await startApply(page);
    await mock.emit('apply_done', {
      running: false,
      steps: pendingSteps(),
      result: { ok: false, code: 'drift_blocked', written: 0 }
    });
    await expect(progress.getByTestId('config-apply-failure')).toContainText(
      'Применение заблокировано: есть расхождения файлов.'
    );
  });
});

test.describe('Ход применения: исходы перезапуска', () => {
  test('остановленное, перечитавшее, не активное ядро и перезапуск Mihomo', async ({ page }) => {
    const { mock, progress } = await startApply(page);
    const rows = progress.locator('.restart-row');

    await mock.emit(
      'apply_done',
      doneWith([{ kernel: 'xray', outcome: 'deferred', note_code: 'kernel_stopped' }])
    );
    await expect(rows).toHaveText('Ядро Xray остановлено — вступит в силу при запуске');

    await mock.emit('apply_step', { id: 'build', state: 'running' });
    await mock.emit('apply_done', doneWith([{ kernel: 'mihomo', outcome: 'hot_reloaded' }]));
    await expect(rows).toHaveText('Mihomo перечитал конфиг без перезапуска');

    await mock.emit('apply_step', { id: 'build', state: 'running' });
    await mock.emit(
      'apply_done',
      doneWith([{ kernel: 'mihomo', outcome: 'untouched_inactive', note_code: 'kernel_inactive' }])
    );
    await expect(rows).toHaveText('Ядро Mihomo не активно — не тронуто');

    await mock.emit('apply_step', { id: 'build', state: 'running' });
    await mock.emit(
      'apply_done',
      doneWith([{ kernel: 'mihomo', outcome: 'restarted_after_reload_failed' }])
    );
    await expect(rows).toHaveText('Mihomo перезапущен');

    // Оба ядра: по строке на каждое
    await mock.emit('apply_step', { id: 'build', state: 'running' });
    await mock.emit(
      'apply_done',
      doneWith([
        { kernel: 'xray', outcome: 'restarted' },
        { kernel: 'mihomo', outcome: 'hot_reloaded' }
      ])
    );
    await expect(rows).toHaveCount(2);
    await expect(rows.nth(0)).toHaveText('Xray перезапущен');
    await expect(rows.nth(1)).toHaveText('Mihomo перечитал конфиг без перезапуска');
  });

  test('шаг перезапуска отложен: маркер info и «вступит в силу при запуске»', async ({ page }) => {
    const { mock, progress } = await startApply(page);
    await mock.emit('apply_done', {
      running: false,
      steps: stepsDone([{ id: 'restart', state: 'deferred', note_code: 'kernel_stopped' }]),
      restart: [{ kernel: 'xray', outcome: 'deferred', note_code: 'kernel_stopped' }],
      result: { ok: true, code: 'applied', written: 1 }
    });
    const step = progress.locator('li.step[data-step="restart"]');
    await expect(step).toHaveAttribute('data-state', 'deferred');
    await expect(step.locator('.step-note')).toHaveText('вступит в силу при запуске');
    await expect(step.getByRole('img', { name: 'отложено' })).toBeVisible();
    await expect(progress.getByTestId('config-apply-success')).toContainText(
      'Применено: записано файлов — 1'
    );
  });
});

test.describe('Ход применения: другая вкладка и «Скрыть»', () => {
  test('применение идёт в другой вкладке: тот же ход только для чтения с подписью', async ({
    page
  }) => {
    const mock = await mockConfigLayer(page, {
      snapshot: {
        draft_changes: 2,
        apply: {
          running: true,
          trigger: 'user',
          steps: [
            { id: 'build', state: 'done' },
            { id: 'validate_xray', state: 'running' }
          ]
        }
      }
    });
    await visitPage(page, '/#/config');
    const progress = page.getByTestId('config-progress');
    await expect(progress).toBeVisible({ timeout: LAZY_LOAD_TIMEOUT });
    await expect(progress).toContainText('Идёт в другой вкладке');
    await expect(progress.locator('ol li.step')).toHaveCount(5);
    await expect(progress.locator('li.step[data-step="validate_xray"] .spinner')).toBeVisible();
    await expect(progress.getByRole('button')).toHaveCount(0);
    await expect(page.getByTestId('config-apply')).toBeDisabled();
    await expect(page.getByTestId('config-draftbar')).toContainText(
      'Применение запущено в другой вкладке'
    );

    // Конец чужого применения: подпись уходит, итог остаётся до «Скрыть»
    await mock.waitConnected();
    await mock.emit('apply_done', doneWith([{ kernel: 'xray', outcome: 'restarted' }]));
    await expect(progress).not.toContainText('Идёт в другой вкладке');
    await expect(progress).toContainText('Применено: записано файлов — 1');
  });

  test('«Скрыть» убирает итог; следующее применение показывает ход снова', async ({ page }) => {
    const { mock, progress } = await startApply(page);
    await mock.emit('apply_done', doneWith([{ kernel: 'xray', outcome: 'restarted' }]));
    await expect(progress).toContainText('Применено');
    await progress.getByRole('button', { name: 'Скрыть' }).click();
    await expect(page.getByTestId('config-progress')).toHaveCount(0);

    await mock.emit('apply_step', { id: 'build', state: 'running' });
    await expect(page.getByTestId('config-progress')).toBeVisible();
    await expect(page.getByTestId('config-progress').locator('.spinner')).toHaveCount(1);
    await expect(page.getByTestId('config-apply-success')).toHaveCount(0);
  });

  test('после загрузки страницы завершённое применение не показывается', async ({ page }) => {
    await mockConfigLayer(page, {
      snapshot: {
        draft_changes: 0,
        apply: {
          running: false,
          steps: stepsDone(),
          restart: [{ kernel: 'xray', outcome: 'restarted' }],
          result: { ok: true, code: 'applied', written: 2 }
        }
      }
    });
    await visitPage(page, '/#/config');
    await expect(page.getByTestId('config-draftbar')).toBeVisible({ timeout: LAZY_LOAD_TIMEOUT });
    await expect(page.getByTestId('config-progress')).toHaveCount(0);
  });
});

test.describe('Диагностика (режим разработчика)', () => {
  test('карточка видна в dev_mode, четыре кнопки шлют POST /diag с действием', async ({ page }) => {
    const mock = await mockConfigLayer(page, { devMode: true });
    await visitPage(page, '/#/config');
    const diag = page.getByTestId('config-diag');
    await expect(diag).toBeVisible({ timeout: LAZY_LOAD_TIMEOUT });
    await expect(diag).toContainText('Диагностика (режим разработчика)');
    await expect(diag).toContainText('Безвредные тестовые файлы для проверки на роутере');

    const buttons: [string, string][] = [
      ['Добавить тестовые файлы', 'add'],
      ['Добавить битый файл Xray', 'add_broken_xray'],
      ['Добавить битый файл Mihomo', 'add_broken_mihomo'],
      ['Убрать тестовые файлы', 'remove']
    ];
    await expect(diag.getByRole('button')).toHaveCount(4);
    for (const [name, action] of buttons) {
      const before = mock.callsTo('POST', '/api/configlayer/diag').length;
      await diag.getByRole('button', { name, exact: true }).click();
      await expect
        .poll(() => mock.callsTo('POST', '/api/configlayer/diag').length)
        .toBe(before + 1);
      const calls = mock.callsTo('POST', '/api/configlayer/diag');
      expect(calls[calls.length - 1].body).toMatchObject({ action });
      expect(typeof (calls[calls.length - 1].body as { revision: unknown }).revision).toBe(
        'number'
      );
    }
  });

  test('без dev_mode карточки нет; во время применения кнопки недоступны', async ({ page }) => {
    await mockConfigLayer(page, { devMode: false });
    await visitPage(page, '/#/config');
    await expect(page.getByTestId('config-draftbar')).toBeVisible({ timeout: LAZY_LOAD_TIMEOUT });
    await expect(page.getByTestId('config-diag')).toHaveCount(0);
  });

  test('кнопки диагностики блокируются, пока идёт применение', async ({ page }) => {
    await mockConfigLayer(page, {
      devMode: true,
      snapshot: { apply: { running: true, steps: [{ id: 'build', state: 'running' }] } }
    });
    await visitPage(page, '/#/config');
    const diag = page.getByTestId('config-diag');
    await expect(diag).toBeVisible({ timeout: LAZY_LOAD_TIMEOUT });
    for (const b of await diag.getByRole('button').all()) await expect(b).toBeDisabled();
  });
});

// --- Длинные сообщения, узкий экран, доступность ---

const LONG_ERROR = [
  ...Array.from(
    { length: 40 },
    (_, i) => `строка ${i}: ошибка конфигурации — ${'очень-длинное-значение-'.repeat(4)}`
  ),
  'X'.repeat(2500)
].join('\n');

const pageOverflow = (page: Page) =>
  page.evaluate(
    () => document.scrollingElement!.scrollWidth - document.scrollingElement!.clientWidth
  );

test.describe('Ход применения: узкие экраны', () => {
  for (const theme of ['dark', 'light'] as const) {
    test(`390 px: ошибка в 5000 символов прокручивается в своём блоке (${theme})`, async ({
      page
    }) => {
      expect(LONG_ERROR.length).toBeGreaterThanOrEqual(4500);
      await page.addInitScript((t) => localStorage.setItem('theme', t), theme);
      await page.setViewportSize({ width: 390, height: 844 });
      const { mock, progress } = await startApply(page);
      await mock.emit('apply_done', {
        ...failedXray(LONG_ERROR),
        result: {
          ...(failedXray(LONG_ERROR).result as Obj),
          issues: [{ key: `xray/${'long-name-'.repeat(10)}.json`, code: 'xray_json_syntax' }]
        }
      });
      const pre = progress.getByTestId('config-apply-failure').locator('pre');
      await expect(pre).toBeVisible();
      const m = await pre.evaluate((e) => ({
        client: e.clientHeight,
        scroll: e.scrollHeight,
        overflowY: getComputedStyle(e).overflowY,
        whiteSpace: getComputedStyle(e).whiteSpace,
        userSelect: getComputedStyle(e).userSelect,
        wide: e.scrollWidth - e.clientWidth
      }));
      expect(m.scroll, 'блок прокручивается сам').toBeGreaterThan(m.client);
      expect(m.client).toBeLessThanOrEqual(202);
      expect(m.overflowY).toBe('auto');
      expect(m.whiteSpace).toBe('pre-wrap');
      expect(m.userSelect).toBe('text');
      expect(m.wide, 'строки переносятся').toBeLessThanOrEqual(0);
      expect(await pageOverflow(page), 'страница не шире окна').toBeLessThanOrEqual(0);

      // Длинный список сирот в успехе тоже переносится
      await mock.emit('apply_step', { id: 'build', state: 'running' });
      await mock.emit(
        'apply_done',
        doneWith([{ kernel: 'xray', outcome: 'restarted' }], {
          orphans_removed: Array.from(
            { length: 12 },
            (_, i) => `xcp-orphan-${i}-${'z'.repeat(30)}.json`
          )
        })
      );
      await expect(progress.getByTestId('config-apply-success')).toContainText('xcp-orphan-11');
      expect(await pageOverflow(page), 'страница не шире окна (сироты)').toBeLessThanOrEqual(0);
    });
  }
});

test.describe('Ход применения: доступность', () => {
  for (const theme of ['dark', 'light'] as const) {
    for (const mode of ['running', 'failed'] as const) {
      test(`axe: ${mode === 'running' ? 'идущее' : 'упавшее'} применение (${theme})`, async ({
        page
      }) => {
        await page.addInitScript((t) => localStorage.setItem('theme', t), theme);
        const { mock, progress } = await startApply(page);
        await mock.emit('apply_step', { id: 'build', state: 'done' });
        await mock.emit('apply_step', { id: 'validate_xray', state: 'running' });
        await mock.emit('apply_step', {
          id: 'validate_mihomo',
          state: 'skipped',
          note_code: 'kernel_not_installed'
        });
        if (mode === 'failed') {
          await mock.emit('apply_done', failedXray('unknown config id: nope'));
          await expect(progress.getByTestId('config-apply-failure')).toBeVisible();
        }
        await expect(progress.locator('ol')).toHaveAttribute('aria-live', 'polite');
        await page.evaluate((t) => {
          document.documentElement.setAttribute('data-theme', t);
        }, theme);
        await page.waitForLoadState('networkidle');
        // Появление страницы идёт с fade: contrast считается по полупрозрачным цветам
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
          `#/config (${theme}, ${mode})`
        ).toEqual([]);
      });
    }
  }
});
