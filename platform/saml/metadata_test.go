package saml

import (
	"errors"
	"strings"
	"sync"
	"testing"
)

// A minimal valid SAML EntityDescriptor (Okta-shaped) used to exercise
// ParseAndCacheMetadata. The descriptor declares an HTTP-POST SSO
// endpoint and a single X509 cert; the contents are not load-bearing —
// only that crewjam's samlsp.ParseMetadata accepts it.
const metadataOktaXML = `<?xml version="1.0" encoding="UTF-8"?>
<md:EntityDescriptor entityID="http://www.okta.com/example" xmlns:md="urn:oasis:names:tc:SAML:2.0:metadata">
  <md:IDPSSODescriptor WantAuthnRequestsSigned="false" protocolSupportEnumeration="urn:oasis:names:tc:SAML:2.0:protocol">
    <md:KeyDescriptor use="signing">
      <ds:KeyInfo xmlns:ds="http://www.w3.org/2000/09/xmldsig#">
        <ds:X509Data>
          <ds:X509Certificate>MIIDpDCCAoygAwIBAgIGAYsR7tcgMA0GCSqGSIb3DQEBCwUAMIGSMQswCQYDVQQGEwJVUzETMBEGA1UECAwKQ2FsaWZvcm5pYTEWMBQGA1UEBwwNU2FuIEZyYW5jaXNjbzENMAsGA1UECgwET2t0YTEUMBIGA1UECwwLU1NPUHJvdmlkZXIxEzARBgNVBAMMCnRyaWFsLTAwMTExHDAaBgkqhkiG9w0BCQEWDWluZm9Ab2t0YS5jb20wHhcNMjMwNzE3MDQ1NTAxWhcNMzMwNzE3MDQ1NjAxWjCBkjELMAkGA1UEBhMCVVMxEzARBgNVBAgMCkNhbGlmb3JuaWExFjAUBgNVBAcMDVNhbiBGcmFuY2lzY28xDTALBgNVBAoMBE9rdGExFDASBgNVBAsMC1NTT1Byb3ZpZGVyMRMwEQYDVQQDDAp0cmlhbC0wMDExMRwwGgYJKoZIhvcNAQkBFg1pbmZvQG9rdGEuY29tMIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKCAQEAv9Iy7q0G5jY0g4+oOhgvJhDQH2dlj0aF4o4D6RhRiZ2tODg3v7B2Q9V9Lj7XwzG1eDmI2hQpW4tF4WyMnj3Tsn6IquM6n3qDQk5lOJI4iiEqsi2Ne4MItvz/SnpHRDJqgPwBvqaXdjykxBwhE5VEYx/PoxqRsi41A7zHEFlYZ4HVvkx5fAaztMaSCWvxa+a73qVtRz4r5W6w1jdyfqWLshGdQ8H4PvfvAGwRfqj33UNX76FXr5GnXFkBJ1lLcsTKogw0K3p/wBJiZW16/n8H+VEx0R/cAxQyD7AcZi9nlbAS1IcAfMrSOLAa+RFp/iaH9bv2u/8tnH2GFmaTNTV7r9TwIDAQABMA0GCSqGSIb3DQEBCwUAA4IBAQB0HX5gC8wKDQGAB3DpoTHQjlhrXIxBvK0R3i5K1ddtw6FT8b/lYuQYx9zZx66kvLZsy/SCq7Yli3pTUvi/jLT4Lzv2C7azPzqI93lD5+JKaiE2vqAOlbVF7d+gZi3djLEm7HF0qEsmwzWQDDQ2T2DXqIu3w8zhPItopBxz3MJ4D2Iu7Tl1KDImr3CcjE6sBwHvRkV7Esoa/HMnRfcZbjnG2YJX7xWFLqq6gV15UN1NoyJjk7B4hCQUF45sxabKRu2OcaIKO0i3a3UVpSlPmrPYJDr4d6gV0gPNaJ5gn/J/0fkUm67IbpW5pT+ScE3Z5xWuTb/iEHwGEh1bvVU8x5lj9</ds:X509Certificate>
        </ds:X509Data>
      </ds:KeyInfo>
    </md:KeyDescriptor>
    <md:NameIDFormat>urn:oasis:names:tc:SAML:1.1:nameid-format:emailAddress</md:NameIDFormat>
    <md:SingleSignOnService Binding="urn:oasis:names:tc:SAML:2.0:bindings:HTTP-POST" Location="https://example.okta.com/app/exampleapp_1/exk1abc/sso/saml"/>
    <md:SingleSignOnService Binding="urn:oasis:names:tc:SAML:2.0:bindings:HTTP-Redirect" Location="https://example.okta.com/app/exampleapp_1/exk1abc/sso/saml"/>
  </md:IDPSSODescriptor>
</md:EntityDescriptor>`

