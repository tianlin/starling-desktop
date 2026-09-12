# Starling 0.1.0-alpha.1 — 开发验证报告

验证记录更新：2026-09-12。源码开发版，不是签名安装包或完成真实账号验收的正式版本。

## 当前 Windows 验证

环境：Windows x64、Go 1.26.8、Node.js 24.5.0、TypeScript 5.8.3、Wails 2.11.0。根目录测试不会自动覆盖 desktop 嵌套模块。以下新结果针对工作区候选版本；没有操作桌面窗口或启动候选程序。

| 检查 | 结果与范围 |
|---|---|
| 根模块 go test -json -count=1 ./... | 69 个测试/子测试通过，2 个显式联网测试默认跳过；包含当前用户 DPAPI 和 Win32 ABI |
| 根模块 go vet ./... | 通过 |
| frontend 中 npm test | TypeScript 编译和 13 项 Node 测试通过 |
| desktop 模块 go test ./... / go vet ./... | 通过编译与静态检查；宿主尚无专用测试文件 |
| scripts/build-windows.ps1 -Candidate -CandidateName Starling-candidate-r3 | 全流程通过，输出独立 Starling-candidate-r3.exe；跳过绑定生成，未启动应用 |
| 依赖校验 | 临时 Go workspace 将两个本地模块都作为 main，go mod download / verify 通过；不忽略校验错误 |
| 浏览器回归 | Windows Headless Chrome，静音、独立临时配置，14 项真实 DOM/合成音频检查通过；保留 CSP，直接连接本机合成 Go 后端 |
| 实际二进制漏洞扫描 | govulncheck v1.8.0 -mode=binary：No vulnerabilities found；只代表本次漏洞库和候选二进制 |
| 依赖材料 | 实际二进制 18 个外部 Go 模块＋Go 运行时/工具链＋TypeScript 构建工具，共 20 项；递归收集 63 份许可证/声明及文件哈希，没有组件缺少声明文本，适用性仍待专业审查 |
| 独立代码检查 | 只读审查和离线 provider/app 测试通过；非 CodeRabbit 报告，未替代实机验收 |
| 应用启动 | 历史版本曾创建主窗口；本次候选程序未启动，以免干扰用户桌面 |
| 官方二维码接口 | 实际客户端成功创建二维码并取得 WAITTING 状态 |
| 扫码状态回归 | CONFIRMED/USED 均进入凭据解析；无完整凭据拒绝连接；401/code 21 按过期处理 |
| 会话回归 | 未确认不连接、身份不一致拒绝连接、取消和旧二维码不能干扰后续会话 |

真实手机扫码曾暴露 USED 状态误判，已修复并通过回归测试。本次读取 Starling 自身保存的会话完成订阅和收藏接口检查；没有重新扫码，也没有进行凭据续期、轮换或写回。完整登录到实机播放链路仍待验收。

## 收藏故障与真实分页证据

适配器版本：`xyz-readonly-2026-09-12.1`。2026-09-12 对同一已保存会话进行最小对照：

- 原收藏请求：HTTP 400、业务码 1、`rpc_error`；去掉 limit 仍失败，同会话订阅可用。
- 保持原请求体、令牌和 Starling User-Agent，仅加入客户端生成的 UUID `x-jike-device-id`：HTTP 200，首屏 10 条收藏，随后空末页。
- 订阅：首屏 30 条、末页 10 条。两类首屏均含 loadMoreKey，末页均只有 data；没有 hasMore。按实测的私有库适配规则识别省略游标的末页，通用解析器和节目单集列表仍保留未知结束状态。
- 显式手动测试遍历两类列表通过，未发现重复 ID。只输出页数、条目数和结构字段，不输出标题、账号、令牌、原始响应或签名音频地址。
- 手机端同期数量核对、其他账号与更大收藏库未验收，不能把这次 10 / 40 的计数当作所有账号的完整性证明。

新增回归覆盖设备 UUID 每客户端稳定且相互独立、不伪装手机、私有库末页规则、异常分页不被覆盖、嵌套错误进入诊断、错误时保留缓存、内存缓存过期和缓存文案。

