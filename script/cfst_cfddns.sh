#!/usr/bin/env bash
PATH=/bin:/sbin:/usr/bin:/usr/sbin:/usr/local/bin:/usr/local/sbin:~/bin
export PATH
# --------------------------------------------------------------
#	项目: CloudflareSpeedTest 测速并同步最优 IP 到 Cloudflare DNS（DDNS）
#	版本: 1.0.0
#	项目: https://github.com/XIU2/CloudflareSpeedTest
# --------------------------------------------------------------
# 依赖：bash 4+、curl、jq，以及支持 -durl / -test_1043_url 参数的 CFST
# 使用前请先复制 cfst_cfddns.conf.example 为 cfst_cfddns.conf 并修改其中的配置

set -uo pipefail
umask 077

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)" || exit 1
CONFIG_FILE="${SCRIPT_DIR}/cfst_cfddns.conf"
CF_API="https://api.cloudflare.com/client/v4"
DRY_RUN=0
FORCE=0
TMP_DIR=""
BEST_IP=""
RECORD_TYPES=()
EXTRA_ARGS=()

# 配置项及默认值（不读取同名环境变量，只以配置文件为准）
CONFIG_KEYS=" CF_API_TOKEN CF_API_KEY CF_EMAIL CF_ZONE_ID CF_ZONE_NAME CF_RECORD_NAME CF_RECORD_TYPE CF_TTL CF_PROXIED CFST_BIN CFST_IP_FILE_V4 CFST_IP_FILE_V6 CFST_DOWNLOAD_URL CFST_HTTPING_URL CFST_HTTPING_CODE CFST_TEST_1043_URL CFST_EXTRA_ARGS "
CF_API_TOKEN=""
CF_API_KEY=""
CF_EMAIL=""
CF_ZONE_ID=""
CF_ZONE_NAME=""
CF_RECORD_NAME=""
CF_RECORD_TYPE="A"
CF_TTL="1"
CF_PROXIED="false"
CFST_BIN="./cfst"
CFST_IP_FILE_V4="ip.txt"
CFST_IP_FILE_V6="ipv6.txt"
CFST_DOWNLOAD_URL=""
CFST_HTTPING_URL=""
CFST_HTTPING_CODE=""
CFST_TEST_1043_URL=""
CFST_EXTRA_ARGS=""

usage() {
	cat <<EOF
用法: $(basename "$0") [-c 配置文件] [-n] [-f] [-h]
    -c 配置文件    指定配置文件路径（默认为脚本所在目录下的 cfst_cfddns.conf）
    -n            试运行：只测速并显示将要更新的 IP，不会修改 DNS 记录
    -f            强制更新：即使 DNS 记录的 IP 和最优 IP 一样也会更新（如修改了 TTL、代理状态后）
    -h            显示帮助
EOF
}

log() { printf '[%s] %s\n' "$(date '+%Y-%m-%d %H:%M:%S')" "$*"; }
info() { log "[信息] $*"; }
warn() { log "[警告] $*" >&2; }
error() { log "[错误] $*" >&2; }
die() {
	error "$*"
	exit 1
}

