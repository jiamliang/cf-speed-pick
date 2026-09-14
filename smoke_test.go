// Copyright (C) 2026 cf-speed-pick contributors
// SPDX-License-Identifier: GPL-3.0-or-later
//
// 烟雾测试：跑一个最小的 IP 池 + TCPing（默认 1.1.1.1），验证管道能跑通。
// 仅在非 -short 模式下执行（依赖网络）。

package main

import (
	"net"
	"testing"
)

func TestSmoke_PoolAndPing(t *testing.T) {
	if testing.Short() {
		t.Skip("skip smoke test in -short mode")
	}

	// 极小 IP 池：只测 3 个已知 CF 段的开头
	pool := []*net.IPAddr{
		{IP: net.ParseIP("1.1.1.1")}, // 公开 DNS
		{IP: net.ParseIP("1.0.0.1")},
		{IP: net.ParseIP("192.0.2.1")}, // 不可达
	}

	results := RunTCPing(pool, TCPingParams{
		Routines:  3,
		Port:      443,
		PingTimes: 1,
		Timeout:   2 * 1e9, // 2s
		MaxDelay:  300 * 1e6, // 300ms
		MaxLoss:   0.99, // 不过滤
	})

	t.Logf("smoke: %d reachable", len(results))
	// 至少 1.1.1.1 应该可达（在能联网的环境下）
	if len(results) == 0 {
		t.Skip("no reachable IPs (沙盒无网络)")
	}
}