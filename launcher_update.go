package main

import (
	"archive/zip"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

const (
	launcherManifestURL = cnbModelAssets + "/launcher-manifest.json"
	launcherGitHubBase  = "https://github.com/OneDragon-Anything/ZenlessZoneZero-OneDragon/releases/download"
)

type launcherAsset struct {
	Name   string `json:"name"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

type launcherManifest struct {
	Version string                   `json:"version"`
	Assets  map[string]launcherAsset `json:"assets"`
}

type launcherInstall struct {
	Kind       string
	ExeName    string
	HasRuntime bool
}

func ensureLauncherCurrent(root, launcherPath string) (bool, error) {
	install, err := identifyLauncher(launcherPath)
	if err != nil {
		return false, err
	}
	text, err := fetchText(launcherManifestURL, 12*time.Second, 512*1024)
	if err != nil {
		return false, fmt.Errorf("读取 CNB 更新清单失败：%w", err)
	}
	var manifest launcherManifest
	if err := json.Unmarshal([]byte(text), &manifest); err != nil {
		return false, fmt.Errorf("更新清单格式错误：%w", err)
	}
	asset, ok := manifest.Assets[install.Kind]
	if !ok {
		return false, fmt.Errorf("更新清单缺少 %s", install.Kind)
	}
	if err := validateLauncherManifest(manifest.Version, asset, install); err != nil {
		return false, err
	}

	currentVersion := getLauncherVersion(launcherPath)
	if currentVersion != "" {
		if comparison, comparable := compareLauncherVersions(currentVersion, manifest.Version); comparable && comparison >= 0 {
			fmt.Printf("      [已有] %s %s（最新稳定版 %s）\n", install.ExeName, currentVersion, manifest.Version)
			return false, nil
		}
		fmt.Printf("      [更新] %s %s -> %s\n", install.ExeName, currentVersion, manifest.Version)
	} else {
		fmt.Printf("      [更新] %s 无法读取版本，将安装最新稳定版 %s\n", install.ExeName, manifest.Version)
	}

	downloadDir := filepath.Join(root, ".install", "zzz-cnb-launcher-update")
	if err := os.MkdirAll(downloadDir, 0755); err != nil {
		return false, err
	}
	defer os.RemoveAll(downloadDir)
	zipPath := filepath.Join(downloadDir, asset.Name)
	var failures []string
	for _, url := range launcherAssetURLs(root, manifest.Version, asset.Name) {
		fmt.Printf("             尝试：%s\n", shortURL(url))
		if err := downloadFile(url, zipPath); err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", shortURL(url), err))
			continue
		}
		info, statErr := os.Stat(zipPath)
		if statErr != nil || info.Size() != asset.Size {
			failures = append(failures, fmt.Sprintf("%s: 文件大小不匹配", shortURL(url)))
			_ = os.Remove(zipPath)
			continue
		}
		valid, hashErr := verifySHA256(zipPath, asset.SHA256)
		if hashErr != nil || !valid {
			failures = append(failures, fmt.Sprintf("%s: SHA-256 校验失败", shortURL(url)))
			_ = os.Remove(zipPath)
			continue
		}
		failures = nil
		break
	}
	if len(failures) > 0 || !fileExists(zipPath) {
		return false, fmt.Errorf("所有下载渠道均失败（最后错误：%s）", failures[len(failures)-1])
	}

	staging := filepath.Join(downloadDir, "staging")
	if err := extractLauncherArchive(zipPath, staging, install); err != nil {
		return false, err
	}
	if err := replaceLauncher(root, staging, install); err != nil {
		return false, err
	}
	return true, nil
}

func getLauncherVersion(path string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, "--version")
	cmd.Env = append(os.Environ(),
		"__COMPAT_LAYER=RunAsInvoker",
		"PYINSTALLER_RESET_ENVIRONMENT=1",
	)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	output, err := cmd.Output()
	if err == nil {
		if version := parseLauncherVersionOutput(string(output)); version != "" {
			return version
		}
	}
	return getWindowsFileVersion(path)
}

func parseLauncherVersionOutput(output string) string {
	ansi := regexp.MustCompile(`\x1b\[[0-?]*[ -/]*[@-~]`)
	clean := strings.TrimSpace(ansi.ReplaceAllString(output, ""))
	versionPattern := regexp.MustCompile(`(?i)v?\d+\.\d+\.\d+(?:\.\d+)?(?:-[0-9a-z.-]+)?`)
	matches := versionPattern.FindAllString(clean, -1)
	if len(matches) == 0 {
		return ""
	}
	return matches[len(matches)-1]
}

func compareLauncherVersions(left, right string) (int, bool) {
	leftParts := strings.SplitN(strings.TrimSpace(left), "-", 2)
	rightParts := strings.SplitN(strings.TrimSpace(right), "-", 2)
	comparison, ok := compareNumericVersions(leftParts[0], rightParts[0])
	if !ok || comparison != 0 {
		return comparison, ok
	}
	leftPre := len(leftParts) == 2
	rightPre := len(rightParts) == 2
	if leftPre == rightPre {
		return strings.Compare(strings.Join(leftParts[1:], "-"), strings.Join(rightParts[1:], "-")), true
	}
	if leftPre {
		return -1, true
	}
	return 1, true
}

func identifyLauncher(path string) (launcherInstall, error) {
	name := filepath.Base(path)
	switch strings.ToLower(name) {
	case strings.ToLower("OneDragon-RuntimeLauncher.exe"):
		return launcherInstall{Kind: "runtime", ExeName: name, HasRuntime: true}, nil
	case strings.ToLower("OneDragon-Launcher.exe"):
		return launcherInstall{Kind: "launcher", ExeName: name}, nil
	default:
		return launcherInstall{}, fmt.Errorf("%s 暂无启动器更新包", name)
	}
}

func validateLauncherManifest(version string, asset launcherAsset, install launcherInstall) error {
	if _, ok := parseNumericVersion(version); !ok {
		return fmt.Errorf("更新清单版本号无效：%q", version)
	}
	expectedName := "ZenlessZoneZero-OneDragon-Launcher.zip"
	if install.HasRuntime {
		expectedName = "ZenlessZoneZero-OneDragon-RuntimeLauncher.zip"
	}
	if asset.Name != expectedName || asset.Size <= 0 || asset.Size > 2*1024*1024*1024 {
		return errors.New("更新清单中的启动器资产无效")
	}
	if matched, _ := regexp.MatchString(`^[0-9a-fA-F]{64}$`, asset.SHA256); !matched {
		return errors.New("更新清单中的 SHA-256 无效")
	}
	return nil
}

func launcherAssetURLs(root, version, name string) []string {
	urls := []string{cnbModelAssets + "/" + name}
	githubURL := fmt.Sprintf("%s/%s/%s", launcherGitHubBase, version, name)
	templates := customMirrorTemplates(root)
	if len(templates) == 0 {
		templates = []string{
			"https://ghfast.top/{url}",
			"https://gh-proxy.com/{url}",
			"https://ghproxy.net/{url}",
			"https://gh.llkk.cc/{url}",
			"{url}",
		}
	}
	for _, template := range templates {
		urls = append(urls, strings.ReplaceAll(template, "{url}", githubURL))
	}
	return uniqueStrings(urls)
}

func extractLauncherArchive(zipPath, staging string, install launcherInstall) error {
	reader, err := zip.OpenReader(zipPath)
	if err != nil {
		return fmt.Errorf("启动器 ZIP 无法打开：%w", err)
	}
	defer reader.Close()
	if err := os.MkdirAll(staging, 0755); err != nil {
		return err
	}
	var total uint64
	for _, entry := range reader.File {
		name := strings.TrimPrefix(strings.ReplaceAll(entry.Name, "\\", "/"), "./")
		clean := filepath.Clean(filepath.FromSlash(name))
		if clean == "." || filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(os.PathSeparator)) {
			return fmt.Errorf("启动器 ZIP 包含不安全路径：%q", entry.Name)
		}
		allowed := strings.EqualFold(clean, install.ExeName)
		if install.HasRuntime && (strings.EqualFold(clean, ".runtime") || strings.HasPrefix(strings.ToLower(clean), ".runtime"+strings.ToLower(string(os.PathSeparator)))) {
			allowed = true
		}
		if !allowed {
			continue
		}
		total += entry.UncompressedSize64
		if total > 2*1024*1024*1024 {
			return errors.New("启动器 ZIP 解压后过大")
		}
		destination := filepath.Join(staging, clean)
		if entry.FileInfo().IsDir() {
			if err := os.MkdirAll(destination, 0755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(destination), 0755); err != nil {
			return err
		}
		if err := extractOne(entry, destination); err != nil {
			return err
		}
	}
	if !fileExists(filepath.Join(staging, install.ExeName)) {
		return fmt.Errorf("启动器 ZIP 缺少 %s", install.ExeName)
	}
	if install.HasRuntime {
		entries, err := os.ReadDir(filepath.Join(staging, ".runtime"))
		if err != nil || len(entries) == 0 {
			return errors.New("RuntimeLauncher ZIP 缺少 .runtime")
		}
	}
	return nil
}

func replaceLauncher(root, staging string, install launcherInstall) error {
	targetExe := filepath.Join(root, install.ExeName)
	backupExe := targetExe + ".zzz-cnb.bak"
	targetRuntime := filepath.Join(root, ".runtime")
	backupRuntime := filepath.Join(root, ".runtime.zzz-cnb.bak")
	_ = os.Remove(backupExe)
	_ = os.RemoveAll(backupRuntime)

	exeBackedUp := false
	runtimeBackedUp := false
	rollback := func() {
		_ = os.Remove(targetExe)
		if exeBackedUp {
			_ = os.Rename(backupExe, targetExe)
		}
		if install.HasRuntime {
			_ = os.RemoveAll(targetRuntime)
			if runtimeBackedUp {
				_ = os.Rename(backupRuntime, targetRuntime)
			}
		}
	}

	if err := os.Rename(targetExe, backupExe); err != nil {
		return fmt.Errorf("备份旧启动器失败：%w", err)
	}
	exeBackedUp = true
	if install.HasRuntime {
		if _, err := os.Stat(targetRuntime); err == nil {
			if err := os.Rename(targetRuntime, backupRuntime); err != nil {
				rollback()
				return fmt.Errorf("备份旧 .runtime 失败：%w", err)
			}
			runtimeBackedUp = true
		} else if !os.IsNotExist(err) {
			rollback()
			return err
		}
	}
	if err := os.Rename(filepath.Join(staging, install.ExeName), targetExe); err != nil {
		rollback()
		return fmt.Errorf("安装新启动器失败：%w", err)
	}
	if install.HasRuntime {
		if err := os.Rename(filepath.Join(staging, ".runtime"), targetRuntime); err != nil {
			rollback()
			return fmt.Errorf("安装新 .runtime 失败：%w", err)
		}
	}
	_ = os.Remove(backupExe)
	_ = os.RemoveAll(backupRuntime)
	return nil
}

func compareNumericVersions(left, right string) (int, bool) {
	a, okA := parseNumericVersion(left)
	b, okB := parseNumericVersion(right)
	if !okA || !okB {
		return 0, false
	}
	for i := 0; i < len(a) || i < len(b); i++ {
		var av, bv int
		if i < len(a) {
			av = a[i]
		}
		if i < len(b) {
			bv = b[i]
		}
		if av < bv {
			return -1, true
		}
		if av > bv {
			return 1, true
		}
	}
	return 0, true
}

func parseNumericVersion(value string) ([]int, bool) {
	value = strings.TrimSpace(strings.TrimPrefix(strings.ToLower(value), "v"))
	parts := strings.Split(value, ".")
	if len(parts) < 2 || len(parts) > 4 {
		return nil, false
	}
	result := make([]int, len(parts))
	for i, part := range parts {
		if part == "" {
			return nil, false
		}
		n, err := strconv.Atoi(part)
		if err != nil || n < 0 || n > 65535 {
			return nil, false
		}
		result[i] = n
	}
	return result, true
}

func getWindowsFileVersion(path string) string {
	versionDLL := syscall.NewLazyDLL("version.dll")
	getSize := versionDLL.NewProc("GetFileVersionInfoSizeW")
	getInfo := versionDLL.NewProc("GetFileVersionInfoW")
	queryValue := versionDLL.NewProc("VerQueryValueW")
	pathPtr, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return ""
	}
	size, _, _ := getSize.Call(uintptr(unsafe.Pointer(pathPtr)), 0)
	if size == 0 {
		return ""
	}
	data := make([]byte, size)
	ok, _, _ := getInfo.Call(uintptr(unsafe.Pointer(pathPtr)), 0, size, uintptr(unsafe.Pointer(&data[0])))
	if ok == 0 {
		return ""
	}
	root, _ := syscall.UTF16PtrFromString("\\")
	var block unsafe.Pointer
	var blockSize uint32
	ok, _, _ = queryValue.Call(
		uintptr(unsafe.Pointer(&data[0])),
		uintptr(unsafe.Pointer(root)),
		uintptr(unsafe.Pointer(&block)),
		uintptr(unsafe.Pointer(&blockSize)),
	)
	if ok == 0 || block == nil || blockSize < 16 {
		return ""
	}
	fixed := unsafe.Slice((*byte)(block), int(blockSize))
	if binary.LittleEndian.Uint32(fixed[0:4]) != 0xFEEF04BD {
		return ""
	}
	ms := binary.LittleEndian.Uint32(fixed[8:12])
	ls := binary.LittleEndian.Uint32(fixed[12:16])
	return fmt.Sprintf("%d.%d.%d.%d", ms>>16, ms&0xffff, ls>>16, ls&0xffff)
}
