#!/usr/bin/env bash
set -euo pipefail

REPO_OWNER="daviddao"
REPO_NAME="clockmail_viewer"
BIN_NAME="cmv"

TMP_DIRS=()

cleanup_tmp_dirs() {
    local dir
    for dir in ${TMP_DIRS[@]+"${TMP_DIRS[@]}"}; do
        [ -n "$dir" ] && rm -rf "$dir"
    done
}

make_tmp_dir() {
    local dir
    dir=$(mktemp -d)
    TMP_DIRS+=("$dir")
    printf '%s\n' "$dir"
}

trap cleanup_tmp_dirs EXIT

default_install_dir() {
    if [ -n "${INSTALL_DIR:-}" ]; then
        echo "$INSTALL_DIR"
        return
    fi

    local user_local_bin="${HOME}/.local/bin"
    if [ -d "$user_local_bin" ] && [ -w "$user_local_bin" ]; then
        echo "$user_local_bin"
        return
    fi

    for dir in /usr/local/bin /opt/homebrew/bin /opt/local/bin; do
        if [ -d "$dir" ] && [ -w "$dir" ]; then
            echo "$dir"
            return
        fi
    done

    IFS=: read -r -a path_entries <<<"${PATH:-}"
    for dir in "${path_entries[@]}"; do
        if [ -d "$dir" ] && [ -w "$dir" ]; then
            echo "$dir"
            return
        fi
    done

    echo "$user_local_bin"
}

is_dir_in_path() {
    local dir="$1"
    case ":${PATH}:" in
        *":${dir}:"*) return 0 ;;
        *) return 1 ;;
    esac
}

check_path_and_warn() {
    local dir="$1"
    if ! is_dir_in_path "$dir"; then
        print_warn "$dir is not in your PATH"
        echo ""
        echo "To add it, add this line to your shell profile (~/.bashrc, ~/.zshrc, etc.):"
        echo ""
        echo "  export PATH=\"$dir:\$PATH\""
        echo ""
    fi
}

INSTALL_DIR="$(default_install_dir)"

print_info() { printf "\033[1;34m==>\033[0m %s\n" "$1"; }
print_success() { printf "\033[1;32m==>\033[0m %s\n" "$1"; }
print_error() { printf "\033[1;31m==>\033[0m %s\n" "$1"; }
print_warn() { printf "\033[1;33m==>\033[0m %s\n" "$1"; }

detect_platform() {
    local os arch

    os="$(uname -s | tr '[:upper:]' '[:lower:]')"
    arch="$(uname -m)"

    case "$os" in
        linux) os="linux" ;;
        darwin) os="darwin" ;;
        mingw*|msys*|cygwin*) os="windows" ;;
        *) print_error "Unsupported OS: $os"; return 1 ;;
    esac

    case "$arch" in
        x86_64|amd64) arch="amd64" ;;
        arm64|aarch64) arch="arm64" ;;
        *) print_error "Unsupported architecture: $arch"; return 1 ;;
    esac

    echo "${os}_${arch}"
}

get_latest_release() {
    local url="https://api.github.com/repos/${REPO_OWNER}/${REPO_NAME}/releases/latest"

    if command -v curl >/dev/null 2>&1; then
        curl -fsSL "$url" 2>/dev/null || return 1
    elif command -v wget >/dev/null 2>&1; then
        wget -qO- "$url" 2>/dev/null || return 1
    else
        return 1
    fi
}

download_file() {
    local url="$1"
    local dest="$2"

    if command -v curl >/dev/null 2>&1; then
        curl -fsSL "$url" -o "$dest" || return 1
    elif command -v wget >/dev/null 2>&1; then
        wget -q "$url" -O "$dest" || return 1
    else
        return 1
    fi
}

ensure_install_dir() {
    local dir="$1"

    if [ -d "$dir" ]; then
        return 0
    fi

    if mkdir -p "$dir" 2>/dev/null; then
        return 0
    fi

    print_info "Creating $dir requires sudo..."
    sudo mkdir -p "$dir"
}

PYTHON_CMD=""

ensure_python() {
    if [ -n "$PYTHON_CMD" ]; then
        return 0
    fi

    if command -v python3 >/dev/null 2>&1; then
        PYTHON_CMD="$(command -v python3)"
        return 0
    fi

    if command -v python >/dev/null 2>&1; then
        PYTHON_CMD="$(command -v python)"
        return 0
    fi

    print_error "Python 3 is required to parse GitHub release metadata."
    return 1
}

