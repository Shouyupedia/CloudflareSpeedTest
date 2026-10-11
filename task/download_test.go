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
)

// 设置测速地址相关的全局变量，测试结束后恢复
func setupURL(t *testing.T, url, downloadURL, httpingURL string, disable, httping bool) {
	oldURL, oldDownloadURL, oldHttpingURL, oldDisable, oldHttping, oldCheck := URL, DownloadURL, HttpingURL, Disable, Httping, HttpCheck
	t.Cleanup(func() {
		URL, DownloadURL, HttpingURL, Disable, Httping, HttpCheck = oldURL, oldDownloadURL, oldHttpingURL, oldDisable, oldHttping, oldCheck
	})
	URL, DownloadURL, HttpingURL, Disable, Httping = url, downloadURL, httpingURL, disable, httping
	HttpCheck = httpingURL != "" // 同 main.go：指定了 HTTP 测试地址时进行 HTTP 检查
}

func TestCheckURLDefault(t *testing.T) {
	setupURL(t, "https://a.example/file", "", "", false, true)
	if err := CheckURL(); err != nil {
		t.Fatal(err)
	}
	if DownloadURL != URL || HttpingURL != URL {
		t.Errorf("DownloadURL, HttpingURL = %q, %q, want %q", DownloadURL, HttpingURL, URL)
	}

	setupURL(t, "", "", "", false, true)
	if err := CheckURL(); err != nil {
		t.Fatal(err)
	}
	if URL != defaultURL || DownloadURL != defaultURL || HttpingURL != defaultURL {
		t.Errorf("URL, DownloadURL, HttpingURL = %q, %q, %q, want %q", URL, DownloadURL, HttpingURL, defaultURL)
	}
}

func TestCheckURLSeparate(t *testing.T) {
	setupURL(t, "https://a.example/", "https://b.example/file", "http://c.example/", false, true)
	if err := CheckURL(); err != nil {
		t.Fatal(err)
	}
	if DownloadURL != "https://b.example/file" || HttpingURL != "http://c.example/" {
		t.Errorf("DownloadURL, HttpingURL = %q, %q", DownloadURL, HttpingURL)
	}
}

func TestCheckURLInvalid(t *testing.T) {
	tests := []struct {
		name                       string
		url, downloadURL, httping  string
		disable, httpingMode, fail bool
	}{
		{"下载测速地址协议无效", "https://a.example/", "ftp://b.example/", "", false, false, true},
		{"下载测速地址缺少域名", "https://a.example/", "https:///file", "", false, false, true},
		{"HTTPing 模式下 HTTP 测试地址无效", "https://a.example/", "", "example.com", false, true, true},
		{"HTTP 检查（TCPing 模式）时 HTTP 测试地址无效", "https://a.example/", "", "example.com", false, false, true},
		{"-url 无效且用于 HTTPing", "a.example", "https://b.example/", "", true, true, true},
		{"禁用下载测速时不检查下载测速地址", "https://a.example/", "ftp://b.example/", "", true, false, false},
		{"未启用 HTTPing 且未指定 HTTP 测试地址时不检查 -url", "a.example", "https://b.example/", "", false, false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setupURL(t, tt.url, tt.downloadURL, tt.httping, tt.disable, tt.httpingMode)
			if err := CheckURL(); (err != nil) != tt.fail {
				t.Errorf("CheckURL() error = %v, want fail = %v", err, tt.fail)
			}
		})
	}
}

// 下载测速应使用 -durl 指定的地址，而不是 -url
func TestDownloadUsesDownloadURL(t *testing.T) {
	var mu sync.Mutex
	var paths []string
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		paths = append(paths, r.URL.Path)
		mu.Unlock()
		if r.URL.Path != "/download" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Write([]byte(strings.Repeat("a", 64<<10)))
	}))
	defer s.Close()

	setupURL(t, "http://cfst.test/url", "http://cfst.test/download", "", false, false)
	oldPort, oldTimeout := TCPPort, Timeout
	defer func() { TCPPort, Timeout = oldPort, oldTimeout }()
	_, port, _ := net.SplitHostPort(s.Listener.Addr().String())
	TCPPort, _ = strconv.Atoi(port)
	Timeout = time.Second

	downloadHandler(&net.IPAddr{IP: net.ParseIP("127.0.0.1")})

	mu.Lock()
	defer mu.Unlock()
	if len(paths) != 1 || paths[0] != "/download" {
		t.Errorf("请求路径 = %v, want [/download]", paths)
	}
}
