# CyberStrikeAI 机器人使用说明

[English](robot_en.md)

本文档说明如何通过**个人微信**、**钉钉**、**飞书**与 **企业微信** 与 CyberStrikeAI 对话（长连接 / 回调模式），在手机端即可使用，无需在服务器上打开网页。按下面步骤操作可避免常见弯路。

---

## 一、在 CyberStrikeAI 里从哪里配置

1. 登录 CyberStrikeAI Web 端  
2. 左侧导航进入 **系统设置**  
3. 在左侧设置分类中点击 **机器人设置**（位于「基本设置」与「安全设置」之间）  
4. 按平台配置：
   - **个人微信**：点击「微信 / iLink」→「生成二维码并绑定」，用微信扫码确认（见 [3.4 个人微信](#34-个人微信-wechat--ilink)）
   - **钉钉**：勾选并填写 Client ID / Client Secret
   - **飞书**：勾选并填写 App ID / App Secret
5. 点击 **应用配置** 保存（微信扫码绑定成功后会**自动保存并启用**，一般无需再点）
6. **重启 CyberStrikeAI 应用**（钉钉/飞书：只保存不重启，长连接不会建立；微信绑定成功后会自动重启连接，通常无需手动重启）

配置会写入 `config.yaml` 的 `robots` 段，也可在配置文件中直接编辑。**修改钉钉/飞书配置后必须重启，长连接才会生效。** 个人微信绑定成功后程序会自动写入 `robots.wechat` 并重启 iLink 长轮询。

---

## 二、支持的平台（长连接 / 回调）

| 平台     | 说明 |
|----------|------|
| 个人微信 | 使用微信 iLink 协议，Web 端扫码绑定后长轮询收消息，**无需公网回调** |
| 钉钉     | 使用 Stream 长连接，程序主动连接钉钉接收消息 |
| 飞书     | 使用长连接，程序主动连接飞书接收消息 |
| 企业微信 | 使用 HTTP 回调接收消息，被动回包 + 主动调用企业微信发送消息 API |
| Telegram | Bot API 长轮询（getUpdates），**无需公网回调** |
| Slack    | Socket Mode（出站 WebSocket），**无需公网回调** |
| Discord  | Gateway WebSocket，**无需公网回调** |
| QQ 机器人 | QQ 开放平台 WebSocket（C2C / 群 @），**无需公网回调** |

下面第三节会按平台写清：在开放平台要做什么、要复制哪些字段、填到 CyberStrikeAI 的哪一栏。

---

## 三、各平台配置项与详细步骤

### 3.1 钉钉

**先搞清楚：两种钉钉机器人不一样**

| 类型 | 从哪里创建 | 能否做「用户发消息→机器人回复」 | 本程序是否支持 |
|------|------------|----------------------------------|----------------|
| **自定义机器人** | 钉钉群里：群设置 → 添加机器人 → 自定义（Webhook） | ❌ 不能，只能你往群里发消息 | ❌ 不支持 |
| **企业内部应用机器人** | [钉钉开放平台](https://open.dingtalk.com) 创建应用并开通机器人 | ✅ 能 | ✅ 支持 |

如果你手里是「自定义机器人」的 Webhook 地址（`oapi.dingtalk.com/robot/send?access_token=xxx`）和加签密钥（`SEC...`），**不能直接填到本程序**，必须按下面步骤在开放平台创建「企业内部应用」并拿到 **Client ID**、**Client Secret**。

---

**钉钉配置完整步骤（按顺序做）**

1. **打开钉钉开放平台**  
   浏览器访问 [https://open.dingtalk.com](https://open.dingtalk.com)，用**企业管理员**账号登录。

2. **进入应用开发**  
   左侧选 **应用开发** → **企业内部开发** → 点击 **创建应用**（或选择已有应用）。填写应用名称等基本信息后创建。

3. **拿到 Client ID 和 Client Secret**  
   - 左侧点 **凭证与基础信息**（在「基础信息」下）。  
   - 页面上有 **Client ID（原 AppKey）** 和 **Client Secret（原 AppSecret）**。  
   - 点击复制，**不要手打**，注意：数字 **0** 和字母 **o**、数字 **1** 和字母 **l** 容易抄错（例如 `ding9gf9tiozuc504aer` 中间是数字 **504** 不是 5o4）。

4. **开通机器人并选 Stream 模式**  
   - 左侧 **应用能力** → **机器人**。  
   - 打开「机器人配置」开关。  
   - 填写机器人名称、简介等（必填项按提示填）。  
   - **关键**：消息接收方式要选 **「Stream 模式」**（流式接入）。若只有「HTTP 回调」或未选 Stream，本程序收不到消息。  
   - 保存。

5. **权限与发布**  
   - 左侧 **权限管理**：搜索「机器人」「消息」等，勾选**接收消息**、**发送消息**等机器人相关权限，并确认授权。  
   - 左侧 **版本管理与发布**：若有未发布配置，点击 **发布新版本** / **上线**，否则修改不生效。

6. **填回 CyberStrikeAI**  
   - 回到 CyberStrikeAI → 系统设置 → 机器人设置 → 钉钉。  
   - 勾选「启用钉钉机器人」。  
   - **Client ID (AppKey)** 粘贴第 3 步复制的 Client ID。  
   - **Client Secret** 粘贴第 3 步复制的 Client Secret。  
   - 点击 **应用配置**，然后**重启 CyberStrikeAI**。

---

**CyberStrikeAI 钉钉栏位对照**

| CyberStrikeAI 中填写项 | 在钉钉开放平台的来源 |
|------------------------|------------------------|
| 启用钉钉机器人 | 勾选即启用 |
| Client ID (AppKey) | 凭证与基础信息 → **Client ID（原 AppKey）** |
| Client Secret | 凭证与基础信息 → **Client Secret（原 AppSecret）** |

---

### 3.2 飞书 (Lark)

| 配置项 | 说明 |
|--------|------|
| 启用飞书机器人 | 勾选后启动飞书长连接 |
| App ID | 飞书开放平台应用凭证中的 App ID |
| App Secret | 飞书开放平台应用凭证中的 App Secret |
| Verify Token | 事件订阅用（可选） |

**飞书配置简要步骤**：登录 [飞书开放平台](https://open.feishu.cn) → 创建企业自建应用 → 在「凭证与基础信息」中获取 **App ID**、**App Secret** → 在「应用能力」中开通**机器人**并启用相应权限 → **在「事件订阅」中添加事件**（见下）→ 发布应用 → 将 App ID、App Secret 填到 CyberStrikeAI 机器人设置 → 保存。

**重要：事件订阅**  
飞书长连接只有在开放平台订阅了「接收消息」事件后才会收到用户消息。请在该应用的 **事件订阅** 页面点击「添加事件」，在「消息与群组」下勾选 **接收消息（im.message.receive_v1）** 或同类事件；若未添加，连接会建立成功但收不到任何消息，表现为发消息后本地无日志、机器人无回复。

**飞书权限配置（必读）**  
在 **权限管理** 中需开通以下权限（与开放平台列表中的名称、标识一致）；修改后需在 **版本管理与发布** 中发布新版本才生效。

| 权限名称（开放平台中显示） | 权限标识 | 说明 |
|----------------------------|----------|------|
| 获取与发送单聊、群组消息 | `im:message` | 收发消息的基础权限，**必须开通**。 |
| 接收群聊中@机器人消息事件 | `im:message.group_at_msg:readonly` | 群聊中 @ 机器人时收消息，需开通。 |
| 读取用户发给机器人的单聊消息 | `im:message.p2p_msg:readonly` | 单聊收消息，**必须开通**，否则私聊发消息没反应。 |
| 获取单聊、群组消息 | `im:message:readonly` | 读取消息内容，**必须开通**。 |

**事件订阅**（与权限分开配置）：在 **事件订阅** 中添加 **接收消息（im.message.receive_v1）**，否则长连接收不到消息推送。

- **单聊**：在飞书里打开与机器人的私聊窗口，直接发「帮助」或任意文字即可，无需 @。  
- **群聊**：在群里只有 **@ 机器人** 后发送的内容才会被机器人收到并回复。

---

### 3.3 企业微信 (WeCom)

> 企业微信目前采用「HTTP 回调 + 主动发送消息 API」的方式工作：  
> - 用户发消息 → 企业微信以加密 XML **回调到你的服务器**（本程序的 `/api/robot/wecom`）；  
> - CyberStrikeAI 解密并调用 AI → 使用企业微信的 `message/send` 接口**主动发消息给用户**。

**配置概览：**

- 在企业微信管理后台创建或选择一个**自建应用**。
- 在该应用的「接收消息」处配置回调 URL、Token、EncodingAESKey。
- 在 CyberStrikeAI 的 `config.yaml` 中填入：
  - `robots.wecom.corp_id`：企业 ID（CorpID）
  - `robots.wecom.agent_id`：应用的 AgentId
  - `robots.wecom.token`：消息回调使用的 Token
  - `robots.wecom.encoding_aes_key`：消息回调使用的 EncodingAESKey
  - `robots.wecom.secret`：该应用的 Secret（用于调用企业微信主动发送消息接口）

> **重要：IP 白名单（errcode 60020）**  
> CyberStrikeAI 使用 `https://qyapi.weixin.qq.com/cgi-bin/message/send` 主动发送 AI 回复。  
> 若企业微信日志或本程序日志中出现 `errcode 60020 not allow to access from your ip`：
>
> - 说明你的服务器出口 IP **没有加入企业微信的 IP 白名单**；  
> - 请在企业微信管理后台中找到该自建应用的**「安全设置 / IP 白名单」**（具体入口可能因版本略有不同），将运行 CyberStrikeAI 的服务器公网 IP（如 `110.xxx.xxx.xxx`）加入白名单；  
> - 保存后等待生效，再次发送消息测试。
>
> 如果 IP 未加入白名单，企业微信会拒绝主动发送消息，表现为：  
> - 回调接口 `/api/robot/wecom` 能正常收到并处理消息；  
> - 但手机端**始终收不到 AI 回复**，日志中有 `not allow to access from your ip` 提示。

---

### 3.4 个人微信 (WeChat / iLink)

> 个人微信采用「Web 扫码绑定 + iLink 长轮询」方式工作：  
> - 在 CyberStrikeAI Web 端生成二维码 → 用**手机微信**扫码并确认绑定；  
> - 绑定成功后自动写入 `config.yaml` 的 `robots.wechat`，并启动 iLink 长轮询（程序主动连接 `ilinkai.weixin.qq.com` 收消息）；  
> - **无需**在服务器上配置公网回调 URL，也**无需**去微信开放平台注册应用。

**与企业微信的区别**

| 项目 | 个人微信 (iLink) | 企业微信 (WeCom) |
|------|------------------|------------------|
| 使用场景 | 个人微信私聊 | 企业微信自建应用 |
| 配置方式 | Web 端扫码绑定 | 管理后台配置回调 URL + Token |
| 是否需要公网 | 否（长轮询出站即可） | 是（需可被企业微信访问的 HTTPS 回调） |
| 配置段 | `robots.wechat` | `robots.wecom` |

**绑定步骤（按顺序做）**

1. **登录 CyberStrikeAI Web 端**  
   左侧 **系统设置** → **机器人设置** → 点击 **微信 / iLink** 卡片。

2. **（可选）勾选「启用微信机器人」**  
   首次绑定可跳过；绑定成功后会自动勾选并启用。

3. **生成二维码**  
   点击 **「生成二维码并绑定」**。页面会显示二维码（约 **5 分钟**有效；过期请重新生成）。

4. **微信扫码确认**  
   - 用手机微信扫描页面二维码；  
   - 按手机提示完成确认；  
   - 若手机微信弹出**配对数字**，在 Web 页面对应输入框填写并点击 **提交**（仅部分账号需要）。

5. **等待绑定完成**  
   页面显示「绑定成功，微信机器人已启用」即完成。`bot_token`、`ilink_bot_id` 等会自动写入 `config.yaml`，程序会自动重启 iLink 长轮询，**一般无需手动重启服务**。

6. **在手机微信里测试**  
   打开与 CyberStrikeAI 机器人的**私聊**（绑定后微信内会出现对应会话），发送「帮助」或任意文字测试。

**CyberStrikeAI 微信栏位说明**

| 栏位 | 说明 |
|------|------|
| 启用微信机器人 | 勾选后启动 iLink 长轮询；绑定成功后会自动勾选 |
| 生成二维码并绑定 | 发起扫码绑定流程 |
| **高级设置**（一般保持默认即可） | |
| API Base URL | 默认 `https://ilinkai.weixin.qq.com` |
| Bot Type | 默认 `3` |
| Bot Agent | 默认 `CyberStrikeAI/1.0` |
| iLink Bot ID | 绑定成功后自动填充，只读 |

**使用方式**

- 仅支持在与机器人的**私聊**中对话，直接发送文字即可，**不需要 @**。  
- 不支持群聊 @ 机器人（与钉钉/飞书群聊不同）。  
- 仅处理**文本消息**；图片、语音等会忽略或提示暂不支持。

**重新绑定**

- 若需更换绑定的微信账号，在机器人设置页点击 **「重新绑定」**，再次扫码即可。  
- 若提示「该微信已绑定过，无需重复绑定」，说明该账号此前已完成绑定。

**常见问题**

| 现象 | 处理 |
|------|------|
| 二维码过期 | 重新点击「生成二维码并绑定」（有效期约 5 分钟） |
| 扫码后要求输入数字 | 查看手机微信显示的配对数字，在 Web 页面输入并提交 |
| 绑定成功但发消息无回复 | 看程序日志是否有 `微信 iLink 长轮询已启动`、`微信收到消息`；确认已勾选「启用微信机器人」 |
| 断网或睡眠后无回复 | 程序会自动重连（约 5～60 秒）；仍无回复可重启 CyberStrikeAI |
| 无法生成二维码 | 确认服务器能访问 `https://ilinkai.weixin.qq.com`（出站 HTTPS） |

---

### 3.5 Telegram

> Telegram 使用 **Bot API 长轮询**（`getUpdates`）：程序主动连接 `api.telegram.org` 收消息，**无需公网回调**。

**配置步骤：**

1. 在 Telegram 中找 **@BotFather**，发送 `/newbot` 创建机器人，获得 **Bot Token**。  
2. CyberStrikeAI → **系统设置** → **机器人设置** → **Telegram**。  
3. 勾选「启用 Telegram 机器人」，粘贴 **Bot Token**。  
4. （可选）填写 Bot Username（不含 `@`），或留空由程序自动 `getMe`。  
5. （可选）勾选「允许群聊」— 群聊中仅响应 **@机器人** 的消息。  
6. 点击 **应用配置**（会自动重启长轮询连接）。

**使用：** 与机器人私聊直接发消息；群聊需 @ 机器人（且已勾选允许群聊）。

---

### 3.6 Slack

> Slack 使用 **Socket Mode**（出站 WebSocket）：需 **Bot Token** 与 **App-Level Token**，**无需公网回调**。

**配置步骤：**

1. 在 [Slack API](https://api.slack.com/apps) 创建 App → 启用 **Socket Mode**。  
2. **Basic Information** → **App-Level Tokens** → 创建 token（scope: `connections:write`），即 **xapp-** 开头。  
3. **OAuth & Permissions** → 添加 Bot Token Scopes：`app_mentions:read`、`chat:write`、`im:history`、`im:read` 等 → 安装到工作区，获得 **xoxb-** Bot Token。  
4. **Event Subscriptions** → 订阅 `message.im`、`app_mention` 等（Socket Mode 下在应用内配置）。  
5. 在 CyberStrikeAI 填入 Bot Token 与 App-Level Token → **应用配置**。

**使用：** 与 Bot 私聊直接发；频道中需 @ 机器人。

---

### 3.7 Discord

> Discord 使用 **Gateway WebSocket**：程序主动连接 Discord Gateway，**无需公网回调**。

**配置步骤：**

1. 在 [Discord Developer Portal](https://discord.com/developers/applications) 创建应用 → **Bot** → 复制 **Token**。  
2. 开启 **Privileged Gateway Intents** 中的 **Message Content Intent**（否则读不到消息正文）。  
3. OAuth2 → URL Generator → scopes: `bot` → 权限勾选 **Send Messages**、**Read Message History** 等 → 邀请 Bot 到服务器。  
4. CyberStrikeAI → **机器人设置** → **Discord** → 填入 Token → **应用配置**。  
5. （可选）勾选「允许服务器频道」— 频道中仅响应 **@机器人**。

**使用：** 与 Bot 私聊直接发；服务器频道需 @ 机器人（且已勾选允许服务器频道）。

---

### 3.8 QQ 机器人

> QQ 机器人使用 **QQ 开放平台 WebSocket**（官方 `botgo` SDK）：支持 C2C 私聊与群 @，**无需公网回调**（WebSocket 出站连接）。

**配置步骤：**

1. 在 [QQ 机器人开放平台](https://q.qq.com) 创建机器人，获取 **App ID** 与 **Client Secret**。  
2. 在沙箱中添加测试成员（上线前仅沙箱可对话）。  
3. 订阅 **C2C 消息**、**群 @ 消息** 等事件（WebSocket 模式）。  
4. CyberStrikeAI → **机器人设置** → **QQ 机器人** → 填入 App ID、Client Secret。  
5. 测试阶段勾选 **沙箱环境**；正式上线后取消沙箱并发布。  
6. 点击 **应用配置**。

**使用：** 与机器人 C2C 私聊直接发；QQ 群中需 @ 机器人。

> 注意：QQ 官方正逐步推广 Webhook 回调；当前实现使用 WebSocket（与钉钉/飞书类似的长连接模式）。若配置变更后连接未刷新，可重启 CyberStrikeAI 进程。

---

## 四、机器人命令

在任一已接入平台（钉钉/飞书/微信/Telegram/Slack/Discord/QQ 等）向机器人发送以下**文本命令**（仅支持文本）：

| 命令 | 说明 |
|------|------|
| **帮助** | 显示命令帮助与说明 |
| **列表** 或 **对话列表** | 列出所有对话的标题与对话 ID |
| **切换 \<对话ID\>** 或 **继续 \<对话ID\>** | 指定对话 ID，后续消息在该对话中继续 |
| **新对话** | 开启一个新对话，后续消息在新对话中 |
| **清空** | 清空当前对话上下文（效果等同「新对话」） |
| **当前** | 显示当前对话 ID 与标题 |
| **停止** | 中断当前正在执行的任务 |
| **角色** 或 **角色列表** | 列出所有可用角色（渗透测试、CTF、Web 应用扫描等） |
| **角色 \<角色名\>** 或 **切换角色 \<角色名\>** | 切换当前使用的角色 |
| **删除 \<对话ID\>** | 删除指定对话 |
| **版本** | 显示当前 CyberStrikeAI 版本号 |

除以上命令外，**直接输入任意文字**会作为用户消息发给 AI，与 Web 端对话逻辑一致（渗透测试/安全分析等）。

---

## 五、如何使用（要 @ 机器人吗？）

- **个人微信**：在与 CyberStrikeAI 机器人的**私聊**中直接发送即可，**不需要 @**（不支持群聊）。  
- **钉钉 / 飞书单聊（推荐）**：**搜索并打开该机器人**，进入**私聊**，直接输入「帮助」或任意文字即可，**不需要 @**。  
- **钉钉 / 飞书群聊**：若机器人被添加到群里，在群内只有 **@机器人** 后发送的消息才会被机器人收到并回复；不 @ 的群消息不会触发机器人。

总结：**个人微信、单聊时直接发**；**钉钉/飞书在群里用时需要 @机器人** 再发内容。

---

## 六、推荐使用流程（避免漏步骤）

**个人微信（最简单，无需开放平台）**

1. CyberStrikeAI Web 端 → 系统设置 → 机器人设置 → **微信 / iLink** → **生成二维码并绑定**。  
2. 手机微信扫码确认（如需配对数字则在 Web 页填写）。  
3. 绑定成功后，在手机微信私聊中发「帮助」测试。

**钉钉 / 飞书**

1. **在开放平台**：按第三节完成应用创建、凭证复制、机器人开通（钉钉务必选 **Stream 模式**）、权限与发布。  
2. **在 CyberStrikeAI**：系统设置 → 机器人设置 → 勾选对应平台，粘贴 Client ID/App ID、Client Secret/App Secret → 点击 **应用配置**。  
3. **重启 CyberStrikeAI 进程**（否则长连接不会建立）。  
4. **在手机钉钉/飞书**：找到该机器人（单聊直接发，群聊需 @机器人），发「帮助」或任意内容测试。

若发消息没反应，先看 **第九节排查** 和 **第十节常见弯路**。

---

## 七、配置文件示例

`config.yaml` 中机器人相关片段示例：

```yaml
robots:
  wechat: # 个人微信 iLink（扫码绑定后自动写入，一般无需手填）
    enabled: true
    bot_token: "your_bot_token@im.bot:..."
    ilink_bot_id: "your_bot_id@im.bot"
    ilink_user_id: "your_user_id@im.wechat"
    base_url: "https://ilinkai.weixin.qq.com"
    bot_type: "3"
    bot_agent: "CyberStrikeAI/1.0"
  dingtalk:
    enabled: true
    client_id: "your_dingtalk_app_key"
    client_secret: "your_dingtalk_app_secret"
  lark:
    enabled: true
    app_id: "your_lark_app_id"
    app_secret: "your_lark_app_secret"
    verify_token: ""
  wecom:
    enabled: false
    corp_id: ""
    agent_id: 0
    token: ""
    encoding_aes_key: ""
    secret: ""
  telegram:
    enabled: false
    bot_token: ""
    allow_group_messages: false
  slack:
    enabled: false
    bot_token: ""
    app_token: ""
  discord:
    enabled: false
    bot_token: ""
    allow_guild_messages: false
  qq:
    enabled: false
    app_id: ""
    client_secret: ""
    sandbox: true
```

修改钉钉/飞书/企业微信/Telegram/Slack/Discord/QQ 配置后，点击 **应用配置** 会自动重启对应长连接。个人微信扫码绑定成功后会自动写入并重启 iLink 连接。

---

## 八、如何验证是否可用（无需钉钉/飞书客户端）

在未安装钉钉或飞书时，可用**测试接口**验证机器人逻辑是否正常：

1. 先登录 CyberStrikeAI Web 端（保证有登录态）。  
2. 使用 curl 调用测试接口（需携带登录后的 Cookie）：

```bash
# 将 YOUR_COOKIE 替换为登录后获得的 Cookie（浏览器 F12 → 网络 → 任意请求 → 请求头中的 Cookie）
curl -X POST "http://localhost:8080/api/robot/test" \
  -H "Content-Type: application/json" \
  -H "Cookie: YOUR_COOKIE" \
  -d '{"platform":"dingtalk","user_id":"test_user","text":"帮助"}'
```

若返回 JSON 中含有 `"reply":"【CyberStrikeAI 机器人命令】..."`，说明命令处理正常。可再试 `"text":"列表"`、`"text":"当前"` 等。

接口说明：`POST /api/robot/test`（需登录），请求体 `{"platform":"可选","user_id":"可选","text":"必填"}`，响应 `{"reply":"回复内容"}`。

---

## 九、发消息没反应时排查

### 9.1 个人微信

按顺序检查：

1. **是否已完成扫码绑定**  
   机器人设置页应显示「已连接」或已绑定 Bot ID；`config.yaml` 中 `robots.wechat.bot_token` 不应为空。

2. **是否已启用**  
   确认「启用微信机器人」已勾选；若刚修改过，可重启 CyberStrikeAI 进程。

3. **看程序日志**  
   - 启动后应看到：`微信 iLink 长轮询已启动`；  
   - 发消息后应有：`微信收到消息`；若没有，多为未绑定成功或 `bot_token` 失效，可尝试 **重新绑定**。  
   - 若出现 `微信 iLink 长轮询异常，将自动重连`，等待自动重连或重启进程。

4. **网络**  
   服务器需能访问 `https://ilinkai.weixin.qq.com`（出站 HTTPS）。绑定阶段若无法生成二维码，优先检查此项。

5. **断网或睡眠后**  
   与钉钉/飞书类似，程序会**自动重连**（约 5～60 秒）；仍无回复可重启 CyberStrikeAI。

### 9.2 钉钉

按顺序检查：

0. **笔记本合盖睡眠 / 断网后**  
   钉钉、飞书均使用长连接收消息，睡眠或断网后连接会断开。程序会**自动重连**（约 5 秒～60 秒内重试）。唤醒或恢复网络后稍等一会儿再发消息；若仍无反应，可重启 CyberStrikeAI 进程。

1. **Client ID / Client Secret 是否与开放平台完全一致**  
   从「凭证与基础信息」里**复制粘贴**，不要手打。注意数字 **0** 与字母 **o**、数字 **1** 与字母 **l**（例如 `ding9gf9tiozuc504aer` 中间是 **504** 不是 5o4）。

2. **是否在保存配置后重启了应用**  
   机器人长连接在**应用启动时**建立。在 Web 端点击「应用配置」只写入配置文件，**必须重启 CyberStrikeAI 进程**后钉钉连接才会生效。

3. **看程序日志**  
   - 启动后应看到：`钉钉 Stream 正在连接…`、`钉钉 Stream 已启动（无需公网），等待收消息`。  
   - 若出现 `钉钉 Stream 长连接退出` 且带错误信息，多为 **Client ID / Client Secret 错误**或**开放平台未开通流式接入**。  
   - 在钉钉里发一条消息后，若有收到，应有日志：`钉钉收到消息`；若没有，说明钉钉未把消息推到本程序（回头检查开放平台「机器人」是否开通、是否选用 **Stream 模式**）。

4. **开放平台侧**  
   应用需已**发布**；在「机器人」能力中需开启**流式接入（Stream）** 用于接收消息（仅 HTTP 回调不够）；权限管理里需有机器人接收、发送消息等权限。

---

## 十、常见弯路（避免踩坑）

- **个人微信与企业微信混淆**：个人微信走 `robots.wechat` + Web 扫码绑定；企业微信走 `robots.wecom` + 管理后台回调 URL，二者完全不同。  
- **个人微信二维码过期**：二维码约 5 分钟有效，过期需重新生成，不要一直扫旧码。  
- **用错了机器人类型**：在钉钉**群里**添加的「自定义」机器人（Webhook + 加签）**不能**用来做对话，本程序只支持**开放平台「企业内部应用」**里的机器人。  
- **只保存没重启**：钉钉/飞书改完配置后必须**重启应用**，否则长连接不会建立（个人微信扫码绑定会自动重启连接）。  
- **Client ID 抄错**：开放平台是 `504` 就填 `504`，不要填成 `5o4`；尽量用复制粘贴。  
- **钉钉只开了 HTTP 回调没开 Stream**：本程序通过 **Stream 长连接**收消息，开放平台里机器人的消息接收方式必须选 **Stream 模式**。  
- **应用没发布**：开放平台里修改了机器人或权限后，要在「版本管理与发布」里**发布新版本**，否则不生效。

---

## 十一、注意事项

- 各平台均**仅处理文本消息**；其他类型（如图片、语音）会提示暂不支持或忽略。  
- 个人微信仅支持**私聊**，不支持群聊 @ 机器人。  
- 会话与 Web 端共用同一套对话数据：在机器人里创建的对话会在 Web 端「对话」列表中看到，反之亦然。  
- Robot execution uses the same **Eino single-agent/multi-agent** logic (`ProcessMessageForRobot`, including progress callbacks and process-detail persistence); it only skips client-side SSE and replies once at the end to DingTalk/Lark/WeCom. Default `robot_default_agent_mode: eino_single`.
