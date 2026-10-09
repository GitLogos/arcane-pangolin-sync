package syncer

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

var (
	portMappingPattern = regexp.MustCompile(`(?:(\[?[0-9a-fA-F:.]+\]?):)?([0-9]+)->([0-9]+)/([a-zA-Z]+)`)
	portOnlyPattern    = regexp.MustCompile(`^([0-9]+)/([a-zA-Z]+)$`)
	slugPattern        = regexp.MustCompile(`[^a-z0-9-]+`)
)

type IgnoredProjectError struct {
	Stack     string
	Subdomain string
}

func (e *IgnoredProjectError) Error() string {
	return "project ignored by pangolin.ignore label"
}

func desiredFromProject(cfg Config, envID string, details ArcaneProjectDetails) (DesiredResource, error) {
	stack := slug(details.Name)
	if stack == "" {
		stack = slug(details.ID)
	}
	if projectIgnored(details) {
		subdomain := stack
		if value := firstServiceLabel(details, "pangolin.subdomain"); value != "" {
			subdomain = slug(value)
		}
		return DesiredResource{}, &IgnoredProjectError{Stack: stack, Subdomain: subdomain}
	}
	servicesByName := map[string]ComposeService{}
	for _, service := range details.Services {
		name := service.Name
		if name == "" {
			name = labelString(service.Labels, "com.docker.compose.service")
		}
		if name != "" {
			servicesByName[name] = service
		}
	}

	var labeled []DesiredTarget
	for _, runtime := range details.RuntimeServices {
		service := servicesByName[runtime.Name]
		if !labelBool(service.Labels, "pangolin.enable") {
			continue
		}
		target, err := targetForService(cfg, envID, runtime, service, true)
		if err != nil {
			return DesiredResource{}, err
		}
		labeled = append(labeled, target)
	}

	targets := labeled
	if len(targets) == 0 {
		for _, runtime := range details.RuntimeServices {
			service := servicesByName[runtime.Name]
			if labelString(service.Labels, "pangolin.enable") == "false" {
				continue
			}
			target, err := targetForService(cfg, envID, runtime, service, false)
			if err != nil {
				continue
			}
			targets = append(targets, target)
		}
	}
	if len(targets) == 0 {
		return DesiredResource{}, fmt.Errorf("no public targets found for project %s", details.Name)
	}

	sort.SliceStable(targets, func(i, j int) bool {
		return targets[i].ServiceName < targets[j].ServiceName
	})

	subdomain := stack
	if len(labeled) > 0 {
		if value := firstServiceLabel(details, "pangolin.subdomain"); value != "" {
			subdomain = slug(value)
		}
	}
	if len(targets) > 1 {
		for i := range targets {
			if targets[i].Path == "" {
				targets[i].Path = "/" + slug(targets[i].ServiceName)
			}
		}
	}

	return DesiredResource{
		Key:         fmt.Sprintf("%s:%s", envID, details.ID),
		Stack:       stack,
		Subdomain:   subdomain,
		DisplayName: details.Name,
		Targets:     targets,
	}, nil
}

func projectIgnored(details ArcaneProjectDetails) bool {
	for _, service := range details.Services {
		if labelBool(service.Labels, "pangolin.ignore") {
			return true
		}
	}
	return false
}

func targetForService(cfg Config, envID string, runtime ArcaneRuntimeSvc, service ComposeService, requireLabel bool) (DesiredTarget, error) {
	method := labelDefault(service.Labels, "pangolin.protocol", "http")
	ip := labelString(service.Labels, "pangolin.ip")
	port := labelInt(service.Labels, "pangolin.port", 0)
	path := labelString(service.Labels, "pangolin.path")

	if ip == "" || port == 0 {
		discovered, ok := discoverPort(cfg, envID, runtime, service)
		if ok {
			if ip == "" {
				ip = discovered.IP
			}
			if port == 0 {
				port = discovered.Port
			}
		}
	}
	if ip == "" {
		ip = targetIPForEnv(cfg, envID)
	}
	if ip == "" || port == 0 {
		if requireLabel {
			return DesiredTarget{}, fmt.Errorf("service %s has pangolin.enable=true but no usable ip/port", runtime.Name)
		}
		return DesiredTarget{}, fmt.Errorf("service %s has no usable published port", runtime.Name)
	}
	return DesiredTarget{
		ServiceName: runtime.Name,
		IP:          ip,
		Port:        port,
		Method:      method,
		Path:        normalizePath(path),
	}, nil
}

