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
    include: [
      '@zxcvbn-ts/core',
      '@zxcvbn-ts/language-common',
      '@zxcvbn-ts/language-en',
      '@zxcvbn-ts/language-ru'
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
    proxy: {
      '/api': {
        target: 'http://localhost:8090',
        changeOrigin: true
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
