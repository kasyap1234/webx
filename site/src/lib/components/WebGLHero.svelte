<script lang="ts">
	import { onMount } from 'svelte';
	import type { GridFloorScene } from '$lib/three/grid-floor';

	/**
	 * Hero background canvas. three.js is dynamically imported inside
	 * onMount — it never enters the SSR graph and never blocks first paint.
	 * If WebGL is unavailable the plain cream canvas stays — invisible win.
	 */
	let container: HTMLDivElement;

	onMount(() => {
		let scene: GridFloorScene | null = null;
		let cancelled = false;

		import('$lib/three/grid-floor')
			.then((mod) => {
				if (cancelled || !container) return;
				scene = mod.createGridFloor(container);
			})
			.catch(() => {
				/* pure black is the fallback — nothing to do */
			});

		return () => {
			cancelled = true;
			scene?.destroy();
		};
	});
</script>

<div bind:this={container} class="gl" aria-hidden="true"></div>

<style>
	.gl {
		position: absolute;
		left: 0;
		right: 0;
		bottom: 0;
		height: 55%;
		overflow: hidden;
		/* dissolve the floor's top edge into the cream above */
		mask-image: linear-gradient(to bottom, transparent 0%, black 42%);
		-webkit-mask-image: linear-gradient(to bottom, transparent 0%, black 42%);
	}
</style>
