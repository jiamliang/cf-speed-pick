// Copyright (C) 2026 cf-speed-pick contributors
// SPDX-License-Identifier: GPL-3.0-or-later
//
// CSV 输出。

package main

import (
	"encoding/csv"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

// CSV 列定义（统一字段，方便下游脚本读取）
const (
	colIP         = "ip"
	colSended     = "sended"
	colReceived   = "received"
	colLossRate   = "loss_rate"
	colDelayMS    = "delay_ms"
	colColo       = "colo"
	colSpeedMBps  = "download_speed_MBps"
	colTimestamp  = "timestamp"
	colSource     = "source" // 标识这是哪一层的结果（layer1/layer2/layer3）
)

// PingResultHeader 中间层（Layer 1/2）CSV 表头。
func PingResultHeader() []string {
	return []string{colIP, colSended, colReceived, colLossRate, colDelayMS, colColo, colTimestamp, colSource}
}

// TopResultHeader 最终 Top N（Layer 3）CSV 表头。
func TopResultHeader() []string {
	return []string{colIP, colSpeedMBps, colColo, colDelayMS, colLossRate, colTimestamp}
}

// EnsureOutDir 确保输出目录存在。
func EnsureOutDir(dir string) error {
	return os.MkdirAll(dir, 0o755)
}

// WritePingCSV 写 Layer 1/2 中间结果。
func WritePingCSV(path string, rows PingDelaySet, source string) error {
	if err := EnsureOutDir(filepath.Dir(path)); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	w := csv.NewWriter(f)
	defer w.Flush()

	if err := w.Write(PingResultHeader()); err != nil {
		return err
	}
	now := time.Now().Format(time.RFC3339)
	for _, r := range rows {
		row := []string{
			r.IP.String(),
			strconv.Itoa(r.Sended),
			strconv.Itoa(r.Received),
			fmt.Sprintf("%.4f", r.LossRate()),
			strconv.FormatInt(r.Delay.Milliseconds(), 10),
			r.Colo,
			now,
			source,
		}
		if err := w.Write(row); err != nil {
			return err
		}
	}
	return nil
}

// WriteDownloadCSV 写 Layer 3 Top N 结果。
func WriteDownloadCSV(path string, rows []DownloadResult, sourceData PingDelaySet) error {
	if err := EnsureOutDir(filepath.Dir(path)); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	w := csv.NewWriter(f)
	defer w.Flush()

	if err := w.Write(TopResultHeader()); err != nil {
		return err
	}
	// 建 IP → 上层数据 索引，方便拿到 delay/loss
	idx := make(map[string]PingData)
	for _, p := range sourceData {
		idx[p.IP.String()] = p
	}
	now := time.Now().Format(time.RFC3339)
	for _, r := range rows {
		var delayMS int64
		var lossRate float64
		if p, ok := idx[r.IP.String()]; ok {
			delayMS = p.Delay.Milliseconds()
			lossRate = p.LossRate()
		}
		row := []string{
			r.IP.String(),
			fmt.Sprintf("%.4f", r.Speed),
			r.Colo,
			strconv.FormatInt(delayMS, 10),
			fmt.Sprintf("%.4f", lossRate),
			now,
		}
		if err := w.Write(row); err != nil {
			return err
		}
	}
	return nil
}

// ReadPingCSV 读 CSV 回 PingDelaySet（供 XIU2 对照测试用）。
func ReadPingCSV(path string) (PingDelaySet, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	r := csv.NewReader(f)
	records, err := r.ReadAll()
	if err != nil {
		return nil, err
	}
	if len(records) <= 1 {
		return nil, nil
	}
	out := make(PingDelaySet, 0, len(records)-1)
	for _, rec := range records[1:] {
		if len(rec) < 5 {
			continue
		}
		ip := net.ParseIP(rec[0])
		if ip == nil {
			continue
		}
		sended, _ := strconv.Atoi(rec[1])
		received, _ := strconv.Atoi(rec[2])
		delayMS, _ := strconv.ParseInt(rec[4], 10, 64)
		p := PingData{
			IP:       &net.IPAddr{IP: ip},
			Sended:   sended,
			Received: received,
			Delay:    time.Duration(delayMS) * time.Millisecond,
			Colo:     safeGet(rec, 5),
		}
		out = append(out, p)
	}
	return out, nil
}

func safeGet(s []string, i int) string {
	if i < len(s) {
		return s[i]
	}
	return ""
}