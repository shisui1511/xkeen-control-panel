import { defineConfig, devices } from '@playwright/test';

// Playwright против настоящей панели роутера (набор router-test, этап pw).
// Запускается scripts/router/pw-suite.sh, по одному процессу на цель. Переменные:
//   XCP_URL        адрес панели цели (https, самоподписанный сертификат)
//   XCP_ARCH       архитектура цели, имя проекта
//   XCP_PW_STATE   файл storageState (создаёт global-setup одним входом)
//   XCP_PW_OUT     каталог трасс и снимков
//   XCP_PW_JSON    файл JSON-отчёта
// Конфиг не падает без переменных (нужно для `--list`): их наличие проверяет global-setup.
// Сервер не поднимается и сеть не подменяется: тесты видят то же, что браузер пользователя.
export default defineConfig({
  testDir: './tests/router',
  testMatch: '**/*.spec.ts',
  tsconfig: './tsconfig.test.json',
  // Сценарии меняют общее состояние устройства: внутри цели всё строго по очереди
  fullyParallel: false,
  workers: 1,
  // Повторов нет: любое падение — находка (метка известного падения или починка)
  retries: 0,
  // Лимит теста учитывает медленные устройства (XCP_SLOW из локального конфига цели)
  timeout: 90_000 * (Number(process.env.XCP_SLOW) > 0 ? Number(process.env.XCP_SLOW) : 1),
  forbidOnly: true,
  globalSetup: './tests/router/global-setup.ts',
  outputDir: process.env.XCP_PW_OUT || './test-results-router',
  reporter: [
    ['list'],
    ['json', { outputFile: process.env.XCP_PW_JSON || 'test-results-router.json' }]
  ],
  use: {
    baseURL: process.env.XCP_URL,
    // Панель отдаётся только по HTTPS, сертификат самоподписанный
    ignoreHTTPSErrors: true,
    storageState: process.env.XCP_PW_STATE,
    trace: 'retain-on-failure',
    screenshot: 'only-on-failure'
  },
  projects: [
    {
      name: process.env.XCP_ARCH || 'router',
      use: { ...devices['Desktop Chrome'] }
    }
  ]
});
