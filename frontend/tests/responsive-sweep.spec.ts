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
  serviceCardOverflow: { cls: string; scrollWidth: number; clientWidth: number }[];
}

/**
 * Собирает метрики переполнения документа/`.main-content`, список
 * карточек правой колонки Dashboard, выходящих за правый край контента,
 * и самопереполнение карточек сервисов (XKeen/Mihomo/Xray) — их кнопки
 * и заголовок не должны раздувать карточку шире выделенной grid-дорожки.
 */
async function collectOverflow(page: Page): Promise<OverflowReport> {
  return page.evaluate(() => {
    const doc = document.documentElement;
    const main = document.querySelector('.main-content');
    const mainRect = main?.getBoundingClientRect();
    const clippedEls = Array.from(
      document.querySelectorAll('.dash-col-right .dash-section')
    ) as HTMLElement[];
    const serviceCards = Array.from(
      document.querySelectorAll('.services-grid .service-card')
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
      })),
      serviceCardOverflow: serviceCards.map((el) => ({
        cls: el.className,
        scrollWidth: el.scrollWidth,
        clientWidth: el.clientWidth
      }))
    };
  });
}

test.describe('Dashboard — адаптивность по ширине области контента', () => {
  // 1260/1250 покрывают полосу непосредственно над порогом схлопывания в одну
  // колонку (~940px ширины контента): здесь двухколоночная раскладка ещё
  // активна, но левая колонка (60%) уже узкая — именно в этом диапазоне
  // карточки сервисов (XKeen/Mihomo/Xray) раньше выталкивались за край.
  const widths = [1920, 1600, 1440, 1280, 1260, 1250, 1100, 1024];

  for (const width of widths) {
    test(`ширина ${width}px: нет переполнения и карточки правой колонки не обрезаны`, async ({
      page
    }) => {
      await setupMocks(page, 'mihomo');
      // Русская локаль: кириллические подписи кнопок ("Перезапустить",
      // "Остановить") заметно длиннее английских и обрезание/переполнение
      // карточек сервисов проявляется только на них — панель русскоязычная.
      await page.addInitScript(() => window.localStorage.setItem('lang', 'ru'));
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

      // Инвариант 1б: карточки сервисов (XKeen/Mihomo/Xray) не раздуваются
      // шире выделенной им grid-дорожки — заголовок и кнопки должны
      // сжиматься/обрезаться троеточием, а не выталкивать карточку за край.
      for (const card of report.serviceCardOverflow) {
        expect(
          card.scrollWidth,
          `карточка сервиса ${card.cls} переполнена по горизонтали (scrollWidth=${card.scrollWidth} > clientWidth=${card.clientWidth}) на ширине ${width}px`
        ).toBeLessThanOrEqual(card.clientWidth + TOLERANCE_PX);
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
  const sweepWidths = [1440, 1280, 1250, 1024];

  for (const route of routes) {
    test(`${route}: нет переполнения на 1440/1280/1250/1024px`, async ({ page }) => {
      await setupMocks(page, 'mihomo');
      await page.addInitScript(() => window.localStorage.setItem('lang', 'ru'));
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
        for (const card of report.serviceCardOverflow) {
          if (card.scrollWidth > card.clientWidth + TOLERANCE_PX) {
            failures.push(
              `${route}@${width}px: карточка сервиса ${card.cls} переполнена (scrollWidth=${card.scrollWidth} > clientWidth=${card.clientWidth})`
            );
          }
        }
      }

      expect(failures, failures.join('\n')).toHaveLength(0);
    });
  }
});
