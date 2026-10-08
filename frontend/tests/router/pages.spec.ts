// e2e-pages: *
import { test, expect } from '@playwright/test';
import AxeBuilder from '@axe-core/playwright';
import { attachConsoleGuard } from './lib/console';
import { T } from './lib/env';
import { knownFailure } from './lib/known';
import { routes } from './lib/pages';

// Обход всех страниц настоящей панели цели (RT-05): две темы, ширины 1440 и 390 px.
// На каждой странице: она отрисовалась, консоль и сеть чистые (D-14), axe не находит
// нарушений critical/serious (WCAG 2.0/2.1 A/AA), на 390 px нет горизонтальной прокрутки.
//
// Список маршрутов берётся из Dashboard.svelte во время прогона (lib/pages.ts): новая
// страница попадает в обход без правки этого файла. Страницы из обхода и проверки из
// axe не исключаются: находка размечается knownFailure с todo (D-12).
//
// Спек касается всех страниц (e2e-pages: *); выбор по маршрутам — через --grep по
// названию `page:<маршрут> theme:<тема> vp:<ширина>`.

const THEMES = ['light', 'dark'] as const;
const VIEWPORTS = [
  { width: 1440, height: 900 },
  { width: 390, height: 844 }
] as const;

// У большинства страниц есть общий заголовок страницы; у остальных ждём собственный
// корневой элемент маршрута.
const HEADING = '.main-content .page-header h1';
const ROOT: Record<string, string> = {
  'mihomo-gen': '.main-content .container'
};

// Адрес, который панель сама подставляет вместо маршрута (tabFromHash: #/mihomo-gen
// открывает конструктор в редакторе). Остальные маршруты должны остаться как есть.
const ALIAS: Record<string, string> = {
  'mihomo-gen': '#/constructor'
};

// Известные находки обхода (D-12): каждая размечена todo из pending и конкретной
// комбинацией маршрута, темы, ширины и цели/ядра. Страница из обхода не исключается и
// проверка axe не отключается: помеченный тест обязан падать, иначе XPASS (метка устарела).
// Селектор: * | <arch> | <arch>/<ядро> | */<ядро>. Вызовы — со строковыми литералами:
// их читает scripts/router/check-known.sh.
function markKnown(route: string, theme: string, width: number): void {
  const light = theme === 'light';
  const dark = theme === 'dark';
  const wide = width === 1440;

  // Капсула состояния на узком экране: тёмный фон не зависит от темы, цвет текста — зависит
  if (light && width === 390) {
    knownFailure('*', 'status-capsule-mobile-traffic-contrast');
  }

  // Зелёный и синий текст на подкрашенном фоне в светлой теме: контраст 3.8–4.3 из 4.5
  if (light && route === 'connections')
    knownFailure('arm64/mihomo', 'light-tinted-badge-text-contrast');
  if (light && route === 'console') knownFailure('*', 'light-tinted-badge-text-contrast');
  if (light && wide && route === 'editor') knownFailure('*', 'light-tinted-badge-text-contrast');
  if (light && route === 'logs') knownFailure('*', 'light-tinted-badge-text-contrast');
  if (light && route === 'proxies') knownFailure('*', 'light-tinted-badge-text-contrast');
  if (light && route === 'traffic') knownFailure('*', 'light-tinted-badge-text-contrast');

  // Приглушённый текст мелких меток, счётчиков и подписей
  if (dark && route === 'dat') knownFailure('*', 'muted-small-label-text-contrast');
  if (light && wide && route === 'dat') knownFailure('*', 'muted-small-label-text-contrast');
  if (light && route === 'logs') knownFailure('*', 'muted-small-label-text-contrast');
  if (route === 'proxies') knownFailure('arm64/mihomo', 'muted-small-label-text-contrast');
  if (light && route === 'services') knownFailure('*', 'muted-small-label-text-contrast');
  if (route === 'smartproxy') knownFailure('*', 'muted-small-label-text-contrast');

  // Строки DAT-файлов с вложенными интерактивными элементами (при Xray)
  if (wide && route === 'dat') knownFailure('*/xray', 'dat-rows-nested-interactive');

  // Конструктор Mihomo: подписи полей, прокручиваемая область, контраст вкладок и кнопок
  if (route === 'mihomo-gen') knownFailure('*/mihomo', 'mihomo-constructor-a11y');
}

