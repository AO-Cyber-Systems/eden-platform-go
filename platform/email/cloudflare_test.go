package email

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCloudflareSenderSuccess(t *testing.T) {
	var gotPath, gotAuth, gotCT string
	var gotBody cfSendRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		gotCT = r.Header.Get("Content-Type")
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &gotBody)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"success":true,"errors":[],"result":{"message_id":"<abc@mail.aocyber.ai>","delivered":["dest@example.com"],"queued":[],"permanent_bounces":[]}}`))
	}))
	defer srv.Close()

	sender := NewCloudflareAPI(CloudflareConfig{
		AccountID: "acct123",
		APIToken:  "cfut_secret",
		BaseURL:   srv.URL,
		HTTPClient: srv.Client(),
	})
	res, err := sender.Send(context.Background(), Message{
		From:     Address{Name: "AO Cyber Systems", Email: "noreply@mail.aocyber.ai"},
		To:       []Address{{Email: "dest@example.com"}},
		Subject:  "hi",
		TextBody: "plain",
		HTMLBody: "<p>rich</p>",
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if res.MessageID != "<abc@mail.aocyber.ai>" {
		t.Errorf("message id: got %q", res.MessageID)
	}
	if gotPath != "/accounts/acct123/email/sending/send" {
		t.Errorf("path: got %q", gotPath)
	}
	if gotAuth != "Bearer cfut_secret" {
		t.Errorf("auth: got %q", gotAuth)
	}
	if !strings.HasPrefix(gotCT, "application/json") {
		t.Errorf("content-type: got %q", gotCT)
	}
	if gotBody.To != "dest@example.com" || gotBody.Subject != "hi" || gotBody.Text != "plain" || gotBody.HTML != "<p>rich</p>" {
		t.Errorf("body mismatch: %+v", gotBody)
	}
	if !strings.Contains(gotBody.From, "noreply@mail.aocyber.ai") {
		t.Errorf("from missing address: %q", gotBody.From)
	}
}

func TestCloudflareSenderAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"success":false,"errors":[{"code":10000,"message":"Authentication error"}],"result":null}`))
	}))
	defer srv.Close()

	sender := NewCloudflareAPI(CloudflareConfig{AccountID: "a", APIToken: "bad", BaseURL: srv.URL, HTTPClient: srv.Client()})
	_, err := sender.Send(context.Background(), Message{
		From: Address{Email: "noreply@mail.aocyber.ai"},
		To:   []Address{{Email: "dest@example.com"}},
	})
	if err == nil {
		t.Fatal("expected error on non-success response")
	}
	if !strings.Contains(err.Error(), "10000") || !strings.Contains(err.Error(), "Authentication error") {
		t.Errorf("error should surface the API error: %v", err)
	}
}

func TestCloudflareSenderRejectsInvalid(t *testing.T) {
	sender := NewCloudflareAPI(CloudflareConfig{AccountID: "a", APIToken: "t"})
	_, err := sender.Send(context.Background(), Message{Subject: "no from no to"})
	if err == nil {
		t.Fatal("expected ErrInvalidMessage for message with no From/To")
	}
}

func TestCloudflareSenderMisconfigured(t *testing.T) {
	sender := NewCloudflareAPI(CloudflareConfig{APIToken: "t"}) // no account id
	_, err := sender.Send(context.Background(), Message{
		From: Address{Email: "a@b.com"},
		To:   []Address{{Email: "c@d.com"}},
	})
	if err == nil {
		t.Fatal("expected error when account id is empty")
	}
}