# 去除首尾空白字符
trim() {
	local s="$1"
	s="${s#"${s%%[![:space:]]*}"}"
	s="${s%"${s##*[![:space:]]}"}"
	printf '%s' "$s"
}

is_ipv4() {
	local re='^([0-9]{1,3})\.([0-9]{1,3})\.([0-9]{1,3})\.([0-9]{1,3})$' i
	[[ "$1" =~ $re ]] || return 1
	for i in 1 2 3 4; do
		((10#${BASH_REMATCH[i]} <= 255)) || return 1
	done
}

is_ipv6() {
	local re='^[0-9A-Fa-f]{0,4}(:[0-9A-Fa-f]{0,4}){2,7}$'
	[[ "$1" =~ $re ]]
}

is_domain() {
	local re='^(\*\.)?([A-Za-z0-9_]([A-Za-z0-9_-]{0,61}[A-Za-z0-9_])?\.)+[A-Za-z0-9-]{2,63}$'
	[[ "$1" =~ $re ]]
}

is_http_url() {
	local re='^https?://[^[:space:][:cntrl:]]+$'
	[[ "$1" =~ $re ]]
}

parse_args() {
	local opt
	while getopts ":c:nfh" opt; do
		case "$opt" in
			c) CONFIG_FILE="$OPTARG" ;;
			n) DRY_RUN=1 ;;
			f) FORCE=1 ;;
			h)
				usage
				exit 0
				;;
			:) die "选项 -$OPTARG 需要指定参数" ;;
			*)
				usage >&2
				exit 1
				;;
		esac
	done
	[[ "$CONFIG_FILE" == /* ]] || CONFIG_FILE="${PWD}/${CONFIG_FILE}" # 后面会切换到脚本所在目录，因此转为绝对路径
}

# 读取配置文件（只解析 配置项=值，不会执行其中的任何代码）
read_config() {
	local line key value perm n=0
	[[ -f "$CONFIG_FILE" ]] || die "配置文件不存在 [${CONFIG_FILE}]，请先复制 cfst_cfddns.conf.example 为 cfst_cfddns.conf 并修改配置"
	[[ -r "$CONFIG_FILE" ]] || die "没有读取配置文件的权限 [${CONFIG_FILE}]"
	# 配置文件中有 API 令牌/密钥，不应该允许其他用户读取
	if perm="$(stat -c '%a' "$CONFIG_FILE" 2>/dev/null)" && [[ "$perm" =~ ^[0-7]+$ ]] && ((8#$perm & 8#077)); then
		warn "配置文件的权限为 ${perm}，其他用户也能读取其中的 API 令牌/密钥，建议执行：chmod 600 \"${CONFIG_FILE}\""
	fi
	while IFS= read -r line || [[ -n "$line" ]]; do
		n=$((n + 1))
		line="${line%$'\r'}" # 兼容 Windows 换行符
		[[ "$line" =~ ^[[:space:]]*(#|$) ]] && continue
		if [[ ! "$line" =~ ^[[:space:]]*([A-Z0-9_]+)[[:space:]]*=(.*)$ ]]; then
			warn "配置文件第 ${n} 行格式错误（应为 配置项=值），已忽略"
			continue
		fi
		key="${BASH_REMATCH[1]}"
		value="$(trim "${BASH_REMATCH[2]}")"
		if [[ "$value" =~ ^\"(.*)\"$ || "$value" =~ ^\'(.*)\'$ ]]; then # 去掉成对的引号
			value="${BASH_REMATCH[1]}"
		fi
		if [[ "$CONFIG_KEYS" != *" ${key} "* ]]; then
			warn "配置文件第 ${n} 行的配置项 [${key}] 未知，已忽略"
			continue
		fi
		printf -v "$key" '%s' "$value"
	done <"$CONFIG_FILE"
}

# 检查配置，认证信息会写入 curl 配置，因此必须严格检查字符，避免注入其他 curl 参数
check_config() {
	local secret_re='^[^[:space:][:cntrl:]"\\]+$' email_re='^[^[:space:][:cntrl:]"\\@]+@[^[:space:][:cntrl:]"\\@]+$' arg
	if [[ -n "$CF_API_TOKEN" ]]; then
		[[ "$CF_API_TOKEN" =~ $secret_re ]] || die "CF_API_TOKEN（API 令牌）格式错误"
	elif [[ -n "$CF_API_KEY" ]]; then
		[[ "$CF_API_KEY" =~ $secret_re ]] || die "CF_API_KEY（全局 API 密钥）格式错误"
		[[ "$CF_EMAIL" =~ $email_re ]] || die "使用 CF_API_KEY（全局 API 密钥）时，需要正确填写 CF_EMAIL（账号邮箱）"
	else
		die "请填写 CF_API_TOKEN（API 令牌），或 CF_API_KEY（全局 API 密钥）+ CF_EMAIL（账号邮箱）"
	fi

	CF_ZONE_NAME="${CF_ZONE_NAME,,}"
	CF_ZONE_NAME="${CF_ZONE_NAME%.}"
	CF_RECORD_NAME="${CF_RECORD_NAME,,}"
	CF_RECORD_NAME="${CF_RECORD_NAME%.}"
	if [[ -n "$CF_ZONE_ID" ]]; then
		[[ "$CF_ZONE_ID" =~ ^[0-9a-fA-F]{32}$ ]] || die "CF_ZONE_ID（区域 ID）格式错误，应为 32 位字母数字"
	elif ! is_domain "$CF_ZONE_NAME"; then
		die "请填写 CF_ZONE_ID（区域 ID）或 CF_ZONE_NAME（根域名，如 example.com）"
	fi
	is_domain "$CF_RECORD_NAME" || die "CF_RECORD_NAME 格式错误，应为完整域名（如 cdn.example.com）"
	if [[ -n "$CF_ZONE_NAME" && "$CF_RECORD_NAME" != "$CF_ZONE_NAME" && "$CF_RECORD_NAME" != *".${CF_ZONE_NAME}" ]]; then
		die "CF_RECORD_NAME [${CF_RECORD_NAME}] 不属于域名 CF_ZONE_NAME [${CF_ZONE_NAME}]"
	fi

	CF_RECORD_TYPE="${CF_RECORD_TYPE^^}"
	CF_RECORD_TYPE="${CF_RECORD_TYPE// /}"
	case "$CF_RECORD_TYPE" in
		A | AAAA) RECORD_TYPES=("$CF_RECORD_TYPE") ;;
		A,AAAA | AAAA,A) RECORD_TYPES=(A AAAA) ;;
		*) die "CF_RECORD_TYPE 只能是 A、AAAA 或 A,AAAA" ;;
	esac
	if [[ ! "$CF_TTL" =~ ^[0-9]+$ ]] || ((10#$CF_TTL != 1 && (10#$CF_TTL < 30 || 10#$CF_TTL > 86400))); then
		die "CF_TTL 只能是 1（自动）或 30~86400 之间的秒数"
	fi
	CF_TTL="$((10#$CF_TTL))"
	CF_PROXIED="${CF_PROXIED,,}"
	[[ "$CF_PROXIED" == "true" || "$CF_PROXIED" == "false" ]] || die "CF_PROXIED 只能是 true 或 false"
	[[ "$CF_PROXIED" == "true" ]] && warn "CF_PROXIED=true 时 DNS 记录会被 Cloudflare 代理，此时访问者看到的不是优选 IP（优选 IP 一般应为 false）"

	if [[ "$CFST_BIN" != */* ]]; then # 只写了程序名，则从 PATH 中查找
		CFST_BIN="$(command -v "$CFST_BIN")" || die "没有找到 CFST 程序，请检查 CFST_BIN 配置"
	fi
	[[ -f "$CFST_BIN" && -x "$CFST_BIN" ]] || die "CFST 程序 [${CFST_BIN}] 不存在或没有执行权限（可执行 chmod +x 赋予权限）"
	for arg in "${RECORD_TYPES[@]}"; do
		if [[ "$arg" == "A" ]]; then
			[[ -f "$CFST_IP_FILE_V4" ]] || die "IPv4 数据文件 [${CFST_IP_FILE_V4}] 不存在，请检查 CFST_IP_FILE_V4 配置"
		else
			[[ -f "$CFST_IP_FILE_V6" ]] || die "IPv6 数据文件 [${CFST_IP_FILE_V6}] 不存在，请检查 CFST_IP_FILE_V6 配置"
		fi
	done
	if [[ -n "$CFST_DOWNLOAD_URL" ]] && ! is_http_url "$CFST_DOWNLOAD_URL"; then
		die "CFST_DOWNLOAD_URL 必须是 http:// 或 https:// 开头的地址"
	fi
	if [[ -n "$CFST_HTTPING_URL" ]] && ! is_http_url "$CFST_HTTPING_URL"; then
		die "CFST_HTTPING_URL 必须是 http:// 或 https:// 开头的地址"
	fi
	if [[ -n "$CFST_TEST_1043_URL" ]] && ! is_http_url "$CFST_TEST_1043_URL"; then
		die "CFST_TEST_1043_URL 必须是 http:// 或 https:// 开头的地址"
	fi
	if [[ -n "$CFST_HTTPING_CODE" && ! "$CFST_HTTPING_CODE" =~ ^[1-5][0-9][0-9]$ ]]; then
		die "CFST_HTTPING_CODE 必须是 HTTP 状态码（如 200）"
	fi

	# 按空白字符分割为参数（不会进行通配符展开，也不会执行其中的命令）
	[[ -n "$CFST_EXTRA_ARGS" ]] && read -r -a EXTRA_ARGS <<<"$CFST_EXTRA_ARGS"
	for arg in ${EXTRA_ARGS[@]+"${EXTRA_ARGS[@]}"}; do
		case "$arg" in
			-f | --f | -f=* | --f=* | -o | --o | -o=* | --o=* | -p | --p | -p=* | --p=*)
				die "CFST_EXTRA_ARGS 中不能包含 ${arg%%=*} 参数（该参数由脚本控制，IP 数据文件请通过 CFST_IP_FILE_V4/V6 配置）"
				;;
		esac
	done
}

