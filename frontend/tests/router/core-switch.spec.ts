// e2e-pages: services
import { test, expect } from '@playwright/test';
import { attachConsoleGuard } from './lib/console';
import { RT, T } from './lib/env';

// Матрица ядер (D-20): в начале каждой ветви scripts/router/run.sh просит этот спек
// переключить устройство на ядро XCP_WANT_CORE (pw-suite.sh --switch). Переключение
// делается так, как его делает пользователь: страница «Службы», карточка ядра,
// подтверждение в диалоге. Прямого вызова API здесь нет: API читается только для
// сверки результата с процессами устройства.
//
// Без XCP_WANT_CORE спек тестов не регистрирует: обычный прогон набора на текущем
// ядре его пропускает.

const WANT = RT.wantCore;

interface KernelStatus {
  active_kernel?: string;
  running_kernels?: string[];
  kernel_conflict?: boolean;
}

if (WANT === 'xray' || WANT === 'mihomo') {
  const LABEL = WANT === 'xray' ? 'Xray' : 'Mihomo';

  test(`core-switch:${WANT}`, async ({ page }) => {
    test.setTimeout(T.switchKernel + 3 * T.page);
    const guard = attachConsoleGuard(page);

    await page.setViewportSize({ width: 1440, height: 900 });
    await page.goto('/#/services');

    const group = page.getByRole('radiogroup').first();
    await expect(group).toBeVisible({ timeout: T.page });
    const card = group.locator('.core-radio-card[role="radio"]').filter({ hasText: LABEL });
    await expect(card).toBeVisible({ timeout: T.page });

    // Активное ядро по интерфейсу: ждём, пока статус перестанет быть «неизвестно».
    // Если ни одна карточка не отмечена (ядро остановлено), переключение всё равно нужно.
    await page
      .waitForFunction(
        () => document.querySelectorAll('.core-radio-card[aria-checked="true"]').length > 0,
        undefined,
        { timeout: T.action }
      )
      .catch(() => {});

    const alreadyActive = (await card.getAttribute('aria-checked')) === 'true';
    if (!alreadyActive) {
      await card.click();
      const dialog = page.getByRole('dialog');
      await expect(dialog).toBeVisible({ timeout: T.action });
      // кнопки диалога: «Отмена», затем подтверждение
      await dialog.locator('.confirm-actions button').last().click();
    }

    // Интерфейс показывает нужное ядро активным и переключение закончено
    await expect(card).toHaveAttribute('aria-checked', 'true', { timeout: T.switchKernel });
    await expect(group).not.toHaveAttribute('aria-busy', 'true', { timeout: T.switchKernel });

    // Сверка с устройством: ровно одно ядро запущено, и это нужное
    await expect
      .poll(
        async () => {
          const res = await page.request.get('/api/service/status');
          if (!res.ok()) return `http ${res.status()}`;
          const body = (await res.json()) as { data?: KernelStatus };
          const d = body.data ?? {};
          if (d.kernel_conflict) return 'conflict';
          return `${d.active_kernel}:${(d.running_kernels ?? []).join(',')}`;
        },
        {
          message: `устройство не пришло к ядру ${WANT}`,
          timeout: T.switchKernel,
          intervals: [2000, 5000]
        }
      )
      .toBe(`${WANT}:${WANT}`);

    await guard.assertClean();
  });
}
