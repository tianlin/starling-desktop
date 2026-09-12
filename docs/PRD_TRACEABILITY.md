# PRD 需求映射

判定词：**实现**不等于**真实环境验收通过**。本次没有替原 PRD 修改 G1～G3 的退出条件。

| 需求 | 对应实现 | 本次证据 / 剩余工作 |
|---|---|---|
| FR-01 登录接入与状态 | provider/client.go、session/manager.go、settings.ts | 合成认证 / 身份核对；短信提交支持独立取消、关闭/Escape、重开和迟到响应隔离；已完成认证明确提示。真实短信/原生交互仍待验证 |
| FR-02 续期与错误隔离 | session/manager.go、provider/client.go | 并发刷新、轮换、单次重放、429 与退出竞态测试；真实刷新待验证 |
| FR-03 安全存储与退出 | security/vault*.go、app/service.go | 当前 Windows 用户 DPAPI 加解密及篡改拒绝通过；跨 Windows 用户隔离仍待验收 |
| FR-04 我的订阅 | app/library.go、shell.ts | 本次真实接口两页 30＋10，识别省略游标末页；手机同期核对待执行 |
| FR-05 收藏单集 | 同上 | 修复缺少设备会话 UUID 导致的 400；真实读取 10 条及空末页；候选 UI/手机数量核对待执行 |
| FR-06 分页与同步完整性 | provider/page.go、app/library.go | 私有库特定末页规则、去重、异常分页、错误保留完整缓存、缓存过期回归通过；其他端点仍严格处理未知结束 |
| FR-07 公开链接与详情 | security/urls.go、provider/public.go、notes.ts | 域名与 ID、公开 JSON 解析、HTML 清洗、时间点测试；已修复公开节目嵌套 episodes 结构；两个真实匿名样本通过单集/节目详情与 1/15 条预览检查，媒体未播放 |
| FR-08 基础播放 | player.ts、shell.ts | 合成 WAV 与 MP3/M4A/AAC 的 Chrome 实际播放、Range 续播/定位及非 Range 降级通过；不可定位时禁用操作并说明；WebView2 常见媒体格式仍待测 |
| FR-09 链接、鉴权与重试 | provider/client.go、app/playback.go、player.ts | 两个匿名真实源的四次 206 匹配；四组受控 HTTP 403 验证一次重解析、失败停止和暂停/切歌；前后端代次水位保护乱序及排队取消，失败后暂停仍可手动重试；真实账号续期/WebView2 待测 |
| FR-10 稍后听与恢复 | app/playback.go、player.ts、shell.ts | 队列去重 / 排序、成功播放后出队、启动暂停；进程强退与真实错误场景待测 |
| FR-11 本机续播 | store、app/playback.go、player.ts | 保存、读取、epoch 拒绝；部分缓冲和赋值失败保留续播目标、禁止早期进度覆盖、手动定位取代目标、写盘恢复与媒体错误隔离均通过回归；桌面断电场景待测 |
| FR-12 后台与系统控制 | internal/desktop、desktop/main.go | Windows 原生 ABI 测试通过；托盘、媒体键、睡眠/设备切换仍待完整实机验收 |
| FR-13 设置与数据控制 | app/service.go、settings.ts | 设置/清理测试通过；列表嵌套错误纳入脱敏诊断；WebView 缓存是退出后尽力清理 |
| FR-14 安装与更新 | scripts/*.ps1、CI | 候选构建脚本全流程通过，含双模块检查与依赖验证；隔离安装/升级/卸载 7 项与远端 Windows 测试通过；中文快捷方式修复；无签名、干净 VM 实测或自动更新 |
| FR-15 可用性 | DOM UI、键盘、错误状态、布局 | 1280×900 / 900×700 浏览器检查与输入保护；弹窗命名、Tab循环、Escape和焦点返回通过；Windows DPI / 屏幕阅读器待测 |

## 总体验收

| 门槛 | 状态 |
|---|---|
| G1 接入与发布边界 | **未通过**：未取得本项目适用的许可或专业法律结论 |
| G2 账号完整闭环 | **未通过**：已有真实订阅/收藏读取与分页证据，扫码/续期/手机对照/完整实机闭环尚未全部通过 |
| G3 Windows 播放闭环 | **未通过**：Wails 候选构建和 Windows 核心测试已通过，完整 WebView2/硬件/安装验收仍缺少证据 |

本交付可作为后续开发与 M0 验证的 Alpha 工程基础，不能通过隐藏缺失功能、把演示数据改名或移除实验提示来满足账号版 MVP 的验收。

最新具体记录见 [TEST_REPORT](TEST_REPORT.md)，全部遗留项及推进顺序见 [REMAINING_WORK](REMAINING_WORK.md)。
