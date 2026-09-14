// Copyright (C) 2026 cf-speed-pick contributors
// SPDX-License-Identifier: GPL-3.0-or-later
//
// cf-speed-pick: 国内→Cloudflare 优选 IP 工具
// 三阶段漏斗：TCPing → HTTPing → 下载测速
//
// 用法：
//   cf-speed-pick                              # 默认跑完整三层
//   cf-speed-pick -h                           # 帮助
//   cf-speed-pick -n 50 -t 2                   # 路由器低配
//   cf-speed-pick -ipv6 -ipv6-sample 500       # IPv4 + IPv6
//   cf-speed-pick -skip-download               # 只跑到 Layer 2
//   cf-speed-pick -colo HKG,NRT,SJC            # 只保留亚洲节点
//   cf-speed-pick -url https://your.domain/file
//
// cf-speed-pick is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.

package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const version = "0.1.0"

// CLI 参数（全部通过 flag 包解析）
var (
	flagOut           = flag.String("out", "./out", "输出目录")
	flagDebug         = flag.Bool("debug", false, "输出中间结果（Layer 1/2 CSV）")
	flagIncludeV6     = flag.Bool("ipv6", false, "启用 IPv6 测速（默认只 IPv4）")
	flagV6Sample      = flag.Int("ipv6-sample", 1000, "IPv6 每段采样数（默认 1000）")
	flagTCPRoutines   = flag.Int("n", 200, "TCPing 并发数（路由器建议 50）")
	flagTCPTimes      = flag.Int("t", 4, "TCPing 每个 IP 测几次")
	flagTCPMaxDelayMS = flag.Int("max-delay", 200, "Layer 1/2 最大平均延迟（ms）")
	flagTCPMaxLoss    = flag.Float64("max-loss", 0.2, "Layer 1/2 最大丢包率 [0,1]")
	flagHTTPURL       = flag.String("url", "https://cf.xiu2.xyz/url", "HTTPing + 下载测速的 URL（必须是 CF-fronted 域名）")
	flagHTTPColo      = flag.String("colo", "", "只保留指定 IATA 码（逗号分隔，如 HKG,NRT,SJC）")
	flagHTTPStatus    = flag.Int("status", 0, "期望的 HTTP 状态码（0=接受 200/301/302）")
	flagDownloadTime  = flag.Duration("dt", 10*time.Second, "每个 IP 下载测速时长")
	flagDownloadTopN  = flag.Int("dn", 10, "最终输出 Top N")
	flagDownloadMinSp = flag.Float64("min-speed", 0.05, "下载速度下限（MB/s，低于此值丢弃）")
	flagSkipDownload  = flag.Bool("skip-download", false, "跳过 Layer 3，只输出 Layer 2 结果")
	flagPort          = flag.Int("tp", 443, "TCP 端口")
	flagShowVersion   = flag.Bool("version", false, "打印版本")
	flagColoPriority  = flag.String("colo-priority", "", "colo 优先级列表（逗号分隔 IATA 码，空=默认 NRT,ICN,KIX,TPE,HKG,MFM,...）")
	flagColoStrategy  = flag.String("colo-strategy", "tiered", "Layer 3 采样策略：tiered（分层）/ only（只测优先级）/ all（全平等）")
	flagInputCSV      = flag.String("input", "", "从 CSV 加载候选 IP（跳过 IP 池构造 + Layer 1）；列：ip,delay_ms,colo")
	flagOutputColos   = flag.Bool("output-colos", false, "按 colo 分桶输出多个 CSV（VPS 端用，给路由器订阅）")
	flagSkipHttping   = flag.Bool("skip-httping", false, "跳过 Layer 2；要求输入已有 colo 字段（如 -input 02_httping.csv）")
)

