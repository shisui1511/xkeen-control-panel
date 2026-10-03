import { describe, it, expect } from 'vitest';
import { kernelGateMessage, readKernelGateMeta, KERNEL_GATE_CODES } from './kernelGateError';

// Переводчик-заглушка: ключ и параметры, чтобы проверять выбор ключа и подстановку
const tr = (key: string, params?: Record<string, string>) =>
  params ? `${key}|${JSON.stringify(params)}` : key;

function response(status: number, body: unknown, broken = false): Response {
  return {
    status,
    clone: () => ({
      status,
      json: () => (broken ? Promise.reject(new Error('bad json')) : Promise.resolve(body))
    })
  } as unknown as Response;
}

describe('kernelGateMessage', () => {
  it('активно другое ядро: kernel.inactive_wrong с подписями брендов', () => {
    expect(
      kernelGateMessage({ code: 'kernel_inactive', required: 'mihomo', active: 'xray' }, tr)
    ).toBe('kernel.inactive_wrong|{"active":"Xray","required":"Mihomo"}');
  });

  it('ядро не запущено: kernel.inactive_none с нужным ядром', () => {
    expect(
      kernelGateMessage({ code: 'kernel_inactive', required: 'xray', active: 'none' }, tr)
    ).toBe('kernel.inactive_none|{"required":"Xray"}');
  });

  it('active отсутствует или неизвестен — как none', () => {
    expect(kernelGateMessage({ code: 'kernel_inactive', required: 'xray' }, tr)).toBe(
      'kernel.inactive_none|{"required":"Xray"}'
    );
    expect(
      kernelGateMessage({ code: 'kernel_inactive', required: 'xray', active: '<b>x</b>' }, tr)
    ).toBe('kernel.inactive_none|{"required":"Xray"}');
  });

  it('active both и kernel_conflict — тост о конфликте', () => {
    expect(
      kernelGateMessage({ code: 'kernel_inactive', required: 'mihomo', active: 'both' }, tr)
    ).toBe('kernel.conflict_blocked_toast');
    expect(kernelGateMessage({ code: 'kernel_conflict' }, tr)).toBe(
      'kernel.conflict_blocked_toast'
    );
  });

  it('kernel_op_in_progress — kernel.op_in_progress', () => {
    expect(kernelGateMessage({ code: 'kernel_op_in_progress' }, tr)).toBe('kernel.op_in_progress');
  });

  it('чужое значение required, неизвестный код и null — null', () => {
    expect(
      kernelGateMessage({ code: 'kernel_inactive', required: '<img>', active: 'xray' }, tr)
    ).toBe(null);
    expect(kernelGateMessage({ code: 'kernel_inactive', active: 'xray' }, tr)).toBeNull();
    expect(kernelGateMessage({ code: 'name_exists' }, tr)).toBeNull();
    expect(kernelGateMessage({}, tr)).toBeNull();
    expect(kernelGateMessage(null, tr)).toBeNull();
    expect(kernelGateMessage(undefined, tr)).toBeNull();
  });

  it('значения с сервера в текст не попадают как есть', () => {
    const msg = kernelGateMessage(
      { code: 'kernel_inactive', required: 'mihomo', active: '<script>alert(1)</script>' },
      tr
    );
    expect(msg).not.toContain('<script>');
  });
});

describe('readKernelGateMeta', () => {
  it('409 с кодом из белого списка — мета с полями-строками', async () => {
    const meta = await readKernelGateMeta(
      response(409, { code: 'kernel_inactive', required: 'mihomo', active: 'xray', error: 'x' })
    );
    expect(meta).toEqual({ code: 'kernel_inactive', required: 'mihomo', active: 'xray' });
  });

  it('все коды белого списка распознаются', async () => {
    for (const code of KERNEL_GATE_CODES) {
      expect(await readKernelGateMeta(response(409, { code }))).toEqual({ code });
    }
  });

  it('409 без кода или с чужим кодом — null', async () => {
    expect(await readKernelGateMeta(response(409, { error: 'busy' }))).toBeNull();
    expect(await readKernelGateMeta(response(409, { code: 'name_exists' }))).toBeNull();
  });

  it('не 409 — null, даже с кодом', async () => {
    expect(await readKernelGateMeta(response(500, { code: 'kernel_inactive' }))).toBeNull();
  });

  it('битое тело — null', async () => {
    expect(await readKernelGateMeta(response(409, null, true))).toBeNull();
    expect(await readKernelGateMeta(response(409, 'text'))).toBeNull();
  });
});
