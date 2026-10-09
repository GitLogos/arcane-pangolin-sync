package syncer

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Syncer struct {
	cfg      Config
	log      *slog.Logger
	arcane   *Client
	pangolin *Pangolin

	mu    sync.Mutex
	state State
}

func New(cfg Config, log *slog.Logger) *Syncer {
	return &Syncer{
		cfg:      cfg,
		log:      log,
		arcane:   newArcaneClient(cfg),
		pangolin: newPangolin(cfg, log),
	}
}

func (s *Syncer) Run(ctx context.Context) error {
	state, err := loadState(s.cfg.StateFile)
	if err != nil {
		return fmt.Errorf("load state: %w", err)
	}
	s.state = state

	s.log.Info("starting Arcane Pangolin syncer", "dryRun", s.cfg.DryRun)
	if s.cfg.StartupCatchup {
		if err := s.catchup(ctx); err != nil {
			s.log.Warn("startup catch-up failed", "error", err)
		}
	}
	if s.cfg.ApplyPolicyOnRun {
		if err := s.pangolin.ApplyPolicyToAllResources(ctx, s.cfg.DryRun, s.shouldSkipPolicy); err != nil {
			return fmt.Errorf("apply policy to existing resources: %w", err)
		}
	}

	for ctx.Err() == nil {
		if err := s.stream(ctx); err != nil && ctx.Err() == nil {
			s.log.Warn("activity stream disconnected", "error", err, "reconnectDelay", s.cfg.ReconnectDelay)
			select {
			case <-ctx.Done():
			case <-time.After(s.cfg.ReconnectDelay):
			}
		}
	}
	return ctx.Err()
}

func (s *Syncer) stream(ctx context.Context) error {
	req, err := s.arcane.newStreamRequest(ctx, "/api/stream?channels=activities&limit=50")
	if err != nil {
		return err
	}
	resp, err := s.arcane.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
		return fmt.Errorf("stream failed: status=%d body=%s", resp.StatusCode, string(data))
	}
	s.log.Info("connected to Arcane activity stream")
	return readSSE(ctx, resp.Body, func(data []byte) error {
		var envelope ArcaneStreamEnvelope
		if err := json.Unmarshal(data, &envelope); err != nil {
			return nil
		}
		return s.handleEnvelope(ctx, envelope)
	})
}

