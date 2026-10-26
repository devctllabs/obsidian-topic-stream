# Write to the vault only through explicit independent snapshots

Status: Accepted
Date: 2026-09-27

Reader never writes files. Save to vault asks for destination and content mode, never overwrites, and creates a new independent snapshot each time; automatic mirrors and managed-note updates were rejected to preserve user ownership of the vault.

