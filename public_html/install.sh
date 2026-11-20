#!/usr/bin/env sh
set -eu

# Gopkin CLI Installer (macOS / Linux / BOSS Linux / ARM64 IoT)
REPO_URL="https://github.com/CodersCircle/gopkin"
ARCHIVE_URL="${REPO_URL}/archive/refs/heads/main.tar.gz"
INSTALL_DIR="${GOPKIN_INSTALL_DIR:-$HOME/.gopkin/bin}"

echo "Installing Gopkin CLI..."

if ! command -v go >/dev/null 2>&1; then
  echo "Error: Go is required to build Gopkin (zero CGO, zero external deps)." >&2
  echo "Install Go from https://go.dev/dl/ and re-run this script." >&2
  exit 1
fi

mkdir -p "$INSTALL_DIR"
TMP_DIR="$(mktemp -d)"
trap 'rm -rf "$TMP_DIR"' EXIT

echo "Downloading Gopkin source from ${REPO_URL}..."
if command -v curl >/dev/null 2>&1; then
  curl -fsSL "$ARCHIVE_URL" -o "$TMP_DIR/gopkin.tar.gz"
elif command -v wget >/dev/null 2>&1; then
  wget -qO "$TMP_DIR/gopkin.tar.gz" "$ARCHIVE_URL"
else
  echo "Error: curl or wget is required." >&2
  exit 1
fi

tar -xzf "$TMP_DIR/gopkin.tar.gz" -C "$TMP_DIR"
SRC_DIR="$(find "$TMP_DIR" -maxdepth 1 -type d -name 'gopkin-*' | head -n 1)"

echo "Building static zero-CGO gopkin binary..."
(cd "$SRC_DIR" && CGO_ENABLED=0 go build -o "$INSTALL_DIR/gopkin" ./cmd/gopkin)
chmod +x "$INSTALL_DIR/gopkin"

echo ""
echo "Gopkin installed successfully at: $INSTALL_DIR/gopkin"
case ":$PATH:" in
  *":$INSTALL_DIR:"*) ;;
  *)
    echo "Add Gopkin to your PATH:"
    echo "  export PATH=\"$INSTALL_DIR:\$PATH\""
    ;;
esac
echo ""
echo "Get started:"
echo "  gopkin new MyApp && cd MyApp && gopkin run"
