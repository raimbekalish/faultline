//go:build !darwin && !linux

package lab

import "os/exec"

func configureProcess(*exec.Cmd) {}
