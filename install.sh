#!/bin/sh
# POSIX bootstrap for Linux/macOS. All platform operations live in the Go CLI.
# Clones/updates its own platform source when piped to sh. No jq, rsync or system Go required.
set -eu
adc_platform=${AI_DEVCONTAINER_HOME:-"$HOME/.local/share/ai-devcontainer"}
adc_state=${ADC_HOME:-"$HOME/.adc"}
adc_repo=${AI_DEVCONTAINER_REPO:-https://github.com/foxford/ai-devcontainer.git}

adc_root() {
  if [ "$(id -u)" = 0 ]; then "$@"; else sudo "$@"; fi
}
adc_unix=$(uname -s)
case "$adc_unix" in
  Linux)
    if ! command -v git >/dev/null 2>&1 || ! command -v curl >/dev/null 2>&1; then
      if command -v apt-get >/dev/null 2>&1; then
        adc_root apt-get update
        adc_root apt-get install -y git curl ca-certificates tar gzip
      elif command -v dnf >/dev/null 2>&1; then
        adc_root dnf install -y git curl ca-certificates tar gzip
      elif command -v pacman >/dev/null 2>&1; then
        adc_root pacman -S --needed --noconfirm git curl ca-certificates tar gzip
      else
        printf '%s\n' 'Unsupported package manager; use apt, dnf or pacman.' >&2; exit 1
      fi
    fi
    adc_os=linux;;
  Darwin)
    if ! command -v brew >/dev/null 2>&1; then
      adc_brew_script=$(mktemp)
      curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh -o "$adc_brew_script"
      if [ -t 1 ] && [ -r /dev/tty ]; then
        /bin/bash "$adc_brew_script" </dev/tty
      else
        /bin/bash "$adc_brew_script"
      fi
      rm -f "$adc_brew_script"
      if [ -x /opt/homebrew/bin/brew ]; then eval "$(/opt/homebrew/bin/brew shellenv)";
      else eval "$(/usr/local/bin/brew shellenv)"; fi
    fi
    if ! git --version >/dev/null 2>&1; then brew install git; fi
    adc_os=darwin;;
  *) printf '%s\n' 'Windows: use install.ps1 in PowerShell.' >&2; exit 1;;
esac
case "$(uname -m)" in x86_64|amd64) adc_arch=amd64;; arm64|aarch64) adc_arch=arm64;; *) printf '%s\n' 'Unsupported CPU architecture' >&2; exit 1;; esac

# Bootstrap owns its compiler; no system Go or manual checkout is required.
adc_go="$adc_state/toolchains/go1.26.1/go/bin/go"
if ! [ -x "$adc_go" ]; then
  case "$adc_os/$adc_arch" in
    linux/amd64) adc_digest=031f088e5d955bab8657ede27ad4e3bc5b7c1ba281f05f245bcc304f327c987a;;
    linux/arm64) adc_digest=a290581cfe4fe28ddd737dde3095f3dbeb7f2e4065cab4eae44dfc53b760c2f7;;
    darwin/amd64) adc_digest=65773dab2f8cc4cd23d93ba6d0a805de150ca0b78378879292be0b903b8cdd08;;
    darwin/arm64) adc_digest=353df43a7811ce284c8938b5f3c7df40b7bfb6f56cb165b150bc40b5e2dd541f;;
  esac
  adc_tmp=$(mktemp -d)
  trap 'rm -rf "$adc_tmp"' EXIT HUP INT TERM
  curl -fL --retry 3 "https://go.dev/dl/go1.26.1.$adc_os-$adc_arch.tar.gz" -o "$adc_tmp/go.tar.gz"
  if command -v shasum >/dev/null 2>&1; then adc_actual=$(shasum -a 256 "$adc_tmp/go.tar.gz" | awk '{print $1}');
  else adc_actual=$(openssl dgst -sha256 "$adc_tmp/go.tar.gz" | awk '{print $NF}'); fi
  if [ "$adc_actual" != "$adc_digest" ]; then printf '%s\n' 'Go checksum mismatch' >&2; exit 1; fi
  mkdir -p "$adc_state/toolchains/go1.26.1"
  tar -xzf "$adc_tmp/go.tar.gz" -C "$adc_state/toolchains/go1.26.1"
fi
if [ -d "$adc_platform/.git" ]; then
  git -C "$adc_platform" pull --ff-only
else
  git clone --branch main "$adc_repo" "$adc_platform"
fi
if [ ! -f "$adc_platform/go.mod" ] || [ ! -f "$adc_platform/runtime/orca-lock.json" ]; then
  printf '%s\n' 'Platform checkout is incomplete: go.mod or runtime/orca-lock.json missing.' >&2
  exit 1
fi
export AI_DEVCONTAINER_HOME="$adc_platform" ADC_HOME="$adc_state"
export GOCACHE="$adc_state/cache/go-build" GOMODCACHE="$adc_state/cache/go-mod"
mkdir -p "$adc_state/bin"
(cd "$adc_platform" && "$adc_go" build -trimpath -o "$adc_state/bin/adc" ./bin/adc)
"$adc_state/bin/adc" install "$@"
