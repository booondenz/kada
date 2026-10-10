import { useEffect, useRef } from "react";
import * as THREE from "three";
import { buildStarLayer } from "@/lib/starfield";
import { buildOphiuchus } from "@/lib/ophiuchus";

const BRAND = 0x4f46e5;

function makeGlowTexture(inner: string): THREE.Texture {
  const size = 128;
  const canvas = document.createElement("canvas");
  canvas.width = size;
  canvas.height = size;
  const ctx = canvas.getContext("2d");
  if (!ctx) throw new Error("2d context unavailable");
  const g = ctx.createRadialGradient(size / 2, size / 2, 0, size / 2, size / 2, size / 2);
  g.addColorStop(0, inner);
  g.addColorStop(1, "rgba(0,0,0,0)");
  ctx.fillStyle = g;
  ctx.fillRect(0, 0, size, size);
  return new THREE.CanvasTexture(canvas);
}

function disposeObject(root: THREE.Object3D) {
  root.traverse((child) => {
    const geometry = (child as { geometry?: THREE.BufferGeometry }).geometry;
    if (geometry) geometry.dispose();
    const material = (child as { material?: THREE.Material | THREE.Material[] }).material;
    if (Array.isArray(material)) material.forEach((m) => m.dispose());
    else if (material) material.dispose();
  });
}

