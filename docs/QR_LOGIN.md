# 小宇宙 App 扫码登录

2026-09-12 增加。账号弹窗中启用实验性账号接入后，点击“生成扫码登录二维码”，使用已经登录的小宇宙 App 扫描，并在手机上确认。不是微信扫一扫。

## 实现与验证边界

- 官方公开认证页面 `https://accounts.xiaoyuzhoufm.com/` 的前端资源确认了二维码创建、轮询状态和失效响应。使用 `https://web-api.xiaoyuzhoufm.com/v1/auth/qrcode/create` 和 `/v1/auth/qrcode/login`；当前 clientId 为 `podcaster-platform`，不宣称是独立获批的 Starling OAuth 客户端。
- 二维码在本地编码为像素矩阵，由 canvas 显示，不调用第三方二维码图片服务。
- 轮询间隔 1.5 秒；等待、已扫描、已确认分别处理；本地最多等待 3 分钟。CONFIRMED 和 USED 均按官方前端的完成状态处理，但必须取得完整凭据；HTTP 401/code 21 才按二维码失效处理。请求失败时停止，不无限重试。
- 确认后的访问与刷新令牌仅留在 Go 后端。先读取网页身份，再读取听众身份；UID 一致后才建立会话。可选使用原有 Windows DPAPI 保存；否则仅本次会话。
- 关闭窗口会取消该二维码；旧二维码不能覆盖新会话。取消和失败后刷新前端会话版本。
- 原有短信路径保留，但未接入官方网页的人机验证流程。

## 验证

已验证真实接口能创建二维码并返回 WAITTING。单元测试覆盖扫描状态、凭据 Cookie 解析、二维码失效、取消后拒绝旧响应、空 ID 不取消其他登录、身份不一致拒绝连接。

真实手机确认后的令牌返回形式、听众资料/订阅/收藏兼容性仍需用户扫码验收。测试通过不代表此链路已经通过真实账号测试。

显式联网探测（不发送短信、不登录账号）：PowerShell 设置 `$env:STARLING_QR_LIVE='1'`，运行 `go test ./internal/provider -run TestQRLiveWaiting -v -count=1`。

新增本地二维码编码依赖：`github.com/skip2/go-qrcode`，固定版本 `v0.0.0-20200617195104-da1b6568686e`，MIT 许可。
