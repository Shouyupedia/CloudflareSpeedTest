package task

import (
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/XIU2/CloudflareSpeedTest/utils"
)

// 设置测速地址相关的全局变量，测试结束后恢复
func setupURL(t *testing.T, url, downloadURL, httpingURL, test1043URL string, disable, httping bool) {
	oldURL, oldDownloadURL, oldHttpingURL, oldTest1043URL, oldDisable, oldHttping := URL, DownloadURL, HttpingURL, Test1043URL, Disable, Httping
	t.Cleanup(func() {
		URL, DownloadURL, HttpingURL, Test1043URL, Disable, Httping = oldURL, oldDownloadURL, oldHttpingURL, oldTest1043URL, oldDisable, oldHttping
	})
	URL, DownloadURL, HttpingURL, Test1043URL, Disable, Httping = url, downloadURL, httpingURL, test1043URL, disable, httping
}

func TestDefaults(t *testing.T) {
	if !Httping || HttpingURL != "https://cp.cloudflare.com/" {
		t.Errorf("默认延迟测速模式 Httping = %v, HttpingURL = %q", Httping, HttpingURL)
	}
	if URL != "https://speed.cloudflare.com/__down?bytes=524288000" || Referer != "https://speed.cloudflare.com" {
		t.Errorf("默认下载测速地址 URL = %q, Referer = %q", URL, Referer)
	}
	if Test1043URL != "" {
		t.Errorf("默认不进行 1034 检查，Test1043URL = %q", Test1043URL)
	}
	for code, want := range map[int]bool{200: true, 204: true, 301: true, 302: true, 403: false, 404: false} {
		if got := isValidStatusCode(code); got != want {
			t.Errorf("默认 HTTPing 有效状态码 %d = %v, want %v", code, got, want)
		}
	}
}

func TestCheckURLDefault(t *testing.T) {
	setupURL(t, "https://a.example/file", "", defaultHttpingURL, "", false, true)
	if err := CheckURL(); err != nil {
		t.Fatal(err)
	}
	if DownloadURL != URL || HttpingURL != defaultHttpingURL {
		t.Errorf("DownloadURL, HttpingURL = %q, %q", DownloadURL, HttpingURL)
	}

	// HTTPing 测试地址为空时使用 -url 的地址
	setupURL(t, "", "", "", "", false, true)
	if err := CheckURL(); err != nil {
		t.Fatal(err)
	}
	if URL != defaultURL || DownloadURL != defaultURL || HttpingURL != defaultURL {
		t.Errorf("URL, DownloadURL, HttpingURL = %q, %q, %q, want %q", URL, DownloadURL, HttpingURL, defaultURL)
	}
}

func TestCheckURLInvalid(t *testing.T) {
	tests := []struct {
		name                                      string
		url, downloadURL, httpingURL, test1043URL string
		disable, httping, fail                    bool
	}{
		{"下载测速地址协议无效", "https://a.example/", "ftp://b.example/", defaultHttpingURL, "", false, true, true},
		{"下载测速地址缺少域名", "https://a.example/", "https:///file", defaultHttpingURL, "", false, true, true},
		{"HTTPing 测试地址无效", "https://a.example/", "", "example.com", "", false, true, true},
		{"TCPing 模式下不检查 HTTPing 测试地址", "https://a.example/", "", "example.com", "", false, false, false},
		{"HTTPing 测试地址为空时使用无效的 -url", "a.example", "https://b.example/", "", "", true, true, true},
		{"1034 检查地址无效", "https://a.example/", "", defaultHttpingURL, "example.com", false, true, true},
		{"禁用下载测速时不检查下载测速地址", "https://a.example/", "ftp://b.example/", defaultHttpingURL, "", true, true, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setupURL(t, tt.url, tt.downloadURL, tt.httpingURL, tt.test1043URL, tt.disable, tt.httping)
			if err := CheckURL(); (err != nil) != tt.fail {
				t.Errorf("CheckURL() error = %v, want fail = %v", err, tt.fail)
			}
		})
	}
}

// 下载测速应使用 -durl 指定的地址，并默认带上 Referer（默认下载测速地址超过 10MB 时必须带 Referer，否则返回 403）
func TestDownloadHandlerURLAndReferer(t *testing.T) {
	for _, tt := range []struct {
		referer string
	}{{defaultReferer}, {""}} {
		var mu sync.Mutex
		var paths, referers []string
		s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			mu.Lock()
			paths = append(paths, r.URL.Path)
			referers = append(referers, r.Header.Get("Referer"))
			mu.Unlock()
			if r.URL.Path != "/download" {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			w.Write([]byte(strings.Repeat("a", 64<<10)))
		}))
		setupURL(t, "http://cfst.test/url", "http://cfst.test/download", defaultHttpingURL, "", false, true)
		oldPort, oldTimeout, oldReferer := TCPPort, Timeout, Referer
		_, port, _ := net.SplitHostPort(s.Listener.Addr().String())
		TCPPort, _ = strconv.Atoi(port)
		Timeout = time.Second
		Referer = tt.referer

		downloadHandler(localIP)

		TCPPort, Timeout, Referer = oldPort, oldTimeout, oldReferer
		s.Close()
		if len(paths) != 1 || paths[0] != "/download" {
			t.Errorf("请求路径 = %v, want [/download]", paths)
		}
		if len(referers) != 1 || referers[0] != tt.referer {
			t.Errorf("Referer = %q, want %q", referers, tt.referer)
		}
	}
}

