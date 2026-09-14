// Copyright (C) 2026 cf-speed-pick contributors
// SPDX-License-Identifier: GPL-3.0-or-later
//
// 共享 dial 工具：把 HTTP 客户端源地址锁定到指定 CF IP。
//
// 原理：CF edge 的"任播 IP"上跑着 HTTPS 终止（用 SNI 区分域名）。
// 我们把 dial 的目标地址直接写成 `ip:port`，TLS 握手时 SNI 用域名，
// 这样会走到 CF 的边缘节点上去处理，CF 把 cf-ray 回给客户端。
//
// Adapted from XIU2/CloudflareSpeedTest task/download.go getDialContext
// https://github.com/XIU2/CloudflareSpeedTest
// Original license: GPL-3.0

package main

import (
	"context"
	"fmt"
	"net"
	"strings"
)

// isIPv4 简单判断 IP 字符串是否为 IPv4（含 IPv4-in-IPv6）。
func isIPv4(s string) bool {
	return strings.Contains(s, ".") && !strings.Contains(s, ":")
}

// getDialContext 返回一个 DialContext 函数，把 HTTP 客户端强制连接到指定 CF IP。
// port 是 TCP 端口（HTTPing 443 / 下载 443）。
func getDialContext(ip *net.IPAddr, port int) func(ctx context.Context, network, address string) (net.Conn, error) {
	var addr string
	if isIPv4(ip.String()) {
		addr = fmt.Sprintf("%s:%d", ip.String(), port)
	} else {
		addr = fmt.Sprintf("[%s]:%d", ip.String(), port)
	}
	return func(ctx context.Context, network, _ string) (net.Conn, error) {
		// 用普通 Dialer，net.Transport.DialContext 签名是 (ctx, network, address)
		// 我们忽略外层传入的 address，用我们预定的 addr
		var d net.Dialer
		return d.DialContext(ctx, network, addr)
	}
}