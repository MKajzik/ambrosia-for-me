#!/bin/bash
# Builds and runs the iOS UI tests on CI, bounding the two steps that have hung the job for its whole 30-minute
# timeout with no test output: booting the simulator, and the test runner starting. Both are retried instead.
# Needs the API and Postgres already running (see .github/workflows/ios.yml).
set -euo pipefail

BOOT_TIMEOUT=${BOOT_TIMEOUT:-180}   # seconds for one simulator boot
START_TIMEOUT=${START_TIMEOUT:-480} # seconds for the first test suite to start after xcodebuild launches
ATTEMPTS=${ATTEMPTS:-3}

# Kills a process and everything it started.
kill_tree() {
  local child
  for child in $(pgrep -P "$1" 2>/dev/null); do
    kill_tree "$child"
  done
  kill "$1" 2>/dev/null || true
}

# Runs a command, killing it after $1 seconds. macOS has no coreutils `timeout`.
with_timeout() {
  local seconds=$1
  shift
  "$@" &
  local pid=$!
  (sleep "$seconds" && kill_tree "$pid") &
  local watchdog=$!
  local status=0
  wait "$pid" || status=$?
  kill_tree "$watchdog"
  return "$status"
}

boot_simulator() {
  local attempt
  for attempt in $(seq 1 "$ATTEMPTS"); do
    xcrun simctl shutdown "$DEVICE_UDID" 2>/dev/null || true
    # `bootstatus -b` boots the device if needed and returns once it is usable.
    if with_timeout "$BOOT_TIMEOUT" xcrun simctl bootstatus "$DEVICE_UDID" -b; then
      return 0
    fi
    echo "::warning::simulator boot attempt $attempt did not finish within ${BOOT_TIMEOUT}s"
  done
  return 1
}

# Exit status 124 means the tests never started and xcodebuild was killed, so the caller may retry.
run_tests() {
  local log pid waited=0
  log=$(mktemp)
  "${XCODEBUILD[@]}" test-without-building > >(tee "$log") 2>&1 &
  pid=$!
  until grep -q "Test Suite 'All tests' started" "$log" || ! kill -0 "$pid" 2>/dev/null; do
    if [ "$waited" -ge "$START_TIMEOUT" ]; then
      echo "::warning::the tests did not start within ${START_TIMEOUT}s; killing xcodebuild"
      kill_tree "$pid"
      wait "$pid" 2>/dev/null || true
      return 124
    fi
    sleep 5
    waited=$((waited + 5))
  done
  wait "$pid"
}

main() {
  cd "$(dirname "$0")/.."
  xcodegen generate

  # Must be an iOS 26 runtime specifically (the deployment target): matching any "iOS" runtime risks picking one
  # below it if the runner image ships more than one.
  DEVICE_UDID=$(xcrun simctl list devices available -j | /usr/bin/python3 -c "
import json, sys
data = json.load(sys.stdin)['devices']
for runtime, devices in data.items():
    if 'iOS-26' in runtime:
        for d in devices:
            if d['name'].startswith('iPhone'):
                print(d['udid']); raise SystemExit
")
  if [ -z "$DEVICE_UDID" ]; then
    echo "::error::No iOS 26 iPhone simulator runtime found on this runner"
    exit 1
  fi
  XCODEBUILD=(xcodebuild -project MealPlanner.xcodeproj -scheme MealPlanner -destination "id=$DEVICE_UDID")

  "${XCODEBUILD[@]}" build-for-testing

  local attempt status
  for attempt in $(seq 1 "$ATTEMPTS"); do
    boot_simulator || { echo "::error::the simulator never booted"; exit 1; }
    status=0
    run_tests || status=$?
    if [ "$status" -ne 124 ]; then
      exit "$status"
    fi
    xcrun simctl shutdown "$DEVICE_UDID" 2>/dev/null || true
  done
  echo "::error::the tests never started after $ATTEMPTS attempts"
  exit 1
}

if [ "${BASH_SOURCE[0]}" = "$0" ]; then
  main "$@"
fi
