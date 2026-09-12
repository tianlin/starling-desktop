# 小宇宙 App 扫码登录

账号接入默认关闭。在账号弹窗阅读风险并启用后，点击“生成扫码登录二维码”，使用已经登录的小宇宙 App 扫描，在手机上确认。二维码使用小宇宙 App 扫描，不是微信扫一扫。

## 实现与验证边界

- 官方公开认证页面 `https://accounts.xiaoyuzhoufm.com/` 的前端资源确认了二维码创建、轮询状态和失效响应。使用 `https://web-api.xiaoyuzhoufm.com/v1/auth/qrcode/create` 和 `/v1/auth/qrcode/login`；当前 clientId 为 `podcaster-platform`，不宣称是独立获批的 Starling OAuth 客户端。
- 二维码在本地编码为像素矩阵，由 canvas 显示，不调用第三方二维码图片服务。
- 轮询间隔 1.5 秒；等待、已扫描、已确认分别处理；本地最多等待 3 分钟。CONFIRMED 和 USED 均按官方前端的完成状态处理，但必须取得完整凭据；HTTP 401/code 21 才按二维码失效处理。请求失败时停止，不无限重试。
- 确认后的访问与刷新令牌仅留在 Go 后端。先读取网页身份，再读取听众身份；UID 一致后才建立会话。可选在 Windows 使用 DPAPI、macOS 使用本机钥匙串保存；否则仅本次会话。保存失败会明确提示，并保留临时连接。
- 关闭窗口会取消该二维码；旧二维码不能覆盖新会话。取消和失败后刷新前端会话版本。
- 原有短信路径保留，但未接入官方网页的人机验证流程。

## 验证

已验证真实接口能创建二维码并返回 WAITTING。单元测试覆盖扫描状态、凭据 Cookie 解析、二维码失效、取消后拒绝旧响应、空 ID 不取消其他登录、身份不一致拒绝连接。

已保存会话的列表读取不能证明一次新的扫码登录成功。手机确认、身份核对、续期和实际播放完整链路仍需验收；已验证结果见 [测试报告](TEST_REPORT.md)。

显式联网探测（不发送短信、不登录账号）：PowerShell 设置 `$env:STARLING_QR_LIVE='1'`，运行 `go test ./internal/provider -run TestQRLiveWaiting -v -count=1`。

本地二维码编码依赖：`github.com/skip2/go-qrcode`，固定版本 `v0.0.0-20200617195104-da1b6568686e`，MIT 许可。
