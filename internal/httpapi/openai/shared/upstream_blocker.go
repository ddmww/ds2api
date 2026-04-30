package shared

import (
	"net/http"

	"ds2api/internal/config"
	"ds2api/internal/upstreamblocker"
	"ds2api/internal/util"
)

type upstreamBlockerConfigReader interface {
	UpstreamBlockerConfig() config.UpstreamBlockerConfig
}

func UpstreamBlockerConfig(store ConfigReader) config.UpstreamBlockerConfig {
	reader, ok := store.(upstreamBlockerConfigReader)
	if !ok || reader == nil {
		return config.UpstreamBlockerConfig{}
	}
	return reader.UpstreamBlockerConfig()
}

func AssertUpstreamAllowed(store ConfigReader, candidate string) error {
	return upstreamblocker.AssertAllowed(UpstreamBlockerConfig(store), candidate)
}

func StreamUpstreamBlockerBufferTokens(store ConfigReader) int {
	cfg := UpstreamBlockerConfig(store)
	if !cfg.Enabled || cfg.StreamBufferTokens <= 0 {
		return 0
	}
	return cfg.StreamBufferTokens
}

func EstimateStreamBlockerTokens(model, candidate string) int {
	return util.EstimateTokensByModel(model, candidate)
}

func WriteUpstreamBlockedError(w http.ResponseWriter, err error) bool {
	matchErr, ok := upstreamblocker.AsMatchError(err)
	if !ok {
		return false
	}
	WriteOpenAIErrorWithCode(w, http.StatusForbidden, matchErr.Error(), upstreamblocker.Code)
	return true
}
