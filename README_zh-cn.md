# ShellBeam Monitoring Agent

[English](readme.md) · [对接约定](docs/integration.md) · [发布流程](docs/releases.md)

ShellBeam Monitoring Agent 向 [ShellBeam Server](https://github.com/0xJean/shellbeam-server)
上报主机监控数据。它是独立维护的 Komari Agent 分支，基线为 `1.5.11`，
固定提交 `c7bafb79b1a73ed9e14e2c248ae4d4163e506036`，采用 MIT 协议。
本分支使用 `1.5.12` 版本号，让现有探针管理器能够从 `1.5.11` 升级；
该版本号属于 ShellBeam 分支。完整来源见 [UPSTREAM.md](UPSTREAM.md)。

保留 Komari 探针协议、命令行参数、`AGENT_*` 环境变量和 JSON 字段，
对接 ShellBeam Server 的 `1.6.0` API 基线。它与本地 Rust `shellbeam-agent`
是不同组件。Server 插件继续保留在现有 ShellBeam 应用工作区中。

## 构建和身份信息

使用 Go 1.25 或更新版本、GNU make 和 Python 3：

```sh
make check build
./dist/shellbeam-monitoring-agent --version
./dist/shellbeam-monitoring-agent --build-info
```

`make artifacts` 同时生成品牌名称与 `komari-agent-<系统>-<架构>` 兼容名称的
相同二进制，并收集依赖许可证。`./build_all.sh` 支持 Linux、Windows、macOS、
FreeBSD。CI 原生测试 Linux amd64/arm64，并交叉编译其他目标；
交叉编译通过不等于已验证相应系统的运行行为。

## 连接 Server

使用权限为 `600` 的 JSON 文件保存配置，避免把 token 放入命令行或终端历史：

```json
{
  "endpoint": "https://monitor.example.com",
  "token": "替换为节点 token",
  "interval": 3,
  "disable_auto_update": true,
  "disable_web_ssh": true,
  "ignore_unsafe_cert": false
}
```

```sh
chmod 600 /absolute/path/to/agent.json
./dist/shellbeam-monitoring-agent --config /absolute/path/to/agent.json
```

`disable_web_ssh: true` 会关闭探针远程控制通道；请按部署策略配置。
App 托管部署应继续使用现有管理器生成的配置和服务，不要另起重复服务。
配置优先级沿用上游，从低到高为：默认值、命令行参数、环境变量、JSON 文件。
全部参数见 `--help`，映射定义见 [`cmd/flags/flag.go`](cmd/flags/flag.go)。

## 安装、升级与回滚

Linux 托管探针继续使用 ShellBeam 已有的签名分发与管理流程。
仓库里的 `install.sh` / `install.ps1` 只输出说明，不修改主机。
管理器继续使用 runtime `komari`、数字格式 `1.5.x` 版本、既有服务路径与兼容文件名。

新建仓库不会自动让新二进制进入 App 的可信发布源。生产推广仍需完成签名、
URL 白名单、哈希、版本固定与真实生命周期验收，见 [对接约定](docs/integration.md)。
独立运行时，自更新指向本分支仓库；App 托管和容器运行应保持禁用自更新，
通过各自经过验证的升级和回滚流程替换二进制或镜像。

## 容器

```sh
docker build -t shellbeam-monitoring-agent:local .
docker run --rm shellbeam-monitoring-agent:local --build-info
```

镜像默认 UID/GID 为 `10001`，包含 CA 证书，默认显示帮助；
不会自动获得主机挂载或权限。没有显式主机访问配置时，指标反映容器视图。
镜像设置 `AGENT_DISABLE_AUTO_UPDATE=true`，JSON 文件优先级更高，
因此文件中也应保留 `disable_auto_update: true`。容器通过更换镜像升级。
保留 `/.komari-agent-container` 兼容标记。

## 许可证

MIT；[LICENSE](LICENSE) 保留上游版权。分发包包含 [NOTICE](NOTICE)、
上游来源与依赖许可证文本。
