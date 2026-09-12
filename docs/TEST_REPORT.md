# Starling 测试与证据

更新：2026-09-12。本文件集中维护核验入口与最新结果，原生长时播放、干净机及多账号等后续事项见 [REMAINING_WORK.md](REMAINING_WORK.md)。自动化通过、真实接口可读、原生交互可用和签名发布是不同证据，不相互替代。

## 本轮 0.2.0 核验

在 `codex/release-0.2.0` 工作区对本轮修改执行。Windows 11 build 26200、PowerShell 7.6.5 / 5.1、WSL Ubuntu 24.04、Go 1.26.8、Node 24.5.0、Chrome 152.0.7977.84。没有启动或替换用户正在使用的 Starling，也没有调用真实账号接口。

| 项目 | 本轮结果 |
| --- | --- |
| 根 Go 模块测试 / race / vet | Windows 与 Linux 均通过 `go test -race ./... -count=1` 和 `go vet ./...`；联网测试默认跳过 |
| desktop Go 模块测试 / vet | Windows 独立执行通过；宿主模块暂无专用测试文件 |
| TypeScript 编译与前端测试 | 104 项全部通过，包含陈旧产物清理、缺少静态文件和编译失败保护；启用未使用代码检查 |
| 浏览器 run / comments / updates / discovery / media / media_failures | 分别 16 / 16 / 7 / 8 / 6 / 4 组通过，共 57 组 |
| Windows Wails 构建与版本资源 | `Starling-candidate-release020.exe` 生产构建通过；实际文件/产品字符串版本为 0.2.0，数字版本均为 0.2.0.0 |
| 安装 / 卸载隔离回归 | PowerShell 7.6.5 与 Windows PowerShell 5.1 各 7 项通过 |
| 实际 EXE 漏洞扫描、依赖材料 | govulncheck 1.8.0 无命中；20 个组件、63 份许可/声明，0 个组件缺少许可文本；许可收集器 2 项测试通过 |
| 工作流语法 | actionlint 1.7.12 通过 |
| 远端 Actions | 以对应提交的 GitHub 检查为准，本地结果不替代远端结果 |

本轮候选大小 11,583,488 字节，SHA-256：`c4ae7b98e16b861ec85c6e7a4c78bd4cf81753e5deefa11aa50e5ca0ed6211b3`。候选尚未安装或执行原生交互验收。构建现在直接校验 EXE 版本资源；Windows 清单位于 Wails 实际读取的 `desktop/build/windows/wails.exe.manifest`，不再保留未被构建使用的 `app.manifest`。

## 已有基线证据及已知失败

- `main` 提交 `7789fb1` 的 Actions `34694868741` 三个 job 成功，0 warning / 0 error；该记录属于对应提交，不代表本轮 0.2.0 最终构建。
- 已有前端 101 项，浏览器主界面 16、评论 16、更新 7、编码媒体 6、媒体错误 4 组通过记录；探索数量以本轮脚本输出为准。已有 Go 双平台测试/静态检查、Wails 构建、安装隔离及候选扫描通过记录。本轮须对最终文件重新核验。
- 真实只读曾验证订阅/收藏分页、更新两页、三类搜索各两页、推荐词、创作者作品及订阅状态。这些样本不证明所有账号、完整历史或手机端逐项一致。
- `17058e6` 修复 REMOVED 引用缺少 text 时整页解析失败。某根评论 22 条回复的首批 15 条含 2 个删除引用，读取正常；不表示所有复杂线程均已验收。
- **另一个真实样本的 TIME 严格降序探针失败。** “最新”使用服务器 TIME 顺序；不能声称跨样本严格按时间降序。该失败不因删除旧清单而消失，应继续核对排序契约与探针预期。
- 真实评论发表、回复及订阅写入没有由自动测试执行。只读验证不代表这些写操作已经实机验收。

## 本地与 CI 命令

工具链基线：Go 1.26.8、Wails 2.11.0、TypeScript 5.8.3。记录执行时 Node、Windows/Linux、浏览器及 WebView2 版本；CI 环境以 `.github/workflows/ci.yml` 为准。

```powershell
# 根模块
go test -count=1 ./...
go vet ./...
# race 需要支持 CGo 的工具链；记录实际运行平台
go test -race ./...
# 宿主模块
Set-Location desktop
go test -count=1 ./...
go vet ./...
Set-Location ../frontend
npm.cmd ci --ignore-scripts
npm.cmd test
Set-Location ..
node --test scripts/license-files.test.mjs
```

浏览器测试先安装 `tests/e2e/requirements.txt` 和 Playwright Chromium，然后在根目录分别执行：

```powershell
python -m pip install -r tests/e2e/requirements.txt
python -m playwright install chromium
python tests/e2e/run.py
python tests/e2e/comments.py
python tests/e2e/updates.py
python tests/e2e/discovery.py
python tests/e2e/media.py
python tests/e2e/media_failures.py
```

可用 `CHROMIUM_PATH` 指定浏览器。测试采用 headless、静音、独立临时配置和合成后端；结果及截图在忽略的 `docs/test-results/`。主界面通过正常 HTTP 连接合成后端，覆盖响应取消竞态和重载；旧内存注入路径已移除。MP3/M4A/AAC 样本与媒体故障为本地受控短音频，不替代原生 WebView2、真实 CDN、实际出声或长时播放。

```powershell
powershell -NoProfile -File .\scripts\build-windows.ps1 -Candidate -CandidateName Starling-candidate-020
powershell -NoProfile -File .\scripts\test-installation.ps1
go run golang.org/x/vuln/cmd/govulncheck@v1.8.0 -mode=binary build/Starling-candidate-020.exe
node scripts/dependency-report.mjs build/Starling-candidate-020.exe
```

