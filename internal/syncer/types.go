package syncer

import "time"

type APIResponse[T any] struct {
	Data    T      `json:"data"`
	Success bool   `json:"success"`
	Error   bool   `json:"error"`
	Message string `json:"message"`
	Status  int    `json:"status"`
}

type ArcaneStreamEnvelope struct {
	Channel  string                `json:"channel"`
	Type     string                `json:"type"`
	Activity *ArcaneActivityStream `json:"activity"`
}

type ArcaneActivityStream struct {
	Type          string           `json:"type"`
	EnvironmentID string           `json:"environmentId"`
	ActivityID    string           `json:"activityId"`
	Activity      *ArcaneActivity  `json:"activity"`
	Activities    []ArcaneActivity `json:"activities"`
	Error         string           `json:"error"`
	Timestamp     time.Time        `json:"timestamp"`
}

type ArcaneActivity struct {
	ID                    string         `json:"id"`
	EnvironmentID         string         `json:"environmentId"`
	SourceEnvironmentID   string         `json:"sourceEnvironmentId"`
	SourceEnvironmentName string         `json:"sourceEnvironmentName"`
	Type                  string         `json:"type"`
	Status                string         `json:"status"`
	ResourceType          *string        `json:"resourceType"`
	ResourceID            *string        `json:"resourceId"`
	ResourceName          *string        `json:"resourceName"`
	Metadata              map[string]any `json:"metadata"`
	StartedAt             time.Time      `json:"startedAt"`
	EndedAt               *time.Time     `json:"endedAt"`
}

type ArcaneProjectDetails struct {
	ID              string             `json:"id"`
	Name            string             `json:"name"`
	ComposeContent  string             `json:"composeContent"`
	RuntimeServices []ArcaneRuntimeSvc `json:"runtimeServices"`
	Services        []ComposeService   `json:"services"`
}

type ArcaneEnvironment struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	APIURL string `json:"apiUrl"`
}

type ArcaneRuntimeSvc struct {
	Name          string   `json:"name"`
	Image         string   `json:"image"`
	Status        string   `json:"status"`
	ContainerID   string   `json:"containerId"`
	ContainerName string   `json:"containerName"`
	Ports         []string `json:"ports"`
}

type ComposeService struct {
	Name        string         `json:"name"`
	Image       string         `json:"image"`
	Labels      map[string]any `json:"labels"`
	Ports       []ComposePort  `json:"ports"`
	Expose      []string       `json:"expose"`
	NetworkMode string         `json:"network_mode"`
}

type ComposePort struct {
	Name      string `json:"name"`
	Target    any    `json:"target"`
	Published any    `json:"published"`
	HostIP    string `json:"host_ip"`
	Protocol  string `json:"protocol"`
	Mode      string `json:"mode"`
}

type DesiredResource struct {
	Key         string
	Stack       string
	Subdomain   string
	DisplayName string
	Targets     []DesiredTarget
}

type DesiredTarget struct {
	ServiceName string
	IP          string
	Port        int
	Method      string
	Path        string
}

type ManagedResource struct {
	SiteID      int   `json:"siteId,omitempty"`
	SiteManaged bool  `json:"siteManaged,omitempty"`
	ResourceID  int   `json:"resourceId,omitempty"`
	TargetIDs   []int `json:"targetIds,omitempty"`
}

type State struct {
	Resources                map[string]ManagedResource `json:"resources"`
	Seen                     map[string]bool            `json:"seen"`
	PolicyIgnoredResourceIDs map[string]bool            `json:"policyIgnoredResourceIds,omitempty"`
	IgnoredSubdomains        map[string]bool            `json:"ignoredSubdomains,omitempty"`
}

type PangolinSite struct {
	SiteID int    `json:"siteId"`
	NiceID string `json:"niceId"`
	Name   string `json:"name"`
}

type PangolinResource struct {
	ResourceID       int     `json:"resourceId"`
	NiceID           string  `json:"niceId"`
	Name             string  `json:"name"`
	Subdomain        *string `json:"subdomain"`
	FullDomain       string  `json:"fullDomain"`
	DomainID         string  `json:"domainId"`
	ResourcePolicyID *int    `json:"resourcePolicyId"`
}

type PangolinTarget struct {
	TargetID int    `json:"targetId"`
	IP       string `json:"ip"`
	Port     int    `json:"port"`
	SiteID   int    `json:"siteId"`
	Enabled  bool   `json:"enabled"`
	Path     string `json:"path"`
}
