#!/usr/bin/env bash
PATH=/bin:/sbin:/usr/bin:/usr/sbin:/usr/local/bin:/usr/local/sbin:~/bin
export PATH
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR" || exit 1
# --------------------------------------------------------------
#	项目: CloudflareSpeedTest 自动更新 Hosts
#	版本: 1.0.6
#	作者: XIU2
#	项目: https://github.com/XIU2/CloudflareSpeedTest
# --------------------------------------------------------------

# 判断是否为有效的 IPv4/IPv6 地址（IP 会被写入 sed 命令中修改 Hosts，因此必须检查，避免注入其他 sed 命令）
_IS_IP() {
	local ipv4='^((25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9]?[0-9])\.){3}(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9]?[0-9])$'
	local ipv6='^[0-9A-Fa-f]{0,4}(:[0-9A-Fa-f]{0,4}){2,7}$'
	[[ "$1" =~ $ipv4 || "$1" =~ $ipv6 ]]
}

_CHECK() {
	while true
		do
		if [[ ! -e "nowip_hosts.txt" ]]; then
			echo -e "该脚本的作用为 CFST 测速后获取最快 IP 并替换 Hosts 中的 Cloudflare CDN IP。\n使用前请先阅读：https://github.com/XIU2/CloudflareSpeedTest/issues/42#issuecomment-768273848"
			echo -e "第一次使用，请先将 Hosts 中所有 Cloudflare CDN IP 统一改为一个 IP。"
			read -r -e -p "输入该 Cloudflare CDN IP 并回车（后续不再需要该步骤）：" NOWIP
			if _IS_IP "${NOWIP}"; then
				echo "${NOWIP}" > nowip_hosts.txt
				break
			else
				echo "请输入有效的 IP 地址！"
			fi
		else
			break
		fi
	done
}

_UPDATE() {
	echo -e "开始测速..."
	NOWIP=$(head -1 nowip_hosts.txt | tr -d '[:space:]')
	if ! _IS_IP "${NOWIP}"; then
		echo "nowip_hosts.txt 中的 IP [${NOWIP}] 无效，请删除该文件后重新运行脚本..."
		exit 1
	fi

	# 删除上次的测速结果，避免本次没有满足条件的 IP 时（不会生成结果文件）误用上次的结果
	rm -f "result_hosts.txt"
	# 这里可以自己添加、修改 CFST 的运行参数
	./cfst -o "result_hosts.txt"

	# 如果需要 "找不到满足条件的 IP 就一直循环测速下去"，那么可以将下面的两个 exit 0 改为 _UPDATE 即可
	[[ ! -e "result_hosts.txt" ]] && echo "CFST 测速结果 IP 数量为 0，跳过下面步骤..." && exit 0

	# 下面这行代码是 "找不到满足条件的 IP 就一直循环测速下去" 才需要的代码
	# 考虑到当指定了下载速度下限，但一个满足全部条件的 IP 都没找到时，CFST 就会输出所有 IP 结果
	# 因此当你指定 -sl 参数时，需要移除下面这段代码开头的 # 井号注释符，来做文件行数判断（比如下载测速数量：10 个，那么下面的值就设在为 11）
	#[[ $(cat result_hosts.txt|wc -l) > 11 ]] && echo "CFST 测速结果没有找到一个完全满足条件的 IP，重新测速..." && _UPDATE


	BESTIP=$(sed -n "2,1p" result_hosts.txt | awk -F, '{print $1}')
	if [[ -z "${BESTIP}" ]]; then
		echo "CFST 测速结果 IP 数量为 0，跳过下面步骤..."
		exit 0
	fi
	if ! _IS_IP "${BESTIP}"; then
		echo "CFST 测速结果中的 IP [${BESTIP}] 无效，跳过下面步骤..."
		exit 1
	fi
	echo "${BESTIP}" > nowip_hosts.txt
	echo -e "\n旧 IP 为 ${NOWIP}\n新 IP 为 ${BESTIP}\n"

	echo "开始备份 Hosts 文件（hosts_backup）..."
	\cp -f /etc/hosts /etc/hosts_backup

	echo -e "开始替换..."
	# 只替换行首的 IP（Hosts 格式为：IP 域名），并转义 IP 中的 . 号，避免误替换其他包含该字符串的 IP（如 1.1.1.1 和 11.1.1.10）
	sed -i '' "s/^\([[:space:]]*\)${NOWIP//./\\.}\([[:space:]]\)/\1${BESTIP}\2/" /etc/hosts
	echo -e "完成..."
}

_CHECK
_UPDATE