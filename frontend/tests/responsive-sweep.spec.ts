import { test, expect, type Page } from '@playwright/test';
import { setupMocks, visitPage } from './helpers/api-mocks';

// ============================================================
// Responsive Sweep — проверка отсутствия горизонтального
// переполнения панели на реальной ширине области контента
// (viewport минус фиксированный сайдбар), а не ширине окна.
// ============================================================

const TOLERANCE_PX = 1;

interface OverflowReport {
  docScrollWidth: number;
  innerWidth: number;
  mainScrollWidth: number;
  mainClientWidth: number;
  mainRight: number;
  clipped: { cls: string; right: number }[];
}

/**
 * Собирает метрики переполнения документа/`.main-content` и список
 * карточек правой колонки Dashboard, выходящих за правый край контента.
 */
async function collectOverflow(page: Page): Promise<OverflowReport> {
  return page.evaluate(() => {
    const doc = document.documentElement;
    const main = document.querySelector('.main-content');
    const mainRect = main?.getBoundingClientRect();
    const clippedEls = Array.from(
      document.querySelectorAll('.dash-col-right .dash-section')
    ) as HTMLElement[];

    return {
      docScrollWidth: doc.scrollWidth,
      innerWidth: window.innerWidth,
      mainScrollWidth: main?.scrollWidth ?? 0,
      mainClientWidth: main?.clientWidth ?? 0,
      mainRight: mainRect?.right ?? 0,
      clipped: clippedEls.map((el) => ({
        cls: el.className,
        right: el.getBoundingClientRect().right
      }))
    };
  });
}

test.describe('Dashboard — адаптивность по ширине области контента', () => {
  const widths = [1920, 1440, 1280, 1100, 1024];

  for (const width of widths) {
    test(`ширина ${width}px: нет переполнения и карточки правой колонки не обрезаны`, async ({
      page
    }) => {
      await setupMocks(page, 'mihomo');
      await page.setViewportSize({ width, height: 900 });
      await visitPage(page, '/#/dashboard');
      await expect(page.locator('.dashboard-layout-grid')).toBeVisible();

      const report = await collectOverflow(page);

      // Инвариант 1: карточки правой колонки не выходят за правый край контента
      for (const item of report.clipped) {
        expect(
          item.right,
          `карточка ${item.cls} выходит за правый край контента (${item.right} > ${report.mainRight + TOLERANCE_PX}) на ширине ${width}px`
        ).toBeLessThanOrEqual(report.mainRight + TOLERANCE_PX);
      }

      // Инвариант 2: нет горизонтальной прокрутки документа
      expect(
        report.docScrollWidth,
        `горизонтальная прокрутка документа на ширине ${width}px`
      ).toBeLessThanOrEqual(report.innerWidth + TOLERANCE_PX);

      // Инвариант 3: .main-content не переполнен по горизонтали
      expect(
        report.mainScrollWidth,
        `.main-content переполнен по горизонтали на ширине ${width}px`
      ).toBeLessThanOrEqual(report.mainClientWidth + TOLERANCE_PX);

      // Инвариант 4: число колонок сетки соответствует ширине контейнера
      const gridInfo = await page.evaluate(() => {
        const grid = document.querySelector('.dashboard-layout-grid') as HTMLElement | null;
        const scope = document.querySelector('.dashboard-grid-scope') as HTMLElement | null;
        if (!grid || !scope) return null;
        const columns = getComputedStyle(grid)
          .gridTemplateColumns.trim()
          .split(/\s+/)
          .filter(Boolean).length;
        return { columns, scopeWidth: scope.clientWidth };
      });

      expect(
        gridInfo,
        `.dashboard-layout-grid / .dashboard-grid-scope не найдены на ширине ${width}px`
      ).not.toBeNull();
      if (gridInfo) {
        if (gridInfo.scopeWidth <= 940) {
          expect(
            gridInfo.columns,
            `на ширине контейнера ${gridInfo.scopeWidth}px (<=940) ожидалась одна колонка сетки`
          ).toBe(1);
        } else {
          expect(
            gridInfo.columns,
            `на ширине контейнера ${gridInfo.scopeWidth}px (>940) ожидались две колонки сетки`
          ).toBe(2);
        }
      }
    });
  }
});

test.describe('Панель — отсутствие горизонтального переполнения', () => {
  const routes = [
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
    '/#/network',
    '/#/subscriptions',
    '/#/settings'
  ];
  const sweepWidths = [1440, 1280, 1024];

  for (const route of routes) {
    test(`${route}: нет переполнения на 1440/1280/1024px`, async ({ page }) => {
      await setupMocks(page, 'mihomo');
      await visitPage(page, route);

      const failures: string[] = [];

      for (const width of sweepWidths) {
        await page.setViewportSize({ width, height: 900 });
        // короткая пауза на стабилизацию раскладки после изменения viewport
        await page.waitForTimeout(150);

        const report = await collectOverflow(page);

        if (report.docScrollWidth > report.innerWidth + TOLERANCE_PX) {
          failures.push(
            `${route}@${width}px: горизонтальная прокрутка документа (scrollWidth=${report.docScrollWidth} > innerWidth=${report.innerWidth})`
          );
        }
        if (report.mainScrollWidth > report.mainClientWidth + TOLERANCE_PX) {
          failures.push(
            `${route}@${width}px: .main-content переполнен (scrollWidth=${report.mainScrollWidth} > clientWidth=${report.mainClientWidth})`
          );
        }
      }

      expect(failures, failures.join('\n')).toHaveLength(0);
    });
  }
});
