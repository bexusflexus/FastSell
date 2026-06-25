# Roadmap

FastSell v0.1 is the public baseline. The immediate goal is a reliable self-hosted inventory intake, review, and sales-prep workflow.

## v0.1 Baseline

- Docker Compose deployment
- PostgreSQL schema baseline
- React/TypeScript frontend
- Go API and workers
- Local image storage
- Inventory, containers, locations, review queues, listing drafts, and admin health screens
- Optional user-configured AI assistance

## Near-Term Work

- Harden authentication and multi-user access
- Improve backup and restore tooling
- Add more focused integration tests
- Expand listing/export workflows
- Improve migration and upgrade documentation
- Improve Whole Scene scan label to crop identification.  Goal is 85% accuracy.  I think that is doable based on past success and experimentation.
- Support for other AI providers such as ChatGPT. Currently only Gemini tooling has been tested and approved. Even though another provider might work results can be undefined.
- Support for local LLMs via Ollama.  

