package basispoints

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

const referenceSourceAuthIDKey = "source_auth_id"

type authParseRequest struct {
	Provider string `json:"Provider"`
	Path     string `json:"Path"`
	FileName string `json:"FileName"`
	RawJSON  []byte `json:"RawJSON"`
}

type authRefreshRequest struct {
	AuthID      string         `json:"AuthID"`
	StorageJSON []byte         `json:"StorageJSON"`
	Metadata    map[string]any `json:"Metadata"`
}

type credential struct {
	AccessToken string
	AccountID   string
	AuthMode    string
	Email       string
	ExpiresAt   time.Time
}

// authParse owns only plugin-created reference files. It never claims a
// type: codex file, so CPA retains normal ownership of the OAuth source file.
func authParse(raw []byte, cfg Config) (map[string]any, error) {
	var request authParseRequest
	if err := json.Unmarshal(raw, &request); err != nil {
		return nil, fail(400, "invalid_auth", "Basis Points auth parse request is invalid")
	}
	provider := strings.ToLower(strings.TrimSpace(request.Provider))
	if provider != "" && provider != Provider {
		return map[string]any{"Handled": false}, nil
	}

	sourceAuthID, matched, err := referenceSourceAuthID(request.RawJSON)
	if err != nil {
		return nil, err
	}
	if !matched {
		return map[string]any{"Handled": false}, nil
	}
	fileName := strings.TrimSpace(request.FileName)
	if fileName == "" {
		fileName = filepath.Base(strings.TrimSpace(request.Path))
	}
	if fileName == "" {
		fileName = sourceReferenceFileName(sourceAuthID)
	}
	return map[string]any{
		"Handled": true,
		"Auth":    referenceAuthData(request.RawJSON, fileName, sourceAuthID, referenceDisabled(request.RawJSON, cfg, sourceAuthID) || !isManagedReferenceFile(fileName, sourceAuthID)),
	}, nil
}

// authRefresh deliberately does not refresh the source OAuth token. CPA's
// built-in Codex provider owns that lifecycle; this only preserves the small
// non-secret reference record.
func authRefresh(raw []byte, cfg Config) (map[string]any, error) {
	var request authRefreshRequest
	if err := json.Unmarshal(raw, &request); err != nil {
		return nil, fail(400, "invalid_auth", "Basis Points auth refresh request is invalid")
	}
	sourceAuthID, matched, err := referenceSourceAuthID(request.StorageJSON)
	if err != nil {
		return nil, err
	}
	if !matched {
		return nil, fail(400, "invalid_reference_auth", "Basis Points auth refresh requires a source_auth_id reference")
	}
	fileName := filepath.Base(strings.TrimSpace(request.AuthID))
	if fileName == "" {
		fileName = sourceReferenceFileName(sourceAuthID)
	}
	next := time.Now().Add(24 * time.Hour).UTC()
	return map[string]any{
		"Auth":             referenceAuthData(request.StorageJSON, fileName, sourceAuthID, referenceDisabled(request.StorageJSON, cfg, sourceAuthID)),
		"NextRefreshAfter": next,
	}, nil
}

func referenceAuthData(raw []byte, fileName, sourceAuthID string, disabled bool) map[string]any {
	metadata := referenceMetadata(raw)
	metadata["type"] = Provider
	metadata["auth_kind"] = "oauth"
	metadata[referenceSourceAuthIDKey] = sourceAuthID
	if strings.TrimSpace(stringValue(metadata["note"])) == "" {
		metadata["note"] = "Basis Points source: " + sourceAuthID
	}
	fileName = filepath.Base(strings.TrimSpace(fileName))
	if fileName == "" {
		fileName = sourceReferenceFileName(sourceAuthID)
	}
	return map[string]any{
		"Provider":         Provider,
		"ID":               fileName,
		"FileName":         fileName,
		"Label":            "Basis Points: " + sourceAuthID,
		"Disabled":         disabled,
		"StorageJSON":      jsonBytes(metadata),
		"Metadata":         metadata,
		"Attributes":       map[string]string{"auth_kind": "oauth", referenceSourceAuthIDKey: sourceAuthID},
		"NextRefreshAfter": time.Now().Add(24 * time.Hour).UTC(),
	}
}

func referenceMetadata(raw []byte) map[string]any {
	metadata := make(map[string]any)
	_ = json.Unmarshal(raw, &metadata)
	return metadata
}

func referenceDisabled(raw []byte, cfg Config, sourceAuthID string) bool {
	metadata := referenceMetadata(raw)
	disabled, _ := metadata["disabled"].(bool)
	return disabled || !cfg.includesSourceAuthID(sourceAuthID)
}

