import { test } from '@playwright/test';
import { RT } from './env';

// Метка известного падения (D-12): тест, который падает из-за найденного дефекта
// продукта, помечается вызовом knownFailure('<селектор>', '<slug todo>') внутри теста.
// Селектор — как в scripts/router/lib.sh (rt_selector_match): * | <arch> | <arch>/<ядро> |
// */<ядро>. Если селектор подходит цели, тест помечается через test.fail: падение
// считается ожидаемым (в отчёте KNOWN), а прохождение — XPASS и красный прогон
// (метка больше не нужна, todo пора закрывать).
//
// Оба аргумента — строковые литералы: их ищет scripts/router/check-known.sh и проверяет,
// что todo с таким slug лежит в pending (D-13).

/** Подходит ли селектор текущей цели (архитектура и ядро из окружения этапа). */
export function selectorApplies(selector: string, arch = RT.arch, core = RT.core): boolean {
  if (selector === '*') return true;
  if (selector.startsWith('*/')) return selector.slice(2) === core;
  const slash = selector.indexOf('/');
  if (slash >= 0) return selector.slice(0, slash) === arch && selector.slice(slash + 1) === core;
  return selector === arch;
}

export function knownFailure(selector: string, slug: string): void {
  if (!selectorApplies(selector, RT.arch, RT.core)) return;
  test.fail(true, slug);
  // pw-parse.js ищет именно эту аннотацию: по ней KNOWN отличается от обычного ожидаемого падения
  test.info().annotations.push({ type: 'known-failure', description: slug });
}
