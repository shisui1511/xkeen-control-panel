import { describe, it, expect } from 'vitest';
import ru from '../locales/ru.json';
import en from '../locales/en.json';
import { FAILURE_CODES, STEP_CODES, failureView, stepLabel } from './updateStatusView';

const base = { message: 'x', progress: 30 };

describe('stepLabel', () => {
  it('downloading с версией → ключ и параметр version', () => {
    expect(
      stepLabel({
        ...base,
        status: 'downloading',
        message_code: 'downloading',
        params: { version: '0.29.0-rc.30' }
      })
    ).toEqual({ key: 'settings.update_step_downloading', params: { version: '0.29.0-rc.30' } });
  });

  it('downloading без версии → вариант _plain без params', () => {
    expect(stepLabel({ ...base, status: 'downloading', message_code: 'downloading' })).toEqual({
      key: 'settings.update_step_downloading_plain'
    });
    expect(
      stepLabel({ ...base, status: 'downloading', message_code: 'downloading', params: {} })
    ).toEqual({ key: 'settings.update_step_downloading_plain' });
  });

  it('checking → ключ без params', () => {
    expect(stepLabel({ ...base, status: 'checking', message_code: 'checking' })).toEqual({
      key: 'settings.update_step_checking'
    });
  });

  it('неизвестный код и отсутствие кода → null', () => {
    expect(stepLabel({ ...base, status: 'downloading', message_code: 'foo' })).toBeNull();
    expect(stepLabel({ ...base, status: 'downloading' })).toBeNull();
  });

  it('статус failed → null', () => {
    expect(stepLabel({ ...base, status: 'failed', message_code: 'downloading' })).toBeNull();
  });
});

describe('failureView', () => {
  it('failed + checksum_failed → причина и деталь', () => {
    expect(
      failureView({
        ...base,
        status: 'failed',
        message_code: 'checksum_failed',
        params: { detail: 'sha mismatch' }
      })
    ).toEqual({ reason: { key: 'settings.update_fail_checksum_failed' }, detail: 'sha mismatch' });
  });

  it('failed + auto_rollback_done без detail → пустая деталь', () => {
    expect(failureView({ ...base, status: 'failed', message_code: 'auto_rollback_done' })).toEqual({
      reason: { key: 'settings.update_fail_auto_rollback_done' },
      detail: ''
    });
  });

  it('неизвестный код → null', () => {
    expect(failureView({ ...base, status: 'failed', message_code: 'future_code' })).toBeNull();
    expect(failureView({ ...base, status: 'failed' })).toBeNull();
  });

  it('статус не failed → null', () => {
    expect(
      failureView({ ...base, status: 'downloading', message_code: 'checksum_failed' })
    ).toBeNull();
  });
});

describe('словари', () => {
  const ruDict = ru as Record<string, string>;
  const enDict = en as Record<string, string>;
  const keys = [
    ...STEP_CODES.map((c) => `settings.update_step_${c}`),
    ...[
      'downloading',
      'backup_creating',
      'installing',
      'restarting',
      'complete',
      'rollback_restoring'
    ].map((c) => `settings.update_step_${c}_plain`),
    ...FAILURE_CODES.map((c) => `settings.update_fail_${c}`)
  ];

  it.each(keys)('ключ %s есть в ru.json и en.json', (key) => {
    expect(ruDict[key], `ru: ${key}`).toBeTypeOf('string');
    expect(enDict[key], `en: ${key}`).toBeTypeOf('string');
  });
});
