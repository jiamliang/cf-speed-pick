// Copyright (C) 2026 cf-speed-pick contributors
// SPDX-License-Identifier: GPL-3.0-or-later
//
// CF IP 段清单：合并 CF 官方 + gslege/CloudflareIP + XIU2/CloudflareSpeedTest 三方。
//
// 来源说明：
//   - cfOfficialV4 / cfOfficialV6: Cloudflare 官方公开段（https://www.cloudflare.com/ips/）
//   - gslegeV4: gslege/CloudflareIP py/All.py 第 154-193 行硬编码段
//   - xiu2V4: XIU2/CloudflareSpeedTest ip.txt
//
// 三方合并后去重，IPv4 池约 30,000~60,000 个 IP。
// IPv6 段是天文数字（如 /32 = 2^96），实际用时按 -ipv6-sample 参数采样。

package main

import (
	"fmt"
	"net"
	"os"
	"strings"
)

// cfOfficialV4 CF 官方公开的 IPv4 段。
// 来源：https://www.cloudflare.com/ips-v4 （2024-2026 期间稳定）
var cfOfficialV4 = []string{
	"173.245.48.0/20",
	"103.21.244.0/22",
	"103.22.200.0/22",
	"103.31.4.0/22",
	"141.101.64.0/18",
	"108.162.192.0/18",
	"190.93.240.0/20",
	"188.114.96.0/20",
	"197.234.240.0/22",
	"198.41.128.0/17",
	"162.158.0.0/15",
	"104.16.0.0/13", // 注意：gslege 的 All.py 把这拆成 /24，XIU2 是 /12（覆盖更广）
	"104.24.0.0/14", // 104.24-104.27
	"172.64.0.0/16",
	"131.0.72.0/22",
}

// cfOfficialV6 CF 官方公开的 IPv6 段。
// 来源：https://www.cloudflare.com/ips-v6
// IPv6 段巨大（每个 /32 = 2^96），必须采样。
var cfOfficialV6 = []string{
	"2400:cb00::/32",
	"2606:4700::/32",
	"2803:f800::/32",
	"2405:b500::/32",
	"2405:8100::/32",
	"2a06:98c0::/29",
	"2c0f:f248::/32",
}

// gslegeV4 gslege/CloudflareIP py/All.py 第 154-193 行的硬编码段。
// 大部分已经被 cfOfficialV4 / xiu2V4 覆盖，这里只列额外的（避免重复）。
// 仔细对照后这些段基本都在主池子内，作为校验参考。
var gslegeV4 = []string{
	// gslege 的 104.x 拆得比官方细，全部被 xiu2V4 覆盖
	// gslege 的 172.64.x 同上
	// 实际上不需要额外添加，但保留变量以明示来源
}

// xiu2V4 XIU2/CloudflareSpeedTest ip.txt 内容。
// 与 cfOfficialV4 高度重叠，但粒度不同（XIU2 有些用 /12 / /18，CF 官方用 /13 / /16）。
// 这里直接用 XIU2 的版本作为主池——他的更全（/12 比 /13 大）。
var xiu2V4 = []string{
	"173.245.48.0/20",
	"103.21.244.0/22",
	"103.22.200.0/22",
	"103.31.4.0/22",
	"141.101.64.0/18",
	"108.162.192.0/18",
	"190.93.240.0/20",
	"188.114.96.0/20",
	"197.234.240.0/22",
	"198.41.128.0/17",
	"162.158.0.0/15",
	"104.16.0.0/12",
	"172.64.0.0/17",
	"172.64.128.0/18",
	"172.64.192.0/19",
	"172.64.224.0/22",
	"172.64.229.0/24",
	"172.64.230.0/23",
	"172.64.232.0/21",
	"172.64.240.0/21",
	"172.64.248.0/21",
	"172.65.0.0/16",
	"172.66.0.0/16",
	"172.67.0.0/16",
	"131.0.72.0/22",
}

// xiu2V6 XIU2/CloudflareSpeedTest ipv6.txt（这里用 cfOfficialV6 等价）
var xiu2V6 = cfOfficialV6

// allV4Ranges 返回所有 IPv4 段（合并去重后的最终清单）。
// 调用方负责去重。
func allV4Ranges() []string {
	seen := make(map[string]bool)
	out := make([]string, 0, 64)
	for _, list := range [][]string{cfOfficialV4, gslegeV4, xiu2V4} {
		for _, r := range list {
			if !seen[r] {
				seen[r] = true
				out = append(out, r)
			}
		}
	}
	return out
}

// allV6Ranges 返回所有 IPv6 段。
func allV6Ranges() []string {
	seen := make(map[string]bool)
	out := make([]string, 0, 16)
	for _, list := range [][]string{cfOfficialV6, xiu2V6} {
		for _, r := range list {
			if !seen[r] {
				seen[r] = true
				out = append(out, r)
			}
		}
	}
	return out
}

// loadRanges 从文件加载 CIDR 段。
// 格式：每行一个 CIDR，支持 # 注释和空行；含 ":" 的当 IPv6。
// path 为空 → 返回内置段。
// 文件不存在或读失败 → 警告 + 返回内置段（不报错，保留可用性）。
// 解析失败（非法 CIDR）→ 报错退出。
//
// 返回 (v4, v6, error)。
func loadRanges(path string) ([]string, []string, error) {
	if path == "" {
		return allV4Ranges(), allV6Ranges(), nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "WARN: read ranges file %s failed: %v — using built-in\n", path, err)
		return allV4Ranges(), allV6Ranges(), nil
	}

	var v4, v6 []string
	for i, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// 基本 CIDR 校验
		if _, _, err := net.ParseCIDR(line); err != nil {
			return nil, nil, fmt.Errorf("ranges file %s line %d: invalid CIDR %q: %w", path, i+1, line, err)
		}
		if strings.Contains(line, ":") {
			v6 = append(v6, line)
		} else {
			v4 = append(v4, line)
		}
	}

	// 如果用户文件全空，fallback 到内置
	if len(v4) == 0 {
		fmt.Fprintf(os.Stderr, "WARN: ranges file %s has no IPv4 — using built-in\n", path)
		v4 = allV4Ranges()
	}
	if len(v6) == 0 {
		v6 = allV6Ranges()
	}

	fmt.Fprintf(os.Stderr, "loaded %d IPv4 + %d IPv6 ranges from %s\n", len(v4), len(v6), path)
	return v4, v6, nil
}
