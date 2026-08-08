#!/usr/bin/env bash
# Real-app fixture e2e test, run inside an OrbStack Linux VM.
#
# Exercises eos against real language-stack processes (Next.js, Vite,
# Express, Hono over npm/pnpm/bun) instead of the synthetic stub commands
# used by cmd/*_e2e_test.go. For each fixture under testdata/fixtures/ it
# registers the service, starts it, asserts it's actually alive (a real
# HTTP response on its port, for fixtures that are servers), checks logs
# were captured, stops it, and asserts the process is gone. This is the
# gap called out in eos#181: the fixture corpus + OrbStack VPS simulator
# existed but were never wired into anything that runs them.
#
# Must run *inside* the target VM (see the `test-fixtures-orb` Makefile
# target, which orb-runs this script with the repo's $PWD shared in).
#
# Env vars:
#   EOS_BIN            path to the eos binary to install (default: dist/eos-linux-<arch>)
#   FIXTURES_DIR        path to the fixture apps (default: testdata/fixtures, relative to repo root)
#   SCRATCH_DIR          VM-local dir fixtures are copied into before install/build
#                        (default: $HOME/eos-fixture-test). Never operate on
#                        FIXTURES_DIR directly -- it's the host's $PWD shared into
#                        every OrbStack VM; installing/building there races
#                        concurrent VMs and mixes host-arch native addons into a
#                        Linux node_modules.
#   SKIP_NODE_INSTALL=1  don't install node/npm if missing. Fixtures whose
#   SKIP_BUN_INSTALL=1   startup command needs the corresponding interpreter
#   SKIP_PNPM_INSTALL=1  switch from the happy path to a negative-path test:
#                        the interpreter is shadowed off PATH (via a stub
#                        binary that exits 127) for the `eos api run` call
#                        only -- prep/build still uses the real toolchain,
#                        if present, so what's being tested is specifically
#                        "eos tries to start a service whose interpreter
#                        just vanished", not "the fixture never built". If
#                        the real tool is also genuinely absent (bare VM +
#                        skip flag), prep fails naturally and the fixture is
#                        logged as SKIPPED, not FAILED -- there's nothing to
#                        assert against without a built app.
#   SKIP_JQ_INSTALL=1    don't install jq if missing. jq is the script's own
#                        tooling dependency (not eos's) -- if genuinely
#                        absent, the script aborts immediately with a clear
#                        message rather than producing unreadable errors
#                        from every downstream jq call.
#   SKIP_ALL_INSTALL=1   shorthand: sets the default for all four SKIP_*_INSTALL
#                        flags above at once, for a VM that already has every
#                        tool and should never attempt an install. Still
#                        overridable per-tool (e.g. SKIP_ALL_INSTALL=1
#                        SKIP_JQ_INSTALL=0 still installs jq). Does not touch
#                        the negative-path (missing-runtime) test semantics --
#                        those stay controlled per-fixture by the individual
#                        flags above.
#   HTTP_WAIT_SECONDS    max seconds to wait for a server to answer HTTP, or
#                        for vite's build to produce dist/index.html, before
#                        declaring the fixture failed (default: 90). A cold
#                        VM doing a real npm-ecosystem install/build needs
#                        real headroom -- 30s measured too tight in practice.
set -uo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
ARCH="$(uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/')"
EOS_BIN="${EOS_BIN:-$REPO_ROOT/dist/eos-linux-$ARCH}"
FIXTURES_DIR="${FIXTURES_DIR:-$REPO_ROOT/testdata/fixtures}"
SCRATCH_DIR="${SCRATCH_DIR:-$HOME/eos-fixture-test}"
export EOS_BASE_DIR="$SCRATCH_DIR/.eos-state"

SKIP_ALL_INSTALL="${SKIP_ALL_INSTALL:-0}"
SKIP_NODE_INSTALL="${SKIP_NODE_INSTALL:-$SKIP_ALL_INSTALL}"
SKIP_BUN_INSTALL="${SKIP_BUN_INSTALL:-$SKIP_ALL_INSTALL}"
SKIP_PNPM_INSTALL="${SKIP_PNPM_INSTALL:-$SKIP_ALL_INSTALL}"
SKIP_JQ_INSTALL="${SKIP_JQ_INSTALL:-$SKIP_ALL_INSTALL}"
HTTP_WAIT_SECONDS="${HTTP_WAIT_SECONDS:-90}"

