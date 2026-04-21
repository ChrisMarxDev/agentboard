#!/bin/sh
#
# AgentBoard — one-line installer
# ─────────────────────────────────
# Installs the agentboard binary, the Claude skill, and (if Claude Desktop is
# present) an MCP server entry so the dashboard is reachable from Claude.
#
# Usage:
#   curl -fsSL https://agentboard.dev/install.sh | sh
#
# Or pin a specific version:
#   curl -fsSL https://agentboard.dev/install.sh | AGENTBOARD_VERSION=v0.1.0 sh
#
# What this installs:
#   ~/.agentboard/bin/agentboard      the binary
#   ~/.agentboard/data/               project data (SQLite, MDX pages, etc.)
#   ~/.claude/skills/agentboard/      the AgentBoard Claude skill
#   MCP entry in Claude Desktop config (if the config exists)
#
# Uninstall:
#   rm -rf ~/.agentboard ~/.claude/skills/agentboard
#   (then remove the "agentboard" entry from claude_desktop_config.json)
#
# Env vars:
#   AGENTBOARD_VERSION   release tag (default: latest)
#   AGENTBOARD_REF       git ref for skill source (default: main)
#   AGENTBOARD_REPO      owner/repo on GitHub (default: anthropics/agentboard)
#   AGENTBOARD_HOME      install prefix (default: ~/.agentboard)
#   SKIP_MCP             set to 1 to skip Claude Desktop MCP wiring
#   FORCE                set to 1 to overwrite an existing install
#
# Exit codes:
#   0   success
#   1   unsupported platform or missing dependency
#   2   download / network failure
#   3   install collision (set FORCE=1 to overwrite)

set -eu

# ── configuration ────────────────────────────────────────────────────────────

REPO="${AGENTBOARD_REPO:-anthropics/agentboard}"
VERSION="${AGENTBOARD_VERSION:-latest}"
REF="${AGENTBOARD_REF:-main}"
AB_HOME="${AGENTBOARD_HOME:-${HOME}/.agentboard}"
BIN_DIR="${AB_HOME}/bin"
DATA_DIR="${AB_HOME}/data"
SKILL_DIR="${HOME}/.claude/skills/agentboard"

RELEASE_BASE="https://github.com/${REPO}/releases"
RAW_BASE="https://raw.githubusercontent.com/${REPO}/${REF}"

# ── output helpers ───────────────────────────────────────────────────────────

if [ -t 1 ] && command -v tput >/dev/null 2>&1 && [ "$(tput colors 2>/dev/null || echo 0)" -ge 8 ]; then
  C_BOLD=$(tput bold); C_DIM=$(tput dim); C_RED=$(tput setaf 1)
  C_GREEN=$(tput setaf 2); C_YEL=$(tput setaf 3); C_BLUE=$(tput setaf 4)
  C_RESET=$(tput sgr0)
else
  C_BOLD=''; C_DIM=''; C_RED=''; C_GREEN=''; C_YEL=''; C_BLUE=''; C_RESET=''
fi

say()  { printf '%s\n' "$*"; }
step() { printf '%s→%s %s\n' "${C_BLUE}" "${C_RESET}" "$*"; }
ok()   { printf '%s✓%s %s\n' "${C_GREEN}" "${C_RESET}" "$*"; }
warn() { printf '%s!%s %s\n' "${C_YEL}" "${C_RESET}" "$*"; }
err()  { printf '%serror:%s %s\n' "${C_RED}" "${C_RESET}" "$*" >&2; }
die()  { err "$1"; exit "${2:-1}"; }

# ── platform detection ───────────────────────────────────────────────────────

detect_platform() {
  uname_s=$(uname -s)
  uname_m=$(uname -m)

  case "${uname_s}" in
    Darwin) os=darwin ;;
    Linux)  os=linux ;;
    MINGW*|MSYS*|CYGWIN*)
      die "Windows is not yet supported natively. Please use WSL." 1 ;;
    *) die "unsupported OS: ${uname_s}" 1 ;;
  esac

  case "${uname_m}" in
    x86_64|amd64)  arch=amd64 ;;
    arm64|aarch64) arch=arm64 ;;
    *) die "unsupported architecture: ${uname_m}" 1 ;;
  esac

  printf '%s_%s' "${os}" "${arch}"
}

