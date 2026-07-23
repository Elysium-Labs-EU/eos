#!/bin/bash
set -euo pipefail

readonly RED='\033[0;31m'
readonly GREEN='\033[0;32m'
readonly YELLOW='\033[1;33m'
readonly BLUE='\033[0;34m'
readonly CYAN='\033[0;36m'
readonly BOLD='\033[1m'
readonly DIM='\033[2m'
readonly NC='\033[0m' # No Color

# Configuration
readonly REPO="Elysium-Labs-EU/eos"
readonly GITHUB_URL="https://github.com"
readonly BINARY_NAME="eos"
readonly INSTALL_DIR="${EOS_INSTALL_DIR:-/usr/local/bin}"
readonly HOME_DIR="${HOME}/.${BINARY_NAME}"

# ECDSA P-256 public key (SubjectPublicKeyInfo, PEM) used to verify the
# detached signature over each release's sha256sums.txt. Keep in sync with
# releaseSigningPublicKeyPEM in cmd/system.go — the matching private key
# lives only as the RELEASE_SIGNING_KEY secret in GitHub Actions.
readonly RELEASE_SIGNING_PUBKEY='-----BEGIN PUBLIC KEY-----
MFkwEwYHKoZIzj0CAQYIKoZIzj0DAQcDQgAEByucQHF5ASSSrPSu6Gb5fvAuWdMw
BNAGlV57YMjkCdpcq8HHRXYXHXqy3cvfIzHYE2UHfftsk83lrSXPkxMyZg==
-----END PUBLIC KEY-----'

AUTO_YES=false

# Print functions
info() {
    echo -e "${BLUE}${BOLD}info${NC} $1"
}

success() {
    echo -e "${GREEN}${BOLD}✓${NC} $1"
}

warn() {
    echo -e "${YELLOW}${BOLD}warning${NC} $1"
}

error() {
    echo -e "${RED}${BOLD}error${NC} $1" >&2
}

step() {
    echo -e "\n${CYAN}${BOLD}→${NC} $1"
}

dim() {
    echo -e "${DIM}$1${NC}"
}

usage() {
    echo "Usage: $0 [OPTIONS]"
    echo ""
    echo "Options:"
    echo "  --local <path>    Use a local binary instead of downloading from GitHub"
    echo "  --help            Show this help message"
    echo "  --yes, -y         Skip all confirmation prompts (non-interactive mode)"
    echo ""
    echo "Environment variables:"
    echo "  EOS_INSTALL_DIR   Install directory (default: /usr/local/bin)"
    echo "  EOS_VERSION       Version to install (default: latest)"
}

confirm() {
    local prompt="$1"
    local default="${2:-n}"

    if [ "$AUTO_YES" = true ]; then
        [[ "$default" =~ ^[Yy]$ ]]
        return $?
    fi


    local response
    
    if [ "$default" = "y" ]; then
        prompt="$prompt [Y/n]"
    else
        prompt="$prompt [y/N]"
    fi
    
    echo -ne "${YELLOW}?${NC} $prompt "
    read -r response
    
    response=${response:-$default}
    [[ "$response" =~ ^[Yy]$ ]]
}

check_root() {
    if [ $EUID -ne 0 ]; then
        error "This script must be run as root"
        dim "  Try: sudo $0"
        exit 1
    fi
}

detect_download_tool() {
    if command -v curl &> /dev/null; then
        echo "curl"
    elif command -v wget &> /dev/null; then
        echo "wget"
    else
        error "Neither curl nor wget is installed"
        echo ""
        echo "Please install one of them:"
        dim "  Debian/Ubuntu: apt-get install curl"
        dim "  RHEL/CentOS:   yum install curl"
        dim "  Alpine:        apk add curl"
        exit 1
    fi
}

download_file() {
    local url="$1"
    local output="$2"
    local tool="$3"
    
    if [ "$tool" = "curl" ]; then
        curl -fsSL -o "$output" "$url" 2>&1 | sed 's/^/  /'
    else
        wget -q --show-progress -O "$output" "$url" 2>&1 | sed 's/^/  /'
    fi
}

