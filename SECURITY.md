# Security policy

## Supported code

Security fixes target the current `main` branch until Finally Server publishes its first release. The policy will identify supported releases when release builds exist. Operators should also track security updates from upstream Vikunja.

## Report a vulnerability privately

Use [GitHub private vulnerability reporting](https://github.com/kaungzinye/finally-server/security/advisories/new). Do not open a public issue for a suspected vulnerability.

If GitHub private reporting is unavailable, email `kaungzinye11@gmail.com` with the subject `Finally Server security report`.

Include the affected commit or release, reproduction steps with secrets removed, impact, affected data, and a safe contact method. You should receive an acknowledgement within 72 hours. Triage and remediation timing depends on severity and reproducibility.

## Scope

Reports are in scope when they affect:

- authentication, API tokens, sessions, or authorization;
- the `/api/v2/finally` contract or its permission boundaries;
- Google OAuth token exchange, encryption, storage, or revocation;
- Redis isolation or calendar account ownership;
- task, attendee, event, or credential exposure through logs or responses;
- configuration that silently disables a documented security control;
- code added or modified by Finally Server.

Report vulnerabilities that reproduce against unmodified Vikunja to [Vikunja's security contact](https://vikunja.io/contact/#security). Reports about Google, Redis, a database, or another independent dependency should go to that project unless Finally Server causes the exposure.

## Research guidelines

Test only systems and accounts you own or have permission to assess. Avoid service disruption, social engineering, persistence, and access to other people's data. Stop and report the issue if private data appears.

The maintainer will not pursue action against good-faith research that follows these guidelines and gives the project a reasonable opportunity to address the report. This statement does not authorize testing against upstream or third-party infrastructure.
