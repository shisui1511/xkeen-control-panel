import { test, expect, type Page } from '@playwright/test';
import { setupMocks, visitPage } from './helpers/api-mocks';

// Вкладка «Настройки → Безопасность»: список активных сессий (D-10), время
// жизни сессии (D-06) и смена пароля с политикой/новым CSRF (D-09/D-17/D-18).

test.use({ locale: 'ru-RU' });

interface MockSession {
  id: string;
  browser: string;
  os: string;
  ip: string;
  created_at: string;
  last_seen: string;
  current: boolean;
}

const DEFAULT_TTL = {
  idle_ttl_hours: 24,
  absolute_ttl_days: 30,
  idle_ttl_min: 1,
  idle_ttl_max: 720,
  absolute_ttl_min: 1,
  absolute_ttl_max: 365
};

function threeSessions(): MockSession[] {
  return [
    {
      id: 's-current',
      browser: 'Chrome',
      os: 'Windows 10',
      ip: '192.168.1.10',
      created_at: '2026-09-27T10:00:00Z',
      last_seen: '2026-09-28T08:00:00Z',
      current: true
    },
    {
      id: 's-2',
      browser: 'Firefox',
      os: 'macOS',
      ip: '192.168.1.11',
      created_at: '2026-09-26T09:00:00Z',
      last_seen: '2026-09-27T20:00:00Z',
      current: false
    },
    {
      id: 's-3',
      browser: 'Safari',
      os: 'iOS',
      ip: '192.168.1.12',
      created_at: '2026-09-25T09:00:00Z',
      last_seen: '2026-09-26T20:00:00Z',
      current: false
    }
  ];
}

async function mockSessions(page: Page, sessions: MockSession[]) {
  await page.route('**/api/auth/sessions', async (route) => {
    if (route.request().method() !== 'GET') return route.fallback();
    return route.fulfill({ json: { success: true, data: sessions } });
  });
}

async function mockSessionTTL(page: Page, ttl: typeof DEFAULT_TTL = DEFAULT_TTL) {
  await page.route('**/api/settings/session', async (route) => {
    if (route.request().method() === 'GET') {
      return route.fulfill({ json: { success: true, data: ttl } });
    }
    const body = route.request().postDataJSON();
    return route.fulfill({
      json: {
        success: true,
        data: {
          ...ttl,
          idle_ttl_hours: body.idle_ttl_hours,
          absolute_ttl_days: body.absolute_ttl_days
        }
      }
    });
  });
}

async function openSecurityTab(page: Page) {
  await visitPage(page, '/#/settings?tab=security');
}

