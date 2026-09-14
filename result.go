// Copyright (C) 2026 cf-speed-pick contributors
// SPDX-License-Identifier: GPL-3.0-or-later
//
// 三层漏斗之间的统一结果类型。

package main

import (
	"net"
	"sort"
	"time"
)

// PingData 单个 IP 的探测结果。
//
// 在 Layer 1 / Layer 2 都用这个结构（Layer 2 增加 Colo 字段）。
type PingData struct {
	IP       *net.IPAddr
	Sended   int           // 发送次数
	Received int           // 收到响应次数
	Delay    time.Duration // 平均延迟（成功才计算）
	Colo     string        // cf-ray 提取的 IATA 码（仅 Layer 2+ 有）
}

// LossRate 丢包率 [0,1]。
func (p *PingData) LossRate() float64 {
	if p.Sended == 0 {
		return 1
	}
	return 1.0 - float64(p.Received)/float64(p.Sended)
}

// PingDelaySet PingData 切片，按延迟升序。
type PingDelaySet []PingData

func (s PingDelaySet) Len() int           { return len(s) }
func (s PingDelaySet) Less(i, j int) bool { return s[i].Delay < s[j].Delay }
func (s PingDelaySet) Swap(i, j int)      { s[i], s[j] = s[j], s[i] }

func (s PingDelaySet) Sort() { sort.Sort(s) }

// FilterLoss 过滤掉丢包率过高的 IP。
func (s PingDelaySet) FilterLoss(maxLoss float64) PingDelaySet {
	out := make(PingDelaySet, 0, len(s))
	for _, p := range s {
		if p.LossRate() <= maxLoss {
			out = append(out, p)
		}
	}
	return out
}

// FilterDelay 过滤掉平均延迟过高的 IP（Delay=0 视为不可达）。
func (s PingDelaySet) FilterDelay(maxDelay time.Duration) PingDelaySet {
	out := make(PingDelaySet, 0, len(s))
	for _, p := range s {
		if p.Delay > 0 && p.Delay <= maxDelay {
			out = append(out, p)
		}
	}
	return out
}