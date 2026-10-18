// e2e-pages: editor
import { test, expect, type Page } from '@playwright/test';
import { attachConsoleGuard } from './lib/console';
import { T } from './lib/env';

// Движение шторки файлов Редактора на настоящей панели при 390 px. Выезд и уход шторки
// занимают около 200 мс, бэкдроп проявляется и гаснет по прозрачности (D-12); при
// prefers-reduced-motion шторка и бэкдроп появляются и исчезают мгновенно, без промежуточных
// кадров (D-15). Сеть не подменяется, стенд не меняется: сценарий только открывает и закрывает
// шторку. Синхронизация — выборка кадров requestAnimationFrame после нажатия, без ожиданий
// по таймеру.

const VIEWPORT = { width: 390, height: 844 };
const OPEN_BUTTON = '.editor-empty-actions .btn-secondary';
const TOGGLE_BUTTON = '.btn-sidebar-toggle';
const CLOSE_BUTTON = '.responsive-sidebar-close';

interface Frame {
  /** Миллисекунды от нажатия */
  t: number;
  /** Есть ли шторка в документе */
  present: boolean;
  /** Левая граница шторки (null, если шторки нет) */
  left: number | null;
  /** Свойство inert шторки (null, если шторки нет) */
  inert: boolean | null;
  /** Прозрачность бэкдропа (null, если бэкдропа нет) */
  backdrop: number | null;
}

/** Нажимает элемент и записывает кадры шторки и бэкдропа до завершения движения. */
async function sampleSheet(page: Page, trigger: string): Promise<Frame[]> {
  return page.evaluate(
    (sel) =>
      new Promise<Frame[]>((resolve, reject) => {
        const el = document.querySelector<HTMLElement>(sel);
        if (!el) {
          reject(new Error(`нет элемента ${sel}`));
          return;
        }
        const frames: Frame[] = [];
        const t0 = performance.now();
        el.click();
        const tick = () => {
          const t = performance.now() - t0;
          const pane = document.querySelector<HTMLElement>('.file-tree-pane.overlay');
          const bd = document.querySelector<HTMLElement>('.responsive-sidebar-backdrop');
          frames.push({
            t,
            present: pane !== null,
            left: pane ? pane.getBoundingClientRect().left : null,
            inert: pane ? pane.inert : null,
            backdrop: bd ? Number(getComputedStyle(bd).opacity) : null
          });
          if (frames.length >= 45 || t >= 800) resolve(frames);
          else requestAnimationFrame(tick);
        };
        requestAnimationFrame(tick);
      }),
    trigger
  );
}

/** Селектор кнопки, которой на этом экране открывается шторка. */
async function openTrigger(page: Page): Promise<string> {
  const emptyBtn = page.locator(OPEN_BUTTON);
  const toggleBtn = page.locator(TOGGLE_BUTTON);
  await expect(emptyBtn.or(toggleBtn).first()).toBeVisible({ timeout: T.page });
  return (await emptyBtn.first().isVisible()) ? OPEN_BUTTON : TOGGLE_BUTTON;
}

/** Отсутствие горизонтальной прокрутки при открытой шторке. */
async function expectNoHorizontalScroll(page: Page, what: string): Promise<void> {
  const m = await page.evaluate(() => ({
    scroll: document.documentElement.scrollWidth,
    inner: window.innerWidth
  }));
  expect(
    m.scroll,
    `${what}: scrollWidth ${m.scroll} при ширине окна ${m.inner}`
  ).toBeLessThanOrEqual(m.inner + 1);
}

/** Кадр выборки при смене медиазапроса (поворот планшета, сужение окна). */
interface SwitchFrame {
  /** Миллисекунды от начала выборки */
  t: number;
  /** Ширина окна в этом кадре */
  width: number;
  /** Есть ли колонка файлов (панель без класса overlay) */
  column: boolean;
  /** Есть ли шторка файлов (панель с классом overlay) */
  sheet: boolean;
  /** Сумма запущенных анимаций у самих панелей .file-tree-pane */
  paneAnims: number;
  /** Есть ли бэкдроп шторки */
  backdrop: boolean;
  /** Число запущенных анимаций бэкдропа */
  backdropAnims: number;
  /** Значение document.body.style.overflow */
  bodyOverflow: string;
}

/**
 * Меняет размер окна и пишет кадры requestAnimationFrame до 600 мс после первого кадра с новой
 * шириной (либо до предела limitMs от начала выборки).
 */
