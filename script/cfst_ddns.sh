#!/usr/bin/env bash
PATH=/bin:/sbin:/usr/bin:/usr/sbin:/usr/local/bin:/usr/local/sbin:~/bin
export PATH
# --------------------------------------------------------------
#	项目: CloudflareSpeedTest 自动更新域名解析记录
#	版本: 1.0.7
#	作者: XIU2
#	项目: https://github.com/XIU2/CloudflareSpeedTest
# --------------------------------------------------------------

# 读取配置项（取第一个匹配的行，值为第一个 = 号之后的全部内容）
_GET() {
	echo "${CONFIG}" | sed -n "s/^[[:space:]]*$1[[:space:]]*=//p" | head -n 1 | tr -d '\r' | sed 's/^[[:space:]]*//;s/[[:space:]]*$//'
}

_READ() {
	[[ ! -e "cfst_ddns.conf" ]] && echo -e "[错误] 配置文件不存在 [cfst_ddns.conf] !" && exit 1
	CONFIG=$(cat "cfst_ddns.conf")
	FOLDER=$(_GET FOLDER)
	[[ -z "${FOLDER}" ]] && echo -e "[错误] 缺少配置项 [FOLDER] !" && exit 1
	ZONE_ID=$(_GET ZONE_ID)
	[[ -z "${ZONE_ID}" ]] && echo -e "[错误] 缺少配置项 [ZONE_ID] !" && exit 1
	DNS_RECORDS_ID=$(_GET DNS_RECORDS_ID)
	[[ -z "${DNS_RECORDS_ID}" ]] && echo -e "[错误] 缺少配置项 [DNS_RECORDS_ID] !" && exit 1
	KEY=$(_GET KEY)
	[[ -z "${KEY}" ]] && echo -e "[错误] 缺少配置项 [KEY] !" && exit 1
	EMAIL=$(_GET EMAIL)
	[[ -z "${EMAIL}" ]] && echo -e "[信息] 缺少配置项 [EMAIL]，由 [API 密钥] 方式转为 [API 令牌] 方式!"
	TYPE=$(_GET TYPE)
	[[ -z "${TYPE}" ]] && echo -e "[错误] 缺少配置项 [TYPE] !" && exit 1
	NAME=$(_GET NAME)
	[[ -z "${NAME}" ]] && echo -e "[错误] 缺少配置项 [NAME] !" && exit 1
	TTL=$(_GET TTL)
	[[ -z "${TTL}" ]] && echo -e "[错误] 缺少配置项 [TTL] !" && exit 1
	PROXIED=$(_GET PROXIED)
	[[ -z "${PROXIED}" ]] && echo -e "[错误] 缺少配置项 [PROXIED] !" && exit 1

	# 配置项会被写入请求地址、请求头和 JSON 数据中，因此需要检查格式，避免注入其他内容
	local SAFE='^[^[:space:][:cntrl:]"\\]+$'
	[[ "${ZONE_ID}" =~ ^[0-9A-Za-z]+$ ]] || { echo -e "[错误] 配置项 [ZONE_ID] 格式错误 !"; exit 1; }
	[[ "${DNS_RECORDS_ID}" =~ ^[0-9A-Za-z]+$ ]] || { echo -e "[错误] 配置项 [DNS_RECORDS_ID] 格式错误 !"; exit 1; }
	[[ "${KEY}" =~ $SAFE ]] || { echo -e "[错误] 配置项 [KEY] 格式错误 !"; exit 1; }
	[[ -z "${EMAIL}" || "${EMAIL}" =~ $SAFE ]] || { echo -e "[错误] 配置项 [EMAIL] 格式错误 !"; exit 1; }
	[[ "${TYPE}" == "A" || "${TYPE}" == "AAAA" ]] || { echo -e "[错误] 配置项 [TYPE] 只能是 A 或 AAAA !"; exit 1; }
	[[ "${NAME}" =~ ^[0-9A-Za-z._*-]+$ ]] || { echo -e "[错误] 配置项 [NAME] 格式错误 !"; exit 1; }
	[[ "${TTL}" =~ ^[0-9]+$ ]] || { echo -e "[错误] 配置项 [TTL] 只能是数字 !"; exit 1; }
	[[ "${PROXIED}" == "true" || "${PROXIED}" == "false" ]] || { echo -e "[错误] 配置项 [PROXIED] 只能是 true 或 false !"; exit 1; }
}

# 认证信息以 curl 配置的格式通过标准输入传给 curl，不会出现在进程列表（ps）中被其他用户看到
_AUTH() {
	if [[ -n "${EMAIL}" ]]; then
		# API 密钥方式（全局权限）
		printf 'header = "X-Auth-Email: %s"\nheader = "X-Auth-Key: %s"\n' "${EMAIL}" "${KEY}"
	else
		# API 令牌方式（自定义权限）
		printf 'header = "Authorization: Bearer %s"\n' "${KEY}"
	fi
}

_UPDATE() {
	# 删除上次的测速结果，避免本次没有满足条件的 IP 时（不会生成结果文件）误用上次的结果
	rm -f "result_ddns.txt"
	# 这里可以自己添加、修改 CFST 的运行参数
	./cfst -o "result_ddns.txt"

	# 判断结果文件是否存在，如果不存在说明结果为 0
	[[ ! -e "result_ddns.txt" ]] && echo "CFST 测速结果 IP 数量为 0，跳过下面步骤..." && exit 0

	CONTENT=$(sed -n "2,1p" result_ddns.txt | awk -F, '{print $1}')
	if [[ -z "${CONTENT}" ]]; then
		echo "CFST 测速结果 IP 数量为 0，跳过下面步骤..."
		exit 0
	fi
	# A 记录只能是 IPv4，AAAA 记录只能是 IPv6
	if [[ "${TYPE}" == "A" && ! "${CONTENT}" =~ ^([0-9]{1,3}\.){3}[0-9]{1,3}$ ]] || [[ "${TYPE}" == "AAAA" && ! "${CONTENT}" =~ ^[0-9A-Fa-f:]+$ ]]; then
		echo "CFST 测速结果中的 IP [${CONTENT}] 不是有效的 ${TYPE} 记录值，跳过下面步骤..."
		exit 1
	fi
	_AUTH | curl -X PUT "https://api.cloudflare.com/client/v4/zones/${ZONE_ID}/dns_records/${DNS_RECORDS_ID}" \
		--config - \
		-H "Content-Type: application/json" \
		--data "{\"type\":\"${TYPE}\",\"name\":\"${NAME}\",\"content\":\"${CONTENT}\",\"ttl\":${TTL},\"proxied\":${PROXIED}}"
}

_READ
cd "${FOLDER}" || { echo -e "[错误] 无法进入目录 [${FOLDER}] !"; exit 1; }
_UPDATE
