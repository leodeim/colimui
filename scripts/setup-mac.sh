#!/bin/bash
# Install the Docker/Colima stack on a new Mac. Safe to rerun.
set -euo pipefail

if [[ "$(uname -s)" != Darwin ]]; then
  echo "This script is for macOS." >&2
  exit 1
fi
if [[ "$EUID" -eq 0 ]]; then
  echo "Run this as your normal user, without sudo." >&2
  exit 1
fi
if [[ "$#" -ne 0 ]]; then
  echo "Usage: bash scripts/setup-mac.sh" >&2
  exit 1
fi

trap 'echo "Setup stopped at line $LINENO. Fix the error above, then rerun this script." >&2' ERR

# Homebrew may already be installed but absent from this shell's PATH.
if command -v brew >/dev/null 2>&1; then
  brew_bin="$(command -v brew)"
elif [[ -x /opt/homebrew/bin/brew ]]; then
  brew_bin=/opt/homebrew/bin/brew
elif [[ -x /usr/local/bin/brew ]]; then
  brew_bin=/usr/local/bin/brew
else
  echo "Installing Homebrew (may ask for your Mac password and Command Line Tools)…"
  installer="$(mktemp -t colimui-homebrew)"
  trap 'rm -f "$installer"' EXIT
  curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh -o "$installer"
  /bin/bash "$installer"
  if [[ -x /opt/homebrew/bin/brew ]]; then
    brew_bin=/opt/homebrew/bin/brew
  else
    brew_bin=/usr/local/bin/brew
  fi
fi

eval "$("$brew_bin" shellenv)"

# Make Homebrew available in future Terminal sessions too.
case "${SHELL:-/bin/zsh}" in
  */bash) shell_profile="$HOME/.bash_profile" ;;
  */zsh) shell_profile="${ZDOTDIR:-$HOME}/.zprofile" ;;
  *) shell_profile="" ;;
esac
if [[ -n "$shell_profile" ]]; then
  shellenv_line="eval \"\$(\"$brew_bin\" shellenv)\""
  mkdir -p "$(dirname "$shell_profile")"
  touch "$shell_profile"
  if ! grep -Fqx "$shellenv_line" "$shell_profile"; then
    printf '\n# Homebrew (added by colimui setup)\n%s\n' "$shellenv_line" >> "$shell_profile"
  fi
else
  echo "Add Homebrew to your shell startup config using: $brew_bin shellenv"
fi

echo "Installing Colima, Docker, and Docker Compose…"
brew install colima docker docker-compose

# Docker discovers user plugins here, including with a custom DOCKER_CONFIG.
# The Homebrew bin path remains stable across package upgrades.
plugin_dir="${DOCKER_CONFIG:-$HOME/.docker}/cli-plugins"
plugin_path="$plugin_dir/docker-compose"
compose_bin="$(brew --prefix)/bin/docker-compose"
mkdir -p "$plugin_dir"
if [[ -L "$plugin_path" ]] && [[ "$(readlink "$plugin_path")" == "$compose_bin" ]]; then
  echo "Compose plugin is already configured."
else
  if [[ -e "$plugin_path" || -L "$plugin_path" ]]; then
    backup_dir="$(mktemp -d "$plugin_dir/compose-backup.XXXXXX")"
    mv "$plugin_path" "$backup_dir/docker-compose"
    echo "Previous Compose plugin saved in $backup_dir"
  fi
  ln -s "$compose_bin" "$plugin_path"
fi
docker compose version

echo "Starting the default Colima profile…"
colima start --runtime docker
docker context use colima
docker --context colima info --format 'Docker server: {{.ServerVersion}}'

if [[ -n "${DOCKER_HOST:-}" || -n "${DOCKER_CONTEXT:-}" ]]; then
  echo "Your shell sets DOCKER_HOST or DOCKER_CONTEXT; these can override the selected context."
  echo "To use the default Colima context, remove those overrides from your shell configuration."
fi

echo "Setup complete. Open a new terminal, then start your repo normally."
echo "After restarting your Mac, run: colima start"
