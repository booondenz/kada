// Runs the two steps of .github/workflows/certificate.yml against a stubbed `ssh` and `gh`, so the
// reporting rules are observed instead of assumed.
//
//   node scripts/verify-certificate-check.test.js
//
// What it pins:
//
//   * a certificate that is far from expiry says nothing and opens nothing;
//   * one inside the warning window opens an issue whose body names the date;
//   * one that already has an open issue gets a comment, not a second issue - a daily job that opens a
//     daily issue is how a tracker turns into noise nobody reads;
//   * a certificate that cannot be read, including one behind a refused SSH connection, fails the run and
//     opens an issue, because a check that checks nothing must not look like a check that passed.
//
// The second step is fed from the file the first step actually wrote, so the handoff between them is under
// test too - that is the part a workflow gets wrong silently.
//
// Generates scripts/.verify-cert-out/ and runs it with bash. Git Bash cannot run under the default sandbox
// (it needs a signal pipe), so this command needs wider access than an ordinary file operation.

const fs = require("fs");
const path = require("path");
const { execFileSync } = require("child_process");

const root = path.join(__dirname, "..");
const outDir = path.join(__dirname, ".verify-cert-out");
const toPosix = (p) => p.replace(/\\/g, "/");

fs.rmSync(outDir, { recursive: true, force: true });
fs.mkdirSync(outDir, { recursive: true });

const workflow = fs.readFileSync(path.join(root, ".github", "workflows", "certificate.yml"), "utf8");

// --- the wiring, which is not visible from inside either step -------------------------------------------
// Both steps are extracted and run on their own, so nothing here would notice a mistyped `steps.read`
// reference or a missing permission: the workflow would simply not do what its steps do.
const wiring = [
  ["the schedule exists", /^\s{2}schedule:/m],
  ["the workflow may open an issue", /^\s{2}issues: write$/m],
  ["the read step is addressable", /^\s+id: read$/m],
  ["the status is handed over", /\$\{\{\s*steps\.read\.outputs\.status\s*\}\}/],
  ["the date is handed over", /\$\{\{\s*steps\.read\.outputs\.not_after\s*\}\}/],
  ["the reason is handed over", /\$\{\{\s*steps\.read\.outputs\.reason\s*\}\}/],
  ["the certificate path is named once", /CERT_PATH: \/etc\/letsencrypt\/live\/kada\.click\/fullchain\.pem/],
];

let wiringFails = 0;
console.log("--- the workflow's wiring ---");
for (const [label, pattern] of wiring) {
  if (pattern.test(workflow)) {
    console.log(`ok    ${label}`);
  } else {
    console.log(`FAIL  ${label}`);
    wiringFails += 1;
  }
}
console.log();

// --- pull the two steps' run: blocks out of the workflow ------------------------------------------------
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

const write = (name, content) => fs.writeFileSync(path.join(outDir, name), content.replace(/\r\n/g, "\n"), "utf8");
write("step-read.sh", extractRun("Read the certificate from the host"));
write("step-report.sh", extractRun("Report the remaining validity"));

