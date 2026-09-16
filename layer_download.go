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
	"sync/atomic"
	"time"
)

// DownloadParams 下载测速参数。
type DownloadParams struct {
	Port       int           // 端口（默认 443）
	URL        string        // 下载文件 URL（默认 CF speed endpoint）
	Timeout    time.Duration // 单 IP 最长测速时间（默认 10s）
	BufferSize int           // 读 buffer 大小（默认 4096）
	MinSpeedMB float64       // 过滤：低于此速度的 IP 丢弃（默认 0.05 MB/s）
	MaxResults int           // 返回 Top N（默认 10；<=0 表示全部）
}

func (p *DownloadParams) defaults() {
	if p.Port <= 0 || p.Port >= 65535 {
		p.Port = 443
	}
	if p.URL == "" {
		p.URL = "https://speed.cloudflare.com/__down?bytes=30000000"
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
	Colo  string  // 保留作为标签（不再参与分桶）
}

// RunDownload 跑 Layer 3：全部 IP 并发测速 → 按速度降序取 Top N。
// 不分 colo 桶，不分 tier（用户决策：速度优先）。
func RunDownload(input PingDelaySet, params DownloadParams) []DownloadResult {
	params.defaults()
	if len(input) == 0 {
		return nil
	}

	fmt.Printf("\n[Layer 3/3] 下载测速 %d IPs · 单IP最长%.0fs · 取Top %d\n",
		len(input), params.Timeout.Seconds(), params.MaxResults)

	// 并发测速
	const concurrency = 200
	sem := make(chan struct{}, concurrency)
	var (
		mu      sync.Mutex
		results = make([]DownloadResult, 0, len(input))
		wg      sync.WaitGroup
		idx     int64
	)

	for _, p := range input {
		wg.Add(1)
		sem <- struct{}{}
		go func(pd PingData) {
			defer wg.Done()
			defer func() { <-sem }()
			speed, colo := measureDownload(pd.IP, params)
			if colo == "" {
				colo = pd.Colo // fallback 到上层输入的 colo
			}
			atomic.AddInt64(&idx, 1)
			fmt.Printf("  [%d] %-15s speed=%6.2f MB/s colo=%s\n",
				idx, pd.IP.String(), speed, colo)
			if speed >= params.MinSpeedMB {
				mu.Lock()
				results = append(results, DownloadResult{
					IP:    pd.IP,
					Speed: speed,
					Colo:  colo,
				})
				mu.Unlock()
			}
		}(p)
	}
	wg.Wait()

	// 纯速度降序排
	sort.SliceStable(results, func(i, j int) bool {
		return results[i].Speed > results[j].Speed
	})

	// Top N
	if len(results) > params.MaxResults {
		results = results[:params.MaxResults]
	}

	fmt.Printf("\n[Layer 3] Top %d（按速度降序）：\n", len(results))
	for i, r := range results {
		fmt.Printf("  #%-2d %-15s  %6.2f MB/s  %s\n",
			i+1, r.IP.String(), r.Speed, r.Colo)
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