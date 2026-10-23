#!/usr/bin/env bash
# Install or upgrade linkprobe on a Debian/Armbian box.
#   sudo deploy/install.sh                     # uses ../build/linkprobe-linux-arm64
#   sudo deploy/install.sh /tmp/linkprobe-new  # or an explicit binary
# Existing configuration and data are preserved; the service is restarted.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BIN="${1:-$SCRIPT_DIR/../build/linkprobe-linux-arm64}"
PREFIX="${PREFIX:-/opt/linkprobe}"
CONF_DIR="${CONF_DIR:-/etc/linkprobe}"
DATA_DIR="${DATA_DIR:-/var/lib/linkprobe}"
SERVICE_USER="${SERVICE_USER:-linkprobe}"

if [ "$(id -u)" -ne 0 ]; then
  echo "请用 root 运行（sudo）" >&2
  exit 1
fi
if [ ! -f "$BIN" ]; then
  echo "找不到二进制：$BIN" >&2
  exit 1
fi

if ! id "$SERVICE_USER" >/dev/null 2>&1; then
  useradd --system --home "$DATA_DIR" --shell /usr/sbin/nologin "$SERVICE_USER"
fi

install -d -m 0755 "$PREFIX"
install -m 0755 "$BIN" "$PREFIX/linkprobe"
install -d -m 0750 -o "$SERVICE_USER" -g "$SERVICE_USER" "$DATA_DIR"
install -d -m 0755 "$CONF_DIR"

if [ ! -f "$CONF_DIR/config.yaml" ]; then
  if [ -z "${LINKPROBE_PASSWORD:-}" ]; then
    read -r -s -p "设置管理密码: " LINKPROBE_PASSWORD
    echo
  fi
  HASH="$("$PREFIX/linkprobe" -hash-password="$LINKPROBE_PASSWORD")"
  cat > "$CONF_DIR/config.yaml" <<EOF
listen: "0.0.0.0:8090"
data_dir: "$DATA_DIR"
admin_password_hash: "$HASH"
session_ttl_hours: 72
trust_proxy: false
retention:
  raw_days: 7
  rollup_5m_days: 90
  rollup_1h_days: 730
probe:
  max_concurrent: 32
  write_batch_seconds: 30
  dns_cache_minutes: 5
  packet_gap_ms: 400
EOF
  chmod 0640 "$CONF_DIR/config.yaml"
  chown root:"$SERVICE_USER" "$CONF_DIR/config.yaml"
  echo "已写入 $CONF_DIR/config.yaml"
else
  echo "保留已有配置 $CONF_DIR/config.yaml"
fi

install -m 0644 "$SCRIPT_DIR/linkprobe.service" /etc/systemd/system/linkprobe.service
systemctl daemon-reload
systemctl enable linkprobe.service
systemctl restart linkprobe.service
sleep 1
systemctl --no-pager --lines=10 status linkprobe.service || true

IP="$(hostname -I | awk '{print $1}')"
echo
echo "完成。浏览器打开 http://${IP}:8090"
echo "日志：journalctl -u linkprobe -f"