async function sampleAcrossResize(
  page: Page,
  vp: { width: number; height: number },
  limitMs: number
): Promise<SwitchFrame[]> {
  const [frames] = await Promise.all([
    page.evaluate(
      (limit) =>
        new Promise<SwitchFrame[]>((resolve) => {
          const frames: SwitchFrame[] = [];
          const startWidth = window.innerWidth;
          const t0 = performance.now();
          let changedAt: number | null = null;
          const tick = () => {
            const t = performance.now() - t0;
            const panes = Array.from(document.querySelectorAll<HTMLElement>('.file-tree-pane'));
            const bd = document.querySelector<HTMLElement>('.responsive-sidebar-backdrop');
            const width = window.innerWidth;
            frames.push({
              t,
              width,
              column: panes.some((p) => !p.classList.contains('overlay')),
              sheet: panes.some((p) => p.classList.contains('overlay')),
              paneAnims: panes.reduce((n, p) => n + p.getAnimations().length, 0),
              backdrop: bd !== null,
              backdropAnims: bd ? bd.getAnimations().length : 0,
              bodyOverflow: document.body.style.overflow
            });
            if (changedAt === null && width !== startWidth) changedAt = t;
            if ((changedAt !== null && t - changedAt >= 600) || t >= limit) resolve(frames);
            else requestAnimationFrame(tick);
          };
          requestAnimationFrame(tick);
        }),
      limitMs
    ),
    page.setViewportSize(vp)
  ]);
  return frames;
}

const MODES = [
  { name: 'default', reducedMotion: 'no-preference' },
  { name: 'reduce', reducedMotion: 'reduce' }
] as const;

for (const mode of MODES) {
  test(`editor-sheet-motion motion:${mode.name} vp:390`, async ({ page }) => {
    const guard = attachConsoleGuard(page);
    await page.setViewportSize(VIEWPORT);
    await page.emulateMedia({ reducedMotion: mode.reducedMotion });
    await page.goto('/#/editor');

    const trigger = await openTrigger(page);
    await expect(page.locator('.file-tree-pane.overlay'), 'шторка до нажатия').toHaveCount(0);

    // Открытие
    const opening = await sampleSheet(page, trigger);
    const shown = opening.filter((f) => f.present);
    expect(shown.length, 'открытие: кадров со шторкой').toBeGreaterThan(0);
    const first = shown[0];
    const lastOpen = opening[opening.length - 1];
    expect(lastOpen.present, 'открытие: шторка в последнем кадре').toBe(true);
    expect(
      Math.abs(lastOpen.left ?? NaN),
      `открытие: левая граница в последнем кадре ${lastOpen.left}`
    ).toBeLessThanOrEqual(1);
    expect(
      lastOpen.backdrop ?? 0,
      `открытие: прозрачность бэкдропа в последнем кадре ${lastOpen.backdrop}`
    ).toBeGreaterThanOrEqual(0.99);

    if (mode.name === 'default') {
      expect(
        first.left ?? 0,
        `первый кадр выезда: left ${first.left}, ожидалось ≤ −80`
      ).toBeLessThanOrEqual(-80);
      for (let i = 1; i < shown.length; i++) {
        expect(
          shown[i].left ?? 0,
          `выезд: кадр ${i} left ${shown[i].left} левее предыдущего ${shown[i - 1].left}`
        ).toBeGreaterThanOrEqual((shown[i - 1].left ?? 0) - 1);
      }
      const settled = shown.find((f) => Math.abs(f.left ?? NaN) <= 1);
      expect(settled, 'выезд: шторка не дошла до края экрана').toBeDefined();
      const took = settled!.t - first.t;
      expect(
        took,
        `выезд занял ${Math.round(took)} мс, ожидалось не меньше 120`
      ).toBeGreaterThanOrEqual(120);
      expect(
        first.backdrop ?? 1,
        `первый кадр: прозрачность бэкдропа ${first.backdrop}, ожидалось ≤ 0.5`
      ).toBeLessThanOrEqual(0.5);
    } else {
      expect(
        Math.abs(first.left ?? NaN),
        `reduced-motion: первый кадр со шторкой left ${first.left}, ожидалось 0`
      ).toBeLessThanOrEqual(1);
      expect(
        first.backdrop ?? 0,
        `reduced-motion: первый кадр, прозрачность бэкдропа ${first.backdrop}`
      ).toBeGreaterThanOrEqual(0.99);
    }

    await expectNoHorizontalScroll(page, 'шторка открыта');

    // Закрытие
    const closing = await sampleSheet(page, CLOSE_BUTTON);
    const lastClose = closing[closing.length - 1];
    expect(lastClose.present, 'закрытие: шторка осталась в документе после ухода').toBe(false);

    if (mode.name === 'default') {
      const leaving = closing.filter((f) => f.present && f.inert === true && (f.left ?? 0) <= -80);
      expect(
        leaving.length,
        'уход: нет кадров, где шторка уже уехала на 80 px и закрыта для нажатий (inert)'
      ).toBeGreaterThan(0);
      const gone = closing.find((f) => !f.present);
      expect(gone, 'уход: нет кадра без шторки').toBeDefined();
      expect(
        gone!.t,
        `шторка исчезла через ${Math.round(gone!.t)} мс, ожидалось не раньше 120`
      ).toBeGreaterThanOrEqual(120);
      const fading = closing.filter(
        (f) => f.backdrop !== null && f.backdrop > 0.01 && f.backdrop < 0.99
      );
      expect(
        fading.length,
        'уход: бэкдроп не гаснет — нет кадра с промежуточной прозрачностью'
      ).toBeGreaterThan(0);
    } else {
      const mid = closing.filter((f) => f.present && (f.left ?? 0) < -1);
      expect(mid.length, 'reduced-motion: промежуточные кадры ухода').toBe(0);
      expect(
        closing[0].backdrop,
        `reduced-motion: бэкдроп ещё виден в первом кадре закрытия, прозрачность ${closing[0].backdrop}`
      ).toBeNull();
    }

    await guard.assertClean();
  });
}

