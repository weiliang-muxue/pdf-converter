package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
)

func configureCommand(cmd *exec.Cmd) { cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true} }
func killCommand(cmd *exec.Cmd) {
	_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	_ = cmd.Process.Kill()
}
func openBrowser(address string) error { return exec.Command("xdg-open", address).Run() }
func showError(message string)         { fmt.Fprintln(os.Stderr, message) }

func detectNginxProcess() (string, string, string, error) {
	entries, _ := os.ReadDir("/proc")
	type instance struct{ exe, prefix, config string }
	found := map[string]instance{}
	args := regexp.MustCompile(`(?:^|\s)-(p|c)\s*(?:"([^"]+)"|'([^']+)'|(\S+))`)
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		dir := filepath.Join("/proc", entry.Name())
		data, err := os.ReadFile(filepath.Join(dir, "cmdline"))
		if err != nil {
			continue
		}
		command := strings.ReplaceAll(string(data), "\x00", " ")
		if !strings.HasPrefix(command, "nginx: master process ") {
			continue
		}
		exe, err := os.Readlink(filepath.Join(dir, "exe"))
		if err != nil {
			continue
		}
		root, _ := os.Readlink(filepath.Join(dir, "root"))
		if root != "/" {
			return "", "", "", fmt.Errorf("Nginx 位于容器或 chroot 中，请在同一运行环境执行部署脚本")
		}
		item := instance{exe: exe}
		for _, m := range args.FindAllStringSubmatch(command, -1) {
			value := m[2]
			if value == "" {
				value = m[3]
			}
			if value == "" {
				value = m[4]
			}
			if m[1] == "p" {
				item.prefix = value
			} else {
				item.config = value
			}
		}
		found[item.exe+"|"+item.prefix+"|"+item.config] = item
	}
	if len(found) > 1 {
		return "", "", "", fmt.Errorf("发现多个 Nginx 实例，请指定 PDF_NGINX_BIN、PDF_NGINX_PREFIX、PDF_NGINX_CONFIG")
	}
	for _, item := range found {
		return item.exe, item.prefix, item.config, nil
	}
	return "", "", "", nil
}
