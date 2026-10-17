import { defineConfig } from 'vite';

// A API (REST + SSE) é o servidor Go (cmd/mdd). Em desenvolvimento, o Vite repassa /api para ele.
const api = process.env.MDD_API ?? 'http://localhost:8080';

export default defineConfig({
  server: {
    proxy: { '/api': { target: api, changeOrigin: true } },
    // No WSL, arquivos em /mnt/c não geram eventos de mudança; polling resolve.
    watch: { usePolling: Boolean(process.env.WSL_DISTRO_NAME) },
  },
});