# extract_tag_name prints the first "tag_name" value found in a JSON blob
# passed on stdin (a single release's JSON).
extract_tag_name() {
    grep -o '"tag_name"[[:space:]]*:[[:space:]]*"[^"]*"' | sed -E 's/"tag_name"[[:space:]]*:[[:space:]]*"([^"]+)"/\1/' | head -1
}

# pick_latest_tag prints a tag_name from a /releases list JSON blob passed on
# stdin, preferring the highest stable (non-prerelease) tag and only falling
# back to the highest prerelease tag when no stable release exists. GitHub's
# /releases list is documented newest-first but has been observed live to
# return a freshly published release out of list order (issue #43), so list
# position can't be trusted. `sort -V` on the raw tag list isn't a safe
# substitute either: it sorts a bare "v0.1.0" *before* "v0.1.0-rc.9", the
# opposite of semver precedence. Pairing tag_name with prerelease sidesteps
# both problems: both fields are release-level only (never present on the
# nested assets array), so a flat grep of each pairs up 1:1 in list order.
pick_latest_tag() {
    local json scratch stable
    json="$(cat)"
    scratch="$(mktemp -d)"
    printf '%s' "$json" | grep -o '"tag_name"[[:space:]]*:[[:space:]]*"[^"]*"' | sed -E 's/.*"([^"]+)"$/\1/' >"$scratch/tags"
    printf '%s' "$json" | grep -o '"prerelease"[[:space:]]*:[[:space:]]*[a-z]*' | sed -E 's/.*:[[:space:]]*//' >"$scratch/prerelease"
    stable="$(paste -d ' ' "$scratch/prerelease" "$scratch/tags" | awk '$1 == "false" { print $2 }' | sort -V | tail -1)"
    if [ -n "$stable" ]; then
        printf '%s' "$stable"
    else
        sort -V "$scratch/tags" | tail -1
    fi
    rm -rf "$scratch"
}

# fetch_latest_version resolves the newest release tag for $REPO. It tries
# /releases/latest first (GitHub's own "newest published, non-prerelease"
# answer, unaffected by the list-ordering bug below) and only falls back to
# scanning the full /releases list when that 404s — e.g. every release so
# far is a prerelease. This avoids trusting /releases?per_page=1's list
# order, which has been observed to place a freshly published release below
# older ones (issue #43).
fetch_latest_version() {
    local tool="$1"
    local api_base url release_json

    api_base="${EOS_API_BASE:-https://api.github.com/repos/${REPO}}"

    url="${api_base}/releases/latest"
    if [ "$tool" = "curl" ]; then
        release_json=$(curl -fsSL "$url" 2>/dev/null) || true
    else
        release_json=$(wget -qO- "$url" 2>/dev/null) || true
    fi

    if [ -n "$release_json" ]; then
        printf '%s' "$release_json" | extract_tag_name
        return
    fi

    url="${api_base}/releases?per_page=100"
    if [ "$tool" = "curl" ]; then
        release_json=$(curl -fsSL "$url") || return 1
    else
        release_json=$(wget -qO- "$url") || return 1
    fi

    printf '%s' "$release_json" | pick_latest_tag
}

detect_os() {
    case "$(uname -s)" in
        Linux)
            echo "linux"
            ;;
        Darwin)
            echo "darwin"
            ;;
        *)
            error "Unsupported OS: $(uname -s)"
            dim "  Supported: Linux, Darwin (macOS)"
            exit 1
            ;;
    esac
}

detect_arch() {
    local arch
    arch=$(uname -m)

    case $arch in
        x86_64)
            echo "amd64"
            ;;
        aarch64|arm64)
            echo "arm64"
            ;;
        *)
            error "Unsupported architecture: $arch"
            dim "  Supported: x86_64, aarch64/arm64"
            exit 1
            ;;
    esac
}

# strip_quarantine removes the macOS Gatekeeper "com.apple.quarantine" xattr
# from a downloaded binary. No-op on non-Darwin, and tolerant of the
# attribute already being absent.
strip_quarantine() {
    if [ "$(uname -s)" = "Darwin" ]; then
        xattr -d com.apple.quarantine "$1" 2>/dev/null || true
    fi
}

