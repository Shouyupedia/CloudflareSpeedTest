package task

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"time"

	"github.com/XIU2/CloudflareSpeedTest/utils"

	"github.com/VividCortex/ewma"
)

const (
	bufferSize                     = 1024
	defaultURL                     = "https://speed.cloudflare.com/__down?bytes=524288000" // Cloudflare 官方测速接口 (500MiB)，默认下载测速时间内不会提前下载完
	defaultReferer                 = "https://speed.cloudflare.com"                        // 默认下载测速地址超过 10MB 时必须带 Referer，否则返回 403
	defaultTimeout                 = 10 * time.Second
	defaultDisableDownload         = false
	defaultTestNum                 = 10
	defaultMinSpeed        float64 = 0.0

	userAgent              = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_12_6) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/98.0.4758.80 Safari/537.36"
	maxResponseHeaderBytes = 64 << 10 // 响应头大小上限 (64 KB)，避免异常的测速 IP 返回超大响应头占用大量内存
)

var (
	URL         = defaultURL     // 测速地址 (-url)：未指定下载测速地址 (-durl) 时用于下载测速，HTTPing 测试地址 (-httping-url) 为空时也使用该地址
	DownloadURL string           // 下载测速地址 (-durl)，为空时使用 URL
	Referer     = defaultReferer // 下载测速请求的 Referer (-referer)，为空时不发送
	Timeout     = defaultTimeout
	Disable     = defaultDisableDownload

	TestCount = defaultTestNum
	MinSpeed  = defaultMinSpeed
)

// 未指定 下载测速地址 (-durl)、HTTPing 测试地址 (-httping-url) 时，使用 测速地址 (-url)
func setDefaultURL() {
	if URL == "" {
		URL = defaultURL
	}
	if DownloadURL == "" {
		DownloadURL = URL
	}
	if HttpingURL == "" {
		HttpingURL = URL
	}
}

// CheckURL 检查测速地址是否有效（只检查实际会用到的地址）
func CheckURL() error {
	setDefaultURL()
	if !Disable {
		if err := checkURLFormat(DownloadURL); err != nil {
			return fmt.Errorf("下载测速地址无效 [%s]：%v", DownloadURL, err)
		}
	}
	if Httping {
		if err := checkURLFormat(HttpingURL); err != nil {
			return fmt.Errorf("HTTPing 测试地址无效 [%s]：%v", HttpingURL, err)
		}
	}
	if Test1043URL != "" {
		if err := checkURLFormat(Test1043URL); err != nil {
			return fmt.Errorf("1034 检查地址无效 [%s]：%v", Test1043URL, err)
		}
	}
	return nil
}

// 测速地址必须是 http:// 或 https:// 开头的完整地址
func checkURLFormat(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return err
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return errors.New("仅支持 http:// 或 https:// 开头的地址")
	}
	if u.Hostname() == "" {
		return errors.New("缺少域名")
	}
	return nil
}

func checkDownloadDefault() {
	setDefaultURL()
	if Timeout <= 0 {
		Timeout = defaultTimeout
	}
	if TestCount <= 0 {
		TestCount = defaultTestNum
	}
	if MinSpeed <= 0.0 {
		MinSpeed = defaultMinSpeed
	}
}

// 下载测速：指定了 1034 检查地址 (-test_1043_url) 时，每个 IP 下载测速前先进行 1034 检查
func testDownloadSpeed(ipSet utils.PingDelaySet, testCount int, name string) utils.DownloadSpeedSet {
	var check func(ip *net.IPAddr) (bool, string)
	if Test1043URL != "" {
		check = httpCheckIP
	}
	return downloadTest(ipSet, testCount, name, downloadHandler, check)
}

