<script lang="ts">
	import { inview } from '$lib/actions/inview';
	import { STATS } from '$lib/data';
</script>

<!-- stats strip — tight band immediately after the hero -->
<section class="section-block stats-sec" id="benchmarks" aria-label="benchmarks">
	<div class="shell">
		<div class="stats bleed reveal" role="list" use:inview>
			{#each STATS as stat (stat.label)}
				<div class="stat" role="listitem">
					<span class="value mono" class:hot={stat.hot}>{stat.value}</span>
					<span class="lbl">{stat.label}</span>
				</div>
			{/each}
		</div>
		<!-- honest logo-strip equivalent — where the binary actually runs -->
		<div class="works bleed reveal" use:inview>
			<span class="w-label">works inside</span>
			<span class="w-items mono"
				>claude code <i>·</i> cursor <i>·</i> devin <i>·</i> windsurf <i>·</i> codex
				<i>·</i> any mcp client</span
			>
		</div>

		<p class="bench-note mono reveal" use:inview>
			js-corpus · go run ./bench · 2026-10-03
		</p>
	</div>
</section>

<style>
	.stats-sec > .shell {
		/* tight strip — the band itself carries the rhythm */
		--pad-y: 0px;
	}

	.stats {
		display: grid;
		grid-template-columns: repeat(4, 1fr);
		border-bottom: 1px solid var(--line);
	}

	.stat {
		display: flex;
		flex-direction: column;
		gap: 6px;
		padding: 28px 24px 24px;
		border-left: 1px solid var(--line);
	}

	.stat:first-child {
		border-left: none;
		padding-left: var(--pad);
	}

	.stat:last-child {
		padding-right: var(--pad);
	}

	.value {
		font-size: 28px;
		font-weight: 500;
		letter-spacing: -0.02em;
		font-variant-numeric: tabular-nums;
		color: var(--ink);
		line-height: 1;
		white-space: nowrap;
	}

	.value.hot {
		color: var(--accent);
	}

	.lbl {
		font-size: 11px;
		letter-spacing: 0;
		color: var(--muted);
	}

	/* works-inside strip — the logo row, but honest */
	.works {
		display: flex;
		align-items: baseline;
		justify-content: center;
		gap: 24px;
		padding: 18px var(--pad) 16px;
		border-bottom: 1px solid var(--line);
		flex-wrap: wrap;
	}

	.w-label {
		font-size: 10.5px;
		letter-spacing: 0.14em;
		text-transform: uppercase;
		color: var(--muted);
		flex-shrink: 0;
	}

	.w-items {
		font-size: 12.5px;
		color: var(--body);
		letter-spacing: -0.01em;
	}

	.w-items i {
		font-style: normal;
		color: var(--faint);
		margin-inline: 10px;
	}

	.bench-note {
		padding-block: 14px 20px;
		font-size: 11px;
		color: var(--muted);
		text-align: right;
	}

	@media (max-width: 720px) {
		.stats {
			grid-template-columns: repeat(2, 1fr);
		}

		.stat {
			padding: 20px var(--pad);
		}

		.stat:nth-child(odd) {
			border-left: none;
		}

		.stat:nth-child(n + 3) {
			border-top: 1px solid var(--line);
		}
	}
</style>
