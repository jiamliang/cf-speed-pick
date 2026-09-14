// Copyright (C) 2026 cf-speed-pick contributors
// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"testing"
	"time"
)

func TestDownloadParams_Defaults(t *testing.T) {
	p := DownloadParams{}
	p.defaults()
	if p.Port != 443 {
		t.Errorf("default port: want 443, got %d", p.Port)
	}
	if p.Timeout != 10*time.Second {
		t.Errorf("default timeout: want 10s, got %v", p.Timeout)
	}
	if p.BufferSize != 4096 {
		t.Errorf("default buffer: want 4096, got %d", p.BufferSize)
	}
	if p.MaxResults != 10 {
		t.Errorf("default max results: want 10, got %d", p.MaxResults)
	}
	if p.URL == "" {
		t.Errorf("URL should have default")
	}
}

// TestEWMAConversion 验证 EWMA → MB/s 的换算逻辑。
// 假设 EWMA 在 timeSlice 内累计 40960 字节（≈40KB），
// timeSlice = 10s / 100 = 0.1s，
// 那么字节/秒 = 40960 / 0.1 = 409600 字节/秒 = 0.390625 MB/s。
func TestEWMAConversion(t *testing.T) {
	ewma := NewMovingAverage()
	// 喂 100 次 4096 字节（每个 timeSlice 10KB 一致输入）
	for i := 0; i < 100; i++ {
		ewma.Add(40960) // bytes per timeSlice
	}
	timeSlice := 10 * time.Second / 100
	bytesPerSec := ewma.Value() / timeSlice.Seconds()
	mbPerSec := bytesPerSec / (1024.0 * 1024.0)
	// 期望：40960 / 0.1 = 409600 B/s = 0.390625 MB/s
	want := 0.390625
	if mbPerSec < want*0.99 || mbPerSec > want*1.01 {
		t.Errorf("conversion: want ~%f MB/s, got %f", want, mbPerSec)
	}
}

func TestRunDownload_Empty(t *testing.T) {
	// 空输入应该返回 nil（不需要网络）
	res := RunDownload(nil, DownloadParams{})
	if res != nil {
		t.Errorf("empty input: want nil, got %v", res)
	}
}