// Inventories the colour utilities used in the frontend and classifies each one against the dark theme,
// so the conversion work is measured rather than guessed:
//
//   replace - a neutral role: the shade means "this theme's background/text/border", so it becomes a
//             semantic token that the dark theme re-points
//   fixed   - brand and status colours, which keep their value in both themes on purpose. `bg-indigo-600`
//             is always paired with `text-white`, so brightening it for a dark canvas would make the
//             primary buttons unreadable
//
//   node web/scripts/report-color-usage.mjs
import { readdirSync, readFileSync, statSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { dirname, join } from "node:path";

const here = dirname(fileURLToPath(import.meta.url));
const srcDir = join(here, "..", "src");

function walk(dir) {
  return readdirSync(dir).flatMap((entry) => {
    const full = join(dir, entry);
    return statSync(full).isDirectory() ? walk(full) : [full];
  });
}

// A surface/text role the palette cannot express on its own: the shade is "this theme's background", and
// what it should become depends on the theme rather than on the shade.
const ROLE_MAP = {
  "bg-white": "bg-canvas",
  "text-gray-900": "text-strong",
  "text-gray-800": "text-strong",
  "text-gray-700": "text-body",
  "text-gray-600": "text-muted",
  "text-gray-500": "text-muted",
  "text-gray-400": "text-faint",
  "border-gray-200": "border-line",
  "border-gray-100": "border-line",
  "border-gray-300": "border-line-strong",
  "bg-gray-100": "bg-muted-surface",
  "bg-gray-200": "bg-raised",
};

const files = walk(srcDir).filter((f) => /\.(tsx|ts)$/.test(f));
const pattern =
  /\b(?:bg|text|border|from|to|via|ring|divide|placeholder|outline|decoration|shadow|accent|caret|fill|stroke)-(?:white|black|gray|slate|zinc|neutral|stone|indigo|blue|red|green|amber|yellow|emerald|rose|sky|violet|purple|teal|cyan|orange|lime|fuchsia|pink)-?\d{0,3}\b|\b(?:bg|text|border)-(?:white|black)\b/g;

const counts = new Map();
const roleByFile = new Map();

for (const file of files) {
  const text = readFileSync(file, "utf8");
  for (const match of text.matchAll(pattern)) {
    const cls = match[0];
    counts.set(cls, (counts.get(cls) ?? 0) + 1);
    if (ROLE_MAP[cls]) {
      const rel = file.slice(srcDir.length + 1);
      if (!roleByFile.has(rel)) roleByFile.set(rel, new Map());
      const m = roleByFile.get(rel);
      m.set(cls, (m.get(cls) ?? 0) + 1);
    }
  }
}

function classify(cls) {
  return cls in ROLE_MAP ? "replace" : "fixed";
}

const byCategory = { replace: 0, fixed: 0 };
for (const [cls, n] of counts) byCategory[classify(cls)] += n;

console.log("=== totals by category ===");
for (const [k, v] of Object.entries(byCategory)) console.log(`${k.padEnd(9)} ${v}`);
console.log(`\ndistinct utilities: ${counts.size}, total uses: ${[...counts.values()].reduce((a, b) => a + b, 0)}`);

const replaces = [...counts.entries()].filter(([c]) => classify(c) === "replace").sort((a, b) => b[1] - a[1]);
console.log(`\n=== neutral roles needing a semantic token (${replaces.length} distinct) ===`);
for (const [cls, n] of replaces) console.log(`${String(n).padStart(4)}  ${cls.padEnd(20)} -> ${ROLE_MAP[cls]}`);

const fixed = [...counts.entries()].filter(([c]) => classify(c) === "fixed").sort((a, b) => b[1] - a[1]);
console.log(`\n=== fixed brand/status colours kept as they are (${fixed.length} distinct) ===`);
for (const [cls, n] of fixed.slice(0, 30)) console.log(`${String(n).padStart(4)}  ${cls}`);

console.log("\n=== per file: neutral roles to convert ===");
for (const [file, m] of [...roleByFile.entries()].sort((a, b) => [...b[1].values()].reduce((x, y) => x + y) - [...a[1].values()].reduce((x, y) => x + y))) {
  const total = [...m.values()].reduce((a, b) => a + b, 0);
  console.log(`${String(total).padStart(4)}  ${file}  (${[...m.keys()].join(", ")})`);
}
