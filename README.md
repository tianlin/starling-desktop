# Starling · 星听

面向 **Windows 11 x64** 的非官方小宇宙桌面客户端，当前版本 **0.2.0**。支持个人库、订阅更新、探索、评论与本机连续收听，提供便携程序和当前用户安装脚本。

独立界面采用 Go + Wails 2 + TypeScript + SQLite，没有项目方账号服务器。原创代码采用 MIT；平台内容与商标不在该许可证授权范围内。本版本未签名，未经过外部安全审计；验证证据和实机边界分别见 [测试报告](docs/TEST_REPORT.md) 和 [后续验证](docs/REMAINING_WORK.md)。

![收藏列表：明确标记的合成测试环境](docs/screenshots/favorites.png)

## 功能

| 功能 | 使用范围 |
| --- | --- |
| 账号接入 | 小宇宙 App 扫码、身份核对、会话续期和可选本机保存；默认关闭，需用户启用。保留兼容性有限的短信路径 |
| 我的订阅 / 收藏单集 | 分页、去重、已加载内容筛选、可取消的加载全部；完整列表缓存与账号隔离 |
| 订阅更新 | 日期分组、播放、稍后听、逐页加载和近期缓存；见 [订阅更新说明](docs/SUBSCRIPTION_UPDATES.md) |
| 探索 | 搜索节目、单集和用户，推荐词、搜索历史、创作者作品、主动订阅及结果核对；见 [探索说明](docs/DISCOVERY.md) |
| 公开链接 / 详情 | 完整节目或单集链接、清洗后的说明和时间点；公开节目页可能只提供部分单集 |
| 评论 | 登录后按热门 / 最新阅读、展开回复、发表文字及回复；最新采用服务器 TIME 顺序，不保证跨样本严格时间降序。见 [评论契约](docs/COMMENTS_API.md) |
| 播放与本地状态 | 播放暂停、拖动、后退 15 秒 / 前进 30 秒、倍速、音量、书签、队列、断点续播；重启不自动播放 |
| Windows 集成 | 托盘、媒体键、睡眠暂停、单实例、用户级 DPAPI 和系统 SQLite；不同设备及 WebView2 的验证边界见测试报告 |

## 开始使用

运行 `build/Starling.exe`，或按下方步骤安装。公开链接和本地书签可在访客模式使用；个人库、探索和评论需要在设置中启用“账号接入”，再使用小宇宙 App 扫描二维码。凭据仅在本机处理，保存会话是可选项。

扫码采用官方网页现有流程，并重新核对听众身份，详见 [扫码说明](docs/QR_LOGIN.md)。这不是平台提供给本项目的 OAuth 授权；私有接口可能变化、拒绝访问或触发风控。短信路径缺少当前网页所需的人机验证参数，可能无法发送。客户端不伪造移动设备指纹，不绕过验证码、付费限制或访问控制，也不读取其他应用会话。

发表评论、回复和订阅须由用户主动操作。请求结果不确定时，先刷新或在官方客户端核对，再决定是否重试；自动测试没有执行真实评论或订阅写入。

## Windows 构建

需要 Windows 11 x64、Go **1.26.8**（语言下限 1.26.0）、Node.js/npm 和 WebView2。Wails 固定 **2.11.0**，TypeScript 固定 **5.8.3**。在根目录执行：

```powershell
powershell -NoProfile -File .\scripts\build-windows.ps1
```

脚本通过临时双模块 workspace 校验依赖，分别检查根模块和 `desktop/` 宿主、测试前端，再构建 `build/Starling.exe` 和 SHA-256 清单。常规构建前退出 Starling，避免绑定生成触发单实例检查。

需要保留当前程序运行时，使用独立候选构建：

```powershell
powershell -NoProfile -File .\scripts\build-windows.ps1 -Candidate -CandidateName Starling-candidate-020
```

候选构建跳过绑定生成，输出独立 EXE 和哈希，不启动或替换现有程序，并拒绝覆盖运行中的同名目标。修改宿主绑定签名后需补做常规构建。

脚本受组织策略限制时，按组织批准方式执行，或逐条运行以下命令，无需关闭执行策略：

```powershell
$env:CGO_ENABLED = "0"
go test ./...
go vet ./...
Set-Location frontend
npm.cmd ci --ignore-scripts
npm.cmd test
Set-Location ../desktop
go test ./...
go vet ./...
go run github.com/wailsapp/wails/v2/cmd/wails@v2.11.0 build -platform windows/amd64 -clean
```

手工构建输出位于 `desktop/build/bin/`。依赖校验入口为 `scripts/verify-dependencies.ps1`；测试及发布物检查见 [测试报告](docs/TEST_REPORT.md)。

## 安装与卸载

```powershell
powershell -NoProfile -File .\scripts\install.ps1
powershell -NoProfile -File .\scripts\uninstall.ps1
# 同时删除用户数据：
powershell -NoProfile -File .\scripts\uninstall.ps1 -RemoveData
```

安装到 `%LOCALAPPDATA%\Programs\Starling`，数据位于 `%APPDATA%\Starling`。安装和卸载前退出 Starling 及候选版；脚本不自动结束进程。升级采用暂存后原子替换，文件占用时保留旧版；目录必须为绝对路径，目标范围内有重解析点时停止。默认卸载保留数据。当前没有自动更新、代码签名或应用商店分发。

## 合成演示与开发

```powershell
Set-Location frontend
npm.cmd ci --ignore-scripts
npm.cmd run build
Set-Location ..
go run ./cmd/demo
```

在浏览器打开 `http://127.0.0.1:34115/`。演示明确标记合成环境，使用临时数据与短测试音频，表单可输入 `00000000000` / `0000`，不会发送真实短信。桌面生产入口不引用演示；直接打开前端 HTML 不会连接后端。Linux 演示及测试需要 GCC 与 `libsqlite3-dev`；Windows 使用系统 SQLite。

| 目录 | 职责 |
| --- | --- |
| `internal/app/` | 业务编排与受限 JSON 桥 |
| `internal/provider/`、`internal/session/` | 平台适配、认证与会话 |
| `internal/store/`、`internal/sqlite/`、`internal/security/` | 本地数据、系统数据库与安全边界 |
| `internal/desktop/`、`desktop/` | Windows 原生集成、独立 Wails Go 模块及内嵌前端 |
| `frontend/src/` | 原生 DOM TypeScript 界面和唯一播放器 |
| `cmd/demo/`、`tests/e2e/` | 合成演示与浏览器回归 |

继续阅读：[贡献指南](CONTRIBUTING.md)、[产品范围](docs/PRD.md)、[架构](docs/ARCHITECTURE.md)、[安全](SECURITY.md)、[隐私](PRIVACY.md)、[第三方许可](THIRD_PARTY_NOTICES.md)。
