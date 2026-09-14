// Copyright (C) 2026 cf-speed-pick contributors
// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"net/http"
	"strings"
	"testing"
)

func TestExtractColo_CF(t *testing.T) {
	h := http.Header{}
	h.Set("server", "cloudflare")
	h.Set("cf-ray", "7bd32409eda7b020-SJC")
	if got := extractColo(h); got != "SJC" {
		t.Errorf("want SJC, got %s", got)
	}
}

func TestExtractColo_NoCF(t *testing.T) {
	h := http.Header{}
	h.Set("server", "nginx")
	if got := extractColo(h); got != "" {
		t.Errorf("want empty, got %s", got)
	}
}

func TestExtractColo_RayWithoutDash(t *testing.T) {
	h := http.Header{}
	h.Set("server", "cloudflare")
	h.Set("cf-ray", "abcdef-NRT")
	if got := extractColo(h); got != "NRT" {
		t.Errorf("want NRT, got %s", got)
	}
}

func TestMatchCFColo(t *testing.T) {
	cases := []struct {
		colo, list string
		want       bool
	}{
		{"SJC", "SJC,NRT", true},
		{"NRT", "SJC,NRT", true},
		{"LAX", "SJC,NRT", false},
		{"SJC", "sjc,nrt", true},  // 大小写不敏感
		{"", "SJC", false},         // colo 空
		{"SJC", "", true},          // 列表空 = 不过滤
		{"SJC", "  SJC  ", true},   // 前后空格
	}
	for _, c := range cases {
		if got := matchCFColo(c.colo, c.list); got != c.want {
			t.Errorf("matchCFColo(%q, %q) = %v, want %v", c.colo, c.list, got, c.want)
		}
	}
}

func TestStatusOK(t *testing.T) {
	cases := []struct {
		got, want int
		ok        bool
	}{
		{200, 0, true},
		{301, 0, true},
		{302, 0, true},
		{404, 0, false},
		{200, 200, true},
		{301, 200, false},
		{200, 404, false},
	}
	for _, c := range cases {
		if got := statusOK(c.got, c.want); got != c.ok {
			t.Errorf("statusOK(%d, %d) = %v, want %v", c.got, c.want, got, c.ok)
		}
	}
}

func TestHTTPingParams_Defaults(t *testing.T) {
	p := HTTPingParams{}
	p.defaults()
	if p.Routines != 50 {
		t.Errorf("default routines: want 50, got %d", p.Routines)
	}
	if p.Port != 443 {
		t.Errorf("default port: want 443, got %d", p.Port)
	}
	if p.PingTimes != 4 {
		t.Errorf("default ping times: want 4, got %d", p.PingTimes)
	}
	if p.URL == "" {
		t.Errorf("URL should have default")
	}
	if !strings.HasPrefix(p.URL, "https://") {
		t.Errorf("URL should be https, got %s", p.URL)
	}
}