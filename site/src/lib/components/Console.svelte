<script lang="ts">
	/**
	 * The hero product window — a plausible webx console served on :8080.
	 * All chrome is monochrome; the only color inside is product content
	 * (syntax-highlighted JSON, status colors, the running crawl card).
	 */
	const rail = [
		{ name: 'overview', meta: '' },
		{ name: 'scrape', meta: '', active: true },
		{ name: 'search', meta: '' },
		{ name: 'crawl', meta: '' },
		{ name: 'extract', meta: '' },
		{ name: 'jobs', meta: '2' },
		{ name: 'keys', meta: '3' },
		{ name: 'usage', meta: '' }
	];

	const tabs = ['markdown', 'json', 'links', 'meta'];
</script>

<div class="console" role="img" aria-label="webx console showing a scrape request to go.dev with syntax-highlighted JSON response and markdown preview">
	<!-- browser chrome -->
	<div class="b-head">
		<span class="dots" aria-hidden="true"><i></i><i></i><i></i></span>
		<span class="addr mono">localhost:8080</span>
	</div>

	<div class="b-body">
		<!-- left nav rail -->
		<nav class="rail" aria-hidden="true">
			{#each rail as item (item.name)}
				<span class="rail-row mono" class:active={item.active}>
					{item.name}
					{#if item.meta}<span class="rail-meta">{item.meta}</span>{/if}
				</span>
			{/each}
			<span class="rail-foot mono">webxd · sqlite</span>
		</nav>

		<!-- request / response -->
		<div class="center">
			<div class="req-row">
				<span class="method mono">POST</span>
				<span class="route mono">/scrape</span>
				<span class="ok mono">200 OK · 996ms</span>
			</div>
			<div class="url-field mono">go.dev/doc/effective_go</div>

			<div class="tabs mono" role="tablist" aria-hidden="true">
				{#each tabs as t (t)}
					<span class="tab" class:on={t === 'json'} role="tab">{t}</span>
				{/each}
			</div>

			<pre class="json mono"><span class="p">{"{"}</span>
  <span class="k">"data"</span><span class="p">:</span> <span class="p">{"{"}</span>
    <span class="k">"url"</span><span class="p">:</span> <span class="s">"https://go.dev/doc/effective_go"</span><span class="p">,</span>
    <span class="k">"title"</span><span class="p">:</span> <span class="s">"Effective Go"</span><span class="p">,</span>
    <span class="k">"markdown"</span><span class="p">:</span> <span class="s">"# Effective Go\n\nGo is a new language…"</span><span class="p">,</span>
    <span class="k">"tier_used"</span><span class="p">:</span> <span class="s">"http"</span><span class="p">,</span>
    <span class="k">"extractor"</span><span class="p">:</span> <span class="s">"trafilatura"</span><span class="p">,</span>
    <span class="k">"est_tokens"</span><span class="p">:</span> <span class="n">6118</span><span class="p">,</span>
    <span class="k">"deduped"</span><span class="p">:</span> <span class="n">2</span><span class="p">,</span>
    <span class="k">"cached"</span><span class="p">:</span> <span class="b">false</span><span class="p">,</span>
    <span class="k">"links"</span><span class="p">:</span> <span class="p">[</span><span class="s">"/doc/"</span><span class="p">,</span> <span class="s">"/doc/effective_go#concurrency"</span><span class="p">,</span> <span class="s">"/pkg/"</span><span class="p">],</span>
    <span class="k">"link_count"</span><span class="p">:</span> <span class="n">214</span><span class="p">,</span>
    <span class="k">"warnings"</span><span class="p">:</span> <span class="p">[]</span>
  <span class="p">{"}"}</span>
<span class="p">{"}"}</span></pre>
		</div>

		<!-- rendered markdown preview — the product's outcome -->
		<div class="preview">
			<span class="pane-label mono">preview</span>
			<h3>Effective Go</h3>
			<p>
				Go is a new language. Although it borrows ideas from
				existing languages, it has unusual properties that make
				effective Go programs different in character.
			</p>
			<ul>
				<li>Concurrency</li>
				<li>Channels and goroutines</li>
				<li>Interfaces</li>
			</ul>
			<span class="stat mono">extractor: trafilatura · −84% tokens</span>
		</div>
	</div>

	<!-- status bar -->
	<div class="b-status mono">
		<span class="s-left">webxd · ready</span>
		<span class="s-right">15 providers · p50 996ms · fts5</span>
	</div>

	<!-- floating job card — overlaps the corner, the layered trick -->
	<div class="job-card" aria-hidden="true">
		<div class="jc-top mono"><span class="dot"></span>crawl 7f3a · running</div>
		<div class="jc-bar mono"><span class="fill">████████░░</span> 82%</div>
		<div class="jc-sub mono">47/214 pages · fts5 + vec</div>
	</div>

	<!-- second float: mcp tool-call popover, bottom-left -->
	<div class="search-pop" aria-hidden="true">
		<div class="sp-head mono"><span class="sp-tag">mcp</span>web_search</div>
		<div class="sp-row mono"><span class="sp-dot" style="background:#15803d"></span>Effective Go — go.dev/doc</div>
		<div class="sp-row mono"><span class="sp-dot" style="background:#2563eb"></span>net/http — pkg.go.dev</div>
		<div class="sp-row mono"><span class="sp-dot" style="background:#b45309"></span>Deploy a Go App — fly.io/docs</div>
	</div>
</div>

<style>
	.console {
		position: relative;
		text-align: left; /* reset the hero's center alignment — product text */
		border-radius: 12px;
		border: 1px solid var(--line-strong);
		background: var(--panel);
		box-shadow: var(--shadow-window);
		/* no overflow clip — the floats must spill onto the stage backdrop;
		   corner rounding is handled on the chrome bars instead */
	}

	/* ---------- browser chrome ---------- */
	.b-head {
		position: relative;
		height: 40px;
		display: flex;
		align-items: center;
		padding-inline: 16px;
		border-bottom: 1px solid var(--line);
		border-radius: 11px 11px 0 0;
		background: var(--panel-2);
	}

	.dots {
		display: inline-flex;
		gap: 7px;
	}

	.dots i {
		width: 11px;
		height: 11px;
		border-radius: 50%;
		background: #ddd8cb;
		border: 1px solid rgba(23, 21, 18, 0.08);
	}

	.addr {
		position: absolute;
		left: 50%;
		transform: translateX(-50%);
		font-size: 12px;
		color: var(--muted);
		background: var(--panel);
		border: 1px solid var(--line-strong);
		border-radius: 999px;
		padding: 3px 14px;
	}

	/* ---------- three panes ---------- */
	.b-body {
		display: grid;
		grid-template-columns: 190px 1fr 290px;
		min-height: 460px;
	}

	/* left rail */
	.rail {
		display: flex;
		flex-direction: column;
		border-right: 1px solid var(--line);
		background: var(--panel-2);
		padding-block: 8px;
	}

	.rail-row {
		position: relative;
		display: flex;
		align-items: center;
		justify-content: space-between;
		font-size: 12.5px;
		color: var(--muted);
		padding: 9px 14px 9px 16px;
	}

	.rail-row::before {
		content: '';
		position: absolute;
		left: 0;
		top: 0;
		bottom: 0;
		width: 2px;
		background: transparent;
	}

	.rail-row.active {
		color: var(--ink);
		background: var(--panel);
	}

	.rail-row.active::before {
		background: var(--accent);
	}

	.rail-meta {
		font-size: 10.5px;
		color: var(--muted);
	}

	.rail-foot {
		margin-top: auto;
		padding: 10px 16px;
		font-size: 11px;
		color: var(--muted);
		border-top: 1px solid var(--line);
	}

	/* center request view */
	.center {
		padding: 18px 20px;
		min-width: 0;
	}

	.req-row {
		display: flex;
		align-items: center;
		gap: 10px;
	}

	.method {
		font-size: 10.5px;
		color: var(--accent);
		border: 1px solid rgba(21, 128, 61, 0.35);
		border-radius: 5px;
		padding: 2px 7px;
		letter-spacing: 0.02em;
	}

	.route {
		font-size: 13px;
		color: var(--ink);
	}

	.ok {
		margin-left: auto;
		font-size: 11.5px;
		color: var(--accent);
		font-variant-numeric: tabular-nums;
	}

	.url-field {
		margin-top: 12px;
		font-size: 12.5px;
		color: var(--body);
		background: var(--canvas-2);
		border: 1px solid var(--line-strong);
		border-radius: 6px;
		padding: 7px 10px;
		white-space: nowrap;
		overflow: hidden;
	}

	.tabs {
		display: flex;
		gap: 16px;
		margin-top: 16px;
		border-bottom: 1px solid var(--line);
	}

	.tab {
		font-size: 12px;
		color: var(--muted);
		padding-bottom: 7px;
		border-bottom: 2px solid transparent;
		margin-bottom: -1px;
	}

	.tab.on {
		color: var(--ink);
		border-bottom-color: var(--ink);
	}

	/* pretty-printed JSON — the page's color source */
	.json {
		margin-top: 14px;
		font-size: 12.5px;
		line-height: 1.65;
		font-variant-numeric: tabular-nums;
		color: var(--body);
		white-space: pre;
	}

	.k {
		color: var(--syn-key);
	}
	.s {
		color: var(--syn-str);
	}
	.n {
		color: var(--syn-num);
	}
	.b {
		color: var(--syn-bool);
	}
	.p {
		color: var(--syn-punc);
	}

	/* markdown preview pane */
	.preview {
		border-left: 1px solid var(--line);
		background: var(--canvas-2);
		padding: 18px 20px;
	}

	.pane-label {
		display: block;
		font-size: 11px;
		color: var(--muted);
		margin-bottom: 14px;
	}

	.preview h3 {
		font-size: 15px;
		font-weight: 560;
		letter-spacing: -0.015em;
		color: var(--ink);
	}

	.preview p {
		margin-top: 10px;
		font-size: 12.5px;
		line-height: 1.65;
		color: var(--body);
	}

	.preview ul {
		margin: 12px 0 0 16px;
		font-size: 12.5px;
		line-height: 1.7;
		color: var(--body);
	}

	.preview li::marker {
		color: var(--faint);
	}

	.preview .stat {
		display: block;
		margin-top: 16px;
		padding-top: 12px;
		border-top: 1px solid var(--line);
		font-size: 11px;
		color: var(--muted);
		font-variant-numeric: tabular-nums;
	}

	/* ---------- status bar ---------- */
	.b-status {
		height: 36px;
		display: flex;
		align-items: center;
		justify-content: space-between;
		padding-inline: 16px;
		border-top: 1px solid var(--line);
		border-radius: 0 0 11px 11px;
		background: var(--panel-2);
		font-size: 11.5px;
		color: var(--muted);
		letter-spacing: -0.01em;
		font-variant-numeric: tabular-nums;
	}

	/* keep telemetry clear of the overlapping job card (card = 240px @ -18px) */
	@media (min-width: 861px) {
		.b-status {
			padding-right: 246px;
		}
	}

	.s-left {
		display: inline-flex;
		align-items: center;
		gap: 8px;
	}

	.dot {
		width: 7px;
		height: 7px;
		border-radius: 50%;
		background: var(--accent);
		display: inline-block;
	}

	/* ---------- floating job card ---------- */
	.job-card {
		position: absolute;
		right: -18px;
		bottom: -26px;
		width: 240px;
		background: var(--panel);
		border: 1px solid var(--line-strong);
		border-radius: 10px;
		box-shadow:
			0 1px 2px rgba(23, 21, 18, 0.05),
			0 8px 24px rgba(23, 21, 18, 0.08),
			0 24px 48px rgba(23, 21, 18, 0.08);
		padding: 12px 14px;
	}

	.jc-top {
		font-size: 11.5px;
		color: var(--ink);
		display: flex;
		align-items: center;
		gap: 7px;
	}

	.jc-top .dot {
		width: 6px;
		height: 6px;
	}

	.jc-bar {
		margin-top: 8px;
		font-size: 12px;
		color: var(--muted);
		font-variant-numeric: tabular-nums;
	}

	.jc-bar .fill {
		color: var(--accent);
	}

	.jc-sub {
		margin-top: 4px;
		font-size: 11px;
		color: var(--muted);
		font-variant-numeric: tabular-nums;
	}

	/* ---------- mcp search popover — second float, bottom-left ---------- */
	.search-pop {
		position: absolute;
		left: -16px;
		bottom: 56px;
		width: 236px;
		background: var(--panel);
		border: 1px solid var(--line-strong);
		border-radius: 10px;
		box-shadow:
			0 1px 2px rgba(23, 21, 18, 0.05),
			0 8px 24px rgba(23, 21, 18, 0.08),
			0 24px 48px rgba(23, 21, 18, 0.08);
		padding: 10px 12px 6px;
	}

	.sp-head {
		display: flex;
		align-items: center;
		gap: 8px;
		font-size: 11px;
		color: var(--muted);
		padding-bottom: 8px;
		border-bottom: 1px solid var(--line);
		margin-bottom: 4px;
	}

	.sp-tag {
		font-size: 9.5px;
		color: var(--accent);
		border: 1px solid rgba(21, 128, 61, 0.35);
		border-radius: 4px;
		padding: 1px 5px;
		letter-spacing: 0.02em;
	}

	.sp-row {
		display: flex;
		align-items: center;
		gap: 8px;
		font-size: 11px;
		color: var(--body);
		padding-block: 5px;
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
	}

	.sp-dot {
		width: 6px;
		height: 6px;
		border-radius: 50%;
		flex-shrink: 0;
	}

	/* ---------- mobile: panes stack, card tucks inline ---------- */
	@media (max-width: 860px) {
		.b-body {
			grid-template-columns: 1fr;
			min-height: 0;
		}

		.rail {
			flex-direction: row;
			overflow-x: auto;
			border-right: none;
			border-bottom: 1px solid var(--line);
			padding: 0;
			scrollbar-width: none;
		}

		.rail::-webkit-scrollbar {
			display: none;
		}

		.rail-row {
			padding: 10px 14px;
			white-space: nowrap;
		}

		.rail-row.active::before {
			width: auto;
			height: 2px;
			left: 0;
			right: 0;
			top: auto;
		}

		.rail-meta,
		.rail-foot {
			display: none;
		}

		.preview {
			border-left: none;
			border-top: 1px solid var(--line);
		}

		.job-card {
			position: static;
			width: auto;
			margin: 0 16px 16px;
			animation: none;
		}

		.search-pop {
			position: static;
			width: auto;
			margin: -8px 16px 16px;
			animation: none;
		}
	}

	@media (prefers-reduced-motion: reduce) {
		.job-card,
		.search-pop {
			animation: none;
		}
	}
</style>