type discoveredPort struct {
	IP   string
	Port int
}

func discoverPort(cfg Config, envID string, runtime ArcaneRuntimeSvc, service ComposeService) (discoveredPort, bool) {
	var candidates []discoveredPort
	for _, raw := range runtime.Ports {
		if candidate, ok := parseRuntimePort(raw, targetIPForEnv(cfg, envID)); ok {
			candidates = append(candidates, candidate)
		}
	}
	if len(candidates) == 1 {
		return candidates[0], true
	}

	for _, port := range service.Ports {
		hostPort := anyInt(port.Published)
		if hostPort == 0 && strings.EqualFold(service.NetworkMode, "host") {
			hostPort = anyInt(port.Target)
		}
		if hostPort == 0 {
			continue
		}
		ip := strings.TrimSpace(port.HostIP)
		if ip == "" || ip == "0.0.0.0" || ip == "::" {
			ip = targetIPForEnv(cfg, envID)
		}
		candidates = append(candidates, discoveredPort{IP: ip, Port: hostPort})
	}
	if len(candidates) == 1 {
		return candidates[0], true
	}
	return discoveredPort{}, false
}

func parseRuntimePort(raw, fallbackIP string) (discoveredPort, bool) {
	raw = strings.TrimSpace(raw)
	match := portMappingPattern.FindStringSubmatch(raw)
	if len(match) == 5 {
		ip := strings.Trim(match[1], "[]")
		if ip == "" || ip == "0.0.0.0" || ip == "::" {
			ip = fallbackIP
		}
		port, _ := strconv.Atoi(match[2])
		return discoveredPort{IP: ip, Port: port}, port > 0
	}
	match = portOnlyPattern.FindStringSubmatch(raw)
	if len(match) == 3 {
		port, _ := strconv.Atoi(match[1])
		return discoveredPort{IP: fallbackIP, Port: port}, port > 0
	}
	return discoveredPort{}, false
}

func targetIPForEnv(cfg Config, envID string) string {
	if ip := cfg.EnvTargetIPs[envID]; ip != "" {
		return ip
	}
	if ip := cfg.EnvTargetIPs["*"]; ip != "" {
		return ip
	}
	return cfg.DefaultTargetIP
}

func firstServiceLabel(details ArcaneProjectDetails, key string) string {
	for _, service := range details.Services {
		if value := labelString(service.Labels, key); value != "" {
			return value
		}
	}
	return ""
}

func labelString(labels map[string]any, key string) string {
	if labels == nil {
		return ""
	}
	value, ok := labels[key]
	if !ok {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func labelDefault(labels map[string]any, key, fallback string) string {
	if value := labelString(labels, key); value != "" {
		return value
	}
	return fallback
}

func labelBool(labels map[string]any, key string) bool {
	value := strings.ToLower(labelString(labels, key))
	return value == "true" || value == "1" || value == "yes" || value == "on"
}

func labelInt(labels map[string]any, key string, fallback int) int {
	value := labelString(labels, key)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func anyInt(value any) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	case string:
		parsed, _ := strconv.Atoi(typed)
		return parsed
	default:
		return 0
	}
}

func normalizePath(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || value == "/" {
		return ""
	}
	if !strings.HasPrefix(value, "/") {
		value = "/" + value
	}
	return value
}

func slug(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = slugPattern.ReplaceAllString(value, "-")
	value = strings.Trim(value, "-")
	for strings.Contains(value, "--") {
		value = strings.ReplaceAll(value, "--", "-")
	}
	if len(value) > 63 {
		value = strings.TrimRight(value[:63], "-")
	}
	return value
}
