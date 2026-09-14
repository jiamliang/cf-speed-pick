// Copyright (C) 2026 cf-speed-pick contributors
// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"net"
	"testing"
	"time"
)

func mkPing(ip string, delay time.Duration, colo string) PingData {
	return PingData{
		IP:       &net.IPAddr{IP: net.ParseIP(ip)},
		Sended:   4, Received: 4,
		Delay: delay,
		Colo:  colo,
	}
}

func TestBuildColoQueue_DefaultTierOrder(t *testing.T) {
	input := PingDelaySet{
		mkPing("1.1.1.1", 50*time.Millisecond, "NRT"),  // Tier 1
		mkPing("2.2.2.2", 60*time.Millisecond, "SIN"),  // Tier 2
		mkPing("3.3.3.3", 70*time.Millisecond, "LAX"),  // Tier 3
		mkPing("4.4.4.4", 80*time.Millisecond, "FRA"),  // Other
	}
	q := buildColoQueue(input, nil, TierStrategyAll)
	if len(q) != 4 {
		t.Fatalf("want 4 buckets, got %d", len(q))
	}
	// 顺序：NRT (T1) → SIN (T2) → LAX (T3) → FRA (Other)
	if q[0].Colo != "NRT" || q[1].Colo != "SIN" || q[2].Colo != "LAX" || q[3].Colo != "OTHER" {
		t.Errorf("bucket order: %v", []string{q[0].Colo, q[1].Colo, q[2].Colo, q[3].Colo})
	}
	if q[0].Tier != 1 || q[1].Tier != 2 || q[2].Tier != 3 || q[3].Tier != 99 {
		t.Errorf("tier values wrong: %d %d %d %d", q[0].Tier, q[1].Tier, q[2].Tier, q[3].Tier)
	}
}

func TestBuildColoQueue_TieredSampling(t *testing.T) {
	// Tier 1 (NRT): 100%
	// Tier 2 (SIN): 50%
	// Tier 3 (LAX): 30%
	// Other (FRA): 10%
	var input PingDelaySet
	for i := 0; i < 10; i++ {
		input = append(input, mkPing("1.1.1.1", time.Duration(50+i)*time.Millisecond, "NRT"))
	}
	for i := 0; i < 10; i++ {
		input = append(input, mkPing("2.2.2.2", time.Duration(60+i)*time.Millisecond, "SIN"))
	}
	for i := 0; i < 10; i++ {
		input = append(input, mkPing("3.3.3.3", time.Duration(70+i)*time.Millisecond, "LAX"))
	}
	for i := 0; i < 10; i++ {
		input = append(input, mkPing("4.4.4.4", time.Duration(80+i)*time.Millisecond, "FRA"))
	}

	q := buildColoQueue(input, nil, TierStrategyTiered)

	got := map[string]int{}
	for _, b := range q {
		got[b.Colo] = len(b.IPs)
	}
	if got["NRT"] != 10 {
		t.Errorf("Tier 1: want 10, got %d", got["NRT"])
	}
	if got["SIN"] != 5 {
		t.Errorf("Tier 2: want 5, got %d", got["SIN"])
	}
	if got["LAX"] != 3 {
		t.Errorf("Tier 3: want 3, got %d", got["LAX"])
	}
	if got["OTHER"] != 1 {
		t.Errorf("Other: want 1, got %d", got["OTHER"])
	}
}

func TestBuildColoQueue_OnlySkipsOther(t *testing.T) {
	input := PingDelaySet{
		mkPing("1.1.1.1", 50*time.Millisecond, "NRT"),
		mkPing("2.2.2.2", 60*time.Millisecond, "FRA"), // 不在优先级里
	}
	q := buildColoQueue(input, []string{"NRT"}, TierStrategyOnly)
	if len(q) != 1 {
		t.Fatalf("Only strategy should skip non-priority: got %d buckets", len(q))
	}
	if q[0].Colo != "NRT" {
		t.Errorf("expected only NRT, got %s", q[0].Colo)
	}
}

func TestBuildColoQueue_CaseInsensitive(t *testing.T) {
	input := PingDelaySet{
		mkPing("1.1.1.1", 50*time.Millisecond, "nrt"), // 小写
		mkPing("2.2.2.2", 60*time.Millisecond, "Sin"), // 混合大小写
	}
	q := buildColoQueue(input, nil, TierStrategyAll)
	if len(q) != 2 {
		t.Errorf("case insensitivity broken: got %d buckets", len(q))
	}
	// 输出应该都是大写
	for _, b := range q {
		if b.Colo != "NRT" && b.Colo != "SIN" {
			t.Errorf("colo not uppercased: %s", b.Colo)
		}
	}
}

func TestBuildColoQueue_SortByDelay(t *testing.T) {
	// 桶内应该按延迟升序
	input := PingDelaySet{
		mkPing("1.1.1.1", 100*time.Millisecond, "NRT"),
		mkPing("2.2.2.2", 50*time.Millisecond, "NRT"),
		mkPing("3.3.3.3", 75*time.Millisecond, "NRT"),
	}
	q := buildColoQueue(input, nil, TierStrategyAll)
	if len(q) != 1 {
		t.Fatal()
	}
	if q[0].IPs[0].Delay != 50*time.Millisecond {
		t.Errorf("first should be 50ms, got %v", q[0].IPs[0].Delay)
	}
	if q[0].IPs[2].Delay != 100*time.Millisecond {
		t.Errorf("last should be 100ms, got %v", q[0].IPs[2].Delay)
	}
}

func TestParseTierStrategy(t *testing.T) {
	cases := []struct {
		in   string
		want TierStrategy
	}{
		{"", TierStrategyTiered},
		{"tiered", TierStrategyTiered},
		{"TIERED", TierStrategyTiered},
		{"only", TierStrategyOnly},
		{"all", TierStrategyAll},
		{"ALL", TierStrategyAll},
	}
	for _, c := range cases {
		if got := ParseTierStrategy(c.in); got != c.want {
			t.Errorf("ParseTierStrategy(%q) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestSplitCSV(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"NRT", []string{"NRT"}},
		{"NRT,KIX,ICN", []string{"NRT", "KIX", "ICN"}},
		{" NRT , KIX ", []string{"NRT", "KIX"}},
		{",NRT,,KIX,", []string{"NRT", "KIX"}},
	}
	for _, c := range cases {
		got := splitCSV(c.in)
		if len(got) != len(c.want) {
			t.Errorf("splitCSV(%q): want %v, got %v", c.in, c.want, got)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("splitCSV(%q)[%d]: want %q, got %q", c.in, i, c.want[i], got[i])
			}
		}
	}
}

func TestTierSampleSize(t *testing.T) {
	cases := []struct {
		tier, total, want int
	}{
		{1, 10, 10},  // 100%
		{1, 100, 100},
		{2, 10, 5},   // 50%
		{2, 100, 50},
		{3, 10, 3},   // 30%
		{3, 100, 30},
		{99, 10, 1},  // 10%, 但最少 1
		{99, 100, 10},
		{1, 0, 0},    // 边界：total=0 → 0
	}
	for _, c := range cases {
		got := tierSampleSize(c.tier, c.total)
		if got != c.want {
			t.Errorf("tierSampleSize(%d, %d) = %d, want %d", c.tier, c.total, got, c.want)
		}
	}
}