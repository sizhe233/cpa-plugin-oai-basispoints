package basispoints

import (
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"testing"
)

func TestEnsureSourceReferencesCreatesOnlyWhitelistedCodexReferences(t *testing.T) {
	cfg := defaultConfig()
	cfg.DataDir = ""
	cfg.IncludeAuthIDs = []string{"paid-a.json", "paid-b.json"}
	service := NewService()
	var saved []hostAuthSaveRequest
	service.SetHost(func(method string, payload any, out any) error {
		switch method {
		case "host.auth.list":
			*out.(*hostAuthListResponse) = hostAuthListResponse{Files: []hostAuthFile{
				{ID: "free.json", Name: "free.json", Provider: AuthProviderID},
				{ID: "paid-a.json", Name: "paid-a.json", Provider: AuthProviderID},
				{ID: "paid-b.json", Name: "paid-b.json", Type: AuthProviderID},
				{Name: sourceReferenceFileName("paid-b.json"), Provider: Provider},
			}}
			return nil
		case "host.auth.save":
			req, ok := payload.(hostAuthSaveRequest)
			if !ok {
				return errors.New("unexpected host.auth.save payload")
			}
			saved = append(saved, req)
			*out.(*hostAuthSaveResponse) = hostAuthSaveResponse{Name: req.Name}
			return nil
		default:
			return errors.New("unexpected host callback: " + method)
		}
	})

	if err := service.ensureSourceReferences(cfg); err != nil {
		t.Fatal(err)
	}
	if len(saved) != 1 || saved[0].Name != sourceReferenceFileName("paid-a.json") {
		t.Fatalf("saved references = %#v", saved)
	}
	var reference map[string]any
	if err := json.Unmarshal(saved[0].JSON, &reference); err != nil {
		t.Fatal(err)
	}
	if reference["type"] != Provider || reference[referenceSourceAuthIDKey] != "paid-a.json" || reference["auth_kind"] != "oauth" {
		t.Fatalf("reference JSON = %#v", reference)
	}
	for _, key := range []string{"access_token", "refresh_token", "id_token"} {
		if _, exists := reference[key]; exists {
			t.Fatalf("reference unexpectedly contains %s", key)
		}
	}
}

