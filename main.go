package main

import (
	"archive/zip"
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const (
	appName        = "ZZZ CNB 一键启动器"
	cnbRepo        = "https://cnb.cool/OneDragon-Anything/ZenlessZoneZero-OneDragon.git"
	cnbModelAssets = "https://cnb.cool/cc-public-assets/release-assets/-/releases/download/models"
)

var appVersion = "1.3.0"

var embeddedProjectConfig = `project_name: "ZenlessZoneZero-OneDragon"
python_version: "3.11"
github_homepage: "https://github.com/OneDragon-Anything/ZenlessZoneZero-OneDragon"
github_https_repository: "https://github.com/OneDragon-Anything/ZenlessZoneZero-OneDragon.git"
github_ssh_repository: "git@github.com:OneDragon-Anything/ZenlessZoneZero-OneDragon.git"
gitee_https_repository: "https://gitee.com/OneDragon-Anything/ZenlessZoneZero-OneDragon.git"
gitee_ssh_repository: "git@gitee.com:OneDragon-Anything/ZenlessZoneZero-OneDragon.git"
project_git_branch: "main"
manifest_path: "deploy/module_manifest.py"
game_executable_name: "ZenlessZoneZero.exe"
screen_standard_width: 1920
screen_standard_height: 1080
pip_source: "https://pypi.tuna.tsinghua.edu.cn/simple"
notice_url: "https://one-dragon.com/notice/zzz/notice.json"
qq_link: "https://pd.qq.com/g/onedrag00n"
quick_start_link: "http://one-dragon.com/zzz/zh/quickstart.html"
home_page_link: "https://one-dragon.com/zzz/zh/home.html"
doc_link: "https://docs.qq.com/doc/p/7add96a4600d363b75d2df83bb2635a7c6a969b5"
`

type modelSpec struct {
	Name          string
	Category      string
	Repository    string
	ReleaseTag    string
	GitHubURL     string
	GiteeURL      string
	SHA256        string
	RequiredFiles []string
	DictRequired  bool
}

var knownModels = map[string]modelSpec{
	"ppocrv6": {
		Name:          "ppocrv6",
		Category:      "onnx_ocr",
		Repository:    "OneDragon-Anything/OneDragon-Env",
		ReleaseTag:    "ppocrv6",
		GitHubURL:     "https://github.com/OneDragon-Anything/OneDragon-Env/releases/download/ppocrv6/ppocrv6.zip",
		GiteeURL:      "https://gitee.com/OneDragon-Anything/OneDragon-Env/releases/download/ppocrv6/ppocrv6.zip",
		SHA256:        "abe2ad83e8b684f4c905619e2271475d921683526dc3b6ab5448d8012f8e24c9",
		RequiredFiles: []string{"det.onnx", "rec.onnx", "cls.onnx", "simfang.ttf"},
		DictRequired:  true,
	},
	"ppocrv5": {
		Name:          "ppocrv5",
		Category:      "onnx_ocr",
		Repository:    "OneDragon-Anything/OneDragon-Env",
		ReleaseTag:    "ppocrv5",
		GitHubURL:     "https://github.com/OneDragon-Anything/OneDragon-Env/releases/download/ppocrv5/ppocrv5.zip",
		GiteeURL:      "https://gitee.com/OneDragon-Anything/OneDragon-Env/releases/download/ppocrv5/ppocrv5.zip",
		SHA256:        "2ab9d021ac7a760ea08d93ff044adff7b3aa5609d05c0e6f95e2327f320d9be3",
		RequiredFiles: []string{"det.onnx", "rec.onnx", "cls.onnx", "simfang.ttf"},
		DictRequired:  true,
	},
	"yolov8n-640-flash-20250921": {
		Name:          "yolov8n-640-flash-20250921",
		Category:      "flash_classifier",
		Repository:    "OneDragon-Anything/OneDragon-YOLO",
		ReleaseTag:    "zzz_model",
		GitHubURL:     "https://github.com/OneDragon-Anything/OneDragon-YOLO/releases/download/zzz_model/yolov8n-640-flash-20250921.zip",
		GiteeURL:      "https://gitee.com/OneDragon-Anything/OneDragon-YOLO/releases/download/zzz_model/yolov8n-640-flash-20250921.zip",
		SHA256:        "7985899ad431d7841c5e9294d7ea7e516e0d2ee59f4c5b53d7c82424a44b964a",
		RequiredFiles: []string{"model.onnx", "labels.csv"},
	},
	"yolov8s-736-hollow-zero-event-0126": {
		Name:          "yolov8s-736-hollow-zero-event-0126",
		Category:      "hollow_zero_event",
		Repository:    "OneDragon-Anything/OneDragon-YOLO",
		ReleaseTag:    "zzz_model",
		GitHubURL:     "https://github.com/OneDragon-Anything/OneDragon-YOLO/releases/download/zzz_model/yolov8s-736-hollow-zero-event-0126.zip",
		GiteeURL:      "https://gitee.com/OneDragon-Anything/OneDragon-YOLO/releases/download/zzz_model/yolov8s-736-hollow-zero-event-0126.zip",
		SHA256:        "c5eac21a5a8810025c8595b3366a2c3f94e9bad4d50efa5d567ecc7564d6813c",
		RequiredFiles: []string{"model.onnx", "labels.csv"},
	},
	"yolov26n-736-lost-void-det-20260630": {
		Name:          "yolov26n-736-lost-void-det-20260630",
		Category:      "lost_void_det",
		Repository:    "OneDragon-Anything/OneDragon-YOLO",
		ReleaseTag:    "zzz_model",
		GitHubURL:     "https://github.com/OneDragon-Anything/OneDragon-YOLO/releases/download/zzz_model/yolov26n-736-lost-void-det-20260630.zip",
		GiteeURL:      "https://gitee.com/OneDragon-Anything/OneDragon-YOLO/releases/download/zzz_model/yolov26n-736-lost-void-det-20260630.zip",
		SHA256:        "08170eb2ac3e167578c01700d9952c82f2b808f85e423bd1e4a3139a40c626b0",
		RequiredFiles: []string{"model.onnx", "labels.csv"},
	},
}

var defaultModelNames = map[string]string{
	"ocr":               "ppocrv5",
	"latest_ocr":        "ppocrv6",
	"flash_classifier":  "yolov8n-640-flash-20250921",
	"hollow_zero_event": "yolov8s-736-hollow-zero-event-0126",
	"lost_void_det":     "yolov26n-736-lost-void-det-20260630",
}

type options struct {
	root               string
	prepareOnly        bool
	skipModels         bool
	skipLauncherUpdate bool
	disableTelemetry   bool
	showVersion        bool
	noPause            bool
}

func main() {
	setUTF8Console()
	if len(os.Args) > 1 && os.Args[1] == "--self-replace" {
		if err := runSelfReplacement(); err != nil {
			fmt.Printf("\n[自更新失败] %v\n", err)
		}
		return
	}
	opts := parseFlags()
	if opts.showVersion {
		fmt.Printf("%s v%s\n", appName, strings.TrimPrefix(appVersion, "v"))
		return
	}
	telemetry := newTelemetryClient(opts.disableTelemetry)
	if telemetry.enabled {
		fmt.Println("[遥测] 仅发送匿名运行结果；可用 --disable-telemetry 或同目录禁用标记关闭。")
	}
	telemetry.record("app_start", "startup", "started", "", "", time.Time{})
	if err := run(opts, telemetry); err != nil {
		if errors.Is(err, errSelfUpdateStarted) {
			telemetry.flush(1500 * time.Millisecond)
			return
		}
		telemetry.record("app_finish", "run", "failure", telemetryErrorCode(err), "", time.Time{})
		fmt.Printf("\n[失败] %v\n", err)
		if !opts.noPause {
			pause()
		}
		telemetry.flush(1500 * time.Millisecond)
		os.Exit(1)
	}
	telemetry.record("app_finish", "run", "success", "", "", time.Time{})
	telemetry.flush(1500 * time.Millisecond)
}

func parseFlags() options {
	var opts options
	flag.StringVar(&opts.root, "root", "", "ZZZ 一条龙安装目录")
	flag.BoolVar(&opts.prepareOnly, "prepare-only", false, "只转换和下载，不启动主程序")
	flag.BoolVar(&opts.skipModels, "skip-models", false, "跳过模型检查与下载")
	flag.BoolVar(&opts.skipLauncherUpdate, "skip-launcher-update", false, "跳过原版启动器更新检查")
	flag.BoolVar(&opts.disableTelemetry, "disable-telemetry", false, "关闭匿名运行遥测")
	flag.BoolVar(&opts.showVersion, "version", false, "显示版本号")
	flag.BoolVar(&opts.noPause, "no-pause", false, "出错时不等待按键")
	flag.Parse()
	return opts
}

func run(opts options, telemetry *telemetryClient) error {
	root, err := resolveRoot(opts.root)
	if err != nil {
		return err
	}

	fmt.Printf("%s\n目录：%s\n\n", appName, root)
	started := time.Now()
	if updated, updateErr := ensureSelfCurrent(root); updateErr != nil {
		telemetry.record("self_update_check", "self_update", "failure", telemetryErrorCode(updateErr), "", started)
		fmt.Printf("[自更新] 暂时无法检查：%v\n", updateErr)
	} else if updated {
		telemetry.record("self_update_check", "self_update", "success", "", "", started)
		fmt.Println("[自更新] 已下载新版，程序将自动重启。")
		return errSelfUpdateStarted
	} else {
		telemetry.record("self_update_check", "self_update", "success", "", "", started)
	}
	launcher, err := findLauncher(root)
	if err != nil {
		return err
	}

	fmt.Println("[1/4] 检查代码源并保持自动更新...")
	started = time.Now()
	if err := configureCNB(root); err != nil {
		telemetry.record("configuration_result", "configure", "failure", telemetryErrorCode(err), "", started)
		return fmt.Errorf("配置 CNB 失败：%w", err)
	}
	telemetry.record("configuration_result", "configure", "success", "", "", started)
	fmt.Println("      已完成。以后请继续通过本启动器打开一条龙。")

	if opts.skipModels {
		telemetry.record("model_prepare_result", "models", "skipped", "", "", time.Time{})
		fmt.Println("[2/4] 已按参数跳过模型检查。")
	} else {
		fmt.Println("[2/4] 检查模型，已有完整文件不会重复下载...")
		started = time.Now()
		if err := ensureModels(root); err != nil {
			telemetry.record("model_prepare_result", "models", "failure", telemetryErrorCode(err), "", started)
			return fmt.Errorf("模型准备失败：%w", err)
		}
		telemetry.record("model_prepare_result", "models", "success", "", "", started)
	}

	if opts.skipLauncherUpdate {
		telemetry.record("launcher_update_result", "launcher_update", "skipped", "", "", time.Time{})
		fmt.Println("[3/4] 已按参数跳过原版启动器更新检查。")
	} else {
		fmt.Println("[3/4] 检查原版启动器更新...")
		started = time.Now()
		updated, updateErr := ensureLauncherCurrent(root, launcher)
		if updateErr != nil {
			telemetry.record("launcher_update_result", "launcher_update", "failure", telemetryErrorCode(updateErr), "", started)
			fmt.Printf("      [提示] 暂时无法更新原版启动器：%v\n", updateErr)
			fmt.Println("      将继续使用当前版本，不影响代码和模型自动更新。")
		} else if updated {
			telemetry.record("launcher_update_result", "launcher_update", "success", "", "", started)
			fmt.Println("      [完成] 原版启动器已更新。")
		} else {
			telemetry.record("launcher_update_result", "launcher_update", "success", "", "", started)
		}
	}

	if opts.prepareOnly {
		telemetry.record("zzz_launch_result", "launch", "skipped", "", "", time.Time{})
		fmt.Println("[4/4] 准备完成（未启动主程序）。")
		return nil
	}

	fmt.Printf("[4/4] 启动 %s...\n", filepath.Base(launcher))
	started = time.Now()
	if err := startLauncher(launcher, root); err != nil {
		telemetry.record("zzz_launch_result", "launch", "failure", telemetryErrorCode(err), "", started)
		return fmt.Errorf("无法启动主程序：%w", err)
	}
	telemetry.record("zzz_launch_result", "launch", "success", "", "", started)
	return nil
}

func resolveRoot(requested string) (string, error) {
	if requested != "" {
		return filepath.Abs(requested)
	}
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("无法确定启动器位置：%w", err)
	}
	return filepath.Dir(exe), nil
}

