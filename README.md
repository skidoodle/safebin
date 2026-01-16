# safebin

`safebin` is a minimalist, self-hosted file storage service with **Zero-Knowledge at Rest** encryption.

## Features

- **End-to-End Encryption**: Files are encrypted using AES-128-GCM before being written to disk.
- **Key-Derived URLs**: The decryption key is part of the URL. The server uses this key to locate and decrypt the file on the fly.
- **Integrity**: Uses GCM (Galois/Counter Mode) to ensure files cannot be tampered with while stored.
- **Storage Deduplication**: Identical files result in the same ID, saving disk space.
- **Chunked Uploads**: Supports large file uploads via the web interface using 8MB chunks.

## Usage

### Web Interface
Simply drag and drop files into the browser. The interface handles chunking and provides a shareable link once the upload is finalized.

### Command Line (CLI)
You can upload files directly using `curl`:

```bash
curl -F 'file=@photo.jpg' https://bin.example.com
```

The server will return a direct link:
`https://bin.example.com/0iEZGtW-ikVdu...jpg`

## Configuration

`safebin` can be configured via environment variables or command-line flags:

| Flag | Environment Variable | Description | Default |
| :--- | :--- | :--- | :--- |
| `-h` | `SAFEBIN_HOST` | Bind address for the server. | `0.0.0.0` |
| `-p` | `SAFEBIN_PORT` | Port to listen on. | `8080` |
| `-s` | `SAFEBIN_STORAGE` | Directory for encrypted storage. | `./storage` |
| `-m` | `SAFEBIN_MAX_MB` | Maximum file size in MB. | `512` |

## Deployment

### Docker Compose
The easiest way to deploy is using the provided `compose.yaml`:

```yaml
services:
  safebin:
    image: ghcr.io/skidoodle/safebin:latest
    container_name: safebin
    restart: unless-stopped
    ports:
      - 8080:8080
    environment:
      - SAFEBIN_HOST=0.0.0.0
      - SAFEBIN_PORT=8080
      - SAFEBIN_STORAGE=/app/storage
      - SAFEBIN_MAX_MB=512
    volumes:
      - data:/app/storage

volumes:
  data:
```

### Manual Build
Requires Go 1.25 or higher.

```bash
go build -o safebin .
./safebin -p 8080 -s ./data
```

## Retention Policy

The server runs a background cleanup task every hour. Retention is calculated using a cubic scaling formula to prioritize small files:

- **Small files (e.g., < 1MB)**: Kept for up to **365 days**.
- **Large files (at Max MB)**: Kept for **24 hours**.
- **Temporary Uploads**: Unfinished chunked uploads are purged after **4 hours**.

## License

This project is licensed under the **GNU General Public License v2.0**.
