import type { Page } from '@playwright/test';

/**
 * Ждёт конца конечных анимаций страницы (появление диалога и листа): размеры, снятые
 * посреди масштабирования, на доли пикселя меньше настоящих и дают ложное «ниже 44 px».
 * Бесконечные анимации (индикаторы загрузки) не ждутся.
 */
export async function settleMotion(page: Page): Promise<void> {
  await page.evaluate(() =>
    Promise.all(
      document
        .getAnimations()
        .filter((a) => Number.isFinite(a.effect?.getComputedTiming().endTime ?? 0))
        .map((a) => a.finished.catch(() => undefined))
    ).then(() => undefined)
  );
}