// 按顺序对 IP 下载测速，直到凑够 testCount 个满足条件的 IP 或测完全部 IP；check 不为空时，没有通过检查的 IP 不进行下载测速，也不会出现在结果中
func downloadTest(ipSet utils.PingDelaySet, testCount int, name string, download func(ip *net.IPAddr) (float64, string), check func(ip *net.IPAddr) (bool, string)) (speedSet utils.DownloadSpeedSet) {
	checkDownloadDefault()
	if len(ipSet) <= 0 { // IP 数组长度(IP数量) 大于 0 时才会继续下载测速
		utils.Yellow.Printf("[信息] %s可用 IP 数量为 0，跳过下载测速。\n", groupLabel(name, " 中"))
		return
	}
	testNum := testCount // 等待下载测速的队列数量 先默认等于 下载测速数量(-dn）
	// 如果延迟测速并过滤后的 IP 数组长度(IP数量) 小于 下载测速数量(-dn），（即 -dn 预期数量是不够的），或者指定了 下载测速下限 (-sl) 条件 或 1034 检查（不满足条件的 IP 不算数，这就可能要全部下载测速一遍，直到找齐预期数量或测完为止），则 等待下载测速的队列数量 修正为 IP 数量
	if len(ipSet) < testCount || MinSpeed > 0 || check != nil {
		testNum = len(ipSet)
	}
	if testNum < testCount { // 如果 等待下载测速的队列数量 小于 下载测速数量(-dn），（显然 -dn 预期数量是不够的），所以 下载测速数量(-dn）修正为 等待下载测速的队列数量
		testCount = testNum
	}

	checkInfo := ""
	if check != nil {
		checkInfo = ", 1034 检查：" + Test1043URL
	}
	utils.Cyan.Printf("开始下载测速（%s下限：%.2f MB/s, 数量：%d, 队列：%d%s）\n", groupLabel(name, ", "), MinSpeed, testCount, testNum, checkInfo)
	// 控制 下载测速进度条 与 延迟测速进度条 长度一致（强迫症）
	bar_a := len(strconv.Itoa(len(ipSet)))
	bar_b := "     "
	for i := 0; i < bar_a; i++ {
		bar_b += " "
	}
	bar := utils.NewBar(testCount, bar_b, "")
	var tested utils.DownloadSpeedSet // 进行了下载测速的 IP（有 1034 检查时即通过了检查的 IP）
	for i := 0; i < testNum; i++ {
		if check != nil {
			ok, colo := check(ipSet[i].IP)
			if !ok { // 返回 Cloudflare 1034 错误或无效状态码的 IP 无法使用，跳过
				continue
			}
			if ipSet[i].Colo == "" {
				ipSet[i].Colo = colo
			}
		}
		speed, colo := download(ipSet[i].IP)
		ipSet[i].DownloadSpeed = speed
		if ipSet[i].Colo == "" { // 只有当 Colo 是空的时候，才写入，否则代表之前是 httping 测速并获取过了
			ipSet[i].Colo = colo
		}
		tested = append(tested, ipSet[i])
		// 在每个 IP 下载测速后，以 [下载速度下限] 条件过滤结果
		if speed >= MinSpeed*1024*1024 {
			bar.Grow(1, "")
			speedSet = append(speedSet, ipSet[i]) // 高于下载速度下限时，添加到新数组中
			if len(speedSet) == testCount {       // 凑够满足条件的 IP 时（下载测速数量 -dn），就跳出循环
				break
			}
		}
	}
	bar.Done()
	if MinSpeed == 0.00 && check == nil { // 如果没有指定下载速度下限（也没有 1034 检查），则直接返回所有测速数据
		speedSet = utils.DownloadSpeedSet(ipSet)
	} else if MinSpeed > 0 && utils.Debug && len(speedSet) == 0 { // 如果指定了下载速度下限，且是调试模式下，且没有找到任何一个满足条件的 IP 时，返回所有测速数据，供用户查看当前的测速结果，以便适当调低预期测速条件
		utils.Yellow.Println("[调试] 没有满足 下载速度下限 条件的 IP，忽略条件返回所有测速数据（方便下次测速时调整条件）。")
		if check == nil {
			speedSet = utils.DownloadSpeedSet(ipSet)
		} else { // 没有通过 1034 检查的 IP 无法使用，不返回
			speedSet = tested
		}
	}
	// 按速度排序
	sort.Sort(speedSet)
	return
}

// 输出信息中的分组名称（如 优选域名 IP），sep 为分组名称后面的分隔符
func groupLabel(name, sep string) string {
	if name == "" {
		return ""
	}
	return name + sep
}

func getDialContext(ip *net.IPAddr) func(ctx context.Context, network, address string) (net.Conn, error) {
	var fakeSourceAddr string
	if isIPv4(ip.String()) {
		fakeSourceAddr = fmt.Sprintf("%s:%d", ip.String(), TCPPort)
	} else {
		fakeSourceAddr = fmt.Sprintf("[%s]:%d", ip.String(), TCPPort)
	}
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, fakeSourceAddr)
	}
}

// 所有连接都发往指定的测速 IP
func getTransport(ip *net.IPAddr) *http.Transport {
	return &http.Transport{
		DialContext:            getDialContext(ip),
		MaxResponseHeaderBytes: maxResponseHeaderBytes,
	}
}

// 统一的请求报错调试输出
func printDownloadDebugInfo(ip *net.IPAddr, err error, statusCode int, url, lastRedirectURL string, response *http.Response) {
	finalURL := url // 默认的最终 URL，这样当 response 为空时也能输出
	if lastRedirectURL != "" {
		finalURL = lastRedirectURL // 如果 lastRedirectURL 不是空，说明重定向过，优先输出最后一次要重定向至的目标
	} else if response != nil && response.Request != nil && response.Request.URL != nil {
		finalURL = response.Request.URL.String() // 如果 response 不为 nil，且 Request 和 URL 都不为 nil，则获取最后一次成功的响应地址
	}
	if url != finalURL { // 如果 URL 和最终地址不一致，说明有重定向，是该重定向后的地址引起的错误
		if statusCode > 0 { // 如果状态码大于 0，说明是后续 HTTP 状态码引起的错误
			utils.Red.Printf("[调试] IP: %s, 下载测速终止，HTTP 状态码: %d, 下载测速地址: %s, 出错的重定向后地址: %s\n", ip.String(), statusCode, url, finalURL)
		} else {
			utils.Red.Printf("[调试] IP: %s, 下载测速失败，错误信息: %v, 下载测速地址: %s, 出错的重定向后地址: %s\n", ip.String(), err, url, finalURL)
		}
	} else { // 如果 URL 和最终地址一致，说明没有重定向
		if statusCode > 0 { // 如果状态码大于 0，说明是后续 HTTP 状态码引起的错误
			utils.Red.Printf("[调试] IP: %s, 下载测速终止，HTTP 状态码: %d, 下载测速地址: %s\n", ip.String(), statusCode, url)
		} else {
			utils.Red.Printf("[调试] IP: %s, 下载测速失败，错误信息: %v, 下载测速地址: %s\n", ip.String(), err, url)
		}
	}
}

