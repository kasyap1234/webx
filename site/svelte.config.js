import adapter from '@sveltejs/adapter-static';
import { vitePreprocess } from '@sveltejs/vite-plugin-svelte';

/** @type {import('@sveltejs/kit').Config} */
const config = {
	preprocess: vitePreprocess(),
	kit: {
		adapter: adapter({
			pages: 'build',
			assets: 'build',
			precompress: false,
			strict: true
		}),
		// GitHub Pages serves the repo site at /<repo> — BASE_PATH is empty on
		// a custom domain, '/webx' on kasyap1234.github.io/webx.
		paths: { base: process.env.BASE_PATH || '' }
	}
};

export default config;
