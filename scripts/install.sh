#!/usr/bin/env sh
# gotun - Installation Script (secure, configurable, with completions)
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/Sesame2/gotun/main/scripts/install.sh | sh
#
# Options:
#   GOTUN_VERSION=vX.Y.Z or --version vX.Y.Z  Specify version to install
#   GOTUN_INSTALL_DIR=/path or --install-dir /path Specify installation directory
#   PREFIX=/path                               Specify prefix (default: ~/.local)
#   --no-completions                           Skip shell completion installation

set -eu
IFS=$(printf '\n\t')

# --- Configuration ---
GITHUB_REPO="Sesame2/gotun"
TARGET_FILE="gotun"
PREFIX_DEFAULT="${PREFIX:-${HOME}/.local}"
INSTALL_DIR_DEFAULT="${PREFIX_DEFAULT}/bin"

# --- Helper Functions ---
info() { printf '[INFO] %s\n' "$*"; }
warn() { printf '[WARN] %s\n' "$*"; }
error() { printf '[ERROR] %s\n' "$*" >&2; exit 1; }

# --- Parse Arguments & Environment Variables ---
GOTUN_VERSION="${GOTUN_VERSION:-}"
INSTALL_DIR_USER_SPECIFIED="${GOTUN_INSTALL_DIR:-}"
INSTALL_DIR="" # Will be determined later
NO_COMPLETIONS=0

while [ $# -gt 0 ]; do
  case "$1" in
    --version) GOTUN_VERSION="${2:-}"; shift 2 ;;
    --install-dir) INSTALL_DIR_USER_SPECIFIED="${2:-}"; shift 2 ;;
    --no-completions) NO_COMPLETIONS=1; shift ;;
    --help|-h)
      cat <<'EOF'
Options:
  --version vX.Y.Z       Specify version to install (default: latest)
  --install-dir DIR      Specify installation directory (default: ~/.local/bin, fallback: /usr/local/bin)
  --no-completions       Do not install shell completions
Env:
  GOTUN_VERSION          Same as --version
  GOTUN_INSTALL_DIR      Same as --install-dir
  PREFIX                 Set prefix for default install dir (default: ~/.local)
EOF
      exit 0
      ;;
    *)
      warn "Unknown argument: $1"
      shift
      ;;
  esac
done

# --- Smart Install Directory Logic ---
is_in_path() {
  case ":${PATH}:" in
    *":$1:"*) return 0 ;;
    *) return 1 ;;
  esac
}

if [ -n "$INSTALL_DIR_USER_SPECIFIED" ]; then
  INSTALL_DIR="$INSTALL_DIR_USER_SPECIFIED"
  info "User specified install directory: $INSTALL_DIR"
else
  INSTALL_DIR="$INSTALL_DIR_DEFAULT"
  if ! is_in_path "$INSTALL_DIR"; then
    warn "Default directory '$INSTALL_DIR' is not in your PATH."
    if command -v sudo >/dev/null 2>&1 && sudo -n true 2>/dev/null; then
      info "Attempting to use fallback directory '/usr/local/bin' with sudo."
      INSTALL_DIR="/usr/local/bin"
    else
      warn "Could not get sudo permissions. Sticking with '$INSTALL_DIR'."
      warn "You will need to add it to your PATH manually."
    fi
  fi
fi

# --- Dependency Check ---
need_cmd() { command -v "$1" >/dev/null 2>&1 || error "Missing dependency: $1"; }
need_cmd tar
if command -v curl >/dev/null 2>&1; then DOWNLOADER=curl
elif command -v wget >/dev/null 2>&1; then DOWNLOADER=wget
else error "Missing dependency: curl or wget"; fi

# --- OS/Architecture Detection ---
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH_RAW=$(uname -m || echo "unknown")
case "$OS" in
  linux)  OS='linux' ;;
  darwin) OS='darwin' ;;
  *) error "Unsupported OS: $OS" ;;
esac
case "$ARCH_RAW" in
  x86_64|amd64) ARCH='amd64' ;;
  aarch64|arm64) ARCH='arm64' ;;
  armv7l) ARCH='armv7' ;;
  riscv64) ARCH='riscv64' ;;
  *) error "Unsupported architecture: $ARCH_RAW" ;;
esac
info "Detected system: ${OS}-${ARCH}"

