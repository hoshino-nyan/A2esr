#!/bin/bash
# API 2 Cursor Docker 一键部署脚本
# 适用于 Linux；支持 deploy / upgrade / uninstall / reset-data

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

log_info() { echo -e "${GREEN}[INFO]${NC} $1"; }
log_warn() { echo -e "${YELLOW}[WARN]${NC} $1"; }
log_error() { echo -e "${RED}[ERROR]${NC} $1"; }
log_prompt() { echo -e "${BLUE}[INPUT]${NC} $1"; }

check_command() {
  if ! command -v "$1" &>/dev/null; then
    log_error "$1 未安装，请先安装 $1"
    exit 1
  fi
}

generate_random_key() {
  if command -v openssl &>/dev/null; then
    openssl rand -hex 24
    return
  fi
  docker run --rm alpine:3.20 sh -c "apk add --no-cache openssl >/dev/null 2>&1 && openssl rand -hex 24"
}

read_with_default() {
  local prompt="$1"
  local default="$2"
  local value
  read -r -p "$prompt [$default]: " value || true
  value=${value//$'\r'/}
  echo "${value:-$default}"
}

get_env_value() {
  local file="$1"
  local key="$2"
  [ -f "$file" ] || return 0
  local value
  value=$(grep -m 1 "^${key}=" "$file" 2>/dev/null | cut -d'=' -f2- || true)
  value=${value//$'\r'/}
  printf '%s' "$value"
}

write_env_file() {
  local env_file="$1"
  local tmp_file="${env_file}.tmp"

  while IFS= read -r line || [ -n "$line" ]; do
    line=${line//$'\r'/}
    case "$line" in
      PORT=*) printf '%s\n' "PORT=$PORT" ;;
      LISTEN_ADDR=*) printf '%s\n' "LISTEN_ADDR=${LISTEN_ADDR:-127.0.0.1}" ;;
      ADMIN_TOKEN=*) printf '%s\n' "ADMIN_TOKEN=$ADMIN_TOKEN" ;;
      DB_PATH=*) printf '%s\n' "DB_PATH=$DB_PATH" ;;
      DATA_DIR=*) printf '%s\n' "DATA_DIR=$DATA_DIR" ;;
      DEBUG_MODE=*) printf '%s\n' "DEBUG_MODE=$DEBUG_MODE" ;;
      *) printf '%s\n' "$line" ;;
    esac
  done <"$env_file" >"$tmp_file"

  mv "$tmp_file" "$env_file"
}

prepare_compose() {
  if [ ! -f compose.yml ]; then
    log_error "未找到 compose.yml，请在 A2esr 项目根目录运行此脚本"
    exit 1
  fi

  log_info "检查系统依赖..."
  check_command docker

  if docker compose version &>/dev/null; then
    DOCKER_COMPOSE="docker compose"
  elif command -v docker-compose &>/dev/null; then
    DOCKER_COMPOSE="docker-compose"
  else
    log_error "未安装 docker compose 或 docker-compose"
    exit 1
  fi
  log_info "使用命令: $DOCKER_COMPOSE"

  COMPOSE_FILES="-f compose.yml"
  compose() {
    $DOCKER_COMPOSE $COMPOSE_FILES --env-file .env "$@"
  }

  if ! docker info &>/dev/null; then
    log_error "Docker 未运行，请先启动 Docker"
    exit 1
  fi
}