func main() {
	// 第一件事：检查代理。测速必须在干净的网络环境下跑。
	if err := checkNoProxy(); err != nil {
		fmt.Fprintf(os.Stderr, "❌ %v\n\n", err)
		os.Exit(2)
	}

	flag.Usage = usage
	flag.Parse()

	if *flagShowVersion {
		fmt.Printf("cf-speed-pick v%s\n", version)
		return
	}

	fmt.Printf("cf-speed-pick v%s\n", version)
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	if err := EnsureOutDir(*flagOut); err != nil {
		return fmt.Errorf("create out dir: %w", err)
	}

	var layer1Result PingDelaySet
	var err error

	if *flagInputCSV != "" {
		// 模式 A：从 CSV 加载候选 IP，跳过 IP 池构造 + Layer 1
		fmt.Printf("\n[准备] 从 CSV 加载候选 IP：%s\n", *flagInputCSV)
		layer1Result, err = ReadPingCSV(*flagInputCSV)
		if err != nil {
			return fmt.Errorf("load input csv: %w", err)
		}
		fmt.Printf("[准备] 加载 %d IP\n", len(layer1Result))
	} else {
		// 模式 B：标准流程，从 IP 池开始
		fmt.Println("\n[准备] 构造 IP 池...")
		pool, err := BuildIPPool(PoolOptions{
			IncludeV4: true,
			IncludeV6: *flagIncludeV6,
			V6Sample:  *flagV6Sample,
		})
		if err != nil {
			return fmt.Errorf("build IP pool: %w", err)
		}
		fmt.Printf("[准备] IPv4 段数=%d IPv6 段数=%d 总 IP 数=%d\n",
			len(allV4Ranges()), len(allV6Ranges()), len(pool))

		tcpingParams := TCPingParams{
			Routines:  *flagTCPRoutines,
			Port:      *flagPort,
			PingTimes: *flagTCPTimes,
			MaxDelay:  time.Duration(*flagTCPMaxDelayMS) * time.Millisecond,
			MaxLoss:   *flagTCPMaxLoss,
		}
		layer1Result = RunTCPing(pool, tcpingParams)
		if len(layer1Result) == 0 {
			return fmt.Errorf("Layer 1 全部 IP 都不可达，请检查网络环境")
		}

		if *flagDebug {
			path := filepath.Join(*flagOut, "01_tcping.csv")
			if err := WritePingCSV(path, layer1Result, "layer1"); err != nil {
				return fmt.Errorf("write layer1 csv: %w", err)
			}
			fmt.Printf("  → 写入 %s\n", path)
		}
	}

	// Layer 2: HTTPing
	var layer2Result PingDelaySet
	if *flagSkipHttping {
		fmt.Println("\n[跳过] -skip-httping 已设置，直接用输入数据的 colo 字段")
		layer2Result = layer1Result
	} else {
		httpParams := HTTPingParams{
			Routines:    *flagTCPRoutines / 4,
			Port:        *flagPort,
			URL:         *flagHTTPURL,
			PingTimes:   *flagTCPTimes,
			MaxDelay:    time.Duration(*flagTCPMaxDelayMS) * time.Millisecond,
			MaxLoss:     *flagTCPMaxLoss,
			CFColo:      *flagHTTPColo,
			MinStatusOK: *flagHTTPStatus,
		}
		if httpParams.Routines < 10 {
			httpParams.Routines = 10
		}
		layer2Result = RunHTTPing(layer1Result, httpParams)
		if len(layer2Result) == 0 {
			return fmt.Errorf("Layer 2 全军覆没，没有 IP 通过 cf-ray 校验")
		}

		if *flagDebug {
			path := filepath.Join(*flagOut, "02_httping.csv")
			if err := WritePingCSV(path, layer2Result, "layer2"); err != nil {
				return fmt.Errorf("write layer2 csv: %w", err)
			}
			fmt.Printf("  → 写入 %s\n", path)
		}
	}

	// Layer 3: 下载测速（可选）
	if *flagSkipDownload {
		fmt.Println("\n[跳过] -skip-download 已设置，仅输出 Layer 2 结果")
		path := filepath.Join(*flagOut, "02_httping.csv")
		if err := WritePingCSV(path, layer2Result, "layer2_final"); err != nil {
			return fmt.Errorf("write final csv: %w", err)
		}
		fmt.Printf("\n✅ Top %d (Layer 2 排序后) → %s\n", len(layer2Result), path)
		return nil
	}

	dlParams := DownloadParams{
		Port:         *flagPort,
		URL:          *flagHTTPURL,
		Timeout:      *flagDownloadTime,
		MaxResults:   *flagDownloadTopN,
		MinSpeedMB:   *flagDownloadMinSp,
		ColoPriority: splitCSV(*flagColoPriority),
		Strategy:     ParseTierStrategy(*flagColoStrategy),
	}
	top := RunDownload(layer2Result, dlParams)

	if len(top) == 0 {
		return fmt.Errorf("Layer 3 没有 IP 通过速度过滤")
	}

	path := filepath.Join(*flagOut, "03_top.csv")
	if err := WriteDownloadCSV(path, top, layer2Result); err != nil {
		return fmt.Errorf("write final csv: %w", err)
	}

	fmt.Printf("\n✅ Top %d → %s\n", len(top), path)

	// 按 colo 分桶输出（VPS 端给路由器订阅用）
	if *flagOutputColos {
		if err := WriteColoBuckets(*flagOut, top); err != nil {
			return fmt.Errorf("write colo buckets: %w", err)
		}
		fmt.Printf("📦 按 colo 分桶输出：%s/colo/*.csv\n", *flagOut)
	}
	return nil
}

func usage() {
	fmt.Fprintf(os.Stderr, `cf-speed-pick v%s — 国内→Cloudflare 优选 IP 工具

用法:
  cf-speed-pick [选项]

输出:
  -out string            输出目录 (默认 ./out)
  -debug                 输出 Layer 1/2 中间 CSV

IP 池:
  -ipv6                  启用 IPv6 测速（默认只 IPv4）
  -ipv6-sample int       IPv6 每段采样数 (默认 1000)

Layer 1 TCPing:
  -n int                  并发数 (默认 200；路由器建议 50)
  -t int                  每个 IP 测几次 (默认 4)
  -tp int                 TCP 端口 (默认 443)
  -max-delay int         最大平均延迟 ms (默认 200)
  -max-loss float        最大丢包率 (默认 0.2)

Layer 2 HTTPing:
  -url string            测试 URL (默认 https://cf.xiu2.xyz/url)
  -colo string           只保留指定 IATA 码（逗号分隔）
  -status int            期望 HTTP 状态码 (0=200/301/302)

Layer 3 下载:
  -skip-download         只跑到 Layer 2
  -dt duration           每 IP 下载测速时长 (默认 10s)
  -dn int                取 Top N (默认 10)
  -min-speed float       最低速度 MB/s (默认 0.05)
  -colo-priority string  colo 优先级列表（逗号分隔 IATA 码）
  -colo-strategy string  采样策略 tiered|only|all (默认 tiered)

输入模式:
  -input string          从 CSV 加载候选 IP（跳过 IP 池构造 + Layer 1）
  -skip-httping          跳过 Layer 2（要求 -input 已有 colo 字段）
  -output-colos          按 colo 分桶输出多个 CSV（out/colo/NRT.csv 等）

其他:
  -version               打印版本
  -h                     显示帮助

⚠️  测速前必须关闭科学上网 / VPN / 代理。
    检测：curl https://ifconfig.me 应返回你真实的国内 IP。
`, version)
}

// splitCSV helper: 逗号分隔字符串切分，去除空白和空项。
func splitCSV(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}