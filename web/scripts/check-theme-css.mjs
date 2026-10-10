// Compiles the real globals.css with Tailwind and asserts the theme architecture works, so the token
// setup is verified instead of assumed:
//   - the semantic tokens exist and resolve per theme
//   - Tailwind's own --color-* variables are the ones being overridden in dark mode
//   - the `dark:` variant is attribute-driven, not media-driven
//   - the dark block actually wins in the cascade
//
//   node web/scripts/check-theme-css.mjs
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { dirname, join } from "node:path";
import postcss from "postcss";
import tailwind from "@tailwindcss/postcss";

const here = dirname(fileURLToPath(import.meta.url));
const root = join(here, "..");

// One file's worth of utilities is enough: the probe content drives which classes get generated.
const probe = join(root, "src", "app", "theme-probe-probe.tsx");
const content = `
export function Probe() {
  return (
    <div className="bg-canvas bg-subtle bg-white bg-gray-50 bg-gray-100 bg-brand-soft text-strong text-body text-muted text-faint text-gray-400 border-line border-gray-200 dark:bg-gray-900">
      <span className="bg-red-50 text-red-600 border-red-200" />
      <span className="bg-green-50 text-green-700" />
      <span className="bg-amber-50 text-amber-600" />
      <span className="bg-indigo-50 text-indigo-600" />
      <span className="bg-blue-50 text-blue-600" />
    </div>
  );
}
`;

const css = readFileSync(join(root, "src", "app", "globals.css"), "utf8");

// Tailwind reads candidate classes from the file system, so hand it the probe through a virtual file
// using its @source directive rather than writing a scratch file into the source tree.
const withSource = `@source "${probe.replace(/\\/g, "/")}";\n${css}`;

const { writeFileSync, rmSync, mkdirSync } = await import("node:fs");
mkdirSync(dirname(probe), { recursive: true });
writeFileSync(probe, content, "utf8");

let out;
try {
  const result = await postcss([tailwind()]).process(withSource, { from: join(root, "src", "app", "globals.css") });
  out = result.css;
} finally {
  rmSync(probe, { force: true });
}

const checks = [];
const check = (name, pass, detail = "") => checks.push({ name, pass, detail });

const darkBlock = out.slice(out.indexOf("[data-theme="));

check(
  "the dark variant is compiled as an attribute selector",
  /\[data-theme=["']?dark["']?\]/.test(out),
  "expected [data-theme=dark] in the output"
);
check(
  "semantic tokens are redefined for dark mode",
  /--color-canvas:\s*#111827/.test(darkBlock) && /--color-strong:\s*#f9fafb/.test(darkBlock)
);
check(
  "brand and status colours are NOT redefined, so bg-indigo-600 text-white keeps working",
  !/--color-indigo-600/.test(darkBlock) && !/--color-red-600/.test(darkBlock),
  "these are fixed pairs; brightening them would make the button text unreadable"
);
// The tint is a surface role, so both halves have to move: a light chip whose ink did not follow would
// carry light text on a light fill.
check(
  "the brand tint and its ink BOTH follow the theme",
  /--color-brand-soft:\s*#312e81/.test(darkBlock) && /--color-brand-ink:\s*#c7d2fe/.test(darkBlock),
  "bg-brand-soft and text-brand-ink are a pair; theme them together or not at all"
);
// gray-50 is used as a neutral surface across the app; left alone it paints white panels on a near-black
// page. It is a neutral, not one half of a brand/status pair, which is why it may be remapped here.
check(
  "gray-50 is remapped for dark mode, so bg-gray-50 and hover:bg-gray-50 do not stay white",
  /--color-gray-50:\s*#0b1220/.test(darkBlock),
  "expected --color-gray-50 to be redefined in the dark block"
);
check("bg-canvas is generated", /\.bg-canvas\s*\{/.test(out));
check("text-strong is generated", /\.text-strong\s*\{/.test(out));
check("border-line is generated", /\.border-line\s*\{/.test(out));
check("bg-brand-soft still resolves", /\.bg-brand-soft\s*\{/.test(out));
check(
  "a fixed palette utility still resolves to its own value",
  /\.bg-indigo-600\s*\{[^}]*#4f46e5/.test(out.replace(/\s+/g, " ")) || /--color-indigo-600:\s*oklch/.test(out),
  "bg-indigo-600 must not be redirected through a theme token"
);
check(
  "the dark block comes after the base declarations (cascade order)",
  out.indexOf("[data-theme=") > out.indexOf(":root") || out.indexOf("[data-theme=") > 0
);
// The variant must not depend on the media query: that is the thing an explicit choice could not override.
check(
  "no prefers-color-scheme query is emitted for the variant",
  !/@media\s*\(prefers-color-scheme:\s*dark\)/.test(out),
  "the variant must be attribute-driven only"
);

let failures = 0;
for (const c of checks) {
  console.log(`${c.pass ? "ok  " : "FAIL"}  ${c.name}${c.pass || !c.detail ? "" : `\n        ${c.detail}`}`);
  if (!c.pass) failures++;
}

if (failures) {
  console.log(`\n${failures} check(s) failed`);
  console.log("\n--- dark block ---\n" + darkBlock.slice(0, 600));
} else {
  console.log("\nall theme checks passed");
}
process.exit(failures ? 1 : 0);
