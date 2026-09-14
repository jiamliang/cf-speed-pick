// Copyright (C) 2026 cf-speed-pick contributors
// SPDX-License-Identifier: GPL-3.0-or-later
//
// Colo 地理优先级：按 colo（CF 机场三字码）分桶，给物理距离近的更高优先级。
//
// 为什么需要这个？
//   - 国内用户到亚洲节点的物理延迟远低于欧美节点（光纤速度 ~200km/ms）
//   - 但 Layer 3 下载测速是单线程顺序跑，全测太慢
//   - 折中方案：Layer 1/2 全测（探路），Layer 3 按 colo 优先级**采样**
//
// 设计：默认分 3 个档
//   - Tier 1（最近）：全测（NRT, KIX, ICN, TPE, HKG, MFM）
//   - Tier 2（次近）：测 Top 50%（SIN, BKK, SGN, MNL, KUL）
//   - Tier 3（兜底）：测 Top 10%（LAX, SJC, SEA）
//   - 其他：测 Top 5%
//
// 用户也可以用 -colo-priority 自定义优先级列表（逗号分隔 IATA 码）。

package main

import (
	"fmt"
	"strings"
)

// defaultColoPriority 默认 colo 优先级（按地理距离升序）。
// 这是面向中国电信用户的合理默认值；其他运营商可自己改。
var defaultColoPriority = []string{
	// Tier 1: 东北亚（最近，< 2000km）
	"NRT", "ICN", "KIX", "TPE", "HKG", "MFM", "FUK",
	// Tier 2: 东南亚（次近，3000-5000km）
	"SIN", "BKK", "SGN", "MNL", "KUL", "CGK",
	// Tier 3: 美西（CN2 精品线路可能很快）
	"LAX", "SJC", "SEA", "PDX", "DEN",
}

// TierStrategy 采样策略。
type TierStrategy int

const (
	// TierStrategyAll 全测（不分层，所有 IP 都跑 Layer 3）。
	TierStrategyAll TierStrategy = iota
	// TierStrategyOnly 只测指定 colo（不在优先级列表里的直接跳过）。
	TierStrategyOnly
	// TierStrategyTiered 分层采样（默认）。
	TierStrategyTiered
)

// ParseTierStrategy 解析 -colo-strategy 参数。
func ParseTierStrategy(s string) TierStrategy {
	switch strings.ToLower(s) {
	case "all":
		return TierStrategyAll
	case "only":
		return TierStrategyOnly
	default:
		return TierStrategyTiered
	}
}

// coloBucket 按 colo 把 Layer 2 结果分桶。
// 返回的 slice 按 colo 优先级排序（Tier 1 在前）。
type coloBucket struct {
	Colo  string
	Tier  int // 1/2/3/4（数字越小优先级越高）
	IPs   PingDelaySet
}

// buildColoQueue 把 Layer 2 结果分桶、按优先级排序，并按比例采样。
//   - strategy: All/Only/Tiered
//   - priority: 用户指定的 colo 优先级列表（空则用默认）
// 返回：分桶后排序好的 IP 列表，每桶顶部放 [tier, total, sampled]
func buildColoQueue(input PingDelaySet, priority []string, strategy TierStrategy) []coloBucket {
	if len(priority) == 0 {
		priority = defaultColoPriority
	}

	// 建立 colo → tier 映射
	coloTier := make(map[string]int)
	for i, c := range priority {
		coloTier[strings.ToUpper(c)] = (i / 6) + 1 // 每 6 个一组 = 1 档
	}

	// 分桶
	buckets := make(map[string]*coloBucket)
	var otherBucket *coloBucket
	for _, p := range input {
		c := strings.ToUpper(p.Colo)
		tier, ok := coloTier[c]
		if !ok {
			tier = 99 // 其他
		}
		if tier == 99 {
			if otherBucket == nil {
				otherBucket = &coloBucket{Colo: "OTHER", Tier: 99}
			}
			otherBucket.IPs = append(otherBucket.IPs, p)
			continue
		}
		b, exists := buckets[c]
		if !exists {
			b = &coloBucket{Colo: c, Tier: tier}
			buckets[c] = b
		}
		b.IPs = append(b.IPs, p)
	}

	// 桶内按 Layer 2 延迟排序
	for _, b := range buckets {
		b.IPs.Sort()
	}
	if otherBucket != nil {
		otherBucket.IPs.Sort()
	}

	// 按 tier 排序桶
	var ordered []*coloBucket
	for i := 1; i <= 3; i++ {
		for _, c := range priority {
			if b, ok := buckets[strings.ToUpper(c)]; ok && b.Tier == i {
				ordered = append(ordered, b)
			}
		}
	}
	if otherBucket != nil {
		ordered = append(ordered, otherBucket)
	}

	// 采样
	out := make([]coloBucket, 0, len(ordered))
	for _, b := range ordered {
		var sampleSize int
		switch strategy {
		case TierStrategyAll:
			sampleSize = len(b.IPs)
		case TierStrategyOnly:
			if b.Tier == 99 {
				continue // only 模式跳过 other
			}
			sampleSize = len(b.IPs)
		default: // Tiered
			sampleSize = tierSampleSize(b.Tier, len(b.IPs))
		}
		if sampleSize < len(b.IPs) {
			b.IPs = b.IPs[:sampleSize]
		}
		out = append(out, *b)
	}
	return out
}

// tierSampleSize 分层采样比例：
//   - Tier 1: 100%
//   - Tier 2: 50%
//   - Tier 3: 30%
//   - Other: 10%
// 最少 1 个，最多 IP 数。
func tierSampleSize(tier, total int) int {
	var ratio float64
	switch tier {
	case 1:
		ratio = 1.0
	case 2:
		ratio = 0.5
	case 3:
		ratio = 0.3
	default:
		ratio = 0.1
	}
	n := int(float64(total) * ratio)
	if n < 1 {
		n = 1
	}
	if n > total {
		n = total
	}
	return n
}

// printColoQueue 打印分桶结果。
func printColoQueue(queue []coloBucket, strategy TierStrategy) {
	if len(queue) == 0 {
		return
	}
	fmt.Println("\n[Layer 3] Colo 分桶（按地理距离优先级）：")
	totals := map[int]int{}
	for _, b := range queue {
		fmt.Printf("  Tier %d  %-5s  %d IP\n", b.Tier, b.Colo, len(b.IPs))
		totals[b.Tier] += len(b.IPs)
	}
	if strategy == TierStrategyTiered {
		fmt.Println("  策略：tiered（Tier1=100% Tier2=50% Tier3=30% Other=10%）")
	} else if strategy == TierStrategyOnly {
		fmt.Println("  策略：only（跳过非优先级 colo）")
	} else {
		fmt.Println("  策略：all（全部等权）")
	}
}