func findLauncher(root string) (string, error) {
	candidates := []string{
		"OneDragon-RuntimeLauncher.exe",
		"OneDragon-Launcher.exe",
		"ZenlessZoneZero-OneDragon.exe",
	}
	for _, name := range candidates {
		path := filepath.Join(root, name)
		if fileExists(path) {
			return path, nil
		}
	}
	return "", errors.New("没有找到 ZZZ 一条龙启动程序。请把本 EXE 放到 OneDragon-RuntimeLauncher.exe 所在目录")
}

func configureCNB(root string) error {
	configDir := filepath.Join(root, "config")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		return err
	}

	nativeRepository := hasNativeRepositoryConfig(root)
	if !nativeRepository {
		projectPath := filepath.Join(configDir, "project.yml")
		projectData, err := loadProjectConfig(root, projectPath)
		if err != nil {
			return err
		}
		projectData = upsertYAML(projectData, "gitee_https_repository", quoteYAML(cnbRepo))
		if err := atomicWrite(projectPath, []byte(projectData)); err != nil {
			return err
		}
	} else if err := repairLegacyProjectConfig(filepath.Join(configDir, "project.yml")); err != nil {
		return err
	}

	envPath := filepath.Join(configDir, "env.yml")
	envData := ""
	if data, readErr := os.ReadFile(envPath); readErr == nil {
		envData = string(data)
	} else if !os.IsNotExist(readErr) {
		return readErr
	}
	if !nativeRepository {
		envData = upsertYAML(envData, "repository_type", "Gitee")
		envData = upsertYAML(envData, "git_remote", "origin")
		envData = upsertYAML(envData, "git_branch", "main")
	} else {
		envData = removeTopLevelYAMLKey(envData, "repository_type")
	}
	envData = upsertYAML(envData, "auto_update_code", "true")
	return atomicWrite(envPath, []byte(envData))
}

