<script lang="ts">
	import { onMount } from 'svelte';
	import { base } from '$app/paths';
	import { REPO_URL, DOCS_URL, VERSION } from '$lib/data';

	const links = [
		{ label: 'docs', href: DOCS_URL, external: true, keep: true },
		{ label: 'github', href: REPO_URL, external: true, keep: false },
		{ label: 'benchmarks', href: '#benchmarks', external: false, keep: false },
		{ label: 'pricing', href: '#pricing', external: false, keep: true }
	];

	let scrolled = $state(false);

	onMount(() => {
		const onScroll = () => (scrolled = window.scrollY > 8);
		onScroll();
		window.addEventListener('scroll', onScroll, { passive: true });
		return () => window.removeEventListener('scroll', onScroll);
	});
</script>

<header class="nav" class:scrolled>
	<div class="shell nav-inner">
		<a href="{base}/" class="wordmark" aria-label="webx home"> webx </a>

		<nav class="nav-links" aria-label="primary">
			{#each links as link (link.label)}
				<a
					href={link.href}
					class="nav-link"
					class:hide-sm={!link.keep}
					target={link.external ? '_blank' : undefined}
					rel={link.external ? 'noopener noreferrer' : undefined}
				>
					{link.label}
				</a>
			{/each}
		</nav>

		<div class="nav-right">
			<span class="version mono">{VERSION}</span>
			<a class="gh" href={REPO_URL} target="_blank" rel="noopener noreferrer">github ↗</a>
		</div>
	</div>
</header>

<style>
	.nav {
		position: fixed;
		inset: 0 0 auto 0;
		z-index: 100;
		height: var(--nav-h);
		border-bottom: 1px solid transparent;
		transition:
			background 0.18s ease-out,
			border-color 0.18s ease-out;
	}

	.nav.scrolled {
		background: rgba(245, 242, 236, 0.85);
		backdrop-filter: blur(12px);
		-webkit-backdrop-filter: blur(12px);
		border-bottom-color: var(--line-strong);
	}

	/* the spine runs through the nav bar too */
	.nav-inner {
		height: 100%;
		display: flex;
		align-items: center;
		gap: 36px;
	}

	.wordmark {
		font-size: 15.5px;
		font-weight: 600;
		letter-spacing: -0.025em;
		color: var(--ink);
	}

	.nav-links {
		display: flex;
		gap: 4px;
		margin-inline: auto;
	}

	.nav-link {
		font-size: 14px;
		font-family: var(--font-sans);
		color: var(--body);
		padding: 6px 12px;
		transition: color 0.18s ease-out;
	}

	.nav-link:hover {
		color: var(--ink);
	}

	.nav-right {
		display: flex;
		align-items: center;
		gap: 16px;
	}

	.version {
		font-size: 12px;
		color: var(--muted);
	}

	/* black pill — same grammar as the hero CTA */
	.gh {
		font-size: 13px;
		font-weight: 500;
		letter-spacing: -0.01em;
		color: var(--bg);
		background: var(--ink);
		padding: 7px 15px;
		border-radius: 999px;
		transition:
			background-color 0.15s ease-out,
			transform 0.15s ease-out;
	}

	.gh:hover {
		background: #000;
		transform: translateY(-1px);
	}

	@media (max-width: 780px) {
		.nav-inner {
			gap: 16px;
		}

		.nav-link.hide-sm {
			display: none;
		}
	}

	@media (max-width: 640px) {
		.gh {
			padding: 4px 10px;
			font-size: 12px;
		}
	}

	@media (max-width: 560px) {
		.version {
			display: none;
		}
	}
</style>