候选文件 `build/Starling-candidate-r3.exe` SHA-256：`34b08d2a71466796d593225a43e488207529a65cef01b9b4f2c64768658b1b93`。

本机证据位于 `build/core-tests.jsonl`、`build/vulnerabilities-after.txt`、`build/compliance/`、`docs/test-results/`（均为不提交的构建/测试产物）。不提交真实账号响应。

## 短信认证取消回归

认证提交后可点击“取消连接”、关闭按钮或按 Escape。取消按发起前的会话代次绑定，仅影响该短信尝试；排队请求和迟到凭据均不能建立会话，不清除已保存凭据，也不影响二维码、恢复或后续登录。若后端已完成认证，界面明确提示账号已连接，不隐式退出。

新增 4 项 Go 测试覆盖取消排队请求、取消在途请求后迟到凭据、其他认证类型隔离、已完成/后续登录保护；2 项前端测试覆盖普通刷新和启动恢复的旧 bootstrap 响应不能覆盖新账号。浏览器新增取消后重开并登录、认证成功响应迟到时 Escape 关闭和重开弹窗两项场景。独立审查发现的两处旧 bootstrap 覆盖问题已复现、修复并复核通过。所有登录均使用合成账号，没有发送真实短信；官方人机验证流程和原生 WebView2 交互仍待验证。

## 存储故障与远端验证

新增 4 项 Windows 系统 SQLite 测试，全部使用临时目录：中文与空格路径重开；通过 max_page_count 注入 SQLITE_FULL 后旧值保留且解除限制可继续写；双连接写锁冲突后旧值保留且解除锁可继续写；隔离子进程不执行 Close/rollback 突然退出后只恢复已提交值，随后可继续写。原损坏数据库测试增加失败打开前后逐字节对比。没有填满真实磁盘、修改当前应用数据库或终止用户进程；这些检查不等于完整桌面强退/磁盘故障验收。生产源码及 r2 二进制未因这批测试变化。

