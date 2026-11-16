// Общий тик «возраста»: одно значение на страницу вместо таймера у каждой плитки.
// Таймер идёт, пока на стор кто-то подписан, и останавливается с последней отпиской.
import { readable } from 'svelte/store';

export const AGE_TICK_MS = 30_000;

export const ageTick = readable(0, (set) => {
  let n = 0;
  const timer = setInterval(() => set(++n), AGE_TICK_MS);
  return () => clearInterval(timer);
});
