import { describe, expect, it } from "vitest";
import { createSeededRandom, buildStarLayer } from "./starfield";

describe("createSeededRandom", () => {
  it("same seed produces the same sequence", () => {
    const a = createSeededRandom(42);
    const b = createSeededRandom(42);
    expect(a()).toBe(b());
    expect(a()).toBe(b());
  });

  it("different seeds produce different first values", () => {
    const a = createSeededRandom(1);
    const b = createSeededRandom(2);
    expect(a()).not.toBe(b());
  });

  it("returns values in [0, 1)", () => {
    const r = createSeededRandom(7);
    for (let i = 0; i < 200; i++) {
      const v = r();
      expect(v).toBeGreaterThanOrEqual(0);
      expect(v).toBeLessThan(1);
    }
  });
});

describe("buildStarLayer", () => {
  it("produces count*3 floats for positions and colors", () => {
    const layer = buildStarLayer({ count: 100, minR: 10, maxR: 50, seed: 1 });
    expect(layer.positions.length).toBe(300);
    expect(layer.colors.length).toBe(300);
  });

  it("keeps every point within [minR, maxR]", () => {
    const layer = buildStarLayer({ count: 500, minR: 10, maxR: 50, seed: 3 });
    for (let i = 0; i < 500; i++) {
      const x = layer.positions[i * 3];
      const y = layer.positions[i * 3 + 1];
      const z = layer.positions[i * 3 + 2];
      const r = Math.sqrt(x * x + y * y + z * z);
      expect(r).toBeGreaterThanOrEqual(10);
      expect(r).toBeLessThanOrEqual(50);
    }
  });

  it("is deterministic for the same seed", () => {
    const a = buildStarLayer({ count: 50, minR: 10, maxR: 50, seed: 9 });
    const b = buildStarLayer({ count: 50, minR: 10, maxR: 50, seed: 9 });
    expect(Array.from(a.positions)).toEqual(Array.from(b.positions));
  });

  it("color components stay within [0, 1]", () => {
    const layer = buildStarLayer({ count: 100, minR: 10, maxR: 50, seed: 5 });
    for (const v of Array.from(layer.colors)) {
      expect(v).toBeGreaterThanOrEqual(0);
      expect(v).toBeLessThanOrEqual(1);
    }
  });

  it("produces varying star brightness", () => {
    const layer = buildStarLayer({ count: 100, minR: 10, maxR: 50, seed: 5 });
    const colors = Array.from(layer.colors);
    // At least two stars differ in brightness: some color component deviates clearly from the first color vector
    const hasVariation = colors.some((v) => Math.abs(v - colors[0]) > 0.2);
    expect(hasVariation).toBe(true);
  });
});
