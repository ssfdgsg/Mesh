#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SIDER_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"

CONFIG="${CONFIG:-${SIDER_DIR}/config.example.json}"
LISTEN_ADDR="${LISTEN_ADDR:-127.0.0.1:8080}"
PPROF_ADDR="${PPROF_ADDR:-127.0.0.1:6060}"
CONCURRENCY="${CONCURRENCY:-500}"
DURATION="${DURATION:-60s}"
PROFILE_SECONDS="${PROFILE_SECONDS:-30}"
OUT_DIR="${OUT_DIR:-${SIDER_DIR}/.bench}"

need_cmd() {
  command -v "$1" >/dev/null 2>&1 || { echo "missing command: $1" >&2; exit 1; }
}

need_cmd go
need_cmd curl

mkdir -p "${OUT_DIR}"

cleanup() {
  set +e
  [[ -n "${SIDER_PID:-}" ]] && kill "${SIDER_PID}" 2>/dev/null
  [[ -n "${SINK1_PID:-}" ]] && kill "${SINK1_PID}" 2>/dev/null
  [[ -n "${SINK2_PID:-}" ]] && kill "${SINK2_PID}" 2>/dev/null
}
trap cleanup EXIT

wait_pprof() {
  local url="http://${PPROF_ADDR}/debug/pprof/"
  for _ in $(seq 1 50); do
    if curl -fsS "${url}" >/dev/null 2>&1; then
      return 0
    fi
    sleep 0.1
  done
  echo "pprof not reachable: ${url}" >&2
  return 1
}

run_case() {
  local name="$1"
  local write_bytes="$2"

  echo ""
  echo "== case=${name} write_bytes=${write_bytes} =="

  local qps_log="${OUT_DIR}/tcpqps_${name}.log"
  local cpu_pb="${OUT_DIR}/cpu_${name}.pb.gz"
  local top_txt="${OUT_DIR}/top_${name}.txt"

  (
    cd "${SIDER_DIR}"
    go run ./cmd/tcpqps --addr "${LISTEN_ADDR}" -c "${CONCURRENCY}" -d "${DURATION}" --write-bytes "${write_bytes}" --read-bytes 0
  ) | tee "${qps_log}" &
  local qps_pid=$!

  sleep 2
  curl -fsS "http://${PPROF_ADDR}/debug/pprof/profile?seconds=${PROFILE_SECONDS}" -o "${cpu_pb}"
  wait "${qps_pid}"

  go tool pprof -top -nodecount=30 "${cpu_pb}" >"${top_txt}"
  echo "saved: ${qps_log}"
  echo "saved: ${cpu_pb}"
  echo "saved: ${top_txt}"
}

echo "out: ${OUT_DIR}"
echo "config: ${CONFIG}"
echo "listen: ${LISTEN_ADDR}"
echo "pprof: http://${PPROF_ADDR}/debug/pprof/"

echo ""
echo "== start upstream sinks =="
(cd "${SIDER_DIR}" && go run ./cmd/tcpsink --listen 127.0.0.1:9000) >"${OUT_DIR}/tcpsink_9000.log" 2>&1 &
SINK1_PID=$!
(cd "${SIDER_DIR}" && go run ./cmd/tcpsink --listen 127.0.0.1:9001) >"${OUT_DIR}/tcpsink_9001.log" 2>&1 &
SINK2_PID=$!

echo ""
echo "== start sider =="
(cd "${SIDER_DIR}" && go run . --config "${CONFIG}" --pprof "${PPROF_ADDR}") >"${OUT_DIR}/sider.log" 2>&1 &
SIDER_PID=$!

wait_pprof

run_case handshake 0
run_case smallpkt16 16

echo ""
echo "done."