# resign_darwin_binary re-signs a binary in place with an ad-hoc identity
# after it's landed in its final installed location. Go's linker already
# ad-hoc-signs arm64 binaries at build time (a hard OS requirement just to
# run at all on Apple Silicon), but overwriting an existing file in place at
# the same path can leave the kernel's per-vnode code-signature validation
# cache pointing at stale state — observed directly as a SIGKILL with
# "Code Signature Invalid" / CODESIGNING "Invalid Page" on this exact
# install-then-overwrite pattern (see golang/go#42684, golang/go#64351).
# Cheap, local, no network — re-sign unconditionally rather than rely on
# root-causing exactly when the kernel cache goes stale. No-op on non-Darwin.
resign_darwin_binary() {
    if [ "$(uname -s)" = "Darwin" ] && command -v codesign &> /dev/null; then
        codesign --force -s - "$1" 2>/dev/null || true
    fi
}

detect_package_manager() {
    if command -v brew &> /dev/null; then
        echo "brew"
    elif command -v apt-get &> /dev/null; then
        echo "apt"
    elif command -v dnf &> /dev/null; then
        echo "dnf"
    elif command -v yum &> /dev/null; then
        echo "yum"
    elif command -v apk &> /dev/null; then
        echo "apk"
    elif command -v pacman &> /dev/null; then
        echo "pacman"
    else
        echo "unknown"
    fi
}

check_sqlite3() {
    if ! command -v sqlite3 &> /dev/null; then
        return 1
    fi
    
    if ! sqlite3 --version &> /dev/null; then
        return 1
    fi
    
    local test_db="/tmp/sqlite_test_$$.db"
    if ! sqlite3 "$test_db" "SELECT 1;" &> /dev/null; then
        rm -f "$test_db"
        return 1
    fi
    rm -f "$test_db"
    
    return 0
}

install_sqlite3() {
    local pkg_manager="$1"
    
    step "Installing SQLite3..."
    
    case $pkg_manager in
        apt)
            if apt-get update -qq && apt-get install -y -qq sqlite3 > /dev/null 2>&1; then
                success "SQLite3 installed via apt"
                return 0
            fi
            ;;
        yum)
            if yum install -y -q sqlite > /dev/null 2>&1; then
                success "SQLite3 installed via yum"
                return 0
            fi
            ;;
        dnf)
            if dnf install -y -q sqlite > /dev/null 2>&1; then
                success "SQLite3 installed via dnf"
                return 0
            fi
            ;;
        apk)
            if apk add --quiet sqlite > /dev/null 2>&1; then
                success "SQLite3 installed via apk"
                return 0
            fi
            ;;
        pacman)
            if pacman -S --noconfirm --quiet sqlite > /dev/null 2>&1; then
                success "SQLite3 installed via pacman"
                return 0
            fi
            ;;
        brew)
            # Homebrew refuses to run as root by design, and this script
            # requires root (check_root) — can't auto-install here. In
            # practice this rarely matters: macOS ships /usr/bin/sqlite3
            # out of the box, so check_sqlite3 almost always already
            # passes before this function is ever reached on Darwin.
            warn "Homebrew refuses to run as root — can't auto-install via sudo"
            dim "  Run without sudo, then re-run this installer: brew install sqlite"
            return 1
            ;;
        *)
            warn "Could not detect package manager"
            echo ""
            echo "Please install SQLite3 manually:"
            dim "  Debian/Ubuntu:  apt-get install sqlite3"
            dim "  RHEL/CentOS:    yum install sqlite"
            dim "  Fedora:         dnf install sqlite"
            dim "  Alpine:         apk add sqlite"
            dim "  Arch:           pacman -S sqlite"
            dim "  macOS:          brew install sqlite (without sudo)"
            echo ""
            return 1
            ;;
    esac
    
    error "Failed to install SQLite3"
    return 1
}

