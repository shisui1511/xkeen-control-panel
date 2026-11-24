import type { Page } from '@playwright/test';
import AxeBuilder from '@axe-core/playwright';
import { settleMotion } from './motion';

/**
 * Критичные и серьёзные нарушения axe (WCAG 2.0/2.1 A и AA, в том числе color-contrast)
 * внутри области. Перед проверкой ждёт конца конечных анимаций (модалка, раскрытие
 * карточки, смена вкладки): axe не должен видеть полупрозрачный кадр, а фиксированная
 * пауза на медленном стенде могла не хватить.
 */
export async function axeSevere(page: Page, scope: string): Promise<string[]> {
  // Анимация, запущенная последним действием, появляется в списке на следующем кадре
  await page.evaluate(
    () =>
      new Promise<void>((done) => requestAnimationFrame(() => requestAnimationFrame(() => done())))
  );
  await settleMotion(page);
  const scan = await new AxeBuilder({ page })
    .withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa'])
    .include(scope)
    .analyze();
  return scan.violations
    .filter((v) => v.impact === 'critical' || v.impact === 'serious')
    .map((v) => {
      const nodes = v.nodes
        .slice(0, 3)
        .map((n) => `${n.target.join(' ')} [${n.any[0]?.message ?? ''}]`);
      return `${v.id} (${v.impact}, узлов ${v.nodes.length}): ${nodes.join(' | ')}`;
    });
}
