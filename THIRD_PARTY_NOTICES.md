# 第三方组件与研究参考

## 代码与组件

本项目原创代码采用根目录 MIT 许可证。不复制 Horizon 的 GPL 实现；MIT 不覆盖小宇宙的音频、封面、文案、商标或对平台接口的授权。

| 项目 | 使用方式 | 状态 |
|---|---|---|
| Wails 2.11.0 | Windows 宿主依赖 | 由 desktop/go.mod 与 go.sum 固定；依赖许可由构建报告收集 |
| TypeScript 5.8.3 | 构建时编译器 | 由 package-lock 固定；node_modules 不随源码包分发 |
| SQLite | Linux 测试链接系统 libsqlite3；Windows 调用系统 winsqlite3.dll | 不分发 SQLite amalgamation 或 Windows DLL；薄绑定为本项目编写 |
| Go / Node / Windows WebView2 | 工具链或宿主运行时 | 不随源码包分发 |
| Chromium / Python Playwright | 浏览器测试环境 | 不作为客户端运行时分发 |

构建后运行 `node scripts/dependency-report.mjs build/Starling.exe`，从实际二进制读取运行时 Go 模块，另列 Go 运行时/工具链与 TypeScript 构建工具。脚本会核对本地 Go 版本及模块图与二进制记录一致。组件数量以本次 `build/compliance/sbom.cdx.json` 的 `components.length` 为准；许可文件数量为 `license-inventory.json` 中各 `modules[].licenseFiles.length` 之和，命令也会输出这两个计数。

`build/compliance/` 包含 CycloneDX SBOM、递归收集的许可证/NOTICE/COPYRIGHT/PATENTS、来源相对路径、逐文件 SHA-256 与待审状态。源树材料是审查集合，不表示每份许可都适用于最终二进制，也不替代媒体编解码、系统运行时条款或专业许可审核。

Go 工具链锁定 1.26.8；x/net 0.56.0、x/text 0.39.0、x/sys 0.46.0、x/crypto 0.53.0。二进制漏洞扫描结果见 [测试报告](docs/TEST_REPORT.md)，只适用于报告中记录的文件与当次漏洞库。

## 二维码编码依赖

`github.com/skip2/go-qrcode v0.0.0-20200617195104-da1b6568686e`，MIT 许可；仅用于本地生成二维码像素，不向图片服务发送登录链接。依赖源码及许可证由 Go 模块管理，未复制进仓库。

## 协议研究参考（非代码复制）

核对日期：2026-09-12。社区接口参考快照：`ultrazg/xyz` 的 `22cfe7848a98b393a5f9f62c5e9a4eea2592eb1b`。

- https://github.com/ultrazg/xyz — 社区协议说明及接口名称；其成功记录不构成本项目当前账号的实测证据。
- https://github.com/ultrazg/xyz/blob/22cfe7848a98b393a5f9f62c5e9a4eea2592eb1b/handlers/login.go — 主播后台短信登录。
- https://github.com/ultrazg/xyz/blob/22cfe7848a98b393a5f9f62c5e9a4eea2592eb1b/handlers/sendcode.go — 发送验证码。
- https://github.com/ultrazg/xyz/blob/22cfe7848a98b393a5f9f62c5e9a4eea2592eb1b/handlers/profile.go — 登录身份核对。
- https://github.com/ultrazg/xyz/blob/22cfe7848a98b393a5f9f62c5e9a4eea2592eb1b/handlers/token.go — 令牌刷新。
- https://github.com/ultrazg/xyz/blob/22cfe7848a98b393a5f9f62c5e9a4eea2592eb1b/handlers/subscription.go — 订阅列表。
- https://github.com/ultrazg/xyz/blob/22cfe7848a98b393a5f9f62c5e9a4eea2592eb1b/handlers/favorite.go — 收藏接口；该参考不证明本客户端传入分页参数后能够完整读取。
- https://github.com/ultrazg/xyz/blob/22cfe7848a98b393a5f9f62c5e9a4eea2592eb1b/handlers/episode.go — 单集详情与节目单集列表。
- https://github.com/ultrazg/horizon — 已有第三方客户端；作为产品与风险参考，没有复制其实现。

本适配器使用自己的 User-Agent，不复制参考实现中的移动设备、系统版本或设备指纹头。缺失适配、许可争议或平台风控不能通过秘密加入这些伪装字段解决。

收藏请求所需 `x-jike-device-id` 使用本次客户端运行随机生成的 UUID，不源自硬件、不跨进程持久化。该头经过同账号只读对照验证；具体证据与末页契约见 [测试报告](docs/TEST_REPORT.md)。没有复制参考中的手机型号或 OS 字段。

## 官方技术参考

- https://github.com/wailsapp/wails/tree/v2.11.0/v2/pkg/options — Wails 配置、资源服务、Windows 选项。
- https://github.com/wailsapp/wails/blob/v2.11.0/v2/pkg/runtime/dialog.go — Wails 对话框 API。
- https://learn.microsoft.com/en-us/windows/win32/api/shellapi/ns-shellapi-notifyicondataw — 托盘结构体。
- https://learn.microsoft.com/en-us/windows/win32/api/winuser/nf-winuser-registerhotkey — 原生媒体热键注册。
- https://go.dev/dl/?mode=json — 2026-09-12 核对 Go 1.26.8 为受维护分支补丁版本，项目与 CI 固定此版本。
