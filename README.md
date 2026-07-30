# Bucketd

`bucketd` is a lightweight, high-performance S3-compatible object storage server written in Go, using SQLite for metadata management and local disk storage for object payload data.

---

## Features

- **S3 Compatible API**: Implements standard S3 REST endpoints for bucket and object operations.
- **Metadata Management**: SQLite database tracking buckets, objects, ownership, sizes, ETags (MD5), storage classes, and modification timestamps.
- **Atomic Disk Streaming**: Uploaded objects are calculated with MD5 checksums (ETag) and written atomically to storage via temporary file renaming.
- **Path Traversal Protection**: Key sanitization preventing relative path escaping beyond bucket boundaries.
- **JWT Authentication**: Middleware validation for bearer tokens.

---

## Supported S3 API Endpoints

### Bucket Operations
- `PUT /{bucket}` - Create a new bucket.
- `GET /` - List all buckets owned by the authenticated user (`ListAllMyBucketsResult`).
- `HEAD /{bucket}` - Check if a bucket exists and user has access.
- `DELETE /{bucket}` - Delete an empty bucket.

### Object Operations
- `PUT /{bucket}/{key...}` - Upload an object (supports overwrites / UPSERT).
- `GET /{bucket}` - List objects in a bucket (`list-type=2`, `ListObjectsResult`).
- `GET /{bucket}/{key...}` - Download an object content stream.
- `HEAD /{bucket}/{key...}` - Retrieve object metadata headers (`Content-Length`, `ETag`, `Last-Modified`).
- `DELETE /{bucket}/{key...}` - Delete an object.

---

## Project Structure

```
bucketd/
├── api/
│   └── server/
│       └── main.go           # Server entrypoint and http.ServeMux routes
├── internal/
│   ├── controller/
│   │   ├── auth.go           # JWT Authentication Middleware
│   │   └── controller.go     # HTTP request handlers & S3 XML response serialization
│   ├── handler/
│   │   └── handler.go        # Storage execution, path sanitization, and business logic
│   ├── model/
│   │   └── model.go          # S3 XML request/response structures & Error types
│   └── repository/
│       └── repository.go     # SQLite database interactions & persistence
├── tests/                    # DB schemas and test scripts
├── go.mod
└── README.md
```

---

## Getting Started

### Prerequisites

- **Go**: 1.22 or higher
- **SQLite3 / CGO**: Required for `github.com/mattn/go-sqlite3`

### Configuration

Create a `.env` file in the root directory (or copy `.env.example`):

```env
PORT=7071
JWT_SECRET=your-secret-key-here
STORAGE_ROOT=./storage
DB_PATH=file:./tests/bucketd.db?_foreign_keys=on
```

| Variable | Description | Default |
| --- | --- | --- |
| `PORT` | HTTP port the server listens on | `7071` |
| `JWT_SECRET` | Secret key for verifying JWT tokens | `97126c91-2838-45c1-a017-8e22dac15fd0` |
| `STORAGE_ROOT` | Directory path for payload storage | `./tests/objectd` |
| `DB_PATH` | SQLite DSN connection string | `file:./tests/bucketd.db?_foreign_keys=on` |

### Building the Server

```bash
go build -o bucketd ./api/server
```

### Running the Server

```bash
./bucketd
```

By default, the server listens on `http://localhost:7071`.

---

## Database Schema

The metadata storage relies on two SQLite tables:

```sql
CREATE TABLE buckets (
    name         TEXT PRIMARY KEY,
    owner_id     TEXT NOT NULL,
    created_at   DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at   DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE objects (
    bucket_name   TEXT NOT NULL,
    key           TEXT NOT NULL,
    size          INTEGER NOT NULL,
    etag          TEXT NOT NULL,
    storage_class TEXT NOT NULL DEFAULT 'STANDARD',
    created_by    TEXT NOT NULL,
    last_modified DATETIME DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (bucket_name, key),
    FOREIGN KEY (bucket_name) REFERENCES buckets(name) ON DELETE CASCADE
);
```

---

## License

MIT License.
