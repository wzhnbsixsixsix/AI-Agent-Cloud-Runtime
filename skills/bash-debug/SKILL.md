---
name: bash-debug
description: Use AgentForge bash tool for concise shell diagnostics inside the isolated sandbox.
---

# Bash Debug

Use this skill when the user asks for shell checks, command output, environment inspection, or quick diagnostics.

- Use the `bash` tool with short, explicit commands.
- Prefer read-only commands first: `pwd`, `ls -la`, `id`, `uname -a`, `env`.
- Keep command output compact by using `head`, `tail`, or targeted flags.
- Do not rely on network access from bash; the sandbox runs with `network=none`.
- If a command fails, report the stderr and the exact command that failed.

