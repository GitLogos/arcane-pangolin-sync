# Arcane Pangolin Sync

Listens to Arcane's existing Server-Sent Events activity stream and reconciles successful Docker Compose project activity into Pangolin public resources.

It does **not** modify Arcane. It connects to:

- `GET /api/stream?channels=activities&limit=50`
- `GET /api/environments/{id}/projects/{projectId}/compose`
- `GET /api/environments/{id}/projects/{projectId}/runtime`

## Behavior

On `project_deploy` and `project_redeploy` success:

1. Fetch Arcane compose + runtime details.
2. If any Compose service has `pangolin.ignore=true`, skip the whole stack and leave any matching Pangolin resource unmanaged.
3. If any Compose service has `pangolin.enable=true`, use only those services.
4. Otherwise, find services with exactly one usable exposed/published port.
5. When `ADOPT_EXISTING=true`, first scan existing Pangolin public resources and adopt one whose enabled target `ip:port` set exactly matches the desired targets.
6. Otherwise, create one Pangolin public HTTP resource at `{stack-name}.{domain}`.
7. If one target is found, route `/` to that `ip:port`.
8. If multiple targets are found, create path targets like `/service-name`.
9. Apply `PANGOLIN_RESOURCE_POLICY_ID` when configured.

On `project_destroy` success:

- Delete the managed Pangolin public resource recorded in local state.
- Delete the site only when this syncer created per-stack sites with `PANGOLIN_CREATE_SITES=true`.

On `project_down` success:

- Leave the Pangolin resource in place. Down is often temporary; change this in code if you prefer deletion/disablement.

## Important networking notes

For published bridge ports and host-network services, the target IP must be the Docker node IP reachable from the Pangolin site/local site. Explicit `ARCANE_ENV_TARGET_IPS` entries win, then `*`, then `DEFAULT_TARGET_IP`. If none are set, the syncer tries to infer the IP from Arcane's configured environment `apiUrl` host.

For self-hosted Pangolin, `PANGOLIN_URL` should point at the Integration API server, not necessarily the dashboard URL. The default self-hosted integration port is usually `3003`; the syncer accepts the URL with or without `/v1`, for example `http://pangolin:3003` or `http://pangolin:3003/v1`.

Macvlan containers usually need the container's own LAN IP. Arcane's current runtime payload may not expose that IP. For those services, set explicit labels:

```yaml
labels:
  pangolin.enable: "true"
  pangolin.ip: "192.168.1.55"
  pangolin.port: "80"
```

## Optional labels

```yaml
services:
  manually-managed:
    image: nginx
    labels:
      pangolin.ignore: "true"      # project-level escape hatch; any service can carry it

  web:
    image: nginx
    labels:
      pangolin.enable: "true"      # if present on any service, unlabeled services are ignored
      pangolin.subdomain: "app"    # defaults to stack/project name
      pangolin.ip: "192.168.1.55"  # optional explicit target IP
      pangolin.port: "8080"        # optional explicit target port
      pangolin.protocol: "http"    # default: http
      pangolin.path: "/web"        # optional explicit path target
```

## Environment variables

Required:

| Variable | Description |
| --- | --- |
| `ARCANE_URL` | Arcane base URL, for example `http://arcane:3552` |
| `ARCANE_API_KEY` | Arcane API key |
| `PANGOLIN_URL` | Pangolin API URL, with or without `/v1` |
| `PANGOLIN_API_KEY` | Pangolin integration API key |
| `PANGOLIN_ORG_ID` | Pangolin organization ID |
| `PANGOLIN_DOMAIN_ID` | Domain ID used for public resources |
| `PANGOLIN_SITE_ID` or `PANGOLIN_CREATE_SITES=true` | Existing site/local site to attach targets to, or create per-stack sites |

Optional:

| Variable | Default | Description |
| --- | --- | --- |
| `PANGOLIN_RESOURCE_POLICY_ID` | unset | Shared policy to apply to all existing and new public resources |
| `ADOPT_EXISTING` | `false` | Adopt an existing Pangolin public resource when its enabled targets exactly match the desired `ip:port` set |
| `DRY_RUN` | `true` | Log actions without changing Pangolin |
| `STATE_FILE` | `/data/state.json` | Local state file used for safe deletes |
| `STARTUP_CATCHUP` | `true` | Query recent successful activities on startup |
| `APPLY_POLICY_ON_RUN` | `true` | Apply `PANGOLIN_RESOURCE_POLICY_ID` to existing public resources on startup |
| `DEFAULT_TARGET_IP` | unset | Fallback Docker node IP reachable from Pangolin |
| `ARCANE_ENV_TARGET_IPS` | unset | Comma-separated map like `0=192.168.1.10,env2=192.168.1.11,*=192.168.1.10` |

When using Pangolin local resources, keep `PANGOLIN_CREATE_SITES=false` and set `PANGOLIN_SITE_ID` to the existing local site ID. Automatic site creation currently creates Newt-style sites and is not recommended for local-resource setups.

`pangolin.ignore=true` is intended for stacks you want to manage manually in Pangolin, for example to avoid the shared policy. On deploy/redeploy, the syncer removes that stack from its managed state without deleting the Pangolin resource. During startup policy application, it skips resources whose recorded ID, name, or subdomain match an ignored stack.

## Run

```bash
docker compose -f docker-compose.example.yml up -d
```

Start with:

```env
DRY_RUN=true
```

When logs look right, switch to:

```env
DRY_RUN=false
```

## GitHub Actions image

The workflow in `.github/workflows/docker-image.yml` builds and pushes `ghcr.io/gitlogos/arcane-pangolin-sync:latest` to GHCR on every push to `main`.