select_release_asset() {
    local platform="$1"
    ensure_python || return 1

    local release_json
    release_json=$(cat) || return 1

    CMV_RELEASE_JSON="$release_json" "$PYTHON_CMD" - "$platform" "$BIN_NAME" <<'PY'
import json
import os
import sys


def pick_asset(data, platform, bin_name):
    ext = ".zip" if platform.startswith("windows_") else ".tar.gz"
    assets = data.get("assets") or []

    for asset in assets:
        name = asset.get("name") or ""
        if platform in name and name.endswith(ext):
            url = asset.get("browser_download_url") or ""
            if url:
                return name, url

    for asset in assets:
        name = asset.get("name") or ""
        url = asset.get("browser_download_url") or ""
        if platform.replace("_", "") in name.replace("_", "") and name.endswith(ext) and url:
            return name, url

    return None, None


def main():
    if len(sys.argv) < 3:
        return 1
    platform = sys.argv[1]
    bin_name = sys.argv[2]
    release_json = os.environ.get("CMV_RELEASE_JSON", "")
    if not release_json:
        sys.stderr.write("Missing release metadata\n")
        return 1
    try:
        data = json.loads(release_json)
    except Exception as exc:
        sys.stderr.write(f"Failed to parse release JSON: {exc}\n")
        return 1

    version = data.get("tag_name") or ""
    name, url = pick_asset(data, platform, bin_name)

    print(version)
    print(url or "")
    print(name or "")

    return 0 if url else 1


if __name__ == "__main__":
    raise SystemExit(main())
PY
}

