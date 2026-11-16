#!/usr/bin/env bash
# 构建并部署 gem-v2 到 https://<host>/gem/（nginx 子路径）。
# 与 wind / script-play 一致：nginx 发静态资源，Go 只监听回环端口提供 API/WS。
set -Eeuo pipefail

if [[ ${EUID} -ne 0 ]]; then
  echo "请用 root 运行：sudo $0" >&2
  exit 1
fi

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
if [[ "$ROOT" != "/root/workspace/project/gem-v2" ]]; then
  echo "请将仓库放在 /root/workspace/project/gem-v2（deploy/gem-v2.service 使用该路径）" >&2
  exit 1
fi

BASE_PATH="/gem"
SITE_ROOT="/usr/share/nginx/gem"
INTERNAL_CONF="/etc/nginx/conf.d/gem.conf"
PUBLIC_SNIPPET="/etc/nginx/snippets/gem.conf"
IP_CONF="/etc/nginx/conf.d/ip-https.conf"
SERVICE_NAME="gem-v2.service"
LEGACY_SERVICE="gem.service"
ENV_FILE="/etc/gem-v2.env"
INTERNAL_PORT=9095
BACKEND_PORT=9096
PUBLIC_ORIGIN="${GEM_PUBLIC_ORIGIN:-https://47.99.132.211}"
GO="${GO:-/usr/local/go/bin/go}"

step() { printf '\n==> %s\n' "$*"; }

if [[ ! -x "$GO" ]]; then
  GO="$(command -v go || true)"
fi
[[ -x "$GO" ]] || { echo "找不到 Go：$GO" >&2; exit 1; }
command -v pnpm >/dev/null || { echo "缺少 pnpm" >&2; exit 1; }
command -v curl >/dev/null || { echo "缺少 curl" >&2; exit 1; }
command -v python3 >/dev/null || { echo "缺少 python3" >&2; exit 1; }
command -v openssl >/dev/null || { echo "缺少 openssl" >&2; exit 1; }

step "构建前端（base=${BASE_PATH}/）"
(
  cd "$ROOT/web"
  pnpm install --frozen-lockfile
  pnpm exec tsc --noEmit
  pnpm exec vite build --base="${BASE_PATH}/"
)

step "构建服务端"
(
  cd "$ROOT"
  mkdir -p dist/bin
  CGO_ENABLED=0 "$GO" build -trimpath -ldflags="-s -w" -o dist/bin/gem-server ./cmd/gem-server
  CGO_ENABLED=0 "$GO" build -trimpath -ldflags="-s -w" -o dist/bin/gem-cli ./cmd/gem-cli
)

step "发布前端到 ${SITE_ROOT}"
install -d -m 0755 /usr/share/nginx
tmp="$(mktemp -d /usr/share/nginx/gem.new.XXXXXX)"
cp -a "$ROOT/web/dist/." "$tmp/"
chmod -R a+rX "$tmp"
old="${SITE_ROOT}.old.$$"
if [[ -e "$SITE_ROOT" ]]; then
  mv "$SITE_ROOT" "$old"
fi
if ! mv "$tmp" "$SITE_ROOT"; then
  [[ -e "$old" ]] && mv "$old" "$SITE_ROOT"
  exit 1
fi
[[ -e "$old" ]] && rm -rf "$old"

step "安装 systemd 服务"
install -d -m 0700 /var/lib/gem-v2
if [[ ! -f "$ENV_FILE" ]]; then
  umask 077
  printf '# Gem v2 runtime secrets; keep this file private.\nGEM_JWT_SECRET=%s\n' "$(openssl rand -hex 32)" >"$ENV_FILE"
else
  chmod 600 "$ENV_FILE"
  if ! grep -q '^GEM_JWT_SECRET=' "$ENV_FILE"; then
    umask 077
    printf 'GEM_JWT_SECRET=%s\n' "$(openssl rand -hex 32)" >>"$ENV_FILE"
  fi
fi
install -m 0644 "$ROOT/deploy/gem-v2.service" "/etc/systemd/system/${SERVICE_NAME}"

# 旧版 gem（v1）与 /gem/ 子路径、9096 端口冲突，确认新版已构建后再停掉。
if systemctl is-active --quiet "$LEGACY_SERVICE"; then
  step "停止旧版 ${LEGACY_SERVICE}（v2 接替 /gem/ 与 9096）"
  systemctl disable --now "$LEGACY_SERVICE" >/dev/null
fi

step "启动后端"
systemctl daemon-reload
systemctl enable "$SERVICE_NAME" >/dev/null
systemctl restart "$SERVICE_NAME"

