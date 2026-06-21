---
name: sandbox-files
description: Use AgentForge sandbox file tools for safe workspace read, write, append, and list operations.
---

# Sandbox File Tools

Use this skill when the user asks to inspect, create, update, or list files in the run workspace.

- Prefer `fs_list` before reading an unfamiliar directory.
- Use `fs_read` for file inspection and pass `max_bytes` when large output is likely.
- Use `fs_write` for create, overwrite, and append operations.
- Keep paths relative to the workspace; do not use `..` or absolute paths.
- After writing important content, read it back or list the directory when confirmation matters.

