# Keep AI secrets out of the product API

Status: Accepted
Date: 2026-09-27

Workspace Settings expose non-secret AI and runtime choices, while API keys and credentials remain in operator-managed environment or secret storage. This keeps secrets out of REST responses, product archives, the Obsidian vault, and synchronized plugin data while still allowing models, prompts, limits, and feature toggles to be configured in the product.
