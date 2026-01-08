# File View API - Fast Image/File Preview via Telegram CDN

## Overview

The `/view` endpoint provides **significantly faster file loading** compared to the standard `/download` endpoint by leveraging Telegram's CDN infrastructure directly. **Optimized for image viewing on web applications.**

### Performance Comparison

| Endpoint | Method | Latency | Use Case |
|----------|--------|---------|----------|
| `/download` | MTProto chunked | Higher (100-500ms+) | Large files, encrypted files |
| `/view` | Bot API CDN redirect | **Very Low (10-50ms cached)** | Images, thumbnails, previews |

### Key Optimizations for Image Viewing

1. **Two-level caching**:
   - Level 1: CDN URL cached by teldrive file ID (skips MTProto entirely)
   - Level 2: file_path cached by Bot API file_id (skips getFile call)

2. **Browser caching**: `Cache-Control: public, max-age=3000` (~50 min)

3. **Direct CDN redirect**: Browser loads image directly from Telegram's global CDN

## How It Works

```
┌──────────────┐     1. Get file metadata     ┌─────────────────┐
│   Client     │ ─────────────────────────►   │    Teldrive     │
│  (Browser)   │                              │     Server      │
└──────────────┘                              └────────┬────────┘
                                                       │
                                              2. Fetch document from Telegram
                                                       │
                                                       ▼
                                              ┌─────────────────┐
                                              │  Telegram API   │
                                              │   (MTProto)     │
                                              └────────┬────────┘
                                                       │
                                              3. Encode file_id & call getFile
                                                       │
                                                       ▼
                                              ┌─────────────────┐
                                              │  Bot API        │
                                              │  (getFile)      │
                                              └────────┬────────┘
                                                       │
                                              4. Return CDN URL
                                                       │
┌──────────────┐     5. HTTP 302 Redirect     ┌────────┴────────┐
│   Client     │ ◄─────────────────────────   │    Teldrive     │
│  (Browser)   │                              │     Server      │
└──────┬───────┘                              └─────────────────┘
       │
       │  6. Direct download from CDN
       ▼
┌─────────────────┐
│  Telegram CDN   │  (Fast global CDN)
│ api.telegram.org│
└─────────────────┘
```

## API Endpoints

### Files View

```http
GET /api/files/{id}/view
GET /api/files/{id}/view/{filename}
```

### Shared Files View

```http
GET /api/shares/{shareId}/files/{fileId}/view
GET /api/shares/{shareId}/files/{fileId}/view/{filename}
```

## Query Parameters

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `mode` | string | `redirect` | Response mode: `redirect` or `url` |
| `hash` | string | - | Authentication hash (alternative to JWT cookie) |

## Response Modes

### Mode: `redirect` (Default)

Returns HTTP 302 redirect to Telegram CDN URL.

**Response:**
```http
HTTP/1.1 302 Found
Location: https://api.telegram.org/file/bot<token>/<file_path>
```

**Use case:** Direct embedding in `<img>`, `<video>`, `<iframe>` tags.

```html
<img src="/api/files/abc123/view" />
<video src="/api/files/abc123/view/video.mp4"></video>
```

### Mode: `url`

Returns JSON with CDN URL for client-side handling.

**Request:**
```http
GET /api/files/{id}/view?mode=url
```

**Response:**
```json
{
  "url": "https://api.telegram.org/file/bot123456789:ABC.../documents/file_0.pdf",
  "expires_in": 3600,
  "file_name": "document.pdf",
  "file_size": 1048576,
  "mime_type": "application/pdf"
}
```

**Use case:** When you need to handle the URL programmatically.

```javascript
const response = await fetch('/api/files/abc123/view?mode=url');
const { url, expires_in } = await response.json();

// Use the URL directly
window.open(url, '_blank');

// Or cache it (valid for 1 hour)
localStorage.setItem('cdn_url', JSON.stringify({ url, expires: Date.now() + expires_in * 1000 }));
```

## Authentication

Same as other Teldrive API endpoints:

1. **JWT Cookie** (preferred for web clients)
2. **Hash parameter** for sharing/embedding

```http
GET /api/files/{id}/view?hash=abc123def456
```

## Limitations

| Limitation | Value | Fallback |
|------------|-------|----------|
| **Max file size** | 20 MB | Use `/download` |
| **Encrypted files** | Not supported | Use `/download` |
| **Multi-part files** | Not supported | Use `/download` |
| **Bot token required** | Yes | Use `/download` |

### Error Responses

| Status | Description |
|--------|-------------|
| 400 | File is encrypted, too large (>20MB), or multi-part |
| 401 | Missing or invalid authentication |
| 404 | File not found |
| 500 | Failed to get CDN URL |

**Example error response:**
```json
{
  "error": "file size exceeds Bot API limit (20MB), use /download instead"
}
```

## Caching

- CDN URLs are cached for **50 minutes** (server-side)
- Telegram guarantees URL validity for **at least 1 hour**
- Cache key: `botapi:file:{file_id}`

## Usage Examples

### HTML Embedding

```html
<!-- Image preview -->
<img src="/api/files/abc123/view/image.jpg" alt="Preview" />

<!-- PDF embed -->
<iframe src="/api/files/abc123/view/document.pdf" width="100%" height="600"></iframe>

<!-- Video player -->
<video controls>
  <source src="/api/files/abc123/view/video.mp4" type="video/mp4">
</video>

<!-- Audio player -->
<audio controls src="/api/files/abc123/view/audio.mp3"></audio>
```

