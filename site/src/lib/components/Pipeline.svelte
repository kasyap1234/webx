<script lang="ts">
	import { inview } from '$lib/actions/inview';
	import { PIPELINE } from '$lib/data';
</script>

<!-- the diagram moment — the whole pipeline, in one process -->
<section class="section-block" id="pipeline" aria-labelledby="pipe-h">
	<div class="shell" style="--pad-y: 110px">
		<div class="section-head reveal" use:inview>
			<h2 id="pipe-h">from url to answer, in-process.</h2>
			<p>
				Four hops, one binary — every stage writes to the same
				sqlite file it reads from.
			</p>
		</div>

		<!-- second ambient frame — cooler mix than the hero's -->
		<div class="stage pipe-stage reveal" use:inview={{ threshold: 0.3 }}>
			<div class="pipe panel">
				<div class="panel-bar">pipeline — single process</div>
				<div class="pipe-body mono">
					{#each PIPELINE as n, i (n.node)}
						<div class="node">
							<span class="box">{n.node}</span>
							<span class="note">{n.note}</span>
						</div>
						{#if i < PIPELINE.length - 1}
							<span class="conn" aria-hidden="true"></span>
						{/if}
					{/each}
				</div>
			</div>
		</div>
	</div>
</section>

<style>
	/* sage/slate-dominant paint — distinct from the hero's peach/sand */
	.pipe-stage {
		--stage-bg:
			radial-gradient(
				100% 85% at 85% 15%,
				rgba(221, 227, 211, 0.9) 0%,
				transparent 55%
			),
			radial-gradient(
				90% 80% at 12% 80%,
				rgba(213, 220, 228, 0.85) 0%,
				transparent 58%
			),
			radial-gradient(
				80% 70% at 75% 85%,
				rgba(232, 223, 206, 0.8) 0%,
				transparent 60%
			),
			linear-gradient(150deg, #eae5d9 0%, #e4e1d1 50%, #dfd9c6 100%);
	}

	.pipe {
		overflow: hidden;
	}

	.pipe-body {
		display: flex;
		align-items: flex-start;
		padding: 28px 28px 26px;
	}

	.node {
		display: flex;
		flex-direction: column;
		align-items: center;
		gap: 10px;
	}

	.box {
		display: inline-flex;
		align-items: center;
		height: 36px;
		padding-inline: 14px;
		border: 1px solid var(--line-strong);
		border-radius: 6px;
		background: var(--panel-2);
		box-shadow: 0 1px 2px rgba(23, 21, 18, 0.04);
		font-size: 13px;
		letter-spacing: -0.01em;
		color: var(--ink);
		white-space: nowrap;
		transition:
			border-color 0.18s ease-out,
			color 0.18s ease-out;
	}

	.node:hover .box {
		border-color: rgba(21, 128, 61, 0.4);
		color: var(--accent);
	}

	.note {
		font-size: 10.5px;
		line-height: 1.4;
		letter-spacing: 0;
		color: var(--muted);
		text-align: center;
		max-width: 128px;
		white-space: nowrap;
	}

	/* hairline connector with arrowhead */
	.conn {
		position: relative;
		flex: 1;
		min-width: 24px;
		height: 1px;
		margin-top: 18px; /* vertically centered on the node boxes */
		background: var(--line-strong);
	}

	.conn::after {
		content: '→';
		position: absolute;
		right: -1px;
		top: 50%;
		transform: translateY(-52%);
		font-size: 11px;
		color: var(--muted);
		background: var(--panel);
		padding-left: 2px;
	}

	@media (max-width: 680px) {
		.pipe-body {
			flex-direction: column;
			align-items: stretch;
			gap: 0;
			padding: 22px 20px;
		}

		.node {
			align-items: flex-start;
			gap: 6px;
		}

		.conn {
			flex: none;
			width: 1px;
			height: 22px;
			margin-top: 0;
			margin-left: 24px;
			background: var(--line-strong);
		}

		.conn::after {
			content: '↓';
			right: auto;
			left: 50%;
			top: auto;
			bottom: -4px;
			transform: translateX(-50%);
			padding-left: 0;
			padding-top: 2px;
		}

		.note {
			text-align: left;
			max-width: none;
		}
	}
</style>
