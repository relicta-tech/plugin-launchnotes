# LaunchNotes Plugin for Relicta

Official LaunchNotes plugin for [Relicta](https://github.com/relicta-tech/relicta) - AI-powered release management.

## Features

- Create announcements in LaunchNotes
- Draft and auto-publish workflow
- Category and change type support
- Email subscriber notifications
- Secure TLS 1.3+ connections
- XSS protection via HTML escaping

## Installation

```bash
relicta plugin install launchnotes
relicta plugin enable launchnotes
```

## Configuration

Add to your `release.config.yaml`:

```yaml
plugins:
  - name: launchnotes
    enabled: true
    config:
      # LaunchNotes API token (Management token required)
      api_token: "ln_..."

      # LaunchNotes project ID
      project_id: "proj_..."

      # Create as draft instead of publishing (default: false)
      create_draft: false

      # Auto-publish after creation (default: true)
      auto_publish: true

      # Category IDs to associate with announcements
      categories:
        - "cat_feature"
        - "cat_bugfix"

      # Change type IDs (e.g., "new", "improved", "fixed")
      change_types:
        - "new"
        - "improved"

      # Include full changelog in content (default: true)
      include_changelog: true

      # Title template (default: "Release {{version}}")
      title_template: "v{{version}} Release"

      # Send email notifications to subscribers (default: false)
      notify_subscribers: false
```

### Environment Variables

Instead of hardcoding sensitive values, use environment variables:

```bash
export LAUNCHNOTES_API_TOKEN="ln_..."
export LAUNCHNOTES_PROJECT_ID="proj_..."
```

## Hooks

This plugin responds to the following hooks:

| Hook | Behavior |
|------|----------|
| `post-publish` | Creates/publishes announcement |
| `on-success` | Creates/publishes announcement |

## Content Generation

The announcement content is generated from release data in this priority:

1. `release_notes` - Custom release notes if provided
2. `changelog` - Full changelog (if `include_changelog: true`)
3. `changes` - Structured changes (breaking, features, fixes)

## Security Features

- **TLS 1.3+**: All connections use modern TLS
- **Redirect protection**: Only allows redirects within launchnotes.io
- **XSS prevention**: HTML-escapes all user content
- **Timeout protection**: 30 second request timeout

## Example Workflow

```yaml
# release.config.yaml
versioning:
  strategy: conventional

plugins:
  - name: launchnotes
    enabled: true
    config:
      auto_publish: true
      notify_subscribers: true
      title_template: "Release {{version}}"
```

```bash
# Run release
relicta plan
relicta bump
relicta notes
relicta approve
relicta publish
```

## Draft Workflow

For reviewing announcements before publishing:

```yaml
plugins:
  - name: launchnotes
    config:
      create_draft: true
      auto_publish: false
```

Then manually publish in the LaunchNotes dashboard after review.

## API Token

You need a **Management token** from LaunchNotes to create announcements:

1. Go to LaunchNotes dashboard
2. Navigate to Settings > API
3. Create a new Management token
4. Copy and securely store the token

## License

MIT License - see [LICENSE](LICENSE) for details.
