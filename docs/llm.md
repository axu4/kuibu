# 大模型配置与外发规则

M4「逐级汇总」的归一、对照、组报、产品报、月报都要调大模型。本页说明模型怎么配、密钥存哪、以及把他人内容发给外部模型的规则。代码在 `internal/llm`，CLI 是 `kuibu llm`，App 在「设置 → AI 模型」。

## 支持的提供方

| 提供方 | `provider` | 请求地址 | 鉴权 |
| --- | --- | --- | --- |
| OpenAI 兼容（DeepSeek、通义、公司内网网关等） | `openai` | `<baseUrl>/chat/completions` | `Authorization: Bearer <key>` |
| Claude（Anthropic Messages API） | `claude` | `<baseUrl>/v1/messages` | `x-api-key: <key>` |

内置两个模板，其余手填：

| 模板 | 提供方 | 接口地址 | 模型 |
| --- | --- | --- | --- |
| `deepseek` | openai | `https://api.deepseek.com` | `deepseek-chat` |
| `claude` | claude | `https://api.anthropic.com` | `claude-sonnet-5-5` |

只用标准库 `net/http` 实现，不引入 SDK。默认单次请求超时 120 秒；只对网络错误、429、5xx 重试（最多 2 次，指数退避，尊重 `Retry-After`，最多等 30 秒）；401 / 403 / 404 / 400 / 402 等错误翻成中文提示。暂不支持流式输出。

## 配置与密钥存储

- 配置文件：`<配置目录>/llm.json`，与 `config.json` 同目录（默认 `os.UserConfigDir()/kuibu`，可用 `KUIBU_CONFIG_DIR` 覆盖），**权限 0600**，原子写入。
- 内容：`{"default": "名称", "models": [{"name", "provider", "baseUrl", "model", "apiKey"}]}`。
- 密钥**不进工作台、不进 git、不进日志**。CLI `--json`、App 绑定、错误信息都只给掩码（如 `sk-…abcd`）；提供方把密钥回显到错误信息里时也会替换成掩码。
- 第一个添加的模型自动成为默认；删除默认模型后默认改为剩下的第一个。
- 以后可能换成 macOS 钥匙串，目前与 GitHub token 的存法一致。

## 用量日志

每次调用（含连通测试）在 `<配置目录>/llm-usage.log` 追加一行 JSON：时间、模型名、模型 ID、输入 / 输出 token、是否成功。**不含任何请求或返回内容。**

## 外发规则（硬性）

凡是把组员 / 他人内容（周报原文、群聊、会议笔记等）发给外部大模型：

1. **只能手动触发**，不允许任何定时 / 自动触发。
2. **必须先预览将要发送的完整内容并确认。** 预览包含目标模型（名称、模型 ID、接口地址）和完整文本（系统提示 + 各条消息），与实际请求内容一致。
3. 这不是可关闭的选项。

实现上所有外发都走 `llm.Outbound`：`Prepare` 生成预览，`Send(..., confirmed)` 只有 `confirmed=true` 才发请求，否则返回 `llm.ErrNeedsConfirm`（CLI 退出码 `8 NEEDS_CONFIRM`）。

- **CLI**：不带 `--yes` 时输出预览并以 8 退出，不发请求；`--dry-run` 只输出预览，退出码 0。
- **App**：用通用组件 `components/SendPreview.tsx` 弹窗展示目标模型和完整文本（可滚动、可复制），点「确认发送」才发。`LLMService` 不提供直接发送他人内容的方法，由具体功能（#25 起）在确认后调用。

连通测试只发固定测试语（`llm.TestPrompt`），不含个人数据，所以不需要确认。

## CLI

```bash
# 添加（密钥从标准输入读取，避免出现在 shell 历史里）
pbpaste | kuibu llm add DeepSeek --preset deepseek
pbpaste | kuibu llm add 内网 --provider openai --base-url https://llm.example.com/v1 --model qwen-max --default

# 更新地址或模型但保留原密钥
kuibu llm add 内网 --provider openai --base-url https://llm2.example.com/v1 --model qwen-max --keep-key

kuibu llm list [--json]
kuibu llm default <名称>
kuibu llm rm <名称>
kuibu llm test [<名称>]          # 不填名称用默认模型

# 通用外发入口：正文从标准输入读取
kuibu llm send --system "整理成两段" < 周报.txt            # 只预览，退出码 8
kuibu llm send --system "整理成两段" --dry-run < 周报.txt  # 只预览，退出码 0
kuibu llm send --system "整理成两段" --yes < 周报.txt      # 人工确认后真正发送
```

错误码：未配置模型 `NO_MODEL`，名称不存在 `NOT_FOUND`，参数不合法 `INVALID`，未确认 `NEEDS_CONFIRM`（退出码 8），其余退出码 1。

## App 接口（`LLMService`）

| 方法 | 说明 |
| --- | --- |
| `Presets()` | 内置模板 |
| `List()` | 模型列表（掩码）与默认模型 |
| `Save(input)` | 新增 / 更新；编辑时 `apiKey` 留空表示保留原密钥 |
| `Remove(name)` / `SetDefault(name)` | 删除 / 设默认 |
| `Test(name)` | 连通测试，返回模型回复 |