### JavaScript/Fetch

```javascript
// Get URL for programmatic use
async function getFileUrl(fileId) {
  const response = await fetch(`/api/files/${fileId}/view?mode=url`, {
    credentials: 'include' // Include JWT cookie
  });

  if (!response.ok) {
    throw new Error(`Failed to get file URL: ${response.status}`);
  }

  return response.json();
}

// Usage
const { url, file_name, mime_type } = await getFileUrl('abc123');
console.log(`Download ${file_name} from: ${url}`);
```

### React Component Example

```jsx
function FilePreview({ fileId, fileName }) {
  const [url, setUrl] = useState(null);
  const [error, setError] = useState(null);

  useEffect(() => {
    fetch(`/api/files/${fileId}/view?mode=url`)
      .then(res => {
        if (!res.ok) throw new Error('File not available for preview');
        return res.json();
      })
      .then(data => setUrl(data.url))
      .catch(err => setError(err.message));
  }, [fileId]);

  if (error) {
    // Fallback to download endpoint
    return <a href={`/api/files/${fileId}/${fileName}`}>Download {fileName}</a>;
  }

  if (!url) return <div>Loading...</div>;

  return <img src={url} alt={fileName} />;
}
```

### Shared File Access

```javascript
// Access shared file without authentication
const shareId = 'share123';
const fileId = 'file456';

// Direct embed (no auth needed if share is public)
const embedUrl = `/api/shares/${shareId}/files/${fileId}/view`;

// Or get URL
const response = await fetch(`/api/shares/${shareId}/files/${fileId}/view?mode=url`);
const { url } = await response.json();
```

## Best Practices

### 1. Check File Size First

```javascript
async function getOptimalFileUrl(file) {
  // Use /view for small files, /download for large
  if (file.size <= 20 * 1024 * 1024 && !file.encrypted) {
    return `/api/files/${file.id}/view`;
  }
  return `/api/files/${file.id}/${file.name}`;
}
```

### 2. Handle Fallback Gracefully

```javascript
async function loadFile(fileId) {
  try {
    // Try fast CDN first
    const response = await fetch(`/api/files/${fileId}/view?mode=url`);
    if (response.ok) {
      return (await response.json()).url;
    }
  } catch (e) {
    console.log('CDN not available, falling back to download');
  }

  // Fallback to standard download
  return `/api/files/${fileId}/download`;
}
```

### 3. Cache URL on Client

```javascript
const urlCache = new Map();

async function getCachedFileUrl(fileId) {
  const cached = urlCache.get(fileId);
  if (cached && cached.expires > Date.now()) {
    return cached.url;
  }

  const response = await fetch(`/api/files/${fileId}/view?mode=url`);
  const data = await response.json();

  urlCache.set(fileId, {
    url: data.url,
    expires: Date.now() + (data.expires_in * 1000) - 60000 // 1 min buffer
  });

  return data.url;
}
```

## Comparison with /download

| Feature | /view | /download |
|---------|-------|-----------|
| **Speed** | Fast (CDN) | Slower (chunked) |
| **Max size** | 20 MB | Unlimited |
| **Encrypted** | No | Yes |
| **Multi-part** | No | Yes |
| **Range requests** | No | Yes |
| **Bot required** | Yes | No |
| **Bandwidth** | Telegram's | Your server's |

## Technical Details

### Bot API Integration

The `/view` endpoint uses Telegram's Bot API `getFile` method:

1. Document is fetched from channel via MTProto
2. `tg.Document` is encoded to Bot API `file_id` using `gotd/td/fileid` package
3. `getFile` API is called to obtain `file_path`
4. CDN URL is constructed: `https://api.telegram.org/file/bot{token}/{file_path}`

### file_id Encoding

```go
import "github.com/gotd/td/fileid"

// From tg.Document to Bot API file_id
fid := fileid.FromDocument(document)
encodedFileID, _ := fileid.EncodeFileID(fid)
// Result: "BQACAgIAAxk..." (Bot API compatible string)
```

### Cache Implementation

```go
// Cache key format
cacheKey := cache.Key("botapi", "file", fileID)

// TTL: 50 minutes (Telegram guarantees 1 hour)
filePath, err := cache.Fetch(c, cacheKey, 50*time.Minute, func() (string, error) {
    file, err := GetBotAPIFilePath(ctx, token, fileID)
    return file.FilePath, err
})
```

## Troubleshooting

### "no bot tokens configured"

Ensure you have at least one bot token configured in your Teldrive settings.

### "file size exceeds Bot API limit"

File is larger than 20MB. Use the standard `/download` endpoint instead.

### "encrypted files are not supported"

Encrypted files must be decrypted on-the-fly, which requires the standard `/download` endpoint.

### "multi-part files are not supported"

Files split into multiple parts cannot use Bot API. Use `/download` instead.

## See Also

- [Telegram Bot API - getFile](https://core.telegram.org/bots/api#getfile)
- [gotd/td fileid package](https://pkg.go.dev/github.com/gotd/td/fileid)
- [Teldrive Download API](./DOWNLOAD_API.md)
