// Copyright (C) 2026 cf-speed-pick contributors
// SPDX-License-Identifier: GPL-3.0-or-later
//
// IP 池构造：从 CIDR 段展开为 IP 列表。
//
// IPv4 策略：
//   - 段内 IP 数 ≤ 1024：完全展开（所有 IP）
//   - 段内 IP 数 > 1024（如 /20 = 4096、/12 = 1M）：随机采样 1024 个
//
// IPv6 策略：
//   - 永远随机采样（-ipv6-sample 控制每段采样数，默认 1000）
//
// 去重：所有展开后的 IP 用 map 去重，避免跨段重复。

package main

import (
	"encoding/binary"
	"fmt"
	"math/rand"
	"net"
	"strings"
)

// maxExpandPerRange 单个段最大展开 IP 数。
// 超过这个数的段用采样代替全展开，避免 /12 这种段展开出 100 万 IP。
const maxExpandPerRange = 1024

// defaultIPv6Sample IPv6 每段默认采样数。
const defaultIPv6Sample = 1000

// PoolOptions 控制 IP 池构造。
type PoolOptions struct {
	IncludeV4 bool // 是否包含 IPv4（默认 true）
	IncludeV6 bool // 是否包含 IPv6（默认 false，需要 -ipv6）
	V6Sample  int  // IPv6 每段采样数（默认 1000）
}

// BuildIPPool 根据 options 构造 IP 池。
// 返回 net.IPAddr 列表（顺序随机）。
func BuildIPPool(opts PoolOptions) ([]*net.IPAddr, error) {
	if opts.V6Sample <= 0 {
		opts.V6Sample = defaultIPv6Sample
	}

	seen := make(map[string]bool)
	var pool []*net.IPAddr

	addIP := func(ip net.IP) {
		var key string
		if v4 := ip.To4(); v4 != nil {
			key = "v4:" + v4.String()
		} else {
			key = "v6:" + ip.String()
		}
		if !seen[key] {
			seen[key] = true
			pool = append(pool, &net.IPAddr{IP: ip})
		}
	}

	if opts.IncludeV4 {
		for _, cidr := range allV4Ranges() {
			ips, err := expandOrSampleV4(cidr, maxExpandPerRange)
			if err != nil {
				return nil, fmt.Errorf("expand %s: %w", cidr, err)
			}
			for _, ip := range ips {
				addIP(ip)
			}
		}
	}

	if opts.IncludeV6 {
		for _, cidr := range allV6Ranges() {
			ips, err := sampleV6(cidr, opts.V6Sample)
			if err != nil {
				return nil, fmt.Errorf("sample %s: %w", cidr, err)
			}
			for _, ip := range ips {
				addIP(ip)
			}
		}
	}

	// 随机化顺序：避免每次测速都按段顺序，遇到风控时分散
	rand.Shuffle(len(pool), func(i, j int) { pool[i], pool[j] = pool[j], pool[i] })

	return pool, nil
}

// expandOrSampleV4 展开 IPv4 段：段小时全展开，段大时采样。
func expandOrSampleV4(cidr string, maxExpand int) ([]net.IP, error) {
	_, ipnet, err := net.ParseCIDR(cidr)
	if err != nil {
		return nil, err
	}

	// 计算段内 IP 数
	ones, bits := ipnet.Mask.Size()
	if bits != 32 {
		return nil, fmt.Errorf("not IPv4: %s", cidr)
	}
	size := 1 << uint(bits-ones)

	if size <= maxExpand {
		// 全展开
		return iterateV4(ipnet), nil
	}
	// 采样
	return sampleV4(ipnet, maxExpand), nil
}

// iterateV4 遍历段内所有 IP（含网络地址和广播地址，跟 XIU2 的 chooseIPv4 一致）。
func iterateV4(ipnet *net.IPNet) []net.IP {
	var ips []net.IP
	ip := ipnet.IP.To4()
	for ip := ip.Mask(ipnet.Mask); ipnet.Contains(ip); incV4(ip) {
		out := make(net.IP, 4)
		copy(out, ip)
		ips = append(ips, out)
	}
	return ips
}

// incV4 IP + 1（原地修改，仅 IPv4）。
func incV4(ip net.IP) {
	for i := len(ip) - 1; i >= 0; i-- {
		ip[i]++
		if ip[i] != 0 {
			return
		}
	}
}

// sampleV4 从段内随机抽 n 个不重复的 IP。
func sampleV4(ipnet *net.IPNet, n int) []net.IP {
	ones, bits := ipnet.Mask.Size()
	size := 1 << uint(bits-ones)
	if n > size {
		n = size
	}

	// 生成 n 个不重复的偏移量
	offsets := rand.Perm(size)[:n]
	base := ipnet.IP.To4().Mask(ipnet.Mask)

	ips := make([]net.IP, 0, n)
	for _, off := range offsets {
		ip := make(net.IP, 4)
		copy(ip, base)
		// 加上偏移量
		carry := uint32(off)
		for i := 3; i >= 0; i-- {
			ip[i] += byte(carry & 0xFF)
			carry >>= 8
			if carry == 0 {
				break
			}
		}
		ips = append(ips, ip)
	}
	return ips
}

// sampleV6 从 IPv6 段随机抽 n 个 IP。
// 直接在末 4 字节随机，最后一段（/128）只取 N 个具体值。
func sampleV6(cidr string, n int) ([]net.IP, error) {
	_, ipnet, err := net.ParseCIDR(cidr)
	if err != nil {
		return nil, err
	}

	// 取段内最低 IP 作为基址
	base := ipnet.IP.To16()
	if base == nil {
		return nil, fmt.Errorf("not IPv6: %s", cidr)
	}

	ips := make([]net.IP, 0, n)
	for i := 0; i < n; i++ {
		ip := make(net.IP, 16)
		copy(ip, base)
		// 末 4 字节随机
		binary.BigEndian.PutUint32(ip[12:], rand.Uint32())
		ips = append(ips, ip)
	}
	return ips, nil
}

// String 辅助调试：把 CIDR 列表打印成易读格式
func summarizeCIDRs(cidrs []string) string {
	if len(cidrs) == 0 {
		return "[]"
	}
	if len(cidrs) <= 5 {
		return strings.Join(cidrs, ", ")
	}
	return fmt.Sprintf("%s, ... (%d total)", strings.Join(cidrs[:5], ", "), len(cidrs))
}
