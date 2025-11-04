package main

import (
	"html"
	"strings"

	"github.com/microcosm-cc/bluemonday"
)

// Create sanitizers for different use cases
var (
	// Strict policy for nicknames - only allows basic text, no HTML
	nicknamePolicy = bluemonday.StrictPolicy()

	// Relaxed policy for messages - allows safe HTML formatting
	messagePolicy = bluemonday.UGCPolicy()
)

func init() {
	// Configure nickname policy - no HTML allowed, just text
	nicknamePolicy = bluemonday.StrictPolicy()

	// Configure message policy - allows safe HTML formatting
	messagePolicy = bluemonday.UGCPolicy().
		// Allow basic formatting
		AllowElements("b", "i", "em", "strong", "u", "s", "strike", "del", "ins").
		// Allow headings
		AllowElements("h1", "h2", "h3", "h4", "h5", "h6").
		// Allow paragraphs and line breaks
		AllowElements("p", "br").
		// Allow lists
		AllowElements("ul", "ol", "li").
		// Allow blockquote
		AllowElements("blockquote").
		// Allow code and pre
		AllowElements("code", "pre").
		// Allow links with safe protocols
		AllowURLSchemes("http", "https", "mailto").
		AllowRelativeURLs(false).
		RequireNoFollowOnLinks(true).
		// Allow images with safe protocols
		AllowURLSchemes("http", "https").
		AllowElements("img").
		AllowAttrs("src", "alt", "title", "width", "height").OnElements("img").
		// Allow tables
		AllowElements("table", "thead", "tbody", "tr", "th", "td").
		// Allow div and span with classes
		AllowElements("div", "span").
		AllowAttrs("class").OnElements("div", "span").
		// Allow text styling
		AllowAttrs("style").OnElements("span").
		// Allow colors
		AllowAttrs("color").OnElements("font", "span").
		// Allow font sizes (limited range)
		AllowAttrs("size").Matching(bluemonday.Number).OnElements("font")
}

// SanitizeNickname sanitizes a nickname, removing all HTML tags and entities
func SanitizeNickname(nickname string) string {
	if nickname == "" {
		return "anon"
	}

	// First decode HTML entities
	decoded := html.UnescapeString(nickname)

	// Remove all HTML tags using strict policy
	sanitized := nicknamePolicy.Sanitize(decoded)

	// Trim whitespace and limit length
	sanitized = strings.TrimSpace(sanitized)
	if len(sanitized) > 24 {
		sanitized = sanitized[:24]
	}

	// If empty after sanitization, use "anon"
	if sanitized == "" {
		sanitized = "anon"
	}

	return sanitized
}

// SanitizeMessage sanitizes a message, allowing safe HTML formatting
func SanitizeMessage(message string) string {
	if message == "" {
		return ""
	}

	// First decode HTML entities
	decoded := html.UnescapeString(message)

	// Sanitize with relaxed policy that allows safe HTML
	sanitized := messagePolicy.Sanitize(decoded)

	// Trim whitespace
	sanitized = strings.TrimSpace(sanitized)

	return sanitized
}

// EscapeHTMLForDisplay escapes HTML for display in contexts where we want to show the raw HTML
func EscapeHTMLForDisplay(text string) string {
	return html.EscapeString(text)
}