check_deps() {
	local dep
	((BASH_VERSINFO[0] >= 4)) || die "需要 bash 4.0 及以上版本"
	for dep in curl jq; do
		command -v "$dep" >/dev/null 2>&1 || die "缺少依赖 [${dep}]，请先安装（如 apt install ${dep} / yum install ${dep} / opkg install ${dep}）"
	done
}

# 避免定时任务重叠运行（锁定配置文件本身，不需要额外创建锁文件）
acquire_lock() {
	command -v flock >/dev/null 2>&1 || return 0
	exec 9<"$CONFIG_FILE" || die "无法打开配置文件 [${CONFIG_FILE}]"
	flock -n 9 || die "使用该配置文件的脚本已经在运行中"
}

# shellcheck disable=SC2329 # 由 trap 调用
cleanup() {
	if [[ -n "$TMP_DIR" ]]; then
		rm -rf "$TMP_DIR"
	fi
}

# 认证信息以 curl 配置的格式通过标准输入传给 curl，不会出现在进程列表（ps）中被其他用户看到
auth_curl_config() {
	if [[ -n "$CF_API_TOKEN" ]]; then
		printf 'header = "Authorization: Bearer %s"\n' "$CF_API_TOKEN"
	else
		printf 'header = "X-Auth-Email: %s"\nheader = "X-Auth-Key: %s"\n' "$CF_EMAIL" "$CF_API_KEY"
	fi
}

