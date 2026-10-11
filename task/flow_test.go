package task

import (
	"net"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/XIU2/CloudflareSpeedTest/utils"
)

func makeResult(ip string, speed float64, delay time.Duration, received int) utils.CloudflareIPData {
	return utils.CloudflareIPData{
		PingData:      &utils.PingData{IP: &net.IPAddr{IP: net.ParseIP(ip)}, Sended: 4, Received: received, Delay: delay},
		DownloadSpeed: speed,
	}
}

func TestMergeResults(t *testing.T) {
	oldDisable := Disable
	defer func() { Disable = oldDisable }()
	domain := utils.DownloadSpeedSet{
		makeResult("10.0.0.1", 5, 150*time.Millisecond, 4),
		makeResult("10.0.0.2", 0, 90*time.Millisecond, 4),
	}
	normal := utils.DownloadSpeedSet{
		makeResult("10.0.1.1", 8, 100*time.Millisecond, 4),
		makeResult("10.0.1.2", 0, 60*time.Millisecond, 4),
		makeResult("10.0.1.3", 0, 50*time.Millisecond, 3), // 有丢包
	}

	Disable = false // 按下载速度排序，速度相同时按丢包率、平均延迟
	if got := ipList(MergeResults(domain, normal)); got != "10.0.1.1,10.0.0.1,10.0.1.2,10.0.0.2,10.0.1.3" {
		t.Errorf("按下载速度合并排序 = %s", got)
	}
	Disable = true // 禁用下载测速时按丢包率、平均延迟排序
	if got := ipList(MergeResults(domain, normal)); got != "10.0.1.2,10.0.0.2,10.0.1.1,10.0.0.1,10.0.1.3" {
		t.Errorf("按延迟合并排序 = %s", got)
	}
	if got := MergeResults(nil, nil); len(got) != 0 {
		t.Errorf("没有结果时 = %s", ipList(got))
	}
}

// TestIPs：延迟测速（HTTPing）-> 1034 检查 -> 下载测速，优选域名的 IP 全部下载测速，其他 IP 按 -dn 下载测速
func TestTestIPs(t *testing.T) {
	var downloads int32
	s := newHttpingTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodHead: // HTTPing
			w.WriteHeader(http.StatusNoContent)
		case r.URL.Path == "/blocked": // 1034 检查
			w.WriteHeader(http.StatusForbidden)
			w.Write([]byte("error code: 1034"))
		case r.URL.Path == "/download":
			if r.Header.Get("Referer") != defaultReferer { // 和 speed.cloudflare.com 一样，没有 Referer 时返回 403
				w.WriteHeader(http.StatusForbidden)
				return
			}
			atomic.AddInt32(&downloads, 1)
			w.Write([]byte(strings.Repeat("a", 64<<10)))
		default:
			w.Write([]byte("ok"))
		}
	})
	setupHttping(t, s, 0)
	setupURL(t, "http://cfst.test/download", "", "http://cfst.test/ping", "", false, true)
	oldTimeout, oldTestCount, oldPingTimes := Timeout, TestCount, PingTimes
	defer func() { Timeout, TestCount, PingTimes = oldTimeout, oldTestCount, oldPingTimes }()
	Timeout, TestCount, PingTimes = time.Second, 1, 1

	ips := func(n int) []*net.IPAddr {
		var list []*net.IPAddr
		for i := 0; i < n; i++ {
			list = append(list, &net.IPAddr{IP: net.ParseIP("127.0.0.1")})
		}
		return list
	}

	for _, tt := range []struct {
		name          string
		domain        bool
		test1043URL   string
		wantDownloads int32
		wantResults   int
	}{
		{"优选域名 IP 全部下载测速", true, "", 3, 3},
		{"其他 IP 按 -dn 下载测速（结果包括没有下载测速的 IP）", false, "", 1, 3},
		{"1034 检查通过", true, "http://cfst.test/ok", 3, 3},
		{"1034 检查不通过的 IP 不下载测速，也不在结果中", true, "http://cfst.test/blocked", 0, 0},
	} {
		t.Run(tt.name, func(t *testing.T) {
			atomic.StoreInt32(&downloads, 0)
			Test1043URL = tt.test1043URL
			result := TestIPs(ips(3), tt.domain)
			if got := atomic.LoadInt32(&downloads); got != tt.wantDownloads {
				t.Errorf("下载测速了 %d 个 IP, want %d", got, tt.wantDownloads)
			}
			if len(result) != tt.wantResults {
				t.Errorf("结果 %d 个, want %d", len(result), tt.wantResults)
			}
		})
	}
	if TestCount != 1 {
		t.Errorf("TestCount 被修改为 %d", TestCount)
	}
}