ensure_env() {
  ENV_BACKUP_FILE=""
  if [ -f .env ]; then
    log_warn ".env 已存在"
    read -r -p "是否重新生成配置（会备份当前 .env）？(y/N): " -n 1 -r
    echo
    if [[ ! $REPLY =~ ^[Yy]$ ]]; then
      log_info "保留现有 .env"
      return 0
    fi
    ENV_BACKUP_FILE=".env.bak.$(date +%Y%m%d_%H%M%S)"
    cp .env "$ENV_BACKUP_FILE"
    log_info "已备份到 $ENV_BACKUP_FILE"
  fi

  if [ ! -f .env.example ]; then
    log_error "缺少 .env.example"
    exit 1
  fi
  cp .env.example .env

  log_info "=== API 2 Cursor Docker 部署配置 ==="
  PORT=$(read_with_default "服务端口（宿主机映射端口）" "28473")

  OLD_ADMIN_TOKEN=$(get_env_value "${ENV_BACKUP_FILE:-}" "ADMIN_TOKEN")
  if [ -n "$OLD_ADMIN_TOKEN" ] && [ "$OLD_ADMIN_TOKEN" != "your-admin-token" ]; then
    ADMIN_TOKEN="$OLD_ADMIN_TOKEN"
    log_info "沿用备份中的 ADMIN_TOKEN"
  else
    ADMIN_TOKEN=$(generate_random_key)
    log_info "已自动生成 ADMIN_TOKEN"
  fi

  DB_PATH="data/api2cursor.db"
  DATA_DIR="data"
  DEBUG_MODE=$(read_with_default "调试模式（off/simple/verbose）" "off")

  write_env_file .env
  log_info "环境变量已写入 .env"
}

# compose.yml 含 build: 时使用本地构建；仅 image 时使用仓库镜像（可 docker load 离线导入）
compose_has_build() {
  [ -f compose.yml ] && grep -qE '^[[:space:]]*build:' compose.yml
}

compose_up_release() {
  compose down 2>/dev/null || true
  set +e
  if compose_has_build; then
    log_info "检测到 build 配置：本地构建镜像并启动..."
    compose up -d --build
  else
    log_info "使用预构建镜像：尝试 pull（需联网；离线请先 docker load 镜像包）..."
    compose pull || log_warn "pull 未成功，将使用本地已有镜像"
    compose up -d
  fi
  up_rc=$?
  set -e
  return $up_rc
}

health_check() {
  local health_url="http://127.0.0.1:${PORT}/health"
  log_info "等待健康检查（约 10s）..."
  sleep 10

  if command -v curl &>/dev/null; then
    if curl -fsS "$health_url" >/dev/null 2>&1; then
      return 0
    fi
  fi

  local status_output
  status_output=$(compose ps 2>/dev/null || true)
  if echo "$status_output" | grep -qEi 'Restarting|Exited'; then
    return 1
  fi
  return 0
}

