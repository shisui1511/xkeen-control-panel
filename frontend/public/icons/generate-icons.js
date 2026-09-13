// Генерирует PWA-иконки из фирменной монограммы X через Playwright (уже используется в проекте для e2e).
// Запуск: node public/icons/generate-icons.js
import { chromium } from 'playwright';
import { fileURLToPath } from 'node:url';
import path from 'node:path';

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const SIZES = [72, 96, 128, 144, 152, 192, 384, 512];

const svg = (size) => `<!doctype html><html><head><style>
html,body{margin:0;padding:0}
</style></head><body>
<svg width="${size}" height="${size}" viewBox="0 0 100 100" xmlns="http://www.w3.org/2000/svg">
  <rect width="100" height="100" fill="#0c2237" rx="22" />
  <path d="M30 30L70 70M70 30L30 70" stroke="white" stroke-width="14" stroke-linecap="round" />
</svg>
</body></html>`;

async function main() {
  const browser = await chromium.launch();
  const page = await browser.newPage();
  for (const size of SIZES) {
    await page.setViewportSize({ width: size, height: size });
    await page.setContent(svg(size));
    await page.locator('svg').screenshot({
      path: path.join(__dirname, `icon-${size}x${size}.png`)
    });
  }
  await browser.close();
  console.log(`Сгенерировано иконок: ${SIZES.length}`);
}

main();
