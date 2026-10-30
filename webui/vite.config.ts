import tailwindcss from '@tailwindcss/vite';
import { sveltekit } from '@sveltejs/kit/vite';
import { defineConfig } from 'vite';

export default defineConfig({
  plugins: [tailwindcss(), sveltekit()],
  cacheDir: '../dist/.cache/vite',
  build: { emptyOutDir: true },
  // The generated SSR code runs outside webui's dependency directory.
  ssr: { noExternal: true },
});
