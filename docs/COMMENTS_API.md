# Comments adapter evidence

Inspected 2026-09-12. Protocol reference: [ultrazg/xyz comment handler](https://github.com/ultrazg/xyz/blob/22cfe7848a98b393a5f9f62c5e9a4eea2592eb1b/handlers/comment.go), commit `22cfe7848a98b393a5f9f62c5e9a4eea2592eb1b`, commit date 2026-08-13. Parameter and response descriptions: [primary comments](https://github.com/ultrazg/xyz/blob/22cfe7848a98b393a5f9f62c5e9a4eea2592eb1b/doc/docs/commentPrimary.md), [threads](https://github.com/ultrazg/xyz/blob/22cfe7848a98b393a5f9f62c5e9a4eea2592eb1b/doc/docs/commentThread.md). This is an unofficial protocol reference, not a platform stability guarantee. Starling's adapter is independently implemented; third-party implementation code is not incorporated.

## Read contract

The optional `provider.CommentReader` leaves the existing Provider interface compatible. Reads require the current account access token. Requests reuse Client.request, its rate limit handling and read retry policy, the existing random per-lifetime device UUID and honest Starling User-Agent. No mobile identity or official app headers are copied.

| Operation | Request | Pagination |
| --- | --- | --- |
| Primary comments | POST `/v1/comment/list-primary`, `owner: {id: episodeID, type: EPISODE}`, `order: HOT` | Opaque local cursor carries the response `loadMoreKey` object: stable `id`, `direction: NEXT`, numeric `hotSortScore`, optional string `section`. No guessed limit. |
| Expand thread | POST `/v1/comment/list-thread`, `primaryCommentId`, `order: SMART` | Source supports no request cursor. Nonempty thread cursor is rejected with UNSUPPORTED before sending. A continuation response remains partial without an actionable cursor. |

`TIME_ASC` is not documented; the thread reference specifies SMART or TIME. Episode ID is checked locally and response owners must match the requested episode. Text is normalized as plain text, with id, author identity, createdAt and threadReplyCount. Inline `replies` on primary items are previews and are not treated as complete threads. Missing/invalid stable IDs, owner mismatch, invalid dates, malformed author/text/counts and within-page duplicate IDs reject the page. Avatar URLs use the existing image allowlist.

The direct upstream envelope contains `data: []`; the extra outer `data` shown in xyz documentation belongs to its proxy wrapper. Primary absence of loadMoreKey means terminal according to the source and live observation. A lone nonempty loadNextKey is rejected instead of silently treated as terminal. Threads require explicit pagination evidence or a nonnegative totalCount equal to the number of returned items to claim completeness. Missing evidence or fewer rows than totalCount stays partial; it never means all replies loaded. Unsupported pagination requires the official client for remaining replies.

## Verification boundary

Unit fixtures in comments_test.go are deliberately synthetic. They exercise body/header construction, primary cursor roundtrip, normalization, missing authentication, invalid IDs/cursors, terminal/partial responses and unsupported thread continuation. They do not prove current platform availability.

The opt-in `TestLiveCommentsReadOnly` uses only Starling's own session.vault, never refreshes or saves credentials, and sends no mutation requests. It logs envelope key names/status/counts only, not account identifiers, comment text, tokens or cursor contents. Set `STARLING_LIVE_COMMENTS=1`; optionally choose `STARLING_LIVE_COMMENT_EPISODE`. Otherwise the first item on the first favorites page is used. The probe reads at most three primary pages and two first-page threads. This bounded sample does not prove large-thread pagination, all accounts, all episodes or future availability.

Live result on 2026-09-12: success with the existing honest Starling headers. Primary page 1 returned 15 items with `data, loadMoreKey, loadNextKey, totalCount`; page 2 returned one item with `data, loadPrevKey, totalCount`, confirming a nonempty terminal page without loadMoreKey. Two sampled threads each had one expected reply and one returned reply, with `data, totalCount` and no cursor. No credential refresh/save and no comment create/delete/like request was performed.
