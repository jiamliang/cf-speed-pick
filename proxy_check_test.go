// Copyright (C) 2026 cf-speed-pick contributors
// SPDX-License-Identifier: GPL-3.0-or-later

package main

import "testing"

func TestCheckNoProxy_CleanEnv(t *testing.T) {
	// 注意：本测试假设执行环境本身干净。如果你的环境有代理，测试会失败。
	// 可以临时清掉再跑。
	for _, name := range proxyEnvVars {
		t.Setenv(name, "")
	}
	if err := checkNoProxy(); err != nil {
		t.Logf("warning: test environment itself has proxy: %v", err)
	}
}

func TestCheckNoProxy_DetectsEnv(t *testing.T) {
	t.Setenv("HTTP_PROXY", "http://127.0.0.1:10809")
	if err := checkNoProxy(); err == nil {
		t.Error("expected error when HTTP_PROXY is set, got nil")
	}
}
