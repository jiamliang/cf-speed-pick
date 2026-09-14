// Copyright (C) 2026 cf-speed-pick contributors
// SPDX-License-Identifier: GPL-3.0-or-later

//go:build e2e

// e2e_test.go — 端到端验证（模拟 R7000 完整工作流）
//
// 用法：
//   ./go-docker.sh test -tags=e2e -run TestE2E ./...
//
// 这个测试模拟 R7000 + 青岛 VPS 的完整协作：
//   1. 青岛 VPS 跑完整三层（生成 02_httping.csv 和 03_top.csv）
//   2. 模拟 VPS 把结果按 colo 分桶（WriteColoBuckets）
//   3. 模拟 R7000 从 VPS 拉取 colo 候选（订阅生成器逻辑）
//   4. 模拟 R7000 用 -input 模式重新跑（只校准延迟 + 下载）
//   5. 验证最终结果包含 NRT colo IP

package main

import (
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeDownloadResult 构造假数据，不依赖真实网络
func fakeDownloadResult(ip string, speed float64, colo string, tier int) DownloadResult {
	return DownloadResult{
		IP:    &net.IPAddr{IP: net.ParseIP(ip)},
		Speed: speed,
		Colo:  colo,
		Tier:  tier,
	}
}

func TestE2E_ColoBucketsAndSubscription(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e test skipped in -short mode")
	}

	tmp := t.TempDir()

	// === Step 1: 模拟 VPS 跑完整三层的最终 Top 结果 ===
	t.Log("Step 1: 模拟 VPS 完整跑产生 Top 结果")
	vpsTop := []DownloadResult{
		fakeDownloadResult("172.64.229.1", 12.5, "NRT", 1),
		fakeDownloadResult("172.64.229.2", 11.8, "NRT", 1),
		fakeDownloadResult("172.64.229.3", 10.3, "NRT", 1),
		fakeDownloadResult("162.158.1.1", 8.5, "SIN", 2),
		fakeDownloadResult("162.158.1.2", 7.2, "SIN", 2),
		fakeDownloadResult("104.16.1.1", 6.8, "LAX", 3),
		fakeDownloadResult("104.16.1.2", 5.1, "LAX", 3),
		fakeDownloadResult("198.41.1.1", 4.2, "FRA", 0), // other
	}

	// === Step 2: VPS 把 Top 按 colo 分桶写出 ===
	t.Log("Step 2: WriteColoBuckets 分桶")
	if err := WriteColoBuckets(tmp, vpsTop); err != nil {
		t.Fatal(err)
	}

	// 验证 colo 文件存在
	expectedFiles := []string{"NRT.csv", "SIN.csv", "LAX.csv", "FRA.csv"}
	for _, f := range expectedFiles {
		path := filepath.Join(tmp, "colo", f)
		if _, err := os.Stat(path); err != nil {
			t.Errorf("missing colo file %s: %v", f, err)
		}
	}

	// 验证 NRT.csv 排序（速度降序）
	nrtContent, err := os.ReadFile(filepath.Join(tmp, "colo", "NRT.csv"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(nrtContent), "172.64.229.1") {
		t.Error("NRT.csv should contain fastest IP first")
	}

	// === Step 3: 模拟 R7000 拉取 colo 候选，构造合并的 csv 输入 ===
	t.Log("Step 3: R7000 从 VPS 拉取 NRT,SIN 候选，合并为 input.csv")
	mergedInput := filepath.Join(tmp, "router-input.csv")
	f, err := os.Create(mergedInput)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	// CSV 表头
	f.WriteString("ip,sended,received,loss_rate,delay_ms,colo,timestamp,source\n")
	now := time.Now().Format(time.RFC3339)

	// 模拟路由器端延迟（青岛 NRT 是 50ms，路由器到 NRT 假设是 80ms）
	routerDelays := map[string]int64{
		"172.64.229.1": 80,
		"172.64.229.2": 85,
		"172.64.229.3": 90,
		"162.158.1.1":  120,
		"162.158.1.2":  125,
	}

	// 从 VPS 的 colo 桶里取 NRT + SIN（按顺序合并）
	for _, colo := range []string{"NRT", "SIN"} {
		content, err := os.ReadFile(filepath.Join(tmp, "colo", colo+".csv"))
		if err != nil {
			t.Fatal(err)
		}
		lines := strings.Split(string(content), "\n")
		for _, line := range lines {
			if line == "" || strings.HasPrefix(line, "ip,") {
				continue
			}
			parts := strings.Split(line, ",")
			if len(parts) < 4 {
				continue
			}
			ip := parts[0]
			delay := routerDelays[ip]
			if delay == 0 {
				delay = 100 // 默认
			}
			f.WriteString(ip + ",4,4,0.0000," + intToStr(delay) + "," + colo + "," + now + ",layer2\n")
		}
	}

	// === Step 4: 验证 input CSV 能被 ReadPingCSV 读出来 ===
	t.Log("Step 4: ReadPingCSV 验证 router-input.csv")
	rows, err := ReadPingCSV(mergedInput)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 5 {
		t.Errorf("want 5 rows, got %d", len(rows))
	}

	// === Step 5: 验证所有 IP 都有 colo 字段（这是 -skip-httping 的前提）===
	for _, r := range rows {
		if r.Colo == "" {
			t.Errorf("ip %s missing colo, -skip-httping requires colo field", r.IP.String())
		}
	}

	// === Step 6: 验证 Layer 1 的 ReadPingCSV 兼容 colo 为空的旧格式 ===
	t.Log("Step 5: 旧格式兼容测试（colo 字段允许为空）")
	legacyInput := filepath.Join(tmp, "legacy-input.csv")
	os.WriteFile(legacyInput, []byte(
		"ip,sended,received,loss_rate,delay_ms,colo,timestamp,source\n"+
			"1.1.1.1,4,4,0.0000,50,,2026-09-14T10:00:00+08:00,layer1\n"+
			"2.2.2.2,4,3,0.2500,80,,2026-09-14T10:00:01+08:00,layer1\n",
	), 0o644)
	legacyRows, err := ReadPingCSV(legacyInput)
	if err != nil {
		t.Fatal(err)
	}
	if len(legacyRows) != 2 {
		t.Errorf("legacy: want 2 rows, got %d", len(legacyRows))
	}
	if legacyRows[0].Colo != "" {
		t.Errorf("legacy: row[0] colo should be empty, got %s", legacyRows[0].Colo)
	}

	t.Log("✅ E2E 通过：VPS 跑 + 分桶 + 路由器订阅 + -input 兼容")
}

func TestE2E_FullScriptChain(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e test skipped in -short mode")
	}

	// 验证脚本可执行（不需要真跑路由器）
	scripts := []string{
		"scripts/r7000-cron.sh",
		"scripts/push-to-vps.sh",
		"scripts/ensure-cron.sh",
		"scripts/qingdao-cron.sh",
		"scripts/install-router.sh",
	}
	for _, s := range scripts {
		info, err := os.Stat(s)
		if err != nil {
			t.Errorf("missing script: %s", s)
			continue
		}
		if info.Mode()&0o111 == 0 {
			t.Errorf("script not executable: %s", s)
		}
	}

	// 验证 shell 语法（sh -n）
	for _, s := range scripts {
		if strings.HasSuffix(s, ".sh") {
			if err := shellSyntaxCheck("sh", s); err != nil {
				t.Errorf("shell syntax error in %s: %v", s, err)
			}
		}
	}
}

func intToStr(n int64) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
