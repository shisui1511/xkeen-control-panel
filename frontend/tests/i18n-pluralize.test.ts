/**
 * i18n-pluralize.test.ts — Unit-тесты функции pluralize() из frontend/src/i18n.ts
 *
 * Проверяет русские правила склонения:
 *   n % 10 === 1 && n % 100 !== 11  → one  (1 подписка, 21 подписка)
 *   n % 10 in [2..4] && n % 100 not in [10..19] → few (2 подписки, 22 подписки)
 *   иначе → many (0 подписок, 5 подписок, 11 подписок, 100 подписок)
 */

import { test, expect } from '@playwright/test';
import { pluralize } from '../src/i18n';

const ONE = 'подписка';
const FEW = 'подписки';
const MANY = 'подписок';

test.describe('pluralize() — русские правила склонения', () => {
  test('0 → many (исключение: нулевое значение)', () => {
    expect(pluralize(0, ONE, FEW, MANY)).toBe(MANY);
  });

  test('1 → one', () => {
    expect(pluralize(1, ONE, FEW, MANY)).toBe(ONE);
  });

  test('2 → few', () => {
    expect(pluralize(2, ONE, FEW, MANY)).toBe(FEW);
  });

  test('3 → few', () => {
    expect(pluralize(3, ONE, FEW, MANY)).toBe(FEW);
  });

  test('4 → few', () => {
    expect(pluralize(4, ONE, FEW, MANY)).toBe(FEW);
  });

  test('5 → many', () => {
    expect(pluralize(5, ONE, FEW, MANY)).toBe(MANY);
  });

  test('11 → many (исключение: 11 не one)', () => {
    expect(pluralize(11, ONE, FEW, MANY)).toBe(MANY);
  });

  test('12 → many (исключение: 12 не few)', () => {
    expect(pluralize(12, ONE, FEW, MANY)).toBe(MANY);
  });

  test('21 → one (21 % 10 === 1, 21 % 100 !== 11)', () => {
    expect(pluralize(21, ONE, FEW, MANY)).toBe(ONE);
  });

  test('22 → few', () => {
    expect(pluralize(22, ONE, FEW, MANY)).toBe(FEW);
  });

  test('100 → many', () => {
    expect(pluralize(100, ONE, FEW, MANY)).toBe(MANY);
  });

  test('101 → one', () => {
    expect(pluralize(101, ONE, FEW, MANY)).toBe(ONE);
  });

  test('111 → many (исключение: 111 % 100 === 11)', () => {
    expect(pluralize(111, ONE, FEW, MANY)).toBe(MANY);
  });

  test('32727 → many (32727 % 10 === 7)', () => {
    expect(pluralize(32727, ONE, FEW, MANY)).toBe(MANY);
  });
});
