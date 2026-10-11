package task

import (
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/XIU2/CloudflareSpeedTest/utils"
)

// Cloudflare 1034 错误页面（浏览器请求时返回的 HTML 页面，节选）
const cfError1034HTML = `<!DOCTYPE html>
<html class="no-js" lang="en-US">
<head>
<title>Edge IP Restricted | cfst.test | Cloudflare</title>
</head>
<body>
<div id="cf-error-details" class="cf-error-details-wrapper">
  <h1>
    <span class="cf-error-type" data-translate="error">Error</span>
    <span class="cf-error-code">1034</span>
  </h1>
</div>
</body>
</html>`

func TestIsCFError1034(t *testing.T) {
	tests := []struct {
		name string
		body string
		want bool
	}{
		{"纯文本错误", "error code: 1034", true},
		{"纯文本错误（带换行）", "error code: 1034\n", true},
		{"HTML 错误页面", cfError1034HTML, true},
		{"只有错误码标签", `<span class="cf-error-code">1034</span>`, true},
		{"只有错误名称", "Edge IP Restricted", true},
		{"Error 1034", "<h1>Error 1034</h1>", true},
		{"其他 Cloudflare 错误", "error code: 1020", false},
		{"其他错误码标签", `<span class="cf-error-code">1020</span>`, false},
		{"相似数字", "error code: 10340", false},
		{"普通页面", "<html><body>Hello 1034 world</body></html>", false},
		{"空内容", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isCFError1034([]byte(tt.body)); got != tt.want {
				t.Errorf("isCFError1034(%q) = %v, want %v", tt.body, got, tt.want)
			}
		})
	}
}

func TestHttpCheckIP(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int // -httping-code
		respCode   int
		respBody   string
		want       bool
	}{
		{"200 正常", 0, 200, "ok", true},
		{"204 正常", 0, 204, "", true},
		{"302 正常", 0, 302, "", true},
		{"404 无效状态码", 0, 404, "not found", false},
		{"500 无效状态码", 0, 500, "error", false},
		{"403 纯文本 1034 错误", 0, 403, "error code: 1034", false},
		{"状态码有效但内容为 1034 错误", 0, 200, cfError1034HTML, false},
		{"-httping-code 只用于 HTTPing，不影响 1034 检查（403）", 403, 403, "forbidden", false},
		{"-httping-code 只用于 HTTPing，不影响 1034 检查（200）", 204, 200, "ok", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newHttpingTestServer(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.respCode)
				w.Write([]byte(tt.respBody))
			})
			setupHttping(t, s, tt.statusCode)
			if got, _ := httpCheckIP(localIP); got != tt.want {
				t.Errorf("httpCheckIP() = %v, want %v", got, tt.want)
			}
			if got := s.count(http.MethodGet); got != 1 {
				t.Errorf("GET 请求次数 = %d, want 1", got)
			}
		})
	}
}

func TestHttpCheckIPColo(t *testing.T) {
	s := newHttpingTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Server", "cloudflare")
		w.Header().Set("CF-RAY", "7bd32409eda7b020-HKG")
	})
	setupHttping(t, s, 0)
	if ok, colo := httpCheckIP(localIP); !ok || colo != "HKG" {
		t.Errorf("httpCheckIP() = %v, %q, want true, %q", ok, colo, "HKG")
	}
}

// 测试地址是大文件时只读取开头部分内容，不会下载整个文件
func TestHttpCheckIPLargeBody(t *testing.T) {
	var written int64
	s := newHttpingTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		chunk := []byte(strings.Repeat("a", 32<<10))
		for i := 0; i < 1024; i++ { // 最多 32 MB
			n, err := w.Write(chunk)
			atomic.AddInt64(&written, int64(n))
			if err != nil {
				return
			}
		}
	})
	setupHttping(t, s, 0)
	start := time.Now()
	if ok, _ := httpCheckIP(localIP); !ok {
		t.Error("httpCheckIP() = false, want true")
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Errorf("耗时 %v，不应下载整个文件", elapsed)
	}
	time.Sleep(100 * time.Millisecond)
	if got := atomic.LoadInt64(&written); got >= 32<<20 {
		t.Errorf("服务器发送了 %d 字节，不应下载整个文件", got)
	}
}

func makeIPSet(ips ...string) utils.PingDelaySet {
	set := make(utils.PingDelaySet, 0, len(ips))
	for _, ip := range ips {
		set = append(set, utils.CloudflareIPData{PingData: &utils.PingData{
			IP: &net.IPAddr{IP: net.ParseIP(ip)}, Sended: 4, Received: 4,
		}})
	}
	return set
}

// 并发检查后按原有排序保留通过检查的 IP，并为没有地区码的 IP（TCPing 模式）写入地区码
func TestCheckHttpFilterKeepsOrder(t *testing.T) {
	var ips []string
	for i := 1; i <= 50; i++ {
		ips = append(ips, fmt.Sprintf("104.16.0.%d", i))
	}
	ipSet := makeIPSet(ips...)
	ipSet[1].Colo = "LAX" // HTTPing 模式下已经获取过的地区码不会被覆盖
	var calls int32
	result := checkHttp(ipSet, func(ip *net.IPAddr) (bool, string) {
		atomic.AddInt32(&calls, 1)
		time.Sleep(time.Duration(255-ip.IP.To4()[3]) * 10 * time.Microsecond) // 打乱完成顺序
		return ip.IP.To4()[3]%2 == 0, "HKG"
	})
	if calls != 50 {
		t.Errorf("检查了 %d 个 IP, want 50", calls)
	}
	if len(result) != 25 {
		t.Fatalf("通过检查的 IP 数量 = %d, want 25", len(result))
	}
	for i, data := range result {
		if want := fmt.Sprintf("104.16.0.%d", (i+1)*2); data.IP.String() != want {
			t.Fatalf("第 %d 个 IP = %s, want %s（应保持原有排序）", i, data.IP, want)
		}
	}
	if result[0].Colo != "LAX" {
		t.Errorf("已有的地区码被覆盖为 %q", result[0].Colo)
	}
	if result[1].Colo != "HKG" {
		t.Errorf("地区码 = %q, want HKG", result[1].Colo)
	}
}

func TestCheckHttpDisabled(t *testing.T) {
	old := Test1043URL
	defer func() { Test1043URL = old }()
	Test1043URL = ""
	ipSet := makeIPSet("192.0.2.1", "192.0.2.2") // 未指定 1034 检查地址时不会访问网络
	if result := CheckHttp(ipSet); len(result) != 2 {
		t.Errorf("未指定 1034 检查地址时 IP 数量 = %d, want 2", len(result))
	}
}

// 禁用下载测速时检查所有 IP：通过 1034 检查地址过滤掉返回 1034 错误的 IP
func TestCheckHttp(t *testing.T) {
	for _, tt := range []struct {
		path string
		want int
	}{{"/ok", 1}, {"/blocked", 0}} {
		s := newHttpingTestServer(t, func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/blocked" {
				w.WriteHeader(http.StatusForbidden)
				w.Write([]byte("error code: 1034"))
			}
		})
		setupHttping(t, s, 0)
		Test1043URL = "http://cfst.test" + tt.path
		if result := CheckHttp(makeIPSet("127.0.0.1")); len(result) != tt.want {
			t.Errorf("%s: 通过检查的 IP 数量 = %d, want %d", tt.path, len(result), tt.want)
		}
	}
}
