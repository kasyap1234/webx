<script lang="ts">
	import { inview } from '$lib/actions/inview';
	import { FOOT_COLS } from '$lib/data';
</script>

<footer class="section-block">
	<div class="shell" style="--pad-y: 0px">
		<div class="cta-line mono reveal" use:inview>
			<span class="prompt">$ </span>webx search "your first query"
		</div>

		<div class="foot-grid bleed reveal" use:inview>
			{#each FOOT_COLS as col (col.head)}
				<div class="f-col">
					<span class="f-head">{col.head}</span>
					<nav class="f-links" aria-label={col.head}>
						{#each col.links as link (link.label)}
							<a
								href={link.href}
								target={link.href.startsWith('#') ? undefined : '_blank'}
								rel={link.href.startsWith('#') ? undefined : 'noopener noreferrer'}
								>{link.label}</a
							>
						{/each}
					</nav>
				</div>
			{/each}
		</div>

		<div class="foot-bar bleed">
			<span class="legal">mit © 2026</span>
			<span class="mark">webx</span>
		</div>
	</div>
</footer>

<style>
	.cta-line {
		/* shell is --pad-y:0 — this padding alone carries the close-out rhythm */
		padding-block: 56px 48px;
		font-size: 15px;
		color: var(--ink);
	}

	.prompt {
		color: var(--accent);
	}

	/* ---- link columns — cursor-style footer grid ---- */
	.foot-grid {
		display: grid;
		grid-template-columns: repeat(4, 1fr);
		gap: 32px;
		padding-block: 0 56px;
	}

	.f-head {
		display: block;
		font-size: 13px;
		font-weight: 500;
		color: var(--muted);
		margin-bottom: 14px;
	}

	.f-links {
		display: flex;
		flex-direction: column;
		gap: 11px;
	}

	.f-links a {
		font-size: 13.5px;
		color: var(--body);
		transition: color 0.18s ease-out;
	}

	.f-links a:hover {
		color: var(--ink);
	}

	.foot-bar {
		height: 52px;
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: 24px;
		padding-inline: var(--pad);
		border-top: 1px solid var(--line);
	}

	.legal {
		font-size: 12.5px;
		color: var(--muted);
	}

	.mark {
		font-size: 12.5px;
		font-weight: 550;
		letter-spacing: -0.01em;
		color: var(--ink);
	}

	@media (max-width: 720px) {
		.foot-grid {
			grid-template-columns: repeat(2, 1fr);
			gap: 28px 24px;
			padding-bottom: 40px;
		}
	}
</style>
