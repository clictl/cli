#!/bin/bash
set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Configuration
REPO="clictl/cli"
DOWNLOAD_BASE="https://download.clictl.dev"
BINARY_NAME="clictl"

# Default install directory: ~/.local/bin (user-local, no sudo needed)
# Override with first argument or CLICTL_INSTALL_DIR env var.
if [ -n "$1" ]; then
    INSTALL_DIR="$1"
elif [ -n "$CLICTL_INSTALL_DIR" ]; then
    INSTALL_DIR="$CLICTL_INSTALL_DIR"
else
    INSTALL_DIR="$HOME/.local/bin"
fi

# Detect OS and architecture
detect_system() {
    local os arch

    case "$(uname -s)" in
        Linux*)
            os="linux"
            ;;
        Darwin*)
            os="darwin"
            ;;
        MINGW*|MSYS*|CYGWIN*)
            os="windows"
            ;;
        *)
            echo -e "${RED}Error: Unsupported operating system${NC}"
            exit 1
            ;;
    esac

    case "$(uname -m)" in
        x86_64|amd64)
            arch="amd64"
            ;;
        aarch64|arm64)
            arch="arm64"
            ;;
        *)
            echo -e "${RED}Error: Unsupported architecture: $(uname -m)${NC}"
            exit 1
            ;;
    esac

    echo "$os" "$arch"
}