PASSED=()
FAILED=()
SKIPPED=()

log() { echo "[test-fixtures-orb] $*"; }
die() { echo "[test-fixtures-orb] FATAL: $*" >&2; exit 1; }

[ -x "$EOS_BIN" ] || die "eos binary not found/executable at $EOS_BIN (run 'make release-local' first)"

# ---- runtime prerequisites (idempotent, honors SKIP_*_INSTALL) ------------
ensure_runtimes() {
	if [ "$SKIP_NODE_INSTALL" = "1" ]; then
		log "SKIP_NODE_INSTALL=1: not installing node (node=$(command -v node || echo absent))"
	elif ! command -v node >/dev/null 2>&1; then
		log "installing node..."
		if command -v apt-get >/dev/null 2>&1; then
			sudo apt-get update -qq && sudo apt-get install -y -qq nodejs npm
		elif command -v dnf >/dev/null 2>&1; then
			sudo dnf install -y -q nodejs npm
		elif command -v apk >/dev/null 2>&1; then
			sudo apk add --no-cache nodejs npm
		else
			die "no known package manager to install node"
		fi
	fi

	if [ "$SKIP_BUN_INSTALL" = "1" ]; then
		log "SKIP_BUN_INSTALL=1: not installing bun (bun=$(command -v bun || echo absent))"
	elif ! command -v bun >/dev/null 2>&1; then
		log "installing bun..."
		curl -fsSL --proto '=https' --tlsv1.2 https://bun.sh/install | bash >/dev/null
		export PATH="$HOME/.bun/bin:$PATH"
	fi

	if [ "$SKIP_PNPM_INSTALL" = "1" ]; then
		log "SKIP_PNPM_INSTALL=1: not installing pnpm (pnpm=$(command -v pnpm || echo absent))"
	elif ! command -v pnpm >/dev/null 2>&1; then
		log "installing pnpm..."
		if command -v corepack >/dev/null 2>&1; then
			corepack enable >/dev/null 2>&1 && corepack prepare pnpm@latest --activate >/dev/null 2>&1
		fi
		command -v pnpm >/dev/null 2>&1 || npm install -g --ignore-scripts pnpm >/dev/null
	fi

	if [ "$SKIP_JQ_INSTALL" = "1" ]; then
		log "SKIP_JQ_INSTALL=1: not installing jq (jq=$(command -v jq || echo absent))"
	elif ! command -v jq >/dev/null 2>&1; then
		log "installing jq..."
		if command -v apt-get >/dev/null 2>&1; then sudo apt-get install -y -qq jq
		elif command -v dnf >/dev/null 2>&1; then sudo dnf install -y -q jq
		elif command -v apk >/dev/null 2>&1; then sudo apk add --no-cache jq
		fi
	fi

	# jq drives every assertion below (including the negative-path ones) --
	# without it the script can't safely tell pass from fail, so this is a
	# hard precondition, not something to soldier on past.
	command -v jq >/dev/null 2>&1 || die "jq is unavailable (SKIP_JQ_INSTALL=$SKIP_JQ_INSTALL) -- cannot make JSON assertions, stopping here"

	log "runtimes: node=$(node --version 2>/dev/null || echo absent) bun=$(bun --version 2>/dev/null || echo absent) pnpm=$(pnpm --version 2>/dev/null || echo absent) jq=$(jq --version 2>/dev/null)"
}

api() { "$EOS_BIN" --no-daemon api "$@"; }

wait_for_http() {
	local port="$1" path="${2:-/}" tries="$HTTP_WAIT_SECONDS"
	while [ "$tries" -gt 0 ]; do
		curl -fsS -o /dev/null "http://127.0.0.1:$port$path" && return 0
		tries=$((tries - 1))
		sleep 1
	done
	return 1
}

