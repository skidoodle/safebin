# safebin

`safebin` is a minimalist, self-hosted file storage service with **Zero-Knowledge at Rest** encryption.

## Features

- **Server-Side Encryption**: Files are encrypted using AES-256-GCM before touching the disk.
- **Log-Safe Keys**: The decryption key is stored in the URL fragment (`#`). Since fragments are never sent to the server, the key never appears in your HTTP access logs.
- **Integrity**: Uses GCM (Galois/Counter Mode) to ensure files cannot be tampered with while stored.
- **Deterministic**: Identical files result in the same ID, allowing for storage deduplication.

## Usage

You can interact with the service via the web interface or through the command line.

### Uploading a file

```bash
curl -F 'file=@archive.zip' https://bin.example.com
```

The server will return a URL containing the file ID and the decryption key:
`https://bin.example.com/vS6_1_8pS-Y_8-8_...`

### Downloading a file

Simply open the link in a browser or use `curl`:

```bash
curl https://bin.example.com/vS6_1_8pS-Y_8-8_... > archive.zip
```

## Configuration

`safebin` is configured via command-line flags:

| Flag | Description | Default |
| :--- | :--- | :--- |
| `-h` | Bind address for the server. | `0.0.0.0` |
| `-p` | Port to listen on. | `8080` |
| `-s` | Directory where encrypted files are stored. | `./storage` |
| `-m` | Maximum file size in mb. | `512` |

## Running Locally

### With Docker

```bash
git clone https://github.com/skidoodle/safebin
cd safebin
docker compose -f compose.dev.yaml up --build
```

### Without Docker

Requires Go 1.25 or higher.

```bash
git clone https://github.com/skidoodle/safebin
cd safebin
go build -o safebin .
./safebin -p 8080 -s ./data
```

## Deploying

### Docker Compose

The easiest way to deploy is using the provided `compose.yaml`.

```yaml
services:
  safebin:
    image: ghcr.io/skidoodle/safebin:main
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

## Retention Policy

The server runs a cleanup task every hour. Retention is calculated using a cubic scaling formula to balance disk usage:
- **Small files (< 1MB)**: Up to 365 days.
- **Large files (512MB)**: 24 hours.

This ensures that the server doesn't run out of disk space due to large binary blobs while allowing small text files or images to persist for longer periods.
