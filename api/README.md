# FastSell API

Minimal Go API for the FastSell ingestion workflow.

## Development

```sh
go test ./...
go mod tidy
go run ./cmd/fastsell-api
```

Configuration:

- `DATABASE_URL` is required.
- `PORT` defaults to `8888`.
- `INTAKE_DIR` defaults to `/app/data/intake/incoming`.
- `INTAKE_PROCESSING_DIR` defaults to `/app/data/intake/processing`.
- `INTAKE_FAILED_DIR` defaults to `/app/data/intake/failed`.
- `IMAGE_ROOT` defaults to `/app/data/images`.
- `IMAGE_ORIGINALS_DIR` defaults to `IMAGE_ROOT/originals`.
- `INTAKE_WORKER_ENABLED` defaults to `true`.
- `INTAKE_SCAN_INTERVAL_SECONDS` defaults to `5`.
- `INTAKE_STABLE_SECONDS` defaults to `3`.
- `MAX_UPLOAD_MB` defaults to `25`.

Endpoints:

- `GET /health`
- `GET /health/db`
- `GET /api/containers`
- `POST /api/containers`
- `GET /api/uploads/{id}`
- `POST /api/uploads/images`

The grouped image upload endpoint accepts `multipart/form-data` with a text `metadata` JSON field and one file part for each metadata file. File part names use `file_<client_file_id>`, for example `file_file_1`.

The intake worker runs inside the API process by default. It periodically scans pending/uploaded `image_assets`, moves files through `intake/processing`, validates v1 image types, writes accepted originals under `images/originals`, and marks invalid or missing files failed.

This baseline intentionally does not implement thumbnail generation, AI enrichment, authentication, or direct frontend database access.

## Upload Filename Rule

Browser-provided filenames are metadata only. The API stores the original value in `original_filename`, generates a server-side collision-resistant `stored_filename`, preserves or normalizes only a safe extension, and never trusts client paths or filename contents.