# eos keeps the last-known pgid in status output after a stop -- only the
# status field itself flips, so that's what we poll on (confirmed against a
# live VM run, not assumed).
wait_for_stopped() {
	local name="$1" tries=15
	while [ "$tries" -gt 0 ]; do
		local status
		status="$(api status | jq -r --arg n "$name" '.services[] | select(.name==$n) | .status')"
		[ "$status" = "stopped" ] && return 0
		tries=$((tries - 1))
		sleep 1
	done
	return 1
}

# ---- per-fixture prep (fresh install, never trust vendored node_modules) --
prep_fixture() {
	local src="$1" dst="$2"
	rm -rf "$dst"
	mkdir -p "$dst"
	cp -r --no-preserve=mode,ownership "$src"/. "$dst"/
	rm -rf "$dst"/node_modules "$dst"/dist "$dst"/.next

	# Every fixture ships a placeholder `runtime.path` pointing at a
	# maintainer's local nvm install (/root/.nvm/versions/node/...), which
	# doesn't exist on any of these VMs and makes `eos api add` reject the
	# config outright (see eos#194). Not something to work around silently
	# forever -- #194 already tracks eos's misleading error message for it
	# -- but a real, nonexistent path has no place blocking *this* script
	# from testing anything else, so comment it out if present and let eos
	# resolve the interpreter from PATH like every other fixture already
	# configured to do.
	if [ -f "$dst/service.yaml" ] && grep -qE '^\s*path:\s*"/root/\.nvm' "$dst/service.yaml"; then
		sed -i.bak -E 's/^(\s*)(path:\s*"\/root\/\.nvm.*)$/\1# \2/' "$dst/service.yaml"
		rm -f "$dst/service.yaml.bak"
	fi
}

# ---- happy path: fixture is a real long-running server -------------------
run_server_fixture() {
	local name="$1" dir="$2" port="$3" path="${4:-/}"
	log "=== $name ==="
	api add "$dir" >/dev/null || { log "FAIL $name: eos api add failed"; return 1; }
	api run "$name" >/dev/null || { log "FAIL $name: eos api run failed"; return 1; }

	if ! wait_for_http "$port" "$path"; then
		api logs "$name" --lines 50 | jq -r '.lines[]' >&2 || true
		log "FAIL $name: no HTTP response on $path:$port within timeout"
		api stop "$name" --force >/dev/null 2>&1 || true
		api remove "$name" >/dev/null 2>&1 || true
		return 1
	fi
	log "$name: responding on :$port"

	local logs_out
	logs_out="$(api logs "$name" --lines 300 | jq -r '.lines | length')"
	if [ "${logs_out:-0}" -le 0 ]; then
		log "FAIL $name: eos api logs returned no captured output"
		api stop "$name" --force >/dev/null 2>&1 || true
		api remove "$name" >/dev/null 2>&1 || true
		return 1
	fi
	log "$name: $logs_out log lines captured"

	api stop "$name" --force >/dev/null || { log "FAIL $name: eos api stop failed"; return 1; }
	if ! wait_for_stopped "$name"; then
		log "FAIL $name: status never reached 'stopped' after eos api stop"
		return 1
	fi
	log "$name: stopped cleanly"

	api remove "$name" >/dev/null || { log "FAIL $name: eos api remove failed"; return 1; }
	return 0
}

