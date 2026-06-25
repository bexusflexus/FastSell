# FastSell Frontend

React + TypeScript + Vite prototype for the FastSell intake/upload workflow.

## Setup

Use Node 22 LTS:

```bash
nvm use
npm install
```

## Development

```bash
npm run dev
```

The app serves the upload workflow at `/upload`; the root route redirects there.

Create a local `.env` from `.env.example` when you need to override the API host:

```bash
VITE_API_BASE_URL=http://localhost:8888
```

For phone testing, use the FastSell server LAN IP address if the phone cannot resolve the server hostname.

## Build

```bash
npm run build
```

Preview a production build with:

```bash
npm run preview
```

## Backend Integration

The upload page calls the FastSell Go API for containers and grouped image uploads. Uploads are sent as `multipart/form-data` with a `metadata` JSON field and file fields named `file_<client_file_id>`, such as `file_file_1`.
