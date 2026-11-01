package flags_pkg

import (
	"net"
	"net/url"
	"strings"
)

type Config struct {
	AutoDiscoveryKey    string  `json:"auto_discovery_key" env:"AGENT_AUTO_DISCOVERY_KEY"`         // 自动发现密钥
	DisableAutoUpdate   bool    `json:"disable_auto_update" env:"AGENT_DISABLE_AUTO_UPDATE"`       // 禁用自动更新
	EnableAutoUpdate    bool    `json:"enable_auto_update" env:"AGENT_ENABLE_AUTO_UPDATE"`         // 启用自动更新（默认关闭，禁用优先）
	DisableWebSsh       bool    `json:"disable_web_ssh" env:"AGENT_DISABLE_WEB_SSH"`               // 禁用远程控制（web ssh 和 rce）
	EnableRemoteControl bool    `json:"enable_remote_control" env:"AGENT_ENABLE_REMOTE_CONTROL"`   // 启用远程控制（默认关闭，禁用优先）
	MemoryModeAvailable bool    `json:"memory_mode_available" env:"AGENT_MEMORY_MODE_AVAILABLE"`   // [deprecated] 已弃用，请使用 MemoryIncludeCache
	Token               string  `json:"token" env:"AGENT_TOKEN"`                                   // Token
	Endpoint            string  `json:"endpoint" env:"AGENT_ENDPOINT"`                             // 面板地址
	Interval            float64 `json:"interval" env:"AGENT_INTERVAL"`                             // 数据采集间隔，单位秒
	IgnoreUnsafeCert    bool    `json:"ignore_unsafe_cert" env:"AGENT_IGNORE_UNSAFE_CERT"`         // 忽略不安全的证书
	MaxRetries          int     `json:"max_retries" env:"AGENT_MAX_RETRIES"`                       // 最大重试次数
	ReconnectInterval   int     `json:"reconnect_interval" env:"AGENT_RECONNECT_INTERVAL"`         // 重连间隔，单位秒
	InfoReportInterval  int     `json:"info_report_interval" env:"AGENT_INFO_REPORT_INTERVAL"`     // 基础信息上报间隔，单位分钟
	IncludeNics         string  `json:"include_nics" env:"AGENT_INCLUDE_NICS"`                     // 仅统计网卡，逗号分隔的网卡名称列表，支持通配符
	ExcludeNics         string  `json:"exclude_nics" env:"AGENT_EXCLUDE_NICS"`                     // 统计时排除的网卡，逗号分隔的网卡名称列表，支持通配符
	IncludeMountpoints  string  `json:"include_mountpoints" env:"AGENT_INCLUDE_MOUNTPOINTS"`       // 磁盘统计的包含挂载点列表，使用分号分隔
	MonthRotate         int     `json:"month_rotate" env:"AGENT_MONTH_ROTATE"`                     // 流量统计的月份重置日期（0表示禁用）
	MemoryIncludeCache  bool    `json:"memory_include_cache" env:"AGENT_MEMORY_INCLUDE_CACHE"`     // 包括缓存/缓冲区的内存使用情况
	MemoryReportRawUsed bool    `json:"memory_report_raw_used" env:"AGENT_MEMORY_REPORT_RAW_USED"` // 使用原始内存使用情况报告
	CustomDNS           string  `json:"custom_dns" env:"AGENT_CUSTOM_DNS"`                         // 使用的自定义DNS服务器
	EnableGPU           bool    `json:"enable_gpu" env:"AGENT_ENABLE_GPU"`                         // 启用详细GPU监控
	ShowWarning         bool    `json:"show_warning" env:"AGENT_SHOW_WARNING"`                     // Windows 上显示安全警告，作为子进程运行一次
	CustomIpv4          string  `json:"custom_ipv4" env:"AGENT_CUSTOM_IPV4"`                       // 自定义 IPv4 地址
	CustomIpv6          string  `json:"custom_ipv6" env:"AGENT_CUSTOM_IPV6"`                       // 自定义 IPv6 地址
	GetIpAddrFromNic    bool    `json:"get_ip_addr_from_nic" env:"AGENT_GET_IP_ADDR_FROM_NIC"`     // 从网卡获取IP地址
	HostProc            string  `json:"host_proc" env:"HOST_PROC"`                                 // 容器环境下宿主机/proc目录的挂载点，用于监控宿主机进程
	ConfigFile          string  `json:"config_file" env:"AGENT_CONFIG_FILE"`                       // JSON配置文件路径
	DisableCompression  bool    `json:"disable_compression" env:"AGENT_DISABLE_COMPRESSION"`       // 禁用v2传输压缩
	PreferIPVersion     string  `json:"prefer_ip_version" env:"AGENT_PREFER_IP_VERSION"`           // 面板连接优先使用的 IP 版本：4 或 6

}

var GlobalConfig = &Config{}

// AutoUpdateEnabled reports the operator's choice; --disable-auto-update wins.
func (c *Config) AutoUpdateEnabled() bool {
	return c.EnableAutoUpdate && !c.DisableAutoUpdate
}

// RemoteControlEnabled reports whether commands, terminals and file operations
// from the server may run.
func (c *Config) RemoteControlEnabled() bool {
	return c.RemoteControlRefusal() == ""
}

// RemoteControlRefusal explains why remote control is off, or returns "" when
// it is allowed. It is off unless requested, and refused on a connection that
// cannot authenticate the server, since that server could then run commands.
func (c *Config) RemoteControlRefusal() string {
	if c.DisableWebSsh {
		return "disabled by disable_web_ssh"
	}
	if !c.EnableRemoteControl {
		return "not enabled; set --enable-remote-control to opt in"
	}
	endpoint, err := url.Parse(strings.TrimSpace(c.Endpoint))
	if err != nil || endpoint.Hostname() == "" {
		return "the endpoint is not a valid URL"
	}
	if c.isLoopback(endpoint.Hostname()) {
		return ""
	}
	if endpoint.Scheme != "https" && endpoint.Scheme != "wss" {
		return "the endpoint does not use TLS (https or wss)"
	}
	if c.IgnoreUnsafeCert {
		return "ignore_unsafe_cert turns off server certificate verification"
	}
	return ""
}

func (c *Config) isLoopback(host string) bool {
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsLoopback()
	}
	// A custom DNS server could answer anything for this name.
	return strings.EqualFold(host, "localhost") && c.CustomDNS == ""
}
