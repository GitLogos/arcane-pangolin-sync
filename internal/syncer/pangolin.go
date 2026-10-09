package syncer

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
)

type Pangolin struct {
	cfg    Config
	client *Client
	log    *slog.Logger
}

func newPangolin(cfg Config, log *slog.Logger) *Pangolin {
	return &Pangolin{cfg: cfg, client: newPangolinClient(cfg), log: log}
}

func (p *Pangolin) ensureResource(ctx context.Context, desired DesiredResource, state ManagedResource, dryRun bool) (ManagedResource, error) {
	siteID := state.SiteID
	if siteID == 0 {
		siteID = p.cfg.PangolinDefaultSiteID
	}
	if siteID == 0 && p.cfg.PangolinCreateSites {
		if dryRun {
			p.log.Info("dry run: would create Pangolin site", "name", desired.DisplayName)
			siteID = -1
		} else {
			site, err := p.createSite(ctx, desired.DisplayName, desired.Stack)
			if err != nil {
				return state, err
			}
			siteID = site.SiteID
		}
	}
	if siteID == 0 {
		return state, fmt.Errorf("no Pangolin site available for %s", desired.Key)
	}

	resourceID := state.ResourceID
	if resourceID == 0 {
		if dryRun {
			p.log.Info("dry run: would create Pangolin public resource",
				"name", desired.DisplayName, "subdomain", desired.Subdomain, "targets", len(desired.Targets))
			resourceID = -1
		} else {
			resource, err := p.createPublicResource(ctx, desired)
			if err != nil {
				return state, err
			}
			resourceID = resource.ResourceID
		}
	} else if p.cfg.PangolinResourcePolicyID > 0 {
		if dryRun {
			p.log.Info("dry run: would ensure shared policy on Pangolin public resource",
				"resourceId", resourceID, "resourcePolicyId", p.cfg.PangolinResourcePolicyID)
		} else if err := p.applyPolicy(ctx, resourceID); err != nil {
			return state, err
		}
	}

	if dryRun {
		for _, target := range desired.Targets {
			p.log.Info("dry run: would create Pangolin target",
				"resourceId", resourceID, "siteId", siteID, "ip", target.IP, "port", target.Port, "path", target.Path)
		}
		return ManagedResource{SiteID: siteID, ResourceID: resourceID}, nil
	}

	for _, targetID := range state.TargetIDs {
		if err := p.deleteTarget(ctx, targetID); err != nil {
			p.log.Warn("failed to delete old target", "targetId", targetID, "error", err)
		}
	}

	var targetIDs []int
	for index, target := range desired.Targets {
		created, err := p.createTarget(ctx, resourceID, siteID, target, index+1)
		if err != nil {
			return state, err
		}
		targetIDs = append(targetIDs, created.TargetID)
	}
	return ManagedResource{SiteID: siteID, ResourceID: resourceID, TargetIDs: targetIDs}, nil
}

func (p *Pangolin) deleteManaged(ctx context.Context, managed ManagedResource, dryRun bool) error {
	if managed.ResourceID != 0 {
		if dryRun {
			p.log.Info("dry run: would delete Pangolin public resource", "resourceId", managed.ResourceID)
		} else if err := p.client.delete(ctx, "/public-resource/"+strconv.Itoa(managed.ResourceID)); err != nil {
			return err
		}
	}
	if p.cfg.PangolinCreateSites && p.cfg.PangolinDefaultSiteID == 0 && managed.SiteID > 0 {
		if dryRun {
			p.log.Info("dry run: would delete Pangolin site", "siteId", managed.SiteID)
		} else if err := p.client.delete(ctx, "/site/"+strconv.Itoa(managed.SiteID)+"?deleteResources=true"); err != nil {
			return err
		}
	}
	return nil
}

func (p *Pangolin) createSite(ctx context.Context, name, niceID string) (PangolinSite, error) {
	var out APIResponse[PangolinSite]
	body := map[string]any{
		"name":   name,
		"type":   "newt",
		"niceId": niceID,
	}
	if err := p.client.put(ctx, "/org/"+escape(p.cfg.PangolinOrgID)+"/site", body, &out); err != nil {
		return PangolinSite{}, err
	}
	p.log.Info("created Pangolin site", "siteId", out.Data.SiteID, "name", name)
	return out.Data, nil
}

