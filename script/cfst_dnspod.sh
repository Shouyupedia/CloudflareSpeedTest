#!/bin/bash

# --------------------------------------------------------------
#	项目: CloudflareSpeedTest 自动更新Dnspod优选解析
#	版本: 1.0.1
#	作者: imashen
# --------------------------------------------------------------

# 切换到脚本所在目录，避免在定时任务中运行时相对路径失效
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR" || exit 1

# 清理历史残留
rm -f result4.csv result6.csv
# DNSPod API 凭据
dnspod_token="${API_TOKEN}"
dnspod_domain="${DOMAIN}"
dnspod_record="${SUB_DOMAIN}"

# DNSPod API URL
dnspod_api_url="https://dnsapi.cn"

if [ -z "$dnspod_token" ] || [ -z "$dnspod_domain" ] || [ -z "$dnspod_record" ]; then
  echo "Please set the API_TOKEN, DOMAIN and SUB_DOMAIN environment variables."
  exit 1
fi
if [[ ! "$dnspod_domain" =~ ^[0-9A-Za-z.-]+$ ]] || [[ ! "$dnspod_record" =~ ^[0-9A-Za-z._*@-]+$ ]]; then
  echo "Invalid DOMAIN or SUB_DOMAIN."
  exit 1
fi

# 调用 DNSPod API（login_token 通过标准输入传给 curl，不会出现在进程列表中被其他用户看到）
dnspod_api() {
  local action=$1
  shift
  local -a params=()
  local param
  for param in "$@"; do
    params+=(--data-urlencode "$param")
  done
  printf '%s' "$dnspod_token" | curl -s -X POST --data-urlencode "login_token@-" --data-urlencode "format=json" "${params[@]}" "$dnspod_api_url/$action"
}

# 获取记录 ID（只匹配当前子域名的记录，避免修改到同域名下其他子域名的记录）
get_record_id() {
  local record_type=$1
  local response
  response=$(dnspod_api Record.List "domain=$dnspod_domain" "sub_domain=$dnspod_record" "record_type=$record_type")
  local record_id
  record_id=$(echo "$response" | jq -r --arg type "$record_type" --arg name "$dnspod_record" '[.records[]? | select(.type == $type and .name == $name) | .id] | first // empty')
  echo "$record_id"
}

# 创建 DNS 记录
create_dns_record() {
  local record_type=$1
  local ip_address=$2
  local response
  response=$(dnspod_api Record.Create "domain=$dnspod_domain" "sub_domain=$dnspod_record" "record_type=$record_type" "record_line=默认" "value=$ip_address")
  local record_id
  record_id=$(echo "$response" | jq -r '.record.id // empty')
  echo "$record_id"
}

# 更新 DNS 记录
update_dns_record() {
  local record_id=$1
  local record_type=$2
  local ip_address=$3
  dnspod_api Record.Modify "domain=$dnspod_domain" "record_id=$record_id" "sub_domain=$dnspod_record" "record_type=$record_type" "record_line=默认" "value=$ip_address"
}

# 运行 CFST v4
./cfst -f ip.txt -n 500 -o result4.csv

# 读取 CSV 文件并提取优选 IPv4 地址
preferred_ipv4=$(awk -F, 'NR==2 {print $1}' result4.csv 2>/dev/null)

# 检查是否获取到了 IPv4 地址
if [[ ! "$preferred_ipv4" =~ ^([0-9]{1,3}\.){3}[0-9]{1,3}$ ]]; then
  echo "Failed to get the preferred IPv4 address."
else
  echo "BETTER IPv4: $preferred_ipv4"

  # 获取 IPv4 记录 ID
  ipv4_record_id=$(get_record_id "A")

  if [ -n "$ipv4_record_id" ]; then
    # 更新 IPv4 记录
    update_dns_record "$ipv4_record_id" "A" "$preferred_ipv4"
    echo "Updated DNSPod record with IPv4: $preferred_ipv4"
  else
    # 创建 IPv4 记录
    new_ipv4_record_id=$(create_dns_record "A" "$preferred_ipv4")
    if [ -n "$new_ipv4_record_id" ]; then
      echo "Created DNSPod record with IPv4: $preferred_ipv4"
    else
      echo "Failed to create DNSPod record with IPv4."
    fi
  fi
fi

# 运行 CFST v6
./cfst -f ipv6.txt -n 500 -o result6.csv

# 读取 CSV 文件并提取优选 IPv6 地址
preferred_ipv6=$(awk -F, 'NR==2 {print $1}' result6.csv 2>/dev/null)

# 检查是否获取到了 IPv6 地址
if [[ ! "$preferred_ipv6" =~ ^[0-9A-Fa-f:]+$ ]]; then
  echo "Failed to get the preferred IPv6 address."
else
  echo "BETTER IPv6: $preferred_ipv6"

  # 获取 IPv6 记录 ID
  ipv6_record_id=$(get_record_id "AAAA")

  if [ -n "$ipv6_record_id" ]; then
    # 更新 IPv6 记录
    update_dns_record "$ipv6_record_id" "AAAA" "$preferred_ipv6"
    echo "Updated DNSPod record with IPv6: $preferred_ipv6"
  else
    # 创建 IPv6 记录
    new_ipv6_record_id=$(create_dns_record "AAAA" "$preferred_ipv6")
    if [ -n "$new_ipv6_record_id" ]; then
      echo "Created DNSPod record with IPv6: $preferred_ipv6"
    else
      echo "Failed to create DNSPod record with IPv6."
    fi
  fi
fi
