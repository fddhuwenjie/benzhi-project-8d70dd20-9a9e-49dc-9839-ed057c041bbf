package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
)

func parseConfig(args []string, portEnv string) (config, error) {
	flags := flag.NewFlagSet("fieldarchive", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	cfg := config{}
	flags.StringVar(&cfg.addr, "addr", defaultAddr, "HTTP 监听地址")
	flags.StringVar(&cfg.dataDir, "data-dir", "data", "持久化数据目录")
	flags.BoolVar(&cfg.selfCheck, "self-check", false, "执行真实 HTTP 自检后退出")
	if err := flags.Parse(args); err != nil {
		return cfg, err
	}
	if flags.NArg() != 0 {
		return cfg, fmt.Errorf("未知位置参数: %s", strings.Join(flags.Args(), " "))
	}
	explicitAddr := false
	flags.Visit(func(f *flag.Flag) {
		if f.Name == "addr" {
			explicitAddr = true
		}
	})
	if !explicitAddr && strings.TrimSpace(portEnv) != "" {
		port, err := strconv.Atoi(portEnv)
		if err != nil || port < 1 || port > 65535 {
			return cfg, errors.New("PORT 必须是 1 到 65535 的端口号")
		}
		cfg.addr = net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	}
	host, port, err := net.SplitHostPort(cfg.addr)
	if err != nil {
		return cfg, fmt.Errorf("-addr 必须是 host:port: %w", err)
	}
	if host == "localhost" {
		host = "127.0.0.1"
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return cfg, errors.New("监听地址必须使用回环 IP，拒绝公开绑定")
	}
	portNum, err := strconv.Atoi(port)
	if err != nil || portNum < 1 || portNum > 65535 {
		return cfg, errors.New("监听端口无效")
	}
	cfg.addr = net.JoinHostPort(host, port)
	if strings.TrimSpace(cfg.dataDir) == "" {
		return cfg, errors.New("-data-dir 不能为空")
	}
	return cfg, nil
}