func (p *Pangolin) createPublicResource(ctx context.Context, desired DesiredResource) (PangolinResource, error) {
	var out APIResponse[PangolinResource]
	body := map[string]any{
		"name":     desired.DisplayName,
		"domainId": p.cfg.PangolinDomainID,
		"mode":     "http",
	}
	if desired.Subdomain != "" {
		body["subdomain"] = desired.Subdomain
	}
	if err := p.client.put(ctx, "/org/"+escape(p.cfg.PangolinOrgID)+"/public-resource", body, &out); err != nil {
		return PangolinResource{}, err
	}
	if p.cfg.PangolinResourcePolicyID > 0 {
		if err := p.applyPolicy(ctx, out.Data.ResourceID); err != nil {
			return PangolinResource{}, err
		}
	}
	p.log.Info("created Pangolin public resource", "resourceId", out.Data.ResourceID, "domain", out.Data.FullDomain)
	return out.Data, nil
}

func (p *Pangolin) createTarget(ctx context.Context, resourceID, siteID int, target DesiredTarget, priority int) (PangolinTarget, error) {
	var out APIResponse[PangolinTarget]
	body := map[string]any{
		"siteId":  siteID,
		"ip":      target.IP,
		"port":    target.Port,
		"mode":    target.Method,
		"method":  target.Method,
		"enabled": true,
	}
	if target.Path != "" {
		body["path"] = target.Path
		body["pathMatchType"] = "prefix"
		body["rewritePath"] = target.Path
		body["rewritePathType"] = "stripPrefix"
		body["priority"] = priority
	}
	if err := p.client.put(ctx, "/public-resource/"+strconv.Itoa(resourceID)+"/target", body, &out); err != nil {
		return PangolinTarget{}, err
	}
	p.log.Info("created Pangolin target", "targetId", out.Data.TargetID, "ip", target.IP, "port", target.Port, "path", target.Path)
	return out.Data, nil
}

func (p *Pangolin) deleteTarget(ctx context.Context, targetID int) error {
	return p.client.delete(ctx, "/target/"+strconv.Itoa(targetID))
}

func (p *Pangolin) applyPolicy(ctx context.Context, resourceID int) error {
	body := map[string]any{"resourcePolicyId": p.cfg.PangolinResourcePolicyID}
	return p.client.post(ctx, "/public-resource/"+strconv.Itoa(resourceID), body, nil)
}

func (p *Pangolin) ApplyPolicyToAllResources(ctx context.Context, dryRun bool, skip func(PangolinResource) bool) error {
	if p.cfg.PangolinResourcePolicyID == 0 {
		return nil
	}
	page := 1
	for {
		var out APIResponse[struct {
			Resources  []PangolinResource `json:"resources"`
			Pagination struct {
				Total int `json:"total"`
			} `json:"pagination"`
		}]
		path := "/org/" + escape(p.cfg.PangolinOrgID) + "/public-resources?pageSize=100&page=" + strconv.Itoa(page)
		if err := p.client.get(ctx, path, &out); err != nil {
			return err
		}
		if len(out.Data.Resources) == 0 {
			return nil
		}
		for _, resource := range out.Data.Resources {
			if skip != nil && skip(resource) {
				p.log.Info("skipping shared policy for ignored Pangolin resource", "resourceId", resource.ResourceID, "name", resource.Name, "subdomain", resource.Subdomain)
				continue
			}
			if resource.ResourcePolicyID != nil && *resource.ResourcePolicyID == p.cfg.PangolinResourcePolicyID {
				continue
			}
			if dryRun {
				p.log.Info("dry run: would apply shared policy to existing Pangolin resource",
					"resourceId", resource.ResourceID, "name", resource.Name, "resourcePolicyId", p.cfg.PangolinResourcePolicyID)
				continue
			}
			if err := p.applyPolicy(ctx, resource.ResourceID); err != nil {
				return err
			}
			p.log.Info("applied shared policy to existing Pangolin resource",
				"resourceId", resource.ResourceID, "name", resource.Name, "resourcePolicyId", p.cfg.PangolinResourcePolicyID)
		}
		page++
	}
}
