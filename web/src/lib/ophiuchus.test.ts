import { describe, expect, it } from "vitest";
import {
  OPHIUCHUS_LINES,
  OPHIUCHUS_STARS,
  buildOphiuchus,
  raDecToVec3,
} from "./ophiuchus";

describe("raDecToVec3", () => {
  it("maps ra=0, dec=0 to the +X axis", () => {
    const v = raDecToVec3(0, 0, 10);
    expect(v.x).toBeCloseTo(10, 5);
    expect(v.y).toBeCloseTo(0, 5);
    expect(v.z).toBeCloseTo(0, 5);
  });

  it("maps dec=90 to the +Y axis", () => {
    const v = raDecToVec3(0, 90, 10);
    expect(v.x).toBeCloseTo(0, 5);
    expect(v.y).toBeCloseTo(10, 5);
    expect(v.z).toBeCloseTo(0, 5);
  });

  it("returns points at the requested radius", () => {
    const v = raDecToVec3(120, 30, 25);
    const r = Math.sqrt(v.x * v.x + v.y * v.y + v.z * v.z);
    expect(r).toBeCloseTo(25, 5);
  });
});

describe("OPHIUCHUS_STARS", () => {
  it("has unique ids", () => {
    const ids = OPHIUCHUS_STARS.map((s) => s.id);
    expect(new Set(ids).size).toBe(ids.length);
  });

  it("has valid coordinate ranges", () => {
    for (const s of OPHIUCHUS_STARS) {
      expect(s.ra).toBeGreaterThanOrEqual(0);
      expect(s.ra).toBeLessThan(360);
      expect(s.dec).toBeGreaterThanOrEqual(-90);
      expect(s.dec).toBeLessThanOrEqual(90);
    }
  });
});

describe("OPHIUCHUS_LINES", () => {
  it("references valid, distinct star indices", () => {
    for (const [a, b] of OPHIUCHUS_LINES) {
      expect(a).toBeGreaterThanOrEqual(0);
      expect(a).toBeLessThan(OPHIUCHUS_STARS.length);
      expect(b).toBeGreaterThanOrEqual(0);
      expect(b).toBeLessThan(OPHIUCHUS_STARS.length);
      expect(a).not.toBe(b);
    }
  });
});

describe("buildOphiuchus", () => {
  it("produces one position per star", () => {
    const { positions } = buildOphiuchus(30);
    expect(positions.length).toBe(OPHIUCHUS_STARS.length);
  });

  it("centers the constellation near the origin", () => {
    const { positions } = buildOphiuchus(30);
    const cx = positions.reduce((s, p) => s + p.x, 0) / positions.length;
    const cy = positions.reduce((s, p) => s + p.y, 0) / positions.length;
    expect(cx).toBeCloseTo(0, 1);
    expect(cy).toBeCloseTo(0, 1);
  });

  it("spans a reasonable size for framing", () => {
    const { positions } = buildOphiuchus(30);
    const xs = positions.map((p) => p.x);
    const ys = positions.map((p) => p.y);
    const width = Math.max(...xs) - Math.min(...xs);
    const height = Math.max(...ys) - Math.min(...ys);
    expect(width).toBeGreaterThan(5);
    expect(height).toBeGreaterThan(5);
  });

  it("places Rasalhague (north, alpha) above Sabik (south, eta)", () => {
    const { positions } = buildOphiuchus(30);
    const alphaIdx = OPHIUCHUS_STARS.findIndex((s) => s.id === "alpha");
    const etaIdx = OPHIUCHUS_STARS.findIndex((s) => s.id === "eta");
    expect(positions[alphaIdx].y).toBeGreaterThan(positions[etaIdx].y);
  });
});