// A minimal valid Entra-shaped (Microsoft) EntityDescriptor. Uses
// /federationmetadata/2007-06/federationmetadata.xml format with EntitiesDescriptor
// wrapper — but crewjam's ParseMetadata accepts either shape. We test
// EntityDescriptor here.
const metadataEntraXML = `<?xml version="1.0" encoding="UTF-8"?>
<EntityDescriptor xmlns="urn:oasis:names:tc:SAML:2.0:metadata" entityID="https://sts.windows.net/aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee/">
  <IDPSSODescriptor protocolSupportEnumeration="urn:oasis:names:tc:SAML:2.0:protocol">
    <KeyDescriptor use="signing">
      <KeyInfo xmlns="http://www.w3.org/2000/09/xmldsig#">
        <X509Data>
          <X509Certificate>MIIDpDCCAoygAwIBAgIGAYsR7tcgMA0GCSqGSIb3DQEBCwUAMIGSMQswCQYDVQQGEwJVUzETMBEGA1UECAwKQ2FsaWZvcm5pYTEWMBQGA1UEBwwNU2FuIEZyYW5jaXNjbzENMAsGA1UECgwET2t0YTEUMBIGA1UECwwLU1NPUHJvdmlkZXIxEzARBgNVBAMMCnRyaWFsLTAwMTExHDAaBgkqhkiG9w0BCQEWDWluZm9Ab2t0YS5jb20wHhcNMjMwNzE3MDQ1NTAxWhcNMzMwNzE3MDQ1NjAxWjCBkjELMAkGA1UEBhMCVVMxEzARBgNVBAgMCkNhbGlmb3JuaWExFjAUBgNVBAcMDVNhbiBGcmFuY2lzY28xDTALBgNVBAoMBE9rdGExFDASBgNVBAsMC1NTT1Byb3ZpZGVyMRMwEQYDVQQDDAp0cmlhbC0wMDExMRwwGgYJKoZIhvcNAQkBFg1pbmZvQG9rdGEuY29tMIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKCAQEAv9Iy7q0G5jY0g4+oOhgvJhDQH2dlj0aF4o4D6RhRiZ2tODg3v7B2Q9V9Lj7XwzG1eDmI2hQpW4tF4WyMnj3Tsn6IquM6n3qDQk5lOJI4iiEqsi2Ne4MItvz/SnpHRDJqgPwBvqaXdjykxBwhE5VEYx/PoxqRsi41A7zHEFlYZ4HVvkx5fAaztMaSCWvxa+a73qVtRz4r5W6w1jdyfqWLshGdQ8H4PvfvAGwRfqj33UNX76FXr5GnXFkBJ1lLcsTKogw0K3p/wBJiZW16/n8H+VEx0R/cAxQyD7AcZi9nlbAS1IcAfMrSOLAa+RFp/iaH9bv2u/8tnH2GFmaTNTV7r9TwIDAQABMA0GCSqGSIb3DQEBCwUAA4IBAQB0HX5gC8wKDQGAB3DpoTHQjlhrXIxBvK0R3i5K1ddtw6FT8b/lYuQYx9zZx66kvLZsy/SCq7Yli3pTUvi/jLT4Lzv2C7azPzqI93lD5+JKaiE2vqAOlbVF7d+gZi3djLEm7HF0qEsmwzWQDDQ2T2DXqIu3w8zhPItopBxz3MJ4D2Iu7Tl1KDImr3CcjE6sBwHvRkV7Esoa/HMnRfcZbjnG2YJX7xWFLqq6gV15UN1NoyJjk7B4hCQUF45sxabKRu2OcaIKO0i3a3UVpSlPmrPYJDr4d6gV0gPNaJ5gn/J/0fkUm67IbpW5pT+ScE3Z5xWuTb/iEHwGEh1bvVU8x5lj9</X509Certificate>
        </X509Data>
      </KeyInfo>
    </KeyDescriptor>
    <SingleSignOnService Binding="urn:oasis:names:tc:SAML:2.0:bindings:HTTP-Redirect" Location="https://login.microsoftonline.com/aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee/saml2"/>
    <SingleSignOnService Binding="urn:oasis:names:tc:SAML:2.0:bindings:HTTP-POST" Location="https://login.microsoftonline.com/aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee/saml2"/>
  </IDPSSODescriptor>
</EntityDescriptor>`

