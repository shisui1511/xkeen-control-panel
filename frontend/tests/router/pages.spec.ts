// e2e-pages: *
// e2e-kind: read
import { test, expect, type Page } from '@playwright/test';
import AxeBuilder from '@axe-core/playwright';
import { attachConsoleGuard } from './lib/console';
import { RT, T } from './lib/env';
import { CORE_TAG, coreTagsOf, knownFailure } from './lib/known';
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

// Часть нарушений axe видна только на загруженных данных страницы (например, путь файла и
// теги в инспекторе DAT). Проверка до их загрузки даёт плавающий вердикт: помеченный тест
// то падает, то проходит (XPASS). Поэтому страница с такими данными ждёт их явно и только
// наблюдает: сеть не подменяется, а не дождавшийся тест падает как находка (стенд или продукт).
async function datReady(page: Page): Promise<void> {
  const fail = (what: string, ms: number) =>
    new Error(`страница dat не загрузила данные за ${Math.round(ms / 1000)} с: ${what}`);
  const workspace = page.locator('.main-content');

  // список файлов загружен: индикатора загрузки на месте рабочей области нет
  await expect(workspace.locator('p.text-secondary').filter({ hasText: /Загрузка|Loading/ }))
    .toHaveCount(0, { timeout: T.page })
    .catch(() => {
      throw fail('список файлов не загрузился', T.page);
    });

  // правая колонка показывает инспектор, заглушку «файл не выбран» или набор по умолчанию;
  // ожидание attached, а не visible: на 390 px колонка может быть скрыта вёрсткой
  await expect(
    page
      .locator(
        [
          '.dat-detail .tags-grid',
          '.dat-detail .inspector-scroll-area .td-state',
          '.dat-detail .inspector-body',
          '.dat-detail .detail-empty-card',
          '[data-testid="dat-standard-sets"]'
        ].join(', ')
      )
      .first()
  )
    .toBeAttached({ timeout: T.page })
    .catch(() => {
      throw fail('правая колонка не отрисована', T.page);
    });

  // теги догружены: тяжёлый geosite на mipsle разбирается дольше обычной загрузки страницы
  await expect(page.locator('.dat-detail .inspector-scroll-area .spinner-circle'))
    .toHaveCount(0, { timeout: T.save })
    .catch(() => {
      throw fail('теги выбранного файла не загрузились', T.save);
    });
}

// Прокси: группы Mihomo и значок внешнего IP клиента. Нарушения контраста живут в счётчиках
// разделов групп и в значке маршрута IP, которых нет, пока виден скелетон или идёт проверка IP.
async function proxiesReady(page: Page): Promise<void> {
  const fail = (what: string, ms: number) =>
    new Error(`страница proxies не загрузила данные за ${Math.round(ms / 1000)} с: ${what}`);
  const workspace = page.locator('.main-content');

  // группы загружены либо показано пустое состояние (Mihomo не запущен): скелетона нет
  await expect(workspace.locator('.skeleton-card'))
    .toHaveCount(0, { timeout: T.page })
    .catch(() => {
      throw fail('группы прокси не загрузились', T.page);
    });

  // проверка внешнего IP клиента закончилась (показан адрес или кнопка повторной проверки)
  await expect(workspace.locator('.pill-label').filter({ hasText: /Определение IP|Detecting IP/ }))
    .toHaveCount(0, { timeout: T.page })
    .catch(() => {
      throw fail('проверка внешнего IP не закончилась', T.page);
    });
}

