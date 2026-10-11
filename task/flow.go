package task

import (
	"net"
	"sort"

	"github.com/XIU2/CloudflareSpeedTest/utils"
)

const domainGroupName = "优选域名 IP"

// TestIPs 对一组 IP 完整测速：延迟测速（连通性）-> 按延迟/丢包条件过滤 -> 1034 检查 + 下载测速
// domain 为 true 时是优选域名解析出的 IP，全部进行下载测速；否则按 下载测速数量 (-dn) 进行下载测速
func TestIPs(ips []*net.IPAddr, domain bool) utils.DownloadSpeedSet {
	checkDownloadDefault()
	name := ""
	if domain {
		name = domainGroupName
	}
	pingData := newPing(ips, name).Run().FilterDelay().FilterLossRate()
	if Disable { // 禁用下载测速时，对所有 IP 进行 1034 检查（未指定 -test_1043_url 时不检查）
		return utils.DownloadSpeedSet(CheckHttp(pingData))
	}
	testCount := TestCount
	if domain {
		testCount = len(pingData)
	}
	return testDownloadSpeed(pingData, testCount, name)
}

// MergeResults 合并多组测速结果并重新排序：按下载速度从高到低（速度相同时按丢包率、平均延迟）；
// 禁用下载测速时按丢包率、平均延迟从低到高
func MergeResults(results ...utils.DownloadSpeedSet) utils.DownloadSpeedSet {
	var merged utils.DownloadSpeedSet
	for _, result := range results {
		merged = append(merged, result...)
	}
	if Disable {
		sort.Stable(utils.PingDelaySet(merged))
	} else {
		sort.Stable(merged)
	}
	return merged
}
