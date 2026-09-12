# 订阅更新

在左侧“订阅更新”集中浏览已订阅播客的新单集。支持刷新、逐页加载、播放、加入稍后听、查看详情和返回原列表位置。已加载单集按发布时间排序、按本机日期分组；无效日期放在“日期未知”。账号接入默认关闭，需主动启用。当前没有星标、未听筛选、新集红点、通知或后台轮询。

## 接口与验证边界

适配器 `xyz-updates-2026-09-12.1`。沿用 `library` JSON 桥，新增 `kind: "updates"`，操作为 `cached / refresh / more / cancel`；返回现有 `LibraryView`，没有更改 Wails 公共桥签名。

平台请求为 `POST /v1/inbox/list`，首请求 `{"limit":"20"}`，后续带平台返回的 `loadMoreKey` 对象（含 `pubDate` 和 `id`）。游标校验后原样转发，不从显示顺序重建。继续使用 Starling 自身的客户端标识与网络/会话策略。

公开契约线索：[ultrazg/xyz 的 inbox 实现](https://github.com/ultrazg/xyz/blob/main/handlers/inbox.go)。本项目独立实现，不依赖该项目或复制其移动设备请求头。

**2026-09-12 真实只读验证：** 两次分页 HTTP 200，每页 15 条（请求 20 并不保证返回 20）；响应顶层含 `data / loadMoreKey / userStats`，单集及所属播客元数据可解析。两页样本存在一次发布时间逆序，所以应用对已加载内容排序。未验证全量历史、末页省略字段语义、手机端逐集比对或原生 WebView2 页面。

结束标志缺失时保持 `unknown_end`，不能视为同步完整；重复游标、空页仍带游标、矛盾结束标志和错误响应均明确报错。接口失败保留可用缓存，不自动切换成逐个订阅抓取。

真实检查默认跳过；仅在明确授权后执行：

```powershell
$env:STARLING_LIVE_UPDATES='1'
try {
    go test ./internal/provider -run '^TestLiveUpdatesReadOnly$' -v -count=1
} finally {
    Remove-Item Env:STARLING_LIVE_UPDATES
}
```

检查只读取 Starling 自己的受保护会话，不续期、不写回凭据、不发送内容，仅输出状态、字段名及计数。它限定最多两页，不能证明完整历史。

## 缓存与状态

- 使用现有按账号隔离的 SQLite `cache` 存储，新键 `updates:`；无需数据库迁移。
- 第一页成功后原子替换近期快照；后续成功页更新去重快照，最多保存最新 200 条节目元数据。截断时清除完整标记，签名音频地址和 Show Notes 不进入快照。
- 内存列表可继续翻页；持久化快照不保存可恢复的游标，应用重启后必须重新获取第一页。
- 首次进入先显示缓存再刷新；同一进程中成功刷新未满 10 分钟时，重新进入恢复列表。手动刷新始终重新取第一页。
- 刷新或翻页失败保留原内容及最后成功时间；取消、退出账号、清缓存或重置后的旧响应不能回写。新旧账号的缓存与列表相互隔离。
- “本轮更新已加载完成”仅表示平台明确结束；“缓存快照”表示此前获取片段。订阅、收藏仍保持各自原有完整快照替换规则。

## 开发与合成验收

`python tests/e2e/updates.py` 启动专用回环地址的合成后端，在无真实账号的 Headless Chromium 中验证列表、播放、队列、详情返回、故障和窗口布局。前端资源需先执行 `npm test`（在 frontend 目录）；Python 依赖使用 `tests/e2e/requirements.txt`。截图和结果写入忽略的 `docs/test-results/`，不表示原生 Windows 界面验收。
