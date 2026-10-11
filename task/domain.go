package task

import (
	"bufio"
	"context"
	"errors"
	"net"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/XIU2/CloudflareSpeedTest/utils"
)

const (
	defaultDomainFile    = "bestcf-domain.txt"
	domainResolveTimeout = 5 * time.Second // 解析每个优选域名的超时时间
	maxDomainResolvers   = 16              // 同时解析的优选域名数量上限
)

var (
	DomainFile          = defaultDomainFile // 优选域名文件 (-df)，为空时不使用
	DomainFileSpecified bool                // 是否通过 -df 参数明确指定了优选域名文件

	regexpDomain = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`) // 域名中只能包含的字符

	lookupIP = net.DefaultResolver.LookupIP // 解析域名（测试时替换）
)

// LoadIPs 读取 IP 段数据，以及优选域名文件 (-df) 中的域名解析出的 IP
// 返回 优选域名的 IP 和 其他要测速的 IP（优选域名中已有的 IP 不会重复测速）
func LoadIPs() (domainIPs, ips []*net.IPAddr) {
	checkPingDefault()
	ips = loadIPRanges()
	domainIPs = loadDomainIPs(ipNetwork(ips))
	if len(domainIPs) > 0 {
		ips = excludeIPs(ips, domainIPs)
	}
	return
}

// 读取并解析优选域名，只保留和 IP 段数据相同类型 (network: ip4 / ip6 / ip) 的 IP
func loadDomainIPs(network string) []*net.IPAddr {
	if DomainFile == "" {
		return nil
	}
	// 通过 -ip 参数指定了要测速的 IP 时，只有明确指定了 -df 才使用优选域名
	if IPText != "" && !DomainFileSpecified {
		return nil
	}
	domains, err := readDomainFile(DomainFile)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) && !DomainFileSpecified { // 默认的优选域名文件不存在时直接跳过
			return nil
		}
		utils.Yellow.Printf("[信息] 读取优选域名文件失败，跳过优选域名：%v\n", err)
		return nil
	}
	if len(domains) == 0 {
		return nil
	}
	utils.Cyan.Printf("开始解析优选域名（文件：%s, 域名：%d 个）\n", DomainFile, len(domains))
	ips := resolveDomains(domains, network)
	if len(ips) == 0 {
		utils.Yellow.Println("[信息] 优选域名没有解析出可以测速的 IP，跳过优选域名。")
	}
	return ips
}

// 读取优选域名文件：每行一个域名（也可以直接填写 IP），# 开头的行为注释
func readDomainFile(path string) ([]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	var domains []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(strings.TrimPrefix(scanner.Text(), "\ufeff")) // 去掉 UTF-8 BOM（如 Windows 记事本保存的文件）
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		domain := strings.Fields(line)[0] // 只取第一列，忽略后面的内容（如行尾注释）
		if net.ParseIP(domain) == nil && !regexpDomain.MatchString(domain) {
			utils.Yellow.Printf("[信息] 优选域名文件中的 [%s] 不是有效的域名或 IP，已跳过。\n", domain)
			continue
		}
		domains = append(domains, domain)
	}
	return domains, scanner.Err()
}

// 并发解析优选域名，返回去重后的 IP（按文件中的顺序）
func resolveDomains(domains []string, network string) []*net.IPAddr {
	results := make([][]net.IP, len(domains))
	errs := make([]error, len(domains))
	var wg sync.WaitGroup
	control := make(chan struct{}, maxDomainResolvers)
	for i, domain := range domains {
		if ip := net.ParseIP(domain); ip != nil { // 直接填写的 IP 不需要解析
			results[i] = []net.IP{ip}
			continue
		}
		wg.Add(1)
		control <- struct{}{}
		go func(i int, domain string) {
			defer wg.Done()
			defer func() { <-control }()
			ctx, cancel := context.WithTimeout(context.Background(), domainResolveTimeout)
			defer cancel()
			results[i], errs[i] = lookupIP(ctx, network, domain)
		}(i, domain)
	}
	wg.Wait()

	seen := make(map[string]bool)
	var ips []*net.IPAddr
	for i, domain := range domains {
		if errs[i] != nil {
			utils.Yellow.Printf("[信息] 优选域名 %s 解析失败：%v\n", domain, errs[i])
			continue
		}
		var resolved []string
		for _, ip := range results[i] {
			if !matchNetwork(ip, network) { // 和 IP 段数据的 IP 类型不同（如测速 IPv4 时的 IPv6）
				continue
			}
			key := ip.String()
			resolved = append(resolved, key)
			if !seen[key] {
				seen[key] = true
				ips = append(ips, &net.IPAddr{IP: ip})
			}
		}
		if utils.Debug { // 调试模式下，输出更多信息
			utils.Yellow.Printf("[调试] 优选域名 %s 解析出 %d 个 IP：%s\n", domain, len(resolved), strings.Join(resolved, ", "))
		}
	}
	return ips
}

// IP 段数据中的 IP 类型：只有 IPv4 时为 ip4，只有 IPv6 时为 ip6，否则为 ip（两者都有或没有 IP）
func ipNetwork(ips []*net.IPAddr) string {
	var v4, v6 bool
	for _, ip := range ips {
		if ip.IP.To4() != nil {
			v4 = true
		} else {
			v6 = true
		}
		if v4 && v6 {
			break
		}
	}
	switch {
	case v4 && !v6:
		return "ip4"
	case v6 && !v4:
		return "ip6"
	}
	return "ip"
}

func matchNetwork(ip net.IP, network string) bool {
	switch network {
	case "ip4":
		return ip.To4() != nil
	case "ip6":
		return ip.To4() == nil
	}
	return true
}

// 从 ips 中去掉 excludes 中已有的 IP（原地过滤）
func excludeIPs(ips, excludes []*net.IPAddr) []*net.IPAddr {
	exclude := make(map[string]bool, len(excludes))
	for _, ip := range excludes {
		exclude[ip.String()] = true
	}
	result := ips[:0]
	for _, ip := range ips {
		if !exclude[ip.String()] {
			result = append(result, ip)
		}
	}
	return result
}
