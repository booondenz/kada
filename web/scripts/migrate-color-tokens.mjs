// Rewrites the frontend's literal grey utilities to semantic tokens, so the light appearance is unchanged
// and dark mode is expressible without a `dark:` variant on every element.
//
// Only the 12 classes reported by report-color-usage.mjs as "replace" are touched, together with their
// state and breakpoint variants. Anything already correct under the inverted scales (text-red-600,
// bg-indigo-50, ...) is left alone, as are black overlays and the neutral-* usages.
//
//   node web/scripts/migrate-color-tokens.mjs [--dry-run]
import { readdirSync, readFileSync, statSync, writeFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { dirname, join } from "node:path";

const here = dirname(fileURLToPath(import.meta.url));
const srcDir = join(here, "..", "src");
const dryRun = process.argv.includes("--dry-run");

// Only the neutral roles the dark theme re-points. Brand and status colours are fixed in both themes, so
// they are not here: `bg-indigo-600 text-white` has to keep meaning the same pair in both.
const MAP = {
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

// Prefixes a utility can carry. Each is rewritten in place so `hover:bg-white` becomes `hover:bg-canvas`.
const VARIANTS = [
  "hover", "focus", "focus-visible", "focus-within", "active", "disabled", "checked",
  "group-hover", "group-focus", "peer-checked", "first", "last", "odd", "even",
  "sm", "md", "lg", "xl", "2xl", "dark", "placeholder", "file", "before", "after",
];

function walk(dir) {
  return readdirSync(dir).flatMap((entry) => {
    const full = join(dir, entry);
    return statSync(full).isDirectory() ? walk(full) : [full];
  });
}

// A left boundary of start-of-string, whitespace, quote, backtick or brace - never a letter or hyphen, so
// `group-hover:bg-white` is matched as a whole and `not-bg-white` would not be matched at all.
//
// The trailing lookahead rejects an opacity modifier: `bg-white/10` and `bg-white/[0.04]` are literal
// white at low alpha (the frosted-glass panels and the dark hero), not "this theme's surface". Rewriting
// those to a themed colour turns a white wash over a dark background into a dark one. `text-white` is not
// in the map either, for the same reason: it is used for text on brand-filled buttons and on the
// permanently dark landing and auth screens, and inverting it would make all of those unreadable.
const boundary = `(?<=^|[\\s"'\`{])`;
const keys = Object.keys(MAP).sort((a, b) => b.length - a.length);
const base = keys.map((k) => k.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")).join("|");
const variantAlt = VARIANTS.map((v) => v.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")).join("|");
const pattern = new RegExp(`${boundary}((?:${variantAlt}):)*(${base})(?![\\w-]|\\/)`, "g");

let totalFiles = 0;
let totalReplacements = 0;
const perClass = new Map();

for (const file of walk(srcDir)) {
  if (!/\.(tsx|ts)$/.test(file)) continue;
  const before = readFileSync(file, "utf8");
  const after = before.replace(pattern, (_match, prefix = "", cls) => {
    totalReplacements++;
    perClass.set(cls, (perClass.get(cls) ?? 0) + 1);
    return `${prefix}${MAP[cls]}`;
  });

  if (after !== before) {
    totalFiles++;
    const rel = file.slice(srcDir.length + 1);
    console.log(`${dryRun ? "would rewrite" : "rewrote"} ${rel}`);
    if (!dryRun) writeFileSync(file, after, "utf8");
  }
}

console.log(`\n${dryRun ? "(dry run) " : ""}${totalReplacements} replacements in ${totalFiles} files`);
console.log("\nper class:");
for (const [cls, n] of [...perClass.entries()].sort((a, b) => b[1] - a[1])) {
  console.log(`${String(n).padStart(4)}  ${cls} -> ${MAP[cls]}`);
}
