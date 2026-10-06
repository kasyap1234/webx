<script lang="ts">
	import WebGLHero from './WebGLHero.svelte';
	import Console from './Console.svelte';
	import { INSTALL_CMD, REPO_URL, VERSION } from '$lib/data';

	let copied = $state(false);
	let copyTimer: ReturnType<typeof setTimeout> | undefined;

	async function copyInstall() {
		try {
			await navigator.clipboard.writeText(INSTALL_CMD);
		} catch {
			const ta = document.createElement('textarea');
			ta.value = INSTALL_CMD;
			ta.style.position = 'fixed';
			ta.style.opacity = '0';
			document.body.appendChild(ta);
			ta.select();
			document.execCommand('copy');
			ta.remove();
		}
		copied = true;
		clearTimeout(copyTimer);
		copyTimer = setTimeout(() => (copied = false), 1600);
	}
</script>

<section class="hero" aria-labelledby="hero-h">
	<WebGLHero />
	<div class="ambient" aria-hidden="true"></div>

	<div class="shell hero-inner">
		<span class="hero-meta mono">open source · mit · {VERSION}</span>

		<h1 id="hero-h">The web toolkit for AI agents.</h1>

		<p class="hero-sub">
			Search, scrape, crawl, and index the web — one static binary, zero API bills.
		</p>

		<div class="cta-row">
			<button
				class="pill pill-primary"
				class:copied
				onclick={copyInstall}
				aria-label="get started — copies the go install command"
			>
				{copied ? 'copied' : 'get started'}
			</button>
			<a class="pill pill-ghost" href={REPO_URL} target="_blank" rel="noreferrer">
				github ↗
			</a>
		</div>

		<div class="stage console-stage">
			<div class="console-wrap">
				<Console />
			</div>
		</div>
	</div>
</section>

<style>
	.hero {
		position: relative;
		overflow: hidden;
	}

	/* painterly warm wash behind the product window — paper warmth,
	   not a spotlight; a whisper of green keeps it alive */
	.ambient {
		position: absolute;
		inset: 0;
		z-index: 1;
		pointer-events: none;
		background:
			radial-gradient(
				ellipse 70% 55% at 50% 40%,
				rgba(180, 160, 120, 0.14),
				rgba(255, 255, 255, 0) 65%
			),
			radial-gradient(
				ellipse 45% 38% at 50% 52%,
				rgba(21, 128, 61, 0.045),
				transparent 65%
			),
			radial-gradient(
				ellipse 52% 34% at 50% 58%,
				rgba(255, 255, 255, 0.7),
				transparent 70%
			);
	}

	.hero-inner {
		position: relative;
		z-index: 2;
		padding-top: calc(var(--nav-h) + clamp(40px, 6vh, 72px));
		padding-bottom: clamp(56px, 8vh, 96px);
		display: flex;
		flex-direction: column;
		align-items: center;
		text-align: center;
	}

	.hero-meta {
		font-size: 12px;
		color: var(--muted);
		animation: rise 0.7s var(--ease-out) 0.1s backwards;
	}

	.hero h1 {
		margin-top: 18px;
		font-size: clamp(2.4rem, 5vw, 3.6rem);
		font-weight: 500;
		letter-spacing: -0.035em;
		line-height: 1.08;
		color: var(--ink);
		animation: rise 0.7s var(--ease-out) 0.2s backwards;
	}

	.hero-sub {
		margin-top: 16px;
		font-size: 16.5px;
		letter-spacing: -0.01em;
		color: var(--muted);
		max-width: 56ch;
		animation: rise 0.7s var(--ease-out) 0.3s backwards;
	}

	.cta-row {
		display: flex;
		gap: 10px;
		margin-top: 26px;
		animation: rise 0.7s var(--ease-out) 0.4s backwards;
	}

	.pill-primary.copied {
		background: var(--accent);
	}

	@keyframes rise {
		from {
			opacity: 0;
			transform: translateY(12px);
		}
		to {
			opacity: 1;
			transform: none;
		}
	}

	/* console = the product shot; painted ambient frame shows ~15-20%
	   around every edge — sand, sage, peach, slate blobs, varied angles
	   so it reads painted, not gradient-y */
	.console-stage {
		--stage-bg:
			radial-gradient(
				120% 90% at 12% 8%,
				rgba(238, 217, 196, 0.95) 0%,
				transparent 55%
			),
			radial-gradient(
				90% 80% at 88% 12%,
				rgba(213, 220, 228, 0.85) 0%,
				transparent 60%
			),
			radial-gradient(
				95% 85% at 15% 88%,
				rgba(221, 227, 211, 0.9) 0%,
				transparent 58%
			),
			radial-gradient(
				85% 75% at 82% 88%,
				rgba(232, 223, 206, 0.95) 0%,
				transparent 60%
			),
			linear-gradient(155deg, #efe8db 0%, #e9e0cf 55%, #e5dcc9 100%);
		width: min(1100px, 100%);
		margin-top: clamp(40px, 6vw, 64px);
		animation: rise 0.8s var(--ease-out) 0.55s backwards;
	}

	.console-wrap {
		position: relative;
		width: 100%;
	}

	@media (max-width: 640px) {
		.hero-inner {
			align-items: flex-start;
			text-align: left;
		}

		.cta-row {
			width: 100%;
		}

		.pill {
			justify-content: center;
		}
	}

	@media (prefers-reduced-motion: reduce) {
		.hero-meta,
		.hero h1,
		.hero-sub,
		.cta-row,
		.console-wrap {
			animation: none;
		}
	}
</style>
