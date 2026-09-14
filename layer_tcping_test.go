// Copyright (C) 2026 cf-speed-pick contributors
// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"net"
	"testing"
	"time"
)

func TestTCPingProbe_KnownGood(t *testing.T) {
	// 1.1.1.1 是 CF 公开 DNS，必然 443 通；其他网络环境下应该能稳定连通。
	// 这个测试如果失败说明网络环境异常或被防火墙拦截。
	if testing.Short() {
		t.Skip("skip network test in -short mode")
	}
	ip := &net.IPAddr{IP: net.ParseIP("1.1.1.1")}
	params := TCPingParams{
		Routines:  1,
		Port:      443,
		PingTimes: 2,
		Timeout:   2 * time.Second,
	}
	data := tcpingProbe(ip, params)
	t.Logf("1.1.1.1:443 → recv=%d/%d delay=%v", data.Received, data.Sended, data.Delay)
	if data.Received == 0 {
		t.Skip("无法连通 1.1.1.1:443（可能沙盒环境无网络）")
	}
	if data.Delay <= 0 {
		t.Errorf("delay should be > 0, got %v", data.Delay)
	}
}

func TestTCPingProbe_KnownBad(t *testing.T) {
	// 192.0.2.0/24 是 TEST-NET-1，IANA 保留，肯定 443 不通。
	ip := &net.IPAddr{IP: net.ParseIP("192.0.2.1")}
	params := TCPingParams{
		Port:      443,
		PingTimes: 1,
		Timeout:   500 * time.Millisecond,
	}
	data := tcpingProbe(ip, params)
	if data.Received != 0 {
		t.Errorf("192.0.2.1 should be unreachable, got recv=%d", data.Received)
	}
}

func TestTCPingParams_Defaults(t *testing.T) {
	p := TCPingParams{}
	p.defaults()
	if p.Routines != 200 {
		t.Errorf("default routines: want 200, got %d", p.Routines)
	}
	if p.Port != 443 {
		t.Errorf("default port: want 443, got %d", p.Port)
	}
	if p.PingTimes != 4 {
		t.Errorf("default ping times: want 4, got %d", p.PingTimes)
	}
	if p.Timeout != time.Second {
		t.Errorf("default timeout: want 1s, got %v", p.Timeout)
	}
}

func TestPingData_LossRate(t *testing.T) {
	d := PingData{Sended: 10, Received: 7}
	got := d.LossRate()
	if got < 0.299 || got > 0.301 {
		t.Errorf("loss: want ~0.3, got %f", got)
	}
	d = PingData{Sended: 0}
	if got := d.LossRate(); got != 1.0 {
		t.Errorf("zero sended: want 1.0, got %f", got)
	}
}

func TestPingDelaySet_Filter(t *testing.T) {
	d0 := PingData{IP: &net.IPAddr{IP: net.ParseIP("1.1.1.1")}, Sended: 4, Received: 4, Delay: 50 * time.Millisecond}
	d1 := PingData{IP: &net.IPAddr{IP: net.ParseIP("2.2.2.2")}, Sended: 4, Received: 1, Delay: 10 * time.Millisecond}
	d2 := PingData{IP: &net.IPAddr{IP: net.ParseIP("3.3.3.3")}, Sended: 4, Received: 4, Delay: 500 * time.Millisecond}
	s := PingDelaySet{d0, d1, d2}

	// 过滤丢包率 > 0.2 → 只剩 d0 和 d2
	got := s.FilterLoss(0.2)
	if len(got) != 2 {
		t.Errorf("FilterLoss: want 2, got %d", len(got))
	}
	// 过滤延迟 > 200ms → 只剩 d0 和 d1
	got = s.FilterDelay(200 * time.Millisecond)
	if len(got) != 2 {
		t.Errorf("FilterDelay: want 2, got %d", len(got))
	}
}