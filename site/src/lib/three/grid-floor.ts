/**
 * grid-floor.ts — the "quiet" hero background.
 *
 * One THREE.GridHelper (a single LineSegments draw call): a dark floor of
 * thin white lines at ~8% opacity receding to a low horizon. Scene fog
 * dissolves the far edge; the floor drifts slowly toward the viewer by
 * wrapping `position.z` modulo the cell size. The only interaction is
 * camera parallax from the pointer — no per-vertex theatrics. It should
 * be felt more than seen.
 *
 * Loaded ONLY via dynamic import() inside onMount — never at SSR top
 * level. destroy() frees the geometry, material, renderer, GL context,
 * listeners and observers.
 */
import * as THREE from 'three';

/* warm-paper re-skin: ink lines on cream fog — blueprint, not void */
const BG = new THREE.Color('#f5f2ec');
const LINE = new THREE.Color('#171512');

/** Cell size must equal size / divisions or the drift wrap will pop. */
const FOG_NEAR = 8;
const FOG_FAR = 68;
const DRIFT_SPEED = 0.45; // units/s — barely perceptible
const PARALLAX_X = 0.55;
const PARALLAX_Y = 0.22;

export interface GridFloorScene {
	destroy: () => void;
}

/**
 * Mounts the scene inside `container`. Returns null if WebGL is
 * unavailable — the caller leaves the pure-black background in place.
 */
export function createGridFloor(container: HTMLElement): GridFloorScene | null {
	const reduced = window.matchMedia('(prefers-reduced-motion: reduce)').matches;

	let renderer: THREE.WebGLRenderer;
	try {
		renderer = new THREE.WebGLRenderer({
			antialias: true,
			alpha: true,
			powerPreference: 'low-power',
			stencil: false,
			depth: true
		});
	} catch {
		return null;
	}

	const pixelRatio = Math.min(window.devicePixelRatio || 1, 2);
	renderer.setPixelRatio(pixelRatio);
	renderer.setClearColor(BG, 0);
	renderer.domElement.style.position = 'absolute';
	renderer.domElement.style.inset = '0';
	renderer.domElement.style.width = '100%';
	renderer.domElement.style.height = '100%';
	container.appendChild(renderer.domElement);

	const scene = new THREE.Scene();
	scene.fog = new THREE.Fog(BG, FOG_NEAR, FOG_FAR);

	const camera = new THREE.PerspectiveCamera(55, 1, 0.1, 120);
	const CAM_Y = 2.5;
	const CAM_Z = 7;
	camera.position.set(0, CAM_Y, CAM_Z);
	const target = new THREE.Vector3(0, 0.35, -30);
	camera.lookAt(target);

	const compact = container.clientWidth < 760;
	const SIZE = compact ? 180 : 240;
	const DIVISIONS = compact ? 72 : 120;
	const CELL = SIZE / DIVISIONS;

	const grid = new THREE.GridHelper(SIZE, DIVISIONS, LINE, LINE);
	const material = grid.material as THREE.LineBasicMaterial;
	material.transparent = true;
	material.opacity = 0.09;
	material.depthWrite = false;
	grid.position.y = 0;
	scene.add(grid);

	/* ---------------- pointer parallax ---------------- */

	const pointerNdc = new THREE.Vector2(0, 0);
	const pointerTarget = new THREE.Vector2(0, 0);

	function onPointerMove(e: PointerEvent) {
		const rect = container.getBoundingClientRect();
		if (
			e.clientY < rect.top ||
			e.clientY > rect.bottom ||
			e.clientX < rect.left ||
			e.clientX > rect.right
		) {
			pointerTarget.set(0, 0);
			return;
		}
		pointerTarget.set(
			((e.clientX - rect.left) / rect.width) * 2 - 1,
			-(((e.clientY - rect.top) / rect.height) * 2 - 1)
		);
	}

	window.addEventListener('pointermove', onPointerMove, { passive: true });

	/* ---------------- sizing ---------------- */

	function resize() {
		const w = container.clientWidth || 1;
		const h = container.clientHeight || 1;
		renderer.setSize(w, h, false);
		camera.aspect = w / h;
		camera.updateProjectionMatrix();
	}
	resize();

	const resizeObs = new ResizeObserver(resize);
	resizeObs.observe(container);

	/* ---------------- loop ---------------- */

	let running = false;
	let inView = true;
	let raf = 0;
	let last = performance.now();

	const visObs = new IntersectionObserver(
		(entries) => {
			inView = entries[0]?.isIntersecting ?? true;
			syncPlayback();
		},
		{ threshold: 0.02 }
	);
	visObs.observe(container);

	function onVisibility() {
		syncPlayback();
	}
	document.addEventListener('visibilitychange', onVisibility);

	function step(dt: number) {
		// drift the floor toward the viewer, wrapped on the cell size
		grid.position.z = (grid.position.z + DRIFT_SPEED * dt) % CELL;

		// gentle camera parallax
		pointerNdc.x += (pointerTarget.x - pointerNdc.x) * Math.min(1, dt * 3.2);
		pointerNdc.y += (pointerTarget.y - pointerNdc.y) * Math.min(1, dt * 3.2);
		camera.position.x = pointerNdc.x * PARALLAX_X;
		camera.position.y = CAM_Y + pointerNdc.y * PARALLAX_Y;
		camera.lookAt(target);
	}

	function frame(now: number) {
		if (!running) return;
		raf = requestAnimationFrame(frame);
		const dt = Math.min((now - last) / 1000, 0.05);
		last = now;
		step(dt);
		renderer.render(scene, camera);
	}

	function syncPlayback() {
		const should = inView && !document.hidden && !reduced;
		if (should && !running) {
			running = true;
			last = performance.now();
			raf = requestAnimationFrame(frame);
		} else if (!should && running) {
			running = false;
			cancelAnimationFrame(raf);
		}
	}

	if (reduced) {
		step(0); // settle camera, render one static frame
		renderer.render(scene, camera);
	} else {
		syncPlayback();
	}

	/* ---------------- teardown ---------------- */

	return {
		destroy() {
			running = false;
			cancelAnimationFrame(raf);
			window.removeEventListener('pointermove', onPointerMove);
			document.removeEventListener('visibilitychange', onVisibility);
			resizeObs.disconnect();
			visObs.disconnect();
			grid.geometry.dispose();
			material.dispose();
			renderer.dispose();
			renderer.forceContextLoss();
			renderer.domElement.remove();
		}
	};
}
