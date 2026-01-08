// Package tgc provides Telegram client utilities.
//
// This file implements Bot API helper functions for fast file access via Telegram CDN.
package tgc

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/gotd/td/fileid"
	"github.com/gotd/td/tg"
	"github.com/tgdrive/teldrive/internal/cache"
)

const (
	// BotAPIBaseURL is the base URL for Telegram Bot API
	BotAPIBaseURL = "https://api.telegram.org"

	// MaxBotAPIFileSize is the maximum file size supported by Bot API (20MB)
	MaxBotAPIFileSize = 20 * 1024 * 1024

	// BotAPIFileCacheTTL is the cache duration for file_path (50 minutes, link valid for 1 hour)
	BotAPIFileCacheTTL = 50 * time.Minute
)

// BotAPIFile represents the response from Bot API getFile method
type BotAPIFile struct {
	FileID       string `json:"file_id"`
	FileUniqueID string `json:"file_unique_id"`
	FileSize     int64  `json:"file_size"`
	FilePath     string `json:"file_path"`
}

// BotAPIResponse represents a generic Bot API response
type BotAPIResponse struct {
	OK          bool        `json:"ok"`
	Result      interface{} `json:"result,omitempty"`
	Description string      `json:"description,omitempty"`
	ErrorCode   int         `json:"error_code,omitempty"`
}

// BotAPIFileResponse represents the response from getFile
type BotAPIFileResponse struct {
	OK          bool       `json:"ok"`
	Result      BotAPIFile `json:"result,omitempty"`
	Description string     `json:"description,omitempty"`
	ErrorCode   int        `json:"error_code,omitempty"`
}

// EncodeFileID encodes a tg.Document to a Bot API compatible file_id string.
//
// This uses gotd/td's fileid package to properly encode the document information
// (ID, AccessHash, FileReference, DC) into the format expected by Bot API.
//
// Example:
//
//	doc := media.Document.(*tg.Document)
//	fileID, err := EncodeFileID(doc)
//	// fileID = "BQACAgIAAxk..." (Bot API compatible string)
func EncodeFileID(doc *tg.Document) (string, error) {
	if doc == nil {
		return "", fmt.Errorf("document is nil")
	}

	fid := fileid.FromDocument(doc)
	encoded, err := fileid.EncodeFileID(fid)
	if err != nil {
		return "", fmt.Errorf("failed to encode file_id: %w", err)
	}

	return encoded, nil
}

// GetBotAPIFilePath calls Bot API getFile to get the file_path for downloading.
//
// The file_path can be used to construct a CDN download URL:
// https://api.telegram.org/file/bot<token>/<file_path>
//
// Note: The returned file_path is guaranteed valid for at least 1 hour.
// After expiration, call this method again to get a new file_path.
//
// Parameters:
//   - ctx: Context for cancellation
//   - token: Bot token (format: "123456789:ABCdefGHI...")
//   - fileID: Bot API file_id string (from EncodeFileID)
//
// Returns:
//   - BotAPIFile containing file_path and metadata
//   - error if the request fails or file is not found
func GetBotAPIFilePath(ctx context.Context, token, fileID string) (*BotAPIFile, error) {
	apiURL := fmt.Sprintf("%s/bot%s/getFile?file_id=%s", BotAPIBaseURL, token, url.QueryEscape(fileID))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to call Bot API: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var apiResp BotAPIFileResponse
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	if !apiResp.OK {
		return nil, fmt.Errorf("Bot API error %d: %s", apiResp.ErrorCode, apiResp.Description)
	}

	return &apiResp.Result, nil
}

// GetBotAPICDNURL constructs the full CDN download URL for a file.
//
// The URL format is: https://api.telegram.org/file/bot<token>/<file_path>
//
// This URL points directly to Telegram's CDN and provides fast downloads
// without going through your server.
//
// Parameters:
//   - token: Bot token
//   - filePath: The file_path from getFile response
//
// Returns:
//   - Full CDN URL for direct file download
func GetBotAPICDNURL(token, filePath string) string {
	return fmt.Sprintf("%s/file/bot%s/%s", BotAPIBaseURL, token, filePath)
}