func repairLegacyProjectConfig(path string) error {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	original := string(data)
	lineRE := regexp.MustCompile(`(?m)^gitee_https_repository\s*:\s*["']?` + regexp.QuoteMeta(cnbRepo) + `["']?\s*\r?\n?`)
	repaired := lineRE.ReplaceAllString(original, "")
	if repaired == original {
		return nil
	}
	return atomicWrite(path, []byte(repaired))
}

func removeTopLevelYAMLKey(content, key string) string {
	lineRE := regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(key) + `\s*:[^\r\n]*(?:\r?\n|$)`)
	return lineRE.ReplaceAllString(content, "")
}

func hasNativeRepositoryConfig(root string) bool {
	paths := []string{
		filepath.Join(root, "config", "repository.yml"),
		filepath.Join(root, ".runtime", "resources", "config", "repository.yml"),
		filepath.Join(root, "resources", "config", "repository.yml"),
	}
	for _, path := range paths {
		if fileExists(path) {
			return true
		}
	}
	return false
}

func loadProjectConfig(root, projectPath string) (string, error) {
	if data, err := os.ReadFile(projectPath); err == nil {
		return string(data), nil
	} else if !os.IsNotExist(err) {
		return "", err
	}

	fallbacks := []string{
		filepath.Join(root, ".runtime", "resources", "config", "project.yml"),
		filepath.Join(root, "resources", "config", "project.yml"),
	}
	for _, path := range fallbacks {
		if data, err := os.ReadFile(path); err == nil {
			return string(data), nil
		}
	}
	return embeddedProjectConfig, nil
}

