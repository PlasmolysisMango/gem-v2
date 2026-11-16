#!/usr/bin/env bash
set -Eeuo pipefail

# 仅编译部署产物；不安装 nginx、不写系统配置，也不重启服务。
ROOT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
OUT_DIR="$ROOT_DIR/dist"

usage() {
  cat <<'EOF'
用法：./deploy.sh

产物：dist/bin/{gem-server,gem-cli,gem-arena} 和 dist/web/。
gem-server 不嵌入前端，由 nginx 托管 dist/web 并代理 /api/ 与 /ws。
可通过 GO、PNPM 指定构建命令（支持空格分隔的参数），通过 GOOS、GOARCH 交叉编译。
示例：PNPM="corepack pnpm" GOOS=linux GOARCH=amd64 ./deploy.sh
nginx 配置和后端运行方式见 README.md。
EOF
}

if [[ $# -gt 0 ]]; then
  case "$1" in
    -h|--help) usage; exit 0 ;;
    *) usage >&2; exit 2 ;;
  esac
fi

read -r -a GO_CMD <<< "${GO:-go}"
read -r -a PNPM_CMD <<< "${PNPM:-pnpm}"
for cmd in "${GO_CMD[0]}" "${PNPM_CMD[0]}"; do
  if ! command -v "$cmd" >/dev/null 2>&1; then
    printf '缺少构建命令：%s。请安装或通过 GO / PNPM 指定。\n' "$cmd" >&2
    exit 1
  fi
done

export GOTOOLCHAIN="${GOTOOLCHAIN:-local}"
trap 'printf "构建失败（第 %s 行），请检查上方错误。\n" "$LINENO" >&2' ERR

cd "$ROOT_DIR"
printf '安装锁定依赖并编译前端…\n'
(
  cd web
  "${PNPM_CMD[@]}" install --frozen-lockfile
  "${PNPM_CMD[@]}" build
)

printf '编译 Go 二进制（无 CGO，不嵌入前端）…\n'
"${GO_CMD[@]}" mod download
mkdir -p "$OUT_DIR/bin" "$OUT_DIR/web"
for app in gem-server gem-cli gem-arena; do
  CGO_ENABLED=0 "${GO_CMD[@]}" build -tags='' -trimpath -ldflags='-s -w' \
    -o "$OUT_DIR/bin/$app" "./cmd/$app"
done
cp -R "$ROOT_DIR/web/dist/." "$OUT_DIR/web/"

printf '\n编译完成：\n  二进制：%s/bin/\n  nginx 静态资源：%s/web/\n' "$OUT_DIR" "$OUT_DIR"
printf '请按照 README.md 配置 nginx 并单独启动 Go 服务。\n'
