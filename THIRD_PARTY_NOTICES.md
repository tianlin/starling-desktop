# 第三方组件与研究参考

## 代码与组件

本项目原创代码采用根目录 MIT 许可证。不复制 Horizon 的 GPL 实现；MIT 不覆盖小宇宙的音频、封面、文案、商标或对平台接口的授权。

| 项目 | 使用方式 | 状态 |
|---|---|---|
| Wails 2.11.0 | Windows 宿主依赖 | 已完成依赖解析、go.sum 和 Windows 构建；完整传递依赖许可清单仍需整理 |
| TypeScript 5.8.3 | 构建时编译器 | 实际离线安装并生成 package-lock；node_modules 不随源码包分发 |
| SQLite | Linux 测试链接系统 libsqlite3；Windows 调用系统 winsqlite3.dll | 不分发 SQLite amalgamation 或 Windows DLL；薄绑定为本项目编写 |
| Go / Node / Windows WebView2 | 工具链或宿主运行时 | 不随源码包分发 |
| Chromium / Python Playwright | 本次测试环境 | 不作为客户端运行时分发 |

未因为“依赖看起来商业友好”而宣称完成了全部传递依赖许可审核。公开二进制分发前，应从实际锁定后的完整依赖图生成 SBOM、许可证文本集合，并检查媒体编解码相关运行时条款。

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

## 官方技术参考

- https://github.com/wailsapp/wails/tree/v2.11.0/v2/pkg/options — Wails 配置、资源服务、Windows 选项。
- https://github.com/wailsapp/wails/blob/v2.11.0/v2/pkg/runtime/dialog.go — Wails 对话框 API。
- https://learn.microsoft.com/en-us/windows/win32/api/shellapi/ns-shellapi-notifyicondataw — 托盘结构体。
- https://learn.microsoft.com/en-us/windows/win32/api/winuser/nf-winuser-registerhotkey — 原生媒体热键注册。
- https://go.dev/dl/?mode=json — 核对 CI 候选 Go 1.26.5 存在；不宣称抓取结果永远代表最新版本。
