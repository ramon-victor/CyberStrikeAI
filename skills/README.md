# Skills Directory (Agent Skills / Eino)

- Each skill is a **subdirectory** whose root must contain **`SKILL.md`** (YAML front matter: `name`, `description` + Markdown body), following [agentskills.io](https://agentskills.io/specification.md).
- The **directory name must match `name`**.
- **Runtime loading**: in **Eino DeepAgent (multi-agent)** sessions, skills are progressively disclosed by the ADK **`skill` middleware**. The system prompt lists each skill's name/description, and the model can then call the **`skill`** tool to load the full `SKILL.md`. Optionally enable **`multi_agent.eino_skills.filesystem_tools`** to use local-style `read_file` / `execute` access for scripts and resources inside the package.
- **Web management**: HTTP `/api/skills/*` is still used to list, edit, and upload package files (implemented by `internal/skillpackage`, not MCP).
- **Runtime scope**: skills are progressively loaded by the ADK **`skill`** tool inside multi-agent (DeepAgent) sessions. The single-agent MCP loop does not include Skills; use multi-agent mode or a later single-agent Eino path.