// Подключения: индикатор в шапке зелёный («Онлайн») только после соединения сокета Mihomo.
// Нарушение контраста живёт именно в этом зелёном индикаторе, поэтому проверка до соединения
// даёт плавающий вердикт (то KNOWN, то XPASS). Если Clash API ядра выключен (api_reachable
// false в /api/capabilities), индикатор законно остаётся «нет связи» и ждать нечего.
async function connectionsReady(page: Page): Promise<void> {
  if (RT.core !== 'mihomo') return;
  const reachable = await page.evaluate(async () => {
    const res = await fetch('/api/capabilities', { credentials: 'same-origin' });
    const body = (await res.json()) as {
      data?: { mihomo?: { api_reachable?: boolean } };
      mihomo?: { api_reachable?: boolean };
    };
    return Boolean((body.data ?? body).mihomo?.api_reachable);
  });
  if (!reachable) return;
  await expect(page.locator('.main-content .status-indicator.connected').first())
    .toBeVisible({ timeout: T.page })
    .catch(() => {
      throw new Error(
        `страница connections не загрузила данные за ${Math.round(T.page / 1000)} с: сокет подключений не соединился`
      );
    });
}

// Конструктор Mihomo: сценарии, вкладки разделов и предпросмотр YAML строятся по схеме,
// пока она грузится, на месте конструктора только индикатор загрузки.
async function mihomoGenReady(page: Page): Promise<void> {
  const fail = (what: string, ms: number) =>
    new Error(`страница mihomo-gen не загрузила данные за ${Math.round(ms / 1000)} с: ${what}`);
  const workspace = page.locator('.main-content');

  await expect(workspace.locator('.loading-state-block'))
    .toHaveCount(0, { timeout: T.page })
    .catch(() => {
      throw fail('схема конструктора не загрузилась', T.page);
    });

  // сценарии из схемы отрисованы; attached, а не visible: на 390 px часть колонки скрыта
  await expect(workspace.locator('.gen-layout button[data-scenario-id]').first())
    .toBeAttached({ timeout: T.page })
    .catch(() => {
      throw fail('сценарии конструктора не отрисованы', T.page);
    });
}

// Ожидание данных страницы перед axe: маршрут -> функция готовности
const READY: Record<string, (page: Page) => Promise<void>> = {
  dat: datReady,
  connections: connectionsReady,
  proxies: proxiesReady,
  'mihomo-gen': mihomoGenReady
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
  // Бейдж вида выпуска в баннере обновления (dashboard, светлая тема, 1440 px) есть, пока
  // доступен более новый не-stable выпуск: метка ставится в самом тесте, когда бейдж на странице

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

// Маршруты, чей тест читает ядро: connectionsReady ждёт сокет Mihomo только на Mihomo
const CORE_ROUTES = new Set(['connections']);

// Тесты одной страницы идут в одном воркере по очереди (mode default): консоль открывает
// сеанс терминала, и параллельные вкладки упираются в лимит сеансов панели
for (const route of routes()) {
  test.describe(route, () => {
    test.describe.configure({ mode: 'default' });
    for (const theme of THEMES) {
      for (const vp of VIEWPORTS) {
        // Теги ядра (D-04): явный @core по CORE_ROUTES и автоматический @core-label из меток
        // известных падений с ядром в селекторе. Название теста не меняется: по нему --grep.
        const tag = [
          ...coreTagsOf(() => markKnown(route, theme, vp.width)),
          ...(CORE_ROUTES.has(route) ? [CORE_TAG] : [])
        ];
        test(`page:${route} theme:${theme} vp:${vp.width}`, { tag }, async ({ page }) => {
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
          await READY[route]?.(page);

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
          // Состояние списка выпусков внешнее, баннер приходит асинхронно: без бейджа нарушения
          // нет, и метка дала бы XPASS. Решение принимается по результату axe, а не по DOM до него.
          // axe называет узел кратчайшим селектором (.badge-primary), без предка, поэтому
          // принадлежность бейджа баннеру проверяется по странице после проверки.
          if (
            route === 'dashboard' &&
            theme === 'light' &&
            vp.width === 1440 &&
            severe.some(
              (v) =>
                v.id === 'color-contrast' &&
                v.nodes.some((n) => n.target.join(' ').includes('.badge-primary'))
            ) &&
            (await page.locator('.update-banner .badge-primary').count()) > 0
          ) {
            knownFailure('*', 'light-tinted-badge-text-contrast');
          }
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
  });
}
