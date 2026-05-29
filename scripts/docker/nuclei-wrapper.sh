#!/usr/bin/env bash

set -uo pipefail

readonly DEFAULT_TEMPLATES_DIR="/opt/cyberstrike/nuclei-templates"
readonly DEFAULT_UPDATE_INTERVAL_SECONDS="86400"

script_dir="$(CDPATH= cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
real_nuclei="${CYBERSTRIKE_NUCLEI_REAL:-${script_dir}/nuclei.real}"

export NUCLEI_TEMPLATES_PATH="${NUCLEI_TEMPLATES_PATH:-${DEFAULT_TEMPLATES_DIR}}"
update_stamp="${CYBERSTRIKE_NUCLEI_UPDATE_STAMP:-${NUCLEI_TEMPLATES_PATH}/.cyberstrike-last-template-update}"
update_interval_seconds="${CYBERSTRIKE_NUCLEI_UPDATE_INTERVAL_SECONDS:-${DEFAULT_UPDATE_INTERVAL_SECONDS}}"

die() {
    echo "cyberstrike nuclei wrapper error: $*" >&2
    exit 127
}

is_false() {
    case "${1,,}" in
        false|0|no|off) return 0 ;;
        *) return 1 ;;
    esac
}


is_manual_update_command() {
    local arg
    for arg in "$@"; do
        case "${arg}" in
            -update-templates|--update-templates|-ut|--ut|-update|--update|-up|--up) return 0 ;;
        esac
    done
    return 1
}

has_disable_update_check() {
    local arg
    for arg in "$@"; do
        case "${arg}" in
            -duc|--duc|-disable-update-check|--disable-update-check) return 0 ;;
        esac
    done
    return 1
}

stamp_is_stale() {
    [[ ! -s "${update_stamp}" ]] && return 0

    local now stamp_mtime age
    now="$(date +%s 2>/dev/null)" || return 0
    stamp_mtime="$(stat -c %Y "${update_stamp}" 2>/dev/null)" || return 0

    if ! [[ "${update_interval_seconds}" =~ ^[0-9]+$ ]]; then
        update_interval_seconds="${DEFAULT_UPDATE_INTERVAL_SECONDS}"
    fi

    age=$((now - stamp_mtime))
    (( age >= update_interval_seconds ))
}

refresh_templates_if_needed() {
    if is_false "${CYBERSTRIKE_NUCLEI_AUTO_UPDATE:-true}"; then
        return 0
    fi

    stamp_is_stale || return 0

    mkdir -p -- "${NUCLEI_TEMPLATES_PATH}" "$(dirname -- "${update_stamp}")" 2>/dev/null || true

    if "${real_nuclei}" -update-templates -ud "${NUCLEI_TEMPLATES_PATH}"; then
        date +%s >"${update_stamp}" 2>/dev/null || true
    else
        echo "cyberstrike nuclei wrapper warning: failed to refresh nuclei templates in ${NUCLEI_TEMPLATES_PATH}; continuing with bundled or existing templates" >&2
    fi
}

[[ -x "${real_nuclei}" ]] || die "real nuclei binary not found or not executable at ${real_nuclei}"

if is_manual_update_command "$@"; then
    exec "${real_nuclei}" "$@"
fi

refresh_templates_if_needed

if has_disable_update_check "$@"; then
    exec "${real_nuclei}" "$@"
fi

exec "${real_nuclei}" -duc "$@"