# ── dependency check ─────────────────────────────────────────────────────────

check_deps() {
  missing=''
  for cmd in curl tar mkdir chmod uname; do
    if ! command -v "${cmd}" >/dev/null 2>&1; then
      missing="${missing} ${cmd}"
    fi
  done
  if [ -n "${missing}" ]; then
    die "missing required commands:${missing}" 1
  fi
}

# ── collision guard ──────────────────────────────────────────────────────────

check_existing() {
  if [ -x "${BIN_DIR}/agentboard" ] && [ "${FORCE:-0}" != "1" ]; then
    existing=$("${BIN_DIR}/agentboard" --version 2>/dev/null || echo "unknown")
    warn "AgentBoard already installed (${existing}) at ${BIN_DIR}/agentboard"
    warn "re-run with FORCE=1 to reinstall:"
    warn "  curl -fsSL https://agentboard.dev/install.sh | FORCE=1 sh"
    exit 3
  fi
}

# ── downloaders ──────────────────────────────────────────────────────────────

download() {
  # download <url> <dest>
  url=$1
  dest=$2
  if ! curl -fsSL --retry 3 --retry-delay 1 -o "${dest}" "${url}"; then
    die "failed to download: ${url}" 2
  fi
}

install_binary() {
  platform=$(detect_platform)

  if [ "${VERSION}" = "latest" ]; then
    asset_url="${RELEASE_BASE}/latest/download/agentboard_${platform}.tar.gz"
    sums_url="${RELEASE_BASE}/latest/download/agentboard_checksums.txt"
  else
    asset_url="${RELEASE_BASE}/download/${VERSION}/agentboard_${platform}.tar.gz"
    sums_url="${RELEASE_BASE}/download/${VERSION}/agentboard_checksums.txt"
  fi

  step "Downloading binary (${platform}, ${VERSION})"
  step "  from ${asset_url}"

  tmp=$(mktemp -d 2>/dev/null || mktemp -d -t agentboard)
  trap 'rm -rf "${tmp}"' EXIT INT TERM

  download "${asset_url}" "${tmp}/agentboard.tar.gz"

  # Checksum verification — best effort. Release MUST ship checksums.txt; skip
  # with a warning if it isn't there, rather than silently continuing.
  if curl -fsSL --retry 2 -o "${tmp}/sums.txt" "${sums_url}" 2>/dev/null; then
    if command -v shasum >/dev/null 2>&1; then
      hasher='shasum -a 256'
    elif command -v sha256sum >/dev/null 2>&1; then
      hasher='sha256sum'
    else
      hasher=''
    fi
    if [ -n "${hasher}" ]; then
      expected=$(grep "agentboard_${platform}.tar.gz" "${tmp}/sums.txt" | awk '{print $1}')
      actual=$(${hasher} "${tmp}/agentboard.tar.gz" | awk '{print $1}')
      if [ -z "${expected}" ]; then
        warn "no checksum entry for agentboard_${platform}.tar.gz — skipping verify"
      elif [ "${expected}" != "${actual}" ]; then
        die "checksum mismatch (expected ${expected}, got ${actual})" 2
      else
        ok "checksum verified"
      fi
    else
      warn "no sha256 tool available — skipping checksum verify"
    fi
  else
    warn "no checksums.txt at ${sums_url} — skipping verify"
  fi

  mkdir -p "${BIN_DIR}" "${DATA_DIR}"
  tar -xzf "${tmp}/agentboard.tar.gz" -C "${tmp}"

  if [ ! -f "${tmp}/agentboard" ]; then
    die "archive did not contain an 'agentboard' binary" 2
  fi

  mv "${tmp}/agentboard" "${BIN_DIR}/agentboard"
  chmod +x "${BIN_DIR}/agentboard"
  ok "binary installed: ${BIN_DIR}/agentboard"
}

install_skill() {
  step "Installing Claude skill"
  mkdir -p "${SKILL_DIR}"

  skill_src="${RAW_BASE}/install/skill/SKILL.md"
  download "${skill_src}" "${SKILL_DIR}/SKILL.md"

  ok "skill installed: ${SKILL_DIR}/SKILL.md"
}

# ── Claude Desktop MCP wiring ────────────────────────────────────────────────

