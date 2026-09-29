import { test, expect } from '@playwright/test';
import { setupMocks, visitPage } from './helpers/api-mocks';

// Восстановление снимка: файлы со стоп-словами XKeen в корне каталога Xray
// сервер пропускает и перечисляет в skipped_stoplist; панель показывает их
// предупреждением рядом с обычным тостом исхода применения (WR-04).

async function restoreSnapshot(page: import('@playwright/test').Page, data: unknown) {
  await setupMocks(page, 'mihomo');
  await page.route('**/api/snapshots/list', async (route) => {
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        success: true,
        data: [{ id: '20260929-120000', label: 'test', created_at: 1790000000, size_bytes: 2048 }]
      })
    });
  });
  await page.route('**/api/snapshots/20260929-120000/restore', async (route) => {
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ success: true, data })
    });
  });

  await visitPage(page, '/#/settings');
  await page
    .getByRole('tab', { name: /Резервные копии|Backups/ })
    .first()
    .click();
  await page.locator('.backup-tr button.btn-secondary').click();
  await page.locator('.confirm-actions').getByRole('button').last().click();
}

test('восстановление снимка: пропущенные стоп-список файлы показываются предупреждением', async ({
  page
}) => {
  await restoreSnapshot(page, {
    outcome: 'restarted',
    kernel: 'xray',
    active_kernel: 'xray',
    active_running: true,
    skipped_stoplist: ['04_outbounds.bak.json', 'x.old.json']
  });

  const toasts = page.locator('.toast, [role="alert"]');
  await expect(toasts.filter({ hasText: /Снимок восстановлен|Snapshot restored/ })).toHaveCount(1);
  const warning = toasts.filter({ hasText: /04_outbounds\.bak\.json, x\.old\.json/ });
  await expect(warning).toHaveCount(1);
  await expect(warning).toContainText(/стоп-слов|stop-list/i);
});

test('восстановление снимка без пропущенных файлов не показывает предупреждение', async ({
  page
}) => {
  await restoreSnapshot(page, {
    outcome: 'restarted',
    kernel: 'xray',
    active_kernel: 'xray',
    active_running: true
  });

  const toasts = page.locator('.toast, [role="alert"]');
  await expect(toasts.filter({ hasText: /Снимок восстановлен|Snapshot restored/ })).toHaveCount(1);
  await expect(toasts.filter({ hasText: /стоп-слов|stop-list/i })).toHaveCount(0);
});