export default function StarfieldCanvas({ className }: { className?: string }) {
  const canvasRef = useRef<HTMLCanvasElement>(null);

  useEffect(() => {
    const canvas = canvasRef.current;
    if (!canvas) return;

    let renderer: THREE.WebGLRenderer;
    try {
      renderer = new THREE.WebGLRenderer({ canvas, antialias: true, alpha: true });
    } catch (err) {
      console.error("[StarfieldCanvas] WebGL unavailable:", err);
      return;
    }

    const reduced = window.matchMedia("(prefers-reduced-motion: reduce)").matches;
    const scene = new THREE.Scene();
    const camera = new THREE.PerspectiveCamera(60, 1, 0.1, 600);
    camera.position.set(0, 0, 36);
    camera.lookAt(0, 0, 0);

    renderer.setPixelRatio(Math.min(window.devicePixelRatio, 2));

    // ---- Far stars: large shell enclosing the camera, slowly rotating ----
    const far = buildStarLayer({ count: 1500, minR: 45, maxR: 140, seed: 101 });
    const farGeo = new THREE.BufferGeometry();
    farGeo.setAttribute("position", new THREE.BufferAttribute(far.positions, 3));
    farGeo.setAttribute("color", new THREE.BufferAttribute(far.colors, 3));
    const farPoints = new THREE.Points(
      farGeo,
      new THREE.PointsMaterial({
        size: 1.4,
        sizeAttenuation: false,
        vertexColors: true,
        transparent: true,
        opacity: 0.9,
        depthWrite: false,
        blending: THREE.AdditiveBlending,
      }),
    );
    const farGroup = new THREE.Group();
    farGroup.add(farPoints);
    scene.add(farGroup);

    // ---- Near stars: closer to the camera, stronger parallax ----
    const near = buildStarLayer({ count: 240, minR: 12, maxR: 34, seed: 202 });
    const nearGeo = new THREE.BufferGeometry();
    nearGeo.setAttribute("position", new THREE.BufferAttribute(near.positions, 3));
    nearGeo.setAttribute("color", new THREE.BufferAttribute(near.colors, 3));
    const nearMat = new THREE.PointsMaterial({
      size: 2.0,
      sizeAttenuation: false,
      vertexColors: true,
      transparent: true,
      opacity: 0.85,
      depthWrite: false,
      blending: THREE.AdditiveBlending,
    });
    const nearPoints = new THREE.Points(nearGeo, nearMat);
    scene.add(nearPoints);

    // ---- Ophiuchus constellation: star points + glow sprites + connecting lines ----
    const constellation = new THREE.Group();
    const oph = buildOphiuchus(45, 1.5);
    const starPos = new Float32Array(oph.positions.length * 3);
    oph.positions.forEach((p, i) => {
      starPos[i * 3] = p.x;
      starPos[i * 3 + 1] = p.y;
      starPos[i * 3 + 2] = p.z;
    });
    const ophGeo = new THREE.BufferGeometry();
    ophGeo.setAttribute("position", new THREE.BufferAttribute(starPos, 3));
    const ophPoints = new THREE.Points(
      ophGeo,
      new THREE.PointsMaterial({
        size: 2.6,
        sizeAttenuation: false,
        color: 0xffffff,
        transparent: true,
        depthWrite: false,
        blending: THREE.AdditiveBlending,
      }),
    );
    constellation.add(ophPoints);

    const glowTex = makeGlowTexture("rgba(140,160,255,0.85)");
    for (const p of oph.positions) {
      const sprite = new THREE.Sprite(
        new THREE.SpriteMaterial({
          map: glowTex,
          color: 0xaabbff,
          transparent: true,
          opacity: 0.55,
          depthWrite: false,
          blending: THREE.AdditiveBlending,
        }),
      );
      sprite.position.set(p.x, p.y, p.z);
      sprite.scale.setScalar(3.2);
      constellation.add(sprite);
    }

    const linePos = new Float32Array(oph.lines.length * 2 * 3);
    oph.lines.forEach(([a, b], i) => {
      linePos[i * 6] = oph.positions[a].x;
      linePos[i * 6 + 1] = oph.positions[a].y;
      linePos[i * 6 + 2] = oph.positions[a].z;
      linePos[i * 6 + 3] = oph.positions[b].x;
      linePos[i * 6 + 4] = oph.positions[b].y;
      linePos[i * 6 + 5] = oph.positions[b].z;
    });
    const lineGeo = new THREE.BufferGeometry();
    lineGeo.setAttribute("position", new THREE.BufferAttribute(linePos, 3));
    const lines = new THREE.LineSegments(
      lineGeo,
      new THREE.LineBasicMaterial({
        color: BRAND,
        transparent: true,
        opacity: 0.55,
        blending: THREE.AdditiveBlending,
        depthWrite: false,
      }),
    );
    constellation.add(lines);
    scene.add(constellation);

    // ---- Background nebula glow ----
    const nebulaTex = makeGlowTexture("rgba(79,70,229,0.28)");
    const nebula = new THREE.Sprite(
      new THREE.SpriteMaterial({
        map: nebulaTex,
        transparent: true,
        opacity: 0.35,
        depthWrite: false,
        blending: THREE.AdditiveBlending,
      }),
    );
    nebula.position.set(0, 0, -12);
    nebula.scale.setScalar(70);
    scene.add(nebula);

    const nebulaTex2 = makeGlowTexture("rgba(99,102,241,0.2)");
    const nebula2 = new THREE.Sprite(
      new THREE.SpriteMaterial({
        map: nebulaTex2,
        transparent: true,
        opacity: 0.25,
        depthWrite: false,
        blending: THREE.AdditiveBlending,
      }),
    );
    nebula2.position.set(22, -14, -20);
    nebula2.scale.setScalar(46);
    scene.add(nebula2);

    // ---- Mouse parallax ----
    let targetX = 0;
    let targetY = 0;
    let curX = 0;
    let curY = 0;
    const onPointerMove = (e: PointerEvent) => {
      targetX = (e.clientX / window.innerWidth) * 2 - 1;
      targetY = (e.clientY / window.innerHeight) * 2 - 1;
    };
    if (!reduced) window.addEventListener("pointermove", onPointerMove);

    // ---- resize ----
    const resize = () => {
      const w = canvas.clientWidth || window.innerWidth;
      const h = canvas.clientHeight || window.innerHeight;
      camera.aspect = w / h;
      camera.updateProjectionMatrix();
      renderer.setSize(w, h, false);
    };
    resize();
    window.addEventListener("resize", resize);

    // ---- Animation loop ----
    const clock = new THREE.Clock();
    let raf = 0;
    const tick = () => {
      const t = clock.getElapsedTime();
      curX += (targetX * 2.2 - curX) * 0.05;
      curY += (targetY * 1.6 - curY) * 0.05;
      camera.position.x = curX;
      camera.position.y = curY;
      camera.lookAt(0, 0, 0);

      farGroup.rotation.y += 0.00006;
      nearMat.size = 2.0 + Math.sin(t * 1.2) * 0.15;
      constellation.rotation.z = Math.sin(t * 0.15) * 0.03;
      constellation.rotation.y = Math.sin(t * 0.1) * 0.05;

      renderer.render(scene, camera);
      raf = requestAnimationFrame(tick);
    };

    if (reduced) {
      renderer.render(scene, camera); // single static frame
    } else {
      raf = requestAnimationFrame(tick);
    }

    return () => {
      cancelAnimationFrame(raf);
      window.removeEventListener("pointermove", onPointerMove);
      window.removeEventListener("resize", resize);
      glowTex.dispose();
      nebulaTex.dispose();
      nebulaTex2.dispose();
      disposeObject(scene);
      renderer.dispose();
    };
  }, []);

  return <canvas ref={canvasRef} className={className} aria-hidden="true" />;
}