# 调用 Cloudflare API，成功时输出响应内容
cf_api() {
	local method="$1" path="$2" data="${3-}" response message
	local -a args=(--silent --show-error --max-time 30 -X "$method" -H "Content-Type: application/json" --config -)
	[[ "$method" == "GET" ]] && args+=(--retry 2 --retry-delay 3) # 只有查询请求才重试，避免重复修改
	[[ -n "$data" ]] && args+=(--data "$data")
	if ! response="$(auth_curl_config | curl "${args[@]}" "${CF_API}${path}")"; then
		error "请求 Cloudflare API 失败：${method} ${path}"
		return 1
	fi
	if [[ "$(jq -r '.success' <<<"$response" 2>/dev/null)" != "true" ]]; then
		message="$(jq -r '[.errors[]? | "\(.code): \(.message)"] | join("; ")' <<<"$response" 2>/dev/null)"
		[[ -z "$message" ]] && message="$(printf '%.300s' "$response")"
		error "Cloudflare API 返回错误（${method} ${path}）：${message}"
		return 1
	fi
	printf '%s' "$response"
}

# 获取区域 ID，并确认 API 令牌有权限读取该域名的 DNS 记录（在测速前检查，避免测速完了才发现配置有误）
check_api() {
	local response
	if [[ -z "$CF_ZONE_ID" ]]; then
		response="$(cf_api GET "/zones?name=${CF_ZONE_NAME}")" || return 1
		CF_ZONE_ID="$(jq -r '.result[0].id // empty' <<<"$response")"
		if [[ ! "$CF_ZONE_ID" =~ ^[0-9a-fA-F]{32}$ ]]; then
			error "没有找到域名 [${CF_ZONE_NAME}]，请检查 CF_ZONE_NAME 是否正确、API 令牌是否有 区域.区域:读取 权限（或直接填写 CF_ZONE_ID）"
			return 1
		fi
		info "域名 [${CF_ZONE_NAME}] 的区域 ID：${CF_ZONE_ID}"
	fi
	cf_api GET "/zones/${CF_ZONE_ID}/dns_records?name=${CF_RECORD_NAME}" >/dev/null || return 1
}

