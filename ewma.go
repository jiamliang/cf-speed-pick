// Copyright (C) 2026 cf-speed-pick contributors
// SPDX-License-Identifier: GPL-3.0-or-later
//
// 自写的指数加权移动平均 (EWMA)，用于平滑下载测速瞬时速率波动。
// 替代 XIU2/CloudflareSpeedTest 使用的 github.com/VividCortex/ewma 外部依赖。
//
// 算法说明：
//   第一次 Add：直接用 sample 作为初值（避免 0 值干扰）
//   之后：value = α·sample + (1-α)·value
//   α 越大越敏感（接近瞬时值），越小越平滑（接近历史值）
//
// 参考 VIU2 实现中的 α ≈ 0.3，对应"10 个采样点左右的窗口"。

package main

// MovingAverage 指数加权移动平均。
// 零值可直接使用：首次 Add 时用 sample 作初值。
type MovingAverage struct {
	value float64
	alpha float64 // 平滑系数，范围 (0, 1]
	set   bool    // 是否收到过第一个样本
}

// NewMovingAverage 默认 α=0.3（与 XIU2 实现接近）。
func NewMovingAverage() *MovingAverage {
	return &MovingAverage{alpha: 0.3}
}

// NewMovingAverageWith 自定义 α。
func NewMovingAverageWith(alpha float64) *MovingAverage {
	if alpha <= 0 || alpha > 1 {
		alpha = 0.3
	}
	return &MovingAverage{alpha: alpha}
}

// Add 推入一个新采样点。
func (m *MovingAverage) Add(sample float64) {
	if !m.set {
		m.value = sample
		m.set = true
		return
	}
	m.value = m.alpha*sample + (1-m.alpha)*m.value
}

// Value 返回当前平滑后的值。
// 未收到任何采样时返回 0。
func (m *MovingAverage) Value() float64 {
	return m.value
}

// Reset 重置状态，可以重新开始一段统计。
func (m *MovingAverage) Reset() {
	m.value = 0
	m.set = false
}