// return download Speed
func downloadHandler(ip *net.IPAddr) (float64, string) {
	var lastRedirectURL string // 用于记录最后一次重定向目标，以便在访问错误时输出
	client := &http.Client{
		Transport: getTransport(ip),
		Timeout:   Timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			lastRedirectURL = req.URL.String() // 记录每次重定向的目标，以便在访问错误时输出
			if len(via) > 10 {                 // 限制最多重定向 10 次
				if utils.Debug { // 调试模式下，输出更多信息
					utils.Red.Printf("[调试] IP: %s, 下载测速地址重定向次数过多，终止测速，下载测速地址: %s\n", ip.String(), req.URL.String())
				}
				return http.ErrUseLastResponse
			}
			if req.Header.Get("Referer") == defaultURL { // 当使用默认下载测速地址时，重定向不携带 Referer
				req.Header.Del("Referer")
			}
			return nil
		},
	}
	defer client.CloseIdleConnections()
	req, err := http.NewRequest("GET", DownloadURL, nil)
	if err != nil {
		if utils.Debug { // 调试模式下，输出更多信息
			utils.Red.Printf("[调试] IP: %s, 下载测速请求创建失败，错误信息: %v, 下载测速地址: %s\n", ip.String(), err, DownloadURL)
		}
		return 0.0, ""
	}

	req.Header.Set("User-Agent", userAgent)
	if Referer != "" { // 默认下载测速地址 (speed.cloudflare.com) 超过 10MB 时要求带 Referer，否则返回 403
		req.Header.Set("Referer", Referer)
	}

	response, err := client.Do(req)
	if err != nil {
		if utils.Debug { // 调试模式下，输出更多信息
			printDownloadDebugInfo(ip, err, 0, DownloadURL, lastRedirectURL, response)
		}
		return 0.0, ""
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		if utils.Debug { // 调试模式下，输出更多信息
			printDownloadDebugInfo(ip, nil, response.StatusCode, DownloadURL, lastRedirectURL, response)
		}
		return 0.0, ""
	}

	// 通过头部参数获取地区码
	colo := getHeaderColo(response.Header)

	timeStart := time.Now()           // 开始时间（当前）
	timeEnd := timeStart.Add(Timeout) // 加上下载测速时间得到的结束时间

	contentLength := response.ContentLength // 文件大小
	buffer := make([]byte, bufferSize)

	var (
		contentRead     int64 = 0
		timeSlice             = Timeout / 100
		timeCounter           = 1
		lastContentRead int64 = 0
	)

	var nextTime = timeStart.Add(timeSlice * time.Duration(timeCounter))
	e := ewma.NewMovingAverage()

	// 循环计算，如果文件下载完了（两者相等），则退出循环（终止测速）
	for contentLength != contentRead {
		currentTime := time.Now()
		if currentTime.After(nextTime) {
			timeCounter++
			nextTime = timeStart.Add(timeSlice * time.Duration(timeCounter))
			e.Add(float64(contentRead - lastContentRead))
			lastContentRead = contentRead
		}
		// 如果超出下载测速时间，则退出循环（终止测速）
		if currentTime.After(timeEnd) {
			break
		}
		bufferRead, err := response.Body.Read(buffer)
		if err != nil {
			if err != io.EOF { // 如果文件下载过程中遇到报错（如 Timeout），且并不是因为文件下载完了，则退出循环（终止测速）
				break
			} else if contentLength == -1 { // 文件下载完成 且 文件大小未知，则退出循环（终止测速），例如：https://speed.cloudflare.com/__down?bytes=200000000 这样的，如果在 10 秒内就下载完成了，会导致测速结果明显偏低甚至显示为 0.00（下载速度太快时）
				break
			}
			// 获取上个时间片
			last_time_slice := timeStart.Add(timeSlice * time.Duration(timeCounter-1))
			// 下载数据量 / (用当前时间 - 上个时间片/ 时间片)
			e.Add(float64(contentRead-lastContentRead) / (float64(currentTime.Sub(last_time_slice)) / float64(timeSlice)))
		}
		contentRead += int64(bufferRead)
	}
	return e.Value() / (Timeout.Seconds() / 120), colo
}
