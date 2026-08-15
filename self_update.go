package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

var errSelfUpdateStarted = errors.New("self-update started")

type selfUpdateManifest struct {
	Version string `json:"version"`
	Name    string `json:"name"`
	Size    int64  `json:"size"`
	SHA256  string `json:"sha256"`
}

const selfUpdateManifestURL = cnbModelAssets + "/launcher-app-manifest.json"

const githubSelfUpdateBase = "https://github.com/niechy/zzz-cnb-launcher/releases/latest/download"

func ensureSelfCurrent(root string) (bool, error) {
	var text string
	var err error
	for _, url := range []string{selfUpdateManifestURL, githubSelfUpdateBase + "/launcher-app-manifest.json"} {
		text, err = fetchText(url, 8*time.Second, 128*1024)
		if err == nil {
			break
		}
	}
	if err != nil {
		return false, nil
	}
	var manifest selfUpdateManifest
	if err := json.Unmarshal([]byte(text), &manifest); err != nil {
		return false, fmt.Errorf("自更新清单格式错误：%w", err)
	}
	if comparison, ok := compareNumericVersions(manifest.Version, appVersion); !ok || comparison <= 0 {
		return false, nil
	}
	if manifest.Name == "" || manifest.Size <= 0 || len(manifest.SHA256) != 64 {
		return false, errors.New("自更新清单中的文件信息无效")
	}
	current, err := os.Executable()
	if err != nil {
		return false, err
	}
	current, err = filepath.Abs(current)
	if err != nil {
		return false, err
	}
	dir := filepath.Join(root, ".install", "zzz-cnb-self-update")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return false, err
	}
	newPath := filepath.Join(dir, manifest.Name)
	urls := []string{cnbModelAssets + "/" + manifest.Name, githubSelfUpdateBase + "/" + manifest.Name}
	for _, url := range urls {
		if err := downloadFile(url, newPath); err != nil {
			continue
		}
		info, statErr := os.Stat(newPath)
		if statErr != nil || info.Size() != manifest.Size {
			_ = os.Remove(newPath)
			continue
		}
		valid, hashErr := verifySHA256(newPath, manifest.SHA256)
		if hashErr != nil || !valid {
			_ = os.Remove(newPath)
			continue
		}
		if err := scheduleSelfReplace(newPath, current); err != nil {
			return false, err
		}
		return true, nil
	}
	return false, errors.New("自更新文件下载或校验失败")
}

func scheduleSelfReplace(newPath, currentPath string) error {
	cmd := exec.Command(newPath, "--self-replace", currentPath)
	cmd.Dir = filepath.Dir(currentPath)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("无法启动自更新替换进程：%w", err)
	}
	return nil
}

func quoteCmdPath(path string) string {
	// Paths come from os.Executable and the local installation directory.
	// Double quotes are not valid in Windows file names, so quoting is enough.
	return `"` + strings.ReplaceAll(path, `"`, ``) + `"`
}

func runSelfReplacement() error {
	if len(os.Args) < 3 {
		return errors.New("自更新缺少目标路径")
	}
	target, err := filepath.Abs(os.Args[2])
	if err != nil {
		return err
	}
	self, err := os.Executable()
	if err != nil {
		return err
	}
	backup := target + ".zzz-cnb-self-old"
	temporary := target + ".zzz-cnb-self-new"
	for attempt := 0; attempt < 120; attempt++ {
		_ = os.Remove(temporary)
		if err := copyFile(self, temporary); err != nil {
			return fmt.Errorf("准备新版文件失败：%w", err)
		}
		_ = os.Remove(backup)
		if err := os.Rename(target, backup); err != nil {
			_ = os.Remove(temporary)
			time.Sleep(500 * time.Millisecond)
			continue
		}
		if err := os.Rename(temporary, target); err != nil {
			_ = os.Rename(backup, target)
			time.Sleep(500 * time.Millisecond)
			continue
		}
		_ = os.Remove(backup)
		return startLauncher(target, filepath.Dir(target))
	}
	return errors.New("等待旧版本退出超时")
}

func copyFile(source, destination string) error {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(destination, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0755)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, in)
	closeErr := out.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

func startLauncher(path, workDir string) error {
	command := fmt.Sprintf("start \"\" %s", quoteCmdPath(path))
	cmd := exec.Command("cmd.exe", "/d", "/c", command)
	cmd.Dir = workDir
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	return cmd.Start()
}