// --- the harness that decides whether each case behaved -------------------------------------------------
// The harness is a String.raw template literal: backslashes are safe, but a `${...}` sequence is not - it
// is interpolation. Bash's `${VAR:-default}` form therefore has to be written as plain `$VAR` with the
// variable initialized, which is why the printf below passes "$STATUS" and not "${STATUS:-none}".
const HARNESS = String.raw`#!/usr/bin/env bash
# Runs the read step and then the reporting step, and reports on each case.
set -u

HERE="$(cd "$(dirname "$0")" && pwd)"
fails=0

# run_case slug mode want_exit want_create want_comment want_grep want_existing label
#
# want_grep is matched against the reporting step's output and against the arguments the stubbed gh was
# called with, so one pattern can assert either what was printed or what was filed.
run_case() {
  slug="$1"; mode="$2"; want_exit="$3"; want_create="$4"; want_comment="$5"
  want_grep="$6"; want_existing="$7"; label="$8"

  dir="$HERE/case-$slug"
  rm -rf "$dir"; mkdir -p "$dir/bin"
  : > "$dir/calls"
  : > "$dir/gh_output"

  # The stub answers with the protocol the real remote command prints, or refuses the connection.
  cat > "$dir/bin/ssh" <<'STUB'
#!/usr/bin/env bash
echo "ssh $*" >> "$STUB_CALLS"
case "$STUB_MODE" in
  ok)         printf 'notAfter=Jan 11 01:53:31 2027 GMT\nstatus=ok\n' ;;
  expiring)   printf 'notAfter=Oct 13 01:53:31 2026 GMT\nstatus=expiring\n' ;;
  unreadable) printf 'status=unreadable\n' ;;
  refused)    echo "ssh: connect to host example.invalid port 22: Connection refused" >&2; exit 255 ;;
  *)          echo "unknown STUB_MODE: $STUB_MODE" >&2; exit 99 ;;
esac
STUB
  chmod +x "$dir/bin/ssh"

  # The stub answers "issue list" with the open issue the case declares, if any, and records everything.
  cat > "$dir/bin/gh" <<'STUB'
#!/usr/bin/env bash
echo "gh $*" >> "$STUB_CALLS"
if [ "$1" = "issue" ] && [ "$2" = "list" ]; then
  [ -n "$STUB_EXISTING" ] && printf '%s\n' "$STUB_EXISTING"
  exit 0
fi
echo "https://example.invalid/issues/1"
STUB
  chmod +x "$dir/bin/gh"

  export STUB_MODE="$mode" STUB_CALLS="$dir/calls" STUB_EXISTING="$want_existing"
  export GITHUB_OUTPUT="$dir/gh_output"
  export SSH_KEY="not-a-real-key" SERVER_HOST="example.invalid" SERVER_USER="kada"
  export RUNNER_TEMP="$dir"
  export CERT_PATH="/etc/letsencrypt/live/kada.click/fullchain.pem"
  export WARN_DAYS="14"
  export EXPIRY_TITLE="The certificate the site serves is close to expiry"
  export UNREADABLE_TITLE="The certificate check cannot read the certificate"
  # cygpath keeps the stubs ahead of the real ssh and gh; a Windows-style PATH would not.
  export PATH="$(cygpath -u "$dir/bin"):$PATH"

  ( cd "$dir" && bash "$HERE/step-read.sh" ) > "$dir/read.out" 2>&1
  read_exit=$?

  # The workflow passes these to the second step through env:, and the harness does the same - from the
  # file the first step actually wrote, so a mismatch in the names shows up here.
  STATUS=$(sed -n 's/^status=//p' "$dir/gh_output")
  NOT_AFTER=$(sed -n 's/^not_after=//p' "$dir/gh_output")
  REASON=$(sed -n 's/^reason=//p' "$dir/gh_output")

  ( cd "$dir" && STATUS="$STATUS" NOT_AFTER="$NOT_AFTER" REASON="$REASON" GH_TOKEN=stub \
      bash "$HERE/step-report.sh" ) > "$dir/report.out" 2>&1
  report_exit=$?

  created=no;   grep -q '^gh issue create' "$dir/calls" && created=yes
  commented=no; grep -q '^gh issue comment' "$dir/calls" && commented=yes
  gh_calls=$(grep -c '^gh ' "$dir/calls" || true)

  problems=""
  [ "$read_exit" = "0" ] || problems="$problems readExit=$read_exit(want 0)"
  [ "$report_exit" = "$want_exit" ] || problems="$problems reportExit=$report_exit(want $want_exit)"
  # Whatever happened, the first step has to answer with a status: an empty one is how a refused
  # connection would silently become "nothing to report".
  [ -n "$STATUS" ] || problems="$problems the read step produced no status"
  [ "$created" = "$want_create" ] || problems="$problems create=$created(want $want_create)"
  [ "$commented" = "$want_comment" ] || problems="$problems comment=$commented(want $want_comment)"
  # Silence has to mean silence: a run that neither creates nor comments must not call gh at all.
  if [ "$want_create" = "no" ] && [ "$want_comment" = "no" ] && [ "$gh_calls" != "0" ]; then
    problems="$problems gh was called $gh_calls time(s) when it should not have been"
  fi
  if [ "$want_grep" != "-" ]; then
    grep -q "$want_grep" "$dir/report.out" "$dir/calls" \
      || problems="$problems neither the output nor the filed issue contains: $want_grep"
  fi

  if [ -z "$problems" ]; then
    printf 'ok    %s\n' "$label"
  else
    printf 'FAIL  %s\n' "$label"
    printf '        reason:%s\n' "$problems"
    fails=$((fails + 1))
  fi
  printf '        mode=%s status=%s notAfter=%s readExit=%s reportExit=%s ghCalls=%s create=%s comment=%s\n' \
    "$mode" "$STATUS" "$NOT_AFTER" "$read_exit" "$report_exit" "$gh_calls" "$created" "$commented"
}

echo "--- bash -n ---"
bash -n "$HERE/step-read.sh" && echo "step-read.sh parses"
bash -n "$HERE/step-report.sh" && echo "step-report.sh parses"
echo

echo "--- cases ---"
run_case far        ok         0 no  no  "-"                                    ""  "far from expiry: says nothing and opens nothing"
run_case near       expiring   0 yes no  "Oct 13 01:53:31 2026 GMT"              ""  "inside the warning window: opens an issue naming the date"
run_case near-open  expiring   0 no  yes "kada:certificate-expiry"               "7" "an issue is already open: comments on it instead of opening another"
run_case unreadable unreadable 1 yes no  "kada:certificate-unreadable"           ""  "cannot read the certificate: opens an issue and fails the run"
run_case refused    refused    1 yes no  "Connection refused"                    ""  "SSH refused: reported as unreadable rather than as nothing to report"
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
// why this command needs wider access than the default.
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
process.exit(run(`bash ${unixDir}/harness.sh`) || (wiringFails ? 1 : 0));
