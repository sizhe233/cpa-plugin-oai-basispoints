# CPA OpenAI Basis Points 插件

这是一个 CLIProxyAPI（CPA）原生插件，用 CPA 已有的 ChatGPT/Codex OAuth 凭据直接请求。

## 通过 CPA 插件商店安装（推荐）

在管理界面的「第三方插件源 → 插件源 registry URL (plugins.store-sources)」中添加以下地址并保存，然后刷新插件商店，搜索 **CPA OpenAI Basis Points**：

```text
https://raw.githubusercontent.com/sizhe233/cpa-plugin-oai-basispoints/main/registry.json
```

也可合并到 CPA **宿主配置**（`config.yaml`，与下方插件配置共用同一个 `plugins` 节点）：

```yaml
plugins:
  enabled: true
  store-sources:
    - https://raw.githubusercontent.com/sizhe233/cpa-plugin-oai-basispoints/main/registry.json
```

保留已有插件源，不要整体覆盖原有 `plugins` 配置；内置官方源由 CPA 自动保留。本源使用宿主原生的 `github-release` 安装方式，最新版本以本仓库已发布的 GitHub Release 为准，不在 registry 中另行维护版本号。CPA 会按运行平台下载 `oai-basispoints_<version>_<goos>_<goarch>.zip`，并使用同一 Release 的 `checksums.txt` 校验。

发行包覆盖 Linux、macOS、Windows 的 AMD64/ARM64。插件商店负责下载、校验和安装；更新已加载的动态库后仍需重启 CPA，使新代码及 OAuth 认证解析生效。

## 安装和配置

1. 将 Linux amd64 动态库复制到 CPA 的插件目录，并使用 `oai-basispoints-v<version>.so` 作为文件名，例如 `oai-basispoints-v0.1.12-sizhe.3.so`。CPA 由文件名解析插件 ID；不要在 `-v` 前加入 Fork 名称，否则它不会匹配 `plugins.configs.oai-basispoints`。
2. 将 `config.example.yaml` 按需合并到 CPA 的 `config.yaml`；它是完整的宿主配置示例，不会由插件自动读取。插件内置默认暴露 `gpt-6-astra-basispoints`，示例同时配置 Astra 和 Sol，可继续增删模型。
3. 在 `plugins.configs.oai-basispoints.include_auth_ids` 中列出允许的源 CPA AuthID。AuthID 就是 CPA 中显示的 OAuth 文件名，例如 `codex-paid-pro.json`。未配置或为空时，Basis Points 请求会直接返回“无可用源凭证”；不会把 Free 等未列入账号纳入路由。
4. 插件为每个白名单源文件创建一个确定命名的 `oai-basispoints-ref-*.json` 引用认证。它只保存 `type`、`auth_kind`、`source_auth_id` 和说明，不保存、复制或同步 OAuth token。每个引用文件只解析为一个插件认证，因此不是 CPA virtual auth。
5. `type: codex` 源 OAuth 文件始终由 CPA 官方 Codex 解析器管理。可继续在 Management Center 修改 `excluded_models`、权重、禁用状态等字段；这些修改写回原始 OAuth 文件，重启后仍由 CPA 官方解析器加载。
6. 客户端使用 Responses 协议调用 `gpt-6-astra-basispoints`。模型目录声明图像输入，以及 `low`、`medium`、`high`、`xhigh`、`max`、`ultra` 思考等级；`max` 映射为 `xhigh`，`ultra` 原样传递，未指定时默认 `medium`。

插件只向这些引用认证注册 `models` 中显式配置的 Basis Points 别名，例如 `gpt-6-astra-basispoints`；所有原生 Codex 模型仍由 CPA 的常规调度和执行器处理。CPA 先在 Basis Points 引用认证之间调度，再由插件通过官方 `host.auth.list` 和 `host.auth.get` 回调读取该引用关联的当前源 OAuth 文件。每次请求读取物理源文件的当前 OAuth 数据，因此在管理页更新凭证后无需同步另一份插件认证。

Basis Points 用量会记在对应的引用 AuthID 上，而不是原始 Codex AuthID；引用与源文件是一对一关系，标签和 `note` 中保留源 AuthID，便于 KEEPER 按来源查看。Basis Points 429 只会冷却该引用认证，不会让 CPA 再回退到同一源文件的原生 Codex 调度。此版本不修改 CPA 源码或 CPA 镜像。流式响应遵循 Responses SSE 格式，但为保证工具调用可在完整 item 上做安全转换，当前会先读完上游 SSE 再回放给客户端，不是 token 级实时转发。

## 构建

```bash
make test
make build
```

## 协议边界

- 上游请求始终带 `Authorization: Bearer <access_token>`、`chatgpt-account-id`、`x-openai-account-id` 和 `x-basispoints-auth-mode: chatgpt`。
- `turn_id` 按会话和当前用户 turn 稳定生成；工具结果回合只递增 `agent_iteration`，不会把同一 turn 重新当成新计划。
- 工具 `code` 是嵌套 JSON 字符串，不是 JavaScript。插件只解析它，不执行其中内容。
- 未能从 OAuth JWT 或凭据字段得到账号 ID、token 过期、上游返回非 2xx、工具名不在客户端目录中时，插件会报告明确错误，不伪造成功。

---

## 版权与社区支持

本项目基于 [MIT License](LICENSE) 开源

感谢 [LINUX DO 社区](https://linux.do/) 的支持

<a href="https://linux.do/">
  <img src="docs/assets/linuxdo.png" alt="LINUX DO 社区" width="360" />
</a>
