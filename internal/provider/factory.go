package provider

import (
	"fmt"
	"time"

	"github.com/GovnocoderAI/kern/internal/config"
)

var (
	ms  = time.Millisecond
	sec = time.Second
)

// FromConfig builds a provider implementation from a resolved config entry,
// wrapped with the default retry middleware so transient provider failures
// (429, 5xx, network blips) recover without the caller noticing.
func FromConfig(providerID string, p *config.Provider, apiKey string) (Provider, error) {
	if p == nil {
		return nil, fmt.Errorf("provider %q is not configured", providerID)
	}
	var base Provider
	switch p.Kind() {
	case "openai":
		base = &OpenAI{
			ProviderID: providerID,
			BaseURL:    p.Endpoint(),
			APIKey:     apiKey,
			Headers:    p.Headers(),
		}
	case "anthropic":
		base = &Anthropic{
			ProviderID: providerID,
			BaseURL:    p.Endpoint(),
			APIKey:     apiKey,
			Headers:    p.Headers(),
		}
	case "google":
		base = &Gemini{
			ProviderID: providerID,
			BaseURL:    p.Endpoint(),
			APIKey:     apiKey,
			Headers:    p.Headers(),
		}
	default:
		return nil, fmt.Errorf("provider %q: unsupported kind %q", providerID, p.Kind())
	}
	return Apply(base,
		Retry(RetryPolicy{MaxAttempts: 4, Initial: 500 * ms, Max: 20 * sec}),
		Timeout(90*sec),
	), nil
}