test('editor-sheet-motion media-switch vp:1280-768', async ({ page }) => {
  const guard = attachConsoleGuard(page);
  await page.emulateMedia({ reducedMotion: 'no-preference' });
  await page.setViewportSize({ width: 1280, height: 800 });
  await page.goto('/#/editor');

  const column = page.locator('.file-tree-pane:not(.overlay)');
  await expect(column.first(), 'ПК: колонка файлов').toBeVisible({ timeout: T.page });
  await expect(page.locator('.file-tree-pane.overlay'), 'ПК: шторка до смены режима').toHaveCount(
    0
  );
  const overflowBefore = await page.evaluate(() => document.body.style.overflow);

  // ПК → 768: колонка скрывается без движения
  const toNarrow = await sampleAcrossResize(page, { width: 768, height: 1024 }, T.action);
  expect(
    toNarrow.some((f) => f.width === 768),
    'ПК → 768: ширина окна не сменилась'
  ).toBe(true);
  const narrowAnimated = toNarrow.filter((f) => f.paneAnims > 0);
  expect(
    narrowAnimated.length,
    `ПК → 768: колонка уходит с анимацией, кадров ${narrowAnimated.length}`
  ).toBe(0);
  const narrowLast = toNarrow[toNarrow.length - 1];
  expect(narrowLast.column, 'ПК → 768: колонка осталась в последнем кадре').toBe(false);
  expect(narrowLast.sheet, 'ПК → 768: шторка появилась сама в последнем кадре').toBe(false);

  // Открытие шторки на узком окне
  const openBtn = page.locator(OPEN_BUTTON);
  await expect(openBtn.first(), '768: кнопка открытия файлов').toBeVisible({ timeout: T.page });
  await openBtn.first().click();
  const sheet = page.locator('.file-tree-pane.overlay');
  await expect(sheet, '768: шторка после нажатия').toBeVisible({ timeout: T.action });
  await expect
    .poll(() => sheet.evaluate((el) => el.getAnimations().length), {
      message: '768: анимации выезда шторки не закончились',
      timeout: T.action
    })
    .toBe(0);
  expect(
    await page.evaluate(() => document.body.style.overflow),
    '768: прокрутка body при открытой шторке'
  ).toBe('hidden');

  // 768 → ПК при открытой шторке: шторка и бэкдроп исчезают сразу, блокировка снимается
  const toDesktop = await sampleAcrossResize(page, { width: 1280, height: 800 }, T.action);
  expect(
    toDesktop.some((f) => f.width === 1280),
    '768 → ПК: ширина окна не сменилась'
  ).toBe(true);
  const desktopAnimated = toDesktop.filter((f) => f.paneAnims > 0 || f.backdropAnims > 0);
  expect(
    desktopAnimated.length,
    `768 → ПК: шторка или бэкдроп уходят с анимацией поверх колонки, кадров ${desktopAnimated.length}`
  ).toBe(0);
  const desktopLast = toDesktop[toDesktop.length - 1];
  expect(desktopLast.column, '768 → ПК: колонки нет в последнем кадре').toBe(true);
  expect(desktopLast.sheet, '768 → ПК: шторка осталась в последнем кадре').toBe(false);
  expect(desktopLast.backdrop, '768 → ПК: бэкдроп остался в последнем кадре').toBe(false);
  expect(
    desktopLast.bodyOverflow,
    `768 → ПК: прокрутка body "${desktopLast.bodyOverflow}", ожидалось "${overflowBefore}"`
  ).toBe(overflowBefore);

  await expectNoHorizontalScroll(page, 'после возврата на ПК');
  await guard.assertClean();
});
