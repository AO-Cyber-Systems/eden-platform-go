package platformv1_test

import (
	"testing"

	platformv1 "go.aocyber.ai/eden-platform-go/gen/go/platform/v1"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// TestValidateApiKeyResponse_IdentityContract pins the wire contract that
// AOEdge and AOCore rely on (aoid#108, aoedge#47): a validated key carries
// the owner's global identity id and the tenant slug — the same `sub` and
// `tnt` a human access token for that person carries — alongside the
// legacy account_id / tenant_id. Field numbers are part of the contract;
// renumbering silently breaks every deployed consumer.
func TestValidateApiKeyResponse_IdentityContract(t *testing.T) {
	desc := (&platformv1.ApiKeyValidationServiceValidateApiKeyResponse{}).ProtoReflect().Descriptor()

	want := []struct {
		name   protoreflect.Name
		number protoreflect.FieldNumber
		json   string
	}{
		{"account_id", 3, "accountId"},
		{"tenant_id", 4, "tenantId"},
		{"identity_id", 7, "identityId"},
		{"tenant_slug", 8, "tenantSlug"},
	}
	for _, w := range want {
		fd := desc.Fields().ByName(w.name)
		if fd == nil {
			t.Fatalf("field %q missing", w.name)
		}
		if fd.Number() != w.number {
			t.Errorf("field %q number = %d, want %d", w.name, fd.Number(), w.number)
		}
		if fd.Kind() != protoreflect.StringKind || fd.Cardinality() != protoreflect.Optional {
			t.Errorf("field %q must be a singular string", w.name)
		}
		if fd.JSONName() != w.json {
			t.Errorf("field %q json name = %q, want %q", w.name, fd.JSONName(), w.json)
		}
	}

	in := &platformv1.ApiKeyValidationServiceValidateApiKeyResponse{
		Valid:      true,
		ApiKeyId:   "key-1",
		AccountId:  "acct-1",
		TenantId:   "6f1c2b9e-0000-4000-8000-000000000001",
		Scopes:     []string{"models:invoke"},
		IdentityId: "ident-1",
		TenantSlug: "acme",
	}
	for name, codec := range map[string]struct {
		marshal   func(proto.Message) ([]byte, error)
		unmarshal func([]byte, proto.Message) error
	}{
		"binary": {proto.Marshal, proto.Unmarshal},
		"json":   {protojson.Marshal, protojson.Unmarshal},
	} {
		b, err := codec.marshal(in)
		if err != nil {
			t.Fatalf("%s marshal: %v", name, err)
		}
		out := &platformv1.ApiKeyValidationServiceValidateApiKeyResponse{}
		if err := codec.unmarshal(b, out); err != nil {
			t.Fatalf("%s unmarshal: %v", name, err)
		}
		if !proto.Equal(in, out) {
			t.Errorf("%s round trip mismatch:\n in=%v\nout=%v", name, in, out)
		}
	}
}
