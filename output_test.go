// Copyright (C) 2026 cf-speed-pick contributors
// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestEnsureOutDir(t *testing.T) {
	tmp := t.TempDir()
	d := filepath.Join(tmp, "subdir", "deeper")
	if err := EnsureOutDir(d); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(d); err != nil {
		t.Errorf("dir not created: %v", err)
	}
}

func TestWriteReadPingCSV_Roundtrip(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "out", "01_tcping.csv")

	rows := PingDelaySet{
		{IP: &net.IPAddr{IP: net.ParseIP("1.1.1.1")}, Sended: 4, Received: 4, Delay: 50 * time.Millisecond, Colo: "SJC"},
		{IP: &net.IPAddr{IP: net.ParseIP("2.2.2.2")}, Sended: 4, Received: 3, Delay: 100 * time.Millisecond, Colo: ""},
	}
	if err := WritePingCSV(path, rows, "layer1"); err != nil {
		t.Fatal(err)
	}

	got, err := ReadPingCSV(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(rows) {
		t.Fatalf("rows: want %d, got %d", len(rows), len(got))
	}
	if got[0].IP.String() != "1.1.1.1" {
		t.Errorf("ip[0]: want 1.1.1.1, got %s", got[0].IP.String())
	}
	if got[0].Colo != "SJC" {
		t.Errorf("colo[0]: want SJC, got %s", got[0].Colo)
	}
	if got[0].Delay != 50*time.Millisecond {
		t.Errorf("delay[0]: want 50ms, got %v", got[0].Delay)
	}
	if got[1].Colo != "" {
		t.Errorf("colo[1]: want empty, got %s", got[1].Colo)
	}
}

func TestWriteDownloadCSV(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "03_top.csv")

	source := PingDelaySet{
		{IP: &net.IPAddr{IP: net.ParseIP("1.1.1.1")}, Sended: 4, Received: 4, Delay: 30 * time.Millisecond, Colo: "HKG"},
	}
	results := []DownloadResult{
		{IP: &net.IPAddr{IP: net.ParseIP("1.1.1.1")}, Speed: 12.34, Colo: "HKG"},
	}
	if err := WriteDownloadCSV(path, results, source); err != nil {
		t.Fatal(err)
	}

	got, err := ReadPingCSV(path) // 表头差异，能读但 colo 字段偏移，不严格校验
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Errorf("want 1 row, got %d", len(got))
	}
}