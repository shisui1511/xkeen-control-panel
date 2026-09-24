import { test, expect } from '@playwright/test';
import AxeBuilder from '@axe-core/playwright';
import { setupMocks, visitPage } from './helpers/api-mocks';

const ROUTES = [
  { name: 'dashboard', path: '/#/dashboard' },
  { name: 'services', path: '/#/services' },
  { name: 'connections', path: '/#/connections' },
  { name: 'proxies', path: '/#/proxies' },
  { name: 'rules', path: '/#/rules' },
  { name: 'settings', path: '/#/settings' },
  { name: 'logs', path: '/#/logs' },
  { name: 'traffic-quotas', path: '/#/trafficquotas' }
];

const THEMES = ['dark', 'light'] as const;

test.describe('A11y automated gate (@axe-core)', () => {
  for (const theme of THEMES) {
    test.describe(`Theme: ${theme}`, () => {
      for (const route of ROUTES) {
        test(`${route.name} passes WCAG 2.1 AA scan without severe violations (${theme})`, async ({
          page
        }) => {
          await setupMocks(page, 'mihomo');
          await page.addInitScript((t) => {
            localStorage.setItem('theme', t);
          }, theme);

          await visitPage(page, route.path);
          // Страница действительно отрисовалась: опечатка в маршруте иначе
          // даёт пустую область, и скан проходит, ничего не проверив
          await expect(page.locator('.main-content .page-header h1').first()).toBeVisible({
            timeout: 15000
          });

          // Гарантируем выставление атрибута data-theme на <html>
          await page.evaluate((t) => {
            document.documentElement.setAttribute('data-theme', t);
          }, theme);

          // Ждем стабилизации рендеринга страницы
          await page.waitForLoadState('networkidle');

          const accessibilityScanResults = await new AxeBuilder({ page })
            .withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa'])
            .exclude('.cm-editor')
            .exclude('.xterm')
            .analyze();

          const severeViolations = accessibilityScanResults.violations.filter(
            (v) => v.impact === 'critical' || v.impact === 'serious'
          );

          if (severeViolations.length > 0) {
            console.log(
              `Severe a11y violations on ${route.name} (${theme}):`,
              JSON.stringify(
                severeViolations.map((v) => ({
                  id: v.id,
                  impact: v.impact,
                  description: v.description,
                  helpUrl: v.helpUrl,
                  nodes: v.nodes.map((n) => ({
                    html: n.html,
                    target: n.target,
                    failureSummary: n.failureSummary
                  }))
                })),
                null,
                2
              )
            );
          }

          expect(
            severeViolations,
            `Found ${severeViolations.length} severe a11y violation(s) on ${route.name} (${theme})`
          ).toEqual([]);
        });
      }
    });
  }
});