main() {
    echo ""
    echo -e "${GREEN} ███                   ████   ███            █████    ████${NC}"
    echo -e "${GREEN}░░░███                ░░███  ░░░            ░░███    ░░███${NC}"
    echo -e "${GREEN}  ░░░███       ██████  ░███  ████   ██████  ███████   ░███${NC}"
    echo -e "${GREEN}    ░░░███    ███░░███ ░███ ░░███  ███░░███░░░███░    ░███${NC}"
    echo -e "${GREEN}     ███░    ░███ ░░░  ░███  ░███ ░███ ░░░   ░███     ░███${NC}"
    echo -e "${GREEN}   ███░      ░███  ███ ░███  ░███ ░███  ███  ░███ ███ ░███${NC}"
    echo -e "${GREEN} ███░        ░░██████  █████ █████░░██████   ░░█████  █████${NC}"
    echo -e "${GREEN}░░░           ░░░░░░  ░░░░░ ░░░░░  ░░░░░░     ░░░░░  ░░░░░${NC}"
    echo -e "${GREEN}  > clictl${NC}"
    echo ""

    # Detect system
    read -r os arch < <(detect_system)
    echo "Detected system: $os/$arch"
    echo "Install directory: $INSTALL_DIR"

    # Get latest release from version manifest (with GitHub API fallback)
    echo "Fetching latest release..."
    RELEASE=$(curl -s "${DOWNLOAD_BASE}/version.json" 2>/dev/null | grep -o '"version": "[^"]*' | cut -d'"' -f4)

    if [ -z "$RELEASE" ]; then
        echo "Falling back to GitHub API..."
        RELEASE=$(curl -s "https://api.github.com/repos/$REPO/releases/latest" | grep -o '"tag_name": "[^"]*' | cut -d'"' -f4)
    fi

    if [ -z "$RELEASE" ]; then
        echo -e "${RED}Error: Could not fetch latest release${NC}"
        exit 1
    fi

    echo "Latest release: $RELEASE"

    # Determine filename
    if [ "$os" = "windows" ]; then
        FILENAME="clictl-windows-$arch.zip"
        EXTRACT_CMD="unzip -q"
        BINARY_FILE="clictl.exe"
    else
        FILENAME="clictl-$os-$arch.tar.gz"
        EXTRACT_CMD="tar -xzf"
        BINARY_FILE="clictl"
    fi

    # Download from R2 (with GitHub Releases fallback)
    DOWNLOAD_URL="${DOWNLOAD_BASE}/releases/${RELEASE}/${FILENAME}"
    echo "Downloading from $DOWNLOAD_URL..."

    TMPDIR=$(mktemp -d)
    trap "rm -rf $TMPDIR" EXIT

    if ! curl -fsSL -o "$TMPDIR/$FILENAME" "$DOWNLOAD_URL" 2>/dev/null; then
        echo "Falling back to GitHub Releases..."
        DOWNLOAD_URL="https://github.com/$REPO/releases/download/$RELEASE/$FILENAME"
        if ! curl -fsSL -o "$TMPDIR/$FILENAME" "$DOWNLOAD_URL"; then
            echo -e "${RED}Error: Failed to download $FILENAME${NC}"
            exit 1
        fi
    fi

    # Extract
    echo "Extracting..."
    if ! (cd "$TMPDIR" && $EXTRACT_CMD "$FILENAME"); then
        echo -e "${RED}Error: Failed to extract archive${NC}"
        exit 1
    fi

    # Verify binary exists
    if [ ! -f "$TMPDIR/$BINARY_FILE" ]; then
        echo -e "${RED}Error: Binary not found after extraction${NC}"
        exit 1
    fi

    # Create install directory and move binary
    mkdir -p "$INSTALL_DIR"
    chmod +x "$TMPDIR/$BINARY_FILE"
    mv "$TMPDIR/$BINARY_FILE" "$INSTALL_DIR/$BINARY_NAME"
    INSTALL_PATH="$INSTALL_DIR/$BINARY_NAME"

    echo ""
    echo -e "${GREEN}Installation successful!${NC}"
    echo ""
    echo "  Binary:  $INSTALL_PATH"
    echo "  Version: $RELEASE"
    echo ""

    # Check if install dir is on PATH
    if ! echo ":$PATH:" | grep -q ":$INSTALL_DIR:"; then
        echo -e "${YELLOW}$INSTALL_DIR is not on your PATH.${NC}"
        echo ""

        # Detect shell rc file
        SHELL_RC=""
        if [ -n "$ZSH_VERSION" ] || [ "$(basename "$SHELL")" = "zsh" ]; then
            SHELL_RC="$HOME/.zshrc"
        elif [ -f "$HOME/.bashrc" ]; then
            SHELL_RC="$HOME/.bashrc"
        elif [ -f "$HOME/.bash_profile" ]; then
            SHELL_RC="$HOME/.bash_profile"
        fi

        if [ -n "$SHELL_RC" ] && ! grep -q "$INSTALL_DIR" "$SHELL_RC" 2>/dev/null; then
            echo -e "Add to PATH automatically? This will append to ${BLUE}$SHELL_RC${NC}"
            printf "  Approve? [Y/n] "
            read -r answer
            answer=$(echo "$answer" | tr '[:upper:]' '[:lower:]')
            if [ -z "$answer" ] || [ "$answer" = "y" ] || [ "$answer" = "yes" ]; then
                echo "" >> "$SHELL_RC"
                echo "# clictl" >> "$SHELL_RC"
                echo "export PATH=\"$INSTALL_DIR:\$PATH\"" >> "$SHELL_RC"
                echo -e "${GREEN}Added to $SHELL_RC${NC}"
                echo "  Run: source $SHELL_RC"
            else
                echo "  Skipped. Add manually:"
                echo "    export PATH=\"$INSTALL_DIR:\$PATH\""
            fi
        else
            echo "  Add manually:"
            echo "    export PATH=\"$INSTALL_DIR:\$PATH\""
        fi
        echo ""
    fi

    # Resolve absolute path for MCP config
    BIN_ABS="$(cd "$(dirname "$INSTALL_PATH")" && pwd)/$(basename "$INSTALL_PATH")"

    # Use short name if on PATH, full path otherwise
    if echo ":$PATH:" | grep -q ":$INSTALL_DIR:"; then
        BIN_CMD="$BINARY_NAME"
    else
        BIN_CMD="$BIN_ABS"
    fi

    echo "Get started:"
    echo ""
    echo "  $BIN_CMD install      # set up your agent"
    echo "  $BIN_CMD search       # find tools"
    echo "  $BIN_CMD run          # run a tool"
    echo ""
    echo -e "Docs: ${BLUE}https://clictl.dev/docs${NC}"
}

main "$@"
