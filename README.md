# CPA OpenAI Basis Points 插件

这是一个 CLIProxyAPI（CPA）原生插件，用 CPA 已有的 ChatGPT/Codex OAuth 凭据直接请求。

## 通过 CPA 插件商店安装（推荐）

在配置面板→高级与实验→第三方插件源 → 插件源 registry URL (plugins.store-sources)」中添加以下地址并保存，然后刷新插件商店，搜索 **CPA OpenAI Basis Points**：

```text
https://raw.githubusercontent.com/sizhe233/cpa-plugin-oai-basispoints/sizhe/upstream-v0.2.9/registry.json
```

也可合并到 CPA **宿主配置**（`config.yaml`，与下方插件配置共用同一个 `plugins` 节点）：

```yaml
plugins:
  enabled: true
  store-sources:
    - https://raw.githubusercontent.com/sizhe233/cpa-plugin-oai-basispoints/sizhe/upstream-v0.2.9/registry.json
```

保留已有插件源，不要整体覆盖原有 `plugins` 配置；内置官方源由 CPA 自动保留。本源使用宿主原生的 `github-release` 安装方式，最新版本以本仓库已发布的 GitHub Release 为准，不在 registry 中另行维护版本号。CPA 会按运行平台下载 `oai-basispoints_<version>_<goos>_<goarch>.zip`，并使用同一 Release 的 `checksums.txt` 校验。

发行包覆盖 Linux、macOS、Windows 的 AMD64/ARM64。插件商店负责下载、校验和安装；更新已加载的动态库后仍需重启 CPA，使新代码及 OAuth 认证解析生效。

## 安装和配置

1. 将 `build/linux/amd64/oai-basispoints.so` 复制到 CPA 的 Linux amd64 插件目录。
2. 将 `config.example.yaml` 按需合并到 CPA 的 `config.yaml`；它是完整的宿主配置示例，不会由插件自动读取。插件内置默认暴露 `gpt-6-astra-basispoints`，示例同时配置 Astra 和 Sol，可继续增删模型。
3. 在 `plugins.configs.oai-basispoints.include_auth_ids` 中列出精确物理 CPA Auth ID 或文件名。未列入的 `type: codex` OAuth 文件不会被 Basis Points 读取；空白名单拒绝所有来源，不会退回到全量账号。
4. 客户端使用 Responses 协议调用 `gpt-6-astra-basispoints`。模型目录声明图像输入，以及 `low`、`medium`、`high`、`xhigh`、`max`、`ultra` 思考等级；`max` 映射为 `xhigh`，`ultra` 原样传递，未指定时默认 `medium`。

默认使用 host 模式：Codex OAuth 文件由 CPA 原生加载、刷新和持久化；插件不接管认证解析，只在执行 Basis Points 别名模型时读取白名单内的当前凭据。现有 Codex 模型继续使用 CPA 原生执行器，插件不生成或改写源凭据文件，源认证仍可在 CPA 管理页修改 `excluded_models` 等字段。

从早期 Fork 升级时，先备份并移除旧的无令牌 `oai-basispoints-ref-*.json` 引用文件；新 host 模式不使用这些引用文件。保留原始 `type: codex` 源文件和 `include_auth_ids` 配置即可。

## 构建

```bash
make test
make build
```

---

## 版权与社区支持

本项目基于 [MIT License](LICENSE) 开源

感谢 [LINUX DO 社区](https://linux.do/) 的支持

<a href="https://linux.do/">
  <img src="docs/assets/linuxdo.png" alt="LINUX DO 社区" width="360" />
</a>
