# Group Auto Message Rotation

## 1. Scope

This document is the implementation contract for "group auto message rotation" across server, iOS, Android, and PC clients.

V1 supports:

- group owner or authorized group admin management
- enable and disable
- minute-based interval
- text-only message items
- add, edit, delete, enable, disable, and sort items
- sequence rotation
- first send after one interval
- next send time and next item preview
- send logs
- Redis lock and database row lock for duplicate prevention
- no catch-up burst after restart or downtime

V1 does not support images, video, files, random mode, fixed daily time, multiple time windows, template variables, cross-group copy, or AI-generated content.

## 2. Client Entry

Entry:

```text
Group detail / group settings -> Auto Messages
```

Visibility:

- owner: visible and editable
- admin: visible and editable only when `admins_can_manage=true`
- member: hidden; if directly opened, server returns `AUTO_MESSAGE_NO_PERMISSION`

The API gateway must inject the logged-in app user id as:

```http
X-HSgram-User-ID: <current_user_id>
```

For local testing only, the REST handlers also accept:

```http
Authorization: User <current_user_id>
```

## 3. Page Fields

| Field | Type | Rule |
| --- | --- | --- |
| `enabled` | switch | cannot enable without at least one enabled item |
| `interval_minutes` | number | 5 to 1440 |
| quick intervals | buttons | 10, 30, 60, 120, 360, 720, 1440 |
| `first_send_mode` | read-only | V1 only `delay` |
| `send_mode` | read-only | V1 only `sequence` |
| `admins_can_manage` | switch | owner can allow group admins to manage |
| `message_items` | list | max 100 |
| item `content` | textarea | required, max 1000 chars |
| item `enabled` | switch | disabled item is skipped |
| item sort | drag/up/down | writes `sort_order` |
| status | read-only | next send, next item, last result |
| logs | paged table | sent time, snapshot, result, failure |

## 4. Core Rules

1. Only `config.enabled=true` is due.
2. Only `item.enabled=true` and `deleted_at IS NULL` items participate.
3. Items are ordered by `sort_order ASC, id ASC`.
4. `current_index` is the index in the enabled item list.
5. Current item is `enabled_items[current_index]`.
6. If `current_index >= len(enabled_items)`, it is corrected to `0`.
7. Success advances `current_index = (current_index + 1) % len(enabled_items)`.
8. Failure keeps `current_index` unchanged.
9. Success and failure both set `next_send_at = now + interval_minutes`.
10. Empty enabled list is not sent and is logged as `AUTO_MESSAGE_NO_ENABLED_ITEMS`.
11. Dissolved group is not sent; config is disabled and logged as `AUTO_MESSAGE_GROUP_DISSOLVED`.
12. Service downtime does not trigger catch-up burst: scheduler sends at most one item for an overdue config.
13. Duplicate prevention uses Redis lock, DB row lock, stable message random id, log, and state advancement.

First send:

```text
enable at T0 with interval 30 minutes
current_index = 0
next_send_at = T0 + 30 minutes
T0+30 -> item 1
T0+60 -> item 2
```

## 5. API

All responses use:

```json
{
  "ok": true,
  "data": {}
}
```

Errors use:

```json
{
  "ok": false,
  "code": "AUTO_MESSAGE_NO_PERMISSION",
  "error": "no permission to manage auto messages"
}
```

### Get Config

```http
GET /groups/{group_id}/auto-messages/config
```

Response:

```json
{
  "ok": true,
  "data": {
    "enabled": true,
    "interval_minutes": 30,
    "send_mode": "sequence",
    "first_send_mode": "delay",
    "admins_can_manage": true,
    "current_index": 0,
    "next_send_at": "2026-05-10T14:30:00Z",
    "last_send_at": "2026-05-10T14:00:00Z",
    "last_send_status": "success",
    "version": 3,
    "message_items": [
      {
        "id": 101,
        "configId": 1,
        "groupId": 2001,
        "sortOrder": 1,
        "content": "Please follow group rules.",
        "contentType": "text",
        "enabled": true
      }
    ],
    "enabled_item_count": 1,
    "next_item": {
      "id": 101,
      "sortOrder": 1,
      "content": "Please follow group rules.",
      "contentType": "text",
      "enabled": true
    },
    "limits": {
      "min_interval_minutes": 5,
      "max_interval_minutes": 1440,
      "max_items": 100,
      "max_content_length": 1000
    }
  }
}
```

### Save Config

```http
POST /groups/{group_id}/auto-messages/config
```

Request:

