package task

import (
	"io"
	"net"
	"net/http"
	"regexp"
	"strconv"
	"sync"
	"time"

	"github.com/XIU2/CloudflareSpeedTest/utils"
)

const (
	httpCheckTimeout  = 5 * time.Second // 1034 检查超时时间（只判断返回结果是否有效、不计算延迟，因此比 HTTPing 宽松）
	httpCheckBodySize = 32 << 10        // 1034 检查时最多读取的响应内容大小 (32 KB)，足够判断是否为 Cloudflare 错误页面
)

var (
	// 1034 检查地址 (-test_1043_url)，为空时不检查；指定后会通过每个 IP 访问该地址，
	// 过滤掉返回 Cloudflare 1034 错误 (Edge IP Restricted，即该 IP 不允许用于访问该域名) 或无效状态码的 IP
	Test1043URL string

	// 匹配 Cloudflare 1034 错误的响应内容：
	// 非浏览器请求返回纯文本 "error code: 1034"，浏览器请求返回包含 "Edge IP Restricted"、<span class="cf-error-code">1034</span> 的 HTML 页面
	regexpCFError1034 = regexp.MustCompile(`(?i)\berror\s*(code)?\s*:?\s*1034\b|edge\s+ip\s+restricted|cf-error-code[^>]*>\s*1034\s*<`)
)

// CheckHttp 对所有 IP 进行 1034 检查并过滤（禁用下载测速时使用；启用下载测速时，会在每个 IP 下载测速前检查）
func CheckHttp(ipSet utils.PingDelaySet) utils.PingDelaySet {
	if Test1043URL == "" || len(ipSet) == 0 {
		return ipSet
	}
	utils.Cyan.Printf("开始 1034 检查（数量：%d, 地址：%s）\n", len(ipSet), Test1043URL)
	result := checkHttp(ipSet, httpCheckIP)
	if len(result) == 0 {
		utils.Yellow.Println("[信息] 没有通过 1034 检查的 IP（可加上 -debug 参数查看原因）。")
	}
	return result
}

// 并发检查所有 IP（并发数同延迟测速线程 -n），返回通过检查的 IP（保持原有排序）
func checkHttp(ipSet utils.PingDelaySet, check func(ip *net.IPAddr) (bool, string)) utils.PingDelaySet {
	routines := Routines
	if routines <= 0 {
		routines = defaultRoutines
	}
	var (
		wg      sync.WaitGroup
		m       sync.Mutex
		passed  = make([]bool, len(ipSet))
		able    int
		control = make(chan struct{}, routines)
		bar     = utils.NewBar(len(ipSet), "可用:", "")
	)
	for i := range ipSet {
		wg.Add(1)
		control <- struct{}{}
		go func(i int) {
			defer wg.Done()
			defer func() { <-control }()
			ok, colo := check(ipSet[i].IP)
			if ok && ipSet[i].Colo == "" { // 只有当 Colo 是空的时候（如 TCPing 模式）才写入，否则代表之前是 HTTPing 测速并获取过了
				ipSet[i].Colo = colo
			}
			m.Lock()
			passed[i] = ok
			if ok {
				able++
			}
			nowAble := able
			m.Unlock()
			bar.Grow(1, strconv.Itoa(nowAble))
		}(i)
	}
	wg.Wait()
	bar.Done()

	// 按原有顺序（延迟测速的排序）保留通过检查的 IP
	result := make(utils.PingDelaySet, 0, able)
	for i := range ipSet {
		if passed[i] {
			result = append(result, ipSet[i])
		}
	}
	return result
}

// 对单个 IP 进行 1034 检查：返回的 HTTP 状态码需为 2xx/3xx，且不能是 Cloudflare 1034 错误，返回是否通过检查 及 地区码
func httpCheckIP(ip *net.IPAddr) (bool, string) {
	client := http.Client{
		Timeout:   httpCheckTimeout,
		Transport: getTransport(ip),
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse // 阻止重定向（重定向状态码本身就可以判断是否有效）
		},
	}
	defer client.CloseIdleConnections()

	request, err := http.NewRequest(http.MethodGet, Test1043URL, nil)
	if err != nil {
		if utils.Debug { // 调试模式下，输出更多信息
			utils.Red.Printf("[调试] IP: %s, 1034 检查请求创建失败，错误信息: %v, 检查地址: %s\n", ip.String(), err, Test1043URL)
		}
		return false, ""
	}
	request.Header.Set("User-Agent", userAgent)
	response, err := client.Do(request)
	if err != nil {
		if utils.Debug { // 调试模式下，输出更多信息
			utils.Red.Printf("[调试] IP: %s, 1034 检查失败，错误信息: %v, 检查地址: %s\n", ip.String(), err, Test1043URL)
		}
		return false, ""
	}
	// 只读取开头部分响应内容（检查地址可能是个大文件），足够判断是否为 Cloudflare 错误页面
	body, err := io.ReadAll(io.LimitReader(response.Body, httpCheckBodySize))
	response.Body.Close()
	if err != nil {
		if utils.Debug { // 调试模式下，输出更多信息
			utils.Red.Printf("[调试] IP: %s, 1034 检查失败，读取响应内容出错: %v, 检查地址: %s\n", ip.String(), err, Test1043URL)
		}
		return false, ""
	}

	// 返回 Cloudflare 1034 错误说明该 IP 不允许用于访问该域名，无论状态码是什么都视为无效
	if isCFError1034(body) {
		if utils.Debug { // 调试模式下，输出更多信息
			utils.Red.Printf("[调试] IP: %s, 1034 检查不通过，返回 Cloudflare 1034 错误 (Edge IP Restricted)，HTTP 状态码: %d, 检查地址: %s\n", ip.String(), response.StatusCode, Test1043URL)
		}
		return false, ""
	}
	if response.StatusCode < 200 || response.StatusCode > 399 {
		if utils.Debug { // 调试模式下，输出更多信息
			utils.Red.Printf("[调试] IP: %s, 1034 检查不通过，HTTP 状态码: %d（有效状态码: 2xx/3xx）, 检查地址: %s\n", ip.String(), response.StatusCode, Test1043URL)
		}
		return false, ""
	}

	// 通过头部参数获取地区码
	return true, getHeaderColo(response.Header)
}

// 判断响应内容是否为 Cloudflare 1034 错误 (Edge IP Restricted)
func isCFError1034(body []byte) bool {
	return regexpCFError1034.Match(body)
}
