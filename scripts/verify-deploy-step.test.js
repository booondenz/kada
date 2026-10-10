// Extracts the "Verify deployment", "Report the smoke test result", "Check the SMS credentials" and
// "Upload the deploy bundle" steps out of ci.yml and runs them, so each outcome is observed instead of
// assumed.
//
//   node scripts/verify-deploy-step.test.js
//
// The verification cases are the ones that actually appeared in CI (35: the edge reset the TLS handshake,
// 6: the host did not resolve) plus the healthy case and a reachable-but-unhealthy API. It asserts that
// the verify step never fails the job, that it records the right status, and that the report step warns
// only when the smoke test genuinely did not pass.
//
// The SMS cases assert the property that makes that step safe to ship: it reports that CI does not manage
// the credentials, and still exits 0. It must not fail the job, because the value only has to reach the
// host's .env by some route and a hand-configured host deploys perfectly well - a step that failed here
// would reject a deployment that was going to work, which is worse than saying nothing.
//
// The upload cases stub `ssh` and drive the three things it can do wrong: complete the transfer, complete
// it after failures, deliver a short stream while still exiting 0, and refuse outright. What they pin is
// that a bad link stays bounded - three attempts, then a failure with the host untouched - because the
// step this replaced could sit in a stalled transfer for half an hour (see the comment in ci.yml), and
// that the bundle still carries every path the deploy step reads.
//
// The swap case asserts the property the nginx bind mount depends on: the deploy moves a new frontend
// build INTO /opt/kada/frontend and never replaces that directory. A bind mount keeps the inode it
// resolved when the container started, so replacing the directory leaves nginx serving the removed, empty
// one - every page a 500, while the API behind its own location blocks answers 200 and every other check
// passes. That is what happened, which is why the case asserts the inode and not only the file contents.
//
// Generates scripts/.verify-deploy-out/ and runs it with bash. Git Bash cannot run under the default
// sandbox (it needs a signal pipe), so this looks for WSL bash first and falls back to Git Bash.

const fs = require("fs");
const path = require("path");
const { execFileSync } = require("child_process");

const root = path.join(__dirname, "..");
const outDir = path.join(__dirname, ".verify-deploy-out");
const toPosix = (p) => p.replace(/\\/g, "/");

fs.rmSync(outDir, { recursive: true, force: true });
fs.mkdirSync(outDir, { recursive: true });

// --- pull the three steps' run: blocks out of the workflow ----------------------------------------------
const workflow = fs.readFileSync(path.join(root, ".github", "workflows", "ci.yml"), "utf8");

function extractRun(name) {
  const lines = workflow.split(/\r?\n/);
  const start = lines.findIndex((l) => l.trim() === `- name: ${name}`);
  if (start < 0) throw new Error(`step not found: ${name}`);
  const runAt = lines.findIndex((l, i) => i > start && l.trim() === "run: |");
  if (runAt < 0) throw new Error(`run: | not found after: ${name}`);
  const body = [];
  for (let i = runAt + 1; i < lines.length; i++) {
    const line = lines[i];
    if (line.trim() === "") { body.push(""); continue; }
    if (!/^\s{10}/.test(line)) break;
    body.push(line.slice(10));
  }
  while (body.length && body[body.length - 1].trim() === "") body.pop();
  return body.join("\n");
}

// The frontend swap is a block inside the deploy step's script:, not a step of its own, and it is the one
// part of the deploy with a property worth pinning down - see the swap case in the harness.
function extractFrontendSwap() {
  const lines = workflow.split(/\r?\n/);
  const start = lines.findIndex((l) => l.includes('echo "Deploying the frontend"'));
  if (start < 0) throw new Error("the frontend swap was not found in the workflow");
  const end = lines.findIndex((l, i) => i > start && l.includes('echo "the frontend is deployed"'));
  if (end < 0) throw new Error("the frontend swap has no end marker");
  return lines
    .slice(start, end + 1)
    .map((l) => l.replace(/^\s{12}/, ""))
    .join("\n");
}

const write = (name, content) => fs.writeFileSync(path.join(outDir, name), content.replace(/\r\n/g, "\n"), "utf8");
write("step-verify.sh", extractRun("Verify deployment"));
write("step-report.sh", extractRun("Report the smoke test result"));
write("step-check-sms.sh", extractRun("Check the SMS credentials"));
write("step-upload-bundle.sh", extractRun("Upload the deploy bundle"));
write("frontend-swap.sh", extractFrontendSwap());