```json
{
  "enabled": true,
  "interval_minutes": 30,
  "send_mode": "sequence",
  "first_send_mode": "delay",
  "admins_can_manage": true,
  "version": 3,
  "message_items": [
    {
      "id": 101,
      "sort_order": 1,
      "content": "Please follow group rules.",
      "content_type": "text",
      "enabled": true
    }
  ]
}
```

Server behavior:

- validates permission, interval, item count, content, mode
- if enabled, requires at least one enabled item
- if enabled, resets `current_index=0`
- if enabled, sets `next_send_at=now+interval`
- soft deletes omitted existing items because this endpoint saves the whole list

### Enable

```http
POST /groups/{group_id}/auto-messages/enable
```

Request:

```json
{
  "interval_minutes": 30
}
```

The interval field is optional. When omitted, the saved interval is used.

### Disable

```http
POST /groups/{group_id}/auto-messages/disable
```

### Add Item

```http
POST /groups/{group_id}/auto-messages/items
```

Request:

```json
{
  "content": "Welcome new members.",
  "content_type": "text",
  "enabled": true
}
```

### Edit Item

```http
PUT /groups/{group_id}/auto-messages/items/{item_id}
```

Request:

```json
{
  "content": "Updated reminder.",
  "content_type": "text",
  "enabled": true
}
```

### Delete Item

```http
DELETE /groups/{group_id}/auto-messages/items/{item_id}
```

The item is soft deleted. If `current_index` is now out of range, the server corrects it to `0`.

### Sort Items

```http
POST /groups/{group_id}/auto-messages/items/sort
```

Request:

```json
{
  "item_ids": [103, 101, 102]
}
```

### Logs

```http
GET /groups/{group_id}/auto-messages/logs?page=1&page_size=20
```

Response:

```json
{
  "ok": true,
  "data": {
    "items": [
      {
        "id": 1,
        "groupId": 2001,
        "configId": 1,
        "itemId": 101,
        "contentSnapshot": "Please follow group rules.",
        "sendIndex": 1,
        "sentAt": "2026-05-10T14:30:00Z",
        "status": "success",
        "messageId": 0,
        "createdAt": "2026-05-10T14:30:00Z"
      }
    ],
    "total": 1,
    "page": 1,
    "pageSize": 20
  }
}
```

## 6. Error Codes

| Code | HTTP | Meaning | Client copy |
| --- | --- | --- | --- |
| `AUTO_MESSAGE_NO_PERMISSION` | 403 | actor is not owner or authorized admin | No permission |
| `AUTO_MESSAGE_NO_ENABLED_ITEMS` | 400 | no enabled items | Add or enable at least one message |
| `AUTO_MESSAGE_INVALID_INTERVAL` | 400 | invalid interval or unsupported mode | Interval must be 5-1440 minutes |
| `AUTO_MESSAGE_CONTENT_EMPTY` | 400 | content is blank | Message content is required |
| `AUTO_MESSAGE_CONTENT_TOO_LONG` | 400 | content exceeds 1000 chars | Message is too long |
| `AUTO_MESSAGE_ITEM_LIMIT_EXCEEDED` | 400 | more than 100 items | Up to 100 messages |
| `AUTO_MESSAGE_GROUP_DISSOLVED` | 404 | group is inactive | Group is unavailable |
| `AUTO_MESSAGE_SYSTEM_MUTED` | 503 | sender is muted | System sender is muted |
| `AUTO_MESSAGE_NO_SEND_PERMISSION` | 503 | sender cannot speak in group | System sender cannot send |
| `AUTO_MESSAGE_SEND_FAILED` | 503 | message RPC failed | Send failed |
| `AUTO_MESSAGE_LOCK_FAILED` | 503 | Redis lock error | Please retry |
| `AUTO_MESSAGE_CONFIG_NOT_FOUND` | 404 | config missing | Config not found |
| `AUTO_MESSAGE_ITEM_NOT_FOUND` | 404 | item missing | Message item not found |
| `AUTO_MESSAGE_VERSION_CONFLICT` | 409 | multi-client edit conflict | Refresh and try again |

## 7. Scheduler

The admin service starts the scheduler when:

```text
ADMIN_ENABLE_AUTO_MESSAGES=true
ADMIN_MSG_RPC_ADDR is configured
```

Environment:

| Env | Default |
| --- | --- |
| `ADMIN_ENABLE_AUTO_MESSAGES` | `true` |
| `ADMIN_AUTO_MESSAGE_SENDER_USER_ID` | `778000` |
| `ADMIN_AUTO_MESSAGE_TICK_INTERVAL` | `1m` |
| `ADMIN_AUTO_MESSAGE_BATCH_SIZE` | `500` |
| `ADMIN_AUTO_MESSAGE_LOCK_TTL` | `120s` |
| `ADMIN_AUTO_MESSAGE_SHARD_TOTAL` | `1` |
| `ADMIN_AUTO_MESSAGE_SHARD_INDEX` | `0` |

