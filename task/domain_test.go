package task

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func ipStrings(ips []*net.IPAddr) string {
	var s []string
	for _, ip := range ips {
		s = append(s, ip.String())
	}
	return strings.Join(s, ",")
}

// 设置优选域名相关的全局变量，测试结束后恢复
func setupDomain(t *testing.T, content string, specified bool) {
	oldFile, oldSpecified, oldIPText, oldIPFile, oldLookup := DomainFile, DomainFileSpecified, IPText, IPFile, lookupIP
	t.Cleanup(func() {
		DomainFile, DomainFileSpecified, IPText, IPFile, lookupIP = oldFile, oldSpecified, oldIPText, oldIPFile, oldLookup
	})
	DomainFile = filepath.Join(t.TempDir(), "bestcf-domain.txt")
	if content != "" {
		if err := os.WriteFile(DomainFile, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	DomainFileSpecified = specified
	IPText = ""
	lookupIP = func(ctx context.Context, network, host string) ([]net.IP, error) {
		return nil, errors.New("不应解析域名")
	}
}

func TestReadDomainFile(t *testing.T) {
	setupDomain(t, "\ufeffa.example\r\n# 注释\n\n  b.example   # 行尾注释\n1.2.3.4\n2606:4700::1\nhttps://bad.example/\nc.example", true)
	domains, err := readDomainFile(DomainFile)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(domains, ","); got != "a.example,b.example,1.2.3.4,2606:4700::1,c.example" {
		t.Errorf("domains = %s", got)
	}
}

func TestResolveDomains(t *testing.T) {
	setupDomain(t, "", false)
	records := map[string][]string{
		"a.example": {"104.16.1.1", "2606:4700::1", "104.16.1.2"},
		"b.example": {"104.16.1.2", "104.16.1.3"}, // 104.16.1.2 重复
	}
	var mu sync.Mutex
	var networks []string
	lookupIP = func(ctx context.Context, network, host string) ([]net.IP, error) {
		mu.Lock()
		networks = append(networks, network)
		mu.Unlock()
		if host == "fail.example" {
			return nil, errors.New("no such host")
		}
		var ips []net.IP
		for _, ip := range records[host] {
			ips = append(ips, net.ParseIP(ip))
		}
		return ips, nil
	}
	ips := resolveDomains([]string{"a.example", "fail.example", "b.example", "9.9.9.9", "2606:4700::9"}, "ip4")
	if got := ipStrings(ips); got != "104.16.1.1,104.16.1.2,104.16.1.3,9.9.9.9" {
		t.Errorf("ips = %s（应去重，并只保留 IPv4）", got)
	}
	for _, network := range networks {
		if network != "ip4" {
			t.Errorf("解析时使用了 %s, want ip4", network)
		}
	}
	if ips := resolveDomains([]string{"a.example"}, "ip6"); ipStrings(ips) != "2606:4700::1" {
		t.Errorf("ip6: %s", ipStrings(ips))
	}
}

func TestLoadDomainIPs(t *testing.T) {
	t.Run("值为空时不使用", func(t *testing.T) {
		setupDomain(t, "1.1.1.1", true)
		DomainFile = ""
		if ips := loadDomainIPs("ip"); len(ips) != 0 {
			t.Errorf("ips = %s", ipStrings(ips))
		}
	})
	t.Run("默认文件不存在时跳过", func(t *testing.T) {
		setupDomain(t, "", false)
		if ips := loadDomainIPs("ip"); len(ips) != 0 {
			t.Errorf("ips = %s", ipStrings(ips))
		}
	})
	t.Run("使用 -ip 且没有明确指定 -df 时不使用", func(t *testing.T) {
		setupDomain(t, "1.1.1.1", false)
		IPText = "2.2.2.2"
		if ips := loadDomainIPs("ip"); len(ips) != 0 {
			t.Errorf("ips = %s", ipStrings(ips))
		}
	})
	t.Run("使用 -ip 且明确指定了 -df 时使用", func(t *testing.T) {
		setupDomain(t, "1.1.1.1", true)
		IPText = "2.2.2.2"
		if ips := loadDomainIPs("ip"); ipStrings(ips) != "1.1.1.1" {
			t.Errorf("ips = %s", ipStrings(ips))
		}
	})
}

// 优选域名的 IP 只保留和 IP 段数据相同类型的 IP，并且不会在 IP 段数据中重复测速
func TestLoadIPs(t *testing.T) {
	setupDomain(t, "104.16.0.2\n2606:4700::1\n104.16.0.3\n", true)
	IPText = "104.16.0.1,104.16.0.2"
	domainIPs, ips := LoadIPs()
	if got := ipStrings(domainIPs); got != "104.16.0.2,104.16.0.3" {
		t.Errorf("domainIPs = %s", got)
	}
	if got := ipStrings(ips); got != "104.16.0.1" {
		t.Errorf("ips = %s", got)
	}
}

func TestIPNetwork(t *testing.T) {
	v4 := &net.IPAddr{IP: net.ParseIP("1.1.1.1")}
	v6 := &net.IPAddr{IP: net.ParseIP("2606:4700::1")}
	for _, tt := range []struct {
		ips  []*net.IPAddr
		want string
	}{{[]*net.IPAddr{v4}, "ip4"}, {[]*net.IPAddr{v6}, "ip6"}, {[]*net.IPAddr{v4, v6}, "ip"}, {nil, "ip"}} {
		if got := ipNetwork(tt.ips); got != tt.want {
			t.Errorf("ipNetwork(%s) = %s, want %s", ipStrings(tt.ips), got, tt.want)
		}
	}
}
