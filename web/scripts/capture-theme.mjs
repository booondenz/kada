// Captures the app in both themes by driving Edge over the DevTools protocol, so the theme and the flag
// switcher are verified by looking at them rather than by assuming the classes resolve.
//
// The stored preference is written with Runtime.evaluate (same origin, so localStorage is shared), then
// the page is reloaded - which is exactly the path a returning visitor takes: ThemeScript must apply the
// theme during parsing or a light flash is visible before the dark paint.
//
//   node web/scripts/capture-theme.mjs [--port 3000]
//
// Writes PNGs into web/.theme-shots/.
import { spawn } from "node:child_process";
import { mkdirSync, writeFileSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { fileURLToPath } from "node:url";
import { dirname, join } from "node:path";

const here = dirname(fileURLToPath(import.meta.url));
const outDir = join(here, "..", ".theme-shots");
rmSync(outDir, { recursive: true, force: true });
mkdirSync(outDir, { recursive: true });

const portArg = process.argv.indexOf("--port");
const appPort = portArg > -1 ? process.argv[portArg + 1] : "3000";
const appOrigin = `http://localhost:${appPort}`;
const debugPort = 9222;

const EDGE = "C:\\Program Files (x86)\\Microsoft\\Edge\\Application\\msedge.exe";
// Kept out of the project: the dev server watches this tree, and a browser profile inside it makes
// Turbopack try to read files that Edge holds open (and fail with a sharing violation).
const profile = join(tmpdir(), `kada-theme-profile-${Date.now()}`);
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
    "--hide-scrollbars",
    "--window-size=1280,900",
    "about:blank",
  ],
  { stdio: "ignore" }
);

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

async function waitForDevTools() {
  for (let i = 0; i < 60; i++) {
    try {
      const res = await fetch(`http://127.0.0.1:${debugPort}/json/version`);
      if (res.ok) return;
    } catch {
      // not up yet
    }
    await sleep(250);
  }
  throw new Error("Edge DevTools endpoint never became reachable");
}

/** Minimal CDP client: one websocket, request id, awaited replies. */
class CDP {
  constructor(ws) {
    this.ws = ws;
    this.id = 0;
    this.pending = new Map();
    ws.addEventListener("message", (event) => {
      const msg = JSON.parse(event.data);
      const resolver = this.pending.get(msg.id);
      if (resolver) {
        this.pending.delete(msg.id);
        msg.error ? resolver.reject(new Error(JSON.stringify(msg.error))) : resolver.resolve(msg.result);
      }
    });
  }

  send(method, params = {}, sessionId) {
    const id = ++this.id;
    return new Promise((resolve, reject) => {
      this.pending.set(id, { resolve, reject });
      this.ws.send(JSON.stringify({ id, method, params, ...(sessionId ? { sessionId } : {}) }));
    });
  }
}

async function connect(url) {
  const ws = new WebSocket(url);
  await new Promise((resolve, reject) => {
    ws.addEventListener("open", resolve, { once: true });
    ws.addEventListener("error", reject, { once: true });
  });
  return new CDP(ws);
}

