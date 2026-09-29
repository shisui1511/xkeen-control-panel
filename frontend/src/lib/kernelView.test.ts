import { describe, it, expect } from 'vitest';
import ru from '../locales/ru.json';
import {
  TRANSITIONAL_KERNEL_STATUSES,
  formatKernelVersion,
  isTransitionalStatus,
  kernelBadge,
  resultMessage,
  rollbackLabel,
  rollbackVisible,
  showInstallStable,
  stageLabelKey
} from './kernelView';

/** Подстановка {name}-плейсхолдеров в шаблон словаря — как это делает $t(). */
function render(key: string, params: Record<string, string> = {}): string {
  const tpl = (ru as Record<string, string>)[key];
  expect(tpl, `ключ ${key} есть в ru.json`).toBeTypeOf('string');
  return tpl.replace(/\{(\w+)\}/g, (_, n) => params[n] ?? '');
}

describe('resultMessage', () => {
  it('обновление: ключ и версия с префиксом v, текст ru «Обновлено до v26.3.27»', () => {
    const m = resultMessage({ status: 'done', result_kind: 'updated', result_version: '26.3.27' });
    expect(m).toEqual({ key: 'svc.kernel_result_updated', params: { version: 'v26.3.27' } });
    expect(render(m!.key, m!.params)).toBe('Обновлено до v26.3.27');
  });

  it('пустая result_version → версия из current_version', () => {
    const m = resultMessage({
      status: 'done',
      result_kind: 'installed',
      result_version: '',
      current_version: '1.19.1'
    });
    expect(m).toEqual({ key: 'svc.kernel_result_installed', params: { version: 'v1.19.1' } });
    expect(render(m!.key, m!.params)).toBe('Установлено v1.19.1');
  });

  it('все допустимые виды итога имеют ключ и ru-текст', () => {
    const expected: Record<string, string> = {
      installed: 'Установлено v1.0.0',
      updated: 'Обновлено до v1.0.0',
      reinstalled: 'Переустановлено v1.0.0',
      rolled_back: 'Откачено на v1.0.0',
      uploaded: 'Загружено v1.0.0'
    };
    for (const [kind, text] of Object.entries(expected)) {
      const m = resultMessage({ status: 'done', result_kind: kind, result_version: '1.0.0' });
      expect(m?.key).toBe(`svc.kernel_result_${kind}`);
      expect(render(m!.key, m!.params)).toBe(text);
    }
  });

  it('неизвестный result_kind → null', () => {
    expect(resultMessage({ status: 'done', result_kind: 'bogus', result_version: '1' })).toBeNull();
  });

  it('не done → null', () => {
    expect(resultMessage({ status: 'idle', result_kind: 'installed' })).toBeNull();
    expect(resultMessage({ status: 'installing', result_kind: 'installed' })).toBeNull();
  });
});

describe('stageLabelKey / isTransitionalStatus', () => {
  it('этап extracting при installing', () => {
    expect(stageLabelKey({ status: 'installing', stage: 'extracting' })).toBe(
      'svc.kernel_stage_extracting'
    );
  });

  it('downloading без stage → этап скачивания', () => {
    expect(stageLabelKey({ status: 'downloading' })).toBe('svc.kernel_stage_downloading');
  });

  it('ru-подписи этапов', () => {
    expect(render('svc.kernel_stage_starting')).toBe('Старт…');
    expect(render('svc.kernel_stage_downloading')).toBe('Скачивание…');
    expect(render('svc.kernel_stage_extracting')).toBe('Распаковка…');
    expect(render('svc.kernel_stage_replacing')).toBe('Замена…');
  });

  it('idle и done → null', () => {
    expect(stageLabelKey({ status: 'idle' })).toBeNull();
    expect(stageLabelKey({ status: 'done', stage: 'replacing' })).toBeNull();
  });

  it('переходные статусы — только checking/downloading/installing', () => {
    expect([...TRANSITIONAL_KERNEL_STATUSES].sort()).toEqual([
      'checking',
      'downloading',
      'installing'
    ]);
    for (const s of ['checking', 'downloading', 'installing']) {
      expect(isTransitionalStatus(s)).toBe(true);
    }
    for (const s of ['idle', 'done', 'failed', '', undefined]) {
      expect(isTransitionalStatus(s)).toBe(false);
    }
  });
});

