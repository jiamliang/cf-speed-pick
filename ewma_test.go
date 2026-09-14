// Copyright (C) 2026 cf-speed-pick contributors
// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"math"
	"testing"
)

func TestMovingAverage_FirstSample(t *testing.T) {
	m := NewMovingAverage()
	m.Add(100)
	if got := m.Value(); got != 100 {
		t.Errorf("first sample: want 100, got %v", got)
	}
}

func TestMovingAverage_Smoothing(t *testing.T) {
	m := NewMovingAverageWith(0.3)
	// 推入 100, 200, 200, 200...
	// 第一次：100
	// 第二次：0.3*200 + 0.7*100 = 130
	// 第三次：0.3*200 + 0.7*130 = 151
	m.Add(100)
	m.Add(200)
	if got := m.Value(); math.Abs(got-130) > 0.01 {
		t.Errorf("after 2 samples: want 130, got %v", got)
	}
	m.Add(200)
	if got := m.Value(); math.Abs(got-151) > 0.01 {
		t.Errorf("after 3 samples: want 151, got %v", got)
	}
}

func TestMovingAverage_Reset(t *testing.T) {
	m := NewMovingAverage()
	m.Add(100)
	m.Reset()
	if got := m.Value(); got != 0 {
		t.Errorf("after reset: want 0, got %v", got)
	}
	m.Add(50)
	if got := m.Value(); got != 50 {
		t.Errorf("first sample after reset: want 50, got %v", got)
	}
}

func TestMovingAverage_Converges(t *testing.T) {
	// α=0.5 时，连续推入相同值 10 次后应非常接近该值
	m := NewMovingAverageWith(0.5)
	for i := 0; i < 10; i++ {
		m.Add(1000)
	}
	if got := m.Value(); math.Abs(got-1000) > 0.01 {
		t.Errorf("converge: want 1000, got %v", got)
	}
}

func TestMovingAverage_InvalidAlpha(t *testing.T) {
	// α ≤ 0 或 α > 1 应该被规整成默认值 0.3
	m := NewMovingAverageWith(0)
	m.Add(100)
	m.Add(200)
	// 应该用 α=0.3 平滑：0.3*200 + 0.7*100 = 130
	if got := m.Value(); math.Abs(got-130) > 0.01 {
		t.Errorf("invalid alpha should fall back to 0.3: want 130, got %v", got)
	}
}
