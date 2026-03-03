package handlers

import "github.com/gin-gonic/gin"

// extractPublicKey reads the client's crypt4gh public key from request headers.
// Exactly one of X-C4GH-Public-Key or Htsget-Context-Public-Key must be present.
// Returns the raw header value (base64-encoded string) or ("", errorCode, detail).
func extractPublicKey(c *gin.Context) (string, string, string) {
	primary := c.GetHeader("X-C4GH-Public-Key")
	secondary := c.GetHeader("Htsget-Context-Public-Key")

	switch {
	case primary != "" && secondary != "":
		return "", "KEY_CONFLICT", "only one of X-C4GH-Public-Key or Htsget-Context-Public-Key may be provided"
	case primary != "":
		return primary, "", ""
	case secondary != "":
		return secondary, "", ""
	default:
		return "", "KEY_MISSING", "X-C4GH-Public-Key or Htsget-Context-Public-Key header is required"
	}
}