describe('kernelBadge', () => {
  const base = { current_version: '26.3.27', channel: 'stable', status: 'idle' };

  it('не установлено', () => {
    expect(kernelBadge({ ...base, current_version: 'not installed' })).toEqual({
      variant: 'stopped',
      key: 'kernel.status.not_installed'
    });
  });

  it('проверяем… при status checking', () => {
    expect(kernelBadge({ ...base, status: 'checking' })).toEqual({
      variant: 'idle',
      key: 'svc.channel_checking'
    });
  });

  it('ошибка при failed', () => {
    expect(kernelBadge({ ...base, status: 'failed' })).toEqual({
      variant: 'stopped',
      key: 'svc.kernel_error_badge'
    });
  });

  it('has_update → «→ vX»', () => {
    expect(kernelBadge({ ...base, has_update: true, latest_version: '26.9.9' })).toEqual({
      variant: 'warning',
      label: '→ v26.9.9'
    });
  });

  it('ahead_of_latest на stable → pre-release', () => {
    const b = kernelBadge({
      ...base,
      current_version: '26.9.8',
      ahead_of_latest: true,
      latest_version: '26.3.27'
    });
    expect(b).toEqual({
      variant: 'warning',
      key: 'svc.prerelease_ahead',
      params: { version: 'v26.3.27' }
    });
    expect(render(b.key!, b.params)).toBe('Установлена pre-release, стабильная — v26.3.27');
  });

  it('ahead_of_latest на preview → актуально', () => {
    expect(
      kernelBadge({ ...base, channel: 'preview', ahead_of_latest: true, latest_version: '26.3.27' })
    ).toEqual({ variant: 'idle', key: 'svc.actual_badge' });
  });

  it('иначе актуально', () => {
    expect(kernelBadge(base)).toEqual({ variant: 'idle', key: 'svc.actual_badge' });
  });
});

describe('откат', () => {
  it('rollbackVisible: нет бэкапа → false', () => {
    expect(rollbackVisible({ has_backup: false, current_version: '26.3.27' })).toBe(false);
  });

  it('rollbackVisible: версия бэкапа совпадает с установленной → false', () => {
    expect(
      rollbackVisible({ has_backup: true, backup_version: '26.9.8', current_version: '26.9.8' })
    ).toBe(false);
    expect(
      rollbackVisible({ has_backup: true, backup_version: 'v26.9.8', current_version: '26.9.8' })
    ).toBe(false);
  });

  it('rollbackVisible: другая версия → true; версия бэкапа неизвестна → true', () => {
    expect(
      rollbackVisible({ has_backup: true, backup_version: '26.9.8', current_version: '26.3.27' })
    ).toBe(true);
    expect(rollbackVisible({ has_backup: true, current_version: '26.3.27' })).toBe(true);
  });

  it('rollbackLabel с версией: «Откатить на v26.9.8», подпись начинается с «Откатить»', () => {
    const l = rollbackLabel({ has_backup: true, backup_version: '26.9.8' });
    expect(l).toEqual({ key: 'svc.rollback_to', params: { version: 'v26.9.8' } });
    const text = render(l.key, l.params);
    expect(text).toBe('Откатить на v26.9.8');
    expect(text.startsWith('Откатить')).toBe(true);
  });

  it('rollbackLabel без версии: «Откатить»', () => {
    const l = rollbackLabel({ has_backup: true });
    expect(l).toEqual({ key: 'svc.rollback' });
    expect(render(l.key)).toBe('Откатить');
  });
});

describe('showInstallStable', () => {
  const ahead = { ahead_of_latest: true, channel: 'stable', status: 'idle' };

  it('ahead на stable вне перехода → true', () => {
    expect(showInstallStable(ahead)).toBe(true);
  });

  it('во время downloading → false', () => {
    expect(showInstallStable({ ...ahead, status: 'downloading' })).toBe(false);
  });

  it('не ahead или preview → false', () => {
    expect(showInstallStable({ ...ahead, ahead_of_latest: false })).toBe(false);
    expect(showInstallStable({ ...ahead, channel: 'preview' })).toBe(false);
  });
});

describe('formatKernelVersion', () => {
  it('not installed и пусто → пустая строка', () => {
    expect(formatKernelVersion('not installed')).toBe('');
    expect(formatKernelVersion('')).toBe('');
    expect(formatKernelVersion(undefined)).toBe('');
  });

  it('числовая версия получает префикс v один раз', () => {
    expect(formatKernelVersion('26.3.27')).toBe('v26.3.27');
    expect(formatKernelVersion('v26.3.27')).toBe('v26.3.27');
  });

  it('плавающая сборка — без префикса', () => {
    expect(formatKernelVersion('alpha-f103639')).toBe('alpha-f103639');
  });
});
