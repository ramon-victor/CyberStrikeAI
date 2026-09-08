#!/usr/bin/env bash

set -Eeuo pipefail

template_config="${CYBERSTRIKE_TEMPLATE_CONFIG:-/app/config.example.yaml}"
runtime_config_dir="${CYBERSTRIKE_RUNTIME_CONFIG_DIR:-/app/runtime-config}"
runtime_config_path="${CYBERSTRIKE_RUNTIME_CONFIG_PATH:-${runtime_config_dir}/config.yaml}"
app_version="${CYBERSTRIKE_APP_VERSION:-v1.7.18}"

if [[ ! "${app_version}" =~ ^v?[0-9]+\.[0-9]+\.[0-9]+([.-][0-9A-Za-z.-]+)?$ ]]; then
    printf '[docker-entrypoint] invalid CYBERSTRIKE_APP_VERSION: %s\n' "${app_version}" >&2
    exit 1
fi

mkdir -p "${runtime_config_dir}"

if [[ ! -f "${runtime_config_path}" ]]; then
    cp "${template_config}" "${runtime_config_path}"
    printf '[docker-entrypoint] initialized runtime config at %s\n' "${runtime_config_path}"
fi

# Update only the top-level release marker; never overwrite persisted settings or secrets.
root_version_count="$(awk '/^version[[:space:]]*:/ { count++ } END { print count + 0 }' "${runtime_config_path}")"
if [[ "${root_version_count}" == "1" ]]; then
    tmp_config="$(mktemp "${runtime_config_path}.tmp.XXXXXX")"
    awk -v version="${app_version}" '/^version[[:space:]]*:/ { print "version: \"" version "\""; next } { print }' \
        "${runtime_config_path}" >"${tmp_config}"
    if ! cmp -s "${runtime_config_path}" "${tmp_config}"; then
        mv "${tmp_config}" "${runtime_config_path}"
    else
        rm -f "${tmp_config}"
    fi
elif [[ "${root_version_count}" == "0" ]]; then
    tmp_config="$(mktemp "${runtime_config_path}.tmp.XXXXXX")"
    {
        printf 'version: "%s"\n' "${app_version}"
        cat "${runtime_config_path}"
    } >"${tmp_config}"
    mv "${tmp_config}" "${runtime_config_path}"
else
    printf '[docker-entrypoint] multiple top-level version keys found; leaving version unchanged\n' >&2
fi

exec "$@"