// A minimal Keycloak-shaped EntityDescriptor.
const metadataKeycloakXML = `<?xml version="1.0" encoding="UTF-8"?>
<EntityDescriptor xmlns="urn:oasis:names:tc:SAML:2.0:metadata" entityID="https://keycloak.example.com/realms/master">
  <IDPSSODescriptor WantAuthnRequestsSigned="true" protocolSupportEnumeration="urn:oasis:names:tc:SAML:2.0:protocol">
    <KeyDescriptor use="signing">
      <KeyInfo xmlns="http://www.w3.org/2000/09/xmldsig#">
        <X509Data>
          <X509Certificate>MIIDpDCCAoygAwIBAgIGAYsR7tcgMA0GCSqGSIb3DQEBCwUAMIGSMQswCQYDVQQGEwJVUzETMBEGA1UECAwKQ2FsaWZvcm5pYTEWMBQGA1UEBwwNU2FuIEZyYW5jaXNjbzENMAsGA1UECgwET2t0YTEUMBIGA1UECwwLU1NPUHJvdmlkZXIxEzARBgNVBAMMCnRyaWFsLTAwMTExHDAaBgkqhkiG9w0BCQEWDWluZm9Ab2t0YS5jb20wHhcNMjMwNzE3MDQ1NTAxWhcNMzMwNzE3MDQ1NjAxWjCBkjELMAkGA1UEBhMCVVMxEzARBgNVBAgMCkNhbGlmb3JuaWExFjAUBgNVBAcMDVNhbiBGcmFuY2lzY28xDTALBgNVBAoMBE9rdGExFDASBgNVBAsMC1NTT1Byb3ZpZGVyMRMwEQYDVQQDDAp0cmlhbC0wMDExMRwwGgYJKoZIhvcNAQkBFg1pbmZvQG9rdGEuY29tMIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKCAQEAv9Iy7q0G5jY0g4+oOhgvJhDQH2dlj0aF4o4D6RhRiZ2tODg3v7B2Q9V9Lj7XwzG1eDmI2hQpW4tF4WyMnj3Tsn6IquM6n3qDQk5lOJI4iiEqsi2Ne4MItvz/SnpHRDJqgPwBvqaXdjykxBwhE5VEYx/PoxqRsi41A7zHEFlYZ4HVvkx5fAaztMaSCWvxa+a73qVtRz4r5W6w1jdyfqWLshGdQ8H4PvfvAGwRfqj33UNX76FXr5GnXFkBJ1lLcsTKogw0K3p/wBJiZW16/n8H+VEx0R/cAxQyD7AcZi9nlbAS1IcAfMrSOLAa+RFp/iaH9bv2u/8tnH2GFmaTNTV7r9TwIDAQABMA0GCSqGSIb3DQEBCwUAA4IBAQB0HX5gC8wKDQGAB3DpoTHQjlhrXIxBvK0R3i5K1ddtw6FT8b/lYuQYx9zZx66kvLZsy/SCq7Yli3pTUvi/jLT4Lzv2C7azPzqI93lD5+JKaiE2vqAOlbVF7d+gZi3djLEm7HF0qEsmwzWQDDQ2T2DXqIu3w8zhPItopBxz3MJ4D2Iu7Tl1KDImr3CcjE6sBwHvRkV7Esoa/HMnRfcZbjnG2YJX7xWFLqq6gV15UN1NoyJjk7B4hCQUF45sxabKRu2OcaIKO0i3a3UVpSlPmrPYJDr4d6gV0gPNaJ5gn/J/0fkUm67IbpW5pT+ScE3Z5xWuTb/iEHwGEh1bvVU8x5lj9</X509Certificate>
        </X509Data>
      </KeyInfo>
    </KeyDescriptor>
    <SingleSignOnService Binding="urn:oasis:names:tc:SAML:2.0:bindings:HTTP-POST" Location="https://keycloak.example.com/realms/master/protocol/saml"/>
  </IDPSSODescriptor>
</EntityDescriptor>`

