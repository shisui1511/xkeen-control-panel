import { test, expect } from '@playwright/test';
import AxeBuilder from '@axe-core/playwright';
import { setupMocks, visitPage } from './helpers/api-mocks';

test.describe('A11y automated gate (@axe-core)', () => {
  test('Dashboard passes WCAG 2.1 AA scan without severe violations', async ({ page }) => {
    await setupMocks(page, 'mihomo');
    await page.addInitScript(() => {
      localStorage.setItem('theme', 'dark');
    });
    await visitPage(page, '/#/dashboard');

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
        'Severe a11y violations:',
        JSON.stringify(
          severeViolations.map((v) => ({
            id: v.id,
            impact: v.impact,
            description: v.description,
            nodes: v.nodes.map((n) => n.html)
          })),
          null,
          2
        )
      );
    }

    expect(severeViolations).toEqual([]);
  });
});
