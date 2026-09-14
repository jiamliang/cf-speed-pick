// Copyright (C) 2026 cf-speed-pick contributors
// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"net"
	"testing"
)

func TestExpandV4_SmallRange(t *testing.T) {
	// /24 应该全展开 = 256 个 IP
	ips, err := expandOrSampleV4("104.16.0.0/24", maxExpandPerRange)
	if err != nil {
		t.Fatal(err)
	}
	if len(ips) != 256 {
		t.Errorf("/24: want 256 IPs, got %d", len(ips))
	}
	// 首尾验证
	if !ips[0].Equal(net.ParseIP("104.16.0.0")) {
		t.Errorf("first: want 104.16.0.0, got %v", ips[0])
	}
	if !ips[255].Equal(net.ParseIP("104.16.0.255")) {
		t.Errorf("last: want 104.16.0.255, got %v", ips[255])
	}
}

func TestExpandV4_LargeRange_Samples(t *testing.T) {
	// /12 太大（1M+），应该被采样
	ips, err := expandOrSampleV4("104.16.0.0/12", maxExpandPerRange)
	if err != nil {
		t.Fatal(err)
	}
	if len(ips) != maxExpandPerRange {
		t.Errorf("/12: want %d (sampled), got %d", maxExpandPerRange, len(ips))
	}
	// 验证都在段内
	_, ipnet, _ := net.ParseCIDR("104.16.0.0/12")
	for _, ip := range ips {
		if !ipnet.Contains(ip) {
			t.Errorf("IP %v out of range", ip)
		}
	}
}

func TestExpandV4_Dedup(t *testing.T) {
	pool, err := BuildIPPool(PoolOptions{IncludeV4: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("IPv4 pool size: %d", len(pool))
	if len(pool) < 10000 {
		t.Errorf("pool too small: %d (expected 10000+)", len(pool))
	}
	// 验证去重
	seen := make(map[string]bool)
	for _, ip := range pool {
		k := ip.IP.String()
		if seen[k] {
			t.Errorf("duplicate IP: %s", k)
		}
		seen[k] = true
	}
}

func TestSampleV6(t *testing.T) {
	ips, err := sampleV6("2400:cb00::/32", 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(ips) != 100 {
		t.Errorf("want 100 IPv6, got %d", len(ips))
	}
	for _, ip := range ips {
		v6 := ip.To16()
		if v6 == nil {
			t.Errorf("not IPv6: %v", ip)
			continue
		}
		// 前 4 字节应该是 2400:cb00 (2400:cb00::/32 的网络前缀)
		if v6[0] != 0x24 || v6[1] != 0x00 || v6[2] != 0xcb || v6[3] != 0x00 {
			t.Errorf("wrong prefix: %v", ip)
		}
	}
}

func TestBuildPool_IPv6Only(t *testing.T) {
	pool, err := BuildIPPool(PoolOptions{IncludeV4: false, IncludeV6: true, V6Sample: 50})
	if err != nil {
		t.Fatal(err)
	}
	if len(pool) != 50*len(allV6Ranges()) {
		t.Errorf("want %d, got %d", 50*len(allV6Ranges()), len(pool))
	}
}