func upsertYAML(content, key, value string) string {
	// 只改顶层键，避免同名嵌套字段被正则误写导致配置结构损坏。
	lineRE := regexp.MustCompile(`(?m)^(` + regexp.QuoteMeta(key) + `[ \t]*:)[^\r\n]*(\r?)$`)
	if lineRE.MatchString(content) {
		return lineRE.ReplaceAllString(content, "${1} "+value+"${2}")
	}
	if content != "" && !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	return content + key + ": " + value + "\n"
}

func quoteYAML(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `\"`) + `"`
}

func atomicWrite(path string, data []byte) error {
	tmp := path + ".zzz-cnb.tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	if !fileExists(path) {
		return os.Rename(tmp, path)
	}

	backup := path + ".zzz-cnb.bak"
	_ = os.Remove(backup)
	if err := os.Rename(path, backup); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, path); err == nil {
		_ = os.Remove(backup)
		return nil
	}
	_ = os.Rename(backup, path)
	_ = os.Remove(tmp)
	return errors.New("unable to replace configuration; original file was restored")
}

func ensureModels(root string) error {
	latest := latestModelNames(root)
	for _, request := range modelsToEnsure(latest) {
		spec := buildModelSpec(request.key, request.name, latest["yolo_release_tag"])

		target := filepath.Join(root, "assets", "models", spec.Category, spec.Name)
		if modelComplete(target, spec) {
			fmt.Printf("      [已有] %s\n", spec.Name)
			continue
		}

		fmt.Printf("      [下载] %s\n", spec.Name)
		if err := downloadAndInstallModel(root, target, spec); err != nil {
			return err
		}
		fmt.Printf("      [完成] %s\n", spec.Name)
	}
	if err := configureLatestModels(root, latest); err != nil {
		return fmt.Errorf("切换最新版模型失败：%w", err)
	}
	fmt.Println("      [配置] 已切换为全部最新版模型。")
	return nil
}

type modelRequest struct {
	key  string
	name string
}

