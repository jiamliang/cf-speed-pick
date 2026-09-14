// Copyright (C) 2026 cf-speed-pick contributors
// SPDX-License-Identifier: GPL-3.0-or-later
//
// Layer 3：下载测速。
//
// 对 Layer 2 筛出来的 IP 跑真实下载：
//   - 用 DialContext 把源地址锁到指定 CF IP（SNI 匹配目标域名）
//   - EWMA 平滑字节/时间片
//   - 转成 MB/s 取 Top N
//
// Adapted from XIU2/CloudflareSpeedTest task/download.go
// https://github.com/XIU2/CloudflareSpeedTest
// Original license: GPL-3.0
//
// 注：XIU2 原版的单位换算用 (Timeout.Seconds()/120) 有 20% 偏差，
// 我们这里用正确的 (Timeout.Seconds()/100) + 字节单位换算。

package main

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"sort"
	"sync"
	"time"
)

// DownloadParams 下载测速参数。
type DownloadParams struct {
	Port         int           // 端口（默认 443）
	URL          string        // 下载文件 URL（默认 https://cf.xiu2.xyz/url）
	Timeout      time.Duration // 单 IP 最长测速时间（默认 10s）
	BufferSize   int           // 读 buffer 大小（默认 4096）
	DisableLog   bool          // 调试输出开关
	MinSpeedMB   float64       // 过滤：低于此速度的 IP 丢弃（默认 0.05 MB/s）
	MaxResults   int           // 返回 Top N（默认 10；<=0 表示全部）
	ColoPriority []string      // colo 优先级列表（空 = 默认）
	Strategy     TierStrategy  // 采样策略（默认 Tiered）
}

func (p *DownloadParams) defaults() {
	if p.Port <= 0 || p.Port >= 65535 {
		p.Port = 443
	}
	if p.URL == "" {
		p.URL = "https://cf.xiu2.xyz/url"
	}
	if p.Timeout <= 0 {
		p.Timeout = 10 * time.Second
	}
	if p.BufferSize <= 0 {
		p.BufferSize = 4096
	}
	if p.MaxResults <= 0 {
		p.MaxResults = 10
	}
}

// DownloadResult 单 IP 下载测速结果。
type DownloadResult struct {
	IP    *net.IPAddr
	Speed float64 // MB/s
	Colo  string
	Tier  int // colo 所在 tier（1/2/3/99）
}

// RunDownload 跑 Layer 3：按 colo 优先级分桶采样 → 单线程顺序测速 → 综合排序取 Top N。
// 综合排序：先按 tier（越小越优先），同 tier 内按速度降序。
func RunDownload(input PingDelaySet, params DownloadParams) []DownloadResult {
	params.defaults()
	if len(input) == 0 {
		return nil
	}

	queue := buildColoQueue(input, params.ColoPriority, params.Strategy)
	if len(queue) == 0 {
		return nil
	}

	totalIPs := 0
	for _, b := range queue {
		totalIPs += len(b.IPs)
	}
	fmt.Printf("\n[Layer 3/3] 下载测速 %d IPs（从 %d 候选采样）· 单IP最长%.0fs · 取Top %d\n",
		totalIPs, len(input), params.Timeout.Seconds(), params.MaxResults)
	printColoQueue(queue, params.Strategy)

	results := make([]DownloadResult, 0, totalIPs)

	// 跨桶累计 idx
	idx := 0
	for _, b := range queue {
		fmt.Printf("\n[Tier %d / %s] %d IPs:\n", b.Tier, b.Colo, len(b.IPs))
		for _, p := range b.IPs {
			idx++
			speed, colo := measureDownload(p.IP, params)
			// 用桶的 tier 信息（measureDownload 返回的 colo 可能与桶不一致，以桶为准）
			if colo == "" {
				colo = b.Colo
			}
			fmt.Printf("  [%d] %-15s speed=%6.2f MB/s colo=%s\n",
				idx, p.IP.String(), speed, colo)
			if speed >= params.MinSpeedMB {
				results = append(results, DownloadResult{
					IP:    p.IP,
					Speed: speed,
					Colo:  colo,
					Tier:  b.Tier,
				})
			}
		}
	}

	// 综合排序：先 tier 升序，同 tier 内按速度降序
	sort.SliceStable(results, func(i, j int) bool {
		if results[i].Tier != results[j].Tier {
			return results[i].Tier < results[j].Tier
		}
		return results[i].Speed > results[j].Speed
	})

	// Top N
	if len(results) > params.MaxResults {
		results = results[:params.MaxResults]
	}

	fmt.Printf("\n[Layer 3] Top %d（按 tier → 速度）：\n", len(results))
	for i, r := range results {
		fmt.Printf("  #%-2d %-15s  %6.2f MB/s  %s  [Tier %d]\n",
			i+1, r.IP.String(), r.Speed, r.Colo, r.Tier)
	}
	return results
}

// measureDownload 对单个 IP 跑真实下载，返回 MB/s 和 colo。
func measureDownload(ip *net.IPAddr, params DownloadParams) (float64, string) {
	hc := &http.Client{
		Transport: &http.Transport{
			DialContext: getDialContext(ip, params.Port),
		},
		Timeout: params.Timeout + 2*time.Second, // 比下载时长稍宽裕
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) > 10 {
				return http.ErrUseLastResponse
			}
			return nil
		},
	}
	defer hc.CloseIdleConnections()

	req, err := http.NewRequest(http.MethodGet, params.URL, nil)
	if err != nil {
		return 0, ""
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_12_6) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/98.0.4758.80 Safari/537.36")

	resp, err := hc.Do(req)
	if err != nil {
		return 0, ""
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return 0, ""
	}

	colo := extractColo(resp.Header)

	// 跑 EWMA
	start := time.Now()
	end := start.Add(params.Timeout)
	contentLength := resp.ContentLength

	timeSlice := params.Timeout / 100
	timeCounter := 1
	nextTime := start.Add(timeSlice)

	ewma := NewMovingAverage() // 自写 EWMA
	var (
		contentRead  int64
		lastRead     int64
		buffer       = make([]byte, params.BufferSize)
	)

	for {
		if contentLength > 0 && contentRead >= contentLength {
			break
		}
		now := time.Now()
		if now.After(nextTime) {
			timeCounter++
			nextTime = start.Add(timeSlice * time.Duration(timeCounter))
			ewma.Add(float64(contentRead - lastRead))
			lastRead = contentRead
		}
		if now.After(end) {
			break
		}
		n, err := resp.Body.Read(buffer)
		if err != nil {
			if err != io.EOF {
				break
			}
			// EOF：把剩余字节补到 EWMA
			if contentLength < 0 {
				break
			}
			lastSlice := start.Add(timeSlice * time.Duration(timeCounter-1))
			elapsed := now.Sub(lastSlice).Seconds()
			if elapsed > 0 {
				ewma.Add(float64(contentRead-lastRead) / (elapsed / timeSlice.Seconds()))
			}
		}
		contentRead += int64(n)
	}

	if ewma.Value() <= 0 {
		return 0, colo
	}

	// 单位换算：
	//   e.Value() = 字节 / 时间片
	//   时间片秒数 = Timeout / 100
	//   字节/秒 = e.Value() / (Timeout.Seconds() / 100)
	//   MB/秒  = 字节/秒 / (1024*1024)
	bytesPerSec := ewma.Value() / timeSlice.Seconds()
	mbPerSec := bytesPerSec / (1024.0 * 1024.0)
	return mbPerSec, colo
}

// _ = sync.Mutex{} 防止 import 报警（如果以后要加并发的话）
var _ = sync.Mutex{}