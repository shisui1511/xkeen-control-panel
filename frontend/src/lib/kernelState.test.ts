import { describe, it, expect } from 'vitest';
import { kernelStateOf, kernelNameOf, kernelLabel, runningKernelsOf } from './kernelState';

describe('kernelStateOf', () => {
  it('null и undefined — состояние неизвестно', () => {
    expect(kernelStateOf(null)).toBeNull();
    expect(kernelStateOf(undefined)).toBeNull();
  });

  it('xray и mihomo возвращаются как есть', () => {
    expect(kernelStateOf({ active_kernel: 'xray' })).toBe('xray');
    expect(kernelStateOf({ active_kernel: 'mihomo' })).toBe('mihomo');
  });

  it('none, пустая и неизвестная строка — none', () => {
    expect(kernelStateOf({ active_kernel: 'none' })).toBe('none');
    expect(kernelStateOf({ active_kernel: '' })).toBe('none');
    expect(kernelStateOf({ active_kernel: 'unknown' })).toBe('none');
    expect(kernelStateOf({})).toBe('none');
  });

  it('both без флага — конфликт', () => {
    expect(kernelStateOf({ active_kernel: 'both' })).toBe('conflict');
  });

  it('флаг kernel_conflict побеждает active_kernel', () => {
    expect(kernelStateOf({ active_kernel: 'xray', kernel_conflict: true })).toBe('conflict');
    expect(kernelStateOf({ active_kernel: 'xray', kernel_conflict: false })).toBe('xray');
  });

  it('для каждого известного ввода ровно одно из четырёх значений', () => {
    const inputs = ['xray', 'mihomo', 'none', '', 'unknown', 'both'];
    const states = ['xray', 'mihomo', 'none', 'conflict'];
    for (const active_kernel of inputs) {
      const s = kernelStateOf({ active_kernel });
      expect(states.filter((x) => x === s)).toHaveLength(1);
    }
  });
});

describe('kernelNameOf и kernelLabel', () => {
  it('имя только у единственного активного ядра', () => {
    expect(kernelNameOf('xray')).toBe('xray');
    expect(kernelNameOf('mihomo')).toBe('mihomo');
    expect(kernelNameOf('conflict')).toBe('');
    expect(kernelNameOf('none')).toBe('');
    expect(kernelNameOf(null)).toBe('');
  });

  it('подпись в регистре бренда', () => {
    expect(kernelLabel('xray')).toBe('Xray');
    expect(kernelLabel('mihomo')).toBe('Mihomo');
    expect(kernelLabel('other')).toBe('other');
  });
});

describe('runningKernelsOf', () => {
  it('берёт поле running_kernels в порядке xray, mihomo и отбрасывает чужие имена', () => {
    expect(runningKernelsOf({ running_kernels: ['mihomo', 'xray'] })).toEqual(['xray', 'mihomo']);
    expect(runningKernelsOf({ running_kernels: ['mihomo', 'foo'] })).toEqual(['mihomo']);
    expect(runningKernelsOf({ running_kernels: [] })).toEqual([]);
  });

  it('при конфликте без поля — оба ядра', () => {
    expect(runningKernelsOf({ active_kernel: 'both', kernel_conflict: true })).toEqual([
      'xray',
      'mihomo'
    ]);
  });

  it('без конфликта и без поля — пусто', () => {
    expect(runningKernelsOf({ active_kernel: 'xray' })).toEqual([]);
    expect(runningKernelsOf(null)).toEqual([]);
  });
});
