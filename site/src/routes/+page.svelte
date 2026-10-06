<script lang="ts">
	import Nav from '$lib/components/Nav.svelte';
	import Hero from '$lib/components/Hero.svelte';
	import Benchmarks from '$lib/components/Benchmarks.svelte';
	import Capabilities from '$lib/components/Capabilities.svelte';
	import Pipeline from '$lib/components/Pipeline.svelte';
	import Compat from '$lib/components/Compat.svelte';
	import Pricing from '$lib/components/Pricing.svelte';
	import Footer from '$lib/components/Footer.svelte';
</script>

<svelte:head>
	<title>webx — the web toolkit for AI agents</title>
	<meta
		name="description"
		content="webx: search, scrape, crawl, index and extract the web from one MIT-licensed static Go binary. No API bills, no SaaS — just a terminal."
	/>
</svelte:head>

<a class="skip-link" href="#main">skip to content</a>
<Nav />
<main id="main">
	<Hero />
	<Benchmarks />
	<Capabilities />
	<Pipeline />
	<Compat />
	<Pricing />
</main>
<Footer />
