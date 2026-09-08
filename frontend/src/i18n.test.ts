/**
 * i18n.test.ts — Unit-тесты функции pluralize() из frontend/src/i18n.ts
 *
 * Проверяет русские правила склонения:
 *   n % 10 === 1 && n % 100 !== 11  → one  (1 подписка, 21 подписка)
 *   n % 10 in [2..4] && n % 100 not in [10..19] → few (2 подписки, 22 подписки)
 *   иначе → many (0 подписок, 5 подписок, 11 подписок, 100 подписок)
 */

import { describe, it, expect } from 'vitest';
import { get } from 'svelte/store';
import { pluralize, tp, loadLanguage, currentLang } from './i18n';

const ONE = 'подписка';
const FEW = 'подписки';
const MANY = 'подписок';

describe('pluralize() — русские правила склонения', () => {
  it('0 → many (исключение: нулевое значение)', () => {
    expect(pluralize(0, ONE, FEW, MANY)).toBe(MANY);
  });

  it('1 → one', () => {
    expect(pluralize(1, ONE, FEW, MANY)).toBe(ONE);
  });

  it('2 → few', () => {
    expect(pluralize(2, ONE, FEW, MANY)).toBe(FEW);
  });

  it('3 → few', () => {
    expect(pluralize(3, ONE, FEW, MANY)).toBe(FEW);
  });

  it('4 → few', () => {
    expect(pluralize(4, ONE, FEW, MANY)).toBe(FEW);
  });

  it('5 → many', () => {
    expect(pluralize(5, ONE, FEW, MANY)).toBe(MANY);
  });

  it('11 → many (исключение: 11 не one)', () => {
    expect(pluralize(11, ONE, FEW, MANY)).toBe(MANY);
  });

  it('12 → many (исключение: 12 не few)', () => {
    expect(pluralize(12, ONE, FEW, MANY)).toBe(MANY);
  });

  it('21 → one (21 % 10 === 1, 21 % 100 !== 11)', () => {
    expect(pluralize(21, ONE, FEW, MANY)).toBe(ONE);
  });

  it('22 → few', () => {
    expect(pluralize(22, ONE, FEW, MANY)).toBe(FEW);
  });

  it('100 → many', () => {
    expect(pluralize(100, ONE, FEW, MANY)).toBe(MANY);
  });

  it('101 → one', () => {
    expect(pluralize(101, ONE, FEW, MANY)).toBe(ONE);
  });

  it('111 → many (исключение: 111 % 100 === 11)', () => {
    expect(pluralize(111, ONE, FEW, MANY)).toBe(MANY);
  });

  it('32727 → many (32727 % 10 === 7)', () => {
    expect(pluralize(32727, ONE, FEW, MANY)).toBe(MANY);
  });
});

describe('pluralize() — английские правила склонения', () => {
  const ONE = 'entry';
  const MANY = 'entries';

  it('1 → one', () => {
    expect(pluralize(1, ONE, '', MANY, 'en')).toBe(ONE);
  });

  it('0, 2, 5, 21 → many', () => {
    expect(pluralize(0, ONE, '', MANY, 'en')).toBe(MANY);
    expect(pluralize(2, ONE, '', MANY, 'en')).toBe(MANY);
    expect(pluralize(5, ONE, '', MANY, 'en')).toBe(MANY);
    expect(pluralize(21, ONE, '', MANY, 'en')).toBe(MANY);
  });
});

describe('tp() store — склоняемые подписи', () => {
  it('русские формы для proxies.nodes и proxies.groups', async () => {
    await loadLanguage('ru');
    currentLang.set('ru');
    const tpFn = get(tp);

    expect(tpFn('proxies.nodes', 1)).toBe('узел');
    expect(tpFn('proxies.nodes', 2)).toBe('узла');
    expect(tpFn('proxies.nodes', 3)).toBe('узла');
    expect(tpFn('proxies.nodes', 4)).toBe('узла');
    expect(tpFn('proxies.nodes', 5)).toBe('узлов');
    expect(tpFn('proxies.nodes', 0)).toBe('узлов');
    expect(tpFn('proxies.nodes', 11)).toBe('узлов');
    expect(tpFn('proxies.nodes', 12)).toBe('узлов');
    expect(tpFn('proxies.nodes', 14)).toBe('узлов');
    expect(tpFn('proxies.nodes', 21)).toBe('узел');
    expect(tpFn('proxies.nodes', 22)).toBe('узла');
    expect(tpFn('proxies.nodes', 43)).toBe('узла');
    expect(tpFn('proxies.nodes', 45)).toBe('узлов');
    expect(tpFn('proxies.nodes', 38)).toBe('узлов');

    expect(tpFn('proxies.groups', 1)).toBe('группа');
    expect(tpFn('proxies.groups', 3)).toBe('группы');
    expect(tpFn('proxies.groups', 17)).toBe('групп');

    expect(() => tpFn('proxies.nodes', 5, { extra: 'x' })).not.toThrow();
  });

  it('английские формы при currentLang === en', async () => {
    await loadLanguage('en');
    currentLang.set('en');
    const tpFn = get(tp);

    expect(tpFn('proxies.nodes', 1)).toBe('node');
    expect(tpFn('proxies.nodes', 5)).toBe('nodes');
    expect(tpFn('proxies.groups', 1)).toBe('group');
    expect(tpFn('proxies.groups', 5)).toBe('groups');
    expect(tpFn('proxies.obs_proxy_nodes', 1)).toBe('proxy');
    expect(tpFn('proxies.obs_proxy_nodes', 43)).toBe('proxies');
  });
});
