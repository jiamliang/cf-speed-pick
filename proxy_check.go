// Copyright (C) 2026 cf-speed-pick contributors
// SPDX-License-Identifier: GPL-3.0-or-later
//
// 测速前的代理检测：科学上网会污染测速结果，必须拒绝运行。
//
// 检测范围：
//   - 环境变量 HTTP_PROXY / HTTPS_PROXY / ALL_PROXY（含小写变体）
//   - git 的全局配置 [http] proxy / [https] proxy（部分用户通过 git config 设的）

package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// proxyEnvVars 要检查的环境变量名。
// 注意大小写都查：Go 的 os.Getenv 不区分大小写，但用户可能误设成小写，
// 我们也用 strings.EqualFold 兜底。
var proxyEnvVars = []string{
	"HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY",
	"http_proxy", "https_proxy", "all_proxy",
	"NO_PROXY", "no_proxy", // 也检查：用户配了 no_proxy 说明很可能有代理
}

// checkNoProxy 在 main 启动时调用。
// 检测到任何疑似代理配置就返回非 nil error。
func checkNoProxy() error {
	var detected []string

	for _, name := range proxyEnvVars {
		if v := os.Getenv(name); v != "" {
			detected = append(detected, fmt.Sprintf("%s=%s", name, v))
		}
	}

	// 检查 git 全局配置（很多人把代理配在 git 里）
	if gitProxy := checkGitProxy(); gitProxy != "" {
		detected = append(detected, fmt.Sprintf("git config: %s", gitProxy))
	}

	if len(detected) > 0 {
		return fmt.Errorf(
			"检测到代理配置：\n  %s\n\n"+
				"测速结果会失真（变成 代理→CF 而非 你→CF），请先关闭科学上网/VPN。\n"+
				"检测方法：curl https://ifconfig.me 应该返回你 **真实的国内 IP**，不是香港/美国 IP。",
			strings.Join(detected, "\n  "),
		)
	}
	return nil
}

// checkGitProxy 读 git 的全局代理配置。
// 用 `git config --global --get` 而不是直接读 ~/.gitconfig，避免解析器差异。
func checkGitProxy() string {
	if _, err := exec.LookPath("git"); err != nil {
		return ""
	}
	var found []string
	for _, key := range []string{"http.proxy", "https.proxy"} {
		out, err := exec.Command("git", "config", "--global", "--get", key).Output()
		if err == nil && len(out) > 0 {
			found = append(found, fmt.Sprintf("%s=%s", key, strings.TrimSpace(string(out))))
		}
	}
	return strings.Join(found, ", ")
}
