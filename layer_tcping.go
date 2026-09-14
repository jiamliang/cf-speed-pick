// Copyright (C) 2026 cf-speed-pick contributors
// SPDX-License-Identifier: GPL-3.0-or-later
//
// Layer 1：TCPing 扫描。
//
// 对每个 IP 用 TCP 三次握手测延迟：连得上 + 延迟低 + 丢包少的留下。
//
// Adapted from XIU2/CloudflareSpeedTest task/tcping.go
// https://github.com/XIU2/CloudflareSpeedTest
// Original license: GPL-3.0

package main

import (
	"context"
	"fmt"
	"net"
	"sort"
	"sync"
	"time"
)

// TCPingParams TCPing 层参数。
type TCPingParams struct {
	Routines  int           // 并发数（默认 200；路由器降到 50）
	Port      int           // TCP 端口（默认 443）
	PingTimes int           // 每个 IP 测几次（默认 4）
	Timeout   time.Duration // 单次超时（默认 1s）
	MaxDelay  time.Duration // 过滤：平均延迟必须 <= 此值（默认 200ms）
	MaxLoss   float64       // 过滤：丢包率必须 <= 此值（默认 0.2）
}

func (p *TCPingParams) defaults() {
	if p.Routines <= 0 {
		p.Routines = 200
	}
	if p.Port <= 0 || p.Port >= 65535 {
		p.Port = 443
	}
	if p.PingTimes <= 0 {
		p.PingTimes = 4
	}
	if p.Timeout <= 0 {
		p.Timeout = time.Second
	}
	if p.MaxDelay <= 0 {
		p.MaxDelay = 200 * time.Millisecond
	}
	if p.MaxLoss <= 0 {
		p.MaxLoss = 0.2
	}
}

// TCPingResult TCPing 单个结果（含延时）。
type TCPingResult struct {
	PingData
}

// RunTCPing 跑 Layer 1：扫描所有 IP，返回延迟升序的 PingDelaySet。
func RunTCPing(ips []*net.IPAddr, params TCPingParams) PingDelaySet {
	params.defaults()
	if len(ips) == 0 {
		return nil
	}

	fmt.Printf("[Layer 1/3] TCPing %d IPs · 端口=%d · 并发=%d · 每IP测%d次\n",
		len(ips), params.Port, params.Routines, params.PingTimes)

	bar := NewBar(len(ips), "TCPing")
	defer bar.Finish()

	var (
		mu      sync.Mutex
		results = make(PingDelaySet, 0, len(ips)/10)
		wg      sync.WaitGroup
		ctrl    = make(chan struct{}, params.Routines)
	)

	for _, ip := range ips {
		wg.Add(1)
		ctrl <- struct{}{}
		go func(ip *net.IPAddr) {
			defer wg.Done()
			defer func() { <-ctrl }()

			data := tcpingProbe(ip, params)
			bar.Tick()
			if data.Received > 0 && data.Delay <= params.MaxDelay {
				bar.MarkOK()
				mu.Lock()
				results = append(results, data)
				mu.Unlock()
			}
			bar.Render()
		}(ip)
	}
	wg.Wait()

	// 过滤丢包率
	before := len(results)
	results = results.FilterLoss(params.MaxLoss)
	fmt.Printf("\n[Layer 1] 过滤: %d → %d（丢包率 ≤ %.0f%%）\n",
		before, len(results), params.MaxLoss*100)

	// 按延迟排序
	sort.Sort(results)
	return results
}

// tcpingProbe 对单个 IP 跑 PingTimes 次 TCP 连接，返回 PingData。
func tcpingProbe(ip *net.IPAddr, params TCPingParams) PingData {
	data := PingData{
		IP:     ip,
		Sended: params.PingTimes,
	}
	dialer := net.Dialer{Timeout: params.Timeout}
	addr := fmt.Sprintf("%s:%d", ip.String(), params.Port)

	var total time.Duration
	for i := 0; i < params.PingTimes; i++ {
		start := time.Now()
		ctx, cancel := context.WithTimeout(context.Background(), params.Timeout)
		conn, err := dialer.DialContext(ctx, "tcp", addr)
		cancel()
		if err == nil {
			_ = conn.Close()
			data.Received++
			total += time.Since(start)
		}
	}
	if data.Received > 0 {
		data.Delay = total / time.Duration(data.Received)
	}
	return data
}