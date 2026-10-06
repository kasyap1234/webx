// @ts-check
import { defineConfig } from 'astro/config';
import starlight from '@astrojs/starlight';
import starlightLlmsTxt from 'starlight-llms-txt';

// https://astro.build/config
export default defineConfig({
	// canonical docs origin — update when the real domain is chosen
	site: 'https://docs.webx.dev',
	integrations: [
		starlight({
			title: 'webx',
			description:
				'Docs for webx — the self-hosted web toolkit for AI coding agents. Scrape, search, crawl, index, extract, and serve. One static binary.',
			favicon: '/favicon.svg',
			logo: {
				src: './src/assets/webx-mark.svg',
				alt: 'webx',
				replacesTitle: false,
			},
			social: [
				{ icon: 'github', label: 'GitHub', href: 'https://github.com/kasyap1234/webx' },
			],
			editLink: {
				baseUrl: 'https://github.com/kasyap1234/webx/edit/main/docs-site/',
			},
			customCss: ['./src/styles/custom.css'],
			components: {
				// keep all stock components — nothing overridden yet
			},
			plugins: [
				starlightLlmsTxt({
					projectName: 'webx',
					description:
						'webx is a single-binary web toolkit for AI coding agents: scrape/search/crawl/index/extract/serve, MCP server, and Firecrawl-compatible HTTP API. MIT licensed, self-hosted.',
					promote: ['index', 'start/*', 'cli/*', 'api/*'],
					demote: ['comparisons/*'],
				}),
			],
			sidebar: [
				{
					label: 'Start here',
					items: [
						{ label: 'Installation', slug: 'start/installation' },
						{ label: 'Quickstart', slug: 'start/quickstart' },
						{ label: 'Configuration', slug: 'start/configuration' },
					],
				},
				{
					label: 'Guides',
					items: [
						{ label: 'Fetch tiers & rendering', slug: 'guides/rendering' },
						{ label: 'Private index & semantic search', slug: 'guides/private-index' },
						{ label: 'Agents, MCP & skills', slug: 'guides/agents' },
						{ label: 'Self-hosting webxd', slug: 'guides/self-hosting' },
						{ label: 'Licenses & tiers', slug: 'guides/license' },
					],
				},
				{
					label: 'CLI reference',
					items: [
						{ label: 'Command map', slug: 'cli' },
						{ label: 'Fetch commands', slug: 'cli/fetch' },
						{ label: 'Search & answer', slug: 'cli/search' },
						{ label: 'Index & watch', slug: 'cli/indexing' },
						{ label: 'Agent commands', slug: 'cli/agents' },
						{ label: 'Server & admin', slug: 'cli/server' },
					],
				},
				{
					label: 'HTTP API',
					items: [
						{ label: 'Server overview', slug: 'api' },
						{ label: 'Firecrawl compatibility', slug: 'api/firecrawl' },
					],
				},
				{
					label: 'SDKs',
					items: [
						{ label: 'Python', slug: 'sdks/python' },
						{ label: 'JavaScript', slug: 'sdks/javascript' },
					],
				},
				{
					label: 'Comparisons',
					items: [
						{ label: 'vs Firecrawl', slug: 'comparisons/vs-firecrawl' },
						{ label: 'vs Crawl4AI', slug: 'comparisons/vs-crawl4ai' },
						{ label: 'vs search APIs', slug: 'comparisons/vs-search-apis' },
					],
				},
			],
		}),
	],
});