# --- Version Resolution ---
get_latest_cli_tag() {
  if [ "$DOWNLOADER" = "curl" ]; then
    releases=$(curl -fsSL "https://api.github.com/repos/${GITHUB_REPO}/releases?per_page=30") || return 1
  else
    releases=$(wget -qO- "https://api.github.com/repos/${GITHUB_REPO}/releases?per_page=30") || return 1
  fi
  asset_url=$(printf '%s\n' "$releases" | grep -oE "https://github[.]com/${GITHUB_REPO}/releases/download/v[0-9]+[.][0-9]+[.][0-9]+/gotun_v[0-9]+[.][0-9]+[.][0-9]+_${OS}_${ARCH}[.]tar[.]gz" | head -n1)
  printf '%s\n' "$asset_url" | sed -nE 's@.*/download/([^/]+)/.*@\1@p'
}
if [ -z "${GOTUN_VERSION}" ]; then
  GOTUN_VERSION="$(get_latest_cli_tag || true)"
  [ -n "$GOTUN_VERSION" ] || error "Could not find a CLI release for ${OS}/${ARCH}; retry or specify --version vX.Y.Z."
fi
printf '%s\n' "$GOTUN_VERSION" | grep -Eq '^v[0-9]+[.][0-9]+[.][0-9]+$' || error "Version must have the form vX.Y.Z."
info "Target version: $GOTUN_VERSION"

# --- Download & Install ---
ASSET="gotun_${GOTUN_VERSION}_${OS}_${ARCH}.tar.gz"
BASE_URL="https://github.com/${GITHUB_REPO}/releases/download/${GOTUN_VERSION}"
DOWNLOAD_URL="${BASE_URL}/${ASSET}"
SHA256_URL="${BASE_URL}/SHA256SUMS"
TMP_DIR="$(mktemp -d)"
cleanup() { rm -rf "$TMP_DIR"; }
trap 'code=$?; [ $code -ne 0 ] && echo "[ERROR] Installation failed (exit code $code)" >&2; cleanup' EXIT
fetch() { if [ "$DOWNLOADER" = "curl" ]; then curl -fL "$1" -o "$2"; else wget -q "$1" -O "$2"; fi; }
info "Downloading $DOWNLOAD_URL"
ARCHIVE="${TMP_DIR}/${ASSET}"
fetch "$DOWNLOAD_URL" "$ARCHIVE" || error "Download failed: $DOWNLOAD_URL"
if ! fetch "$SHA256_URL" "${TMP_DIR}/SHA256SUMS"; then
  # The existing v0.8.1 release predates manifests. These immutable archive hashes
  # were checked against GitHub's asset digest and the downloaded bytes.
  case "$GOTUN_VERSION/$OS/$ARCH" in
    v0.8.1/darwin/amd64) digest=c35275c9d656840016c02c7e2804b9e330d9a8bd2a3bc4e13f329cf04ac4158e ;;
    v0.8.1/darwin/arm64) digest=65fed95e82770e03461f258af7ad24a155b1172247a616e7f04228a7695dc9ce ;;
    v0.8.1/linux/amd64) digest=5527f91342b7fe57f53fc8cfef95e39e2edd7089cdaa42696a5bbc7f016e96b9 ;;
    v0.8.1/linux/arm64) digest=7ef455e8f2795bf7678bacc595ed38b7e6e359e9bcbd048691eec2889a91e4b2 ;;
    *) error "SHA256SUMS is required; refusing an unverified download." ;;
  esac
  printf '%s  %s\n' "$digest" "$ASSET" > "${TMP_DIR}/SHA256SUMS"
fi
info "Verifying SHA256 checksum..."
if command -v sha256sum >/dev/null 2>&1; then
  (cd "$TMP_DIR" && awk -v "asset=$ASSET" '$2 == asset' SHA256SUMS | sha256sum -c -) || error "Checksum validation failed."
elif command -v shasum >/dev/null 2>&1; then
  (cd "$TMP_DIR" && awk -v "asset=$ASSET" '$2 == asset' SHA256SUMS | shasum -a 256 -c -) || error "Checksum validation failed."
else
  error "Missing dependency: sha256sum or shasum"
