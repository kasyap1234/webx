import { sveltekit } from '@sveltejs/kit/vite';
import { defineConfig } from 'vite';

export default defineConfig({
	plugins: [sveltekit()],
	build: {
		// three.js is only ever loaded via dynamic import() inside onMount,
		// so it lands in its own lazy chunk and never touches SSR or LCP.
		chunkSizeWarningLimit: 900
	}
});
