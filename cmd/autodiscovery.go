package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/komari-monitor/komari-agent/dnsresolver"
	"github.com/komari-monitor/komari-agent/utils"
)

// AutoDiscoveryConfig 自动发现配置结构体
type AutoDiscoveryConfig struct {
	UUID  string `json:"uuid"`
	Token string `json:"token"`
}

// RegisterRequest 注册请求结构体
type RegisterRequest struct {
	Key string `json:"key"`
}

// RegisterResponse 注册响应结构体
type RegisterResponse struct {
	Status  string `json:"status"`
	Message string `json:"message"`
	Data    struct {
		UUID  string `json:"uuid"`
		Token string `json:"token"`
	} `json:"data"`
}

// getAutoDiscoveryFilePath 获取自动发现配置文件路径
func getAutoDiscoveryFilePath() string {
	// 获取程序运行目录
	execPath, err := os.Executable()
	if err != nil {
		log.Println("Failed to get executable path:", err)
		return "auto-discovery.json"
	}
	execDir := filepath.Dir(execPath)
	return filepath.Join(execDir, "auto-discovery.json")
}

// loadAutoDiscoveryConfig 加载自动发现配置
func loadAutoDiscoveryConfig() (*AutoDiscoveryConfig, error) {
	return loadAutoDiscoveryConfigFrom(getAutoDiscoveryFilePath())
}

func loadAutoDiscoveryConfigFrom(configPath string) (*AutoDiscoveryConfig, error) {
	// 检查文件是否存在
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		return nil, nil // 文件不存在，返回nil
	}
	tightenAutoDiscoveryFile(configPath)

	// 读取文件内容
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read auto-discovery config: %v", err)
	}

	// 解析JSON
	var config AutoDiscoveryConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse auto-discovery config: %v", err)
	}

	return &config, nil
}

// saveAutoDiscoveryConfig 保存自动发现配置
func saveAutoDiscoveryConfig(config *AutoDiscoveryConfig) error {
	return saveAutoDiscoveryConfigTo(getAutoDiscoveryFilePath(), config)
}

func saveAutoDiscoveryConfigTo(configPath string, config *AutoDiscoveryConfig) error {
	// 序列化为JSON
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal auto-discovery config: %v", err)
	}

	// 写入文件
	if err := writeFileAtomicPrivate(configPath, data); err != nil {
		return fmt.Errorf("failed to write auto-discovery config: %v", err)
	}

	log.Printf("Auto-discovery config saved to: %s", configPath)
	return nil
}

// writeFileAtomicPrivate never exposes the token: os.CreateTemp creates the file
// as 0600 and the rename replaces any previous file (and any symlink) at once.
// Windows ignores those bits, so there the file gets a private ACL before the
// token is written, and the write fails if it cannot.
func writeFileAtomicPrivate(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".auto-discovery-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := restrictFileACL(tmp.Name()); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(0600); err != nil && runtime.GOOS != "windows" {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// tightenAutoDiscoveryFile repairs files written 0644 by earlier versions. A
// failure is only logged so an existing deployment keeps reporting.
func tightenAutoDiscoveryFile(path string) {
	info, err := os.Lstat(path)
	if err != nil {
		log.Printf("Failed to check auto-discovery config permissions: %v", err)
		return
	}
	if !info.Mode().IsRegular() {
		log.Printf("[warn] auto-discovery config %s is not a regular file; permissions left unchanged", path)
		return
	}
	if runtime.GOOS == "windows" {
		if err := restrictFileACL(path); err != nil {
			log.Printf("[warn] could not restrict auto-discovery config ACL: %v", err)
		}
		return
	}
	if info.Mode().Perm()&0077 == 0 {
		return
	}
	if err := os.Chmod(path, 0600); err != nil {
		log.Printf("[warn] could not restrict auto-discovery config permissions: %v", err)
		return
	}
	log.Printf("Restricted auto-discovery config permissions to 0600: %s", path)
}

// registerWithAutoDiscovery 使用自动发现key注册
func registerWithAutoDiscovery() error {
	// 构造注册请求
	requestData := RegisterRequest{
		Key: flags.AutoDiscoveryKey,
	}

	hostname, _ := os.Hostname()

	jsonData, err := json.Marshal(requestData)
	if err != nil {
		return fmt.Errorf("failed to marshal register request: %v", err)
	}

	// 构造请求URL
	endpoint := flags.Endpoint
	if len(endpoint) > 0 && endpoint[len(endpoint)-1] == '/' {
		endpoint = endpoint[:len(endpoint)-1]
	}

	// 转换中文域名为 ASCII 兼容编码
	endpoint, err = utils.ConvertIDNToASCII(endpoint)
	if err != nil {
		log.Printf("Warning: Failed to convert IDN to ASCII: %v", err)
		// 继续使用原始 endpoint，可能在某些情况下仍能工作
	}

	registerURL := fmt.Sprintf("%s/api/clients/register?name=%s", endpoint, url.QueryEscape(hostname))

	// 创建HTTP请求
	req, err := http.NewRequest("POST", registerURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("failed to create register request: %v", err)
	}

	// 设置请求头
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", flags.AutoDiscoveryKey))

	// 发送请求
	client := dnsresolver.GetHTTPClientWithPreference(30*time.Second, flags.PreferIPVersion)
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send register request: %v", err)
	}
	defer resp.Body.Close()

	// 检查响应状态
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("register request failed with status %d: %s", resp.StatusCode, string(body))
	}

	// 解析响应
	var registerResp RegisterResponse
	if err := json.NewDecoder(resp.Body).Decode(&registerResp); err != nil {
		return fmt.Errorf("failed to parse register response: %v", err)
	}

	// 检查响应状态
	if registerResp.Status != "success" {
		return fmt.Errorf("register request failed: %s", registerResp.Message)
	}

	// 保存配置
	config := &AutoDiscoveryConfig{
		UUID:  registerResp.Data.UUID,
		Token: registerResp.Data.Token,
	}

	if err := saveAutoDiscoveryConfig(config); err != nil {
		return fmt.Errorf("failed to save auto-discovery config: %v", err)
	}

	// 设置token
	flags.Token = registerResp.Data.Token
	utils.RegisterSecret(flags.Token)
	log.Printf("Successfully registered with auto-discovery. UUID: %s", registerResp.Data.UUID)

	return nil
}

// handleAutoDiscovery 处理自动发现逻辑
func handleAutoDiscovery() error {
	// 尝试加载现有配置
	config, err := loadAutoDiscoveryConfig()
	if err != nil {
		log.Printf("Failed to load auto-discovery config: %v", err)
		// 继续尝试注册
	}

	if config != nil {
		// 配置文件存在，使用现有token
		flags.Token = config.Token
		utils.RegisterSecret(flags.Token)
		log.Printf("Using existing auto-discovery token for UUID: %s", config.UUID)
		return nil
	}

	// 配置文件不存在，进行注册
	log.Println("Auto-discovery config not found, registering with server...")
	return registerWithAutoDiscovery()
}