func modelsToEnsure(selected map[string]string) []modelRequest {
	requests := []modelRequest{
		{key: "ocr", name: selected["latest_ocr"]},
		{key: "flash_classifier", name: selected["flash_classifier"]},
		{key: "hollow_zero_event", name: selected["hollow_zero_event"]},
		{key: "lost_void_det", name: selected["lost_void_det"]},
	}
	seen := make(map[string]bool)
	result := make([]modelRequest, 0, len(requests))
	for _, request := range requests {
		if request.name == "" {
			continue
		}
		id := request.key + "\x00" + request.name
		if seen[id] {
			continue
		}
		seen[id] = true
		result = append(result, request)
	}
	return result
}

func configureLatestModels(root string, latest map[string]string) error {
	path := filepath.Join(root, "config", "model.yml")
	content := ""
	if data, err := os.ReadFile(path); err == nil {
		content = string(data)
	} else if !os.IsNotExist(err) {
		return err
	}
	values := []struct {
		key   string
		value string
	}{
		{key: "ocr", value: latest["latest_ocr"]},
		{key: "flash_classifier", value: latest["flash_classifier"]},
		{key: "hollow_zero_event", value: latest["hollow_zero_event"]},
		{key: "lost_void_det", value: latest["lost_void_det"]},
	}
	for _, item := range values {
		if item.value == "" {
			return fmt.Errorf("最新版模型声明缺少 %s", item.key)
		}
		content = upsertYAML(content, item.key, quoteYAML(item.value))
	}
	return atomicWrite(path, []byte(content))
}

func latestModelNames(root string) map[string]string {
	result := map[string]string{}
	for k, v := range defaultModelNames {
		result[k] = v
	}
	result["yolo_release_tag"] = "zzz_model"

	// Follow model renames made by future OneDragon updates.
	modelSource := filepath.Join(root, "src", "zzz_od", "config", "model_config.py")
	if data, err := os.ReadFile(modelSource); err == nil {
		text := string(data)
		pythonConstants := map[string]string{
			"flash_classifier":  "_DEFAULT_FLASH_CLASSIFIER",
			"hollow_zero_event": "_DEFAULT_HOLLOW_ZERO_EVENT",
			"lost_void_det":     "_DEFAULT_LOST_VOID_DET",
			"yolo_release_tag":  "YOLO_RELEASE_TAG",
		}
		for key, constant := range pythonConstants {
			if value := parsePythonStringConstant(text, constant); value != "" {
				result[key] = value
			}
		}
	}
	ocrSource := filepath.Join(root, "src", "one_dragon", "base", "matcher", "ocr", "onnx_ocr_matcher.py")
	if data, err := os.ReadFile(ocrSource); err == nil {
		updateOCRModelNames(result, string(data))
	}

	// Read the latest lightweight model declarations from the official CNB
	// mirror before OneDragon performs its full Git update. This prevents a
	// newly released model from being discovered one launch too late.
	remoteFiles := []struct {
		url       string
		constants map[string]string
		ocrModels bool
	}{
		{
			url: "https://cnb.cool/OneDragon-Anything/ZenlessZoneZero-OneDragon/-/git/raw/main/src/zzz_od/config/model_config.py",
			constants: map[string]string{
				"flash_classifier":  "_DEFAULT_FLASH_CLASSIFIER",
				"hollow_zero_event": "_DEFAULT_HOLLOW_ZERO_EVENT",
				"lost_void_det":     "_DEFAULT_LOST_VOID_DET",
				"yolo_release_tag":  "YOLO_RELEASE_TAG",
			},
		},
		{
			url:       "https://cnb.cool/OneDragon-Anything/ZenlessZoneZero-OneDragon/-/git/raw/main/src/one_dragon/base/matcher/ocr/onnx_ocr_matcher.py",
			ocrModels: true,
		},
	}
	for _, remote := range remoteFiles {
		if text, err := fetchText(remote.url, 8*time.Second, 512*1024); err == nil {
			if remote.ocrModels {
				updateOCRModelNames(result, text)
			}
			for key, constant := range remote.constants {
				if value := parsePythonStringConstant(text, constant); value != "" {
					result[key] = value
				}
			}
		}
	}

	return result
}

func fetchText(url string, timeout time.Duration, maxBytes int64) (string, error) {
	client := &http.Client{Timeout: timeout}
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "ZZZ-CNB-Launcher/1.0")
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
	if err != nil {
		return "", err
	}
	if int64(len(data)) > maxBytes {
		return "", errors.New("response exceeds size limit")
	}
	return string(data), nil
}