// --- the harness that decides whether each case behaved -------------------------------------------------
const HARNESS = String.raw`#!/usr/bin/env bash
# Runs the two extracted steps against a stubbed curl and reports on each case.
set -u

HERE="$(cd "$(dirname "$0")" && pwd)"
fails=0

run_case() {
  mode="$1"; want_verify_exit="$2"; want_status="$3"; want_warning="$4"; want_body="$5"; label="$6"
  dir="$HERE/case-$mode"
  rm -rf "$dir"; mkdir -p "$dir/bin"
  : > "$dir/calls"
  export STUB_CALLS="$dir/calls"

  # Every case here is one where the release did reach the host: the smoke test is what is being judged. The
  # report step reads this to tell "the site was unreachable" apart from "the deploy never happened", and it
  # treats an unset value as the latter - which is the separate case further down, not this one.
  export APPLY_OUTCOME=success

  # The stub: every call is refused unless the case says otherwise. It records each call so the harness
  # can tell that the stub - and not the real curl - was what ran.
  cat > "$dir/bin/curl" <<'STUB'
#!/usr/bin/env bash
echo x >> "$STUB_CALLS"
out=/dev/null
want_out=0
url=""
for arg in "$@"; do
  if [ "$want_out" = 1 ]; then out="$arg"; want_out=0; continue; fi
  if [ "$arg" = "-o" ]; then want_out=1; continue; fi
  case "$arg" in
    http://* | https://*) url="$arg" ;;
  esac
done

# The real endpoints answer differently: /api/health is JSON, / is HTML.
case "$url" in
  */api/health) healthy='{"service":"kada-api","status":"ok","version":"0.2.0"}'; unhealthy='{"status":"degraded"}' ;;
  *)            healthy='<!doctype html><html><title>Kada</title></html>'; unhealthy="$healthy" ;;
esac

case "$STUB_MODE" in
  ok)
    printf '%s' "$healthy" > "$out"
    exit 0 ;;
  reset)
    echo "curl: (35) Recv failure: Connection reset by peer" >&2
    exit 35 ;;
  dns)
    echo "curl: (6) Could not resolve host: kada.click" >&2
    exit 6 ;;
  unhealthy)
    printf '%s' "$unhealthy" > "$out"
    exit 0 ;;
  *)
    echo "unknown STUB_MODE: $STUB_MODE" >&2
    exit 99 ;;
esac
STUB
  chmod +x "$dir/bin/curl"

  export STUB_MODE="$mode"
  export SITE_ORIGIN="https://kada.click"
  export GITHUB_STEP_SUMMARY="$dir/summary.md"
  # cygpath keeps the stub ahead of the real curl; a Windows-style PATH would not.
  export PATH="$(cygpath -u "$dir/bin"):$PATH"

  ( cd "$dir" && bash "$HERE/step-verify.sh" ) > "$dir/verify.out" 2>&1
  verify_exit=$?

  status=$(cat "$dir/verify-status.txt" 2>/dev/null || echo missing)
  body=$(cat "$dir/verify-body.txt" 2>/dev/null || echo "(none)")
  # Proves the stub answered rather than the real curl, and that --retry actually retried.
  calls=$(wc -l < "$dir/calls" | tr -d ' ')

  ( cd "$dir" && bash "$HERE/step-report.sh" ) > "$dir/report.out" 2>&1
  report_exit=$?

  warned=no
  grep -q '::warning' "$dir/report.out" && warned=yes

  problems=""
  [ "$verify_exit" = "$want_verify_exit" ] || problems="$problems verifyExit=$verify_exit(want $want_verify_exit)"
  [ "$status" = "$want_status" ] || problems="$problems status=$status(want $want_status)"
  [ "$warned" = "$want_warning" ] || problems="$problems warning=$warned(want $want_warning)"
  [ "$report_exit" = "0" ] || problems="$problems reportExit=$report_exit"
  [ "$calls" -ge 1 ] 2>/dev/null || problems="$problems the stubbed curl never ran"
  # Note: curl's --retry does not retry a *shell* stub that exits non-zero - it only retries failures it
  # raises itself - so this harness cannot assert the retry count. That was verified separately against a
  # socket that really resets: --retry 5 produced 6 connections. Here the stub refuses persistently, which
  # is the worst case, and the point is that the step survives it.
  printf '%s' "$body" | grep -q "$want_body" || problems="$problems body=$body(does not contain $want_body)"
  # A refusal must never be the thing that fails the step.
  [ -s "$dir/verify.out" ] || problems="$problems the step printed nothing"

  if [ -z "$problems" ]; then
    printf 'ok    %s\n' "$label"
  else
    printf 'FAIL  %s\n' "$label"
    printf '        reason:%s\n' "$problems"
    fails=$((fails + 1))
  fi
  printf '        status=%s verifyExit=%s reportExit=%s warning=%s stubCalls=%s\n' "$status" "$verify_exit" "$report_exit" "$warned" "$calls"
  printf '        body: %s\n' "$body"
}

echo "--- bash -n ---"
bash -n "$HERE/step-verify.sh" && echo "step-verify.sh parses"
bash -n "$HERE/step-report.sh" && echo "step-report.sh parses"
bash -n "$HERE/step-check-sms.sh" && echo "step-check-sms.sh parses"
bash -n "$HERE/step-upload-bundle.sh" && echo "step-upload-bundle.sh parses"
echo

echo "--- cases ---"
run_case ok        0 passed no  "(none)"                              "runner reaches the site"
run_case reset     0 failed yes "curl 35: a TLS handshake failure"    "edge resets the TLS handshake (exit 35)"
run_case dns       0 failed yes "curl 6: could not fetch"             "DNS does not resolve (exit 6)"
run_case unhealthy 0 failed yes "did not answer with a healthy payload" "reachable but the API is not healthy"
echo

echo "--- the report step when the release never reached the host ---"

# The failure this exists for: a stalled upload was reported as "the smoke test did not run" next to "the
# deployment itself was checked on the host and is not affected" - a reassuring sentence about a deployment
# that never happened, printed at the moment a reader most needs the truth. The step cannot tell the two
# apart from the files it reads, so it is told which one occurred, and these cases pin what it then says.
run_no_deploy_case() {
  mode="$1"; label="$2"
  dir="$HERE/case-nodeploy-$mode"
  rm -rf "$dir"; mkdir -p "$dir"
  : > "$dir/summary.md"

  # No verify-status.txt and no verify-body.txt: the verify step was skipped, exactly as it is when an
  # earlier step of the job fails.
  APPLY_OUTCOME="$mode" GITHUB_STEP_SUMMARY="$dir/summary.md" \
    SITE="https://kada.click" bash "$HERE/step-report.sh" > "$dir/report.out" 2>&1
  rc=$?

  problems=""
  [ "$rc" = "0" ] || problems="$problems reportExit=$rc"
  grep -q 'The release did not reach the host' "$dir/summary.md" || problems="$problems summary-does-not-say-what-happened"
  # Nothing about the site may be claimed: no smoke test ran, so there is no result to report.
  grep -q 'Public smoke test did not pass' "$dir/summary.md" && problems="$problems claimed-a-smoke-test-failure"
  grep -q 'not affected' "$dir/summary.md" && problems="$problems claimed-the-deployment-was-checked"
  # An error annotation, not a warning: the deployment did not happen.
  grep -q '::error' "$dir/report.out" || problems="$problems no-error-annotation"
  grep -q '::warning' "$dir/report.out" && problems="$problems warned-instead-of-erroring"

  if [ -z "$problems" ]; then
    printf 'ok    %s\n' "$label"
  else
    printf 'FAIL  %s\n' "$label"
    printf '        reason:%s\n' "$problems"
    fails=$((fails + 1))
  fi
  printf '        exit=%s summary: %s\n' "$rc" "$(tail -n 1 "$dir/summary.md")"
}

run_no_deploy_case skipped "the apply step never ran, because an earlier step failed"
run_no_deploy_case failure "the apply step itself failed"
echo

echo "--- the SMS advisory step ---"

# A missing SMS secret must be reported without failing the deploy, and must name every key that is not
# managed here; with all four set it must be completely silent. It must never dress this up as a failure,
# because whether the deployment really breaks depends on the host's own .env, which this step cannot see.
run_sms_case() {
  mode="$1"; want_notice="$2"; want_summary="$3"; label="$4"
  dir="$HERE/case-sms-$mode"
  rm -rf "$dir"; mkdir -p "$dir"
  : > "$dir/summary.md"

  if [ "$mode" = "present" ]; then
    # Obviously fake values on purpose. Nothing here asserts on what a credential *is* (only on whether the
    # step stays quiet when all four are set), so using the account's real template code would mean putting
    # an account-specific value in the tree for no gain - see the note in docs/design.md section 9.1.
    SMS_ACCESS_KEY_ID=id SMS_ACCESS_KEY_SECRET=secret SMS_SIGN_NAME=kada SMS_TEMPLATE_CODE=SMS_000000000 \
      GITHUB_STEP_SUMMARY="$dir/summary.md" bash "$HERE/step-check-sms.sh" > "$dir/out.txt" 2>&1
  else
    SMS_ACCESS_KEY_ID= SMS_ACCESS_KEY_SECRET= SMS_SIGN_NAME= SMS_TEMPLATE_CODE= \
      GITHUB_STEP_SUMMARY="$dir/summary.md" bash "$HERE/step-check-sms.sh" > "$dir/out.txt" 2>&1
  fi
  rc=$?

  noted=no
  grep -q '::notice' "$dir/out.txt" && noted=yes
  # An annotation that cries failure where there is none is how the annotations that matter stop being read.
  alarmed=no
  grep -qE '::(warning|error)' "$dir/out.txt" && alarmed=yes
  summary=no
  [ -s "$dir/summary.md" ] && summary=yes

  problems=""
  [ "$rc" = "0" ] || problems="$problems exit=$rc(want 0)"
  [ "$noted" = "$want_notice" ] || problems="$problems notice=$noted(want $want_notice)"
  [ "$alarmed" = "no" ] || problems="$problems emitted-a-warning-or-error"
  [ "$summary" = "$want_summary" ] || problems="$problems summary=$summary(want $want_summary)"

  if [ "$want_notice" = "yes" ]; then
    for key in SMS_ACCESS_KEY_ID SMS_ACCESS_KEY_SECRET SMS_SIGN_NAME SMS_TEMPLATE_CODE; do
      grep -q "$key" "$dir/out.txt" || problems="$problems does-not-name-$key"
      grep -q "$key" "$dir/summary.md" || problems="$problems summary-missing-$key"
    done
    grep -q '| Secret |' "$dir/summary.md" || problems="$problems summary-missing-the-table"
  fi

  if [ -z "$problems" ]; then
    printf 'ok    %s\n' "$label"
  else
    printf 'FAIL  %s\n' "$label"
    printf '        reason:%s\n' "$problems"
    fails=$((fails + 1))
  fi
  printf '        exit=%s notice=%s summary=%s\n' "$rc" "$noted" "$summary"
  printf '        out: %s\n' "$(head -c 300 "$dir/out.txt")"
}

run_sms_case missing yes yes "SMS secrets unset: says so, without failing the deploy"
run_sms_case present no  no  "SMS secrets set: says nothing at all"
echo

echo "--- the deploy bundle upload step ---"

# The step talks to a host this machine cannot reach, so ssh is stubbed and the modes are the ways a
# real link misbehaves. The interesting one is "truncated": ssh exits 0 having delivered less than it was
# given, which is the case the size check exists for and the one a retry loop would otherwise paper over.
run_upload_case() {
  mode="$1"; want_exit="$2"; want_attempts="$3"; want_unpacked="$4"; label="$5"
  dir="$HERE/case-upload-$mode"
  rm -rf "$dir"; mkdir -p "$dir/bin" "$dir/backend/bin" "$dir/deploy-pkg" "$dir/deploy"
  : > "$dir/calls"

  # The files the step tars. Their names are the contract with the deploy step that follows, so the
  # assertions below also check the received tar carries every one of them.
  for f in backend/bin/server backend/bin/migrate deploy-pkg/kada-fe.tar.gz \
           deploy-version.txt \
           deploy/upsert-env.sh; do
    echo "fixture" > "$dir/$f"
  done

  cat > "$dir/bin/ssh" <<'STUB'
#!/usr/bin/env bash
# Stub for ssh. The step sends three remote commands and the last argument is always the command itself.
cmd=""
for arg in "$@"; do cmd="$arg"; done
echo "$cmd" >> "$STUB_CALLS"

case "$cmd" in
  'cat > /tmp/kada-deploy.tar.gz')
    n=$(cat "$STUB_ATTEMPTS" 2>/dev/null || echo 0)
    n=$((n + 1))
    echo "$n" > "$STUB_ATTEMPTS"
    case "$STUB_MODE" in
      ok|truncated) cat > "$STUB_REMOTE" ;;
      flaky) if [ "$n" -ge 3 ]; then cat > "$STUB_REMOTE"; else echo "connection reset by peer" >&2; exit 1; fi ;;
      dead) echo "connection refused" >&2; exit 1 ;;
    esac
    ;;
  'stat -c%s /tmp/kada-deploy.tar.gz'|'stat -c%s /tmp/kada-deploy.tar.gz 2>/dev/null || echo 0')
    # Both forms of the size query: the success path asks once, and the failure path asks again to report how
    # much of the bundle arrived - which is the number that decides whether the link is slow or dead. The
    # second form carries the step's "no file yet" fallbacks, so this has to answer 0 rather than fail when
    # nothing was ever delivered.
    if [ -f "$STUB_REMOTE" ]; then size=$(wc -c < "$STUB_REMOTE"); else size=0; fi
    if [ "$STUB_MODE" = "truncated" ]; then size=$((size - 1)); fi
    printf '%s\n' "$size"
    ;;
  *)
    echo "unpacked" > "$STUB_UNPACKED"
    ;;
esac
STUB
  chmod +x "$dir/bin/ssh"

  # timeout is coreutils and present on the runner. Stubbing it keeps these cases from depending on
  # whether the bash running them here ships it.
  cat > "$dir/bin/timeout" <<'STUB'
#!/usr/bin/env bash
shift
exec "$@"
STUB
  chmod +x "$dir/bin/timeout"

  export STUB_MODE="$mode"
  export STUB_CALLS="$dir/calls"
  export STUB_ATTEMPTS="$dir/attempts"
  export STUB_REMOTE="$dir/remote.tar.gz"
  export STUB_UNPACKED="$dir/unpacked"
  export SSH_KEY="not-a-real-key"
  export SERVER_HOST="example.invalid"
  export SERVER_USER="kada"
  export RUNNER_TEMP="$dir"
  # The step's backoff, set to nothing so a case does not wait it out.
  export RETRY_DELAY_SECONDS=0
  export PATH="$(cygpath -u "$dir/bin"):$PATH"

  ( cd "$dir" && bash "$HERE/step-upload-bundle.sh" ) > "$dir/upload.out" 2>&1
  upload_exit=$?

  attempts=$(grep -c 'cat > /tmp/kada-deploy.tar.gz' "$dir/calls" || true)
  unpacked=no
  [ -s "$dir/unpacked" ] && unpacked=yes

  problems=""
  [ "$upload_exit" = "$want_exit" ] || problems="$problems exit=$upload_exit(want $want_exit)"
  [ "$attempts" = "$want_attempts" ] || problems="$problems attempts=$attempts(want $want_attempts)"
  [ "$unpacked" = "$want_unpacked" ] || problems="$problems unpacked=$unpacked(want $want_unpacked)"
  [ -s "$dir/upload.out" ] || problems="$problems the step printed nothing"

  # A failure must say what happened, and must never be quiet about it.
  if [ "$want_exit" != "0" ]; then
    grep -q 'nothing on the host was changed' "$dir/upload.out" || problems="$problems the failure does not say the host was left alone"
  fi

  if [ "$want_unpacked" = "yes" ]; then
    tar tzf "$dir/remote.tar.gz" > "$dir/members" 2>/dev/null || problems="$problems the host did not receive a tar"
    for member in backend/bin/server backend/bin/migrate deploy-pkg/kada-fe.tar.gz \
                  deploy-version.txt \
                  deploy/upsert-env.sh; do
      grep -qx "$member" "$dir/members" || problems="$problems the bundle is missing $member"
    done
  fi

  if [ -z "$problems" ]; then
    printf 'ok    %s\n' "$label"
  else
    printf 'FAIL  %s\n' "$label"
    printf '        reason:%s\n' "$problems"
    fails=$((fails + 1))
  fi
  printf '        exit=%s attempts=%s unpacked=%s\n' "$upload_exit" "$attempts" "$unpacked"
  printf '        out: %s\n' "$(tail -n 2 "$dir/upload.out" | tr '\n' ' ')"
}

run_upload_case ok        0 1 yes "a good link: one attempt, then the bundle is unpacked"
run_upload_case flaky     0 3 yes "two dropped connections: the third attempt delivers it"
run_upload_case truncated 1 3 no  "ssh exits 0 with a short stream: refused, host untouched"
run_upload_case dead      1 3 no  "the host refuses: three attempts, then a bounded failure"
echo

# The frontend swap, sourced from the workflow itself. The directory stands in for the one bind-mounted
# into the nginx container, and the property under test is that the swap never replaces it.
run_swap_case() {
  dir="$HERE/case-swap"
  rm -rf "$dir"; mkdir -p "$dir/stage/assets" "$dir/deploy/deploy-pkg"
  FRONTEND_DIR="$dir/frontend"
  DEPLOY_DIR="$dir/deploy"
  mkdir -p "$FRONTEND_DIR/assets"

  # The build being served, including one file the next build does not contain.
  echo 'old index'  > "$FRONTEND_DIR/index.html"
  echo 'old bundle' > "$FRONTEND_DIR/assets/index-OLDBUNDLE.js"
  echo 'old robots' > "$FRONTEND_DIR/robots.txt"
  inode_before=$(stat -c %i "$FRONTEND_DIR")

  # The build arriving in the bundle, packed the way the frontend-build job packs it.
  echo 'new index'  > "$dir/stage/index.html"
  echo 'new bundle' > "$dir/stage/assets/index-NEWBUNDLE.js"
  echo 'new robots' > "$dir/stage/robots.txt"
  tar czf "$DEPLOY_DIR/deploy-pkg/kada-fe.tar.gz" -C "$dir/stage" .

  problems=""
  ( FRONTEND_DIR="$FRONTEND_DIR"; DEPLOY_DIR="$DEPLOY_DIR"; . "$HERE/frontend-swap.sh" ) \
    > "$dir/swap.out" 2>&1 || problems="$problems the swap exited $?"

  [ "$(stat -c %i "$FRONTEND_DIR")" = "$inode_before" ] \
    || problems="$problems the directory was replaced, so a bind mount would still point at the old one"
  grep -q 'new index' "$FRONTEND_DIR/index.html" \
    || problems="$problems index.html is not the new build"
  [ -f "$FRONTEND_DIR/assets/index-NEWBUNDLE.js" ] \
    || problems="$problems the new bundle is missing"
  if [ -f "$FRONTEND_DIR/assets/index-OLDBUNDLE.js" ]; then
    problems="$problems the stale bundle survived the swap"
  fi
  grep -q 'new robots' "$FRONTEND_DIR/robots.txt" \
    || problems="$problems robots.txt was not updated"
  if [ -e "$FRONTEND_DIR.new" ]; then
    problems="$problems the staging directory was left behind"
  fi

  if [ -z "$problems" ]; then
    printf 'ok    %s\n' "the frontend swap keeps the directory nginx has mounted"
  else
    printf 'FAIL  %s\n' "the frontend swap keeps the directory nginx has mounted"
    printf '        reason:%s\n' "$problems"
    fails=$((fails + 1))
  fi
  printf '        inode %s before, %s after\n' "$inode_before" "$(stat -c %i "$FRONTEND_DIR" 2>/dev/null || echo gone)"
  printf '        out: %s\n' "$(tail -n 2 "$dir/swap.out" | tr '\n' ' ')"
}

run_swap_case
echo

if [ "$fails" = "0" ]; then
  echo "all cases behaved"
  exit 0
fi
echo "$fails case(s) failed"
exit 1
`;
write("harness.sh", HARNESS);

// --- run it --------------------------------------------------------------------------------------------
// Git Bash has to be invoked from here because it needs a signal pipe, which the sandbox refuses; that is
// why this command needs wider access than the default. WSL would be closer to the runner (Linux) but is
// not installed on this machine.
const GIT_BASH = "C:\\Program Files\\Git\\bin\\bash.exe";

function run(command) {
  try {
    execFileSync(GIT_BASH, ["-lc", command], { stdio: "inherit" });
    return 0;
  } catch (err) {
    return err.status ?? 1;
  }
}

const unixDir = "/" + toPosix(outDir).replace(/^([A-Za-z]):/, (_, d) => d.toLowerCase());
process.exit(run(`bash ${unixDir}/harness.sh`));
