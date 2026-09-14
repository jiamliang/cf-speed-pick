// Copyright (C) 2026 cf-speed-pick contributors
// SPDX-License-Identifier: GPL-3.0-or-later
//
// Layer 2：HTTPing 过滤。
//
// 对 Layer 1 筛出来的 IP 跑 HTTP HEAD 请求：
//   - 必须 200/301/302 通过
//   - 必须能从 cf-ray 头拿到 IATA 地区码
//   - 计算平均延迟、丢包率
//
// Adapted from XIU2/CloudflareSpeedTest task/httping.go
// https://github.com/XIU2/CloudflareSpeedTest
// Original license: GPL-3.0

package main

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// HTTPingParams HTTPing 层参数。
type HTTPingParams struct {
	Routines    int           // 并发数（默认 50）
	Port        int           // 端口（默认 443）
	URL         string        // 测试 URL（必须是 CF-fronted 域名）
	PingTimes   int           // 每个 IP 测几次（默认 4）
	Timeout     time.Duration // 单次超时（默认 2s）
	MaxDelay    time.Duration // 过滤：平均延迟上限（默认 200ms）
	MaxLoss     float64       // 过滤：丢包率上限（默认 0.2）
	CFColo      string        // 可选：只保留指定 IATA 码（如 "HKG,NRT,SJC"）
	MinStatusOK int           // 通过的 HTTP 状态码（默认 0 = 200/301/302 都行）
}

func (p *HTTPingParams) defaults() {
	if p.Routines <= 0 {
		p.Routines = 50
	}
	if p.Port <= 0 || p.Port >= 65535 {
		p.Port = 443
	}
	if p.URL == "" {
		p.URL = "https://cf.xiu2.xyz/url"
	}
	if p.PingTimes <= 0 {
		p.PingTimes = 4
	}
	if p.Timeout <= 0 {
		p.Timeout = 2 * time.Second
	}
	if p.MaxDelay <= 0 {
		p.MaxDelay = 200 * time.Millisecond
	}
	if p.MaxLoss <= 0 {
		p.MaxLoss = 0.2
	}
}

// regexpColoIATA 匹配 cf-ray 中的 IATA 机场三字码（如 SJC、NRT、FRA）。
var regexpColoIATA = regexp.MustCompile(`[A-Z]{3}`)

// extractColo 从 HTTP 响应头提取 CF IATA 码。
// 只识别 Cloudflare（通过 server: cloudflare + cf-ray 头）。
func extractColo(h http.Header) string {
	if strings.ToLower(h.Get("server")) == "cloudflare" {
		if ray := h.Get("cf-ray"); ray != "" {
			return regexpColoIATA.FindString(ray)
		}
	}
	return ""
}

// matchCFColo 判断 colo 是否在用户指定的列表中（逗号分隔）。
func matchCFColo(colo, list string) bool {
	if list == "" {
		return true
	}
	if colo == "" {
		return false
	}
	for _, c := range strings.Split(strings.ToUpper(list), ",") {
		if strings.EqualFold(colo, strings.TrimSpace(c)) {
			return true
		}
	}
	return false
}

// RunHTTPing 跑 Layer 2：在 Layer 1 结果基础上做 HTTP 探测。
func RunHTTPing(input PingDelaySet, params HTTPingParams) PingDelaySet {
	params.defaults()
	if len(input) == 0 {
		return nil
	}

	fmt.Printf("\n[Layer 2/3] HTTPing %d IPs · URL=%s · 并发=%d\n",
		len(input), params.URL, params.Routines)
	if params.CFColo != "" {
		fmt.Printf("  限定地区码：%s\n", params.CFColo)
	}

	bar := NewBar(len(input), "HTTPing")
	defer bar.Finish()

	var (
		mu      sync.Mutex
		results = make(PingDelaySet, 0, len(input)/3)
		wg      sync.WaitGroup
		ctrl    = make(chan struct{}, params.Routines)
	)

	for _, p := range input {
		wg.Add(1)
		ctrl <- struct{}{}
		go func(p PingData) {
			defer wg.Done()
			defer func() { <-ctrl }()

			data := httpingProbe(p.IP, params)
			bar.Tick()

			// 必须有 colo 且状态码通过 + 延迟/丢包过滤
			if data.Colo == "" {
				bar.Render()
				return
			}
			if data.Received == 0 {
				bar.Render()
				return
			}
			if data.LossRate() > params.MaxLoss {
				bar.Render()
				return
			}
			if data.Delay > params.MaxDelay {
				bar.Render()
				return
			}

			bar.MarkOK()
			mu.Lock()
			results = append(results, data)
			mu.Unlock()
			bar.Render()
		}(p)
	}
	wg.Wait()

	sort.Sort(results)
	fmt.Printf("\n[Layer 2] 通过：%d / %d\n", len(results), len(input))
	return results
}

// httpingProbe 对单个 IP 跑 HTTP HEAD 探测。
// 第一次请求用于拿状态码 + cf-ray 头（colo）；
// 后续 PingTimes 次请求用于测延迟。
func httpingProbe(ip *net.IPAddr, params HTTPingParams) PingData {
	data := PingData{IP: ip, Sended: params.PingTimes}

	hc := &http.Client{
		Timeout: params.Timeout,
		Transport: &http.Transport{
			DialContext: getDialContext(ip, params.Port),
		},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	defer hc.CloseIdleConnections()

	ua := "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_12_6) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/98.0.4758.80 Safari/537.36"

	doOne := func(closeConn bool) (ok bool, status int, colo string, dur time.Duration) {
		req, err := http.NewRequest(http.MethodHead, params.URL, nil)
		if err != nil {
			return false, 0, "", 0
		}
		req.Header.Set("User-Agent", ua)
		if closeConn {
			req.Header.Set("Connection", "close")
		}
		start := time.Now()
		resp, err := hc.Do(req)
		if err != nil {
			return false, 0, "", 0
		}
		defer resp.Body.Close()
		_, _ = io.Copy(io.Discard, resp.Body)
		c := extractColo(resp.Header)
		return true, resp.StatusCode, c, time.Since(start)
	}

	// 第一次：状态码 + colo
	ok, status, colo, _ := doOne(false)
	if !ok {
		return data
	}
	if !statusOK(status, params.MinStatusOK) {
		return data
	}
	if !matchCFColo(colo, params.CFColo) {
		return data
	}
	data.Colo = colo

	// 后续：测延迟
	var total time.Duration
	for i := 0; i < params.PingTimes; i++ {
		last := i == params.PingTimes-1
		ok, _, _, dur := doOne(last)
		if ok {
			data.Received++
			total += dur
		}
	}
	if data.Received > 0 {
		data.Delay = total / time.Duration(data.Received)
	}
	return data
}

// statusOK 判断响应状态码是否通过。
// wantStatus = 0 表示只接受 200/301/302；否则必须严格相等。
func statusOK(got, want int) bool {
	if want == 0 {
		return got == 200 || got == 301 || got == 302
	}
	return got == want
}