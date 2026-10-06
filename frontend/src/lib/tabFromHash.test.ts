import { describe, it, expect } from 'vitest';
import { tabFromHash, legacyRedirectHash } from './tabFromHash';

describe('tabFromHash', () => {
  it('пустой хэш — дашборд', () => {
    expect(tabFromHash('')).toBe('dashboard');
    expect(tabFromHash('#')).toBe('dashboard');
    expect(tabFromHash('#/')).toBe('dashboard');
  });

  it('обычные маршруты', () => {
    expect(tabFromHash('#/settings')).toBe('settings');
    expect(tabFromHash('#/services')).toBe('services');
    expect(tabFromHash('#/proxies?tab=providers')).toBe('proxies');
  });

  it('синонимы редактора', () => {
    expect(tabFromHash('#/constructor')).toBe('editor');
    expect(tabFromHash('#/mihomo-gen')).toBe('editor');
    expect(tabFromHash('#/editor?tab=constructor')).toBe('editor');
  });

  it('раздел «Конфигурация» и его подразделы', () => {
    expect(tabFromHash('#/config')).toBe('config');
    expect(tabFromHash('#/config/sources')).toBe('config');
    expect(tabFromHash('#/config/sources?x=1')).toBe('config');
  });

  it('устаревшие адреса подписок — прокси', () => {
    expect(tabFromHash('#/subscriptions')).toBe('proxies');
    expect(tabFromHash('#/subscriptions/abc')).toBe('proxies');
  });
});

describe('legacyRedirectHash', () => {
  it('переводит адреса подписок', () => {
    expect(legacyRedirectHash('#/subscriptions')).toBe('#/proxies?tab=providers');
    expect(legacyRedirectHash('#/subscriptions/abc')).toBe('#/proxies?tab=providers&expand=abc');
  });

  it('прочие адреса не трогает', () => {
    expect(legacyRedirectHash('#/settings')).toBeNull();
    expect(legacyRedirectHash('')).toBeNull();
    expect(legacyRedirectHash('#/proxies?tab=providers')).toBeNull();
  });
});
