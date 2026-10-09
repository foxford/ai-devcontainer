#!/bin/sh
# Sourced by Bash and Zsh, including login shells.
export ADC_WORKSPACE="${ADC_WORKSPACE:-developer}"
export ASDF_DATA_DIR="$HOME/.asdf"
export AI_TOOLS_HOME=/opt/ai-tools
export NPM_CONFIG_PREFIX="$HOME/.npm-global"
export CLAUDE_CONFIG_DIR="$HOME/.claude"
export GRAPHIFY_VIZ_NODE_LIMIT=50000
export GRAPHIFY_NO_AUTO_REFRESH=1 GRAPHIFY_NO_TIPS=1
export PLAYWRIGHT_BROWSERS_PATH="$HOME/.cache/ms-playwright"
# Restore tools after /etc/profile resets PATH; sourcing twice is harmless.
if [ "${ADC_SHELL_PATH:-}" != "$PATH" ]; then
  export PATH="/opt/adc-agents:$HOME/.asdf/shims:$HOME/.asdf/bin:$AI_TOOLS_HOME/bin:$HOME/.npm-global/bin:$HOME/.local/bin:/usr/local/go/bin:/usr/local/cargo/bin:/usr/local/bun/bin:/usr/local/bin:/usr/sbin:/usr/bin:/bin:$PATH"
  export ADC_SHELL_PATH="$PATH"
fi
mkdir -p "$HOME/.history"
export HISTSIZE=50000 HISTFILESIZE=50000 SAVEHIST=50000
if [ -n "${ZSH_VERSION:-}" ]; then
  export HISTFILE="$HOME/.history/.zsh_history"
else
  export HISTFILE="$HOME/.history/.bash_history"
fi