func parsePythonStringConstant(content, name string) string {
	re := regexp.MustCompile(`(?m)^\s*` + regexp.QuoteMeta(name) + `(?:\s*:\s*[^=]+)?\s*=\s*["']([^"']+)["']`)
	match := re.FindStringSubmatch(content)
	if len(match) != 2 {
		return ""
	}
	return strings.TrimSpace(match[1])
}

func updateOCRModelNames(result map[string]string, content string) {
	if value := parsePythonStringConstant(content, "DEFAULT_OCR_MODEL_NAME"); value != "" {
		result["ocr"] = value
	}
	if value := parseLatestOCRModelName(content); value != "" {
		result["latest_ocr"] = value
	}
}

func parseLatestOCRModelName(content string) string {
	re := regexp.MustCompile(`(?m)^\s*PPOCRV(\d+)_MODEL_NAME(?:\s*:\s*[^=]+)?\s*=\s*["']([^"']+)["']`)
	latestVersion := -1
	latestName := ""
	for _, match := range re.FindAllStringSubmatch(content, -1) {
		version, err := strconv.Atoi(match[1])
		if err == nil && version > latestVersion {
			latestVersion = version
			latestName = strings.TrimSpace(match[2])
		}
	}
	return latestName
}

func buildModelSpec(key, name, yoloTag string) modelSpec {
	if known, ok := knownModels[name]; ok {
		return known
	}
	if key == "ocr" {
		return modelSpec{
			Name:          name,
			Category:      "onnx_ocr",
			Repository:    "OneDragon-Anything/OneDragon-Env",
			ReleaseTag:    name,
			GitHubURL:     fmt.Sprintf("https://github.com/OneDragon-Anything/OneDragon-Env/releases/download/%s/%s.zip", name, name),
			GiteeURL:      fmt.Sprintf("https://gitee.com/OneDragon-Anything/OneDragon-Env/releases/download/%s/%s.zip", name, name),
			RequiredFiles: []string{"det.onnx", "rec.onnx", "cls.onnx", "simfang.ttf"},
			DictRequired:  true,
		}
	}
	if yoloTag == "" {
		yoloTag = "zzz_model"
	}
	category := key
	return modelSpec{
		Name:          name,
		Category:      category,
		Repository:    "OneDragon-Anything/OneDragon-YOLO",
		ReleaseTag:    yoloTag,
		GitHubURL:     fmt.Sprintf("https://github.com/OneDragon-Anything/OneDragon-YOLO/releases/download/%s/%s.zip", yoloTag, name),
		GiteeURL:      fmt.Sprintf("https://gitee.com/OneDragon-Anything/OneDragon-YOLO/releases/download/%s/%s.zip", yoloTag, name),
		RequiredFiles: []string{"model.onnx", "labels.csv"},
	}
}

