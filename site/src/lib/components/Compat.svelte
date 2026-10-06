<script lang="ts">
	import { inview } from '$lib/actions/inview';
	import { COMPAT_ROUTES, COMPAT_DIFF, DOCS_URL } from '$lib/data';
</script>

<section class="section-block" id="compat" aria-labelledby="compat-h">
	<div class="shell" style="--pad-y: 120px">
		<div class="compat">
			<!-- visual-left this round — the alternating split -->
			<div class="visual reveal" use:inview={{ threshold: 0.3 }}>
				<div class="diff panel" aria-label="Firecrawl SDK diff">
					<div class="panel-bar">diff</div>
					<div class="d-body mono">
						<div class="d-line ctx">{COMPAT_DIFF.context}</div>
						<div class="d-line before"><span class="mark">−</span>{COMPAT_DIFF.before}</div>
						<div class="d-line after"><span class="mark">+</span>{COMPAT_DIFF.after}</div>
						<div class="d-line ctx">{COMPAT_DIFF.end}</div>
					</div>
				</div>
			</div>

			<div class="copy reveal" use:inview style="--reveal-delay: 80ms">
				<h2 id="compat-h">drop-in for firecrawl.</h2>
				<p class="desc">
					Repaint your base URL — the SDK keeps working.
				</p>
				<div class="routes-panel panel mono">
					<div class="panel-bar">routes</div>
					<ul class="routes">
						{#each COMPAT_ROUTES as r (r)}
							<li>{r}</li>
						{/each}
					</ul>
				</div>
				<a class="go-link mono" href={DOCS_URL} target="_blank" rel="noopener noreferrer"
					>migration guide →</a
				>
			</div>
		</div>
	</div>
</section>

<style>
	.compat {
		position: relative;
		z-index: 1;
		display: grid;
		grid-template-columns: 1fr 1fr;
		gap: clamp(48px, 7vw, 96px);
		align-items: center;
	}

	h2 {
		font-size: clamp(1.5rem, 2.6vw, 1.9rem);
		font-weight: 540;
		letter-spacing: -0.02em;
		line-height: 1.2;
	}

	.desc {
		margin-top: 12px;
		font-size: 15px;
		letter-spacing: -0.005em;
		color: var(--body);
		max-width: 40ch;
	}

	/* routes as a second artifact — fills the left column, echoes diff */
	.routes-panel {
		margin-top: 28px;
		border-radius: 8px;
		overflow: hidden;
		max-width: 320px;
	}

	.routes {
		list-style: none;
		padding: 10px 14px 12px;
		font-size: 12.5px;
		line-height: 1.7;
		color: var(--muted);
	}

	/* smaller window frame — same chrome language as the console */
	.diff {
		border-radius: 8px;
		overflow: hidden;
	}

	.d-body {
		padding: 14px 16px;
		font-size: 12.5px;
		line-height: 1.6;
	}

	.d-line {
		white-space: pre;
	}

	.d-line .mark {
		display: inline-block;
		width: 1em;
	}

	.ctx {
		color: var(--muted);
	}

	.before {
		color: #b91c1c;
	}

	.before .mark {
		color: #b91c1c;
		opacity: 0.6;
	}

	.after {
		color: var(--accent);
		background: rgba(21, 128, 61, 0.06);
		border-radius: 3px;
	}

	/* the one sanctioned exit link per section */
	.go-link {
		display: inline-block;
		margin-top: 20px;
		font-size: 13px;
		color: var(--accent);
		transition: opacity 0.15s ease-out;
	}

	.go-link:hover {
		opacity: 0.75;
	}

	@media (max-width: 760px) {
		.compat {
			grid-template-columns: 1fr;
			gap: 40px;
		}

		/* copy first on mobile — visual follows */
		.copy {
			order: -1;
		}
	}
</style>
