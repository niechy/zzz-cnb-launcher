package main

import (
	"archive/zip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUpsertYAML(t *testing.T) {
	input := "repository_type: GitHub\nauto_update_code: false\n"
	actual := upsertYAML(input, "auto_update_code", "true")
	actual = upsertYAML(actual, "git_branch", "main")
	if !strings.Contains(actual, "auto_update_code: true") {
		t.Fatalf("value was not replaced: %q", actual)
	}
	if !strings.Contains(actual, "git_branch: main") {
		t.Fatalf("value was not added: %q", actual)
	}
}

func TestUpsertYAMLDoesNotModifyNestedKey(t *testing.T) {
	input := "group:\n  auto_update_code: false\nauto_update_code: false\n"
	actual := upsertYAML(input, "auto_update_code", "true")
	if !strings.Contains(actual, "  auto_update_code: false") {
		t.Fatalf("nested key was modified: %q", actual)
	}
	if !strings.Contains(actual, "\nauto_update_code: true") {
		t.Fatalf("top-level key was not modified: %q", actual)
	}
}

func TestConfigureCNB(t *testing.T) {
	root := t.TempDir()
	runtimeConfig := filepath.Join(root, ".runtime", "resources", "config")
	if err := os.MkdirAll(runtimeConfig, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(runtimeConfig, "project.yml"), []byte(embeddedProjectConfig), 0644); err != nil {
		t.Fatal(err)
	}
	if err := configureCNB(root); err != nil {
		t.Fatal(err)
	}
	project, err := os.ReadFile(filepath.Join(root, "config", "project.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(project), cnbRepo) {
		t.Fatalf("CNB URL missing: %s", project)
	}
	env, err := os.ReadFile(filepath.Join(root, "config", "env.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(env), "auto_update_code: true") {
		t.Fatalf("auto update not enabled: %s", env)
	}
}

func TestConfigureCNBLeavesNativeRepositoryConfigUntouched(t *testing.T) {
	root := t.TempDir()
	configDir := filepath.Join(root, "config")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		t.Fatal(err)
	}
	repository := []byte("repositories:\n  primary: github\n")
	if err := os.WriteFile(filepath.Join(configDir, "repository.yml"), repository, 0644); err != nil {
		t.Fatal(err)
	}
	if err := configureCNB(root); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(configDir, "project.yml")); !os.IsNotExist(err) {
		t.Fatalf("native repository config should not create legacy project.yml: %v", err)
	}
	env, err := os.ReadFile(filepath.Join(configDir, "env.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(env), "repository_type:") {
		t.Fatalf("native repository config should not write repository_type: %s", env)
	}
	if !strings.Contains(string(env), "auto_update_code: true") {
		t.Fatalf("auto update not enabled: %s", env)
	}
}

func TestConfigureCNBRepairsVersion11Mutations(t *testing.T) {
	root := t.TempDir()
	configDir := filepath.Join(root, "config")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "repository.yml"), []byte("repositories:\n  primary: github\n"), 0644); err != nil {
		t.Fatal(err)
	}
	project := "project_name: zzz\ngitee_https_repository: \"" + cnbRepo + "\"\n"
	if err := os.WriteFile(filepath.Join(configDir, "project.yml"), []byte(project), 0644); err != nil {
		t.Fatal(err)
	}
	env := "repository_type: Gitee\nrepository_url: auto\ngit_branch: test\nauto_update_code: false\n"
	if err := os.WriteFile(filepath.Join(configDir, "env.yml"), []byte(env), 0644); err != nil {
		t.Fatal(err)
	}
	if err := configureCNB(root); err != nil {
		t.Fatal(err)
	}
	projectData, _ := os.ReadFile(filepath.Join(configDir, "project.yml"))
	if strings.Contains(string(projectData), "gitee_https_repository") {
		t.Fatalf("legacy project mutation remains: %s", projectData)
	}
	envData, _ := os.ReadFile(filepath.Join(configDir, "env.yml"))
	content := string(envData)
	if strings.Contains(content, "repository_type") {
		t.Fatalf("legacy env mutation remains: %s", envData)
	}
	if !strings.Contains(content, "git_branch: test") || !strings.Contains(content, "repository_url: auto") {
		t.Fatalf("user settings were changed: %s", envData)
	}
	if !strings.Contains(content, "auto_update_code: true") {
		t.Fatalf("auto update was not preserved: %s", envData)
	}
}

