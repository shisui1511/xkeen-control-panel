import { test, expect } from '@playwright/test';
import { setupMocks, visitPage } from './helpers/api-mocks';

const ENTRIES = [
  {
    time: '2026/09/24 04:00:04',
    source_ip: '172.16.0.136',
    device: 'LEGION',
    status: 'accepted',
    network: 'tcp',
    destination: 'github.com',
    port: '443',
    inbound: 'tproxy',
    outbound: 'direct'
  },
  {
    time: '2026/09/24 04:00:03',
    source_ip: '172.16.0.63',
    status: 'accepted',
    network: 'tcp',
    destination: 'ya.ru',
    port: '443',
    inbound: 'tproxy',
    outbound: 'direct'
  },
  {
    time: '2026/09/24 04:00:02',
    source_ip: '172.16.0.136',
    device: 'LEGION',
    status: 'accepted',
    network: 'tcp',
    destination: 'youtube.com',
    port: '443',
    inbound: 'tproxy',
    outbound: 'proxy'
  }
];

test.describe('Logs: Xray by device', () => {
  test('enables the access log and filters by device', async ({ page }) => {
    await setupMocks(page, 'xray');
    let enabled = false;
    const requests: string[] = [];

    await page.route('**/api/xray/access-log/toggle', async (route) => {
      enabled = route.request().postDataJSON().enabled;
      return route.fulfill({
        json: {
          success: true,
          data: {
            status: { access_path: '/opt/var/log/xray/access.log', enabled, file_size: 0 },
            restart_required: false
          }
        }
      });
    });
    await page.route('**/api/xray/access-log?**', async (route) => {
      const url = new URL(route.request().url());
      requests.push(url.search);
      const ip = url.searchParams.get('ip');
      const entries = enabled ? ENTRIES.filter((e) => !ip || e.source_ip === ip) : [];
      return route.fulfill({
        json: {
          success: true,
          data: {
            status: { access_path: '/opt/var/log/xray/access.log', enabled, file_size: 4096 },
            report: {
              parsed: 3,
              entries,
              devices: enabled
                ? [
                    {
                      ip: '172.16.0.136',
                      device: 'LEGION',
                      connections: 2,
                      rejected: 0,
                      last_seen: '',
                      top_destinations: ['youtube.com', 'github.com']
                    },
                    {
                      ip: '172.16.0.63',
                      connections: 1,
                      rejected: 0,
                      last_seen: '',
                      top_destinations: ['ya.ru']
                    }
                  ]
                : []
            }
          }
        }
      });
    });

    await visitPage(page, '/#/logs');
    await page.getByRole('button', { name: /Xray по устройствам|Xray by device/ }).click();

    await page.getByRole('button', { name: /Включить access-лог|Turn on access log/ }).click();
    await page.locator('.confirm-actions').getByRole('button').last().click();

    const legion = page.getByRole('button', { name: /LEGION/ });
    await expect(legion).toBeVisible();
    await expect(page.locator('.xdev-table tbody tr')).toHaveCount(3);

    await legion.click();
    await expect(legion).toHaveAttribute('aria-pressed', 'true');
    await expect(page.locator('.xdev-table tbody tr')).toHaveCount(2);
    expect(requests.some((q) => q.includes('ip=172.16.0.136'))).toBe(true);
  });
});
