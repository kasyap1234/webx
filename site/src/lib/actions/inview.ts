import type { ActionReturn } from 'svelte/action';

interface InViewParams {
	threshold?: number;
	once?: boolean;
}

/**
 * Adds `.is-inview` when the element enters the viewport.
 * Pair with the global `.reveal` class for scroll-triggered materialization.
 */
export function inview(
	node: HTMLElement,
	params: InViewParams = {}
): ActionReturn<InViewParams> {
	const once = params.once !== false;

	const observer = new IntersectionObserver(
		(entries) => {
			for (const entry of entries) {
				if (entry.isIntersecting) {
					node.classList.add('is-inview');
					if (once) observer.unobserve(node);
				} else if (!once) {
					node.classList.remove('is-inview');
				}
			}
		},
		{ threshold: params.threshold ?? 0.15, rootMargin: '0px 0px -8% 0px' }
	);

	observer.observe(node);

	return {
		destroy() {
			observer.disconnect();
		}
	};
}
