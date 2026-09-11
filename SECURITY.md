# Security policy

## Development status

Ark is under active development and has no published releases. Report vulnerabilities
against the current `main` branch and include the exact commit you tested. A supported
release policy will be defined when releases are published.

## Report a vulnerability privately

Email [chitpole@proton.me](mailto:chitpole@proton.me). If GitHub's private vulnerability
reporting is enabled, you can also use **Report a vulnerability** on the repository's
[Security advisories page](https://github.com/ararat-network/ark/security/advisories).
If that option is unavailable, use email.

Do not put exploit details in public issues, pull requests or discussions. Ordinary
bugs without a security impact can use the public issue tracker.

Include:

- The affected component, commit or version, and relevant configuration.
- The expected behaviour, observed behaviour and potential impact.
- Reproduction steps or a minimal proof of concept, preferably on a disposable localnet.
- Relevant logs with credentials, personal data and signing material removed.

Never send private keys, seed phrases or live provider credentials. Test only on
systems you own or have permission to test.

## Scope and handling

Reports may concern the node, consensus and transaction processing, chain modules,
pricefeed sidecar, or the repository's build and release tooling. Include any relevant
upstream dependency and version when the issue involves third-party code.

Maintainers will assess the report and coordinate fixes and disclosure through the
private channel. Keep follow-up details there while a vulnerability remains unresolved.
The [threat model](docs/design/THREAT_MODEL.md) describes Ark's trust boundaries and
controls.
