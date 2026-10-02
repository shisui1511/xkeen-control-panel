import { test, expect } from '@playwright/test';
import { setupMocks, visitPage } from './helpers/api-mocks';

const FILES = [
  {
    kind: 'port_proxying',
    path: '/opt/etc/xkeen/port_proxying.lst',
    exists: true,
    content: '#80\n#443\n',
    entries: 0,
    issues: []
  },
  {
    kind: 'port_exclude',
    path: '/opt/etc/xkeen/port_exclude.lst',
    exists: true,
    content: '',
    entries: 0,
    issues: []
  },
  {
    kind: 'ip_exclude',
    path: '/opt/etc/xkeen/ip_exclude.lst',
    exists: false,
    content: '',
    entries: 0,
    issues: []
  },
  {
    kind: 'xkeen_json',
    path: '/opt/etc/xkeen/xkeen.json',
    exists: true,
    content: '{\n}\n',
    entries: 0,
    issues: []
  }
];

test.describe('XKeen settings card', () => {
  test('loads files, validates live and saves', async ({ page }) => {
    await setupMocks(page, 'mihomo');
    const saved: { kind: string; content: string }[] = [];

    // Registered after setupMocks, so these handlers take precedence.
    await page.route('**/api/xkeen/settings**', async (route) => {
      const url = route.request().url();
      const body = route.request().postDataJSON?.() ?? {};
      if (url.endsWith('/api/xkeen/settings')) {
        await route.fulfill({ json: { success: true, data: FILES } });
      } else if (url.includes('/validate')) {
        const bad = String(body.content)
          .split('\n')
          .findIndex((l: string) => l.trim() === '99999');
        await route.fulfill({
          json: {
            success: true,
            data: {
              entries: 1,
              issues:
                bad >= 0
                  ? [{ line: bad + 1, severity: 'error', code: 'invalid_port', value: '99999' }]
                  : []
            }
          }
        });
      } else if (url.includes('/save')) {
        saved.push(body);
        await route.fulfill({
          json: {
            success: true,
            data: { ...FILES[0], content: body.content, entries: 2, issues: [] }
          }
        });
      }
    });

    await visitPage(page, '/#/services');
    const card = page.locator('.xkeen-settings-card');
    await expect(card).toBeVisible();
    await expect(card.locator('.xs-path')).toHaveText('/opt/etc/xkeen/port_proxying.lst');

    const editor = card.locator('textarea');
    await expect(editor).toHaveValue('#80\n#443\n');

    const saveBtn = card.getByRole('button', { name: /^Сохранить$|^Save$/ });
    await expect(saveBtn).toBeDisabled();

    await editor.fill('80\n99999\n');
    await expect(card.locator('.xs-issue.is-error')).toContainText('99999');
    await expect(saveBtn).toBeDisabled();

    await editor.fill('80\n443\n');
    await expect(card.locator('.xs-issue')).toHaveCount(0);
    await expect(saveBtn).toBeEnabled();
    await saveBtn.click();

    await expect.poll(() => saved.length).toBe(1);
    expect(saved[0]).toEqual({ kind: 'port_proxying', content: '80\n443\n' });
    await expect(saveBtn).toBeDisabled();

    // Switching files shows the matching path and the "will be created" badge.
    await card.getByRole('button', { name: /Исключённые IP|Excluded IPs/ }).click();
    await expect(card.locator('.xs-path')).toHaveText('/opt/etc/xkeen/ip_exclude.lst');
    await expect(card.locator('.xs-badge')).toBeVisible();
  });

  test.describe('save and restart (ru)', () => {
    test.use({ locale: 'ru-RU' });

    async function mockSettingsAndApply(
      page: import('@playwright/test').Page,
      applyBody: Record<string, unknown>,
      controlRequests: string[]
    ) {
      await setupMocks(page, 'mihomo');
      await page.route('**/api/xkeen/settings**', async (route) => {
        const url = route.request().url();
        const body = route.request().postDataJSON?.() ?? {};
        if (url.endsWith('/api/xkeen/settings')) {
          await route.fulfill({ json: { success: true, data: FILES } });
        } else if (url.includes('/validate')) {
          await route.fulfill({ json: { success: true, data: { entries: 1, issues: [] } } });
        } else if (url.includes('/save')) {
          await route.fulfill({
            json: {
              success: true,
              data: { ...FILES[0], content: body.content, entries: 2, issues: [] }
            }
          });
        }
      });
      await page.route('**/api/service/control**', async (route) => {
        const url = route.request().url();
        controlRequests.push(url);
        await route.fulfill({
          json: { success: true, data: url.includes('action=apply') ? applyBody : {} }
        });
      });
    }

    async function saveAndRestart(page: import('@playwright/test').Page) {
      await visitPage(page, '/#/services');
      const card = page.locator('.xkeen-settings-card');
      await expect(card).toBeVisible();
      await card.locator('textarea').fill('80\n443\n');
      const restartBtn = card.getByRole('button', {
        name: /Сохранить и перезапустить|Save and restart/
      });
      await expect(restartBtn).toBeEnabled();
      await restartBtn.click();
    }

    test('save and restart with stopped kernel does not start it', async ({ page }) => {
      const controlRequests: string[] = [];
      await mockSettingsAndApply(
        page,
        { outcome: 'saved_kernel_stopped', kernel: 'mihomo', active_kernel: 'mihomo' },
        controlRequests
      );
      await saveAndRestart(page);

      const toast = page.locator('.toast', { hasText: 'вступят в силу при запуске' });
      await expect(toast).toBeVisible();
      await expect(toast.locator('button.toast__action')).toHaveText('Запустить сейчас');
      expect(controlRequests.some((u) => u.includes('action=apply&kernel=active'))).toBe(true);
      expect(controlRequests.some((u) => u.includes('action=restart'))).toBe(false);
      expect(controlRequests.some((u) => u.includes('action=start'))).toBe(false);
    });

    test('save and restart is blocked while both kernels are running', async ({ page }) => {
      await mockSettingsAndApply(page, {}, []);
      await page.route('**/api/capabilities', (route) =>
        route.fulfill({
          json: {
            success: true,
            data: {
              kernels: {
                xray: { installed: true, version: '1.8.4' },
                mihomo: { installed: true, version: '1.19.31' }
              },
              active_kernel: 'both',
              kernel_conflict: true,
              running_kernels: ['xray', 'mihomo']
            }
          }
        })
      );
      await visitPage(page, '/#/services');
      const card = page.locator('.xkeen-settings-card');
      await expect(card).toBeVisible();
      await card.locator('textarea').fill('80\n443\n');
      const restartBtn = card.getByRole('button', { name: /Сохранить и перезапустить/ });
      await expect(restartBtn).toBeDisabled();
      await expect(restartBtn).toHaveAttribute('title', 'Недоступно, пока запущены оба ядра');
    });

    test('a gate refusal of the restart (409) is shown as a translated toast', async ({ page }) => {
      await mockSettingsAndApply(page, {}, []);
      await page.route('**/api/service/control**', (route) =>
        route.fulfill({
          status: 409,
          json: { success: false, error: 'kernel conflict', code: 'kernel_conflict' }
        })
      );
      await saveAndRestart(page);
      await expect(
        page.locator('.toast--error', { hasText: 'Действие заблокировано: запущены оба ядра' })
      ).toBeVisible();
    });

    test('save and restart with running kernel reports the restart', async ({ page }) => {
      const controlRequests: string[] = [];
      await mockSettingsAndApply(
        page,
        { outcome: 'restarted', kernel: 'mihomo', active_kernel: 'mihomo', active_running: true },
        controlRequests
      );
      await saveAndRestart(page);

      await expect(
        page.locator('.toast', { hasText: 'Сохранено, XKeen перезапущен' })
      ).toBeVisible();
      expect(controlRequests.some((u) => u.includes('action=apply&kernel=active'))).toBe(true);
      expect(controlRequests.some((u) => u.includes('action=restart'))).toBe(false);
    });
  });
});
