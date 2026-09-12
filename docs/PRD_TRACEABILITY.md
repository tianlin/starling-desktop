# PRD 需求映射

判定词：**实现**不等于**真实环境验收通过**。本次没有替原 PRD 修改 G1～G3 的退出条件。

| 需求 | 对应实现 | 本次证据 / 剩余工作 |
|---|---|---|
| FR-01 登录接入与状态 | provider/client.go、session/manager.go、settings.ts | 合成认证 / 身份核对；真实账号待验证。认证提交后模态窗口暂不提供单独取消，退出应用会取消请求 |
| FR-02 续期与错误隔离 | session/manager.go、provider/client.go | 并发刷新、轮换、单次重放、429 与退出竞态测试；真实刷新待验证 |
| FR-03 安全存储与退出 | security/vault*.go、app/service.go | 模拟保护器 / 文件存储、epoch 清理测试；Windows DPAPI 原生测试尚未执行 |
| FR-04 我的订阅 | app/library.go、shell.ts | 只读列表、缓存与分页；真实集合核对未执行 |
| FR-05 收藏单集 | 同上 | 与订阅 / 本地书签区分；真实收藏分页契约未确认 |
| FR-06 分页与同步完整性 | provider/page.go、app/library.go | 去重、循环、空页、缺失结束、保留完整缓存；手机侧同期增删待测 |
| FR-07 公开链接与详情 | security/urls.go、provider/public.go、notes.ts | 域名与 ID、公开 JSON 解析、HTML 清洗、时间点测试；当前真实公开页面兼容性待测 |
| FR-08 基础播放 | player.ts、shell.ts | 合成 WAV 的真实浏览器播放、拖动、暂停 / 切歌；Windows 常见媒体格式待测 |
| FR-09 链接、鉴权与重试 | provider/client.go、app/playback.go、player.ts | 白名单、不传令牌、受限内容拒绝、一次媒体重解析；真实 CDN / Range 待测 |
| FR-10 稍后听与恢复 | app/playback.go、player.ts、shell.ts | 队列去重 / 排序、成功播放后出队、启动暂停；进程强退与真实错误场景待测 |
| FR-11 本机续播 | store、app/playback.go、player.ts | 保存、读取、epoch 拒绝、seekable 就绪后续播；断电场景待测 |
| FR-12 后台与系统控制 | internal/desktop、desktop/main.go | 源码与独立原生模块 Windows 交叉编译；没有 Windows 实机通过记录 |
| FR-13 设置与数据控制 | app/service.go、settings.ts | 设置验证、清理边界、诊断；WebView 缓存是退出后尽力清理 |
| FR-14 安装与更新 | scripts/*.ps1、CI | 构建 / 用户级安装卸载脚本；尚未实际构建完整 exe，没有签名或自动更新 |
| FR-15 可用性 | DOM UI、键盘、错误状态、布局 | 1280×900 / 900×700 浏览器截图与输入保护；Windows DPI / 屏幕阅读器待测 |

## 总体验收

| 门槛 | 状态 |
|---|---|
| G1 接入与发布边界 | **未通过**：未取得本项目适用的许可或专业法律结论 |
| G2 账号完整闭环 | **未通过**：仅有合成 / 契约测试，没有真实账号闭环证据 |
| G3 Windows 播放闭环 | **未通过**：没有完整 Wails 构建与 Windows 实机证据 |

本交付可作为后续开发与 M0 验证的 Alpha 工程基础，不能通过隐藏缺失功能、把演示数据改名或移除实验提示来满足账号版 MVP 的验收。
