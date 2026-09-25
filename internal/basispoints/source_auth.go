package basispoints

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"time"
)

type hostAuthListResponse struct {
	Files []hostAuthFile `json:"files"`
}

type hostAuthFile struct {
	ID          string `json:"id"`
	AuthIndex   string `json:"auth_index"`
	Name        string `json:"name"`
	Type        string `json:"type"`
	Provider    string `json:"provider"`
	Status      string `json:"status"`
	Disabled    bool   `json:"disabled"`
	Unavailable bool   `json:"unavailable"`
}

type hostAuthGetRequest struct {
	AuthIndex string `json:"auth_index"`
}

type hostAuthGetResponse struct {
	AuthIndex string          `json:"auth_index"`
	JSON      json.RawMessage `json:"json"`
}

type hostAuthSaveRequest struct {
	Name string          `json:"name"`
	JSON json.RawMessage `json:"json"`
}

type hostAuthSaveResponse struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

type sourceAuth struct {
	RawJSON []byte
}

type modelForAuthRequest struct {
	AuthID       string         `json:"AuthID"`
	AuthProvider string         `json:"AuthProvider"`
	StorageJSON  []byte         `json:"StorageJSON"`
	Metadata     map[string]any `json:"Metadata"`
}

// ensureSourceReferences creates one plugin-owned, token-free file per
// explicitly allowed physical Codex file. The deterministic name makes this
// idempotent across plugin restarts and configuration reloads.
func (s *Service) ensureSourceReferences(cfg Config) error {
	if len(cfg.IncludeAuthIDs) == 0 || !s.hasHost() {
		return nil
	}

	var listed hostAuthListResponse
	if err := s.call("host.auth.list", map[string]any{}, &listed); err != nil {
		return fail(502, "source_auth_lookup_failed", "CPA source credential lookup failed while creating Basis Points references")
	}
	for _, sourceAuthID := range cfg.IncludeAuthIDs {
		if !hasCodexSource(listed.Files, sourceAuthID) {
			// Plugin registration can precede CPA's first auth-store load. Leave
			// this source for the next reload instead of failing all plugin setup.
			continue
		}
		referenceName := sourceReferenceFileName(sourceAuthID)
		if hasAuthFile(listed.Files, referenceName) {
			continue
		}
		var saved hostAuthSaveResponse
		if err := s.call("host.auth.save", hostAuthSaveRequest{
			Name: referenceName,
			JSON: referenceAuthJSON(sourceAuthID),
		}, &saved); err != nil {
			return fail(502, "source_reference_save_failed", "CPA could not create a Basis Points source reference")
		}
	}
	return nil
}

func (s *Service) hasHost() bool {
	s.mu.RLock()
	host := s.host
	s.mu.RUnlock()
	return host != nil
}

// attachSourceCredential resolves the selected reference into the current
// physical OAuth JSON for this one execution. AuthID intentionally remains
// the reference AuthID so CPA and KEEPER retain per-source Basis Points usage.
func (s *Service) attachSourceCredential(request *ExecutorRequest) error {
	if request == nil {
		return fail(400, "invalid_request", "executor request is missing")
	}
	storageJSON := request.StorageJSON
	if len(storageJSON) == 0 && len(request.AuthMetadata) > 0 {
		storageJSON = jsonBytes(request.AuthMetadata)
	}
	sourceAuthID, matched, err := referenceSourceAuthID(storageJSON)
	if err != nil {
		return err
	}
	if !matched {
		// Kept solely for direct unit callers that pass a physical OAuth JSON.
		// A CPA-routed Basis Points call must always use a reference auth.
		if strings.TrimSpace(request.AuthProvider) == "" && len(storageJSON) > 0 {
			return nil
		}
		return fail(401, "invalid_reference_auth", "Basis Points requests require a configured source reference")
	}
	if !s.config().includesSourceAuthID(sourceAuthID) {
		return fail(403, "source_auth_not_allowed", "Basis Points source reference is not in include_auth_ids")
	}
	if request.AuthID != "" && !isManagedReferenceFile(request.AuthID, sourceAuthID) {
		return fail(403, "invalid_reference_auth", "Basis Points reference file name is not managed by this plugin")
	}
	source, err := s.loadSourceAuth(sourceAuthID)
	if err != nil {
		return err
	}
	request.StorageJSON = source.RawJSON
	return nil
}

