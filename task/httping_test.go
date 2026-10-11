package task

import (
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
)

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

// 设置 HTTPing / HTTP 检查相关的全局变量（测试地址指向测试服务器），测试结束后恢复
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

var localIP = &net.IPAddr{IP: net.ParseIP("127.0.0.1")}

func runHttping() (int, string) {
	recv, _, colo := (&Ping{}).httping(localIP)
	return recv, colo
}

// HTTPing 只负责连通性及延迟测速（只获取响应头），1034 错误由之后的 HTTP 检查判断
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
		{"403 无效状态码", 0, 403, "error code: 1034", false},
		{"指定状态码 403", 403, 403, "forbidden", true},
		{"指定状态码 403 时不检查响应内容（由 HTTP 检查判断 1034）", 403, 403, "error code: 1034", true},
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
			if got := s.count(http.MethodGet); got != 0 {
				t.Errorf("GET 请求次数 = %d, want 0（HTTPing 只发送 HEAD 请求）", got)
			}
			if got := atomic.LoadInt32(&s.conns); got != 1 {
				t.Errorf("连接数 = %d, want 1（所有请求复用同一个连接）", got)
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
