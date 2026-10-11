package model

// ProviderFailureMessage is shared by HTTP replies, activity logs and history.
// Only fixed classifications may cross those boundaries, never provider text.
func ProviderFailureMessage(code string) string {
	switch code {
	case "authentication":
		return "The provider refused the credential. Check the service key."
	case "rate_limit":
		return "The provider request rate limit was reached. Wait before trying again."
	case "account_limit":
		return "The provider account reached its torrent or download limit. Free capacity or wait for the limit to reset."
	case "subscription":
		return "The provider requires an active subscription or a different plan for this operation."
	case "provider_unavailable":
		return "The provider is temporarily unavailable. Try again later."
	case "invalid_torrent":
		return "The provider rejected the torrent."
	case "file_too_large":
		return "The file exceeds the provider or account size limit."
	case "not_found":
		return "The requested torrent or file is no longer available at the provider."
	case "timeout":
		return "The provider request timed out. Try again later."
	default:
		return "The provider could not complete the request. Try again later."
	}
}