func readSSE(ctx context.Context, r io.Reader, onData func([]byte) error) error {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	var data strings.Builder
	for scanner.Scan() {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		line := scanner.Text()
		if line == "" {
			if data.Len() > 0 {
				payload := strings.TrimSpace(data.String())
				data.Reset()
				if payload != "" {
					if err := onData([]byte(payload)); err != nil {
						return err
					}
				}
			}
			continue
		}
		if strings.HasPrefix(line, "data:") {
			if data.Len() > 0 {
				data.WriteByte('\n')
			}
			data.WriteString(strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	return fmt.Errorf("stream ended")
}

func (s *Syncer) handleEnvelope(ctx context.Context, envelope ArcaneStreamEnvelope) error {
	if envelope.Activity == nil {
		return nil
	}
	var activities []ArcaneActivity
	if envelope.Activity.Activity != nil {
		activities = append(activities, *envelope.Activity.Activity)
	}
	activities = append(activities, envelope.Activity.Activities...)
	for _, activity := range activities {
		if err := s.handleActivity(ctx, activity); err != nil {
			s.log.Warn("activity handling failed", "activityId", activity.ID, "type", activity.Type, "error", err)
		}
	}
	return nil
}

func (s *Syncer) catchup(ctx context.Context) error {
	for _, envID := range s.knownEnvIDs() {
		path := "/api/environments/" + escape(envID) + "/activities?limit=50&status=success&type=project_deploy,project_redeploy,project_destroy,project_down"
		// Arcane's paginated envelope has data at the top-level body, not nested under data.data.
		var raw struct {
			Success bool             `json:"success"`
			Data    []ArcaneActivity `json:"data"`
		}
		if err := s.arcane.get(ctx, path, &raw); err != nil {
			return err
		}
		for i := len(raw.Data) - 1; i >= 0; i-- {
			if err := s.handleActivity(ctx, raw.Data[i]); err != nil {
				s.log.Warn("catch-up activity handling failed", "activityId", raw.Data[i].ID, "error", err)
			}
		}
	}
	return nil
}

func (s *Syncer) knownEnvIDs() []string {
	ids := []string{"0"}
	for id := range s.cfg.EnvTargetIPs {
		if id != "*" && id != "0" {
			ids = append(ids, id)
		}
	}
	return ids
}

func (s *Syncer) handleActivity(ctx context.Context, activity ArcaneActivity) error {
	if activity.Status != "success" || !interestingActivity(activity.Type) {
		return nil
	}
	seenKey := activity.ID + ":" + activity.Status
	s.mu.Lock()
	if s.state.Seen[seenKey] {
		s.mu.Unlock()
		return nil
	}
	s.state.Seen[seenKey] = true
	s.mu.Unlock()

	envID := activity.SourceEnvironmentID
	if envID == "" {
		envID = activity.EnvironmentID
	}
	if envID == "" {
		envID = "0"
	}
	projectID := ""
	if activity.ResourceID != nil {
		projectID = *activity.ResourceID
	}
	if projectID == "" {
		projectID = stringFromMetadata(activity.Metadata, "projectID")
	}
	if projectID == "" {
		return fmt.Errorf("activity has no project id")
	}

	switch activity.Type {
	case "project_destroy":
		return s.deleteProject(ctx, envID, projectID)
	case "project_down":
		s.log.Info("project down observed; leaving Pangolin resource in place", "environmentId", envID, "projectId", projectID)
		return s.save()
	default:
		return s.reconcileProject(ctx, envID, projectID)
	}
}

func (s *Syncer) reconcileProject(ctx context.Context, envID, projectID string) error {
	compose, err := s.getProjectCompose(ctx, envID, projectID)
	if err != nil {
		return err
	}
	runtime, err := s.getProjectRuntime(ctx, envID, projectID)
	if err != nil {
		return err
	}
	compose.RuntimeServices = runtime.RuntimeServices

	desired, err := desiredFromProject(s.cfg, envID, compose)
	if err != nil {
		var ignored *IgnoredProjectError
		if errors.As(err, &ignored) {
			return s.ignoreProject(envID, projectID, ignored)
		}
		return err
	}

	s.mu.Lock()
	current := s.state.Resources[desired.Key]
	s.mu.Unlock()

	managed, err := s.pangolin.ensureResource(ctx, desired, current, s.cfg.DryRun)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.state.Resources[desired.Key] = managed
	s.mu.Unlock()
	s.log.Info("reconciled project", "environmentId", envID, "projectId", projectID, "subdomain", desired.Subdomain, "targets", len(desired.Targets))
	return s.save()
}

func (s *Syncer) ignoreProject(envID, projectID string, ignored *IgnoredProjectError) error {
	key := envID + ":" + projectID
	s.mu.Lock()
	if managed, ok := s.state.Resources[key]; ok {
		if managed.ResourceID > 0 {
			s.state.PolicyIgnoredResourceIDs[strconv.Itoa(managed.ResourceID)] = true
		}
		delete(s.state.Resources, key)
	}
	if ignored.Stack != "" {
		s.state.IgnoredSubdomains[ignored.Stack] = true
	}
	if ignored.Subdomain != "" {
		s.state.IgnoredSubdomains[ignored.Subdomain] = true
	}
	s.mu.Unlock()
	s.log.Info("project ignored by pangolin.ignore label; leaving Pangolin resources unmanaged", "environmentId", envID, "projectId", projectID, "stack", ignored.Stack, "subdomain", ignored.Subdomain)
	return s.save()
}

func (s *Syncer) shouldSkipPolicy(resource PangolinResource) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state.PolicyIgnoredResourceIDs[strconv.Itoa(resource.ResourceID)] {
		return true
	}
	if resource.Subdomain != nil && s.state.IgnoredSubdomains[slug(*resource.Subdomain)] {
		return true
	}
	if s.state.IgnoredSubdomains[slug(resource.Name)] {
		return true
	}
	return false
}

func (s *Syncer) deleteProject(ctx context.Context, envID, projectID string) error {
	key := envID + ":" + projectID
	s.mu.Lock()
	managed, ok := s.state.Resources[key]
	s.mu.Unlock()
	if !ok {
		s.log.Info("project destroyed but no managed Pangolin resource was recorded", "key", key)
		return s.save()
	}
	if err := s.pangolin.deleteManaged(ctx, managed, s.cfg.DryRun); err != nil {
		return err
	}
	s.mu.Lock()
	delete(s.state.Resources, key)
	s.mu.Unlock()
	s.log.Info("deleted managed Pangolin resource", "key", key)
	return s.save()
}

func (s *Syncer) getProjectCompose(ctx context.Context, envID, projectID string) (ArcaneProjectDetails, error) {
	var out APIResponse[ArcaneProjectDetails]
	path := "/api/environments/" + escape(envID) + "/projects/" + escape(projectID) + "/compose"
	if err := s.arcane.get(ctx, path, &out); err != nil {
		return ArcaneProjectDetails{}, err
	}
	return out.Data, nil
}

func (s *Syncer) getProjectRuntime(ctx context.Context, envID, projectID string) (ArcaneProjectDetails, error) {
	var out APIResponse[ArcaneProjectDetails]
	path := "/api/environments/" + escape(envID) + "/projects/" + escape(projectID) + "/runtime"
	if err := s.arcane.get(ctx, path, &out); err != nil {
		return ArcaneProjectDetails{}, err
	}
	return out.Data, nil
}

func (s *Syncer) save() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cfg.DryRun {
		return nil
	}
	return saveState(s.cfg.StateFile, s.state)
}

func interestingActivity(activityType string) bool {
	switch activityType {
	case "project_deploy", "project_redeploy", "project_destroy", "project_down":
		return true
	default:
		return false
	}
}

func stringFromMetadata(metadata map[string]any, key string) string {
	value, ok := metadata[key]
	if !ok {
		return ""
	}
	switch typed := value.(type) {
	case string:
		return typed
	case float64:
		return strconv.Itoa(int(typed))
	default:
		return fmt.Sprint(typed)
	}
}
