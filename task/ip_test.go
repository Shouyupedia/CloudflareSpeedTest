package task

import (
	"net"
	"testing"
	"time"
)

// 设置 IP 段相关的全局变量并生成要测速的 IP，生成超时（死循环）时测试失败
func loadIPRangesFor(t *testing.T, ipText string, testAll bool) []*net.IPAddr {
	t.Helper()
	oldIPText, oldTestAll := IPText, TestAll
	defer func() { IPText, TestAll = oldIPText, oldTestAll }()
	IPText, TestAll = ipText, testAll

	done := make(chan []*net.IPAddr, 1)
	go func() { done <- loadIPRanges() }()
	select {
	case ips := <-done:
		return ips
	case <-time.After(30 * time.Second):
		t.Fatalf("生成 IP 段 %s 的 IP 超时（死循环）", ipText)
		return nil
	}
}

func assertInRange(t *testing.T, cidr string, ips []*net.IPAddr) {
	t.Helper()
	_, ipNet, err := net.ParseCIDR(cidr)
	if err != nil {
		t.Fatal(err)
	}
	for _, ip := range ips {
		if !ipNet.Contains(ip.IP) {
			t.Fatalf("IP %s 超出了 IP 段 %s 的范围", ip, cidr)
		}
	}
}

func TestLoadIPRangesIPv4(t *testing.T) {
	if ips := loadIPRangesFor(t, "1.1.1.1", false); len(ips) != 1 || ips[0].String() != "1.1.1.1" {
		t.Errorf("单个 IP: %v", ips)
	}

	ips := loadIPRangesFor(t, "104.16.0.0/22", false)
	if len(ips) != 4 { // 每个 /24 随机一个 IP
		t.Errorf("/22 生成了 %d 个 IP, want 4", len(ips))
	}
	assertInRange(t, "104.16.0.0/22", ips)

	ips = loadIPRangesFor(t, "104.16.0.0/24", true)
	if len(ips) != 256 {
		t.Errorf("-allip /24 生成了 %d 个 IP, want 256", len(ips))
	}
	assertInRange(t, "104.16.0.0/24", ips)
}

func TestChooseIPv4ZeroPrefix(t *testing.T) {
	// 0.0.0.0/0 曾经会在遍历到 255.255.255.X 后回到 0.0.0.X 导致死循环（完整遍历会生成 2^24 个 IP，因此从结尾附近开始遍历）
	r := newIPRanges()
	r.parseCIDR("0.0.0.0/0")
	r.firstIP = net.ParseIP("255.255.250.0")
	done := make(chan struct{})
	go func() {
		r.chooseIPv4()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("生成 0.0.0.0/0 的 IP 超时（死循环）")
	}
	if len(r.ips) != 6 {
		t.Errorf("255.255.250.0 ~ 255.255.255.255 生成了 %d 个 IP, want 6", len(r.ips))
	}
	assertInRange(t, "255.255.248.0/21", r.ips)

	// 结尾的 IP 段也不能越界回到开头
	ips := loadIPRangesFor(t, "255.255.254.0/23", false)
	if len(ips) != 2 {
		t.Errorf("255.255.254.0/23 生成了 %d 个 IP, want 2", len(ips))
	}
	assertInRange(t, "255.255.254.0/23", ips)
}

func TestLoadIPRangesIPv6(t *testing.T) {
	if ips := loadIPRangesFor(t, "2606:4700::1", false); len(ips) != 1 || ips[0].String() != "2606:4700::1" {
		t.Errorf("单个 IP: %v", ips)
	}
	for _, cidr := range []string{"2606:4700::/32", "2606:4700::/112", "2606:4700::/120", "2606:4700::ff00/124", "2606:4700::/127"} {
		for i := 0; i < 20; i++ { // IP 是随机生成的，多试几次
			ips := loadIPRangesFor(t, cidr, false)
			if len(ips) == 0 {
				t.Fatalf("%s 没有生成 IP", cidr)
			}
			assertInRange(t, cidr, ips)
		}
	}
}

func TestLoadIPRangesIPv6ZeroPrefix(t *testing.T) {
	// ::/0 曾经会在最高位进位溢出后回到开头导致死循环
	if ips := loadIPRangesFor(t, "::/0", false); len(ips) == 0 {
		t.Error("::/0 没有生成 IP")
	}
}