for (const route of routes()) {
  for (const theme of THEMES) {
    for (const vp of VIEWPORTS) {
      test(`page:${route} theme:${theme} vp:${vp.width}`, async ({ page }) => {
        // сборщики консоли и сети подключаются до перехода, чтобы ранние ошибки не терялись
        const guard = attachConsoleGuard(page);
        const problems: string[] = [];
        markKnown(route, theme, vp.width);

        await page.setViewportSize({ width: vp.width, height: vp.height });
        await page.addInitScript((th) => {
          localStorage.setItem('theme', th);
        }, theme);

        await page.goto(`/#/${route}`);
        // Страница действительно отрисовалась: опечатка в маршруте или остановка на
        // скелетоне иначе дали бы пустую область, и проверки прошли бы, ничего не проверив
        await expect(page.locator(ROOT[route] ?? HEADING).first()).toBeVisible({
          timeout: T.page
        });
        // Панель опрашивает API постоянно, полной тишины сети не будет: ждём затишья
        // ограниченное время и в любом случае проверяем консоль
        await page.waitForLoadState('networkidle', { timeout: T.network }).catch(() => {});
        // переход между вкладками идёт с затуханием: axe не должен видеть полупрозрачный кадр
        await page.waitForTimeout(500);

        const hash = await page.evaluate(() => window.location.hash);
        // Маршрут #/config панель открывает только при включённом флаге config_layer, иначе
        // сама уводит на #/dashboard (по замыслу, Dashboard.svelte). Это состояние устройства,
        // а не находка: тест пропускается с причиной, страница из обхода не исключается.
        if (route === 'config' && !hash.startsWith('#/config')) {
          const res = await page.request.get('/api/settings');
          const body = res.ok()
            ? ((await res.json()) as { data?: { config_layer?: boolean } })
            : {};
          test.skip(
            body.data?.config_layer === false,
            'флаг config_layer выключен на этом устройстве: панель не открывает #/config'
          );
        }
        // маршрут не подменён редиректом
        if (!hash.startsWith(ALIAS[route] ?? `#/${route}`)) {
          problems.push(`маршрут: после перехода адрес ${hash}`);
        }
        const applied = await page.evaluate(() =>
          document.documentElement.getAttribute('data-theme')
        );
        if (applied !== theme) {
          problems.push(`тема: применена ${applied}, ожидалась ${theme}`);
        }

        // консоль и сеть
        await guard.assertClean().catch((e: Error) => problems.push(e.message));

        // доступность
        const scan = await new AxeBuilder({ page })
          .withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa'])
          .exclude('.cm-editor')
          .exclude('.xterm')
          .analyze();
        const severe = scan.violations.filter(
          (v) => v.impact === 'critical' || v.impact === 'serious'
        );
        if (severe.length > 0) {
          // Только правило, селекторы и числа контраста: HTML узлов может содержать имена узлов
          const list = severe.map((v) => {
            const nodes = v.nodes.slice(0, 3).map((n) => {
              const msg = (n.any[0]?.message ?? '')
                .replace(/^Element has insufficient color contrast of /, 'контраст ')
                .replace(/, font size:.*?\)/, ')')
                .replace(/\. Expected contrast ratio of /, ', нужно ');
              return `${n.target.join(' ')} [${msg}]`;
            });
            return `${v.id} (${v.impact}, узлов ${v.nodes.length}): ${nodes.join(' | ')}`;
          });
          problems.push(
            `axe: нарушений critical/serious ${severe.length}:\n- ${list.join('\n- ')}`
          );
        }

        // горизонтальная прокрутка на узком экране
        if (vp.width === 390) {
          const wide = await page.evaluate(() => ({
            scroll: document.documentElement.scrollWidth,
            inner: window.innerWidth
          }));
          if (wide.scroll > wide.inner + 1) {
            problems.push(`прокрутка: scrollWidth ${wide.scroll} шире окна ${wide.inner}`);
          }
        }

        if (problems.length > 0) {
          throw new Error(problems.join('\n'));
        }
      });
    }
  }
}
