package shared

// MaskAPIKey masks an API key for display, showing only the first 4 and last 4 characters.
func MaskAPIKey(key string) string {
	if len(key) <= 8 {
		return "****"
	}
	return key[:4] + "..." + key[len(key)-4:]
}