for _ in $(seq 1 30); do
  if curl -fsS "http://127.0.0.1:${BACKEND_PORT}/api/healthz" >/dev/null 2>&1; then
    break
  fi
  sleep 0.5
done
curl -fsS "http://127.0.0.1:${BACKEND_PORT}/api/healthz" >/dev/null || {
  echo "后端健康检查失败，查看：journalctl -u ${SERVICE_NAME} -n 80" >&2
  exit 1
}

step "安装 nginx 配置"
BACKUP_DIR="/var/www/deploy-backups/gem-v2/$(date +%Y%m%d-%H%M%S)"
install -d -m 0755 "$BACKUP_DIR"
for f in "$INTERNAL_CONF" "$PUBLIC_SNIPPET" "$IP_CONF"; do
  [[ -f "$f" ]] && cp -a "$f" "$BACKUP_DIR/"
done
install -m 0644 "$ROOT/deploy/nginx/gem.conf" "$INTERNAL_CONF"
install -m 0644 "$ROOT/deploy/nginx/gem-location.conf" "$PUBLIC_SNIPPET"

if ! grep -q 'snippets/gem.conf' "$IP_CONF"; then
  python3 - "$IP_CONF" <<'PY'
import sys

path = sys.argv[1]
text = open(path, encoding="utf-8").read()
line = "    include /etc/nginx/snippets/gem.conf;\n"
if "snippets/gem.conf" in text:
    raise SystemExit(0)
idx = text.rfind("\n}")
if idx < 0:
    raise SystemExit("无法在 ip-https.conf 中找到 server 块结尾")
text = text[: idx + 1] + line + text[idx + 1 :]
open(path, "w", encoding="utf-8").write(text)
PY
fi

nginx -t
systemctl reload nginx

step "验证内部站点"
for _ in $(seq 1 30); do
  if curl -fsS "http://127.0.0.1:${INTERNAL_PORT}/api/healthz" >/dev/null 2>&1; then
    break
  fi
  sleep 0.2
done
curl -fsS "http://127.0.0.1:${INTERNAL_PORT}/api/healthz" >/dev/null
if ! curl -fsS "http://127.0.0.1:${INTERNAL_PORT}/" | grep -q "${BASE_PATH}/assets/"; then
  echo "前端外壳未引用 ${BASE_PATH}/assets/，请检查构建 base" >&2
  exit 1
fi
asset_path="$(curl -fsS "http://127.0.0.1:${INTERNAL_PORT}/" | grep -oE "${BASE_PATH}/assets/[^\"']+\.(js|css)" | head -1)"
[[ -n "$asset_path" ]] || { echo "未找到前端资源路径" >&2; exit 1; }
asset_rel="${asset_path#"${BASE_PATH}/"}"
curl -fsS "http://127.0.0.1:${INTERNAL_PORT}/${asset_rel}" >/dev/null

step "验证公网入口"
public_host="$(printf '%s' "$PUBLIC_ORIGIN" | sed -E 's#^https?://([^/]+).*#\1#')"
for _ in $(seq 1 30); do
  if curl -fsSk --resolve "${public_host}:443:127.0.0.1" \
    "${PUBLIC_ORIGIN}${BASE_PATH}/api/healthz" >/dev/null 2>&1; then
    break
  fi
  sleep 0.2
done
curl -fsSk --resolve "${public_host}:443:127.0.0.1" \
  "${PUBLIC_ORIGIN}${BASE_PATH}/api/healthz" >/dev/null
curl -fsSk --resolve "${public_host}:443:127.0.0.1" \
  "${PUBLIC_ORIGIN}${BASE_PATH}/" | grep -q "${BASE_PATH}/assets/"
curl -fsSk --resolve "${public_host}:443:127.0.0.1" \
  "${PUBLIC_ORIGIN}${asset_path}" >/dev/null
ws_code="$(curl -sk -o /dev/null -w '%{http_code}' --max-time 5 \
  --resolve "${public_host}:443:127.0.0.1" \
  -H 'Connection: Upgrade' -H 'Upgrade: websocket' \
  -H 'Sec-WebSocket-Version: 13' -H 'Sec-WebSocket-Key: Z2VtLXByb2JlLTEyMzQ1Ng==' \
  "${PUBLIC_ORIGIN}${BASE_PATH}/ws")"
if [[ "$ws_code" != "401" ]]; then
  echo "WebSocket 入口异常：期望未登录返回 401，实际 ${ws_code}" >&2
  exit 1
fi

printf '\n部署完成：%s%s/\n' "$PUBLIC_ORIGIN" "$BASE_PATH"
printf '服务：systemctl status %s\n' "$SERVICE_NAME"
printf '日志：journalctl -u %s -f\n' "$SERVICE_NAME"
