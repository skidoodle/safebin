# safebin

[![Go Version](https://img.shields.io/badge/Go-1.25+-00ADD8?style=flat-square&logo=go)](https://go.dev/)
[![License](https://img.shields.io/badge/License-GPLv2-blue.svg?style=flat-square)](LICENSE)
[![Docker Image](https://img.shields.io/badge/Docker-ghcr.io%2Fskidoodle%2Fsafebin-blue?style=flat-square&logo=docker)](https://github.com/skidoodle/safebin/pkgs/container/safebin)

**safebin** is a minimalist, self-hosted file storage service designed for efficiency and privacy. It utilizes **Convergent Encryption** to provide secure storage at rest while automatically deduplicating identical files to save disk space.

## 📖 Architecture & Security Model

Safebin is designed to be **Host-Proof at Rest**. While it is not a client-side E2EE solution, it ensures that the server cannot access stored data without the specific link generated at upload time.

### How it Works
1.  **Upload**: The server receives the file stream and calculates a SHA-256 hash of the content.
2.  **Key Generation**: This hash becomes the encryption key (Convergent Encryption).
3.  **Encryption**: The file is encrypted using **AES-128-GCM** and written to disk.
4.  **Deduplication**: Because the key is derived from the content, identical files generate the same ID. The server detects this and stores only one physical copy, regardless of how many times it is uploaded.
5.  **Zero-Knowledge Storage**: The server saves the file metadata (ID, size, expiry) but **discards the encryption key**.
6.  **Link Generation**: The key is encoded into the URL fragment returned to the user.

> **Security Note**: If the server's database or physical storage is seized, the files are mathematically inaccessible. However, because encryption occurs on the server, the process does have access to the plaintext in memory during the brief window of upload and download.

## ✨ Features

-   **Convergent Encryption & Deduplication**: Files are addressed by their content. Uploading the same file twice results in a single storage entry, significantly reducing disk usage.
-   **Tamper-Proof Storage**: Uses Galois/Counter Mode (GCM) to ensure data integrity. Modified files will fail decryption.
-   **Volatile Keys**: Decryption keys reside only in the generated URLs, not in the database.
-   **Smart Retention**: A cubic scaling algorithm prioritizes keeping small files (snippets, logs) for a long time, while large binaries expire quickly.
-   **Chunked Uploads**: Robust handling of large files via the web interface using 8MB chunks.

## 🚀 Deployment

### Docker Compose (Recommended)

```yaml
services:
  safebin:
    image: ghcr.io/skidoodle/safebin:latest
    container_name: safebin
    restart: unless-stopped
    ports:
      - "8080:8080"
    environment:
      - SAFEBIN_MAX_MB=512
    volumes:
      - safebin_data:/app/storage

volumes:
  safebin_data:
```

### Manual Installation

Requires Go 1.25 or higher.

```bash
# Build the binary
go build -o safebin .

# Run the server
./safebin -p 8080 -s ./data -m 1024
```

## ⚙️ Configuration

Configuration is handled via environment variables or command-line flags. Flags take precedence over environment variables.

| Flag | Environment Variable | Description | Default |
| :--- | :--- | :--- | :--- |
| `-h` | `SAFEBIN_HOST` | Interface/Bind address. | `0.0.0.0` |
| `-p` | `SAFEBIN_PORT` | Port to listen on. | `8080` |
| `-s` | `SAFEBIN_STORAGE` | Directory for database and files. | `./storage` |
| `-m` | `SAFEBIN_MAX_MB` | Maximum allowed file size in MB. | `512` |

## 💻 Usage

### Web Interface
Navigate to `http://localhost:8080`. Drag and drop files to upload. The browser handles chunking automatically.

### CLI (curl)
Safebin is optimized for terminal usage. You can upload files directly via `curl`:

```bash
# Upload a file
curl -F 'file=@screenshot.png' https://bin.example.com

# Response
https://bin.example.com/0iEZGtW-ikVdu...png
```

## ⏳ Retention Policy

To keep storage manageable, Safebin runs a cleanup task every hour. File lifetime is determined by size using a cubic curve:

*   **Small Files (< 1MB)**: Retained for **365 days**.
*   **Medium Files (~50% Max Size)**: Retained for ~30 days.
*   **Large Files (Max Size)**: Retained for **24 hours**.
*   **Incomplete Uploads**: Purged after **4 hours**.

## 📄 License

This project is licensed under the [GNU General Public License v2.0](LICENSE).
