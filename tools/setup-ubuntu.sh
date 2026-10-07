#!/usr/bin/env bash
# Install the missing build prerequisites on Ubuntu: Node 22, pnpm (the version pinned in
# web/package.json), the Quake-2 submodule and the web dependencies.
# Usage: tools/setup-ubuntu.sh [--e2e]   (--e2e also installs Playwright Chromium)
set -euo pipefail
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
e2e=0; [[ "${1:-}" == "--e2e" ]] && e2e=1

# base tools (most are usually present already)
sudo apt-get update
sudo apt-get install -y build-essential git curl unzip patch perl python3 ca-certificates

# Node 22: NodeSource, falling back to nvm if the release is not supported
if ! command -v node >/dev/null || [[ "$(node -p 'process.versions.node.split(".")[0]')" -lt 22 ]]; then
  if curl -fsSL https://deb.nodesource.com/setup_22.x | sudo -E bash -; then
    sudo apt-get install -y nodejs
  else
    echo "NodeSource failed, using nvm" >&2
    curl -fsSL https://raw.githubusercontent.com/nvm-sh/nvm/v0.40.3/install.sh | bash
    export NVM_DIR="$HOME/.nvm"; . "$NVM_DIR/nvm.sh"
    nvm install 22
  fi
fi
echo "node $(node --version)"

# pnpm at the version pinned in web/package.json
pnpm_ver="$(node -p "require('$root/web/package.json').packageManager.split('@')[1]")"
if command -v corepack >/dev/null; then
  corepack enable 2>/dev/null || sudo corepack enable
  (cd "$root/web" && corepack prepare "pnpm@$pnpm_ver" --activate)
else
  npm install -g "pnpm@$pnpm_ver" 2>/dev/null || sudo npm install -g "pnpm@$pnpm_ver"
fi
echo "pnpm $(cd "$root/web" && pnpm --version)"

# the original C source (test oracle)
git -C "$root" submodule update --init

# web dependencies
(cd "$root/web" && pnpm install)

if [[ $e2e -eq 1 ]]; then
  (cd "$root/web/apps/web" && pnpm exec playwright install --with-deps chromium)
fi

command -v go >/dev/null || echo "WARNING: Go 1.22+ not found (https://go.dev/dl/)" >&2

cat <<'EOF'

Setup done. Next:
  make demo                 # Quake II demo pak0.pak
  make oracle fixtures      # C oracle + golden fixtures (~1 min)
  make test golden
EOF
