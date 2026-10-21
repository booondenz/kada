#!/bin/bash
# Set or refresh KEY=VALUE pairs in a runtime env file, on the server.
#
#   bash upsert-env.sh /opt/kada/backend/.env \
#     --set DEEPSEEK_API_KEY="$DEEPSEEK_API_KEY" \
#     --require DEEPSEEK_API_KEY
#
# Why this exists: the AI keys and the SMS credentials used to be typed into a file on the server by hand,
# and nothing anywhere failed when they were forgotten. The service started, passed its health check, and
# then failed every request that mattered - "the AI page is broken" and "the SMS code never arrives" were
# both, in the end, an empty line in a file nobody had looked at. The values now come from GitHub Secrets
# through CI, and --require turns "forgot to configure it" into a failed deployment that says which key is
# missing, before any service on the host has been touched.
#
# --set with an EMPTY value is a no-op: it leaves whatever the file already holds. That is what makes it
# safe to run on a host that was configured by hand before the secrets existed - and --require is what
# stops "empty means skip" from quietly passing when nothing is there at all.
#
# --require also decides *where* the check belongs. It reads the file as it will be after the sync, rather
# than testing whether a repository secret happens to be set, and that is deliberate: a host configured by
# hand keeps deploying normally, because the value only has to reach this file by some route. A check that
# looked at the secrets instead would fail a deployment that was going to work.
#
# The value is never passed through sed: an API key can contain `/`, `&` or `\`, every one of which sed
# would treat as part of its own syntax. The line is removed with grep and appended with printf instead.
set -euo pipefail

usage() {
  cat >&2 <<'EOF'
usage: upsert-env.sh FILE [--set KEY=VALUE]... [--require KEY]...

  --set KEY=VALUE   Write KEY=VALUE into FILE, replacing any existing KEY= line.
                    An empty VALUE leaves the current line untouched.
  --require KEY     Fail unless FILE ends up containing a non-empty KEY=.
EOF
}

file=""
sets=()
requires=()

while [ $# -gt 0 ]; do
  case "$1" in
    --set)
      [ $# -ge 2 ] || { echo "--set needs KEY=VALUE" >&2; usage; exit 2; }
      sets+=("$2")
      shift 2
      ;;
    --require)
      [ $# -ge 2 ] || { echo "--require needs KEY" >&2; usage; exit 2; }
      requires+=("$2")
      shift 2
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    -*)
      # Without this, a mistyped option (`--requrie`) is taken for the file path and the script silently
      # creates a file by that name, then checks nothing - the exact shape of failure this whole script
      # exists to remove.
      echo "unknown option: $1" >&2
      usage
      exit 2
      ;;
    *)
      if [ -z "$file" ]; then
        file="$1"
      else
        echo "unexpected argument: $1" >&2
        usage
        exit 2
      fi
      shift
      ;;
  esac
done

if [ -z "$file" ]; then
  usage
  exit 2
fi

if [ ! -f "$file" ]; then
  # 600 from the moment it exists: these files hold provider keys and the JWT secret, and a file that is
  # world-readable for even a second is a file that has leaked. The umask covers the creation itself; the
  # explicit chmod makes the intent independent of whatever umask the calling shell happens to have.
  mkdir -p "$(dirname "$file")"
  (umask 077; : > "$file")
  chmod 600 "$file"
  echo "created $file (mode 600)"
fi

for pair in ${sets[@]+"${sets[@]}"}; do
  key=${pair%%=*}
  value=${pair#*=}

  # The key is interpolated into a grep pattern, so it has to be boring: letters, digits and underscore.
  case "$key" in
    ''|*[!A-Za-z0-9_]*)
      echo "refusing to write an invalid key: $key" >&2
      exit 2
      ;;
  esac

  if [ -z "$value" ]; then
    echo "keeping the existing $key in $file (no value given)"
    continue
  fi

  tmp=$(mktemp)
  # `|| true`: grep exits 1 when it removes nothing, which under `set -e` would abort the script.
  grep -v -E "^[[:space:]]*${key}=" "$file" > "$tmp" || true
  printf '%s=%s\n' "$key" "$value" >> "$tmp"
  cat "$tmp" > "$file"
  rm -f "$tmp"
  echo "set $key in $file"
done

missing=""
for key in ${requires[@]+"${requires[@]}"}; do
  if ! grep -qE "^[[:space:]]*${key}=[^[:space:]]" "$file"; then
    missing="$missing $key"
  fi
done

if [ -n "$missing" ]; then
  cat >&2 <<EOF
missing required value(s) in $file:$missing

Add them as repository secrets (Settings -> Secrets and variables -> Actions): the
deployment refuses to continue rather than start a service that cannot work, and it
refuses before touching anything on this host. A value already present in this file
also satisfies the check - --set with an empty value leaves it alone, so a host that
was configured by hand keeps deploying without the secret being set here.

Expected secrets:
  DEEPSEEK_API_KEY    chat model key           -> DEEPSEEK_API_KEY in the host's /opt/kada/backend/.env
  
  SMS_ACCESS_KEY_ID, SMS_ACCESS_KEY_SECRET, SMS_SIGN_NAME, SMS_TEMPLATE_CODE -> the host's /opt/kada/backend/.env
      (phone + SMS code is how a guest account is claimed and the only way into an account that has one,
       so without these no one can claim a phone number)
EOF
  exit 1
fi

exit 0
