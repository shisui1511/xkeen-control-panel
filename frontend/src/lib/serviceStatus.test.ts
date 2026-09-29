import { describe, it, expect } from 'vitest';
import {
  STALE_BADGE_AFTER_SECONDS,
  isColdUnknown,
  parseServiceStatus,
  snapshotTimeLabel,
  staleBadgeVisible
} from './serviceStatus';

describe('parseServiceStatus', () => {
  it('разбирает ответ с конвертом {success, data}', () => {
    const d = parseServiceStatus({
      success: true,
      data: { is_running: false, raw: '', stale: true }
    });
    expect(d).not.toBeNull();
    expect(d!.is_running).toBe(false);
    expect(d!.stale).toBe(true);
  });

  it('принимает сырой объект без конверта', () => {
    expect(parseServiceStatus({ is_running: true, age_seconds: 4 })?.is_running).toBe(true);
  });

  it('мусор, null и неуспех → null', () => {
    expect(parseServiceStatus('мусор')).toBeNull();
    expect(parseServiceStatus(null)).toBeNull();
    expect(parseServiceStatus(42)).toBeNull();
    expect(parseServiceStatus({ success: false, error: 'x' })).toBeNull();
    expect(parseServiceStatus({ success: true, data: { pid: 1 } })).toBeNull();
  });
});

describe('staleBadgeVisible', () => {
  it('порог равен 30 секундам', () => {
    expect(STALE_BADGE_AFTER_SECONDS).toBe(30);
  });

  it('граница 30: 30 с → скрыт, 31 с → виден', () => {
    expect(staleBadgeVisible({ is_running: true, stale: true, age_seconds: 30 })).toBe(false);
    expect(staleBadgeVisible({ is_running: true, stale: true, age_seconds: 31 })).toBe(true);
  });

  it('возраст отсутствует (холодный кэш) → скрыт даже при stale', () => {
    expect(staleBadgeVisible({ is_running: false, stale: true })).toBe(false);
  });

  it('свежее значение → скрыт', () => {
    expect(staleBadgeVisible({ is_running: true, stale: false, age_seconds: 2 })).toBe(false);
  });
});

describe('isColdUnknown', () => {
  it('stale без возраста → холодный кэш', () => {
    expect(isColdUnknown({ is_running: false, stale: true })).toBe(true);
  });

  it('есть возраст или не stale → не холодный', () => {
    expect(isColdUnknown({ is_running: false, stale: true, age_seconds: 0 })).toBe(false);
    expect(isColdUnknown({ is_running: false, stale: false })).toBe(false);
  });
});

describe('snapshotTimeLabel', () => {
  it('время момента (now − age) в HH:MM', () => {
    const now = Date.UTC(2026, 8, 29, 10, 0, 45);
    expect(snapshotTimeLabel(45, now, 'ru', 'UTC')).toBe('10:00');
  });

  it('переходит через границу минуты и часа', () => {
    const now = Date.UTC(2026, 8, 29, 10, 0, 10);
    expect(snapshotTimeLabel(40, now, 'en-GB', 'UTC')).toBe('09:59');
  });
});