deploy() {
  echo ""
  log_info "开始部署 API 2 Cursor..."
  prepare_compose
  ensure_env

  PORT=$(get_env_value .env PORT)
  ADMIN_TOKEN=$(get_env_value .env ADMIN_TOKEN)

  if [ -z "$ADMIN_TOKEN" ] || [ ${#ADMIN_TOKEN} -lt 12 ]; then
    log_error "ADMIN_TOKEN 为空或长度过短，请检查 .env"
    exit 1
  fi

  log_info "启动容器..."
  set +e
  compose_up_release
  up_rc=$?
  set -e

  if [ "$up_rc" -ne 0 ]; then
    log_warn "compose 启动返回非零 ($up_rc)，继续检查状态..."
  fi

  if ! health_check; then
    log_error "部署未通过健康检查"
    echo ""
    compose ps || true
    echo ""
    compose logs --tail=80 || true
    exit 1
  fi

  echo ""
  log_info "=========================================="
  log_info "API 2 Cursor 部署完成"
  log_info "=========================================="
  echo ""
  echo "  服务地址: http://localhost:${PORT}"
  echo "  管理面板: http://localhost:${PORT}/admin"
  echo "  健康检查: http://localhost:${PORT}/health"
  echo "  ADMIN_TOKEN: ${ADMIN_TOKEN}"
  echo ""
  log_warn "请妥善保存 .env 和 ADMIN_TOKEN"
  echo ""
  log_info "常用命令："
  echo "  日志: $DOCKER_COMPOSE $COMPOSE_FILES --env-file .env logs -f"
  echo "  状态: $DOCKER_COMPOSE $COMPOSE_FILES --env-file .env ps"
  echo "  停止: $DOCKER_COMPOSE $COMPOSE_FILES --env-file .env down"
  echo ""
}

upgrade() {
  echo ""
  log_info "升级 API 2 Cursor（拉取/构建新镜像并重启，不删除 data 目录）..."
  prepare_compose

  if [ ! -f .env ]; then
    log_warn "未找到 .env，转入部署流程"
    deploy
    return 0
  fi

  PORT=$(get_env_value .env PORT)
  [ -z "$PORT" ] && PORT=28473

  set +e
  compose_up_release
  up_rc=$?
  set -e
  if [ "$up_rc" -ne 0 ]; then
    log_warn "compose 启动返回非零 ($up_rc)，继续检查状态..."
  fi

  if ! health_check; then
    log_error "升级后健康检查失败"
    compose ps || true
    echo ""
    compose logs --tail=80 || true
    exit 1
  fi

  log_info "升级完成"
  echo "  状态: $DOCKER_COMPOSE $COMPOSE_FILES --env-file .env ps"
  echo ""
}

reset_data() {
  echo ""
  log_warn "将删除本项目 data/ 数据目录（SQLite 数据不可恢复）"
  read -r -p "确认请输入大写 YES: " confirm
  echo
  if [ "$confirm" != "YES" ]; then
    log_info "已取消"
    return 0
  fi

  prepare_compose
  compose down -v --remove-orphans 2>/dev/null || true
  rm -rf data
  mkdir -p data

  if [ ! -f .env ]; then
    log_warn "未找到 .env，重新进入部署流程"
    deploy
    return 0
  fi

  PORT=$(get_env_value .env PORT)
  [ -z "$PORT" ] && PORT=28473

  set +e
  compose_up_release
  up_rc=$?
  set -e
  if [ "$up_rc" -ne 0 ]; then
    log_warn "compose 启动返回非零 ($up_rc)，继续检查状态..."
  fi

  if ! health_check; then
    log_error "重置数据后启动失败"
    compose ps || true
    echo ""
    compose logs --tail=80 || true
    exit 1
  fi

  log_info "数据已重置并重新启动"
  echo ""
}

uninstall() {
  echo ""
  log_warn "即将卸载 API 2 Cursor 容器"
  prepare_compose

  read -r -p "是否删除 data 数据目录（不可恢复）？(y/N): " -n 1 -r
  echo
  DELETE_DATA=false
  if [[ $REPLY =~ ^[Yy]$ ]]; then
    DELETE_DATA=true
  fi

  compose down -v --remove-orphans 2>/dev/null || true

  if [ "$DELETE_DATA" = true ]; then
    rm -rf data
    log_info "已删除 data 目录"
  fi

  read -r -p "是否删除 .env？(y/N): " -n 1 -r
  echo
  if [[ $REPLY =~ ^[Yy]$ ]]; then
    rm -f .env
    log_info "已删除 .env"
  fi

  log_info "卸载完成"
  echo ""
}

show_menu() {
  echo ""
  log_info "请选择操作："
  echo "  1) 一键部署（首次 / 重新生成 .env 并启动）"
  echo "  2) 升级（拉取或构建镜像并重启，保留 data）"
  echo "  3) 卸载（停止容器，可选删数据）"
  echo "  4) 重置数据并启动（会清空 SQLite 数据）"
  echo "  0) 退出"
  echo ""

  while true; do
    read -r -p "请输入 [0-4]: " choice
    choice=${choice//$'\r'/}
    case "$choice" in
      1) deploy; break ;;
      2) upgrade; break ;;
      3) uninstall; break ;;
      4) reset_data; break ;;
      0) log_info "已退出"; exit 0 ;;
      *) log_warn "无效选择" ;;
    esac
  done
}

case "${1:-}" in
  1 | deploy | install) deploy ;;
  2 | upgrade | update) upgrade ;;
  3 | uninstall | remove) uninstall ;;
  4 | reset-data | reset) reset_data ;;
  -h | --help | help)
    echo "用法: $0 [deploy|upgrade|uninstall|reset-data]"
    echo "  deploy      一键部署（交互配置 .env）"
    echo "  upgrade     拉取/构建镜像并升级容器（保留 data）"
    echo "  uninstall   停止容器，可选删除 data 与 .env"
    echo "  reset-data  清空 data 后重新启动"
    echo ""
    echo "无参数时进入交互菜单。"
    ;;
  "")
    show_menu
    ;;
  *)
    log_warn "未知参数: $1"
    echo "使用: deploy | upgrade | uninstall | reset-data | --help，或不传参进入菜单。"
    exit 1
    ;;
esac
