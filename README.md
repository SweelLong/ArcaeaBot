# ArcaeaBot

这是基于 NapCatQQ 框架开发的面向 [Arcaea-server](https://github.com/Lost-MSth/Arcaea-server) 部署的 Go 版 ArcaeaBot。

数据库访问统一使用 `sqlite://` URL(Windows 不要用反斜杠 `\` 推荐使用相对路径定位如`../`)，理论上支持 [Arcaea_server_rs](https://github.com/YinMo19/Arcaea_server_rs) 版本服务端。

## 运行要求

- Go 1.24+
- NapCat WebSocket 服务器
- Arcaea SQLite 业务数据库
- Arcaea bundle 资源，指服务器的 bundle (含 char、songs 等) 目录 （解包并获取于客户端一致的贴图）

## 配置

配置文件为 `config.yaml`，模板为 `config.yaml.example`。

```sh
cp config.yaml.example config.yaml
```

`config.yaml` 包含 Token、数据库密码和本机路径，已被 Git 忽略，不要提交。程序只读取 YAML 配置。

主要配置项如下，完整默认值见模板：

- `ws_url`、`ws_token`、`bot_qq`、`thread_count`：NapCat 与运行时配置。
- `enabled_plugins`：插件内部名列表，空列表表示全部启用，`[none]` 表示全部关闭。
- `public_commands`：未绑定账号时仍允许执行的指令名称。
- `arcaea_database_url`：Arcaea 业务主库。
- `arcaea_log_database_url`：Arcaea 日志库，PTT 趋势使用其中的 `user_rating` 表。
- `data_path`、`resources_path`、`tmp_path`、`bundle_path`：本地路径。
- `group_whitelist`：群聊插件范围和注册身份验证群列表。
- `llm_id`、`llm_url`、`llm_api_key`：聊天 AI 配置。

SQLite 业务库配置示例：

```yaml
arcaea_database_url: sqlite:///srv/Arcaea-server/database/arcaea_database.db
```

## 目录结构

```text
main.go                 程序入口
internal/config/        config.yaml 配置加载
internal/database/      SQLite 连接、多库封装及我 KV 存储
internal/debundler/     Go 版 ArcaeaDebundler
internal/plugins/       具体插件实现
internal/utils/         插件总控及可复用的文件、数据库、交互和绘图工具

arcaea_database.db      SQLite 业务主库，本地文件不提交

data/
  arcaeabot.db          本地我/插件 KV 数据库，运行时生成
  *.yaml、图片及目录     插件静态资源，参与版本管理

tmp/
  debundler/            启动时自动解包得到的资源
  b30/                  B30/P30/T30 图片输出
  recent/               最近游玩图片输出
  rank/                 排行榜图片输出
  help/                 帮助图输出
  report/               BUG 反馈图片暂存
```

`data` 顶层存放插件静态资源和我 KV 数据库；运行时生成、下载、解包的文件统一放在 `tmp`。各插件输出使用固定文件名，重复生成时覆盖旧文件。

戳一戳回复与 AI 提示词位于 `data/chat/poke.yaml` 和 `data/chat/chat.yaml`，Ai 酱资源位于 `data/ai_chan/`。修改这些资源文件即可更新内容，无需修改插件代码。帮助菜单的注意事项由 `help_tips` 配置项提供。

## 启动

```sh
go run .
go build -o arcaeabot . && ./arcaeabot
```

正常启动会自动执行 Go 版 Debundler：

## 部署

推荐在本机交叉编译后上传到服务器。

### 交叉编译

```sh
# Linux amd64（最常用的 x86_64 实例）
GOOS=linux GOARCH=amd64 go build -o arcaeabot-linux-amd64 .

# Linux arm64（如树莓派等 ARM 架构实例）
GOOS=linux GOARCH=arm64 go build -o arcaeabot-linux-arm64 .
```

### 上传文件

```sh
# 上传二进制、配置和静态资源
mkdir -p /opt/arcaeabot
scp arcaeabot-linux-amd64 config.yaml root@<服务器>:/opt/arcaeabot/
rsync -av --exclude 'arcaeabot.db*' data/ root@<服务器>:/opt/arcaeabot/data/
```

### 服务器启动

```sh
ssh root@<服务器>
cd /opt/arcaeabot && ./arcaeabot
```

建议使用 systemd 管理进程以保证开机自启和崩溃恢复：

```ini
# /etc/systemd/system/arcaeabot.service
[Unit]
Description=ArcaeaBot
After=network.target

[Service]
Type=simple
User=root
WorkingDirectory=/opt/arcaeabot
ExecStart=/opt/arcaeabot/arcaeabot
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
```

```sh
systemctl start arcaeabot     # 启动
systemctl stop arcaeabot      # 停止
systemctl restart arcaeabot   # 重启
journalctl -u arcaeabot -f    # 查看实时日志
```

### 部署清单

| 文件/目录 | 说明 |
|---|---|
| `arcaeabot-linux-*` | 编译后的二进制 |
| `config.yaml` | 生产环境配置 |
| `data/` | 静态资源及我 KV 数据库 |
其余源码文件（`internal/`、`go.mod` 等）无需上传。

1. 读取 `bundle_path` 下所有 JSON 元数据及其同名 CB 文件
2. 从 `previousVersionNumber: null` 开始按版本链顺序处理
3. 每个版本先删除 `removed`，再将 `added` 解压覆盖到 `tmp/debundler`
4. 按 `debundler_folders` 过滤资源目录

## 数据存储

默认情况下，本地插件数据存放在一个 SQLite 文件中：

```text
data/arcaeabot.db
```

Bot 数据库是高度封装的 KV 数据库。程序会自动创建数据库文件和基础结构；某个插件第一次调用自己的命名空间时，才会自动创建对应的 KV 表，不需要手动建库、建表或执行迁移。

每个插件可以使用自己的 KV 命名空间。内部会创建 `kv_<命名空间>` 表，表结构只包含：

- `key`
- `value`

插件代码通过统一的键路径接口读写数据，键路径的第一个元素是命名空间，后续元素是具体键：

```go
var value MyValue
ok, err := db.Get(ctx, []string{"plugin_name", "user", "123"}, &value)

err = db.Set(ctx, []string{"plugin_name", "user", "123"}, value)
_, err = db.Delete(ctx, []string{"plugin_name", "user", "123"})
```

字符串、数字、结构体、切片和 map 都会自动使用 JSON 序列化。插件不需要直接操作 Bot 数据库，也不需要自行检查或创建表。

用户 QQ 与游戏 `user_id` 的绑定关系也保存在 KV 中：

```text
binding / qq / <QQ> -> <user_id>
binding / user / <user_id> -> <QQ>
```

游戏业务库由 `arcaea_database_url` 指定。Bot KV 数据库与 Arcaea 业务数据库相互独立。

## 注册、绑定与权限

注册和绑定允许在私聊中执行，但用户必须属于配置文件 `group_whitelist` 中的至少一个群。我会通过群成员信息完成身份验证，群聊消息仍然只在白名单群中处理。

未绑定用户由插件总路由统一拦截，其他插件和注册插件内部都不再重复回复注册提示。未绑定用户仅允许使用注册和绑定命令：

- `#注册 [用户名] [密码]`
- `#绑定 [好友码]`

包括 `#帮助`、`#新密码`、`#改名` 在内的其他命令，都会统一回复：

```text
请先注册或绑定账号！(#注册 [用户名] [密码] / #绑定 [好友码])
```

`#改名` 等需要账号数据的命令也由总开关限制。插件业务代码只处理正常业务和实际数据库错误，不再自行回复“请先注册”类提示。

`#绑定` 使用游戏账号的 `user_code` 好友码查找账号；好友码不存在时会提示：`绑定失败：找不到这个好友码！`

用户退群时，当前绑定的游戏账号密码会被清空。

## 插件

插件总控与可复用工具位于 `internal/utils`，具体插件均位于 `internal/plugins`，插件通过各自文件中的 `init()` 自动登记，新增插件无需修改中央加载清单。同一业务域的插件会合并在同一个源码文件中。可以在 `config.yaml` 中只启用需要的插件，例如：

```yaml
enabled_plugins: [help, acct, b30, stats, notice]
```

可用的插件内部名：

```text
help, acct, alias, song, ai, b30, stats, guy, rpt, ptt,
tx, frag, check, snatch, notice, acv, sticker, chat, rep,
rand, guess, tarot, admin, files, board, fun
```

留空或填写 `*` 会加载全部插件；填写 `none` 会关闭全部插件。未启用的插件不会注册，其指令也不会出现在帮助菜单中。

功能包括：

- 账号注册、绑定、找回、改名
- 曲目别名与曲目信息
- B30、P30、T30
- 排行榜、最近游玩、课题排行、定数查询
- 签到（奖励由 `check_in_rewards` 配置）、物品兑换、转账、全量抢夺
- BUG 反馈
- Ai 酱推荐
- 钙哥图片
- 猜歌、塔罗、随机问答、留言板、群管理、群文件和趣味图片
- 聊天 AI、戳一戳、进退群通知、表情处理和 Arcaea C 版最新直链查询。

## TODO

- [ ] 当 NapCat 断连的时候，应该静候等待，间隔性地主动尝试重新连接，而不是直接关闭进程。
- [ ] 微调图片生成相关插件的排版。

## License

[MIT License](LICENSE)
