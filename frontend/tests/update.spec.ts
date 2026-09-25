import { test, expect, type Page } from '@playwright/test';

test.use({ locale: 'ru-RU' });

const REPO = 'https://github.com/shisui1511/xkeen-control-panel';

const RC_NOTES = `> 🧪 **Release candidate v0.29.0**

### 🚀 Новые возможности

- автоматический выпуск release candidate [\`a4878ff\`](${REPO}/commit/a4878ff9)

### 🐛 Исправления

- сжатие \`.gz\` при скачивании вместо UPX [\`82e1755\`](${REPO}/commit/82e17554)

---

## 📥 Установка и обновление

- этот пункт не показывается
`;

interface MockOptions {
  check?: unknown;
  checkStatus?: number;
  backups?: unknown[];
}

async function mockApi(page: Page, opts: MockOptions = {}) {
  await page.addInitScript(() => {
    Object.defineProperty(window.navigator, 'serviceWorker', {
      value: undefined,
      writable: false,
      configurable: true
    });
  });

  const json = (data: unknown, status = 200) => ({
    status,
    contentType: 'application/json',
    body: JSON.stringify(data)
  });

  await page.route('**/api/**', async (route) => {
    const url = route.request().url();

    if (url.includes('/api/auth/me')) {
      await route.fulfill(
        json({ authenticated: true, setup_required: false, csrf_token: 'mock-csrf-token' })
      );
    } else if (url.includes('/api/update/channel')) {
      await route.fulfill(json({ channel: 'beta' }));
    } else if (url.includes('/api/version')) {
      await route.fulfill(json({ panel_version: 'v0.28.0' }));
    } else if (url.includes('/api/update/status')) {
      await route.fulfill(json({ status: 'idle', message: '', progress: 0 }));
    } else if (url.includes('/api/update/check')) {
      if (opts.checkStatus && opts.checkStatus >= 400) {
        await route.fulfill(
          json({ success: false, error: 'GitHub API: 403 rate limit' }, opts.checkStatus)
        );
      } else {
        await route.fulfill(json({ success: true, data: opts.check ?? {} }));
      }
    } else if (url.includes('/api/update/backups')) {
      await route.fulfill(json({ success: true, data: opts.backups ?? [] }));
    } else {
      await route.fulfill(json({ success: true, data: {} }));
    }
  });
}

async function openUpdatesTab(page: Page) {
  await page.goto('/#/settings');
  const updatesTab = page
    .locator('[role="tab"]:has-text("Обновления"), .tab-btn:has-text("Обновления")')
    .first();
  await expect(updatesTab).toBeVisible({ timeout: 5000 });
  await updatesTab.click();
}

test.describe('Settings updates tab', () => {
  test('shows the available release candidate with parsed release notes', async ({ page }) => {
    await mockApi(page, {
      check: {
        current_version: '0.28.0',
        latest_version: '0.29.0-rc.2',
        has_update: true,
        channel: 'beta',
        prerelease: true,
        download_size: 7_300_000,
        published_at: '2026-09-25T19:30:00Z',
        releases: [
          {
            version: '0.29.0-rc.2',
            prerelease: true,
            published_at: '2026-09-25T19:30:00Z',
            url: `${REPO}/releases/tag/v0.29.0-rc.2`,
            body: RC_NOTES
          },
          { version: '0.28.1', prerelease: false, body: '_Список изменений недоступен._' }
        ]
      }
    });
    await openUpdatesTab(page);

    await expect(page.getByText('Доступна версия 0.29.0-rc.2')).toBeVisible();
    await expect(page.getByText('Загрузка 7.0 MB')).toBeVisible();
    await expect(page.getByText('Новых версий: 2')).toBeVisible();

    await expect(page.locator('.card-label', { hasText: 'Что нового' })).toBeVisible();
    await expect(page.locator('.section-title', { hasText: 'Новые возможности' })).toBeVisible();
    await expect(page.locator('.release-section code', { hasText: '.gz' })).toBeVisible();
    await expect(page.locator('a.commit-link', { hasText: 'a4878ff' })).toHaveAttribute(
      'href',
      `${REPO}/commit/a4878ff9`
    );
    await expect(page.getByText('этот пункт не показывается')).toHaveCount(0);
    await expect(page.getByText('Описание изменений недоступно')).toBeVisible();

    await page.getByRole('button', { name: 'Установить' }).click();
    await expect(page.getByText(/Панель скачает версию 0\.29\.0-rc\.2 \(7\.0 MB\)/)).toBeVisible();
  });

  test('says the panel is up to date and lists backups for rollback', async ({ page }) => {
    await mockApi(page, {
      check: {
        current_version: '0.28.0',
        latest_version: '0.28.0',
        has_update: false,
        channel: 'beta'
      },
      backups: [
        {
          name: 'xcp.bak.1790000000.0.27.3',
          version: '0.27.3',
          created_at: 1790000000,
          size: 19_000_000
        },
        { name: 'xcp.bak.1780000000', created_at: 1780000000, size: 19_000_000 }
      ]
    });
    await openUpdatesTab(page);

    await expect(page.getByText('Установлена последняя версия канала')).toBeVisible();
    await expect(page.getByText(/Проверено в/)).toBeVisible();
    await expect(page.locator('.card-label', { hasText: 'Что нового' })).toHaveCount(0);

    await expect(page.getByText('v0.27.3')).toBeVisible();
    await expect(page.getByText('версия неизвестна')).toBeVisible();

    await page.getByRole('button', { name: 'Откатить' }).first().click();
    await expect(page.getByText('Панель перезапустится с версией 0.27.3')).toBeVisible();
  });

  test('shows the check error instead of a silent failure', async ({ page }) => {
    await mockApi(page, { checkStatus: 500 });
    await openUpdatesTab(page);

    await expect(page.getByText('GitHub API: 403 rate limit')).toBeVisible();
    await expect(page.getByText('Резервных копий пока нет')).toBeVisible();
  });

  test('no templates card on the updates tab', async ({ page }) => {
    await mockApi(page, {
      check: { current_version: '0.28.0', latest_version: '0.28.0', has_update: false }
    });
    await openUpdatesTab(page);
    await expect(page.locator('.card-label', { hasText: 'Шаблоны' })).toHaveCount(0);
  });
});
