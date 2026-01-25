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

## Code Quality
- Maintain high test coverage for both Regex and Client Hints logic.
- Ensure thread safety when handling shared resources (like the parser instance and cache).

## Project Info & Distribution
- **Official Repository**: [https://github.com/Octanium91/ua-parser](https://github.com/Octanium91/ua-parser)
- **Multi-Platform Clients**: The project provides official clients for multiple platforms (Go, Python, Node.js, Java) located in the `/clients` directory.
- **Package Distribution**:
  - All artifacts are primarily published to **GitHub Packages** (Maven for Java, npm for Node.js).
  - Docker images are published to both **GitHub Container Registry (ghcr.io)** and **Docker Hub**.
  - Shared libraries and Python wheels are distributed via **GitHub Releases**.

## Performance & Logging
- The application is designed for **high performance**; use LRU caching and avoid unnecessary allocations in the hot path.
- The system must provide **clear logs for resource updates** (e.g., downloading and swapping `regexes.yaml`) to ensure observability of the background updater.
