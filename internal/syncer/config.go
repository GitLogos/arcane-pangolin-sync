package syncer

import (
	"errors"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	ArcaneURL    string
	ArcaneAPIKey string

	PangolinURL              string
	PangolinAPIKey           string
	PangolinOrgID            string
	PangolinDomainID         string
	PangolinDefaultSiteID    int
	PangolinCreateSites      bool
	PangolinResourcePolicyID int
	AdoptExisting            bool

	DefaultTargetIP string
	EnvTargetIPs    map[string]string

	DryRun           bool
	StateFile        string
	ReconnectDelay   time.Duration
	StartupCatchup   bool
	ApplyPolicyOnRun bool
}

func LoadConfig() (Config, error) {
	cfg := Config{
		ArcaneURL:                trimSlash(os.Getenv("ARCANE_URL")),
		ArcaneAPIKey:             os.Getenv("ARCANE_API_KEY"),
		PangolinURL:              trimSlash(os.Getenv("PANGOLIN_URL")),
		PangolinAPIKey:           os.Getenv("PANGOLIN_API_KEY"),
		PangolinOrgID:            os.Getenv("PANGOLIN_ORG_ID"),
		PangolinDomainID:         os.Getenv("PANGOLIN_DOMAIN_ID"),
		PangolinDefaultSiteID:    envInt("PANGOLIN_SITE_ID", 0),
		PangolinCreateSites:      envBool("PANGOLIN_CREATE_SITES", false),
		PangolinResourcePolicyID: envInt("PANGOLIN_RESOURCE_POLICY_ID", 0),
		AdoptExisting:            envBool("ADOPT_EXISTING", false),
		DefaultTargetIP:          os.Getenv("DEFAULT_TARGET_IP"),
		EnvTargetIPs:             envMap("ARCANE_ENV_TARGET_IPS"),
		DryRun:                   envBool("DRY_RUN", true),
		StateFile:                envDefault("STATE_FILE", "/data/state.json"),
		ReconnectDelay:           time.Duration(envInt("RECONNECT_DELAY_SECONDS", 5)) * time.Second,
		StartupCatchup:           envBool("STARTUP_CATCHUP", true),
		ApplyPolicyOnRun:         envBool("APPLY_POLICY_ON_RUN", true),
	}

	var missing []string
	for name, value := range map[string]string{
		"ARCANE_URL":         cfg.ArcaneURL,
		"ARCANE_API_KEY":     cfg.ArcaneAPIKey,
		"PANGOLIN_URL":       cfg.PangolinURL,
		"PANGOLIN_API_KEY":   cfg.PangolinAPIKey,
		"PANGOLIN_ORG_ID":    cfg.PangolinOrgID,
		"PANGOLIN_DOMAIN_ID": cfg.PangolinDomainID,
	} {
		if strings.TrimSpace(value) == "" {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		return Config{}, errors.New("missing required environment variables: " + strings.Join(missing, ", "))
	}
	if cfg.PangolinDefaultSiteID == 0 && !cfg.PangolinCreateSites {
		return Config{}, errors.New("set PANGOLIN_SITE_ID or set PANGOLIN_CREATE_SITES=true")
	}
	return cfg, nil
}

func envDefault(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

func envBool(name string, fallback bool) bool {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func envInt(name string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func envMap(name string) map[string]string {
	out := map[string]string{}
	for _, part := range strings.Split(os.Getenv(name), ",") {
		key, value, ok := strings.Cut(strings.TrimSpace(part), "=")
		if !ok || strings.TrimSpace(key) == "" || strings.TrimSpace(value) == "" {
			continue
		}
		out[strings.TrimSpace(key)] = strings.TrimSpace(value)
	}
	return out
}

func trimSlash(value string) string {
	return strings.TrimRight(strings.TrimSpace(value), "/")
}