func TestModelComplete(t *testing.T) {
	dir := t.TempDir()
	spec := knownModels["yolov8n-640-flash-20250921"]
	if modelComplete(dir, spec) {
		t.Fatal("empty directory must not be complete")
	}
	for _, name := range spec.RequiredFiles {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	if !modelComplete(dir, spec) {
		t.Fatal("required model files should be complete")
	}
}

func TestAtomicWriteReplacesExistingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "env.yml")
	if err := os.WriteFile(path, []byte("old"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := atomicWrite(path, []byte("new")); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "new" {
		t.Fatalf("unexpected content: %q", data)
	}
	if fileExists(path + ".zzz-cnb.bak") {
		t.Fatal("backup should be removed after a successful replacement")
	}
}

func TestParsePythonStringConstant(t *testing.T) {
	source := "_DEFAULT_MODEL: str = 'model-20270101'\n"
	if actual := parsePythonStringConstant(source, "_DEFAULT_MODEL"); actual != "model-20270101" {
		t.Fatalf("unexpected model name: %q", actual)
	}
}

func TestParseLatestOCRModelName(t *testing.T) {
	source := "DEFAULT_OCR_MODEL_NAME = 'ppocrv5'\nPPOCRV6_MODEL_NAME: str = 'ppocrv6'\nPPOCRV12_MODEL_NAME = 'ppocrv12'\n"
	if actual := parseLatestOCRModelName(source); actual != "ppocrv12" {
		t.Fatalf("unexpected latest OCR model: %q", actual)
	}
}

func TestModelsToEnsureUsesOnlyLatestModels(t *testing.T) {
	selected := map[string]string{
		"latest_ocr":        "ppocrv6",
		"flash_classifier":  "flash",
		"hollow_zero_event": "hollow",
		"lost_void_det":     "lost",
	}
	requests := modelsToEnsure(selected)
	var names []string
	for _, request := range requests {
		names = append(names, request.name)
	}
	actual := strings.Join(names, ",")
	if actual != "ppocrv6,flash,hollow,lost" {
		t.Fatalf("unexpected model queue: %s", actual)
	}
}

func TestConfigureLatestModels(t *testing.T) {
	root := t.TempDir()
	configDir := filepath.Join(root, "config")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(configDir, "model.yml")
	if err := os.WriteFile(path, []byte("ocr: ppocrv5\nflash_classifier_gpu: true\n"), 0644); err != nil {
		t.Fatal(err)
	}
	latest := map[string]string{
		"latest_ocr":        "ppocrv6",
		"flash_classifier":  "flash-new",
		"hollow_zero_event": "hollow-new",
		"lost_void_det":     "lost-new",
	}
	if err := configureLatestModels(root, latest); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	for _, expected := range []string{
		`ocr: "ppocrv6"`,
		`flash_classifier: "flash-new"`,
		`hollow_zero_event: "hollow-new"`,
		`lost_void_det: "lost-new"`,
		"flash_classifier_gpu: true",
	} {
		if !strings.Contains(content, expected) {
			t.Fatalf("missing %q in %q", expected, content)
		}
	}
}

func TestParseModelDigestManifest(t *testing.T) {
	content := `{"ppocrv7.zip":"sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"}`
	expected := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	if actual := parseModelDigestManifest(content, "ppocrv7.zip"); actual != expected {
		t.Fatalf("unexpected digest: %q", actual)
	}
	if actual := parseModelDigestManifest(content, "missing.zip"); actual != "" {
		t.Fatalf("unexpected missing digest: %q", actual)
	}
}

func TestCompareNumericVersions(t *testing.T) {
	if actual, ok := compareNumericVersions("2.4.6.0", "v2.4.6"); !ok || actual != 0 {
		t.Fatalf("expected equal versions, got %d %v", actual, ok)
	}
	if actual, ok := compareNumericVersions("2.5.0", "v2.4.6"); !ok || actual <= 0 {
		t.Fatalf("expected newer version, got %d %v", actual, ok)
	}
	if actual, ok := compareNumericVersions("2.3.9", "v2.4.6"); !ok || actual >= 0 {
		t.Fatalf("expected older version, got %d %v", actual, ok)
	}
}

func TestParseLauncherVersionOutput(t *testing.T) {
	for input, expected := range map[string]string{
		"绝区零 一条龙 启动器 v2.5.1\n":            "v2.5.1",
		"launcher 2.5.2-beta.1":           "2.5.2-beta.1",
		"\x1b[32mOneDragon v2.5.1\x1b[0m": "v2.5.1",
	} {
		if actual := parseLauncherVersionOutput(input); actual != expected {
			t.Fatalf("parse %q: expected %q, got %q", input, expected, actual)
		}
	}
}

func TestGetLauncherVersionRealExecutable(t *testing.T) {
	path := os.Getenv("ZZZ_TEST_LAUNCHER_EXE")
	if path == "" {
		t.Skip("ZZZ_TEST_LAUNCHER_EXE is not set")
	}
	if actual := getLauncherVersion(path); actual != "v2.5.1" {
		t.Fatalf("expected v2.5.1, got %q", actual)
	}
}

func TestCompareLauncherVersions(t *testing.T) {
	if actual, ok := compareLauncherVersions("v2.5.2-beta.1", "v2.5.1"); !ok || actual <= 0 {
		t.Fatalf("newer beta should not downgrade: %d %v", actual, ok)
	}
	if actual, ok := compareLauncherVersions("v2.5.1-beta.1", "v2.5.1"); !ok || actual >= 0 {
		t.Fatalf("stable should be newer than same-version beta: %d %v", actual, ok)
	}
}

func TestExtractLauncherArchive(t *testing.T) {
	root := t.TempDir()
	archivePath := filepath.Join(root, "runtime.zip")
	file, err := os.Create(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(file)
	for name, content := range map[string]string{
		"OneDragon-RuntimeLauncher.exe": "exe",
		".runtime/python.exe":           "runtime",
	} {
		entry, createErr := writer.Create(name)
		if createErr != nil {
			t.Fatal(createErr)
		}
		if _, writeErr := entry.Write([]byte(content)); writeErr != nil {
			t.Fatal(writeErr)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	staging := filepath.Join(root, "staging")
	install := launcherInstall{Kind: "runtime", ExeName: "OneDragon-RuntimeLauncher.exe", HasRuntime: true}
	if err := extractLauncherArchive(archivePath, staging, install); err != nil {
		t.Fatal(err)
	}
	if !fileExists(filepath.Join(staging, install.ExeName)) || !fileExists(filepath.Join(staging, ".runtime", "python.exe")) {
		t.Fatal("runtime launcher archive was not extracted")
	}
}
