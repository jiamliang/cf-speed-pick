// Copyright (C) 2026 cf-speed-pick contributors
// SPDX-License-Identifier: GPL-3.0-or-later

//go:build e2e

package main

import (
	"os/exec"
)

// shellSyntaxCheck 用 sh -n 或 bash -n 检查 shell 脚本语法
func shellSyntaxCheck(interpreter, path string) error {
	cmd := exec.Command(interpreter, "-n", path)
	return cmd.Run()
}
