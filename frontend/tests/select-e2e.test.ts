import { describe, it, expect } from 'vitest';
import { selectSpecs, specRoutes, buildModel, allSpecs } from '../scripts/select-e2e.mjs';

// Проверки выбора e2e-спеков на реальном графе импортов проекта

const fe = (p: string) => `frontend/${p}`;

describe('select-e2e: граф страниц', () => {
  const model = buildModel();

  it('находит ленивые страницы из Dashboard.svelte', () => {
    for (const route of ['proxies', 'logs', 'editor', 'dat', 'settings', 'dashboard']) {
      expect(model.routes.has(route)).toBe(true);
    }
  });

  it('оболочка содержит App, Sidebar и глобальные стили', () => {
    const shell = [...model.shell].map((f: string) => f.replace(/.*\/frontend\//, ''));
    expect(shell).toContain('src/App.svelte');
    expect(shell).toContain('src/components/Sidebar.svelte');
    expect(shell).toContain('src/styles/global.css');
    expect(shell).toContain('src/styles/fonts.css');
    expect(shell).not.toContain('src/Logs.svelte');
  });
});

describe('select-e2e: выбор спеков', () => {
  const total = allSpecs().length;

  it('страница запускает только свои спеки', () => {
    const res = selectSpecs([fe('src/Logs.svelte')]);
    expect(res.mode).toBe('some');
    expect(res.specs).toContain('tests/logs.spec.ts');
    expect(res.specs).not.toContain('tests/proxies-ui.spec.ts');
    expect(res.specs.length).toBeLessThan(total);
  });

  it('виджет дашборда запускает спеки дашборда, а не все', () => {
    const res = selectSpecs([fe('src/components/dashboard/SystemInfoWidget.svelte')]);
    expect(res.mode).toBe('some');
    expect(res.specs).toContain('tests/dashboard.spec.ts');
    expect(res.specs).not.toContain('tests/logs.spec.ts');
  });

  it('оболочка приложения запускает все спеки', () => {
    expect(selectSpecs([fe('src/components/Sidebar.svelte')]).mode).toBe('all');
    expect(selectSpecs([fe('src/styles/global.css')]).mode).toBe('all');
  });

  it('хелперы тестов, конфиги и CI запускают все спеки', () => {
    expect(selectSpecs([fe('tests/helpers/api-mocks.ts')]).mode).toBe('all');
    expect(selectSpecs([fe('package.json')]).mode).toBe('all');
    expect(selectSpecs(['.github/workflows/ci.yml']).mode).toBe('all');
  });

  it('неизвестный файл фронтенда запускает все спеки', () => {
    expect(selectSpecs([fe('some-new-config.json')]).mode).toBe('all');
  });

  it('изменения вне фронтенда не запускают e2e', () => {
    const res = selectSpecs(['internal/services/kernel.go', 'scripts/setup.sh', 'README.md']);
    expect(res.mode).toBe('none');
    expect(res.specs).toEqual([]);
  });

  it('unit-тесты и документация фронтенда не запускают e2e', () => {
    expect(selectSpecs([fe('src/lib/format.test.ts'), fe('README.md')]).mode).toBe('none');
  });

  it('изменённый спек запускается сам', () => {
    const res = selectSpecs([fe('tests/logs.spec.ts')]);
    expect(res.specs).toEqual(['tests/logs.spec.ts']);
  });
});

describe('select-e2e: маршруты спека', () => {
  const known = new Set(['proxies', 'logs', 'editor', 'dashboard', 'settings']);

  it('берёт маршруты из хэшей и синонимов', () => {
    const routes = specRoutes(`await page.goto('/#/constructor'); goto('/#/subscriptions')`, known);
    expect([...routes!].sort()).toEqual(['editor', 'proxies']);
  });

  it('понимает экранированный маршрут в регулярке toHaveURL', () => {
    expect([...specRoutes('await expect(page).toHaveURL(/#\\/logs/);', known)!]).toEqual(['logs']);
  });

  it('переход на корень — дашборд', () => {
    expect([...specRoutes(`await page.goto('/');`, known)!]).toEqual(['dashboard']);
  });

  it('учитывает аннотацию e2e-pages', () => {
    const routes = specRoutes(`// e2e-pages: logs, settings\ntest('x', () => {})`, known);
    expect([...routes!].sort()).toEqual(['logs', 'settings']);
  });

  it('без маршрутов возвращает null — такой спек запускается всегда', () => {
    expect(specRoutes(`test('x', () => {})`, known)).toBeNull();
  });
});
