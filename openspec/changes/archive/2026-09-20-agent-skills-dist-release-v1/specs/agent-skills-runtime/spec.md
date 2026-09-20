## ADDED Requirements

### Requirement: The runtime SHALL resolve exact-version dist releases safely

`ResolveDistRelease` SHALL download only the exact `<product>/v<X.Y.Z>` tag from
the configured asset base, SHALL require a bare `X.Y.Z` version and a lowercase
product name, SHALL verify every downloaded byte against both `checksums.txt`
and the install manifest's embedded SHA-256, SHALL reject asset bases that are
not HTTPS or loopback HTTP, and SHALL extract bundles into a private staging
directory guarded against path traversal, links, and unbounded size. The
returned cleanup function SHALL remove the staging directory.

#### Scenario: A bundle digest disagrees with the install manifest

- **WHEN** the manifest declares a SHA-256 that does not match the downloaded bytes
- **THEN** resolution SHALL fail with `AGENT_SKILLS_DIGEST_MISMATCH`
- **AND** no staging directory SHALL survive

#### Scenario: The requested version does not match the manifest identity

- **WHEN** the install manifest names a different product version or tag
- **THEN** resolution SHALL fail with `AGENT_SKILLS_RELEASE_UNAVAILABLE`

#### Scenario: The asset base is plain HTTP on a non-loopback host

- **WHEN** the caller passes `http://10.0.0.5:8080/releases`
- **THEN** resolution SHALL fail with `AGENT_SKILLS_SOURCE_UNTRUSTED` before any download
