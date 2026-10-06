/**
 * Single source of truth for page content.
 * Every fact here traces back to the product brief — no invented numbers.
 */

export const REPO_URL = 'https://github.com/kasyap1234/webx';
// Starlight docs deployed under the Pages site's /docs path — swap to a
// real domain when one is chosen.
export const DOCS_URL = 'https://kasyap1234.github.io/webx/docs/';
export const INSTALL_CMD = 'go install github.com/kasyap1234/webx/cmd/webx@latest';
export const VERSION = 'v0.1.0';

/* ---------------- capability spec sheet ---------------- */

export interface Capability {
	cmd: string;
	desc: string;
	/** tiny right-column annotation, man-page style */
	flag: string;
}

export const CAPABILITIES: Capability[] = [
	{
		cmd: 'scrape',
		desc: 'page → clean markdown, 3-tier fetch, --fit token saver',
		flag: '3-tier'
	},
	{
		cmd: 'search',
		desc: '15-provider metasearch, weighted rrf, --scrape --highlights',
		flag: 'rrf'
	},
	{
		cmd: 'index',
		desc: 'sqlite fts5 + embeddings — your corpus, your box',
		flag: 'fts5+vec'
	},
	{
		cmd: 'query',
		desc: 'exa-style semantic search over your index',
		flag: 'offline'
	},
	{
		cmd: 'crawl',
		desc: 'goal-directed adaptive crawl, concurrency, robots-aware',
		flag: 'robots'
	},
	{
		cmd: 'extract',
		desc: 'css, schema.org, or llm via your endpoint (ollama ok)',
		flag: 'css+llm'
	},
	{
		cmd: 'serve',
		desc: 'http api, async jobs, webhooks, tenancy',
		flag: 'api+jobs'
	},
	{
		cmd: 'mcp',
		desc: '17-tool mcp server, stdio + sse',
		flag: '17 tools'
	}
];

/** shown in the capabilities left column as a help artifact */
export const HELP_HINT = {
	cmd: 'webx --help',
	out: 'scrape search index query\ncrawl extract serve mcp'
};

/* ---------------- pipeline diagram ---------------- */

export interface PipeNode {
	node: string;
	note: string;
}

export const PIPELINE: PipeNode[] = [
	{ node: 'url', note: 'any site' },
	{ node: 'scrape', note: '3-tier fetch' },
	{ node: 'md', note: 'trafilatura' },
	{ node: 'index', note: 'fts5+vec' },
	{ node: 'query', note: 'bm25+cos' }
];

/* ---------------- one binary / three bills ---------------- */

export interface Bill {
	name: string;
	price: string;
}

export const BILLS: Bill[] = [
	{ name: 'scraping api', price: '$83/mo' },
	{ name: 'search api', price: '$7/1k' },
	{ name: 'browser infra', price: '$0.10/hr' }
];

export const WEBX_BILL: Bill = { name: 'webx', price: '$0' };

/* ---------------- stats strip ---------------- */

export interface Stat {
	value: string;
	label: string;
	/** the two numbers that earn the accent */
	hot?: boolean;
}

export const STATS: Stat[] = [
	{ value: '15/15', label: 'js corpus', hot: true },
	{ value: '2.8×', label: 'tighter md' },
	{ value: '996ms', label: 'p50 scrape' },
	{ value: 'mit', label: 'license' }
];

/* ---------------- firecrawl compat ---------------- */

export const COMPAT_ROUTES = [
	'/v2/scrape',
	'/v2/crawl',
	'/v2/search',
	'/v2/map',
	'/v2/extract',
	'/v2/agent'
] as const;

export const COMPAT_DIFF = {
	context: 'app = FirecrawlApp(',
	before: '    api_key="fc-…"',
	after: '    api_url="http://localhost:8080"',
	end: ')'
};

/* ---------------- pricing ---------------- */

export interface PriceCol {
	name: string;
	price: string;
	per: string;
	tagline: string;
	includes?: string;
	features: string[];
	recommended?: boolean;
	cta: string;
	href: string;
}

export const PRICE_COLS: PriceCol[] = [
	{
		name: 'community',
		price: '$0',
		per: 'forever',
		tagline: 'for the self-hoster',
		features: ['all 16 commands', '3 api keys', 'community support', 'self-hosted'],
		cta: 'go install',
		href: REPO_URL
	},
	{
		name: 'pro',
		price: '$49',
		per: '/mo',
		recommended: true,
		tagline: 'for team deployments',
		includes: 'everything in community, plus:',
		features: ['unlimited api keys', 'rbac', 'audit log', 'priority support'],
		cta: 'get pro',
		href: `${REPO_URL}#pro`
	},
	{
		name: 'enterprise',
		price: 'contact',
		per: '',
		tagline: 'for scaled orgs',
		includes: 'everything in pro, plus:',
		features: ['per-deployment license', 'dedicated support', 'air-gap ok'],
		cta: 'talk to us',
		href: `${REPO_URL}/issues`
	}
];

export const PRICE_NOTE = 'the free tier is a product, not a demo.';

/* ---------------- footer ---------------- */

export interface FootCol {
	head: string;
	links: { label: string; href: string }[];
}

export const FOOT_COLS: FootCol[] = [
	{
		head: 'product',
		links: [
			{ label: 'docs', href: DOCS_URL },
			{ label: 'mcp server', href: `${REPO_URL}#mcp-server` },
			{ label: 'sdks', href: `${REPO_URL}/tree/main/sdk` },
			{ label: 'benchmarks', href: '#benchmarks' }
		]
	},
	{
		head: 'compare',
		links: [
			{ label: 'vs firecrawl', href: `${REPO_URL}/blob/main/docs/vs-firecrawl.md` },
			{ label: 'vs crawl4ai', href: `${REPO_URL}/blob/main/docs/vs-crawl4ai.md` },
			{ label: 'vs search apis', href: `${REPO_URL}/blob/main/docs/vs-search-apis.md` }
		]
	},
	{
		head: 'resources',
		links: [
			{ label: 'changelog', href: `${REPO_URL}/blob/main/CHANGELOG.md` },
			{ label: 'llms.txt', href: `${REPO_URL}/blob/main/llms.txt` },
			{ label: 'security', href: `${REPO_URL}/blob/main/SECURITY.md` }
		]
	},
	{
		head: 'connect',
		links: [
			{ label: 'github', href: REPO_URL },
			{ label: 'issues', href: `${REPO_URL}/issues` },
			{ label: 'license', href: `${REPO_URL}/issues/new?template=license-request.yml` }
		]
	}
];
