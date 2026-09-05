<p align="center">
  <img src="assets/colimui-logo.png" alt="colimui logo" width="350">
</p>

<p align="center">
  <a href="https://github.com/leodeim/colimui/actions/workflows/release.yml"><img src="https://github.com/leodeim/colimui/actions/workflows/release.yml/badge.svg" alt="Build and Release"></a>
  <a href="https://github.com/leodeim/colimui/releases/latest"><img src="https://img.shields.io/github/v/release/leodeim/colimui" alt="Latest Release"></a>
  <a href="https://github.com/leodeim/colimui/releases"><img src="https://img.shields.io/github/downloads/leodeim/colimui/total" alt="Downloads"></a>
  <a href="https://github.com/leodeim/colimui/blob/main/LICENSE"><img src="https://img.shields.io/github/license/leodeim/colimui" alt="License"></a>
</p>

# ColimUI

A lightweight terminal UI for Colima and Docker.

## Install

The installer supports macOS and Linux on Intel/AMD and ARM CPUs. It downloads binary, verifies its checksum, and installs to `/usr/local/bin`; set `INSTALL_DIR` to override it. 

```sh
curl -fsSL https://raw.githubusercontent.com/leodeim/colimui/main/scripts/install.sh | sh
```

### Run

```sh
colimui
```

### Update

```sh
colimui update
```

### Install using Go:

```sh
go install github.com/leodeim/colimui@latest
```

## Keys

| Key | Action |
| --- | --- |
| `?` | Open or close the Actions menu |
| `u` | Open/close Docker usage overview: total CPU, RAM, and storage |
| `c` | Open cleanup confirmation for reclaimable Docker storage |
| `↑` / `↓` or `k` / `j` | Select a container or menu item |
| `Tab`, `←`, `→` | Switch focus between containers and logs |
| `Enter` | Start/stop the selected container, expand/collapse a Compose group, or run the selected menu action |
| `/` | Search containers |
| `R` (`Shift+R`) | Toggle running containers only |
| `Esc` | Clear filters; while searching, cancel editing; in a menu, close it |
| `r` | Refresh profiles and containers |
| `[` / `]` | Switch to the previous/next Colima profile |
| `s` / `x` | Start/stop the current Colima profile |
| `t` | Restart the selected container |
| `d` | Open deletion confirmation for a stopped container |
| `y` / `n` | Confirm/cancel deletion |
| `l` | Reload the selected container's logs |
| `f` | Pause streaming without clearing logs, or resume from the latest 200 lines |
| `L` (`Shift+L`) | Search retained log text (pauses streaming) |
| `T` (`Shift+T`) | Show/hide Docker timestamps |
| `Home` | Load log history from the start, subject to retention limits |
| `Page Up` / `Page Down` | Scroll logs |
| `End` | Jump to the latest logs |
| `q` | Quit, or close the current menu |
| `Ctrl+C` | Quit from any mode |

## New macOS setup script

Installs Homebrew if needed, then Colima, Docker, Docker Compose, and the latest ColimUI release.

```sh
bash -c "$(curl -fsSL https://raw.githubusercontent.com/leodeim/colimui/main/scripts/setup-mac.sh)"
```

References: [Homebrew installation](https://docs.brew.sh/Installation),
[Colima installation](https://colima.run/docs/installation/),
[Compose Homebrew package](https://formulae.brew.sh/formula/docker-compose).
