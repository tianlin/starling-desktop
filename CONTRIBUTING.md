# 贡献指南

先阅读 [README](README.md)、[产品范围](docs/PRD.md) 和 [架构](docs/ARCHITECTURE.md)。说明修改解决的问题、实际验证范围和剩余限制；构建、合成浏览器回归、真实接口与原生实机结果应分别记录。修改会话、分页、播放器或权限边界时，添加能够复现问题的测试。核心测试不使用真实账号。

根目录与 `desktop/` 是两个 Go 模块，必须分别检查。前端采用严格 TypeScript，`npm.cmd test` 先编译再运行 Node 内置测试；浏览器回归位于 `tests/e2e/`。命令和结果入口见 [测试报告](docs/TEST_REPORT.md)。

工具链固定 Go 1.26.8（语言下限 1.26.0）。`scripts/verify-dependencies.ps1` 通过临时双模块 workspace 校验依赖，不忽略校验错误或关闭 TLS。用户正在使用程序时采用 `build-windows.ps1 -Candidate`，避免绑定生成或重启用户应用。

账号接入的界面名称不改变持久化键 `experimentalAccount`，默认仍为关闭。改变设置结构时保留已有数据兼容性。修改平台适配器应说明核对日期、契约差异、授权范围和故障退化；不得通过伪造设备、绕过付费、关闭 CSP 或公共账号代理处理兼容问题。

不要提交凭据、数据库、真实认证响应、手机号、未脱敏截图、依赖目录或构建产物。真实只读测试必须显式启用对应 `STARLING_LIVE_*` 开关，不能纳入默认 CI；评论、回复和订阅等真实写操作由用户主动完成，不加入自动测试。

发布时核对版本、当前提交的 CI、实际二进制哈希、漏洞扫描及依赖材料。SBOM 和许可证文本收集不代替适用性审查，未签名构建不得标为已签名。后续实机事项集中在 [REMAINING_WORK.md](docs/REMAINING_WORK.md)。

macOS 修改还需通过两个原生架构 CI：`bash scripts/build-macos.sh` 检查根模块、宿主、前端及实际应用包。钥匙串测试只能使用隔离临时钥匙串；冒烟使用临时 HOME 的访客数据。安装和验收说明见 [MACOS.md](docs/MACOS.md)。
