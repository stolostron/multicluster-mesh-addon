#!/usr/bin/env bash
# Functional tests for hack/install-bin.sh.

SCRIPT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)/hack/install-bin.sh"

set -u
pass=0
failures=0
work="$(mktemp -d)"
srv="${work}/srv"
tmp="${work}/tmp"
bin="${work}/bin"
mkdir -p "${srv}" "${tmp}" "${bin}"
# Steer the tested script's mktemp into a suite-owned dir so its trap cleanup is observable.
export TMPDIR="${tmp}"
# shellcheck disable=SC2329 # Invoked only through the EXIT trap.
cleanup() { kill "${server_pid}" 2>/dev/null || true; rm -rf "${work:?}"; }
trap cleanup EXIT

# Shell stubs are faithful: the script only execs them and matches their version output.
# They carry no exec bits, so the script's own chmod must make them run.
printf '#!/bin/sh\necho v1.0.0\n' > "${srv}/tool-v1"

mkdir -p "${work}/pkg/bin"
printf '#!/bin/sh\necho v2.0.0\n' > "${work}/pkg/bin/tool"
tar czf "${srv}/tool-v2.tar.gz" -C "${work}/pkg" bin/tool

port=$((10000 + RANDOM % 50000))
python3 -m http.server "${port}" --bind 127.0.0.1 --directory "${srv}" >/dev/null 2>&1 &
server_pid=$!
base="http://127.0.0.1:${port}"

wait_for_server() {
    for _ in {1..50}; do
        curl -fsS --max-time 1 "${base}/tool-v1" >/dev/null 2>&1 && return 0
        sleep 0.1
    done
    echo "test server did not come up on ${base}" >&2
    exit 1
}
wait_for_server

# assert <description> <condition command...>: a passing condition is a passing test
assert() {
    local desc="$1"; shift
    if "$@"; then
        pass=$((pass + 1))
    else
        echo "FAIL: ${desc}"
        failures=$((failures + 1))
    fi
}
# shellcheck disable=SC2329 # Invoked only as an assert condition word.
tmp_is_empty() { [ -z "$(ls -A "${tmp}")" ]; }
# stat -c is GNU, stat -f is BSD/macOS; GNU must go first because its -f means file-system status.
file_inode() { stat -c %i "$1" 2>/dev/null || stat -f %i "$1" 2>/dev/null; }

# 1. install missing target, direct binary
assert "direct install succeeds" "${SCRIPT}" "${bin}/tool" v1.0.0 version "${base}/tool-v1"
assert "direct install creates an executable binary" test -x "${bin}/tool"
assert "direct install prints the wanted version" [ "$("${bin}/tool" version)" = "v1.0.0" ]
assert "direct install cleans its temp dir" tmp_is_empty

# 2. same version is a silent no-op that does not touch the binary
inode_before="$(file_inode "${bin}/tool")"
assert "inode lookup succeeds" test -n "${inode_before}"
out="$("${SCRIPT}" "${bin}/tool" v1.0.0 version "${base}/tool-v1" 2>&1)"
assert "same version exits zero" test "$?" -eq 0
assert "same version is silent" [ -z "${out}" ]
assert "same version does not touch the binary" [ "$(file_inode "${bin}/tool")" = "${inode_before}" ]

# 3. wrong version is replaced, from a tarball with a nested inner path
assert "tarball replacement succeeds" "${SCRIPT}" "${bin}/tool" v2.0.0 version "${base}/tool-v2.tar.gz" bin/tool
assert "wrong version replaced from tarball" [ "$("${bin}/tool" version)" = "v2.0.0" ]

# 4. the pin is authoritative: a newer installed version is downgraded
# The newer version is pre-created so the scenario owns its starting state.
cp "${work}/pkg/bin/tool" "${bin}/tool"
chmod +x "${bin}/tool"
assert "downgrade succeeds" "${SCRIPT}" "${bin}/tool" v1.0.0 version "${base}/tool-v1"
assert "pin downgrades a newer binary" [ "$("${bin}/tool" version)" = "v1.0.0" ]

# 5. an HTTP error fails the install and preserves the working binary
"${SCRIPT}" "${bin}/tool" v9.9.9 version "${base}/missing" >/dev/null 2>&1
assert "404 fails the install" test "$?" -ne 0
assert "404 preserves the working binary" [ "$("${bin}/tool" version)" = "v1.0.0" ]
assert "failed install cleans its temp dir" tmp_is_empty

# 6. multi-word check args are word-split, not passed as one argument
# The target is pre-installed and the URL is a 404, so the check must match without any fetch.
printf '#!/bin/sh\n[ "$#" -eq 2 ] && echo v1.0.0\n' > "${bin}/strict"
chmod +x "${bin}/strict"
out="$("${SCRIPT}" "${bin}/strict" v1.0.0 "version --remote=false" "${base}/missing" 2>&1)"
assert "multi-word no-op exits zero" test "$?" -eq 0
assert "multi-word check args are split and matched" [ -z "${out}" ]

echo "passed: ${pass}, failed: ${failures}"
exit "${failures}"
