package basispoints

import (
	"reflect"
	"testing"
)

func TestRegistrationUsesReferenceAuthModelScope(t *testing.T) {
	capabilities := registration(defaultConfig())["capabilities"].(map[string]any)
	if capabilities["model_provider"] != true || capabilities["executor"] != true || capabilities["auth_provider"] != true {
		t.Fatalf("routing capabilities = %#v", capabilities)
	}
	if capabilities["model_router"] == true {
		t.Fatalf("reference auth plugin must not register a static model router: %#v", capabilities)
	}
	if capabilities["executor_model_scope"] != "oauth" {
		t.Fatalf("executor scope = %#v, want oauth", capabilities["executor_model_scope"])
	}
}

func TestConfigEmptyAuthAllowlistDoesNotSelectAnyCredential(t *testing.T) {
	cfg := defaultConfig()
	if cfg.includesSourceAuthID("codex-pro.json") {
		t.Fatal("empty include_auth_ids must not select a source credential")
	}
	if err := cfg.normalize(); err != nil {
		t.Fatal(err)
	}
	if len(cfg.IncludeAuthIDs) != 0 {
		t.Fatalf("IncludeAuthIDs = %#v, want empty", cfg.IncludeAuthIDs)
	}
}

func TestConfigNormalizesAuthAllowlist(t *testing.T) {
	cfg := defaultConfig()
	cfg.IncludeAuthIDs = []string{"  codex-pro.json  ", "codex-pro.json", "codex-plus.json"}
	if err := cfg.normalize(); err != nil {
		t.Fatal(err)
	}
	want := []string{"codex-pro.json", "codex-plus.json"}
	if !reflect.DeepEqual(cfg.IncludeAuthIDs, want) {
		t.Fatalf("IncludeAuthIDs = %#v, want %#v", cfg.IncludeAuthIDs, want)
	}
}

func TestModelRegistrationAdvertisesImagesAndReasoningLevels(t *testing.T) {
	response := modelRegistration(defaultConfig())
	for _, model := range response["Models"].([]map[string]any) {
		if !reflect.DeepEqual(model["SupportedInputModalities"], []string{"text", "image"}) {
			t.Fatal("image modality missing")
		}
		thinking := objectValue(model["Thinking"])
		if !reflect.DeepEqual(thinking["Levels"], []string{"low", "medium", "high", "xhigh", "max", "ultra"}) {
			t.Fatal("reasoning levels missing")
		}
	}
}

func TestPrepareRequestPreservesRemoteImagesAndReasoningEffort(t *testing.T) {
	images := []string{"https://example.com/image.png"}
	efforts := []struct{ input, want string }{
		{"low", "low"}, {"medium", "medium"}, {"high", "high"}, {"xhigh", "xhigh"},
		{"max", "xhigh"}, {"ultra", "ultra"}, {" MAX ", "xhigh"}, {"", "medium"},
	}
	for _, image := range images {
		for _, effort := range efforts {
			for _, nested := range []bool{true, false} {
				content := []any{map[string]any{"type": "input_text", "text": "Describe this image"}, map[string]any{"type": "input_image", "image_url": image, "detail": "high"}}
				message := map[string]any{"role": "user", "content": content}
				source := map[string]any{"model": DefaultModelID, "input": []any{message}}
				if nested {
					source["reasoning"] = map[string]any{"effort": effort.input}
				} else {
					source["reasoning_effort"] = effort.input
				}
				raw := jsonBytes(source)
				for _, original := range []bool{true, false} {
					request := ExecutorRequest{Model: DefaultModelID, Payload: raw, StorageJSON: jsonBytes(map[string]any{"access_token": "test-access", "account_id": "test-account"})}
					if original {
						request.OriginalRequest = raw
					}
					body, _, err := NewService().prepareRequest(request)
					if err != nil {
						t.Fatal(err)
					}
					if got := body["reasoning_effort"]; got != effort.want {
						t.Fatalf("effort %q = %v, want %s", effort.input, got, effort.want)
					}
					items := body["input"].([]any)
					if !reflect.DeepEqual(items[len(items)-1], message) {
						t.Fatal("image input changed during request preparation")
					}
				}
			}
		}
	}
}

func TestModelRegistrationUsesExistingResponseInterceptor(t *testing.T) {
	cfg := defaultConfig()
	capabilities := registration(cfg)["capabilities"].(map[string]any)
	if capabilities["response_interceptor"] != true {
		t.Fatal("model catalog interceptor is not registered")
	}
	for _, model := range modelRegistration(cfg)["Models"].([]map[string]any) {
		for _, field := range []string{"MetadataModelID", "SupportedServiceTiers", "ContextLength"} {
			if _, exists := model[field]; exists {
				t.Fatalf("unexpected host metadata dependency: %s", field)
			}
		}
	}
}
