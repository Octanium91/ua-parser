# Documentation & Coding Guidelines

## Language
- All code documentation, including comments, docstrings, and README files, must be written exclusively in **English**.
- Variable names, functions, and other identifiers should be descriptive and in English.

## Header Priority
- When parsing User-Agent data, **Client Hints (Sec-CH-UA headers) must take priority** over the raw User-Agent string.
- The logic should first check for available Client Hints to determine the Operating System (e.g., distinguishing Windows 11 from Windows 10) and Device details before falling back to Regex-based UA parsing.

## CI/CD
- The project uses **GitHub Actions** for automated builds and testing.
- Any changes to the core logic or infrastructure should be verified against existing CI workflows.

## Go Version
- The project requires **Go 1.25 or higher**. This is a hard minimum — do not downgrade.
- Go 1.25 includes a critical fix for `c-shared` builds on **musl/Alpine Linux** ([golang/go#13492](https://github.com/golang/go/issues/13492)), resolving `initial-exec TLS` errors when loading `.so` via `dlopen()`. Earlier Go versions are incompatible with Alpine-based deployments.
- This constraint applies to all build targets: `go.mod`, `Dockerfile`, CI workflows (`release.yml`), and Docker images used for musl builds (`golang:1.25-alpine`).

## Code Quality
- Maintain high test coverage for both Regex and Client Hints logic.
- Ensure thread safety when handling shared resources (like the parser instance and cache).

## Package Distribution
- **Official Repository**: [https://github.com/Octanium91/ua-parser](https://github.com/Octanium91/ua-parser)
- **Multi-Platform Clients**: The project provides official clients for multiple platforms (Go, Python, Node.js, Java) located in the `/clients` directory.
- **Package Distribution**:
  - All artifacts are primarily published to **GitHub Packages** (Maven for Java, npm for Node.js).
  - Docker images are published to **GitHub Container Registry (ghcr.io)**.
  - Shared libraries and Python wheels are distributed via **GitHub Releases**.
- **Version Management**: Manual changes to package versions (e.g., in `package.json`, `pom.xml`, `setup.py`) are strictly prohibited. Versions are automatically managed and synchronized by the CI/CD pipeline during the release process based on the Git tag.
- **Installation Guides**: All installation documentation must explicitly state that for Node.js and Java, the user must configure their local package manager (npm, Maven) to use the GitHub Packages registry. Since the repository is public, authentication is generally not required for downloading packages, but registry configuration is still necessary.

## Performance & Logging
- The application is designed for **high performance**; use LRU caching and avoid unnecessary allocations in the hot path.
- The system must provide **clear logs for resource updates** (e.g., downloading and swapping `regexes.yaml`) to ensure observability of the background updater.

## Git Operations
- **Restrictions**: The AI assistant is strictly prohibited from performing any Git operations, including `git commit`, `git push`, `git tag`, or creating GitHub releases.
- **Responsibility**: All version control operations and deployment triggers must be performed manually by the user.
