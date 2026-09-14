// Copyright (C) 2026 cf-speed-pick contributors
// SPDX-License-Identifier: GPL-3.0-or-later
//
// 极简进度条（不依赖 XIU2 utils.Bar）。
//
// 用法：
//
//	bar := NewBar(total, prefix)
//	for bar.Next() {
//	    // 干活
//	}
//	bar.Finish()
//
// 渲染格式（覆盖同一行）：
//	前缀 123/456 (27%) [成功: 234]
//
// 线程不安全：单 goroutine 调用 Next() 即可。

package main

import (
	"fmt"
	"strings"
)

type Bar struct {
	total   int
	cur     int
	ok      int
	prefix  string
	width   int
	enabled bool
}

// NewBar total<=0 时返回禁用实例（Next/Finish 都为空操作）。
func NewBar(total int, prefix string) *Bar {
	return &Bar{total: total, prefix: prefix, width: 30, enabled: total > 0}
}

func (b *Bar) Tick() {
	b.cur++
}

func (b *Bar) MarkOK() {
	b.ok++
}

func (b *Bar) Render() {
	if !b.enabled {
		return
	}
	// 计算填充宽度
	pct := 0
	if b.total > 0 {
		pct = b.cur * 100 / b.total
	}
	fill := b.width * b.cur / b.total
	if fill > b.width {
		fill = b.width
	}
	bar := strings.Repeat("█", fill) + strings.Repeat("░", b.width-fill)
	fmt.Printf("\r%s [%s] %d/%d (%d%%) ✓%d   ",
		b.prefix, bar, b.cur, b.total, pct, b.ok)
}

func (b *Bar) Finish() {
	if !b.enabled {
		return
	}
	b.Render()
	fmt.Println()
}