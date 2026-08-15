# ZZZ CNB Launcher

由 [niechy](https://github.com/niechy) 维护的非官方 Windows 单文件兼容启动器，面向无法稳定访问 GitHub、Gitee 或部分代理站的 ZZZ 一条龙用户。

当前适配上游稳定版 `v2.5.1`，同时从官方 CNB `main` 动态读取未来模型声明。

将 `ZZZ-CNB-Launcher.exe` 放到 `OneDragon-RuntimeLauncher.exe` 所在目录，之后双击本启动器运行。

它会：

1. 新版 ZZZ 已原生支持 CNB 时保留其代码源和自动容灾；旧版安装才使用兼容转换。
2. 保持 `auto_update_code: true`，然后启动原版一条龙。
3. 启动 Git 更新前，先从官方 CNB 读取最新版模型声明；CNB 不可用时回退本地 ZZZ 源码。每类只准备并强制选用最新版，完整文件已存在时跳过。
4. 新模型和启动器都优先从 CNB 同步清单读取官方 SHA-256，随后按多个下载源依次尝试，校验一致时才安装。
5. 启动前检查当前正在使用的原始启动器或 RuntimeLauncher；稳定版有更新时自动备份、校验并替换，RuntimeLauncher 会连同 `.runtime` 一起更新，失败自动回滚。
6. 检查本启动器自身版本；有新版本时校验 EXE，由独立更新进程循环等待旧程序退出，替换成功后自动重启。
7. 识别原版启动器版本时优先执行官方支持的 `--version`，并隔离 PyInstaller 环境；PE 文件版本仅作为后备。

程序不会修改 Python 源码。新版 ZZZ 已原生支持 CNB 时，不修改 `project.yml` 的仓库架构；同时会清理 1.1 曾注入的废弃字段，并保留用户的代码源、分支和其他设置。模型准备完成后，仅更新 `config/model.yml` 中仍由上游使用的四个模型选择。

## 可选镜像

可在同目录创建 `ZZZ-CNB-Launcher.mirrors.txt`，每行一个模板，必须包含 `{url}`：

```text
https://your-proxy.example/{url}
{url}
```

存在该文件时，将使用其中的模板替换内置 GitHub 下载回退列表。默认顺序包含公开 CNB Release、Gitee、`ghfast.top`、`gh-proxy.com`、`ghproxy.net`、`gh.llkk.cc` 和 GitHub 直连，并尝试从 `ghproxy.link` 发现当前推荐节点。

第三方镜像只能传输文件，不能决定文件是否可信。没有官方 SHA-256 时，本启动器只接受项目官方 Gitee 账号直接返回的文件，不会静默信任公共代理。

## 命令行

```text
ZZZ-CNB-Launcher.exe --prepare-only
ZZZ-CNB-Launcher.exe --skip-models
ZZZ-CNB-Launcher.exe --skip-launcher-update
ZZZ-CNB-Launcher.exe --version
ZZZ-CNB-Launcher.exe --root D:\Path\To\ZZZ
```

## 构建

```powershell
go test ./...
go vet ./...
$env:CGO_ENABLED='0'
go build -trimpath -ldflags='-s -w' -o ZZZ-CNB-Launcher.exe .
```

项目采用 GPL-3.0 许可证。这不是 OneDragon-Anything 或 Mirror酱官方产品；上游程序、模型和发布资产的权利归各自贡献者所有。
