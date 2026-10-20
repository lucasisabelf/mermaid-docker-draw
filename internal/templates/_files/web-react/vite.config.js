import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';

export default defineConfig({
  plugins: [react()],
  // O site é aberto pela porta publicada no host (ex.: 8085 → 80): aceita qualquer nome de host.
  server: { allowedHosts: true },
  preview: { allowedHosts: true },
});
