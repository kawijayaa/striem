import { defineConfig } from 'vite';
import { fileURLToPath } from 'node:url';
import tailwindcss from '@tailwindcss/vite';

export default defineConfig({
  root: 'web',
  plugins: [tailwindcss()],
  build: {
    outDir: 'dist',
    emptyOutDir: true,
    rolldownOptions: {
      input: { main: fileURLToPath(new URL('./web/index.html', import.meta.url)),
        kusto: fileURLToPath(new URL('./web/kusto.html', import.meta.url)) },
    },
  },
  server: {
    proxy: {
      '/api': {
        target: 'http://localhost:8080',
        changeOrigin: false,
        configure(proxy) {
          proxy.on('proxyReq', (proxyRequest, request) => {
            if (request.headers.host) proxyRequest.setHeader('Host', request.headers.host);
          });
        },
      },
    },
  },
});