async function main() {
  await waitForDevTools();

  const targets = await (await fetch(`http://127.0.0.1:${debugPort}/json/list`)).json();
  const page = targets.find((t) => t.type === "page");
  const cdp = await connect(page.webSocketDebuggerUrl);

  await cdp.send("Page.enable");
  await cdp.send("Runtime.enable");
  await cdp.send("Emulation.setDeviceMetricsOverride", {
    width: 1280,
    height: 900,
    deviceScaleFactor: 1,
    mobile: false,
  });

  const shots = [];

  async function goto(url) {
    await cdp.send("Page.navigate", { url });
    // Network idle is not exposed without the Network domain; the app is small and local, so a settle
    // plus a readiness probe is enough and keeps this dependency-free.
    for (let i = 0; i < 40; i++) {
      await sleep(250);
      const ready = await cdp.send("Runtime.evaluate", {
        expression: "document.readyState === 'complete'",
        returnByValue: true,
      });
      if (ready.result?.value) break;
    }
    await sleep(600);
  }

  async function setStoredTheme(value) {
    await cdp.send("Runtime.evaluate", {
      expression: value === null
        ? "localStorage.removeItem('kada.theme')"
        : `localStorage.setItem('kada.theme', ${JSON.stringify(value)})`,
    });
  }

  /**
   * Satisfies AppLayout's guard, which sends the browser to /login when there is no user. The API calls
   * behind the shell will fail without a real session, which is fine: this is about how the shell paints,
   * and it means the dashboard rather than the sign-in page is what gets captured.
   */
  async function signInLocally() {
    await cdp.send("Runtime.evaluate", {
      expression: `(function(){
        try {
          localStorage.setItem('kada_token', 'capture-only');
          localStorage.setItem('kada_user', JSON.stringify({ id: 1, name: 'Kada', email: 'capture@example.com', created_at: '2026-01-01T00:00:00Z' }));
        } catch (e) {}
      })()`,
    });
  }

  async function capture(name) {
    const shot = await cdp.send("Page.captureScreenshot", { format: "png", captureBeyondViewport: true });
    const file = join(outDir, `${name}.png`);
    writeFileSync(file, Buffer.from(shot.data, "base64"));
    shots.push(`${name}.png`);
    console.log(`captured ${name}.png`);
    return file;
  }

  /** Reads what the page ended up with, so the capture is backed by the DOM and not only by pixels. */
  async function probe() {
    const res = await cdp.send("Runtime.evaluate", {
      expression: `JSON.stringify({
        theme: document.documentElement.getAttribute('data-theme'),
        colorScheme: getComputedStyle(document.documentElement).colorScheme,
        bodyBg: getComputedStyle(document.body).backgroundColor,
        bodyColor: getComputedStyle(document.body).color,
      })`,
      returnByValue: true,
    });
    return JSON.parse(res.result.value);
  }

  const report = [];

  // --- the language switcher on the landing page, both themes ---
  for (const theme of ["light", "dark"]) {
    await goto(appOrigin + "/");
    await setStoredTheme(theme);
    await goto(appOrigin + "/"); // reload: the stored value must win during parsing
    const state = await probe();
    report.push({ page: "/", requested: theme, ...state });
    await capture(`landing-${theme}`);
  }

  // --- the login screen (permanently dark by design, so it must not change) ---
  await goto(`${appOrigin}/login`);
  await setStoredTheme("dark");
  await goto(`${appOrigin}/login`);
  report.push({ page: "/login", requested: "dark", ...(await probe()) });
  await capture("login-dark");
  await setStoredTheme("light");
  await goto(`${appOrigin}/login`);
  report.push({ page: "/login", requested: "light", ...(await probe()) });
  await capture("login-light");

  // --- the dashboard shell: sidebar, top bar, language switcher ---
  await goto(appOrigin + "/dashboard");
  await signInLocally();
  await goto(`${appOrigin}/dashboard`);
  await setStoredTheme("dark");
  await goto(`${appOrigin}/dashboard`);
  report.push({ page: "/dashboard", requested: "dark", ...(await probe()) });
  await capture("dashboard-dark");
  await setStoredTheme("light");
  await goto(`${appOrigin}/dashboard`);
  report.push({ page: "/dashboard", requested: "light", ...(await probe()) });
  await capture("dashboard-light");

  // --- settings, which carries the appearance control ---
  await goto(`${appOrigin}/dashboard/settings`);
  await setStoredTheme("dark");
  await goto(`${appOrigin}/dashboard/settings`);
  report.push({ page: "/dashboard/settings", requested: "dark", ...(await probe()) });
  await capture("settings-dark");
  await setStoredTheme("light");
  await goto(`${appOrigin}/dashboard/settings`);
  report.push({ page: "/dashboard/settings", requested: "light", ...(await probe()) });
  await capture("settings-light");

  // --- no stored value: the theme must come from the browser preference ---
  await goto(`${appOrigin}/dashboard`);
  await setStoredTheme(null);
  await cdp.send("Emulation.setEmulatedMedia", {
    features: [{ name: "prefers-color-scheme", value: "dark" }],
  });
  await goto(`${appOrigin}/dashboard`);
  report.push({ page: "/dashboard", requested: "system:dark", ...(await probe()) });
  await capture("dashboard-system-dark");

  await cdp.send("Emulation.setEmulatedMedia", {
    features: [{ name: "prefers-color-scheme", value: "light" }],
  });
  await goto(`${appOrigin}/dashboard`);
  report.push({ page: "/dashboard", requested: "system:light", ...(await probe()) });
  await capture("dashboard-system-light");

  console.log("\n=== observed ===");
  for (const row of report) {
    console.log(
      `${row.page.padEnd(22)} asked=${String(row.requested).padEnd(12)} data-theme=${String(row.theme).padEnd(5)} color-scheme=${String(row.colorScheme).padEnd(5)} bodyBg=${row.bodyBg} bodyColor=${row.bodyColor}`
    );
  }

  writeFileSync(join(outDir, "observed.json"), JSON.stringify(report, null, 2));
  console.log(`\n${shots.length} screenshots in web/.theme-shots/`);

  // Fail loudly if a requested theme did not actually take effect.
  const expected = { light: "light", dark: "dark" };
  let bad = 0;
  for (const row of report) {
    const want = expected[row.requested] ?? row.requested.replace("system:", "");
    if (row.theme !== want) {
      console.log(`MISMATCH ${row.page} asked ${row.requested} but got ${row.theme}`);
      bad++;
    }
  }
  console.log(bad === 0 ? "every requested theme was applied" : `${bad} theme(s) did not apply`);
  process.exitCode = bad === 0 ? 0 : 1;
}

try {
  await main();
} finally {
  edge.kill();
}