func parseCredential(raw []byte) (credential, error) {
	var root map[string]any
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.UseNumber()
	if err := decoder.Decode(&root); err != nil || root == nil {
		return credential{}, fail(400, "invalid_auth", "OAuth credential is not valid JSON")
	}
	token := findToken(root)
	if token == "" {
		return credential{}, fail(401, "invalid_auth", "ChatGPT OAuth credential has no access_token")
	}
	claims := jwtPayload(token)
	accountID := accountIDFromClaims(claims)
	if accountID == "" {
		accountID = findAccountID(root)
	}
	if accountID == "" {
		return credential{}, fail(401, "invalid_auth", "ChatGPT OAuth credential has no account ID")
	}
	authMode := firstString(root, "auth_mode", "authMode")
	if !strings.EqualFold(authMode, "chatgpt") {
		authMode = "chatgpt"
	}
	email := firstString(root, "email")
	if email == "" {
		email = stringValue(claims["email"])
	}
	expiresAt := jwtExpiry(claims)
	if rawExpiry := firstValue(root, "expires_at", "expired"); expiresAt.IsZero() {
		expiresAt = timeFromValue(rawExpiry)
	}
	return credential{
		AccessToken: token,
		AccountID:   accountID,
		AuthMode:    authMode,
		Email:       email,
		ExpiresAt:   expiresAt,
	}, nil
}

func findToken(root map[string]any) string {
	for _, key := range []string{"access_token", "accessToken"} {
		if token := stringValue(root[key]); token != "" {
			return strings.TrimPrefix(strings.TrimSpace(token), "Bearer ")
		}
	}
	for _, key := range []string{"token_data", "tokenData", "sessionInfo", "session_info", "oauth", "tokens"} {
		if nested, ok := root[key].(map[string]any); ok {
			if token := findToken(nested); token != "" {
				return token
			}
		}
	}
	return ""
}

func findAccountID(root map[string]any) string {
	for _, key := range []string{"userInfo", "user_info", "auth", "token_data", "tokenData", "sessionInfo", "session_info"} {
		if nested, ok := root[key].(map[string]any); ok {
			if id := firstString(nested, "chatgpt_account_id", "account_id", "accountId"); id != "" {
				return id
			}
		}
	}
	return firstString(root, "chatgpt_account_id", "account_id", "accountId")
}

func firstString(object map[string]any, keys ...string) string {
	for _, key := range keys {
		if value := stringValue(object[key]); value != "" {
			return value
		}
	}
	return ""
}

func firstValue(object map[string]any, keys ...string) any {
	for _, key := range keys {
		if value, ok := object[key]; ok {
			return value
		}
	}
	return nil
}

func jwtPayload(token string) map[string]any {
	parts := strings.Split(strings.TrimSpace(token), ".")
	if len(parts) != 3 {
		return map[string]any{}
	}
	data, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		data, err = base64.URLEncoding.DecodeString(parts[1])
	}
	if err != nil {
		return map[string]any{}
	}
	var claims map[string]any
	if json.Unmarshal(data, &claims) != nil || claims == nil {
		return map[string]any{}
	}
	return claims
}

func accountIDFromClaims(claims map[string]any) string {
	if auth, ok := claims["https://api.openai.com/auth"].(map[string]any); ok {
		if accountID := firstString(auth, "chatgpt_account_id", "account_id"); accountID != "" {
			return accountID
		}
	}
	return firstString(claims, "chatgpt_account_id", "account_id")
}

func jwtExpiry(claims map[string]any) time.Time {
	if claims == nil {
		return time.Time{}
	}
	if value, ok := claims["exp"]; ok {
		return timeFromValue(value)
	}
	return time.Time{}
}

func timeFromValue(value any) time.Time {
	switch number := value.(type) {
	case json.Number:
		if seconds, err := number.Int64(); err == nil && seconds > 0 {
			return time.Unix(seconds, 0)
		}
	case float64:
		if number > 0 {
			return time.Unix(int64(number), 0)
		}
	case int64:
		if number > 0 {
			return time.Unix(number, 0)
		}
	case string:
		if timestamp, err := time.Parse(time.RFC3339, strings.TrimSpace(number)); err == nil {
			return timestamp
		}
	}
	return time.Time{}
}

func credentialFromExecutor(request ExecutorRequest) (credential, error) {
	if len(request.StorageJSON) > 0 {
		return parseCredential(request.StorageJSON)
	}
	metadata := map[string]any{}
	for key, value := range request.AuthMetadata {
		metadata[key] = value
	}
	for key, value := range request.AuthAttributes {
		metadata[key] = value
	}
	if token := stringValue(metadata["access_token"]); token != "" {
		data := map[string]any{"access_token": token}
		if accountID := stringValue(metadata["account_id"]); accountID != "" {
			data["account_id"] = accountID
		}
		return parseCredential(jsonBytes(data))
	}
	return credential{}, fail(401, "missing_auth", "CPA did not provide a ChatGPT OAuth credential")
}

func redactTokenMessage(message string) string {
	for _, prefix := range []string{"Bearer ", "bearer "} {
		if index := strings.Index(message, prefix); index >= 0 {
			end := index + len(prefix)
			for end < len(message) && message[end] != ' ' && message[end] != '"' && message[end] != '\n' {
				end++
			}
			message = message[:index] + prefix + "[REDACTED]" + message[end:]
		}
	}
	return fmt.Sprintf("%s", message)
}