func TestReferenceExecutorUsesSelectedSourceAndKeepsReferenceAuthID(t *testing.T) {
	cfg := defaultConfig()
	cfg.DataDir = ""
	cfg.IncludeAuthIDs = []string{"paid-a.json", "paid-b.json"}
	service := NewService()
	credentials := map[string][]byte{
		"paid-a": jsonBytes(map[string]any{"type": "codex", "access_token": "test-token-a", "account_id": "account-a"}),
		"paid-b": jsonBytes(map[string]any{"type": "codex", "access_token": "test-token-b", "account_id": "account-b"}),
	}
	var fetched []string
	var upstreamAccounts []string
	service.SetHost(func(method string, payload any, out any) error {
		switch method {
		case "host.auth.list":
			*out.(*hostAuthListResponse) = hostAuthListResponse{Files: []hostAuthFile{
				{ID: "paid-a.json", Name: "paid-a.json", AuthIndex: "paid-a", Provider: AuthProviderID},
				{ID: "paid-b.json", Name: "paid-b.json", AuthIndex: "paid-b", Provider: AuthProviderID},
			}}
			return nil
		case "host.auth.save":
			req := payload.(hostAuthSaveRequest)
			*out.(*hostAuthSaveResponse) = hostAuthSaveResponse{Name: req.Name}
			return nil
		case "host.auth.get":
			req, ok := payload.(hostAuthGetRequest)
			if !ok {
				return errors.New("unexpected host.auth.get payload")
			}
			fetched = append(fetched, req.AuthIndex)
			raw, ok := credentials[req.AuthIndex]
			if !ok {
				return errors.New("unexpected source credential lookup")
			}
			*out.(*hostAuthGetResponse) = hostAuthGetResponse{AuthIndex: req.AuthIndex, JSON: raw}
			return nil
		case "host.http.do":
			request := objectValue(payload.(map[string]any))
			headers := request["headers"].(http.Header)
			upstreamAccounts = append(upstreamAccounts, headers["ChatGPT-Account-ID"][0])
			response := out.(*upstreamResponse)
			response.StatusCode = http.StatusOK
			response.Headers = http.Header{"Content-Type": {"application/json"}}
			response.Body = jsonBytes(map[string]any{
				"id": "resp-source-auth", "status": "completed",
				"output": []any{map[string]any{"type": "message", "role": "assistant", "content": []any{map[string]any{"type": "output_text", "text": "ok"}}}},
			})
			return nil
		default:
			return errors.New("unexpected host callback: " + method)
		}
	})
	configureForTest(t, service, cfg)

	referenceAuthID := sourceReferenceFileName("paid-b.json")
	request := ExecutorRequest{
		AuthID:          referenceAuthID,
		AuthProvider:    Provider,
		StorageJSON:     referenceAuthJSON("paid-b.json"),
		Model:           DefaultModelID,
		OriginalRequest: jsonBytes(map[string]any{"model": DefaultModelID, "input": "hello"}),
		Payload:         jsonBytes(map[string]any{"model": DefaultModelID, "input": "hello"}),
	}
	if err := service.attachSourceCredential(&request); err != nil {
		t.Fatal(err)
	}
	if request.AuthID != referenceAuthID {
		t.Fatalf("AuthID = %q, want reference auth ID %q", request.AuthID, referenceAuthID)
	}
	if !reflect.DeepEqual(request.StorageJSON, credentials["paid-b"]) {
		t.Fatalf("resolved storage = %s, want selected source", request.StorageJSON)
	}

	request.StorageJSON = referenceAuthJSON("paid-b.json")
	if _, err := service.Handle("executor.execute", jsonBytes(request)); err != nil {
		t.Fatal(err)
	}
	if want := []string{"paid-b", "paid-b"}; !reflect.DeepEqual(fetched, want) {
		t.Fatalf("fetched source credentials = %#v, want %#v", fetched, want)
	}
	if want := []string{"account-b"}; !reflect.DeepEqual(upstreamAccounts, want) {
		t.Fatalf("upstream source accounts = %#v, want %#v", upstreamAccounts, want)
	}
}

func TestReferenceExecutorRejectsUnconfiguredSource(t *testing.T) {
	service := NewService()
	err := service.attachSourceCredential(&ExecutorRequest{
		AuthProvider: Provider,
		StorageJSON:  referenceAuthJSON("paid-a.json"),
	})
	apiErr, ok := err.(*APIError)
	if !ok || apiErr.Status != http.StatusForbidden || apiErr.Kind != "source_auth_not_allowed" {
		t.Fatalf("error = %#v", err)
	}
}

func TestModelsForReferenceAuthRequiresAnAllowedReference(t *testing.T) {
	cfg := defaultConfig()
	cfg.IncludeAuthIDs = []string{"paid-a.json"}
	allowed, err := modelsForReferenceAuth(jsonBytes(modelForAuthRequest{AuthID: sourceReferenceFileName("paid-a.json"), AuthProvider: Provider, StorageJSON: referenceAuthJSON("paid-a.json")}), cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(allowed["Models"].([]map[string]any)) == 0 {
		t.Fatal("allowed reference did not receive Basis Points models")
	}

	blocked, err := modelsForReferenceAuth(jsonBytes(modelForAuthRequest{AuthID: sourceReferenceFileName("free.json"), AuthProvider: Provider, StorageJSON: referenceAuthJSON("free.json")}), cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(blocked["Models"].([]map[string]any)) != 0 {
		t.Fatalf("unconfigured reference received models: %#v", blocked)
	}

	unmanaged, err := modelsForReferenceAuth(jsonBytes(modelForAuthRequest{AuthID: "manual-basispoints.json", AuthProvider: Provider, StorageJSON: referenceAuthJSON("paid-a.json")}), cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(unmanaged["Models"].([]map[string]any)) != 0 {
		t.Fatalf("unmanaged reference received models: %#v", unmanaged)
	}
}
