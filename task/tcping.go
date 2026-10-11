package task

import (
	"fmt"
	"net"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/XIU2/CloudflareSpeedTest/utils"
)

const (
	tcpConnectTimeout = time.Second * 1
	maxRoutine        = 1000
	defaultRoutines   = 200
	defaultPort       = 443
	defaultPingTimes  = 4
)

var (
	Routines      = defaultRoutines
	TCPPort   int = defaultPort
	PingTimes int = defaultPingTimes
)

type Ping struct {
	wg      *sync.WaitGroup
	m       *sync.Mutex
	ips     []*net.IPAddr
	csv     utils.PingDelaySet
	control chan bool
	bar     *utils.Bar
	name    string // 分组名称（如 优选域名 IP），用于输出信息
}

func checkPingDefault() {
	if Routines <= 0 {
		Routines = defaultRoutines
	} else if Routines > maxRoutine { // 限制最大线程数，避免并发连接过多耗尽系统资源
		Routines = maxRoutine
	}
	if TCPPort <= 0 || TCPPort > 65535 {
		TCPPort = defaultPort
	}
	if PingTimes <= 0 {
		PingTimes = defaultPingTimes
	}
	setDefaultURL()
}

// 对指定的 IP 进行延迟测速，name 为分组名称（如 优选域名 IP）
func newPing(ips []*net.IPAddr, name string) *Ping {
	checkPingDefault()
	return &Ping{
		wg:      &sync.WaitGroup{},
		m:       &sync.Mutex{},
		ips:     ips,
		csv:     make(utils.PingDelaySet, 0),
		control: make(chan bool, Routines),
		name:    name,
	}
}

func (p *Ping) Run() utils.PingDelaySet {
	if len(p.ips) == 0 {
		return p.csv
	}
	mode := "TCP"
	if Httping {
		mode = "HTTP"
	}
	utils.Cyan.Printf("开始延迟测速（%s模式：%s, 端口：%d, 范围：%v ~ %v ms, 丢包：%.2f)\n", groupLabel(p.name, ", "), mode, TCPPort, utils.InputMinDelay.Milliseconds(), utils.InputMaxDelay.Milliseconds(), utils.InputMaxLossRate)
	p.bar = utils.NewBar(len(p.ips), "可用:", "") // 有 IP 时才创建进度条（创建后就会开始输出）
	for _, ip := range p.ips {
		p.wg.Add(1)
		p.control <- false
		go p.start(ip)
	}
	p.wg.Wait()
	p.bar.Done()
	sort.Sort(p.csv)
	return p.csv
}

func (p *Ping) start(ip *net.IPAddr) {
	defer p.wg.Done()
	p.tcpingHandler(ip)
	<-p.control
}

// bool connectionSucceed float32 time
func (p *Ping) tcping(ip *net.IPAddr) (bool, time.Duration) {
	startTime := time.Now()
	var fullAddress string
	if isIPv4(ip.String()) {
		fullAddress = fmt.Sprintf("%s:%d", ip.String(), TCPPort)
	} else {
		fullAddress = fmt.Sprintf("[%s]:%d", ip.String(), TCPPort)
	}
	conn, err := net.DialTimeout("tcp", fullAddress, tcpConnectTimeout)
	if err != nil {
		return false, 0
	}
	defer conn.Close()
	duration := time.Since(startTime)
	return true, duration
}

// pingReceived pingTotalTime
func (p *Ping) checkConnection(ip *net.IPAddr) (recv int, totalDelay time.Duration, colo string) {
	if Httping {
		recv, totalDelay, colo = p.httping(ip)
		return
	}
	colo = "" // TCPing 不获取 colo
	for i := 0; i < PingTimes; i++ {
		if ok, delay := p.tcping(ip); ok {
			recv++
			totalDelay += delay
		}
	}
	return
}

// 添加测速结果，并返回当前可用 IP 数量
func (p *Ping) appendIPData(data *utils.PingData) int {
	p.m.Lock()
	defer p.m.Unlock()
	p.csv = append(p.csv, utils.CloudflareIPData{
		PingData: data,
	})
	return len(p.csv)
}

// 当前可用 IP 数量（p.csv 会被多个线程同时修改，因此需要加锁读取）
func (p *Ping) ableCount() int {
	p.m.Lock()
	defer p.m.Unlock()
	return len(p.csv)
}

// handle tcping
func (p *Ping) tcpingHandler(ip *net.IPAddr) {
	recv, totalDlay, colo := p.checkConnection(ip)
	if recv == 0 {
		p.bar.Grow(1, strconv.Itoa(p.ableCount()))
		return
	}
	data := &utils.PingData{
		IP:       ip,
		Sended:   PingTimes,
		Received: recv,
		Delay:    totalDlay / time.Duration(recv),
		Colo:     colo,
	}
	p.bar.Grow(1, strconv.Itoa(p.appendIPData(data)))
}