// GetCachedBotAPIFilePath retrieves file_path from cache or fetches from Bot API.
//
// This function implements caching to avoid repeated getFile calls.
// The cache TTL is 50 minutes (Bot API guarantees 1 hour validity).
//
// Cache key format: "botapi:file:{fileID}"
//
// Parameters:
//   - ctx: Context for cancellation
//   - c: Cache instance
//   - token: Bot token
//   - fileID: Bot API file_id string
//
// Returns:
//   - file_path string for CDN URL construction
//   - error if fetch fails
func GetCachedBotAPIFilePath(ctx context.Context, c cache.Cacher, token, fileID string) (string, error) {
	cacheKey := cache.Key("botapi", "file", fileID)

	filePath, err := cache.Fetch(c, cacheKey, BotAPIFileCacheTTL, func() (string, error) {
		file, err := GetBotAPIFilePath(ctx, token, fileID)
		if err != nil {
			return "", err
		}
		return file.FilePath, nil
	})

	return filePath, err
}

// DocumentToCDNURL is a convenience function that converts a tg.Document directly to a CDN URL.
//
// This combines EncodeFileID, GetCachedBotAPIFilePath, and GetBotAPICDNURL into a single call.
//
// Parameters:
//   - ctx: Context for cancellation
//   - c: Cache instance
//   - token: Bot token
//   - doc: Telegram document from message
//
// Returns:
//   - Full CDN URL for direct file download
//   - error if any step fails
//
// Example:
//
//	cdnURL, err := DocumentToCDNURL(ctx, cache, botToken, document)
//	if err != nil {
//	    return err
//	}
//	// Redirect client to cdnURL for fast download
//	http.Redirect(w, r, cdnURL, http.StatusFound)
func DocumentToCDNURL(ctx context.Context, c cache.Cacher, token string, doc *tg.Document) (string, error) {
	fileID, err := EncodeFileID(doc)
	if err != nil {
		return "", fmt.Errorf("failed to encode file_id: %w", err)
	}

	filePath, err := GetCachedBotAPIFilePath(ctx, c, token, fileID)
	if err != nil {
		return "", fmt.Errorf("failed to get file_path: %w", err)
	}

	return GetBotAPICDNURL(token, filePath), nil
}

// GetCachedViewURL retrieves or generates a CDN URL for a teldrive file.
//
// This function provides two-level caching:
// 1. First level: Cache by teldrive file ID (avoids MTProto calls entirely)
// 2. Second level: Cache by Bot API file_id (avoids getFile calls)
//
// For image viewing, this dramatically reduces latency since most requests
// will hit the first-level cache and skip MTProto entirely.
//
// Cache key format: "view:cdn:{teldriveFileId}"
// TTL: 50 minutes (Telegram guarantees 1 hour validity)
//
// Note: The CDN URL contains the bot token that was used to generate it.
// On cache hit, the cached URL is returned regardless of which token is
// passed in. This is fine because all bots in the same channel can access
// the same files, and the file_path remains valid.
//
// Parameters:
//   - ctx: Context for cancellation
//   - c: Cache instance
//   - teldriveFileId: The teldrive file ID (UUID)
//   - token: Bot token (used only on cache miss)
//   - fetchDocument: Function to fetch tg.Document if cache miss
//
// Returns:
//   - Full CDN URL for direct file download
//   - error if fetch fails
func GetCachedViewURL(ctx context.Context, c cache.Cacher, teldriveFileId string, token string, fetchDocument func() (*tg.Document, error)) (string, error) {
	cacheKey := cache.Key("view", "cdn", teldriveFileId)

	cdnURL, err := cache.Fetch(c, cacheKey, BotAPIFileCacheTTL, func() (string, error) {
		// Cache miss - need to fetch document and get CDN URL
		doc, err := fetchDocument()
		if err != nil {
			return "", err
		}

		url, err := DocumentToCDNURL(ctx, c, token, doc)
		if err != nil {
			return "", err
		}

		return url, nil
	})

	return cdnURL, err
}
