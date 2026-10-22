import { defineConfig } from 'vitest/config';
import preact from '@preact/preset-vite';

// The build goes straight into the Go server's embed folder. emptyOutDir is off because that folder holds a
// committed .gitkeep; scripts/clean-dist.mjs clears the old build instead.
export default defineConfig({
  plugins: [preact()],
  base: './',
  build: { outDir: '../server/internal/webui/dist', emptyOutDir: false },
  server: { proxy: { '/api': 'http://localhost:8080' } },
  test: { environment: 'node' },
});
