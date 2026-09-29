## DevHub manifest maintenance

The root `devhub.yaml` is the source of truth for this service in the DevHub catalog. Keep it updated in the same pull request whenever any of these change:

- service purpose, domain, or owning team;
- public APIs or gRPC methods;
- service or infrastructure dependencies;
- relevant documentation paths.

Ensure referenced services and documentation paths remain valid. Do not merge changes that leave `devhub.yaml` stale.
