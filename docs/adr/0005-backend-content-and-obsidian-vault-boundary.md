# Keep backend content separate from user-owned vault snapshots

The backend owns canonical extracted Markdown and search representations. Obsidian Reader renders that content without creating files, while Save to vault creates an independent user-owned snapshot with no synchronization registry. This avoids hidden vault mutations and makes saved notes durable even if the backend disappears.