提交 `66bf853adba343175e0d818a13722f00a3274ee9` 已推送远端 main；[GitHub Actions 34677155739](https://github.com/tianlin/starling-desktop/actions/runs/34677155739) 全部成功，覆盖 Linux go test -race / vet / frontend、Windows 完整绑定构建、二进制扫描和依赖材料归档。后续增量的远端状态以各自提交的运行结果为准。

## 安装/卸载脚本隔离回归

`test-installation.ps1` 在 Windows PowerShell 5.1 和 PowerShell 7 下均有 7 项检查通过：相对目录拒绝、中文路径安装及快捷方式目标、模拟正式/候选进程保护、真实文件锁导致升级失败时旧文件保留且暂存清理、正常升级、目录 junction 拒绝前保留原文件、卸载保留数据与显式删除。数据及快捷方式全部位于唯一临时目录，没有执行真实程序、改动用户安装或开始菜单。安装/卸载之前检查完整路径与重解析点；升级采用暂存后 File.Replace。远端 Windows Server 对照暴露 WScript.Shell 对中文目标路径赋值失败（ASCII 测试文件和系统文件均接受）；快捷方式已改用原生 Unicode IShellLinkW / IPersistFile，测试从保存的 .lnk 读回完整目标，保留中文断言。对应接口依据 [Microsoft IShellLinkW 文档](https://learn.microsoft.com/en-us/windows/win32/api/shobjidl_core/nn-shobjidl_core-ishelllinkw)。

这不替代干净 VM、非管理员账号、真实运行中程序与已签名安装包验收。当前二进制内容不因脚本改动而变化。

提交 `e0ab02d52bac00959e6122e71f7be37e30e7dbbd` 的 [CI 34677317958](https://github.com/tianlin/starling-desktop/actions/runs/34677317958) 全部成功，已包含 69 项核心测试、13 项前端测试和 Linux 合成浏览器回归。

## 递归许可证材料与工具链一致性

新增收集器测试覆盖嵌套相对路径、NOTICE/PATENTS、排除同名源码和 .git、拒绝目录链接。对实际 r2 候选生成的 63 份文件逐一核对 SHA-256，通过；重复生成通过。Go 运行时/工具链 component 使用与候选记录相同版本的 GOROOT，版本不一致会拒绝生成。Wails 与 WebView2 原先遗漏的 4 份子目录许可证均已纳入。

这些材料是源树范围的审查集合，不能据此声称全部许可通过。原生系统 DLL、WebView2 和编解码运行时条款仍需单独评估。

安装修复提交 `948dff17597e7ef3013c860c5ce665dc4e75e391` 的 [CI 34678084404](https://github.com/tianlin/starling-desktop/actions/runs/34678084404) 已全部成功，包括独立 Windows 安装测试、Linux 核心/浏览器测试与 Windows 构建/漏洞扫描。

## 弹窗可访问名称与键盘回归

所有共用弹窗通过 aria-labelledby 关联当前标题；关闭按钮名称为“关闭弹窗”。新增合成浏览器检查先复现缺少弹窗名称，再复现 Tab 离开末项落到 body；修复后验证 Enter 打开、双向 Tab 首尾循环、Escape 关闭与焦点返回触发按钮。监听器随关闭移除，动态禁用/隐藏的控件不纳入焦点循环。14 项浏览器回归、13 项前端测试和 r3 完整候选构建通过；r3 漏洞扫描无命中，20 项组件 / 63 份材料已重新生成。

此项没有运行真实屏幕阅读器，也不替代原生 WebView2 或 Windows DPI 验收。

依赖材料提交 `65374385e4d56228a3734e6a42a2204349e16250` 的 [CI 34678340168](https://github.com/tianlin/starling-desktop/actions/runs/34678340168) 已全部成功。

## 历史合成测试

较早的 Linux 环境记录：Go 1.23.2、Node.js 22.16.0、系统 SQLite；45 项核心测试及竞态检查、10 项前端测试、11 项 Chromium 合成浏览器检查通过。这些是历史结果，不代表后续新增代码全部经过相同浏览器或竞态检查。

浏览器检查使用合成账号、合成 WAV 与内存页面测试桥，覆盖播放、页面切换、队列、书签、退出和紧凑布局；不等于原生 WebView2、真实 CDN 或真实账号验收。原始运行输出不纳入源码仓库。

## 已知限制

- 原 Go 1.23.12 候选二进制扫描命中 56 个漏洞记录；更新到 Go 1.26.8 和修复后的 x/net、x/sys、x/text 依赖后，新候选扫描通过。旧程序不会自动替换，不能继续分发旧二进制。
- 常规构建须先退出 Starling；后台使用 -Candidate 构建独立文件，跳过绑定生成。本次未执行常规绑定生成流程。检测到旧 Starling-candidate 正在运行后，改用 -CandidateName 输出 r2，保留运行中的程序。
- 没有独立 CodeRabbit 报告。历史环境中 CLI 缺失、安装源解析失败；不把本地人工检查称作 CodeRabbit 审查。
- GitHub Actions 的通过状态应以远端运行结果为准，配置文件存在不等于 CI 成功。
- 托盘、媒体键、睡眠、设备切换、长时播放、安装卸载及不同 DPI 尚未完成完整实机验收。
- 根目录及嵌套声明、Go 运行时许可证已收集并核对哈希；具体适用条款、系统/编解码运行时许可、专业审查、签名及平台接入边界仍需继续处理。

完整验收项见 [真实环境清单](REAL_ENVIRONMENT_CHECKLIST.md)，后续顺序见 [遗留工作](REMAINING_WORK.md)，扫码说明见 [QR_LOGIN.md](QR_LOGIN.md)。

## 演示截图

[screenshots/favorites.png](screenshots/favorites.png)、[screenshots/detail.png](screenshots/detail.png)、[screenshots/compact.png](screenshots/compact.png) 均来自合成演示环境，不含真实账号登录证据。