desktop_config_path() {
  case "$(uname -s)" in
    Darwin) printf '%s' "${HOME}/Library/Application Support/Claude/claude_desktop_config.json" ;;
    Linux)  printf '%s' "${HOME}/.config/Claude/claude_desktop_config.json" ;;
    *) printf '' ;;
  esac
}

wire_mcp() {
  if [ "${SKIP_MCP:-0}" = "1" ]; then
    step "Skipping MCP wiring (SKIP_MCP=1)"
    return 0
  fi

  config=$(desktop_config_path)
  if [ -z "${config}" ] || [ ! -f "${config}" ]; then
    step "Claude Desktop config not detected — skipping MCP wiring"
    say "  (Claude Code users: the skill handles MCP at runtime.)"
    return 0
  fi

  step "Wiring AgentBoard MCP into ${config}"

  if ! command -v jq >/dev/null 2>&1; then
    warn "jq not installed — can't safely merge your Claude Desktop config."
    warn "Add this to \"mcpServers\" in ${config} manually:"
    say ''
    say '    "agentboard": {'
    say "      \"command\": \"${BIN_DIR}/agentboard\","
    say '      "args": ["mcp"]'
    say '    }'
    say ''
    return 0
  fi

  backup="${config}.agentboard-backup-$(date +%s)"
  cp "${config}" "${backup}"

  tmp="${config}.agentboard.tmp"
  jq --arg bin "${BIN_DIR}/agentboard" \
     '.mcpServers = (.mcpServers // {}) |
      .mcpServers.agentboard = {command: $bin, args: ["mcp"]}' \
     "${config}" > "${tmp}"

  if [ ! -s "${tmp}" ]; then
    rm -f "${tmp}"
    die "jq produced empty output — aborting. Backup at ${backup}" 1
  fi

  mv "${tmp}" "${config}"
  ok "MCP wired (backup: ${backup})"
  say '  Restart Claude Desktop for the MCP server to appear.'
}

# ── PATH hint ────────────────────────────────────────────────────────────────

path_hint() {
  case ":${PATH}:" in
    *":${BIN_DIR}:"*) return 0 ;;
  esac

  say ''
  warn "${BIN_DIR} is not on your PATH."
  say "  To run 'agentboard' from your shell, add this to your profile"
  say "  (~/.zshrc, ~/.bashrc, or equivalent):"
  say ''
  say "    export PATH=\"${BIN_DIR}:\$PATH\""
  say ''
  say "  Or invoke it directly: ${BIN_DIR}/agentboard"
}

# ── summary ──────────────────────────────────────────────────────────────────

print_summary() {
  say ''
  say "${C_BOLD}────────────────────────────────────────────────${C_RESET}"
  say "${C_BOLD}${C_GREEN}  AgentBoard installed ✓${C_RESET}"
  say "${C_BOLD}────────────────────────────────────────────────${C_RESET}"
  say ''
  say "${C_BOLD}Get started in Claude:${C_RESET}"
  say '    Say "start AgentBoard" or "open my dashboard"'
  say ''
  say "${C_BOLD}Or run it directly:${C_RESET}"
  say "    ${BIN_DIR}/agentboard serve"
  say ''
  say "${C_DIM}Installed to:${C_RESET}"
  say "    binary  ${BIN_DIR}/agentboard"
  say "    data    ${DATA_DIR}"
  say "    skill   ${SKILL_DIR}/SKILL.md"
  say ''
  say "${C_DIM}Docs:     https://agentboard.dev${C_RESET}"
  say "${C_DIM}GitHub:   https://github.com/${REPO}${C_RESET}"
  say ''
}

# ── main ─────────────────────────────────────────────────────────────────────

main() {
  say ''
  say "${C_BOLD}AgentBoard installer${C_RESET}"
  say "${C_DIM}  repo:     ${REPO}${C_RESET}"
  say "${C_DIM}  version:  ${VERSION}${C_RESET}"
  say "${C_DIM}  ref:      ${REF}${C_RESET}"
  say "${C_DIM}  prefix:   ${AB_HOME}${C_RESET}"
  say ''

  check_deps
  check_existing
  install_binary
  install_skill
  wire_mcp
  path_hint
  print_summary
}

main "$@"
