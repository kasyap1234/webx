<script lang="ts">
	import { inview } from '$lib/actions/inview';
	import { CAPABILITIES, HELP_HINT, DOCS_URL } from '$lib/data';
</script>

<section class="section-block" id="capabilities" aria-labelledby="cap-h">
	<div class="shell split" style="--pad-y: 120px">
		<span class="rule-tag">spec</span>
		<!-- sticky context rail -->
		<aside class="rail reveal" use:inview>
			<h2 id="cap-h">Eight verbs. One binary.</h2>
			<p class="lede">
				Every stage of the retrieval pipeline runs in the same
				process — no daemons, no sidecars, no meter.
			</p>
			<div class="help panel mono" aria-hidden="true">
				<div class="panel-bar">webx --help</div>
				<div class="help-body">
					<span class="prompt">$ </span>{HELP_HINT.cmd}
					<pre>{HELP_HINT.out}</pre>
				</div>
			</div>
			<a class="go-link mono" href={DOCS_URL} target="_blank" rel="noopener noreferrer"
				>read the docs →</a
			>
		</aside>

		<!-- spec sheet as a raised artifact -->
		<dl class="sheet panel reveal" use:inview={{ threshold: 0.15 }}>
			{#each CAPABILITIES as cap (cap.cmd)}
				<div class="row">
					<dt class="mono">{cap.cmd}</dt>
					<dd>{cap.desc}</dd>
					<span class="flag mono">{cap.flag}</span>
				</div>
			{/each}
		</dl>
	</div>
</section>

<style>
	.split {
		display: grid;
		grid-template-columns: 2fr 3fr;
		gap: clamp(40px, 6vw, 80px);
		align-items: start;
	}

	/* left rail sticks while the sheet scrolls past */
	.rail {
		position: sticky;
		top: calc(var(--nav-h) + 32px);
	}

	.rail h2 {
		font-size: clamp(1.5rem, 2.6vw, 1.9rem);
		font-weight: 540;
		letter-spacing: -0.02em;
		line-height: 1.2;
	}

	.lede {
		margin-top: 14px;
		font-size: 15px;
		letter-spacing: -0.005em;
		color: var(--body);
		max-width: 34ch;
	}

	/* --help artifact — same chrome language as the terminal */
	.help {
		margin-top: 32px;
		border-radius: 8px;
		overflow: hidden;
	}

	.help-body {
		padding: 12px 14px;
		font-size: 12px;
		line-height: 1.6;
		color: var(--body);
	}

	.help .prompt {
		color: var(--accent);
	}

	.help pre {
		margin-top: 4px;
		font: inherit;
		color: var(--muted);
		white-space: pre-wrap;
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

	/* the sheet is an artifact, not a bare list */
	.sheet {
		overflow: hidden;
	}

	.row {
		position: relative;
		display: grid;
		grid-template-columns: 140px 1fr auto;
		gap: 24px;
		align-items: baseline;
		padding: 16px 20px;
		border-bottom: 1px solid var(--line);
	}

	.row:last-child {
		border-bottom: none;
	}

	/* hover: a 2px tick of light on the left edge, not a bg flood */
	.row::before {
		content: '';
		position: absolute;
		left: 0;
		top: 0;
		bottom: 0;
		width: 2px;
		background: var(--accent);
		transform: scaleY(0);
		transform-origin: top;
		transition: transform 0.18s ease-out;
	}

	.row:hover::before {
		transform: scaleY(1);
	}

	.row dt {
		font-size: 13px;
		color: var(--body);
		transition: color 0.18s ease-out;
	}

	/* hover: green tick + the command darkens to ink */
	.row:hover dt {
		color: var(--ink);
	}

	.row dd {
		font-size: 14px;
		letter-spacing: -0.005em;
		color: var(--body);
	}

	.row .flag {
		font-size: 11.5px;
		color: var(--muted);
		text-align: right;
		white-space: nowrap;
	}

	@media (max-width: 760px) {
		.split {
			grid-template-columns: 1fr;
			gap: 36px;
		}

		.rail {
			position: static;
		}
	}

	@media (max-width: 640px) {
		.row {
			grid-template-columns: 1fr auto;
			gap: 4px 16px;
		}

		.row dd {
			grid-column: 1 / -1;
			grid-row: 2;
		}
	}
</style>