# 下载测速是否被禁用（-dd）
download_disabled() {
	local arg
	for arg in ${EXTRA_ARGS[@]+"${EXTRA_ARGS[@]}"}; do
		case "$arg" in
			-dd | --dd | -dd=true | --dd=true) return 0 ;;
		esac
	done
	return 1
}

# 运行 CFST 测速，最优 IP 保存到 BEST_IP
run_cfst() {
	local type="$1" ip_file result_file ip loss delay speed colo
	local -a args
	BEST_IP=""
	if [[ "$type" == "A" ]]; then
		ip_file="$CFST_IP_FILE_V4"
	else
		ip_file="$CFST_IP_FILE_V6"
	fi
	result_file="${TMP_DIR}/result_${type}.csv"

	# 脚本控制的参数放在最前面，这样额外参数中有误时不会影响这些参数
	# 测速流程：先测速优选域名（脚本目录下的 bestcf-domain.txt）解析出的 IP，再测速 IP 段数据，最后合并排序；
	# 每组 IP：延迟测速（默认 HTTPing）-> 1034 检查（填写了 CFST_TEST_1043_URL 时）-> 下载测速
	args=(-f "$ip_file" -o "$result_file" -p 0)
	[[ -n "$CFST_DOWNLOAD_URL" ]] && args+=(-durl "$CFST_DOWNLOAD_URL")
	[[ -n "$CFST_HTTPING_URL" ]] && args+=(-httping-url "$CFST_HTTPING_URL")
	[[ -n "$CFST_TEST_1043_URL" ]] && args+=(-test_1043_url "$CFST_TEST_1043_URL")
	[[ -n "$CFST_HTTPING_CODE" ]] && args+=(-httping-code "$CFST_HTTPING_CODE")
	args+=(${EXTRA_ARGS[@]+"${EXTRA_ARGS[@]}"})

	info "开始测速（${type} 记录）：${CFST_BIN} ${args[*]}"
	if ! "$CFST_BIN" "${args[@]}"; then
		error "CFST 运行失败，跳过更新 ${type} 记录"
		return 1
	fi
	echo
	# 没有满足条件的 IP 时，CFST 不会生成结果文件
	if [[ ! -s "$result_file" ]]; then
		error "CFST 测速结果 IP 数量为 0（没有满足条件的 IP），跳过更新 ${type} 记录"
		return 1
	fi
	# 结果文件格式：IP 地址,已发送,已接收,丢包率,平均延迟,下载速度(MB/s),地区码
	IFS=, read -r ip _ _ loss delay speed colo < <(sed -n '2p' "$result_file")
	if [[ "$type" == "A" ]] && ! is_ipv4 "${ip:-}"; then
		error "CFST 测速结果中的最优 IP [${ip:-}] 不是有效的 IPv4 地址，跳过更新 A 记录"
		return 1
	fi
	if [[ "$type" == "AAAA" ]] && ! is_ipv6 "${ip:-}"; then
		error "CFST 测速结果中的最优 IP [${ip:-}] 不是有效的 IPv6 地址，跳过更新 AAAA 记录"
		return 1
	fi
	# 下载测速全部失败时，所有 IP 的下载速度都是 0.00，此时的排序没有意义
	if [[ "${speed:-}" == "0.00" ]] && ! download_disabled; then
		error "最优 IP [${ip}] 的下载速度为 0.00 MB/s，可能是下载测速地址不可用（可在 CFST_EXTRA_ARGS 中加上 -debug 排查），跳过更新 ${type} 记录"
		return 1
	fi
	info "最优 IP：${ip}（平均延迟 ${delay:-} ms，丢包率 ${loss:-}，下载速度 ${speed:-} MB/s，地区码 ${colo:-N/A}）"
	BEST_IP="$ip"
}

