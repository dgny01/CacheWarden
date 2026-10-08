#!/usr/bin/env bash
set -euo pipefail
readonly SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
readonly PROJECT_ROOT="$(cd -- "${SCRIPT_DIR}/.." && pwd)"
readonly OUTPUT_DIR="${PROJECT_ROOT}/bin/ebpf"
fail() { echo "eBPF build: $*" >&2; exit 1; }
[[ "$(uname -s)" == "Linux" ]] || fail "Linux is required."
for command in clang bpftool pkg-config; do
  command -v "${command}" >/dev/null 2>&1 || fail "required command '${command}' was not found."
done
[[ -r /sys/kernel/btf/vmlinux ]] || fail "kernel BTF is unavailable at /sys/kernel/btf/vmlinux."
pkg-config --exists libbpf || fail "libbpf development files were not found (pkg-config package 'libbpf')."
case "$(uname -m)" in
  x86_64) target_arch=x86 ;; aarch64|arm64) target_arch=arm64 ;;
  s390x) target_arch=s390 ;; ppc64|ppc64le) target_arch=powerpc ;;
  riscv64) target_arch=riscv ;;
  *) fail "unsupported architecture $(uname -m); add the matching __TARGET_ARCH value." ;;
esac
mkdir -p "${OUTPUT_DIR}"
bpftool btf dump file /sys/kernel/btf/vmlinux format c >"${OUTPUT_DIR}/vmlinux.h"
clang -O2 -g -target bpf -D"__TARGET_ARCH_${target_arch}" -I"${OUTPUT_DIR}" +  -c "${PROJECT_ROOT}/ebpf/scheduler.bpf.c" -o "${OUTPUT_DIR}/scheduler.bpf.o"
clang -O2 -g "${PROJECT_ROOT}/ebpf/scheduler_collector.c" +  -o "${OUTPUT_DIR}/scheduler-collector" $(pkg-config --cflags --libs libbpf)
echo "Built ${OUTPUT_DIR}/scheduler.bpf.o"
echo "Built ${OUTPUT_DIR}/scheduler-collector"