stop_running_daemon() {
    local eos_bin="${INSTALL_DIR}/${BINARY_NAME}"

    if [ ! -x "$eos_bin" ]; then
        return 0
    fi

    local pid
    pid=$(pgrep -x "$BINARY_NAME" 2>/dev/null || true)
    if [ -z "$pid" ]; then
        return 0
    fi

    echo ""
    warn "eos daemon is running (PID $pid)"
    dim "  Replacing binary while daemon is active may cause issues"
    echo ""

    if confirm "Stop eos daemon before installing?" "y"; then
        if "$eos_bin" daemon stop &>/dev/null; then
            local retries=5
            while [ $retries -gt 0 ]; do
                pid=$(pgrep -x "$BINARY_NAME" 2>/dev/null || true)
                [ -z "$pid" ] && break
                sleep 1
                retries=$((retries - 1))
            done
        fi

        pid=$(pgrep -x "$BINARY_NAME" 2>/dev/null || true)
        if [ -n "$pid" ]; then
            warn "Graceful stop timed out — force killing PID $pid"
            kill -9 "$pid" 2>/dev/null || true
            sleep 1
        fi

        pid=$(pgrep -x "$BINARY_NAME" 2>/dev/null || true)
        if [ -n "$pid" ]; then
            error "Failed to stop eos daemon (PID $pid)"
            if ! confirm "Continue anyway?" "n"; then
                exit 1
            fi
        else
            success "Daemon stopped"
        fi
    else
        warn "Continuing with daemon running"
    fi
}

refresh_completions() {
    local eos_bin="${INSTALL_DIR}/${BINARY_NAME}"
    local target_user="${SUDO_USER:-$(whoami)}"
    local target_home
    target_home=$(getent passwd "$target_user" 2>/dev/null | cut -d: -f6)

    if [ -z "$target_home" ]; then
        return 0
    fi

    # Keep in sync with completionTargetPath() in cmd/completion.go
    local bash_completion="${target_home}/.local/share/bash-completion/completions/${BINARY_NAME}"
    local zsh_completion="${target_home}/.zsh/completions/_${BINARY_NAME}"
    local fish_completion="${target_home}/.config/fish/completions/${BINARY_NAME}.fish"

    local refreshed=false
    if [ -f "$bash_completion" ] && "$eos_bin" completion bash > "$bash_completion" 2>/dev/null; then
        refreshed=true
    fi
    if [ -f "$zsh_completion" ] && "$eos_bin" completion zsh > "$zsh_completion" 2>/dev/null; then
        refreshed=true
    fi
    if [ -f "$fish_completion" ] && "$eos_bin" completion fish > "$fish_completion" 2>/dev/null; then
        refreshed=true
    fi

    if [ "$refreshed" = true ]; then
        success "Refreshed shell completion for ${target_user}"
    fi
}

setup_sqlite3() {
    local pkg_manager="$1"
    
    echo ""
    step "Checking SQLite3..."
    
    if check_sqlite3; then
        local version
        version=$(sqlite3 --version | cut -d' ' -f1)
        success "SQLite3 is already installed (version ${version})"
        dim "  Location: $(command -v sqlite3)"
        return 0
    fi
    
    info "SQLite3 is not installed"
    dim "  SQLite3 is required for storing service state and configuration"
    echo ""
    
    if [ "$pkg_manager" = "unknown" ]; then
        warn "Cannot install automatically (unknown package manager)"
        echo ""
        if ! confirm "Continue without SQLite3? (not recommended)" "n"; then
            error "Installation cancelled"
            exit 1
        fi
        return 1
    fi
    
    if confirm "Install SQLite3 now?" "y"; then
        if install_sqlite3 "$pkg_manager"; then
            if check_sqlite3; then
                local version
                version=$(sqlite3 --version | cut -d' ' -f1)
                success "Installation verified (version ${version})"
                return 0
            else
                warn "Installation completed but verification failed"
                return 1
            fi
        else
            return 1
        fi
    else
        warn "Skipping SQLite3 installation"
        echo ""
        dim "  You can install it later with:"
        case $pkg_manager in
            apt) dim "    sudo apt-get install sqlite3" ;;
            yum) dim "    sudo yum install sqlite" ;;
            dnf) dim "    sudo dnf install sqlite" ;;
            apk) dim "    sudo apk add sqlite" ;;
            pacman) dim "    sudo pacman -S sqlite" ;;
        esac
        echo ""
        
        if ! confirm "Continue without SQLite3? (not recommended)" "n"; then
            error "Installation cancelled"
            exit 1
        fi
        return 1
    fi
}


