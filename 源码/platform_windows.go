package main

import (
	"fmt"
	"os/exec"
	"syscall"
	"unsafe"
)

func configureCommand(cmd *exec.Cmd) { cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true} }

// Windows launcher uses WMI to provide the exact running Nginx instance.
func detectNginxProcess() (string, string, string, error) { return "", "", "", nil }
func killCommand(cmd *exec.Cmd) {
	kill := exec.Command("taskkill.exe", "/PID", fmt.Sprint(cmd.Process.Pid), "/T", "/F")
	configureCommand(kill)
	_ = kill.Run()
	_ = cmd.Process.Kill()
}
func openBrowser(address string) error {
	cmd := exec.Command("rundll32.exe", "url.dll,FileProtocolHandler", address)
	configureCommand(cmd)
	return cmd.Run()
}
func showError(message string) {
	fmt.Println(message)
	text, _ := syscall.UTF16PtrFromString(message)
	title, _ := syscall.UTF16PtrFromString("PDF 工具箱")
	proc := syscall.NewLazyDLL("user32.dll").NewProc("MessageBoxW")
	_, _, _ = proc.Call(0, uintptr(unsafe.Pointer(text)), uintptr(unsafe.Pointer(title)), 0x10)
}
