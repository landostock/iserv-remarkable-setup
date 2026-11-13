# Privacy

IServ → reMarkable is designed without a project-operated central account or credential service. Each installation connects services owned or chosen by the user.

## Data flow

During normal operation, documents and control data can pass through the following third-party services:

- the user's IServ server, which is the WebDAV source;
- the user's Fly.io account, where the sync job runs and the manifest is stored;
- the user's Cloudflare account, where the scheduler/dashboard configuration is stored;
- the user's reMarkable account/cloud, which receives documents selected for synchronization.

Those providers have their own privacy terms and operational logging. This project does not control those third-party systems.

## Credentials

The local setup companion receives credentials only when needed to connect or repair a service. IServ passwords and reMarkable authorization tokens are kept in process memory during setup/repair and are not written to `installation.json`. After provisioning they are stored using the user's cloud provider secret mechanisms where required for operation.

The local installation metadata contains non-secret deployment information such as generated app/Worker identifiers, region, Machine/KV identifiers, version, timestamps, dashboard address, and control-token expiry metadata. Recovery is intentionally limited to non-secret deployment identifiers and requires reauthentication for provider access.

## Local files

The setup companion creates an application-support/work directory for runtime templates, managed tools, resumable provisioning state, installation metadata, and optional diagnostics. On Unix-like systems private metadata is written with restrictive permissions. Local application removal and deliberate cloud-resource teardown are separate actions.

## Diagnostics

Diagnostic reports intentionally omit known credential fields and apply best-effort redaction to free-form messages, including common authentication tokens, URLs, email addresses, and the user's home-directory path. Because free-form diagnostic text cannot be proven to contain no personal information, users should review a report before sharing it.

## Telemetry

The project does not intentionally include a project-operated analytics or telemetry service. Third-party services and downloaded command-line tools may have their own operational logging or telemetry behavior; their policies are outside this project's control.

## Unofficial integrations

The project is not affiliated with or endorsed by IServ or reMarkable. The reMarkable cloud integration is unofficial and can require changes if the provider changes its API.