func (s *Service) loadSourceAuth(sourceAuthID string) (sourceAuth, error) {
	var listed hostAuthListResponse
	if err := s.call("host.auth.list", map[string]any{}, &listed); err != nil {
		return sourceAuth{}, fail(502, "source_auth_lookup_failed", "CPA source credential lookup failed")
	}
	for _, candidate := range listed.Files {
		if !sourceMatches(candidate, sourceAuthID) || !isUsableCodexSource(candidate) {
			continue
		}
		var fetched hostAuthGetResponse
		if err := s.call("host.auth.get", hostAuthGetRequest{AuthIndex: candidate.AuthIndex}, &fetched); err != nil {
			continue
		}
		raw := append([]byte(nil), fetched.JSON...)
		credential, err := parseCredential(raw)
		if err != nil || (!credential.ExpiresAt.IsZero() && !time.Now().Before(credential.ExpiresAt)) {
			continue
		}
		return sourceAuth{RawJSON: raw}, nil
	}
	return sourceAuth{}, fail(503, "source_auth_unavailable", "No configured Basis Points source OAuth credential is usable")
}

func modelsForReferenceAuth(raw []byte, cfg Config) (map[string]any, error) {
	var request modelForAuthRequest
	if err := json.Unmarshal(raw, &request); err != nil {
		return nil, fail(400, "invalid_request", "Basis Points model registration request is invalid")
	}
	if provider := strings.TrimSpace(request.AuthProvider); provider != "" && !strings.EqualFold(provider, Provider) {
		return emptyModelRegistration(), nil
	}
	storageJSON := request.StorageJSON
	if len(storageJSON) == 0 && len(request.Metadata) > 0 {
		storageJSON = jsonBytes(request.Metadata)
	}
	sourceAuthID, matched, err := referenceSourceAuthID(storageJSON)
	if err != nil || !matched || referenceDisabled(storageJSON, cfg, sourceAuthID) || (request.AuthID != "" && !isManagedReferenceFile(request.AuthID, sourceAuthID)) {
		return emptyModelRegistration(), nil
	}
	return modelRegistration(cfg), nil
}

func emptyModelRegistration() map[string]any {
	return map[string]any{"Provider": Provider, "Models": []map[string]any{}}
}

func referenceSourceAuthID(raw []byte) (sourceAuthID string, matched bool, err error) {
	if len(strings.TrimSpace(string(raw))) == 0 {
		return "", false, nil
	}
	var metadata map[string]any
	if err := json.Unmarshal(raw, &metadata); err != nil || metadata == nil {
		return "", false, fail(400, "invalid_reference_auth", "Basis Points reference is not valid JSON")
	}
	if !strings.EqualFold(stringValue(metadata["type"]), Provider) {
		return "", false, nil
	}
	sourceAuthID = strings.TrimSpace(stringValue(metadata[referenceSourceAuthIDKey]))
	if sourceAuthID == "" {
		return "", true, fail(400, "invalid_reference_auth", "Basis Points reference has no source_auth_id")
	}
	return sourceAuthID, true, nil
}

func referenceAuthJSON(sourceAuthID string) json.RawMessage {
	return jsonBytes(map[string]any{
		"type":                   Provider,
		"auth_kind":              "oauth",
		referenceSourceAuthIDKey: sourceAuthID,
		"note":                   "Basis Points source: " + sourceAuthID,
	})
}

func sourceReferenceFileName(sourceAuthID string) string {
	sum := sha256.Sum256([]byte(sourceAuthID))
	return "oai-basispoints-ref-" + hex.EncodeToString(sum[:16]) + ".json"
}

func isManagedReferenceFile(fileName, sourceAuthID string) bool {
	return strings.EqualFold(strings.TrimSpace(fileName), sourceReferenceFileName(sourceAuthID))
}

func hasAuthFile(entries []hostAuthFile, name string) bool {
	for _, entry := range entries {
		if entry.Name == name || entry.ID == name {
			return true
		}
	}
	return false
}

func hasCodexSource(entries []hostAuthFile, sourceAuthID string) bool {
	for _, entry := range entries {
		if sourceMatches(entry, sourceAuthID) && isCodexSource(entry) {
			return true
		}
	}
	return false
}

func sourceMatches(entry hostAuthFile, sourceAuthID string) bool {
	return entry.ID == sourceAuthID || entry.Name == sourceAuthID
}

func isCodexSource(entry hostAuthFile) bool {
	return strings.EqualFold(strings.TrimSpace(entry.Provider), AuthProviderID) || strings.EqualFold(strings.TrimSpace(entry.Type), AuthProviderID)
}

func isUsableCodexSource(entry hostAuthFile) bool {
	return strings.TrimSpace(entry.AuthIndex) != "" && !entry.Disabled && !entry.Unavailable && isCodexSource(entry)
}