main() {
    # Parse arguments
    local local_binary=""

    while [[ $# -gt 0 ]]; do
        case "$1" in
            --local)
                if [[ $# -lt 2 ]]; then
                    error "--local requires a path argument"
                    usage
                    exit 1
                fi
                local_binary="$2"
                shift 2
                ;;
            --local=*)
                local_binary="${1#*=}"
                shift
                ;;
            --help|-h)
                usage
                exit 0
                ;;
            --yes|-y)
                AUTO_YES=true
                shift
                ;;
            *)
                error "Unknown option: $1"
                usage
                exit 1
                ;;
        esac
    done

    # Validate local binary if specified
    if [ -n "$local_binary" ]; then
        if [ ! -f "$local_binary" ]; then
            error "Local binary not found: $local_binary"
            exit 1
        fi
    fi

    echo ""
    echo -e "${BOLD}eos installer${NC}"
    echo ""
    
    info "Running pre-flight checks..."
    check_root
    
    local download_tool
    download_tool=$(detect_download_tool)
    dim "  Download tool: $download_tool"
    
    local os
    os=$(detect_os)
    dim "  OS: $os"

    local arch
    arch=$(detect_arch)
    dim "  Architecture: $arch"

    local pkg_manager
    pkg_manager=$(detect_package_manager)
    dim "  Package manager: $pkg_manager"
    
    if [ "$INSTALL_DIR" != "/usr/local/bin" ]; then
        dim "  Install directory: $INSTALL_DIR (custom)"
    fi

    echo ""

    # Version resolution - skip when using a local binary
    local version=""
    if [ -z "$local_binary" ]; then
        version="${EOS_VERSION:-}"
        if [ -z "$version" ]; then
            step "Fetching latest version..."
            version=$(fetch_latest_version "$download_tool") || true

            if [ -z "$version" ]; then
                error "Failed to fetch latest version"
                dim "  Set EOS_VERSION environment variable to specify manually"
                exit 1
            fi
            
            info "Latest version: ${BOLD}$version${NC}"
        else
            info "Using version: ${BOLD}$version${NC}"
        fi
    else
        info "Using local binary: ${BOLD}$local_binary${NC}"
    fi
    
    echo ""
    
    echo -e "${BOLD}Installation plan:${NC}"
    if [ -n "$local_binary" ]; then
        echo "  1. Use local binary: ${local_binary}"
    else
        echo "  1. Download binary from GitHub"
    fi
    echo "  2. Install to ${INSTALL_DIR}/${BINARY_NAME}"
    echo "  3. Install SQLite3 (if needed)"
    echo "  4. Create home directory at ${HOME_DIR}"
    echo ""
    
    if ! confirm "Continue with installation?" "y"; then
        info "Installation cancelled"
        exit 0
    fi
    
    # Get the binary - either from local path or download
    local tmp_binary
    if [ -n "$local_binary" ]; then
        tmp_binary="$local_binary"
        success "Using local binary"
    else
        echo ""
        step "Downloading ${BINARY_NAME} ${version} for ${os}-${arch}..."

        local download_url="${GITHUB_URL}/${REPO}/releases/download/${version}/eos-${os}-${arch}"
        local tmp_dir
        tmp_dir="$(mktemp -d "${TMPDIR:-/tmp}/eos-install.XXXXXXXX")" || { error "Failed to create secure temp dir"; exit 1; }
        trap 'rm -rf "${tmp_dir:-}"' EXIT
        tmp_binary="${tmp_dir}/${BINARY_NAME}"
        
        if ! download_file "$download_url" "$tmp_binary" "$download_tool"; then
            error "Download failed"
            dim "  URL: $download_url"
            exit 1
        fi

        if [ ! -f "$tmp_binary" ]; then
            error "Binary not found after download"
            exit 1
        fi

        success "Downloaded successfully"

        step "Verifying checksum..."
        local checksums_url="${GITHUB_URL}/${REPO}/releases/download/${version}/sha256sums.txt"
        local tmp_checksums="${tmp_dir}/${BINARY_NAME}_sha256sums.txt"

        if ! download_file "$checksums_url" "$tmp_checksums" "$download_tool"; then
            error "Failed to download sha256sums.txt"
            exit 1
        fi

        local binary_name="eos-${os}-${arch}"
        local expected_checksum
        expected_checksum=$(grep "  ${binary_name}$" "$tmp_checksums" | awk '{print $1}')

        if [ -z "$expected_checksum" ]; then
            error "No checksum found for ${binary_name} in sha256sums.txt"
            exit 1
        fi

        local actual_checksum
        actual_checksum=$(sha256sum "$tmp_binary" | awk '{print $1}')

        if [ "$expected_checksum" != "$actual_checksum" ]; then
            error "Checksum mismatch — binary may be corrupted"
            dim "  expected: $expected_checksum"
            dim "  got:      $actual_checksum"
            rm -f "$tmp_binary" "$tmp_checksums"
            exit 1
        fi

        success "Checksum verified"

        step "Verifying release signature..."
        local sig_url="${GITHUB_URL}/${REPO}/releases/download/${version}/sha256sums.txt.sig"
        local tmp_sig="${tmp_dir}/${BINARY_NAME}_sha256sums.txt.sig"

        if download_file "$sig_url" "$tmp_sig" "$download_tool" && [ -s "$tmp_sig" ]; then
            if ! command -v openssl &> /dev/null; then
                error "sha256sums.txt.sig is present but openssl is not installed — cannot verify it"
                dim "  Install openssl or use --local with a binary you've verified yourself"
                rm -f "$tmp_binary" "$tmp_checksums" "$tmp_sig"
                exit 1
            fi

            local tmp_pubkey="${tmp_dir}/release-signing-pubkey.pem"
            printf '%s\n' "$RELEASE_SIGNING_PUBKEY" > "$tmp_pubkey"

            if openssl dgst -sha256 -verify "$tmp_pubkey" -signature "$tmp_sig" "$tmp_checksums" &> /dev/null; then
                success "Signature verified"
            else
                error "Signature verification failed — refusing to install (release may be tampered)"
                rm -f "$tmp_binary" "$tmp_checksums" "$tmp_sig" "$tmp_pubkey"
                exit 1
            fi
        else
            # Soft-fail: releases published before signing was introduced have
            # no sha256sums.txt.sig. Keep in sync with requireReleaseSignature
            # in cmd/system.go — once that flips to true, this should too.
            warn "Release has no sha256sums.txt.sig — checksum-only integrity (release predates signing)"
        fi

        rm -f "$tmp_checksums"
    fi
    
    # Stop running daemon before overwriting binary
    stop_running_daemon

    # Install binary
    step "Installing binary..."
    mkdir -p "$INSTALL_DIR"
    strip_quarantine "$tmp_binary"
    chmod +x "$tmp_binary"
    final_binary="${INSTALL_DIR}/${BINARY_NAME}"
    tmp_install="${final_binary}.tmp.$$"
    cp "$tmp_binary" "$tmp_install"
    mv -f "$tmp_install" "$final_binary"
    resign_darwin_binary "$final_binary"
    success "Installed to ${final_binary}"

    # Refresh any shell completion already installed for the invoking user
    refresh_completions

    # Setup SQLite3 (auto-detects if already installed)
    setup_sqlite3 "$pkg_manager"
    
    # Create home directory
    echo ""
    step "Creating home directory..."
    mkdir -p "$HOME_DIR"
    success "Created ${HOME_DIR}"

    echo ""
    echo -e "${GREEN}${BOLD}Installation complete!${NC}"
    echo ""
    echo -e "${BOLD}Next steps:${NC}"
    echo "  1. Run a service:"
    echo -e "     ${CYAN}eos run -f /path/to/project/service.yaml${NC}"
    echo ""
    echo "  2. Check status:"
    echo -e "     ${CYAN}eos status${NC}"
    echo ""
    echo "  3. View logs:"
    echo -e "     ${CYAN}eos daemon logs${NC}"
    echo ""
    echo -e "${BOLD}Enable tab completion:${NC}"
    echo -e "  bash:  ${CYAN}eos completion bash > /etc/bash_completion.d/eos${NC}"
    echo -e "  zsh:   ${CYAN}eos completion zsh > \"\${fpath[1]}/_eos\"${NC}"
    echo -e "  fish:  ${CYAN}eos completion fish > ~/.config/fish/completions/eos.fish${NC}"
    echo ""
    dim "Database: ${HOME_DIR}/state.db"
    echo ""
}

# EOS_INSTALL_SOURCE_ONLY lets tests `source` this file to call its helper
# functions (e.g. pick_latest_tag) directly, without running the installer.
if [ "${EOS_INSTALL_SOURCE_ONLY:-}" != "1" ]; then
    main "$@"
fi