// Dumps the computed styles of specific elements, so a visual defect can be traced to the actual cascade
// rather than guessed at. Reads the selectors passed as arguments.
//
//   node web/scripts/inspect-elements.mjs /login "button"
import { spawn } from "node:child_process";
import { mkdirSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

const appPort = "3000";
const debugPort = 9223;
const EDGE = "C:\\Program Files (x86)\\Microsoft\\Edge\\Application\\msedge.exe";
const profile = join(tmpdir(), `kada-inspect-${Date.now()}`);
mkdirSync(profile, { recursive: true });

const route = process.argv[2] ?? "/login";
const selector = process.argv[3] ?? "button";

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
  await send("Page.navigate", { url: `http://localhost:${appPort}${route}` });
  for (let i = 0; i < 40; i++) {
    await sleep(250);
    const r = await send("Runtime.evaluate", { expression: "document.readyState==='complete'", returnByValue: true });
    if (r.result?.value) break;
  }
  await sleep(500);

  await send("Runtime.evaluate", {
    expression: "localStorage.setItem('kada.theme','dark')",
  });
  await send("Page.navigate", { url: `http://localhost:${appPort}${route}` });
  await sleep(1500);

  const dump = await send("Runtime.evaluate", {
    expression: `(() => {
      const nodes = [...document.querySelectorAll(${JSON.stringify(selector)})];
      return JSON.stringify(nodes.slice(0, 8).map((el) => {
        const cs = getComputedStyle(el);
        return {
          text: (el.textContent || '').trim().slice(0, 24),
          className: el.className.toString().slice(0, 120),
          backgroundColor: cs.backgroundColor,
          color: cs.color,
          borderColor: cs.borderColor,
          opacity: cs.opacity,
          display: cs.display,
          visibility: cs.visibility,
        };
      }), null, 2);
    })()`,
    returnByValue: true,
  });

  console.log(`route ${route}  selector ${selector}`);
  console.log(dump.result.value);
}

try {
  await main();
} finally {
  edge.kill();
}
