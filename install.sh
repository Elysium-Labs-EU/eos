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
readonly BINARY_NAME="eos"
readonly INSTALL_DIR="${EOS_INSTALL_DIR:-/usr/local/bin}"
readonly HOME_DIR="${HOME}/.${BINARY_NAME}"

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
    echo ""
    echo "Environment variables:"
    echo "  EOS_INSTALL_DIR   Install directory (default: /usr/local/bin)"
    echo "  EOS_VERSION       Version to install (default: latest)"
}

confirm() {
    local prompt="$1"
    local default="${2:-n}"
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

# Check if running as root
check_root() {
    if [ $EUID -ne 0 ]; then
        error "This script must be run as root"
        dim "  Try: sudo $0"
        exit 1
    fi
}

# Detect download tool
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

# Download file
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

# Fetch JSON field
fetch_json_field() {
    local url="$1"
    local field="$2"
    local tool="$3"
    
    local response
    if [ "$tool" = "curl" ]; then
        response=$(curl -fsSL "$url")
    else
        response=$(wget -qO- "$url")
    fi
    
    echo "$response" | grep "\"$field\"" | sed -E 's/.*"([^"]+)".*/\1/' | head -1
}

# Detect architecture
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

# Detect package manager
detect_package_manager() {
    if command -v apt-get &> /dev/null; then
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

# Check if SQLite3 is installed and functional
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

# Install SQLite
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
        *)
            warn "Could not detect package manager"
            echo ""
            echo "Please install SQLite3 manually:"
            dim "  Debian/Ubuntu:  apt-get install sqlite3"
            dim "  RHEL/CentOS:    yum install sqlite"
            dim "  Fedora:         dnf install sqlite"
            dim "  Alpine:         apk add sqlite"
            dim "  Arch:           pacman -S sqlite"
            echo ""
            return 1
            ;;
    esac
    
    error "Failed to install SQLite3"
    return 1
}

# Setup SQLite3 with smart detection
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

    # Version resolution — skip when using a local binary
    local version=""
    if [ -z "$local_binary" ]; then
        version="${EOS_VERSION:-}"
        if [ -z "$version" ]; then
            step "Fetching latest version..."
            version=$(fetch_json_field "https://api.github.com/repos/${REPO}/releases/latest" "tag_name" "$download_tool")
            
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
    
    # Get the binary — either from local path or download
    local tmp_binary
    if [ -n "$local_binary" ]; then
        tmp_binary="$local_binary"
        success "Using local binary"
    else
        echo ""
        step "Downloading ${BINARY_NAME} ${version} for linux-${arch}..."
        
        local download_url="https://github.com/${REPO}/releases/download/${version}/eos-linux-${arch}"
        tmp_binary="/tmp/${BINARY_NAME}"
        
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
    fi
    
    # Install binary
    step "Installing binary..."
    mkdir -p "$INSTALL_DIR"
    chmod +x "$tmp_binary"
    cp "$tmp_binary" "${INSTALL_DIR}/${BINARY_NAME}"
    success "Installed to ${INSTALL_DIR}/${BINARY_NAME}"
    
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
    echo "  1. Register a service:"
    echo -e "     ${CYAN}eos add /path/to/project${NC}"
    echo ""
    echo "  2. Check status:"
    echo -e "     ${CYAN}eos status${NC}"
    echo ""
    echo "  3. View logs:"
    echo -e "     ${CYAN}eos daemon logs${NC}"
    echo ""
    dim "Database: ${HOME_DIR}/state.db"
    dim "Service configs as 'service.yaml' in respective app directory"
    echo ""
}

main "$@"