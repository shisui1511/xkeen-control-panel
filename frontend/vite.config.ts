import { defineConfig } from 'vite';
import { svelte } from '@sveltejs/vite-plugin-svelte';

export default defineConfig({
  plugins: [svelte()],
  base: './',
  optimizeDeps: {
    // Пакеты грузятся только динамическим import() внутри passwordStrength.ts
    // (D-18); включение сюда лишь стабилизирует dev-сервер Vite при первом
    // ленивом импорте (без этого он перезагружает страницу посреди
    // Playwright-теста) — на состав production-сборки не влияет.
    // CodeMirror, markdown-it и xterm тоже подгружаются только через import()
    // в шаблонах Svelte, сканер Vite их не видит, и первый заход на редактор
    // или терминал перезагружал страницу посреди теста.
    include: [
      '@zxcvbn-ts/core',
      '@zxcvbn-ts/language-common',
      '@zxcvbn-ts/language-en',
      '@zxcvbn-ts/language-ru',
      '@xterm/xterm',
      '@xterm/addon-fit',
      '@codemirror/state',
      '@codemirror/view',
      '@codemirror/autocomplete',
      '@codemirror/commands',
      '@codemirror/lang-json',
      '@codemirror/lang-yaml',
      '@codemirror/language',
      '@codemirror/lint',
      '@codemirror/search',
      '@lezer/highlight',
      'codemirror-json-schema',
      'codemirror-json-schema/yaml',
      'markdown-it'
    ]
  },
  build: {
    outDir: 'dist',
    emptyOutDir: true,
    manifest: true,
    rollupOptions: {
      output: {
        manualChunks: undefined
      }
    }
  },
  server: {
    port: 5173,
    // Лениво загружаемые страницы и разделы конструктора грузятся через
    // {#await import(...)}, поэтому Vite видит их только при первом заходе
    // браузера: на холодном dev-сервере 4 параллельных воркера Playwright
    // одновременно трансформируют сотни модулей, и первая отрисовка
    // не укладывается в 20 с (стресс 136-17). Предварительная трансформация
    // при старте переносит эту работу до первого теста.
    warmup: {
      clientFiles: [
        './src/Editor.svelte',
        './src/Constructor.svelte',
        './src/XrayRoutingConstructor.svelte',
        './src/MihomoGenerator.svelte',
        './src/Services.svelte',
        './src/Traffic.svelte'
      ]
    },
    proxy: {
      // D-12: панель отвечает только по HTTPS (134-06); dev-сервер на ПК не
      // запускается (CLAUDE.md — службы только на роутере), правка нужна
      // лишь для согласованности конфига с HTTPS-only панелью.
      '/api': {
        target: 'https://localhost:8090',
        changeOrigin: true,
        secure: false
      }
    }
  },
  test: {
    include: [
      'src/**/*.test.{js,mjs,cjs,ts,mts,cts,jsx,tsx}',
      'tests/**/*.test.{js,mjs,cjs,ts,mts,cts,jsx,tsx}'
    ],
    coverage: {
      provider: 'v8',
      reporter: ['text', 'json', 'html']
    }
  }
});