候选构建不启动程序，跳过绑定生成；宿主绑定改变时还需常规构建。安装回归仅使用隔离目录和合成文件，覆盖升级、占用、进程保护、链接拒绝及保留/删除数据，不等同于干净机安装。依赖报告在 `build/compliance/`，必须对应扫描的实际 EXE；记录扫描日期、工具版本和 SHA-256。SBOM、许可证文本及无漏洞命中不等于外部安全或许可审查。

## 授权后的真实只读核验

只读测试默认跳过，不纳入常规 CI。使用本应用受保护会话，不续期、不写回凭据、不发表或订阅；不要把真实令牌、账号标识、正文或原始响应写入报告。

| 环境变量 | 测试名 |
| --- | --- |
| `STARLING_LIVE_LIBRARY=1` | `TestLiveLibraryReadOnly` |
| `STARLING_LIVE_UPDATES=1` | `TestLiveUpdatesReadOnly` |
| `STARLING_LIVE_DISCOVERY=1` | `TestLiveDiscoveryReadOnly` |
| `STARLING_LIVE_COMMENTS=1` | `TestLiveCommentsReadOnly` |
| 上项及 `STARLING_LIVE_COMMENT_EPISODE` | `TestLiveCommentThreadsReadOnly` |
| `STARLING_LIVE_PUBLIC_URL` 为公开分享 URL | `TestLivePublicShare` |
| `STARLING_LIVE_MEDIA_URL` 为公开单集 URL | `TestLivePublicMediaRange` |

例如明确启用后运行 `go test ./internal/provider -run '^TestLiveLibraryReadOnly$' -v -count=1`，结束后删除本次设置的变量。评论样本可通过 `STARLING_LIVE_COMMENT_EPISODE` 指定；通用评论探针还检查 TIME 时间顺序，已知服务器样本可能使该断言失败，需如实记录而非宣称全通过。

每条新增证据记录：场景、环境版本、命令或操作、预期、实际、脱敏输出位置、执行日期、提交及候选哈希。失败保留原因与复现入口，缺少条件的项目保留待验证。

## 0.3.0 macOS 候选实现

本次新增 macOS 平台层、隔离钥匙串原生测试、双架构 CI、DMG 检查和访客启动/退出冒烟。执行环境与最终结果在本节更新；已有 0.2.0 结果仅属于历史构建，不能作为 0.3.0 验收。Mac 实机逐项表见 [MACOS.md](MACOS.md)。

### 0.3.0 本地结果（2026-09-13）

- Windows 根模块 test/vet、桌面模块 test/vet 通过；前端 107 项通过。
- 六组 Chromium 合成浏览器回归共 57 项通过（run 16、comments 16、updates 7、discovery 8、media 6、media_failures 4）。更新流用例中一条已被 0.2.0 后续提交移除的技术提示断言已同步；未知结束和确认空列表仍分别验证。
- Windows 候选 Starling-candidate-030.exe 构建及 0.3.0 版本资源检查通过，未替换或启动用户运行中的应用；SHA-256 位于 build/SHA256SUMS-candidate-030.txt。实际 EXE 扫描无漏洞命中，依赖材料 20 个组件、63 份许可文本。
- Windows 安装/卸载隔离回归 7 项、许可收集器 2 项、actionlint 1.7.12 均通过。
- WSL Ubuntu 24.04 根模块 `go test -race ./...` / `go vet ./...` 通过；macOS shell 脚本语法和产物校验 Python 脚本编译检查通过。
- macOS 原生验证通过下述 CI 完成；最低系统版本、实际出声与用户账号流程待实机验收。

### 0.3.0 双架构 CI 结果

实现提交：`356b5a601af1f0b95426871c85fbde093b24a175`。2026-09-13 核对 [CI run 34717600249](https://github.com/tianlin/starling-desktop/actions/runs/34717600249) 已完成且结论为 success，五个作业全部通过。后续文档补充不改变此实现提交的源码。

| 作业 | 已通过范围 |
| --- | --- |
| macOS 15 arm64 | 根模块 race/vet、宿主 test/vet、隔离钥匙串测试、前端测试、Wails 应用构建、ad-hoc 签名完整性、架构/最低目标/动态库检查、漏洞扫描、依赖材料、DMG 打包与挂载、隔离访客前端初始化和实际退出保存握手 |
| macOS 15 Intel amd64 | 与 arm64 相同范围，在 Intel runner 原生执行 |
| Windows candidate | 测试、构建、版本资源、实际 EXE 漏洞扫描和依赖材料 |
| Windows installation | 隔离安装/卸载检查 |
| Linux core | 核心 race/vet、前端与合成浏览器回归 |

候选下载（GitHub Actions artifact，可能需要登录 GitHub）：[Apple Silicon](https://github.com/tianlin/starling-desktop/actions/runs/34717600249/artifacts/10305171941)、[Intel](https://github.com/tianlin/starling-desktop/actions/runs/34717600249/artifacts/10304933187)。每个压缩包内包含对应 DMG、SHA256SUMS.txt、构建环境、校验/启动日志和依赖材料。制品有保留期限，并非永久 Release 附件。

上述启动冒烟不播放真实节目，不登录真实账号；拒绝把六秒超时退出视为正常保存握手。macOS 14 实机、Gatekeeper 首次下载提示、实际音频、Dock/菜单栏交互、系统睡眠和长时播放仍需用户按 MACOS.md 验收。
