#!/usr/bin/env sh
# Download and install the latest colimui release for macOS or Linux.
set -eu

repository="leodeim/colimui"
install_dir="${INSTALL_DIR:-/usr/local/bin}"

os=$(uname -s)
arch=$(uname -m)

case "$os" in
  Darwin) os="darwin" ;;
  Linux) os="linux" ;;
  *)
    echo "Unsupported operating system: $os" >&2
    exit 1
    ;;
esac

case "$arch" in
  x86_64|amd64) arch="amd64" ;;
  arm64|aarch64) arch="arm64" ;;
  *)
    echo "Unsupported architecture: $arch" >&2
    exit 1
    ;;
esac

if ! command -v curl >/dev/null 2>&1; then
  echo "curl is required to install colimui." >&2
  exit 1
fi

binary="colimui_${os}_${arch}"
download_url="https://github.com/${repository}/releases/latest/download"
temp_dir=$(mktemp -d)
trap 'rm -rf "$temp_dir"' EXIT HUP INT TERM

echo "Downloading colimui for ${os}/${arch}..."
curl -fsSL "${download_url}/${binary}" -o "${temp_dir}/${binary}"
curl -fsSL "${download_url}/checksums.txt" -o "${temp_dir}/checksums.txt"

expected_checksum=$(awk -v file="$binary" '$2 == file { print $1 }' "${temp_dir}/checksums.txt")
if [ -z "$expected_checksum" ]; then
  echo "No checksum found for ${binary}." >&2
  exit 1
fi

if command -v sha256sum >/dev/null 2>&1; then
  actual_checksum=$(sha256sum "${temp_dir}/${binary}" | awk '{ print $1 }')
elif command -v shasum >/dev/null 2>&1; then
  actual_checksum=$(shasum -a 256 "${temp_dir}/${binary}" | awk '{ print $1 }')
else
  echo "sha256sum or shasum is required to verify the download." >&2
  exit 1
fi

if [ "$actual_checksum" != "$expected_checksum" ]; then
  echo "Checksum verification failed for ${binary}." >&2
  exit 1
fi

if [ ! -d "$install_dir" ]; then
  if ! mkdir -p "$install_dir" 2>/dev/null; then
    echo "Creating ${install_dir} (you may be asked for your password)..."
    sudo mkdir -p "$install_dir"
  fi
fi

if [ -w "$install_dir" ]; then
  install -m 0755 "${temp_dir}/${binary}" "${install_dir}/colimui"
else
  echo "Installing to ${install_dir} (you may be asked for your password)..."
  sudo install -m 0755 "${temp_dir}/${binary}" "${install_dir}/colimui"
fi

echo "colimui installed to ${install_dir}/colimui"
"${install_dir}/colimui" --version
