package task

import (
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
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

// 测试服务器：记录请求方法和新建连接数，所有连接都由 getDialContext 发往 127.0.0.1:服务器端口
type httpingTestServer struct {
	*httptest.Server
	mu      sync.Mutex
	methods []string
	conns   int32
}

func newHttpingTestServer(t *testing.T, handler http.HandlerFunc) *httpingTestServer {
	s := &httpingTestServer{}
	s.Server = httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.methods = append(s.methods, r.Method)
		s.mu.Unlock()
		handler(w, r)
	}))
	s.Config.ConnState = func(c net.Conn, state http.ConnState) {
		if state == http.StateNew {
			atomic.AddInt32(&s.conns, 1)
		}
	}
	s.Start()
	t.Cleanup(s.Close)
	return s
}

func (s *httpingTestServer) count(method string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, m := range s.methods {
		if m == method {
			n++
		}
	}
	return n
}

// 设置 HTTPing 相关的全局变量，测试结束后恢复
func setupHttping(t *testing.T, s *httpingTestServer, statusCode int) {
	oldURL, oldPort, oldTimes, oldCode, oldColo, oldColoMap := HttpingURL, TCPPort, PingTimes, HttpingStatusCode, HttpingCFColo, HttpingCFColomap
	t.Cleanup(func() {
		HttpingURL, TCPPort, PingTimes, HttpingStatusCode, HttpingCFColo, HttpingCFColomap = oldURL, oldPort, oldTimes, oldCode, oldColo, oldColoMap
	})
	_, port, err := net.SplitHostPort(s.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	TCPPort, _ = strconv.Atoi(port)
	HttpingURL = "http://cfst.test/test"
	PingTimes = 3
	HttpingStatusCode = statusCode
	HttpingCFColo = ""
	HttpingCFColomap = nil
}

func runHttping() (int, string) {
	recv, _, colo := (&Ping{}).httping(&net.IPAddr{IP: net.ParseIP("127.0.0.1")})
	return recv, colo
}

func TestHttping(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int // -httping-code
		respCode   int
		respBody   string
		wantRecv   bool
	}{
		{"200 正常", 0, 200, "ok", true},
		{"302 正常", 0, 302, "", true},
		{"404 无效状态码", 0, 404, "not found", false},
		{"403 纯文本 1034 错误", 0, 403, "error code: 1034", false},
		{"指定状态码 403 时仍过滤纯文本 1034 错误", 403, 403, "error code: 1034", false},
		{"指定状态码 403 时仍过滤 HTML 1034 错误", 403, 403, cfError1034HTML, false},
		{"状态码有效但内容为 1034 错误", 0, 200, cfError1034HTML, false},
		{"指定状态码 403 且不是 1034 错误", 403, 403, "forbidden", true},
		{"指定状态码不匹配", 200, 302, "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newHttpingTestServer(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.respCode)
				w.Write([]byte(tt.respBody))
			})
			setupHttping(t, s, tt.statusCode)
			recv, _ := runHttping()
			if tt.wantRecv && recv != PingTimes {
				t.Errorf("recv = %d, want %d", recv, PingTimes)
			}
			if !tt.wantRecv && recv != 0 {
				t.Errorf("recv = %d, want 0", recv)
			}
			if got := s.count(http.MethodGet); got != 1 {
				t.Errorf("GET 请求次数 = %d, want 1", got)
			}
		})
	}
}

func TestHttpingColo(t *testing.T) {
	s := newHttpingTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Server", "cloudflare")
		w.Header().Set("CF-RAY", "7bd32409eda7b020-SJC")
	})
	setupHttping(t, s, 0)
	if recv, colo := runHttping(); recv != PingTimes || colo != "SJC" {
		t.Errorf("recv, colo = %d, %q, want %d, %q", recv, colo, PingTimes, "SJC")
	}

	HttpingCFColo = "hkg,lax"
	HttpingCFColomap = MapColoMap()
	if recv, _ := runHttping(); recv != 0 {
		t.Errorf("地区码不匹配时 recv = %d, want 0", recv)
	}
}

func TestHttpingConnectionReuse(t *testing.T) {
	t.Run("小页面复用首次请求的连接", func(t *testing.T) {
		s := newHttpingTestServer(t, func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte("ok"))
		})
		setupHttping(t, s, 0)
		if recv, _ := runHttping(); recv != PingTimes {
			t.Fatalf("recv = %d, want %d", recv, PingTimes)
		}
		if got := s.count(http.MethodHead); got != PingTimes {
			t.Errorf("HEAD 请求次数 = %d, want %d", got, PingTimes)
		}
		if got := atomic.LoadInt32(&s.conns); got != 1 {
			t.Errorf("连接数 = %d, want 1", got)
		}
	})

	t.Run("大文件只读取开头部分，并在计时前重新建立连接", func(t *testing.T) {
		big := strings.Repeat("a", 4*httpingBodyCheckSize)
		s := newHttpingTestServer(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Length", strconv.Itoa(len(big)))
			if r.Method != http.MethodHead {
				w.Write([]byte(big))
			}
		})
		setupHttping(t, s, 0)
		if recv, _ := runHttping(); recv != PingTimes {
			t.Fatalf("recv = %d, want %d", recv, PingTimes)
		}
		// 1 次不计时的 HEAD 用于重新建立连接，之后 PingTimes 次计时的 HEAD 都复用该连接
		if got := s.count(http.MethodHead); got != PingTimes+1 {
			t.Errorf("HEAD 请求次数 = %d, want %d", got, PingTimes+1)
		}
		if got := atomic.LoadInt32(&s.conns); got != 2 {
			t.Errorf("连接数 = %d, want 2", got)
		}
	})
}
