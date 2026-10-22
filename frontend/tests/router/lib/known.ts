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

// Теги зависимости от ядра (D-04). Playwright отбирает по ним тесты второй ветви набора
// changed: размеченные идут под обоими ядрами, остальные — под одним.
export const CORE_TAG = '@core'; // тест явно читает ядро (RT.core, переключение)
export const CORE_LABEL_TAG = '@core-label'; // ядро названо в селекторе метки knownFailure

// Сборщик селекторов: пока он включён, knownFailure только запоминает селектор (см. coreTagsOf).
let collected: string[] | null = null;

/**
 * Теги теста по его меткам известных падений: [CORE_LABEL_TAG], если среди селекторов,
 * которые вызывает mark, есть называющий ядро (содержит «/»), иначе []. Тег выводится из
 * тех же вызовов knownFailure, что и сама метка, поэтому снятая метка снимает и тег.
 * mark вызывается при объявлении теста: без test.fail и без test.info().
 */
export function coreTagsOf(mark: () => void): string[] {
  const previous = collected;
  const selectors: string[] = [];
  collected = selectors;
  try {
    mark();
  } finally {
    collected = previous;
  }
  return selectors.some((selector) => selector.includes('/')) ? [CORE_LABEL_TAG] : [];
}

export function knownFailure(selector: string, slug: string): void {
  if (collected) {
    collected.push(selector);
    return;
  }
  if (!selectorApplies(selector, RT.arch, RT.core)) return;
  test.fail(true, slug);
  // pw-parse.js ищет именно эту аннотацию: по ней KNOWN отличается от обычного ожидаемого падения
  test.info().annotations.push({ type: 'known-failure', description: slug });
}
