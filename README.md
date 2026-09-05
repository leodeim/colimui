<p align="center">
  <img src="assets/colimui-logo.png" alt="colimui logo" width="350">
</p>
<p align="center">
  <a href="https://goreportcard.com/report/github.com/leodeim/colimui"><img src="https://goreportcard.com/badge/github.com/leodeim/colimui" alt="go report card"></a>
  <a href="https://github.com/leodeim/colimui/blob/main/go.mod"><img src="https://img.shields.io/github/go-mod/go-version/leodeim/colimui" alt="go version"></a>
  <a href="https://github.com/leodeim/colimui/commits/main"><img src="https://img.shields.io/github/last-commit/leodeim/colimui" alt="last commit"></a>
  <a href="https://github.com/leodeim/colimui"><img src="https://img.shields.io/github/stars/leodeim/colimui" alt="github stars"></a>
</p>

# colimUI

A small terminal UI app for `colima` and `docker`.

## Install

The installer supports macOS and Linux on Intel/AMD and ARM CPUs. It downloads binary, verifies its checksum, and installs to `/usr/local/bin`.

```sh
curl -fsSL https://raw.githubusercontent.com/leodeim/colimui/main/scripts/install.sh | sh
```

Using Go:

```sh
go install github.com/leodeim/colimui@latest
```

### Run

```sh
colimui
```

### Update

```sh
colimui update
```

## New Mac setup

```sh
bash scripts/setup-mac.sh
```

Installs Homebrew if needed, then Colima, Docker, and Docker Compose. Configures
`docker compose`, starts the default Colima profile, selects its Docker context,
and checks the engine connection. Supports Apple Silicon and Intel Macs and can
be rerun. Existing Compose plugins are backed up before replacement.

References: [Homebrew installation](https://docs.brew.sh/Installation),
[Colima installation](https://colima.run/docs/installation/),
[Compose Homebrew package](https://formulae.brew.sh/formula/docker-compose).

## Keys

`s` start colima · `x` stop colima · `enter` expand groups/start-stop services · `t` restart · `d` delete · `l` reload logs · `f` follow logs · `r` refresh · `q` quit
