// Copyright (C) 2026 cf-speed-pick contributors
// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"net"
	"os"
	"path/filepath"
	"testing"
)

func TestWriteColoBuckets(t *testing.T) {
	tmp := t.TempDir()

	results := []DownloadResult{
		{IP: &net.IPAddr{IP: net.ParseIP("1.1.1.1")}, Speed: 12.5, Colo: "NRT", Tier: 1},
		{IP: &net.IPAddr{IP: net.ParseIP("2.2.2.2")}, Speed: 8.3, Colo: "NRT", Tier: 1},
		{IP: &net.IPAddr{IP: net.ParseIP("3.3.3.3")}, Speed: 5.1, Colo: "SIN", Tier: 2},
		{IP: &net.IPAddr{IP: net.ParseIP("4.4.4.4")}, Speed: 3.2, Colo: ""}, // UNKNOWN bucket
	}

	if err := WriteColoBuckets(tmp, results); err != nil {
		t.Fatal(err)
	}

	// 检查文件
	expected := []string{"NRT.csv", "SIN.csv", "UNKNOWN.csv"}
	for _, f := range expected {
		path := filepath.Join(tmp, "colo", f)
		if _, err := os.Stat(path); err != nil {
			t.Errorf("expected file %s, got error %v", path, err)
		}
	}

	// 验证 NRT.csv 内容（2 行 + 表头）
	nrtContent, err := os.ReadFile(filepath.Join(tmp, "colo", "NRT.csv"))
	if err != nil {
		t.Fatal(err)
	}
	if len(nrtContent) == 0 {
		t.Error("NRT.csv is empty")
	}
	t.Logf("NRT.csv:\n%s", string(nrtContent))
}

func TestReadPingCSV_WithColo(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "test.csv")

	// 写一个有 colo 的 CSV
	content := `ip,sended,received,loss_rate,delay_ms,colo,timestamp,source
1.1.1.1,4,4,0.0000,50,NRT,2026-09-14T10:00:00+08:00,layer2
2.2.2.2,4,3,0.2500,80,SIN,2026-09-14T10:00:01+08:00,layer2
3.3.3.3,4,4,0.0000,100,,2026-09-14T10:00:02+08:00,layer2
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	rows, err := ReadPingCSV(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 {
		t.Errorf("want 3 rows, got %d", len(rows))
	}
	if rows[0].Colo != "NRT" {
		t.Errorf("row[0] colo: want NRT, got %s", rows[0].Colo)
	}
	if rows[2].Colo != "" {
		t.Errorf("row[2] colo: want empty, got %s", rows[2].Colo)
	}
}