Pseudo flow:

```text
every tick:
  now = current time
  configs = enabled configs where next_send_at <= now order by next_send_at limit batch
  for config:
    lock key auto_message:{group_id} with NX and TTL
    if lock fails: skip
    in DB tx:
      select config for update
      recheck enabled and next_send_at
      check group not dissolved
      load enabled items order by sort_order,id
      correct current_index if out of range
      item = enabled_items[current_index]
      biz_id = auto_message:{config_id}:{scheduled_at}:{current_index}:{item_id}
      send group text through msg.sendMessageV2 with stable random id derived from biz_id
      insert success or failed log with content snapshot
      if success: advance current_index
      if failed: keep current_index
      next_send_at = now + interval
      last_send_at = now
      last_send_status = success or failed
    release Redis lock by request id
```

The stable random id makes retries idempotent at the message service layer when a process dies after sending but before committing state.

## 8. Test Matrix

| ID | Scenario | Expected |
| --- | --- | --- |
| TC01 | owner opens config | 200 with default config |
| TC02 | authorized admin opens config | 200 |
| TC03 | unauthorized admin opens config | 403 |
| TC04 | member opens config | 403 |
| TC05 | enable without items | `AUTO_MESSAGE_NO_ENABLED_ITEMS` |
| TC06 | add one item | item appears at end |
| TC07 | add ten items | all saved with order |
| TC08 | add 101st item | `AUTO_MESSAGE_ITEM_LIMIT_EXCEEDED` |
| TC09 | edit item | content updated |
| TC10 | delete item | item soft deleted |
| TC11 | delete current item | current index corrected |
| TC12 | disable item | skipped by scheduler |
| TC13 | disable all items | scheduler writes failed log |
| TC14 | sort items | next send uses new order |
| TC15 | set 30 minute interval | accepted |
| TC16 | set interval 4 | rejected |
| TC17 | set interval 1441 | rejected |
| TC18 | first due at T+30 | item 1 sent |
| TC19 | second due at T+60 | item 2 sent |
| TC20 | tenth due at T+300 | item 10 sent |
| TC21 | eleventh due at T+330 | item 1 sent |
| TC22 | disable config | no more sending |
| TC23 | change interval to 10 | next send recomputed |
| TC24 | send success | success log written |
| TC25 | send failure | failed log and reason written |
| TC26 | sender muted | failed log `AUTO_MESSAGE_SYSTEM_MUTED` |
| TC27 | sender no permission | failed log `AUTO_MESSAGE_NO_SEND_PERMISSION` |
| TC28 | group dissolved | config disabled |
| TC29 | service restart | no duplicate send |
| TC30 | downtime 2 hours | only one item sent after recovery |
| TC31 | two nodes scan | one gets Redis lock |
| TC32 | Redis error | config skipped and worker logs |
| TC33 | DB update error | tx rolls back |
| TC34 | message timeout | failed log, next interval |
| TC35 | mobile saves then PC refreshes | PC sees new version |
| TC36 | PC saves then mobile refreshes | mobile sees new version |
| TC37 | stale version save | 409 conflict |
| TC38 | next send display | matches server time |
| TC39 | next item display | matches enabled list index |
| TC40 | logs paging | stable order by sent_at,id |
| TC41 | empty content | rejected |
| TC42 | content over 1000 chars | rejected |
| TC43 | admin permission revoked | next request 403 |
| TC44 | owner transfer | new creator can manage |
| TC45 | duplicate sort id | rejected |

## 9. Acceptance

1. Owner can enable auto messages.
2. Authorized admin can manage auto messages.
3. Member cannot configure auto messages.
4. 30 minute interval is supported.
5. 10 text items are supported.
6. First send happens at T+interval.
7. T+30 sends item 1.
8. T+60 sends item 2.
9. T+90 sends item 3.
10. T+300 sends item 10.
11. T+330 sends item 1 again.
12. Disable stops sending.
13. Interval changes recompute next send.
14. Deleted item is never sent.
15. Disabled item is skipped.
16. Sort changes affect next send.
17. Restart does not duplicate.
18. Downtime does not catch up multiple messages.
19. Multi-node deployment does not duplicate.
20. Success writes a log.
21. Failure writes code and reason.
22. No enabled item blocks enabling.
23. Status shows next send time.
24. Status shows next item.
25. iOS, Android, and PC use the same contract and display the same state.
