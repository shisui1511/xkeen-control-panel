import { describe, it, expect } from 'vitest';
import {
  NAV_CAPS_KEY,
  NAV_CAPS_VERSION,
  anyKernelInstalled,
  mergeNavCaps,
  parseNavCaps,
  showMihomoNavFor,
  toNavCaps,
  type NavCaps
} from './navCaps';

const nav = (active: NavCaps['active_kernel'], xray = true, mihomo = false): NavCaps => ({
  v: 1,
  active_kernel: active,
  kernels: { xray, mihomo }
});

describe('константы', () => {
  it('ключ хранилища и версия формата', () => {
    expect(NAV_CAPS_KEY).toBe('xcp_nav_caps');
    expect(NAV_CAPS_VERSION).toBe(1);
  });
});

describe('parseNavCaps', () => {
  it('принимает корректный срез', () => {
    const r = parseNavCaps('{"v":1,"active_kernel":"xray","kernels":{"xray":true,"mihomo":false}}');
    expect(r).toEqual({ v: 1, active_kernel: 'xray', kernels: { xray: true, mihomo: false } });
  });

  it('сохраняет xkeen_installed, если он boolean', () => {
    const r = parseNavCaps(
      '{"v":1,"active_kernel":"none","kernels":{"xray":false,"mihomo":false},"xkeen_installed":true}'
    );
    expect(r?.xkeen_installed).toBe(true);
  });

  it('иная версия формата → null', () => {
    expect(
      parseNavCaps('{"v":2,"active_kernel":"xray","kernels":{"xray":true,"mihomo":false}}')
    ).toBeNull();
  });

  it('active_kernel вне xray|mihomo|none → null', () => {
    expect(
      parseNavCaps('{"v":1,"active_kernel":"both","kernels":{"xray":true,"mihomo":false}}')
    ).toBeNull();
  });

  it('kernels.* не boolean → null', () => {
    expect(
      parseNavCaps('{"v":1,"active_kernel":"xray","kernels":{"xray":"yes","mihomo":false}}')
    ).toBeNull();
    expect(parseNavCaps('{"v":1,"active_kernel":"xray"}')).toBeNull();
  });

  it('битый JSON, null, пустая строка, не объект → null', () => {
    expect(parseNavCaps('{oops')).toBeNull();
    expect(parseNavCaps(null)).toBeNull();
    expect(parseNavCaps('')).toBeNull();
    expect(parseNavCaps('42')).toBeNull();
    expect(parseNavCaps('null')).toBeNull();
  });
});

describe('toNavCaps', () => {
  const caps = {
    active_kernel: 'mihomo',
    kernels: { xray: { installed: false, version: '1' }, mihomo: { installed: true } },
    xkeen_installed: true,
    mihomo: { discovered_secret: 'S3cr3t', api_url: 'http://x' },
    global_hwid: 'HWID-VALUE'
  };

  it('переносит только active_kernel, kernels.*.installed и xkeen_installed', () => {
    const r = toNavCaps(caps);
    expect(r).toEqual({
      v: 1,
      active_kernel: 'mihomo',
      kernels: { xray: false, mihomo: true },
      xkeen_installed: true
    });
    expect(Object.keys(r!).sort()).toEqual(['active_kernel', 'kernels', 'v', 'xkeen_installed']);
  });

  it('секреты и идентификаторы не попадают в сериализацию', () => {
    const json = JSON.stringify(toNavCaps(caps));
    expect(json).not.toContain('S3cr3t');
    expect(json).not.toContain('HWID-VALUE');
    expect(json).not.toContain('discovered_secret');
    expect(json).not.toContain('global_hwid');
  });

  it('без kernels → null', () => {
    expect(toNavCaps({ active_kernel: 'xray' })).toBeNull();
    expect(toNavCaps(null)).toBeNull();
  });

  it('отсутствующее ядро в kernels считается не установленным', () => {
    expect(
      toNavCaps({ active_kernel: 'xray', kernels: { xray: { installed: true } } })?.kernels
    ).toEqual({ xray: true, mihomo: false });
  });

  it('неизвестный active_kernel → null', () => {
    expect(toNavCaps({ active_kernel: 'weird', kernels: {} })).toBeNull();
  });
});

describe('mergeNavCaps', () => {
  it('none у нового ответа не затирает прежнее активное ядро', () => {
    const r = mergeNavCaps(nav('xray'), nav('none', false, false));
    expect(r.active_kernel).toBe('xray');
    expect(r.kernels).toEqual({ xray: false, mihomo: false });
  });

  it('без прежнего значения none остаётся none', () => {
    expect(mergeNavCaps(null, nav('none')).active_kernel).toBe('none');
  });

  it('явная смена ядра применяется', () => {
    expect(mergeNavCaps(nav('xray'), nav('mihomo')).active_kernel).toBe('mihomo');
  });
});

describe('showMihomoNavFor', () => {
  it('нет среза → null', () => {
    expect(showMihomoNavFor(null)).toBeNull();
  });

  it('xray → false; mihomo и none → true', () => {
    expect(showMihomoNavFor(nav('xray'))).toBe(false);
    expect(showMihomoNavFor(nav('mihomo'))).toBe(true);
    expect(showMihomoNavFor(nav('none'))).toBe(true);
  });
});

describe('anyKernelInstalled', () => {
  it('capabilities неизвестны → null', () => {
    expect(anyKernelInstalled(null)).toBeNull();
    expect(anyKernelInstalled(undefined)).toBeNull();
    expect(anyKernelInstalled({})).toBeNull();
  });

  it('пустой kernels → null, а не «нет ядер»', () => {
    expect(anyKernelInstalled({ kernels: {} })).toBeNull();
  });

  it('ни одно не установлено → false', () => {
    expect(
      anyKernelInstalled({ kernels: { xray: { installed: false }, mihomo: { installed: false } } })
    ).toBe(false);
  });

  it('хотя бы одно установлено → true', () => {
    expect(
      anyKernelInstalled({ kernels: { xray: { installed: false }, mihomo: { installed: true } } })
    ).toBe(true);
  });
});