# 更新 DNS 记录（不存在时自动创建）
update_record() {
	local type="$1" ip="$2" response count record_id old_ip data
	response="$(cf_api GET "/zones/${CF_ZONE_ID}/dns_records?type=${type}&name=${CF_RECORD_NAME}")" || return 1
	count="$(jq -r '.result | length' <<<"$response")"
	record_id="$(jq -r '.result[0].id // empty' <<<"$response")"
	old_ip="$(jq -r '.result[0].content // empty' <<<"$response")"
	if [[ -n "$record_id" && ! "$record_id" =~ ^[0-9a-fA-F]{32}$ ]]; then
		error "Cloudflare API 返回的 DNS 记录 ID 格式错误：${record_id}"
		return 1
	fi
	if ((count > 1)); then
		warn "域名 [${CF_RECORD_NAME}] 有 ${count} 条 ${type} 记录，只会更新其中第一条（ID：${record_id}，IP：${old_ip}）"
	fi
	if [[ -n "$record_id" && "$old_ip" == "$ip" ]] && ((!FORCE)); then
		info "${type} 记录 [${CF_RECORD_NAME}] 已经是 ${ip}，无需更新"
		return 0
	fi
	if ((DRY_RUN)); then
		info "[试运行] ${type} 记录 [${CF_RECORD_NAME}] 将会从 ${old_ip:-（记录不存在，将会创建）} 更新为 ${ip}"
		return 0
	fi
	data="$(jq -cn --arg type "$type" --arg name "$CF_RECORD_NAME" --arg content "$ip" --argjson ttl "$CF_TTL" --argjson proxied "$CF_PROXIED" \
		'{type: $type, name: $name, content: $content, ttl: $ttl, proxied: $proxied}')" || return 1
	if [[ -n "$record_id" ]]; then
		cf_api PATCH "/zones/${CF_ZONE_ID}/dns_records/${record_id}" "$data" >/dev/null || return 1
		info "已将 ${type} 记录 [${CF_RECORD_NAME}] 从 ${old_ip} 更新为 ${ip}"
	else
		cf_api POST "/zones/${CF_ZONE_ID}/dns_records" "$data" >/dev/null || return 1
		info "已创建 ${type} 记录 [${CF_RECORD_NAME}]：${ip}"
	fi
}

main() {
	local type failed=0
	parse_args "$@"
	check_deps
	read_config
	cd "$SCRIPT_DIR" || die "无法进入脚本所在目录 [${SCRIPT_DIR}]"
	check_config
	acquire_lock
	trap cleanup EXIT
	trap 'exit 1' HUP INT TERM
	# 测速结果写入只有当前用户能访问的临时目录，避免被其他用户篡改结果或通过符号链接覆盖其他文件
	TMP_DIR="$(mktemp -d "${TMPDIR:-/tmp}/cfst_cfddns.XXXXXX")" || die "创建临时目录失败"

	check_api || die "Cloudflare API 检查失败，请检查配置"
	for type in "${RECORD_TYPES[@]}"; do
		if run_cfst "$type"; then
			update_record "$type" "$BEST_IP" || failed=1
		else
			failed=1
		fi
	done
	exit "$failed"
}

main "$@"
