package router

import (
	"fmt"
	"net"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// Action定义流量走向
type Action string

const (
	ActionProxy  Action = "PROXY"  // 走代理
	ActionDirect Action = "DIRECT" // 直连
	ActionReject Action = "REJECT" // 拒绝
)

// Mode定义全局路由的模式
type Mode string

const (
	ModeRule   Mode = "rule"   // 规则模式
	ModeDirect Mode = "direct" // 全局直连
	ModeGlobal Mode = "global" // 全局代理
)

// 定义规则类型
type RuleType string

const (
	DomainSuffix  RuleType = "DOMAIN-SUFFIX"  // 域名后缀匹配
	DomainKeyword RuleType = "DOMAIN-KEYWORD" // 域名关键字匹配
	Domain        RuleType = "DOMAIN"         // 完整域名匹配
	IPCIDR        RuleType = "IP-CIDR"        // IP段匹配
	IPCIDR6       RuleType = "IP-CIDR6"       // IPv6
	Match         RuleType = "MATCH"          // 所有规则都没命中时的匹配
)

// Rule 代表一条路由规则
type Rule struct {
	Type    RuleType
	Payload string // ip或者域名（具体的待匹配值）
	Target  Action // 动作
}

// Router 路由的核心结构体
type Router struct {
	mode  Mode
	rules []Rule
}

// routerConfig 用于解析路由yaml文件
type routerConfig struct {
	Mode  Mode     `yaml:"mode"`
	Rules []string `yaml:"rules"`
}

// 从指定YAML文件路径中创建并初始化一个新的Router
func NewRouter(path string) (*Router, error) {
	if path == "" {
		return nil, fmt.Errorf("规则不能路径为空")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("读取规则文件失败: %w", err)
	}

	var cfg routerConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("解析YAML规则文件失败:%w", err)
	}

	// 默认模式为 rule
	if cfg.Mode == "" {
		cfg.Mode = ModeRule
	}

	router := &Router{
		mode:  cfg.Mode,
		rules: make([]Rule, 0, len(cfg.Rules)),
	}

	for i, line := range cfg.Rules {
		parts := strings.Split(line, ",")
		if len(parts) < 2 {
			continue
		}

		// 处理RuleType未匹配的情况
		ruleType := RuleType(strings.ToUpper(parts[0]))
		switch ruleType {
		case DomainSuffix, DomainKeyword, Domain, IPCIDR, IPCIDR6, Match:
			// 合法的 RuleType
		default:
			return nil, fmt.Errorf("规则文件第 %d 行存在未知的规则类型: %s", i+1, parts[0])
		}

		var target Action
		if len(parts) > 2 {
			// 如果是预期之外的类型统一视为 PROXY
			switch strings.ToUpper(parts[2]) {
			case "DIRECT":
				target = ActionDirect
			case "REJECT":
				target = ActionReject
			default:
				target = ActionProxy
			}
		} else {
			// 如果只有两部分，默认使用 PROXY
			target = ActionProxy
		}
		rule := Rule{
			Type:    ruleType,
			Payload: parts[1],
			Target:  target,
		}
		router.rules = append(router.rules, rule)
	}
	return router, nil
}

// 根据主机名决定流量的走向
func (r *Router) Match(host string) Action {
	// 1.处理全局模式
	switch r.mode {
	case ModeGlobal:
		return ActionProxy
	case ModeDirect:
		return ActionDirect
	}

	// 2.处理规则模式
	hostname := host
	if h, _, err := net.SplitHostPort(host); err == nil {
		// 如果有端口就去掉端口
		hostname = h
	}

	// IP形式的规则
	ip := net.ParseIP(hostname)
	for _, rule := range r.rules {
		match := false
		switch rule.Type {
		case DomainSuffix:
			match = strings.HasSuffix(hostname, rule.Payload)
		case DomainKeyword:
			match = strings.Contains(hostname, rule.Payload)
		case Domain:
			match = hostname == rule.Payload
		case IPCIDR, IPCIDR6:
			if ip != nil {
				_, cidr, err := net.ParseCIDR(rule.Payload)
				if err == nil && cidr.Contains(ip) {
					match = true
				}
			}
		case Match:
			// 最终匹配规则
			match = true
		}

		if match {
			return rule.Target
		}
	}

	// 如果所有规则都未匹配，默认走代理
	return ActionProxy
}
