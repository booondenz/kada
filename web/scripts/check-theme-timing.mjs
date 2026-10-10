// Measures when `data-theme` is applied relative to the document lifecycle, to check the no-flash claim
// rather than assume it. A theme that is set only after `DOMContentLoaded` or after first paint means a
// light flash before the dark paint.
//
//   node web/scripts/check-theme-timing.mjs
import { spawn } from "node:child_process";
import { mkdirSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

const appPort = process.argv[2] ?? "3000";
const debugPort = 9224;
const EDGE = "C:\\Program Files (x86)\\Microsoft\\Edge\\Application\\msedge.exe";
const profile = join(tmpdir(), `kada-timing-${Date.now()}`);
mkdirSync(profile, { recursive: true });

const edge = spawn(
  EDGE,
  [
    "--headless=new",
    `--remote-debugging-port=${debugPort}`,
    `--user-data-dir=${profile}`,
    "--no-first-run",
    "--no-default-browser-check",
    "--disable-gpu",
    "--window-size=1280,900",
    "about:blank",
  ],
  { stdio: "ignore" }
);

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

async function main() {
  for (let i = 0; i < 60; i++) {
    try {
      if ((await fetch(`http://127.0.0.1:${debugPort}/json/version`)).ok) break;
    } catch {}
    await sleep(250);
  }

  const targets = await (await fetch(`http://127.0.0.1:${debugPort}/json/list`)).json();
  const page = targets.find((t) => t.type === "page");
  const ws = new WebSocket(page.webSocketDebuggerUrl);
  await new Promise((res, rej) => {
    ws.addEventListener("open", res, { once: true });
    ws.addEventListener("error", rej, { once: true });
  });

  let id = 0;
  const pending = new Map();
  ws.addEventListener("message", (e) => {
    const m = JSON.parse(e.data);
    const p = pending.get(m.id);
    if (p) {
      pending.delete(m.id);
      m.error ? p.reject(new Error(JSON.stringify(m.error))) : p.resolve(m.result);
    }
  });
  const send = (method, params = {}) =>
    new Promise((resolve, reject) => {
      pending.set(++id, { resolve, reject });
      ws.send(JSON.stringify({ id, method, params }));
    });

  await send("Page.enable");
  await send("Runtime.enable");

  // Dark browser preference, no stored choice: the theme must come from the media query alone.
  await send("Emulation.setEmulatedMedia", {
    features: [{ name: "prefers-color-scheme", value: "dark" }],
  });

  // Record the lifecycle, then navigate into a document that already has the recorder installed.
  // Note: at addScriptToEvaluateOnNewDocument time there is no documentElement yet, so the observer is
  // attached once <html> exists rather than immediately - attaching earlier silently observes nothing.
  await send("Page.addScriptToEvaluateOnNewDocument", {
    source: `
      window.__themeTiming = { events: [], themeAtEvent: undefined };
      const rec = (name) => window.__themeTiming.events.push([name, performance.now()]);
      const note = () => {
        window.__themeTiming.themeAtEvent = document.documentElement.getAttribute('data-theme');
      };
      rec('script-start');
      const watch = () => {
        if (!document.documentElement) return;
        if (document.documentElement.getAttribute('data-theme')) rec('data-theme-already-set');
        new MutationObserver(() => { note(); rec('data-theme-set'); })
          .observe(document.documentElement, { attributes: true, attributeFilter: ['data-theme'] });
      };
      watch();
      // <html> may not exist yet; retry on the next microtask frames until it does.
      let tries = 0;
      const tick = () => { if (!document.documentElement && tries++ < 200) requestAnimationFrame(tick); else watch(); };
      tick();
      document.addEventListener('readystatechange', () => rec('readystate:' + document.readyState));
      document.addEventListener('DOMContentLoaded', () => { note(); rec('DOMContentLoaded'); });
      window.addEventListener('load', () => { note(); rec('load'); });
      requestAnimationFrame(() => { note(); rec('first-animation-frame'); });
    `,
  });

  await send("Page.navigate", { url: `http://localhost:${appPort}/login` });
  await sleep(3000);

  const res = await send("Runtime.evaluate", {
    expression: "JSON.stringify(window.__themeTiming)",
    returnByValue: true,
  });

  const timing = JSON.parse(res.result.value);
  console.log("event timeline (ms since document start):");
  for (const [name, t] of timing.events) console.log(`  ${String(Math.round(t)).padStart(6)}  ${name}`);

  // Independent of the recorder, so a broken probe cannot look like a passing theme.
  const state = JSON.parse(
    (
      await send("Runtime.evaluate", {
        expression: `JSON.stringify({ ready: document.readyState, theme: document.documentElement.getAttribute('data-theme'), bg: getComputedStyle(document.body).backgroundColor })`,
        returnByValue: true,
      })
    ).result.value
  );
  console.log(`\ndocument: readyState=${state.ready} data-theme=${state.theme} bodyBackground=${state.bg}`);

  const set = timing.events.find(([n]) => n === "data-theme-set" || n === "data-theme-already-set");
  const firstFrame = timing.events.find(([n]) => n === "first-animation-frame");
  const domReady = timing.events.find(([n]) => n.startsWith("readystate:interactive"));

  if (state.theme !== "dark") {
    console.log(`\nFAIL  expected data-theme=dark from the browser preference, got ${state.theme}`);
    process.exitCode = 1;
  } else if (!set) {
    console.log("\nFAIL  the recorder never saw data-theme change");
    process.exitCode = 1;
  } else if (firstFrame && set[1] > firstFrame[1]) {
    console.log(`\nFAIL  the theme was applied ${Math.round(set[1] - firstFrame[1])}ms after the first frame - a flash`);
    process.exitCode = 1;
  } else {
    console.log(
      `\nok    data-theme was applied at ${Math.round(set[1])}ms (${set[0]})` +
        (firstFrame ? `, before the first frame (${Math.round(firstFrame[1])}ms)` : "") +
        (domReady ? ` and before DOMContentLoaded at ${Math.round(domReady[1])}ms` : "")
    );
  }
}

try {
  await main();
} finally {
  edge.kill();
}
