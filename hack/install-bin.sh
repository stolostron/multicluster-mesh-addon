#!/usr/bin/env bash
# Installs a tool binary when the installed version differs from the wanted one.
#
# Usage: hack/install-bin.sh TARGET VERSION CHECK_ARGS URL [INNER_PATH]
# CHECK_ARGS are passed to "TARGET CHECK_ARGS" to print the version.
# INNER_PATH is the binary's path inside the downloaded tarball; omit it when the download URL is a single binary.

set -euo pipefail

target="$1"
version="$2"
check_args="$3"
url="$4"
inner_path="${5:-}"

read -r -a check <<< "${check_args}"
if [[ -x "${target}" && "$("${target}" "${check[@]}" 2>/dev/null)" == *"${version}"* ]]; then
    exit 0
fi

echo "Installing $(basename "${target}") ${version}..."
tmp_dir="$(mktemp -d)"
trap 'rm -rf "${tmp_dir}"' EXIT

curl -fsSL "${url}" -o "${tmp_dir}/tool"
[[ -z "${inner_path}" ]] || tar xzf "${tmp_dir}/tool" -C "${tmp_dir}"
chmod +x "${tmp_dir}/${inner_path:-tool}"
mv "${tmp_dir}/${inner_path:-tool}" "${target}"