fi
info "Extracting archive..."
EXTRACT_DIR="${TMP_DIR}/out"
mkdir -p "$EXTRACT_DIR"
tar -xzf "$ARCHIVE" -C "$EXTRACT_DIR"
FOUND_BINARY="${EXTRACT_DIR}/${TARGET_FILE}"
if [ ! -f "$FOUND_BINARY" ]; then
  FOUND_BINARY="${EXTRACT_DIR}/gotun_${GOTUN_VERSION}_${OS}_${ARCH}"
fi
[ -f "$FOUND_BINARY" ] || error "CLI binary not found in the archive."
info "Installing to ${INSTALL_DIR}"
if ! mkdir -p "$INSTALL_DIR" 2>/dev/null; then
  need_cmd sudo
  sudo mkdir -p "$INSTALL_DIR"
fi
if [ -w "$INSTALL_DIR" ]; then
  install -m 0755 "$FOUND_BINARY" "${INSTALL_DIR}/${TARGET_FILE}"
else
  sudo install -m 0755 "$FOUND_BINARY" "${INSTALL_DIR}/${TARGET_FILE}"
fi
INSTALLED_PATH="${INSTALL_DIR}/${TARGET_FILE}"
info "Installed: ${INSTALLED_PATH}"

# --- Install Completions ---
if [ "$NO_COMPLETIONS" -ne 1 ]; then
  info "Installing shell completions..."
  # Zsh
  if command -v zsh >/dev/null 2>&1; then
    ZSH_COMP_DIR="${ZDOTDIR:-$HOME}/.zsh/completions"; mkdir -p "$ZSH_COMP_DIR"
    "${INSTALLED_PATH}" completion zsh > "${ZSH_COMP_DIR}/_gotun" || warn "Failed to generate zsh completions."
    info "zsh completions written to: ${ZSH_COMP_DIR}/_gotun"
  fi
  # Bash
  if command -v bash >/dev/null 2>&1; then
    BASH_COMP_DIR="${HOME}/.local/share/bash-completion/completions"; mkdir -p "$BASH_COMP_DIR"
    "${INSTALLED_PATH}" completion bash > "${BASH_COMP_DIR}/gotun" || warn "Failed to generate bash completions."
    info "bash completions written to: ${BASH_COMP_DIR}/gotun"
  fi
  # Fish
  if command -v fish >/dev/null 2>&1; then
    FISH_COMP_DIR="${HOME}/.config/fish/completions"; mkdir -p "$FISH_COMP_DIR"
    "${INSTALLED_PATH}" completion fish > "${FISH_COMP_DIR}/gotun.fish" || warn "Failed to generate fish completions."
    info "fish completions written to: ${FISH_COMP_DIR}/gotun.fish"
  fi
else info "Skipped shell completion installation."; fi

# --- Final Instructions ---
printf '\n✅ Installation complete!\n'
printf '   Version: %s\n' "$GOTUN_VERSION"
printf '   Location: %s\n' "$INSTALLED_PATH"

if ! is_in_path "$INSTALL_DIR"; then
  printf '\n[IMPORTANT] Your PATH is not configured correctly.\n'
  printf '  Run the following command to add gotun to your PATH:\n\n'
  
  SHELL_NAME=$(basename "${SHELL:-sh}")
  case "$SHELL_NAME" in
    bash)
      RC_FILE=~/.bashrc
      ;;
    zsh)
      RC_FILE=~/.zshrc
      ;;
    fish)
      RC_FILE=~/.config/fish/config.fish
      ;;
    *)
      RC_FILE="your shell's config file (e.g., ~/.profile)"
      ;;
  esac

  if [ "$SHELL_NAME" = "fish" ]; then
    printf "    set -U fish_user_paths %s \$fish_user_paths\n" "$INSTALL_DIR"
    if [ "$NO_COMPLETIONS" -ne 1 ]; then
      printf "\n  Then, add the following to '%s':\n\n" "$RC_FILE"
      printf "    # Add gotun completions\n"
      printf "    source %s\n" "${FISH_COMP_DIR}/gotun.fish"
    fi
  else
    printf "    echo 'export PATH=\"%s:\$PATH\"' >> %s\n" "$INSTALL_DIR" "$RC_FILE"
    printf "\n  Then, restart your terminal or run:\n\n"
    printf "    source %s\n" "$RC_FILE"
  fi
fi

printf "\nRun 'gotun --version' to verify the installation.\n"
