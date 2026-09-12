# Exploration provider contract

Source: [jihuayu/xyz at 025ae32a4d7461aa47191cb5cd5043a1d2a92a19](https://github.com/jihuayu/xyz/tree/025ae32a4d7461aa47191cb5cd5043a1d2a92a19), inspected 2026-09-12. This is a third-party compatibility reference, not a platform support guarantee. These adapters call the existing direct API origin; the proxy's outer `{code,data,msg}` wrapper is absent.

| Feature | Direct request | Source files |
| --- | --- | --- |
| Search | POST `/v1/search/create`, `keyword`, uppercase `type`, string `limit:"20"`, `sourcePageName:"4"`, `currentPageName:"4"`, optional `loadMoreKey` | `handlers/search.go`, `doc/docs/search.md` |
| Suggestions | GET `/v1/search/get-preset` | `handlers/search.go`, `doc/docs/searchPreset.md` |
| Creator profile | GET `/v1/profile/get?uid=...` | `handlers/profile.go`, `doc/docs/getProfile.md` |
| Creator owned shows | POST `/v1/podcaster/owned-podcasts`, `uid` | `handlers/podcast.go`, `doc/docs/ownedPodcasts.md` |
| Subscription read | GET `/v1/podcast/get?pid=...` | `handlers/podcast.go`, `doc/docs/podcastDetail.md` |
| Subscribe only | POST `/v1/subscription/update`, `pid`, `mode:"ON"` | `handlers/subscription.go`, `doc/docs/subscriptionUpdate.md` |

All requests retain Starling's existing User-Agent, per-client random device identifier, redirect restrictions, public-address DNS checks, concurrency limits, timeouts, response caps and cooldown behavior. No mobile impersonation headers were copied.

## Minimal synthetic fixtures

These are hand-written contract fixtures, not captured account data. Arbitrary IDs are valid-shaped test identifiers. Content fields are deliberately reduced.

```json
{"data":[{"pid":"6013f9f58e2f7ee375cf4216","title":"节目","subscriptionStatus":"OFF"}],"hasMore":false}
```

```json
{"data":[{"eid":"6013f9f58e2f7ee375cf4216","podcast":{"pid":"6013f9f58e2f7ee375cf4216"}}],"loadMoreKey":{"loadMoreKey":20,"searchId":"search-one"}}
```

```json
{"data":[{"type":"SEARCHED_USERS","users":[{"uid":"5fa391a5e0f5e723bbd34c78","nickname":"作者"}]}],"hasMore":false}
```

```json
{"data":[{"text":"中文"}]}
```

```json
{"data":{"uid":"5fa391a5e0f5e723bbd34c78","nickname":"作者","bio":"简介"}}
```

```json
{"data":[]}
```

```json
{"data":{"pid":"6013f9f58e2f7ee375cf4216","subscriptionStatus":"ON"}}
```

The source search example is an ALL-category aggregate; we request only PODCAST, EPISODE or USER. Decoder supports direct USER objects and the documented SEARCHED_USERS container. Unexpected entries fail rather than silently discard results. The synthetic `hasMore` cases exercise existing generic page semantics; they do not assert a live search server emits this flag.

## Normalization and limits

Queries must be valid UTF-8, trim to 1–200 Unicode runes, and contain no control characters before trimming. Search kinds are an explicit allowlist. Tokens and stable IDs use existing validation. Metadata text is bounded and control-cleaned; images use the existing HTTPS image allowlist. Show notes and media URLs are removed from discovery metadata. Renderer must continue treating metadata as plain text. Duplicate stable IDs are retained once per page. Missing or unfamiliar subscription status remains `unknown`; only exact ON and OFF become subscribed/not_subscribed.

Search lists cap at 200 entries, including nested users. Suggestions cap at 100 entries and use query validation. Creator owned lists cap at 200 entries. Over-limit responses fail; no silent truncation of result lists. Existing HTTP body cap applies before JSON parsing.

Search cursors contain a query+kind SHA-256 scope and a normalized upstream key. The API key must contain a bounded nonnegative integer offset and nonempty bounded searchId, within a 2 KiB JSON limit. The outer cursor has the existing 8 KiB cap. Changed query/category and repeated cursor fail. Direct read-only checks on 2026-09-12 confirmed the same continuation contract for PODCAST, EPISODE and USER: each returned two pages of 20 results. All three categories support continuation. Follow-up terminal-page probes confirmed that successful valid search arrays omitting both loadMoreKey and hasMore are terminal, including zero results. This rule is local to Search; DecodePage remains conservative for other endpoints. Contradictory flags, empty flattened pages with continuation and invalid keys fail.

Owned-podcasts is documented as a complete nonpaginated UID list with no cursor input. A successful array is complete; continuation metadata or a supplied cursor fails. Profile UID must match the requested UID. An empty owned array still preserves the profile.

Subscribe issues one POST and has no automatic retry. It verifies returned podcast ID and exact subscriptionStatus ON. Network/timeout/cancellation after dispatch, server failures, malformed or mismatched response produce `SUBSCRIPTION_UNCERTAIN`. Explicit auth/permission/rate/request rejection stays typed. A pre-cancelled context is rejected before dispatch. A subsequent authoritative read can reconcile unknown results. Neither this adapter nor its opt-in live test performs a reconciliation write or unsubscribe.

## Validation boundary

`go test ./internal/provider` runs synthetic contracts and skips live account checks by default. `TestLiveDiscoveryReadOnly` requires explicit `STARLING_LIVE_DISCOVERY=1`, uses only Starling's own protected vault, makes no credential refresh/save or subscription mutation, logs counts/status only, and has a 55-second total context timeout. It checks suggestions, three search types, up to one continuation, one creator and one subscription state read. Optional `STARLING_LIVE_DISCOVERY_QUERY` selects the query; the default is 科技.

2026-09-12 parent verification: both 科技 and 有知有行 probes passed after correcting the older reference's EPISODE-only paging assumption. 科技 returned 7 suggestions, 20+20 results for each of the three types, a valid profile with zero owned shows and not_subscribed state. 有知有行 returned 7 podcast and 12 user results with no continuation metadata, 20+20 episodes, a valid creator profile with 5 owned shows, and not_subscribed state. No credentials, content bodies or IDs were emitted in logs. Initial end-of-list uncertainty was resolved by the follow-up probes below; no live subscription POST was sent. Real subscription success/readback and native Wails interaction require the user to perform a deliberate subscription in the candidate UI.

Follow-up 2026-09-12: PODCAST 量子 returned 20 x 6 + 6 = 126 results; USER 有知 returned 20 + 20 + 10 = 50; EPISODE 脑放电波 returned 20 x 7 + 1 = 141. All preceding pages included a valid loadMoreKey, all terminal pages omitted it, and no page included hasMore. No cross-page duplicate IDs occurred. Exact podcast search 脑放电波 returned one result; a no-match query returned zero, both with the same terminal envelope. Requested limit=2 was not honored, so row count is not a termination rule. Minimal terminal shape: {data: [...], highlightWord: {...}}. These are read-only observations, not an official API guarantee.