# ---- negative path: simulate the fixture's interpreter going missing -----
# Shadows the given tool name(s) off PATH with a stub that exits 127, scoped
# to the `eos api run` call only (prep/build already happened with the real
# toolchain). Asserts eos surfaces a clean non-"starting" status rather than
# hanging forever with a dead pgid and no diagnostics -- see eos#195, which
# this exact mechanism found: as of 2ef7ae5 it does NOT, so this assertion
# is expected to fail until #195 is fixed. That's intentional: once fixed,
# this stops failing and starts catching a regression if it ever comes back.
run_missing_runtime_fixture() {
	local name="$1" dir="$2" tools="$3"
	log "=== $name (simulating missing: $tools) ==="
	api add "$dir" >/dev/null || { log "FAIL $name: eos api add failed"; return 1; }

	local stub_dir t
	stub_dir="$(mktemp -d)"
	for t in $tools; do
		printf '#!/bin/sh\necho "%s: command not found (simulated missing runtime via test-fixtures-orb)" >&2\nexit 127\n' "$t" >"$stub_dir/$t"
		chmod +x "$stub_dir/$t"
	done

	PATH="$stub_dir:$PATH" "$EOS_BIN" --no-daemon api run "$name" >/dev/null 2>&1
	rm -rf "$stub_dir"

	local tries=15 status
	status="starting"
	while [ "$tries" -gt 0 ]; do
		status="$(api status | jq -r --arg n "$name" '.services[] | select(.name==$n) | .status')"
		[ "$status" != "starting" ] && break
		tries=$((tries - 1))
		sleep 1
	done

	local pgid alive=0
	pgid="$(api status | jq -r --arg n "$name" '.services[] | select(.name==$n) | .pgid')"
	if [ -n "$pgid" ] && [ "$pgid" != "0" ] && kill -0 "$pgid" 2>/dev/null; then
		alive=1
	fi

	api stop "$name" --force >/dev/null 2>&1 || true
	api remove "$name" >/dev/null 2>&1 || true

	if [ "$alive" -eq 1 ]; then
		log "FAIL $name: a process is still alive at pgid $pgid with $tools shadowed off PATH -- eos may have fallen back to some other interpreter on PATH instead of failing"
		return 1
	fi
	if [ "$status" = "starting" ]; then
		log "FAIL $name: stuck in 'starting' with a dead pgid after $tools went missing -- this is eos#195 (unfixed as of this run)"
		return 1
	fi

	local errmsg
	errmsg="$(api status | jq -r --arg n "$name" '.services[] | select(.name==$n) | .error // empty')"
	log "PASS $name: cleanly surfaced as status=$status${errmsg:+ (error: $errmsg)}"
	return 0
}

# ---- vite: build-only fixture (its "command" IS the build step) ----------
run_vite_fixture() {
	local dir="$SCRATCH_DIR/vite"
	prep_fixture "$FIXTURES_DIR/vite" "$dir"

	# vite's own service.yaml command is "pnpm install && pnpm run build",
	# run live by eos -- pre-approve pnpm's native-dep build scripts here
	# (same first-install trap as every other pnpm fixture, see PNPM_PREP)
	# so that when eos runs the real command, its `pnpm install` is already
	# clean instead of failing on the very first invocation.
	if [ "$SKIP_PNPM_INSTALL" != "1" ]; then
		( cd "$dir" && eval "$PNPM_PREP" >/dev/null 2>&1 ) || true
	fi

	api add "$dir" >/dev/null || { log "FAIL vite: eos api add failed"; return 1; }

	if [ "$SKIP_PNPM_INSTALL" = "1" ]; then
		log "=== vite (simulating missing: pnpm) ==="
		local stub_dir
		stub_dir="$(mktemp -d)"
		printf '#!/bin/sh\necho "pnpm: command not found (simulated missing runtime via test-fixtures-orb)" >&2\nexit 127\n' >"$stub_dir/pnpm"
		chmod +x "$stub_dir/pnpm"
		PATH="$stub_dir:$PATH" "$EOS_BIN" --no-daemon api run vite --once >/dev/null 2>&1
		rm -rf "$stub_dir"
		sleep 3
		api stop vite --force >/dev/null 2>&1 || true
		api remove vite >/dev/null 2>&1 || true
		if [ -f "$dir/dist/index.html" ]; then
			log "FAIL vite: dist/index.html exists even with pnpm shadowed off PATH -- build must have used a real pnpm found elsewhere"
			return 1
		fi
		log "PASS vite: build correctly did not complete with pnpm unavailable"
		return 0
	fi

	log "=== vite (build-only) ==="
	api run vite --once >/dev/null || { log "FAIL vite: eos api run failed"; return 1; }
	local tries="$HTTP_WAIT_SECONDS"
	while [ "$tries" -gt 0 ] && [ ! -f "$dir/dist/index.html" ]; do
		tries=$((tries - 1))
		sleep 1
	done
	if [ ! -f "$dir/dist/index.html" ]; then
		api logs vite --lines 100 | jq -r '.lines[]' >&2 || true
		log "FAIL vite: build did not produce dist/index.html within timeout"
		api stop vite --force >/dev/null 2>&1 || true
		api remove vite >/dev/null 2>&1 || true
		return 1
	fi
	api stop vite --force >/dev/null 2>&1 || true
	api remove vite >/dev/null || { log "FAIL vite: eos api remove failed"; return 1; }
	log "PASS vite: build completed, dist/index.html present"
	return 0
}