func modelComplete(dir string, spec modelSpec) bool {
	for _, name := range spec.RequiredFiles {
		info, err := os.Stat(filepath.Join(dir, name))
		if err != nil || info.IsDir() || info.Size() == 0 {
			return false
		}
	}
	if spec.DictRequired {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return false
		}
		found := false
		for _, entry := range entries {
			if !entry.IsDir() && strings.HasSuffix(strings.ToLower(entry.Name()), "_dict.txt") {
				info, statErr := entry.Info()
				if statErr == nil && info.Size() > 0 {
					found = true
					break
				}
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func downloadAndInstallModel(root, target string, spec modelSpec) error {
	downloadDir := filepath.Join(root, "assets", "models", ".zzz-cnb-downloads")
	if err := os.MkdirAll(downloadDir, 0755); err != nil {
		return err
	}
	zipPath := filepath.Join(downloadDir, spec.Name+".zip")

	expectedHash := spec.SHA256
	if expectedHash == "" {
		fmt.Printf("             检测到新模型，正在获取官方 SHA-256...\n")
		expectedHash = resolveReleaseDigest(root, spec)
	}

	if fileExists(zipPath) && expectedHash != "" {
		if ok, _ := verifySHA256(zipPath, expectedHash); !ok {
			_ = os.Remove(zipPath)
		}
	}

	if !fileExists(zipPath) {
		var lastErr error
		for _, url := range modelURLs(root, spec) {
			fmt.Printf("             尝试 %s\n", shortURL(url))
			if err := downloadFile(url, zipPath); err != nil {
				lastErr = err
				fmt.Printf("             失败：%v\n", err)
				continue
			}
			// A direct download from the project's official Gitee account is a
			// trusted fallback when GitHub's digest API is unreachable.
			if expectedHash == "" && url == spec.GiteeURL {
				trustedHash, hashErr := fileSHA256(zipPath)
				if hashErr == nil {
					expectedHash = trustedHash
					fmt.Printf("             已由官方 Gitee 文件建立 SHA-256 校验。\n")
					lastErr = nil
					break
				}
			}
			if expectedHash == "" {
				_ = os.Remove(zipPath)
				lastErr = errors.New("无法取得官方 SHA-256，拒绝使用第三方镜像文件")
				continue
			}
			ok, err := verifySHA256(zipPath, expectedHash)
			if err == nil && ok {
				lastErr = nil
				break
			}
			_ = os.Remove(zipPath)
			if err != nil {
				lastErr = err
			} else {
				lastErr = errors.New("SHA-256 与官方文件不一致")
			}
			fmt.Printf("             拒绝：%v\n", lastErr)
		}
		if lastErr != nil || !fileExists(zipPath) {
			return fmt.Errorf("所有下载源均不可用：%s（最后错误：%v）", spec.Name, lastErr)
		}
	}

	tmpDir := target + ".zzz-cnb-new"
	_ = os.RemoveAll(tmpDir)
	if err := os.MkdirAll(tmpDir, 0755); err != nil {
		return err
	}
	if err := extractExpected(zipPath, tmpDir, spec); err != nil {
		_ = os.RemoveAll(tmpDir)
		_ = os.Remove(zipPath)
		return err
	}
	if !modelComplete(tmpDir, spec) {
		_ = os.RemoveAll(tmpDir)
		return errors.New("解压后模型文件不完整")
	}

	_ = os.RemoveAll(target)
	if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
		return err
	}
	if err := os.Rename(tmpDir, target); err != nil {
		return err
	}
	return nil
}

func modelURLs(root string, spec modelSpec) []string {
	urls := []string{cnbModelAssets + "/" + spec.Name + ".zip"}
	if spec.GiteeURL != "" {
		urls = append(urls, spec.GiteeURL)
	}

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
	if dynamic := discoverRecommendedProxy(); dynamic != "" {
		templates = append([]string{strings.TrimRight(dynamic, "/") + "/{url}"}, templates...)
	}
	for _, template := range templates {
		urls = append(urls, strings.ReplaceAll(template, "{url}", spec.GitHubURL))
	}
	return uniqueStrings(urls)
}

func discoverRecommendedProxy() string {
	client := &http.Client{Timeout: 5 * time.Second}
	req, _ := http.NewRequest(http.MethodGet, "https://ghproxy.link/js/src_views_home_HomeView_vue.js", nil)
	req.Header.Set("User-Agent", "Mozilla/5.0 ZZZ-CNB-Launcher/1.0")
	resp, err := client.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ""
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024))
	if err != nil {
		return ""
	}
	re := regexp.MustCompile(`https://[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}`)
	for _, candidate := range re.FindAllString(string(data), -1) {
		host := strings.TrimPrefix(candidate, "https://")
		blocked := host == "github.com" || host == "api.github.com" ||
			strings.HasSuffix(host, ".github.com") || strings.Contains(host, "ghproxy.link")
		if !blocked && (strings.Contains(host, "gh") || strings.Contains(host, "proxy")) {
			return candidate
		}
	}
	return ""
}

type githubRelease struct {
	Assets []struct {
		Name   string `json:"name"`
		Digest string `json:"digest"`
	} `json:"assets"`
}

func resolveReleaseDigest(root string, spec modelSpec) string {
	if text, err := fetchText(cnbModelAssets+"/model-digests.json", 8*time.Second, 256*1024); err == nil {
		if digest := parseModelDigestManifest(text, spec.Name+".zip"); digest != "" {
			return digest
		}
	}

	apiURL := fmt.Sprintf("https://api.github.com/repos/%s/releases/tags/%s", spec.Repository, spec.ReleaseTag)
	urls := []string{apiURL, "https://gh-proxy.com/" + apiURL}
	for _, template := range customMirrorTemplates(root) {
		urls = append(urls, strings.ReplaceAll(template, "{url}", apiURL))
	}
	for _, url := range uniqueStrings(urls) {
		client := &http.Client{Timeout: 12 * time.Second}
		req, err := http.NewRequest(http.MethodGet, url, nil)
		if err != nil {
			continue
		}
		req.Header.Set("User-Agent", "ZZZ-CNB-Launcher/1.0")
		req.Header.Set("Accept", "application/vnd.github+json")
		resp, err := client.Do(req)
		if err != nil {
			continue
		}
		var release githubRelease
		decodeErr := json.NewDecoder(io.LimitReader(resp.Body, 8*1024*1024)).Decode(&release)
		resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 || decodeErr != nil {
			continue
		}
		for _, asset := range release.Assets {
			if asset.Name == spec.Name+".zip" && strings.HasPrefix(strings.ToLower(asset.Digest), "sha256:") {
				digest := strings.TrimPrefix(strings.ToLower(asset.Digest), "sha256:")
				if len(digest) == 64 {
					return digest
				}
			}
		}
	}
	return ""
}

