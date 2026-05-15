# Citations and External Links (Example)

This file verifies that the skill package's **`references/`** directory is recognized by the list API, HTTP `resource_path`, and multi-agent local file tools.

## Test Method (Authorized Environment)

1. The `GET /api/skills/cyberstrike-eino-demo` response should include `references/citations.md` in `package_files`.
2. `GET /api/skills/cyberstrike-eino-demo?resource_path=references/citations.md` should return this file's content.
3. When multi-agent mode is enabled with `eino_skills.filesystem_tools`, this file can be read through a relative path.

## Placeholder Citation

- [OWASP Testing Guide](https://owasp.org/www-project-web-security-testing-guide/) (link-format example only)
