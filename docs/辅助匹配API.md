# 服务端辅助匹配 API

从 fan-web **v1.6.0** 起，下载脚本、Hermes 等工具可以在视频写入服务器的视频库后调用 `POST /api/library/assist-match`。fan-web 会解析指定文件、匹配 Bangumi 条目并核对集号。调用方可以先查看匹配结果，也可以直接提交入库。服务端需启用默认的 `v2` 匹配引擎。

## 创建密钥

使用管理员账号登录 Web，进入「用户管理 → API 密钥（外部工具接入）」，创建并复制密钥。完整密钥只在创建时显示一次。将它保存在下载工具的密钥配置中，请求使用 `X-API-Key: fw_…`；也支持 `Authorization: Bearer fw_…`。管理员登录 JWT 可通过 `Authorization: Bearer <JWT>` 调用同一接口。

API Key 具有媒体库管理权限，应只交给受信任的工具。远程调用使用 HTTPS，密钥泄露后在用户管理页吊销并更换。

## 先预览，再提交

假设服务器的视频根目录是 `/srv/anime`，已下载的文件在 `/srv/anime/ReZero/Season4`。下例用 Bash 和 curl，按实际服务器地址和文件名替换示例值。在终端输入完整 API Key。

```bash
FAN_WEB_URL='https://fan-web.example.com'
read -rsp 'fan-web API Key: ' FAN_WEB_API_KEY
printf '\n'

curl -sS "${FAN_WEB_URL}/api/library/assist-match" \
  -H "X-API-Key: ${FAN_WEB_API_KEY}" \
  -H 'Content-Type: application/json' \
  --data-binary @- <<'JSON'
{
  "file_path": "ReZero/Season4",
  "file_names": ["Re.Zero.S04E12.mkv", "Re.Zero.S04E13.mkv"],
  "bangumi_id": 633836,
  "episode_offset": 11,
  "auto_commit": false
}
JSON
```

收到 `code: 0` 和 `data.status: "matched"` 后，核对 `data.candidate`（如条目 `633836`，第四季夺还篇）与 `data.bindings`（如文件号 12、13 对应条目内第 1、2 集）。确认后，把请求中的 `auto_commit` 改为 `true`，其余字段保持一致并重发。收到 `committed` 后，可以从 `data.anime.id` 取得 fan-web 内部番剧 ID。已入库的同一批文件符合归属条件时，重复调用会返回已有记录。

指定 `bangumi_id` 后，服务器直接用该 Bangumi 条目的章节解释文件集号。这里选择第四季夺还篇（`633836`），用 `episode_offset: 11` 将文件号 12、13 换算为条目内第 1、2 集。偏移必须小于每个待换算的文件号，且仅适用于条目总话数已知、原始编号超过该条目总话数的累计编号。文件本来就按条目内集号命名时，省略偏移即可。只处理一个文件时，也可以直接把它的相对路径放进 `file_path`，省略 `file_names`。

## 让服务器推断条目

不知道 Bangumi ID 时，省略 `bangumi_id`。服务器可以只根据文件名和目录匹配，也可以使用 `title_hint` 和 `season_hint` 缩小范围。优先提供包含续作或分篇的完整标题。先用 `auto_commit: false` 看候选与集号，例如：

```json
{
  "file_path": "ReZero/Season4",
  "file_names": ["Re.Zero.S04E12.mkv", "Re.Zero.S04E13.mkv"],
  "title_hint": "Re：从零开始的异世界生活 第四季 夺还篇",
  "season_hint": 4,
  "auto_commit": false
}
```

候选证据不足时会返回 `review_required`。核对 Bangumi 上的具体条目后，可用上面的 ID 请求预览。如果同一目录混有不同作品或分篇，请用 `file_names` 分开提交。

## 请求字段

| 字段 | 用法 |
| --- | --- |
| `file_path` | 相对于 **fan-web 服务器的视频根目录**的目录或单个视频文件，例如 `ReZero/Season4`。空字符串表示视频根目录。 |
| `file_names` | 目录模式下，可指定同目录内的文件名数组，文件名不带路径。省略时处理该目录层级尚未关联的视频，不递归子目录。单文件路径可省略此字段。 |
| `bangumi_id` | 已知的 Bangumi 条目 ID，填正整数。省略或为 0 时，服务器从文件名、目录及可选提示推断候选。 |
| `title_hint`、`season_hint` | 自动推断使用的作品名和季号。应只把同一作品、同一季的文件放在同一批中。 |
| `episode_offset` | 从文件号减去的偏移量，默认 0。 |
| `auto_commit` | `false` 只预览，不变更番剧、剧集或收件箱。API Key 的最近使用时间仍可能更新。默认 `true`，允许入库及写入收件箱。 |

## 如何处理响应

接口返回 JSON 信封 `{ "code": ..., "message": ..., "data": ... }`。业务响应使用 HTTP 200，客户端应先检查 `code`，再按 `data.status` 分支。

| `data.status`（`code=0`） | 含义 |
| --- | --- |
| `matched` | 预览已得到整批绑定。读取 `candidate` 和 `bindings`，每个绑定包含 `file_name` 与 `ep_number`。 |
| `committed` | 已入库，或请求中的文件已全部归属同一番剧。读取 `anime` 与 `episodes`。只有新建番剧时才返回 `created: true`；字段缺省等价于 `false`。 |
| `review_required` | 身份或章节证据不足，需要确认。读取 `reason` 和可用的 `candidates`，可以缩小文件范围、补充条目或修正偏移后重试。 |
| `no_match` | 未找到候选，或目标无法收纳整批指定文件。读取 `reason` 和可用的 `file_names`。 |

自动推断未取得可提交结果时，`auto_commit: true` 会将待匹配文件写入未识别收件箱，供管理员复核。指定 `bangumi_id` 后仍遇到章节问题时，按返回的 `reason` 处理。

`code` 非 0 时读取 `message` 与可用的 `data.reason`。`2001` 表示凭据无效或已吊销，`2002` 表示权限不足，`1001` 表示参数、路径或文件选择有误。`9999` 时根据原因处理，例如 `dir_mismatch` 表示条目已绑定到其他目录，`conflict` 表示文件归属或并发状态冲突，`upstream` 表示匹配依赖或服务操作失败，`unsupported_engine` 表示当前引擎不支持辅助匹配。

匹配同步执行，耗时取决于文件数量和 Bangumi 响应。调用方超时后，先预览或查询入库结果，再决定是否重试。
