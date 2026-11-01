/* eslint-disable */
// Генерирует PNG-иконки PWA из public/favicon.svg (знак XKeen).
// Запуск вручную после правки знака: `node scripts/gen-icons.cjs`.
// Нужен Chromium от Playwright (devDependency @playwright/test).
//   icon-NxN.png          — purpose "any": плитка со скруглением, прозрачные углы
//   icon-maskable-NxN.png — purpose "maskable": фон в край, знак в безопасной зоне 80 %
//   apple-touch-icon.png  — 180×180 без прозрачности, iOS скругляет сам
const fs = require('fs');
const path = require('path');
const { chromium } = require('@playwright/test');

const PUBLIC = path.join(__dirname, '../public');
const OUT = path.join(PUBLIC, 'icons');
const svg = fs.readFileSync(path.join(PUBLIC, 'favicon.svg'), 'utf8');

// Полноформатный вариант: та же плитка без скругления, знак мельче
const fullBleed = (scale) =>
  svg.replace('rx="7"', 'rx="0"').replace('scale(0.72)', `scale(${scale})`);

const ANY = [72, 96, 128, 144, 152, 192, 384, 512];
const MASKABLE = [192, 512];

async function render(page, markup, size, file, transparent) {
  await page.setViewportSize({ width: size, height: size });
  await page.setContent(
    `<html><body style="margin:0;background:transparent">` +
      markup.replace('<svg ', `<svg width="${size}" height="${size}" `) +
      `</body></html>`
  );
  await page.screenshot({ path: path.join(OUT, file), omitBackground: transparent });
}

(async () => {
  // CHROMIUM_PATH — свой бинарник, если версия браузеров Playwright не совпадает
  const browser = await chromium.launch(
    process.env.CHROMIUM_PATH ? { executablePath: process.env.CHROMIUM_PATH } : {}
  );
  const page = await browser.newPage({ deviceScaleFactor: 1 });
  for (const s of ANY) await render(page, svg, s, `icon-${s}x${s}.png`, true);
  for (const s of MASKABLE)
    await render(page, fullBleed(0.56), s, `icon-maskable-${s}x${s}.png`, false);
  await render(page, fullBleed(0.62), 180, 'apple-touch-icon.png', false);
  await browser.close();
  console.log('icons written to', OUT);
})();