func parseModelDigestManifest(content, assetName string) string {
	var digests map[string]string
	if err := json.Unmarshal([]byte(content), &digests); err != nil {
		return ""
	}
	digest := strings.ToLower(strings.TrimSpace(digests[assetName]))
	digest = strings.TrimPrefix(digest, "sha256:")
	if matched, _ := regexp.MatchString(`^[0-9a-f]{64}$`, digest); matched {
		return digest
	}
	return ""
}

func customMirrorTemplates(root string) []string {
	path := filepath.Join(root, "ZZZ-CNB-Launcher.mirrors.txt")
	file, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer file.Close()

	var result []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") || !strings.Contains(line, "{url}") {
			continue
		}
		result = append(result, line)
	}
	return result
}

func downloadFile(url, path string) error {
	partial := path + ".part"
	_ = os.Remove(partial)

	client := &http.Client{Timeout: 3 * time.Minute}
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "ZZZ-CNB-Launcher/1.0")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	out, err := os.Create(partial)
	if err != nil {
		return err
	}
	defer out.Close()

	reader := &progressReader{reader: resp.Body, total: resp.ContentLength}
	if _, err := io.Copy(out, reader); err != nil {
		_ = os.Remove(partial)
		return err
	}
	fmt.Print("\r             下载完成                         \n")
	if err := out.Close(); err != nil {
		return err
	}
	return os.Rename(partial, path)
}

type progressReader struct {
	reader   io.Reader
	total    int64
	read     int64
	lastDraw time.Time
}

func (p *progressReader) Read(buf []byte) (int, error) {
	n, err := p.reader.Read(buf)
	p.read += int64(n)
	if time.Since(p.lastDraw) > 250*time.Millisecond {
		if p.total > 0 {
			fmt.Printf("\r             %.1f / %.1f MiB (%d%%)", float64(p.read)/1048576, float64(p.total)/1048576, p.read*100/p.total)
		} else {
			fmt.Printf("\r             已下载 %.1f MiB", float64(p.read)/1048576)
		}
		p.lastDraw = time.Now()
	}
	return n, err
}

func verifySHA256(path, expected string) (bool, error) {
	actual, err := fileSHA256(path)
	if err != nil {
		return false, err
	}
	return strings.EqualFold(actual, expected), nil
}

func fileSHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func extractExpected(zipPath, target string, spec modelSpec) error {
	reader, err := zip.OpenReader(zipPath)
	if err != nil {
		return fmt.Errorf("ZIP 无法打开：%w", err)
	}
	defer reader.Close()

	wanted := map[string]bool{}
	for _, name := range spec.RequiredFiles {
		wanted[strings.ToLower(name)] = true
	}
	for _, entry := range reader.File {
		base := filepath.Base(filepath.FromSlash(entry.Name))
		lower := strings.ToLower(base)
		if !wanted[lower] && !(spec.DictRequired && strings.HasSuffix(lower, "_dict.txt")) {
			continue
		}
		if entry.FileInfo().IsDir() || entry.UncompressedSize64 > 2*1024*1024*1024 {
			continue
		}
		destination := filepath.Join(target, base)
		if err := extractOne(entry, destination); err != nil {
			return err
		}
	}
	return nil
}

func extractOne(entry *zip.File, destination string) error {
	in, err := entry.Open()
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(destination, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
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

func shortURL(value string) string {
	if len(value) <= 100 {
		return value
	}
	return value[:97] + "..."
}

func uniqueStrings(values []string) []string {
	seen := map[string]bool{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
	}
	return result
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func pause() {
	fmt.Print("\n按回车键关闭...")
	_, _ = bufio.NewReader(os.Stdin).ReadString('\n')
}

func setUTF8Console() {
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	setOutputCP := kernel32.NewProc("SetConsoleOutputCP")
	setInputCP := kernel32.NewProc("SetConsoleCP")
	_, _, _ = setOutputCP.Call(65001)
	_, _, _ = setInputCP.Call(65001)
}
