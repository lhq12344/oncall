# OnCall Hooks

OnCall hooks are an observation, notification, audit, and conservative blocking layer for agent lifecycle events. They do not replace the permission checker and cannot turn a denied or approval-gated tool call into an allowed call.

## Configuration

Hooks are disabled by default. Set ONCALL_HOOKS_CONFIG to a local YAML file before starting OnCall.

    $env:ONCALL_HOOKS_CONFIG="D:\Code\project\oncall\.oncall\hooks.local.yaml"
    cd backend && go run main.go

Do not commit local hook files that contain URLs, tokens, headers, or incident-specific payloads.

## Supported Events

- session_start
- session_end
- turn_start
- turn_end
- agent_start
- agent_end
- agent_error
- tool_pre_use
- tool_post_use
- tool_error
- approval_requested
- resume_requested

## Supported Actions

- log: records a hook notification in the in-process queue.
- message: records a human-readable hook notification.
- audit: records an audit-style hook notification.
- webhook: sends a capped, redacted JSON payload to an allowlisted HTTP(S) endpoint.

Arbitrary command hooks are not enabled by default. If command hooks are introduced later, they must go through explicit allowlists and the existing permission system.

## Conditions

Conditions support equality, inequality, regex, glob, boolean AND/OR, and negation.

    tool == "WriteFile"
    args.file_path =* "**/*.go"
    event =~ /^tool_/
    tool == "Bash" && args.command =~ /rm -rf/

Common fields include event, tool, agent, session_id, checkpoint_id, args.<name>, metadata.<name>, file_path, message, error, and result.

## Examples

### Read-only Notification

    enabled: true
    hooks:
      - id: observe-turn-start
        event: turn_start
        action:
          type: audit
          message: "turn started"

### Pre-tool Reject

This blocks matching tool calls before the target tool runs, but it does not bypass the existing permission checker for non-matching calls.

    enabled: true
    hooks:
      - id: block-env-write
        event: tool_pre_use
        if: 'tool == "WriteFile" && args.file_path =* "**/.env"'
        reject: true
        reject_reason: "writing .env files is blocked by hook policy"
        action:
          type: message
          message: "blocked sensitive file write"

### Approval Audit

    enabled: true
    hooks:
      - id: audit-approval
        event: approval_requested
        action:
          type: audit
          message: "tool approval requested"

### Webhook

Webhook hosts must be allowlisted, payloads are capped, and sensitive fields such as token, secret, password, and authorization are redacted.

    enabled: true
    webhook_allowed_hosts:
      - hooks.example.com
    hooks:
      - id: notify-tool-error
        event: tool_error
        action:
          type: webhook
          url: "https://hooks.example.com/oncall"
          timeout_ms: 3000

## Runtime Visibility

Use /hooks status or /status in the chat UI to view whether the hook engine is enabled, how many rules are loaded, and how many notifications are pending. These commands report counts and safety posture only; they do not dump complete tool arguments or secrets.

