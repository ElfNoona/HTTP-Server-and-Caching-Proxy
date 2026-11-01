package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// ComputeETag computes a strong entity tag for the given payload using a SHA-256 hash.
// It returns an RFC-compliant quoted hex string.
func ComputeETag(body []byte) string {
	hash := sha256.Sum256(body)
	hexStr := hex.EncodeToString(hash[:])
	return fmt.Sprintf("\"%s\"", hexStr[:16])
}

// ParseCacheControl extracts max-age, no-cache, and no-store directives from a Cache-Control header.
func ParseCacheControl(headerVal string) (maxAge time.Duration, noCache bool, noStore bool) {
	if headerVal == "" {
		return 0, false, false
	}

	parts := strings.Split(headerVal, ",")
	for _, part := range parts {
		token := strings.TrimSpace(part)
		tokenLower := strings.ToLower(token)

		switch {
		case tokenLower == "no-cache":
			noCache = true
		case tokenLower == "no-store":
			noStore = true
		case strings.HasPrefix(tokenLower, "max-age="):
			valStr := strings.TrimSpace(token[8:])
			if sec, err := strconv.ParseInt(valStr, 10, 64); err == nil && sec >= 0 {
				maxAge = time.Duration(sec) * time.Second
			}
		}
	}

	return maxAge, noCache, noStore
}

// getHeaderCaseInsensitive searches a header map case-insensitively.
func getHeaderCaseInsensitive(headers map[string]string, key string) string {
	for k, v := range headers {
		if strings.EqualFold(k, key) {
			return v
		}
	}
	return ""
}

// IsCacheable determines whether a response qualifies for in-memory caching under RFC 9111.
func IsCacheable(method string, statusCode int, headers map[string]string) bool {
	// Only GET and HEAD methods are cacheable
	if method != "GET" && method != "HEAD" {
		return false
	}

	// Only 200 OK responses are cacheable
	if statusCode != 200 {
		return false
	}

	// Check for Cache-Control: no-store
	ccVal := getHeaderCaseInsensitive(headers, "Cache-Control")
	if ccVal != "" {
		_, _, noStore := ParseCacheControl(ccVal)
		if noStore {
			return false
		}
	}

	return true
}

// CheckConditionalValidation tests if a client's conditional headers match the cached entry.
// Returns true if the client can be served a 304 Not Modified response.
func CheckConditionalValidation(clientHeaders map[string]string, entry *CacheEntry) bool {
	if entry == nil {
		return false
	}

	// 1. If-None-Match (ETag validation)
	inm := getHeaderCaseInsensitive(clientHeaders, "If-None-Match")
	if inm != "" {
		cleanClient := strings.Trim(strings.TrimSpace(inm), "\"")
		cleanEntry := strings.Trim(strings.TrimSpace(entry.ETag), "\"")

		if cleanClient == "*" || cleanClient == cleanEntry || strings.TrimSpace(inm) == entry.ETag {
			return true
		}
		// When If-None-Match is present, it takes precedence over If-Modified-Since
		return false
	}

	// 2. If-Modified-Since (Timestamp validation)
	ims := getHeaderCaseInsensitive(clientHeaders, "If-Modified-Since")
	if ims != "" && entry.LastModified != "" {
		imsStr := strings.TrimSpace(ims)
		entryLM := strings.TrimSpace(entry.LastModified)

		if imsStr == entryLM {
			return true
		}

		// Parse timestamps per RFC standards
		clientTime, errClient := time.Parse(time.RFC1123, imsStr)
		entryTime, errEntry := time.Parse(time.RFC1123, entryLM)
		if errClient == nil && errEntry == nil {
			// If origin resource is not newer than client's cached copy, return 304
			if !entryTime.After(clientTime) {
				return true
			}
		}
	}

	return false
}

