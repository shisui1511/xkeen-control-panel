import { describe, it, expect, beforeEach } from 'vitest';
import { capabilities } from '../../stores';
import { ProvidersState, type Subscription } from './providersState.svelte';

function sub(over: Partial<Subscription>): Subscription {
  return { enable_xray: false, enable_mihomo: false, ...over } as Subscription;
}

function setKernel(active: string, conflict = false) {
  capabilities.set({ active_kernel: active, kernel_conflict: conflict } as never);
}

describe('ProvidersState.getNodeSource — источник узлов подписки', () => {
  let state: ProvidersState;

  beforeEach(() => {
    state = new ProvidersState();
    capabilities.set(null);
  });

  it('один флаг определяет источник независимо от активного ядра', () => {
    setKernel('xray');
    expect(state.getNodeSource(sub({ enable_mihomo: true }))).toBe('mihomo');
    setKernel('mihomo');
    expect(state.getNodeSource(sub({ enable_xray: true }))).toBe('xray');
  });

  it('оба флага при активном Mihomo — mihomo', () => {
    setKernel('mihomo');
    expect(state.getNodeSource(sub({ enable_xray: true, enable_mihomo: true }))).toBe('mihomo');
  });

  it.each([
    ['xray', false],
    ['none', false],
    ['both', true]
  ])('оба флага при active_kernel=%s (конфликт=%s) — xray', (active, conflict) => {
    setKernel(active, conflict);
    expect(state.getNodeSource(sub({ enable_xray: true, enable_mihomo: true }))).toBe('xray');
  });

  it('оба флага до ответа capabilities — xray', () => {
    expect(state.getNodeSource(sub({ enable_xray: true, enable_mihomo: true }))).toBe('xray');
  });
});
