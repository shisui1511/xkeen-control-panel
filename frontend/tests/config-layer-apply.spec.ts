// e2e-pages: config
import { test, expect } from '@playwright/test';
import { visitPage } from './helpers/api-mocks';
import { mockConfigLayer } from './helpers/config-layer-mocks';
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
