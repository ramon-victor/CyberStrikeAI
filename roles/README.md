# Role Configuration Files

This directory contains all role configuration files. Each role defines an AI behavior mode and the tools available to it.

## Creating a New Role

Create a YAML file under `roles/` with one of the following formats:

**Option 1: Explicitly specify the tool list (recommended)**

```yaml
name: Role name
description: Role description
user_prompt: User prompt (prepended to the user message to guide AI behavior)
icon: "Icon (optional)"
tools:
    # Add the tools you need...
    # Important: include the following core built-in MCP tools when the role needs vulnerability and knowledge-base workflows.
    - record_vulnerability
    - list_knowledge_risk_types
    - search_knowledge_base
enabled: true
```

**Option 2: Omit the `tools` field (use all enabled tools)**

```yaml
name: Role name
description: Role description
user_prompt: User prompt (prepended to the user message to guide AI behavior)
icon: "Icon (optional)"
# If the tools field is omitted, the role uses all tools enabled in MCP management by default.
enabled: true
```

## Important: Core Built-in MCP Tools

If you set the `tools` field, include the following tools in the list when the role needs vulnerability tracking or knowledge-base access (at minimum these three):

1. **`record_vulnerability`** - Vulnerability management tool used to record discovered vulnerabilities.
2. **`list_knowledge_risk_types`** - Knowledge-base risk-type listing tool used to view available vulnerability classifications.
3. **`search_knowledge_base`** - Knowledge-base search tool used to retrieve relevant vulnerability knowledge and remediation guidance.

## Field Reference

- `name`: Display name for the role.
- `description`: Short description of the role.
- `user_prompt`: Prompt text prepended to user messages for this role.
- `icon`: Optional display icon.
- `tools`: Optional list of tool names. Omit this field to use all enabled MCP tools.
- `enabled`: Whether the role is available.
