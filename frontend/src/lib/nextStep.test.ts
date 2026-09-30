import { describe, it, expect } from 'vitest';
import { nextStep, preflightConfigReady, type NextStepInput } from './nextStep';

const ready: NextStepInput = {
  xkeen: 'stopped',
  kernelsInstalled: true,
  configReady: true,
  isRunning: false
};

describe('nextStep', () => {
  it('XKeen не установлен → install_xkeen', () => {
    expect(nextStep({ ...ready, xkeen: 'not_installed' })).toBe('install_xkeen');
  });

  it('прерванная настройка → finish_xkeen_setup', () => {
    expect(nextStep({ ...ready, xkeen: 'setup_incomplete' })).toBe('finish_xkeen_setup');
  });

  it('оба ядра не установлены → install_kernel', () => {
    expect(nextStep({ ...ready, kernelsInstalled: false })).toBe('install_kernel');
  });

  it('нет подключений → configure', () => {
    expect(nextStep({ ...ready, configReady: false })).toBe('configure');
  });

  it('всё готово, не запущено → start', () => {
    expect(nextStep(ready)).toBe('start');
  });

  it('запущено → null', () => {
    expect(nextStep({ ...ready, xkeen: 'running', isRunning: true })).toBeNull();
    expect(nextStep({ ...ready, isRunning: true })).toBeNull();
  });

  it('состояние неизвестно (холодный кэш) → null, шаги configure/start не выдаются', () => {
    expect(nextStep({ ...ready, xkeen: 'unknown' })).toBeNull();
    expect(nextStep({ ...ready, xkeen: 'unknown', configReady: false })).toBeNull();
    expect(nextStep({ ...ready, xkeen: 'unknown', kernelsInstalled: false })).toBeNull();
  });

  it('configReady неизвестен → шаг не выдаётся (ни configure, ни start)', () => {
    expect(nextStep({ ...ready, configReady: null })).toBeNull();
  });

  it('configReady === true → start', () => {
    expect(nextStep({ ...ready, configReady: true })).toBe('start');
  });

  it('kernelsInstalled неизвестен → install_kernel не выдаётся', () => {
    expect(nextStep({ ...ready, kernelsInstalled: null })).not.toBe('install_kernel');
  });

  it('приоритет: отсутствие XKeen важнее отсутствия ядер', () => {
    expect(nextStep({ ...ready, xkeen: 'not_installed', kernelsInstalled: false })).toBe(
      'install_xkeen'
    );
  });
});

describe('preflightConfigReady', () => {
  it('Xray: нет реальных outbound → false', () => {
    expect(
      preflightConfigReady('xray', { errors: [], warnings: [{ code: 'no_real_outbounds' }] })
    ).toBe(false);
  });

  it('Mihomo: нет прокси и провайдеров → false', () => {
    expect(
      preflightConfigReady('mihomo', { warnings: [{ code: 'no_proxies_or_providers' }] })
    ).toBe(false);
  });

  it('код чужого ядра не считается', () => {
    expect(
      preflightConfigReady('xray', {
        valid: true,
        errors: [],
        warnings: [{ code: 'no_proxies_or_providers' }]
      })
    ).toBe(true);
  });

  it('непустые errors → false', () => {
    expect(
      preflightConfigReady('xray', { valid: false, errors: [{ code: 'x', message: 'y' }] })
    ).toBe(false);
  });

  it('прочие предупреждения не включают шаг «Настройте»', () => {
    expect(
      preflightConfigReady('mihomo', { valid: true, errors: [], warnings: [{ code: 'other' }] })
    ).toBe(true);
  });

  it('валидный → true', () => {
    expect(preflightConfigReady('xray', { valid: true, errors: [], warnings: [] })).toBe(true);
  });

  it('некорректный ответ → null', () => {
    expect(preflightConfigReady('xray', null)).toBeNull();
    expect(preflightConfigReady('xray', 'oops')).toBeNull();
    expect(preflightConfigReady('xray', {})).toBeNull();
  });
});