func TestParseAndCacheMetadata_Okta(t *testing.T) {
	ResetMetadataCache()
	ed, err := ParseAndCacheMetadata([]byte(metadataOktaXML))
	if err != nil {
		t.Fatalf("ParseAndCacheMetadata: %v", err)
	}
	if ed.EntityID != "http://www.okta.com/example" {
		t.Fatalf("EntityID: got %q, want okta", ed.EntityID)
	}
}

func TestParseAndCacheMetadata_Entra(t *testing.T) {
	ResetMetadataCache()
	ed, err := ParseAndCacheMetadata([]byte(metadataEntraXML))
	if err != nil {
		t.Fatalf("ParseAndCacheMetadata Entra: %v", err)
	}
	if !strings.HasPrefix(ed.EntityID, "https://sts.windows.net/") {
		t.Fatalf("entra EntityID: %s", ed.EntityID)
	}
}

func TestParseAndCacheMetadata_Keycloak(t *testing.T) {
	ResetMetadataCache()
	ed, err := ParseAndCacheMetadata([]byte(metadataKeycloakXML))
	if err != nil {
		t.Fatalf("ParseAndCacheMetadata Keycloak: %v", err)
	}
	if !strings.HasPrefix(ed.EntityID, "https://keycloak.example.com/") {
		t.Fatalf("keycloak EntityID: %s", ed.EntityID)
	}
}

func TestParseAndCacheMetadata_SameBytesReturnsSamePointer(t *testing.T) {
	ResetMetadataCache()
	first, err := ParseAndCacheMetadata([]byte(metadataOktaXML))
	if err != nil {
		t.Fatalf("first parse: %v", err)
	}
	second, err := ParseAndCacheMetadata([]byte(metadataOktaXML))
	if err != nil {
		t.Fatalf("second parse: %v", err)
	}
	if first != second {
		t.Fatalf("expected same pointer on cache hit, got distinct: %p vs %p", first, second)
	}
}

func TestParseAndCacheMetadata_DifferentBytesReturnsDifferentPointer(t *testing.T) {
	ResetMetadataCache()
	a, err := ParseAndCacheMetadata([]byte(metadataOktaXML))
	if err != nil {
		t.Fatalf("okta: %v", err)
	}
	b, err := ParseAndCacheMetadata([]byte(metadataEntraXML))
	if err != nil {
		t.Fatalf("entra: %v", err)
	}
	if a == b {
		t.Fatalf("expected distinct pointers for distinct bytes, got identical")
	}
}

func TestParseAndCacheMetadata_MalformedXMLWrappedError(t *testing.T) {
	ResetMetadataCache()
	_, err := ParseAndCacheMetadata([]byte("<not-saml/>"))
	if err == nil {
		t.Fatal("expected error for malformed metadata, got nil")
	}
	if !errors.Is(err, ErrParseMetadata) {
		t.Fatalf("expected ErrParseMetadata, got %v", err)
	}
}

func TestRefreshMetadata_ReplacesCachedEntry(t *testing.T) {
	ResetMetadataCache()
	first, err := ParseAndCacheMetadata([]byte(metadataOktaXML))
	if err != nil {
		t.Fatalf("first parse: %v", err)
	}
	if err := RefreshMetadata([]byte(metadataOktaXML)); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	second, err := ParseAndCacheMetadata([]byte(metadataOktaXML))
	if err != nil {
		t.Fatalf("second parse: %v", err)
	}
	if first == second {
		t.Fatalf("expected new pointer after Refresh, got cached %p", first)
	}
}

func TestParseAndCacheMetadata_ConcurrentAccess(t *testing.T) {
	ResetMetadataCache()
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := ParseAndCacheMetadata([]byte(metadataOktaXML)); err != nil {
				t.Errorf("concurrent parse: %v", err)
			}
		}()
	}
	wg.Wait()
}
