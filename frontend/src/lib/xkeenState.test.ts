import { describe, it, expect } from 'vitest';
import { xkeenCardStatus, xkeenState, xkeenVersionLabel } from './xkeenState';

const svc = (over: Record<string, unknown> = {}) => ({ is_running: false, ...over });

describe('xkeenState', () => {
  it('capabilities: XKeen не установлен', () => {
    expect(xkeenState({ xkeen_installed: false }, svc())).toBe('not_installed');
  });

  it('статус: XKeen не установлен', () => {
    expect(xkeenState(null, svc({ xkeen_installed: false }))).toBe('not_installed');
  });

  it('прерванная настройка', () => {
    expect(xkeenState(null, svc({ xkeen_installed: true, xkeen_setup_incomplete: true }))).toBe(
      'setup_incomplete'
    );
  });

  it('запущен', () => {
    expect(xkeenState({ xkeen_installed: true }, svc({ is_running: true }))).toBe('running');
  });

  it('холодный кэш и не запущен → unknown', () => {
    expect(xkeenState(null, svc({ stale: true }))).toBe('unknown');
  });

  it('есть ответ и не запущен → stopped', () => {
    expect(xkeenState(null, svc({ stale: false, age_seconds: 3 }))).toBe('stopped');
    expect(xkeenState(null, svc())).toBe('stopped');
  });

  it('нет ни capabilities, ни статуса → unknown', () => {
    expect(xkeenState(null, null)).toBe('unknown');
  });

  it('только capabilities с установленным XKeen, без статуса → unknown', () => {
    expect(xkeenState({ xkeen_installed: true }, null)).toBe('unknown');
  });
});

describe('xkeenCardStatus', () => {
  it('прерванная настройка показывается как остановлен', () => {
    expect(xkeenCardStatus('setup_incomplete')).toBe('stopped');
  });

  it('остальные состояния переносятся как есть', () => {
    expect(xkeenCardStatus('running')).toBe('running');
    expect(xkeenCardStatus('stopped')).toBe('stopped');
    expect(xkeenCardStatus('unknown')).toBe('unknown');
    expect(xkeenCardStatus('not_installed')).toBe('not_installed');
  });
});

describe('xkeenVersionLabel', () => {
  it('не установлен → ключ словаря', () => {
    expect(xkeenVersionLabel('not_installed', '1.0')).toEqual({
      key: 'dash.xkeen_version_not_installed'
    });
  });

  it('unknown и пустая версия → тире', () => {
    expect(xkeenVersionLabel('stopped', 'unknown')).toEqual({ text: '—' });
    expect(xkeenVersionLabel('running', '')).toEqual({ text: '—' });
    expect(xkeenVersionLabel('running', undefined)).toEqual({ text: '—' });
  });

  it('обычная версия как есть', () => {
    expect(xkeenVersionLabel('running', '2.0 Beta')).toEqual({ text: '2.0 Beta' });
  });
});
