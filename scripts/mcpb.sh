#!/usr/bin/env bash
# Збирає dist/prro-mcp.mcpb — пакет для Claude Desktop — із бінарників,
# які goreleaser поклав у dist/: універсальний для macOS і windows/amd64
# (Windows на arm64 запускає його через емуляцію). Поруч кладе
# dist/server.json для реєстру MCP з версією і SHA-256 саме цього пакета.
#
#   scripts/mcpb.sh 0.1.0
set -euo pipefail

version=${1:?usage: scripts/mcpb.sh VERSION}
version=${version#v}
root=$(cd "$(dirname "$0")/.." && pwd)
dist=$root/dist

darwin=$(find "$dist" -path '*darwin_all*' -name prro-mcp -type f | head -n1)
windows=$(find "$dist" -path '*windows_amd64*' -name prro-mcp.exe -type f | head -n1)
[[ -n $darwin && -n $windows ]] || { echo "mcpb: run goreleaser first (dist/ lacks darwin_all or windows_amd64 binaries)" >&2; exit 1; }

stage=$(mktemp -d)
trap 'rm -rf "$stage"' EXIT
mkdir "$stage/server"
cp "$darwin" "$stage/server/prro-mcp"
cp "$windows" "$stage/server/prro-mcp.exe"
chmod +x "$stage/server/prro-mcp"
cp "$root/mcpb/icon.png" "$root/LICENSE" "$stage/"
jq --arg v "$version" '.version = $v' "$root/mcpb/manifest.json" > "$stage/manifest.json"

out=$dist/prro-mcp.mcpb
rm -f "$out"
(cd "$stage" && zip -qrX "$out" .)
sha=$(sha256sum "$out" | cut -d' ' -f1)

jq --arg v "$version" --arg sha "$sha" '
  .version = $v
  | .packages |= map(
      if .registryType == "mcpb" then
        .identifier = "https://github.com/prro-cloud/prro-mcp/releases/download/v\($v)/prro-mcp.mcpb"
        | .fileSha256 = $sha
      elif .registryType == "oci" then
        .identifier = "ghcr.io/prro-cloud/prro-mcp:\($v)"
      else . end)
' "$root/server.json" > "$dist/server.json"
echo "$out sha256=$sha"
echo "$dist/server.json"