// 用于下载测速的假函数：按 IP 最后一段返回速度 (MB/s)，并记录调用顺序
type fakeTester struct {
	mu        sync.Mutex
	downloads []string
	checks    []string
	speeds    map[string]float64 // MB/s
	fails     map[string]bool    // 没有通过 1034 检查的 IP
}

func (f *fakeTester) download(ip *net.IPAddr) (float64, string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.downloads = append(f.downloads, ip.String())
	return f.speeds[ip.String()] * 1024 * 1024, "HKG"
}

func (f *fakeTester) check(ip *net.IPAddr) (bool, string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.checks = append(f.checks, ip.String())
	return !f.fails[ip.String()], "LAX"
}

func ipList(set utils.DownloadSpeedSet) string {
	var ips []string
	for _, v := range set {
		ips = append(ips, v.IP.String())
	}
	return strings.Join(ips, ",")
}

func setupMinSpeed(t *testing.T, minSpeed float64) {
	old := MinSpeed
	t.Cleanup(func() { MinSpeed = old })
	MinSpeed = minSpeed
}

func TestDownloadTest(t *testing.T) {
	speeds := map[string]float64{"10.0.0.1": 1, "10.0.0.2": 5, "10.0.0.3": 3, "10.0.0.4": 4, "10.0.0.5": 2}
	ips := []string{"10.0.0.1", "10.0.0.2", "10.0.0.3", "10.0.0.4", "10.0.0.5"}

	t.Run("没有 1034 检查时按 -dn 下载测速，并返回所有 IP（包括没有下载测速的）", func(t *testing.T) {
		setupMinSpeed(t, 0)
		f := &fakeTester{speeds: speeds}
		result := downloadTest(makeIPSet(ips...), 2, "", f.download, nil)
		if got := strings.Join(f.downloads, ","); got != "10.0.0.1,10.0.0.2" {
			t.Errorf("下载测速了 %s", got)
		}
		if got := ipList(result); len(result) != 5 || !strings.HasPrefix(got, "10.0.0.2,10.0.0.1,") {
			t.Errorf("结果 = %s（下载测速过的 IP 应排在前面）", got)
		}
	})

	t.Run("1034 检查不通过的 IP 跳过，直到凑够 -dn 个，并且只返回通过检查的 IP", func(t *testing.T) {
		setupMinSpeed(t, 0)
		f := &fakeTester{speeds: speeds, fails: map[string]bool{"10.0.0.1": true, "10.0.0.3": true}}
		result := downloadTest(makeIPSet(ips...), 2, "", f.download, f.check)
		if got := strings.Join(f.checks, ","); got != "10.0.0.1,10.0.0.2,10.0.0.3,10.0.0.4" {
			t.Errorf("检查了 %s", got)
		}
		if got := strings.Join(f.downloads, ","); got != "10.0.0.2,10.0.0.4" {
			t.Errorf("下载测速了 %s（没有通过检查的 IP 不应下载测速）", got)
		}
		if got := ipList(result); got != "10.0.0.2,10.0.0.4" {
			t.Errorf("结果 = %s", got)
		}
		if result[0].Colo != "LAX" { // 地区码来自 1034 检查（TCPing 模式下之前没有地区码）
			t.Errorf("地区码 = %q", result[0].Colo)
		}
	})

	t.Run("优选域名 IP 全部下载测速", func(t *testing.T) {
		setupMinSpeed(t, 0)
		f := &fakeTester{speeds: speeds}
		result := downloadTest(makeIPSet(ips...), len(ips), domainGroupName, f.download, nil)
		if len(f.downloads) != 5 || len(result) != 5 {
			t.Errorf("下载测速了 %d 个, 结果 %d 个, want 5, 5", len(f.downloads), len(result))
		}
		if got := ipList(result); got != "10.0.0.2,10.0.0.4,10.0.0.3,10.0.0.5,10.0.0.1" {
			t.Errorf("结果应按下载速度排序 = %s", got)
		}
	})

	t.Run("下载速度下限 + 1034 检查", func(t *testing.T) {
		setupMinSpeed(t, 3)
		f := &fakeTester{speeds: speeds, fails: map[string]bool{"10.0.0.4": true}}
		result := downloadTest(makeIPSet(ips...), 2, "", f.download, f.check)
		if got := ipList(result); got != "10.0.0.2,10.0.0.3" {
			t.Errorf("结果 = %s", got)
		}
	})

	t.Run("不会修改全局的下载测速数量", func(t *testing.T) {
		setupMinSpeed(t, 0)
		old := TestCount
		f := &fakeTester{speeds: speeds}
		downloadTest(makeIPSet("10.0.0.1"), 10, "", f.download, nil)
		if TestCount != old {
			t.Errorf("TestCount 被修改为 %d, want %d", TestCount, old)
		}
	})
}
