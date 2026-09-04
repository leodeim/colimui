<p align="center">
  <img src="assets/colimui-logo.png" alt="colimui logo" width="350">
</p>
<p align="center">
  <a href="https://goreportcard.com/report/github.com/leodeim/colimui"><img src="https://goreportcard.com/badge/github.com/leodeim/colimui" alt="go report card"></a>
  <a href="https://github.com/leodeim/colimui/blob/main/go.mod"><img src="https://img.shields.io/github/go-mod/go-version/leodeim/colimui" alt="go version"></a>
  <a href="https://github.com/leodeim/colimui/commits/main"><img src="https://img.shields.io/github/last-commit/leodeim/colimui" alt="last commit"></a>
  <a href="https://github.com/leodeim/colimui"><img src="https://img.shields.io/github/stars/leodeim/colimui" alt="github stars"></a>
</p>

# colimui

a small terminal ui for colima and docker.

## run

```sh
go run .
```

colimui uses the selected colima profile's docker context. press `s` to start a stopped profile.

## keys

`s` start colima · `x` stop colima · `enter` expand groups/start-stop services · `t` restart · `d` delete · `l` reload logs · `f` follow logs · `r` refresh · `q` quit