version_ge() {
    local IFS=.
    local i ver1=($1) ver2=($2)
    for ((i=0; i<${#ver1[@]} || i<${#ver2[@]}; i++)); do
        local v1=${ver1[i]:-0}
        local v2=${ver2[i]:-0}
        if ((10#$v1 > 10#$v2)); then return 0; fi
        if ((10#$v1 < 10#$v2)); then return 1; fi
    done
    return 0
}

ensure_go() {
    local min_version="1.21"
    local go_version=""

    if command -v go >/dev/null 2>&1; then
        go_version=$(go version 2>/dev/null | awk '{print $3}' | sed 's/^go//')
        if version_ge "$go_version" "$min_version"; then
            printf '%s' "$go_version"
            return 0
        fi
        print_warn "Go $min_version or later is required. Found: go$go_version"
    else
        print_warn "Go is not installed."
    fi

    if command -v brew >/dev/null 2>&1; then
        print_info "Attempting to install Go via Homebrew..."
        if brew install go || brew upgrade go; then
            go_version=$(go version 2>/dev/null | awk '{print $3}' | sed 's/^go//')
            if version_ge "$go_version" "$min_version"; then
                print_success "Installed Go $go_version via Homebrew"
                printf '%s' "$go_version"
                return 0
            fi
        fi
    fi

    return 1
}

try_binary_install() {
    local platform="$1"
    local tmp_dir

    print_info "Checking for pre-built binary..."

    local release_json
    release_json=$(get_latest_release) || return 1

    local parsed version download_url asset_name
    parsed=$(printf '%s' "$release_json" | select_release_asset "$platform") || true

    version=$(printf '%s' "$parsed" | sed -n '1p')
    download_url=$(printf '%s' "$parsed" | sed -n '2p')
    asset_name=$(printf '%s' "$parsed" | sed -n '3p')

    if [ -z "$download_url" ]; then
        print_warn "No pre-built binary found for $platform"
        return 1
    fi

    if [ -z "$version" ]; then
        version="unknown"
    fi

    print_info "Latest release: $version"
    if [ -n "$asset_name" ]; then
        print_info "Selected asset: $asset_name"
    fi

    print_info "Downloading $download_url..."

    tmp_dir=$(make_tmp_dir)

    local ext=".tar.gz"
    if [[ "$download_url" == *.zip ]]; then
        ext=".zip"
    fi

    local archive_path="$tmp_dir/archive${ext}"

    if ! download_file "$download_url" "$archive_path"; then
        print_warn "Download failed"
        return 1
    fi

    # SHA256 checksum verification (adventure4-ckg).
    # Download checksums.txt from the same release and verify the archive hash.
    local base_url checksums_url checksums_path
    base_url="${download_url%/*}"
    checksums_url="${base_url}/checksums.txt"
    checksums_path="$tmp_dir/checksums.txt"

    print_info "Verifying checksum..."
    if download_file "$checksums_url" "$checksums_path" 2>/dev/null; then
        local expected_hash actual_hash
        expected_hash=$(grep -F "$asset_name" "$checksums_path" | awk '{print $1}')
        if [ -n "$expected_hash" ]; then
            if command -v sha256sum >/dev/null 2>&1; then
                actual_hash=$(sha256sum "$archive_path" | awk '{print $1}')
            elif command -v shasum >/dev/null 2>&1; then
                actual_hash=$(shasum -a 256 "$archive_path" | awk '{print $1}')
            else
                print_warn "No SHA256 tool found (sha256sum or shasum). Skipping verification."
                actual_hash=""
            fi

            if [ -n "$actual_hash" ]; then
                if [ "$actual_hash" != "$expected_hash" ]; then
                    print_error "Checksum mismatch!"
                    print_error "  Expected: $expected_hash"
                    print_error "  Got:      $actual_hash"
                    print_error "The downloaded file may be corrupted or tampered with."
                    return 1
                fi
                print_success "Checksum verified (SHA256)"
            fi
        else
            print_warn "Asset '$asset_name' not found in checksums.txt. Skipping verification."
        fi
    else
        print_warn "Could not download checksums.txt. Skipping checksum verification."
    fi

    print_info "Extracting..."

    if [[ "$ext" == ".zip" ]]; then
        if command -v unzip >/dev/null 2>&1; then
            unzip -q "$archive_path" -d "$tmp_dir"
        else
            print_warn "unzip not found"
            return 1
        fi
    else
        tar -xzf "$archive_path" -C "$tmp_dir"
    fi

    local binary_path
    binary_path=$(find "$tmp_dir" -type f -name "$BIN_NAME" -perm -111 2>/dev/null | head -1)

    if [ -z "$binary_path" ]; then
        binary_path=$(find "$tmp_dir" -type f -name "$BIN_NAME" 2>/dev/null | head -1)
    fi

    if [ -z "$binary_path" ] && [[ "$platform" == windows_* ]]; then
        binary_path=$(find "$tmp_dir" -type f -name "${BIN_NAME}.exe" 2>/dev/null | head -1)
    fi

    if [ -z "$binary_path" ]; then
        print_warn "Binary not found in archive"
        return 1
    fi

    chmod +x "$binary_path"

    ensure_install_dir "$INSTALL_DIR"
    local dest_path="$INSTALL_DIR/$BIN_NAME"

    if [ -w "$INSTALL_DIR" ]; then
        mv "$binary_path" "$dest_path"
    else
        print_info "Installing to $INSTALL_DIR requires sudo..."
        sudo mv "$binary_path" "$dest_path"
    fi

    print_success "Installed $BIN_NAME $version to $dest_path"
    return 0
}

try_go_install() {
    print_info "Attempting to build from source with go build..."

    local go_version
    if ! go_version=$(ensure_go); then
        print_error "Go 1.21 or later is required for building from source."
        exit 1
    fi

    print_info "Using Go $go_version"

    local tmp_dir src_dir repo_url tarball_url tarball_path build_output fetched=0
    tmp_dir=$(make_tmp_dir)
    src_dir="$tmp_dir/src"
    repo_url="https://github.com/${REPO_OWNER}/${REPO_NAME}.git"

    print_info "Fetching source..."

    if command -v git >/dev/null 2>&1; then
        if git clone --depth 1 "$repo_url" "$src_dir" >/dev/null 2>&1; then
            fetched=1
        else
            print_warn "git clone failed, attempting tarball download..."
        fi
    fi

    if [ "$fetched" -ne 1 ]; then
        tarball_url="https://codeload.github.com/${REPO_OWNER}/${REPO_NAME}/tar.gz/refs/heads/main"
        tarball_path="$tmp_dir/source.tar.gz"
        if ! download_file "$tarball_url" "$tarball_path"; then
            print_error "Failed to download source tarball from GitHub."
            exit 1
        fi
        tar -xzf "$tarball_path" -C "$tmp_dir"
        src_dir=$(find "$tmp_dir" -maxdepth 1 -type d -name "${REPO_NAME}-*" | head -1)
        if [ -z "$src_dir" ]; then
            print_error "Could not locate extracted source directory."
            exit 1
        fi
    fi

    print_info "Building $BIN_NAME from source..."
    build_output="$tmp_dir/$BIN_NAME"
    if ! (cd "$src_dir" && GO111MODULE=on CGO_ENABLED=0 go build -o "$build_output" "./cmd/$BIN_NAME"); then
        print_error "Go build failed."
        exit 1
    fi

    ensure_install_dir "$INSTALL_DIR"
    local dest_path="$INSTALL_DIR/$BIN_NAME"

    if [ -w "$INSTALL_DIR" ]; then
        mv "$build_output" "$dest_path"
    else
        print_info "Installing to $INSTALL_DIR requires sudo..."
        sudo mv "$build_output" "$dest_path"
    fi

    print_success "Built and installed $BIN_NAME from source to $dest_path"
    return 0
}

main() {
    print_info "Installing $BIN_NAME (clockmail viewer)..."

    local platform
    platform=$(detect_platform) || {
        print_warn "Could not detect platform, will try building from source"
        try_go_install
        check_path_and_warn "$INSTALL_DIR"
        exit 0
    }

    print_info "Detected platform: $platform"

    if try_binary_install "$platform"; then
        check_path_and_warn "$INSTALL_DIR"
        print_info "Run '$BIN_NAME' in any project with .clockmail/ to monitor agent coordination."
        exit 0
    fi

    print_info "Pre-built binary not available, falling back to source build..."
    try_go_install

    check_path_and_warn "$INSTALL_DIR"
    print_info "Run '$BIN_NAME' in any project with .clockmail/ to monitor agent coordination."
}

if [[ ${BASH_SOURCE+x} != x ]]; then
    main "$@"
elif [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
    main "$@"
fi
