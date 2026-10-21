import { defineConfig } from 'vitest/config';
import react from '@vitejs/plugin-react';

export default defineConfig({
  plugins: [react({ fastRefresh: false })],
  test: {
    environment: 'jsdom',
    // Vitest 0.34 accepts local environment paths through match globs.
    environmentMatchGlobs: [['**/*', './src/test/environment.ts']],
    setupFiles: ['./src/test/setup.ts'],
    clearMocks: true,
    css: { include: [/src\/styles\/.*\.css/] },
    threads: false,
    coverage: {
      provider: 'v8',
      include: ['src/hooks/**/*.{ts,tsx}', 'src/providers/**/*.tsx', 'src/pages/**/*.tsx', 'src/components/**/*.tsx', 'src/settings.ts'],
      reporter: ['text', 'json-summary'],
      reportsDirectory: './coverage',
    },
  },
});
