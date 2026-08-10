import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';

// https://vite.dev/config/
export default defineConfig({
  plugins: [react()],
  resolve: {
    tsconfigPaths: true,
  },
  server: {
    // У бэкенда нет CORS-миддлвари (AGENTS.md): в деве проксируем запросы,
    // чтобы браузер видел same-origin вместо кросс-доменных.
    proxy: {
      '/api': { target: 'http://localhost:8080', changeOrigin: true },
      '/debug': { target: 'http://localhost:8080', changeOrigin: true },
      '/ws': { target: 'http://localhost:8080', changeOrigin: true, ws: true },
    },
  },
});
