import { test, expect } from '@playwright/test';
import { setupMocks, visitPage } from './helpers/api-mocks';

// ============================================================
// Design System Sweep — Playwright-замер вычисленных стилей по
// всем четырнадцати маршрутам панели на соответствие единому
// словарю компонентов и токенам дизайн-системы (UI-01, UI-04, UI-06).
// ============================================================

export const MIGRATED_ROUTES = [
  '/#/dashboard',
  '/#/services',
  '/#/connections',
  '/#/proxies',
  '/#/rules',
  '/#/smartproxy',
  '/#/traffic',
  '/#/trafficquotas',
  '/#/logs',
  '/#/editor',
  '/#/dat',
  '/#/subscriptions',
  '/#/settings'
];

const THEMES = ['light', 'dark'] as const;

test.describe('Design System Sweep — тринадцать маршрутов панели', () => {
  for (const route of MIGRATED_ROUTES) {
    for (const theme of THEMES) {
      test(`${route} [${theme}]: ровно один h1 с вычисленным font-size 22px`, async ({ page }) => {
        await setupMocks(page, 'mihomo');
        await visitPage(page, route);
        await page.evaluate((t) => {
          document.documentElement.setAttribute('data-theme', t);
        }, theme);

        const h1Elements = page.locator('h1');
        await expect(h1Elements, `на ${route} ожидался ровно один h1`).toHaveCount(1);

        const fontSize = await h1Elements.first().evaluate((el) => getComputedStyle(el).fontSize);
        expect(fontSize, `h1 на ${route} должен иметь вычисленный font-size 22px`).toBe('22px');
      });

      test(`${route} [${theme}]: все нативные select находятся внутри .xcp-select`, async ({
        page
      }) => {
        await setupMocks(page, 'mihomo');
        await visitPage(page, route);
        await page.evaluate((t) => {
          document.documentElement.setAttribute('data-theme', t);
        }, theme);

        const bareSelectCount = await page.evaluate(() => {
          const selects = Array.from(document.querySelectorAll('select'));
          return selects.filter((el) => !el.closest('.xcp-select')).length;
        });
        expect(
          bareSelectCount,
          `на ${route} не должно быть нативных select вне обёртки .xcp-select`
        ).toBe(0);
      });

      test(`${route} [${theme}]: элементы пустого состояния без пунктирной рамки`, async ({
        page
      }) => {
        await setupMocks(page, 'mihomo');
        await visitPage(page, route);
        await page.evaluate((t) => {
          document.documentElement.setAttribute('data-theme', t);
        }, theme);

        const dashedEmptyStates = await page.evaluate(() => {
          const emptyEls = Array.from(
            document.querySelectorAll('.empty-state, [class*="empty-state"]')
          );
          return emptyEls.filter((el) => {
            const cs = getComputedStyle(el);
            return cs.borderStyle === 'dashed' || cs.borderTopStyle === 'dashed';
          }).length;
        });
        expect(
          dashedEmptyStates,
          `на ${route} пустые состояния не должны иметь пунктирной рамки`
        ).toBe(0);
      });

      test(`${route} [${theme}]: замер шкалы кеглей видимых текстовых узлов (≤6 значений из шкалы токенов)`, async ({
        page
      }) => {
        await setupMocks(page, 'mihomo');
        await visitPage(page, route);
        await page.evaluate((t) => {
          document.documentElement.setAttribute('data-theme', t);
        }, theme);

        const { fontSizes, unexpected } = await page.evaluate(() => {
          const root = document.querySelector('.main-content') || document.body;
          const walker = document.createTreeWalker(root, NodeFilter.SHOW_TEXT);
          const sizes = new Set<number>();
          const unexp: string[] = [];
          let node: Node | null;
          while ((node = walker.nextNode())) {
            if (!node.textContent || !node.textContent.trim()) continue;
            const parent = node.parentElement;
            if (!parent) continue;
            const rect = parent.getBoundingClientRect();
            if (rect.width === 0 || rect.height === 0) continue;
            const cs = window.getComputedStyle(parent);
            if (cs.display === 'none' || cs.visibility === 'hidden' || cs.opacity === '0') continue;
            if (parent.closest('.cm-editor, .xterm')) continue;
            const fs = Math.round(parseFloat(cs.fontSize));
            if (!isNaN(fs)) {
              sizes.add(fs);
              if (![12, 13, 14, 16, 18, 22].includes(fs)) {
                unexp.push(
                  `${parent.tagName}.${parent.className}: ${cs.fontSize} ("${node.textContent.trim().slice(0, 25)}")`
                );
              }
            }
          }
          return {
            fontSizes: Array.from(sizes).sort((a, b) => a - b),
            unexpected: unexp
          };
        });

        expect(
          unexpected,
          `на ${route} найдены размеры шрифта вне шкалы токенов: ${unexpected.join('; ')}`
        ).toEqual([]);

        expect(
          fontSizes.length,
          `на ${route} число различных размеров шрифта (${fontSizes.join(', ')}) должно быть ≤ 6`
        ).toBeLessThanOrEqual(6);
      });

      test(`${route} [${theme}]: замер геометрии кнопок (≤3 высот, ≤2 радиусов)`, async ({
        page
      }) => {
        await setupMocks(page, 'mihomo');
        await visitPage(page, route);
        await page.evaluate((t) => {
          document.documentElement.setAttribute('data-theme', t);
        }, theme);

        const { heightCount, radiusCount, heights, radii } = await page.evaluate(() => {
          const root = document.querySelector('.main-content') || document.body;
          const buttons = Array.from(root.querySelectorAll<HTMLElement>('.btn, button.btn')).filter(
            (el) => {
              const rect = el.getBoundingClientRect();
              if (rect.width === 0 || rect.height === 0) return false;
              const cs = window.getComputedStyle(el);
              if (cs.display === 'none' || cs.visibility === 'hidden' || cs.opacity === '0')
                return false;
              if (el.closest('.cm-editor, .xterm')) return false;
              return true;
            }
          );

          const hSet = new Set(buttons.map((b) => Math.round(b.getBoundingClientRect().height)));
          const rSet = new Set(
            buttons.map((b) => {
              const cs = window.getComputedStyle(b);
              const r = Math.round(
                parseFloat(cs.borderTopLeftRadius) ||
                  parseFloat(cs.borderTopRightRadius) ||
                  parseFloat(cs.borderBottomLeftRadius) ||
                  parseFloat(cs.borderBottomRightRadius) ||
                  parseFloat(cs.borderRadius) ||
                  0
              );
              return `${r}px`;
            })
          );

          return {
            heightCount: hSet.size,
            radiusCount: rSet.size,
            heights: Array.from(hSet),
            radii: Array.from(rSet)
          };
        });

        expect(
          heightCount,
          `на ${route} число различных высот кнопок (${heights.join(', ')}) должно быть ≤ 3`
        ).toBeLessThanOrEqual(3);

        expect(
          radiusCount,
          `на ${route} число различных радиусов кнопок (${radii.join(', ')}) должно быть ≤ 2`
        ).toBeLessThanOrEqual(2);
      });
    }
  }
});
