package shared

import (
	"net/http"

	"ds2api/internal/config"
	"ds2api/internal/upstreamblocker"
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

func WriteUpstreamBlockedError(w http.ResponseWriter, err error) bool {
	matchErr, ok := upstreamblocker.AsMatchError(err)
	if !ok {
		return false
	}
	WriteOpenAIErrorWithCode(w, http.StatusForbidden, matchErr.Error(), upstreamblocker.Code)
	return true
}