test.describe('Настройки → Активные сессии', () => {
  test('три сессии: карточки, бейдж «Эта сессия» и кнопка завершения только у чужих', async ({
    page
  }) => {
    await setupMocks(page, 'mihomo');
    await mockSessions(page, threeSessions());
    await mockSessionTTL(page);
    await openSecurityTab(page);

    const rows = page.locator('[data-testid="session-row"]');
    await expect(rows).toHaveCount(3);

    const currentRow = rows.filter({ hasText: 'Эта сессия' });
    await expect(currentRow).toHaveCount(1);
    await expect(currentRow.getByRole('button', { name: 'Завершить сессию' })).toHaveCount(0);

    await expect(page.getByRole('button', { name: 'Завершить сессию' })).toHaveCount(2);
  });

  test('завершение чужой сессии через подтверждение', async ({ page }) => {
    await setupMocks(page, 'mihomo');
    let currentSessions = threeSessions();
    let terminateBody: any = null;

    await page.route('**/api/auth/sessions', async (route) => {
      if (route.request().method() !== 'GET') return route.fallback();
      return route.fulfill({ json: { success: true, data: currentSessions } });
    });
    await page.route('**/api/auth/sessions/terminate', async (route) => {
      terminateBody = route.request().postDataJSON();
      currentSessions = currentSessions.filter((s) => s.id !== terminateBody.id);
      return route.fulfill({ json: { success: true, data: { terminated: 1 } } });
    });
    await mockSessionTTL(page);
    await openSecurityTab(page);

    await page
      .locator('[data-testid="session-row"]', { hasText: 'Firefox' })
      .getByRole('button', { name: 'Завершить сессию' })
      .click();

    const dialog = page.getByRole('dialog');
    await expect(dialog).toContainText('Завершить сессию?');
    await dialog.getByRole('button', { name: 'Завершить', exact: true }).click();

    expect(terminateBody).toEqual({ id: 's-2' });
    await expect(page.getByText('Сессия завершена')).toBeVisible();
    await expect(page.locator('[data-testid="session-row"]')).toHaveCount(2);
  });

  test('«Завершить все другие» завершает остальные сессии', async ({ page }) => {
    await setupMocks(page, 'mihomo');
    let currentSessions = threeSessions();
    let terminateOthersCalled = false;

    await page.route('**/api/auth/sessions', async (route) => {
      if (route.request().method() !== 'GET') return route.fallback();
      return route.fulfill({ json: { success: true, data: currentSessions } });
    });
    await page.route('**/api/auth/sessions/terminate-others', async (route) => {
      terminateOthersCalled = true;
      currentSessions = currentSessions.filter((s) => s.current);
      return route.fulfill({ json: { success: true, data: { terminated: 2 } } });
    });
    await mockSessionTTL(page);
    await openSecurityTab(page);

    await page.getByRole('button', { name: 'Завершить все другие' }).click();
    const dialog = page.getByRole('dialog');
    await expect(dialog).toContainText('Завершить все другие сессии?');
    await dialog.getByRole('button', { name: 'Завершить все', exact: true }).click();

    expect(terminateOthersCalled).toBe(true);
    await expect(page.getByText('Все другие сессии завершены')).toBeVisible();
    await expect(page.locator('[data-testid="session-row"]')).toHaveCount(1);
  });

  test('одна сессия — «Завершить все другие» disabled', async ({ page }) => {
    await setupMocks(page, 'mihomo');
    await mockSessions(page, [threeSessions()[0]]);
    await mockSessionTTL(page);
    await openSecurityTab(page);

    await expect(page.locator('[data-testid="session-row"]')).toHaveCount(1);
    await expect(page.getByRole('button', { name: 'Завершить все другие' })).toBeDisabled();
  });

  test('ошибка загрузки списка сессий — «Повторить» грузит заново', async ({ page }) => {
    await setupMocks(page, 'mihomo');
    let callCount = 0;
    await page.route('**/api/auth/sessions', async (route) => {
      if (route.request().method() !== 'GET') return route.fallback();
      callCount += 1;
      if (callCount === 1) {
        return route.fulfill({
          status: 500,
          json: { success: false, error: 'internal error' }
        });
      }
      return route.fulfill({ json: { success: true, data: threeSessions() } });
    });
    await mockSessionTTL(page);
    await openSecurityTab(page);

    await expect(page.getByText('Не удалось загрузить список сессий')).toBeVisible();
    await page.getByRole('button', { name: 'Повторить' }).click();
    await expect(page.locator('[data-testid="session-row"]')).toHaveCount(3);
  });

  test('пустые browser/os — «Неизвестное устройство», IP и время выводятся', async ({ page }) => {
    await setupMocks(page, 'mihomo');
    await mockSessions(page, [
      {
        id: 's1',
        browser: '',
        os: '',
        ip: '10.0.0.5',
        created_at: '2026-09-27T10:00:00Z',
        last_seen: '2026-09-27T11:00:00Z',
        current: true
      }
    ]);
    await mockSessionTTL(page);
    await openSecurityTab(page);

    await expect(page.getByText('Неизвестное устройство')).toBeVisible();
    await expect(page.locator('[data-testid="session-row"]')).toContainText('10.0.0.5');
  });

  test.describe('overflow', () => {
    test.use({ viewport: { width: 393, height: 851 } });

    test('20 сессий — список скроллится, страница не переполняется по горизонтали', async ({
      page
    }) => {
      await setupMocks(page, 'mihomo');
      const many: MockSession[] = Array.from({ length: 20 }, (_, i) => ({
        id: `s-${i}`,
        browser: 'Chrome',
        os: 'Windows 10',
        ip: `192.168.1.${i}`,
        created_at: '2026-09-27T10:00:00Z',
        last_seen: '2026-09-27T10:00:00Z',
        current: i === 0
      }));
      await mockSessions(page, many);
      await mockSessionTTL(page);
      await openSecurityTab(page);

      await expect(page.locator('[data-testid="session-row"]')).toHaveCount(20);

      const list = page.locator('.sessions-list');
      const [scrollHeight, clientHeight] = await list.evaluate((el) => [
        el.scrollHeight,
        el.clientHeight
      ]);
      expect(scrollHeight).toBeGreaterThan(clientHeight);

      const overflowX = await page.evaluate(
        () => document.documentElement.scrollWidth > document.documentElement.clientWidth
      );
      expect(overflowX).toBe(false);
    });
  });
});
