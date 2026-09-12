# Starling 0.1.0-alpha.1 — 开发验证报告

验证记录更新：2026-09-12。源码开发版，不是签名安装包或完成真实账号验收的正式版本。

## 当前 Windows 验证

环境：Windows x64、Go 1.23.12、Node.js 24.5.0、TypeScript 5.8.3、Wails 2.11.0。根目录测试不会自动覆盖 desktop 嵌套模块。

| 检查 | 结果与范围 |
|---|---|
| 根模块 go test ./... | 通过；包含 Windows 下实际运行的核心与原生测试 |
| 根模块 go vet ./... | 通过 |
| frontend 中 npm test | TypeScript 编译和 10 项 Node 测试通过 |
| Wails windows/amd64 production build | 已成功构建完整 Starling.exe；实际依赖校验文件已生成 |
| 应用启动 | 已创建主窗口，进程响应正常；不等于完整 UI/播放验收 |
| 官方二维码接口 | 实际客户端成功创建二维码并取得 WAITTING 状态 |
| 扫码状态回归 | CONFIRMED/USED 均进入凭据解析；无完整凭据拒绝连接；401/code 21 按过期处理 |
| 会话回归 | 未确认不连接、身份不一致拒绝连接、取消和旧二维码不能干扰后续会话 |

真实手机扫码曾暴露 USED 状态误判，现已修复并通过回归测试。修正后的真实账号、订阅、收藏和播放完整链路仍待验收，不能据此宣称账号接入成功。

## 历史合成测试

较早的 Linux 环境记录：Go 1.23.2、Node.js 22.16.0、系统 SQLite；45 项核心测试及竞态检查、10 项前端测试、11 项 Chromium 合成浏览器检查通过。这些是历史结果，不代表后续新增代码全部经过相同浏览器或竞态检查。

浏览器检查使用合成账号、合成 WAV 与内存页面测试桥，覆盖播放、页面切换、队列、书签、退出和紧凑布局；不等于原生 WebView2、真实 CDN 或真实账号验收。原始运行输出不纳入源码仓库。

## 已知限制

- Go 1.23.12 下曾出现 go mod verify 对本地 replace 模块 starling v0.0.0 报 missing ziphash；随后独立 Wails 构建成功。仓库构建脚本保留该检查，因此此检查问题仍需定位，不能声称脚本全流程通过。
- 构建前应退出正在运行的 Starling，避免 Wails 绑定生成程序触发单实例检查。
- 没有独立 CodeRabbit 报告。历史环境中 CLI 缺失、安装源解析失败；不把本地人工检查称作 CodeRabbit 审查。
- GitHub Actions 的通过状态应以远端运行结果为准，配置文件存在不等于 CI 成功。
- 托盘、媒体键、睡眠、设备切换、长时播放、安装卸载及不同 DPI 尚未完成完整实机验收。
- 依赖许可汇总、签名及平台接入边界仍需继续审查。

完整验收项见 [真实环境清单](REAL_ENVIRONMENT_CHECKLIST.md)，扫码说明见 [QR_LOGIN.md](QR_LOGIN.md)。

## 演示截图

[screenshots/favorites.png](screenshots/favorites.png)、[screenshots/detail.png](screenshots/detail.png)、[screenshots/compact.png](screenshots/compact.png) 均来自合成演示环境，不含真实账号登录证据。
