<script lang="ts">
	import { inview } from '$lib/actions/inview';
	import { BILLS, WEBX_BILL, PRICE_COLS, PRICE_NOTE } from '$lib/data';
</script>

<section class="section-block" id="pricing" aria-labelledby="price-h">
	<div class="shell" style="--pad-y: 110px">
		<span class="rule-tag">pricing</span>

		<div class="section-head reveal" use:inview>
			<h2 id="price-h">one binary, zero bills.</h2>
		</div>

		<!-- the punchline: three struck invoices, one free binary -->
		<div class="bills-strip bleed mono reveal" role="list" use:inview>
			{#each BILLS as bill (bill.name)}
				<span class="bill struck" role="listitem">
					<span class="name">{bill.name}</span>
					<span class="price">{bill.price}</span>
				</span>
			{/each}
			<span class="bill webx" role="listitem">
				<span class="name">{WEBX_BILL.name}</span>
				<span class="price">{WEBX_BILL.price}</span>
			</span>
		</div>

		<div class="tiers reveal" use:inview>
			{#each PRICE_COLS as col (col.name)}
				<div class="tier-card" class:pro={col.recommended}>
					<div class="t-head">
						<span class="t-name">{col.name}</span>
						<span class="t-tag">{col.tagline}</span>
					</div>
					<div class="t-price">
						{col.price}<span class="per">{col.per ? ` ${col.per}` : ''}</span>
					</div>

					<span class="t-inc">{col.includes ?? 'includes:'}</span>
					<ul class="t-feats">
						{#each col.features as feat (feat)}
							<li><span class="check" aria-hidden="true">✓</span>{feat}</li>
						{/each}
					</ul>

					<a
						class="t-cta pill"
						class:pill-primary={col.recommended}
						class:pill-ghost={!col.recommended}
						href={col.href}
						target="_blank"
						rel="noopener noreferrer">{col.cta}</a
					>
				</div>
			{/each}
		</div>

		<p class="price-note reveal" use:inview>
			{PRICE_NOTE}
		</p>
	</div>
</section>

<style>
	/* ---- bills lead-in strip ---- */
	.bills-strip {
		position: relative;
		z-index: 1;
		display: grid;
		grid-template-columns: repeat(4, 1fr);
		border-block: 1px solid var(--line);
		margin-bottom: 64px;
	}

	.bill {
		display: flex;
		align-items: baseline;
		justify-content: space-between;
		gap: 16px;
		padding: 16px 20px;
		border-left: 1px solid var(--line);
		font-size: 13px;
	}

	.bill:first-child {
		border-left: none;
		padding-left: var(--pad);
	}

	.bill:last-child {
		padding-right: var(--pad);
	}

	.bill.struck {
		color: var(--faint);
	}

	.bill.struck .name {
		text-decoration: line-through;
		text-decoration-color: var(--faint);
	}

	.bill.struck .price {
		color: var(--faint);
		text-decoration: line-through;
		text-decoration-color: var(--faint);
	}

	.bill.webx .name {
		color: var(--ink);
	}

	.bill.webx .price {
		color: var(--accent);
	}

	/* ---- tier cards — cursor-style columns ---- */
	.tiers {
		position: relative;
		z-index: 1;
		display: grid;
		grid-template-columns: repeat(3, 1fr);
		gap: 20px;
	}

	.tier-card {
		display: flex;
		flex-direction: column;
		background: var(--panel);
		border: 1px solid var(--line-strong);
		border-radius: 14px;
		box-shadow: var(--shadow-panel);
		padding: 26px 26px 24px;
	}

	.tier-card.pro {
		border-top: 2px solid var(--accent);
		padding-top: 24px;
	}

	.t-head {
		display: flex;
		flex-direction: column;
		gap: 3px;
	}

	.t-name {
		font-size: 15px;
		font-weight: 550;
		letter-spacing: -0.01em;
		color: var(--ink);
	}

	.t-tag {
		font-size: 13px;
		color: var(--muted);
	}

	.t-price {
		margin-top: 18px;
		font-size: 30px;
		font-weight: 560;
		letter-spacing: -0.035em;
		font-variant-numeric: tabular-nums;
		color: var(--ink);
		line-height: 1;
	}

	.per {
		font-size: 13px;
		font-weight: 400;
		letter-spacing: 0;
		color: var(--muted);
	}

	.t-inc {
		margin-top: 24px;
		font-size: 12.5px;
		color: var(--muted);
	}

	.t-feats {
		list-style: none;
		margin-top: 12px;
		display: flex;
		flex-direction: column;
		gap: 9px;
		flex: 1;
	}

	.t-feats li {
		display: flex;
		align-items: baseline;
		gap: 9px;
		font-size: 13.5px;
		color: var(--body);
	}

	.check {
		font-size: 11px;
		color: var(--muted);
		flex-shrink: 0;
	}

	.t-cta {
		margin-top: 26px;
		align-self: flex-start;
		font-size: 13px;
		padding: 8px 16px;
	}

	.price-note {
		position: relative;
		z-index: 1;
		margin-top: 28px;
		font-size: 14px;
		color: var(--muted);
	}

	@media (max-width: 780px) {
		.tiers {
			grid-template-columns: 1fr;
			max-width: 420px;
		}
	}

	@media (max-width: 640px) {
		.bills-strip {
			grid-template-columns: repeat(2, 1fr);
		}

		.bill {
			padding: 14px var(--pad);
		}

		.bill:nth-child(odd) {
			border-left: none;
		}

		.bill:nth-child(n + 3) {
			border-top: 1px solid var(--line);
		}
	}
</style>
