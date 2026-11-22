package provider

import (
	"fmt"

	"github.com/GovnocoderAI/kern/internal/config"
)

// FromConfig builds a provider implementation from a resolved config entry.
func FromConfig(providerID string, p *config.Provider, apiKey string) (Provider, error) {
	if p == nil {
		return nil, fmt.Errorf("provider %q is not configured", providerID)
	}
	switch p.Kind() {
	case "openai":
		return &OpenAI{
			ProviderID: providerID,
			BaseURL:    p.Endpoint(),
			APIKey:     apiKey,
			Headers:    p.Headers(),
		}, nil
	case "anthropic":
		return &Anthropic{
			ProviderID: providerID,
			BaseURL:    p.Endpoint(),
			APIKey:     apiKey,
			Headers:    p.Headers(),
		}, nil
	case "google":
		return nil, fmt.Errorf("provider %q: Google Gemini is not implemented yet", providerID)
	default:
		return nil, fmt.Errorf("provider %q: unsupported kind %q", providerID, p.Kind())
	}
}