# ---- one fixture: prep (real toolchain) then happy-path or negative-path -
run_fixture() {
	local name="$1" dir_name="$2" port="$3" prep_cmd="$4" run_tools="$5" skip_var="$6" path="${7:-/}"
	local dir="$SCRATCH_DIR/$dir_name"

	prep_fixture "$FIXTURES_DIR/$dir_name" "$dir"
	log "$name: prep ($prep_cmd)"
	if ! ( cd "$dir" && eval "$prep_cmd" >build.log 2>&1 ); then
		log "SKIP $name: prep failed -- $(tail -5 "$dir/build.log" 2>/dev/null | tr '\n' ' ')"
		SKIPPED+=("$name (prep failed, likely real-absent toolchain)")
		return
	fi

	local ok=0
	if [ "${!skip_var}" = "1" ]; then
		run_missing_runtime_fixture "$name" "$dir" "$run_tools" && ok=1
	else
		run_server_fixture "$name" "$dir" "$port" "$path" && ok=1
	fi

	if [ "$ok" -eq 1 ]; then
		PASSED+=("$name")
	else
		FAILED+=("$name")
	fi
}

# pnpm >=10 refuses to run dependency postinstall/build scripts (native
# addons like sharp) without explicit approval: the FIRST `pnpm install`
# after a clean node_modules always exits 1 for that reason alone (verified
# live -- `--silent` hides the warning, not the exit code, and a naive `&&`
# chain here would make every fixture with native deps look like a prep
# failure). Packages are still installed despite the nonzero exit, so:
# tolerate it, approve, then a second `pnpm install` is clean (exit 0) and
# that's the one gating the rest of the chain. Approval persists at the
# project level (verified: survives a second install+build in the same
# dir), so this only needs to run once per fixture.
PNPM_PREP="pnpm install --silent; pnpm approve-builds --all >/dev/null 2>&1; pnpm install --silent"

main() {
	ensure_runtimes
	rm -rf "$EOS_BASE_DIR"
	mkdir -p "$SCRATCH_DIR"

	run_fixture nextjs nextjs 3001 "$PNPM_PREP && pnpm run build" "node npm" SKIP_NODE_INSTALL /
	run_fixture express-bun express-bun 3000 "bun install --silent" "bun" SKIP_BUN_INSTALL /health
	run_fixture hono-bun hono-bun 3000 "bun install --silent" "bun" SKIP_BUN_INSTALL /health
	run_fixture express-pnpm express-pnpm 3000 "$PNPM_PREP && pnpm run build" "node" SKIP_NODE_INSTALL /health
	run_fixture hono-pnpm hono-pnpm 3000 "$PNPM_PREP && pnpm run build" "node" SKIP_NODE_INSTALL /health

	if run_vite_fixture; then PASSED+=(vite); else FAILED+=(vite); fi

	rm -rf "$EOS_BASE_DIR"
	if [ "${#FAILED[@]}" -eq 0 ]; then
		rm -rf "$SCRATCH_DIR"
	else
		log "FAILED fixtures present -- leaving $SCRATCH_DIR in place for inspection"
	fi

	echo ""
	log "==================== summary ===================="
	log "passed (${#PASSED[@]}): ${PASSED[*]:-none}"
	log "skipped (${#SKIPPED[@]}): ${SKIPPED[*]:-none}"
	log "failed (${#FAILED[@]}): ${FAILED[*]:-none}"

	[ "${#FAILED[@]}" -eq 0 ]
}

main "$@"
