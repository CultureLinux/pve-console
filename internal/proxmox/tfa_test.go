package proxmox

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"pvc/internal/command"
)

func TestNewTOTPSecret(t *testing.T) {
	t.Parallel()
	secret, err := NewTOTPSecret(bytes.NewReader(make([]byte, 20)))
	if err != nil {
		t.Fatal(err)
	}
	if secret != strings.Repeat("A", 32) {
		t.Fatalf("secret = %q", secret)
	}
	other, err := NewTOTPSecret(nil)
	if err != nil || len(other) != 32 {
		t.Fatalf("random secret = %q, %v", other, err)
	}
	if _, err := NewTOTPSecret(bytes.NewReader(make([]byte, 5))); err == nil {
		t.Fatal("short source must fail")
	}
}

func TestTOTPURI(t *testing.T) {
	t.Parallel()
	secret := strings.Repeat("A", 32)
	tests := []struct {
		name, issuer, user, secret, want string
		wantErr                          bool
	}{
		{"plain", "pve01", "alice@pve", secret, "otpauth://totp/pve01:alice@pve?secret=" + secret, false},
		{"bad user", "pve01", "alice", secret, "", true},
		{"bad secret", "pve01", "alice@pve", "not base32!", "", true},
		{"empty secret", "pve01", "alice@pve", "", "", true},
		{"empty issuer", "", "alice@pve", secret, "", true},
		{"issuer with a colon", "a:b", "alice@pve", secret, "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := TOTPURI(tt.issuer, tt.user, tt.secret)
			if (err != nil) != tt.wantErr || got != tt.want {
				t.Fatalf("TOTPURI = %q, %v", got, err)
			}
		})
	}
}

func TestValidTOTPCode(t *testing.T) {
	t.Parallel()
	for code, want := range map[string]bool{"123456": true, "000000": true, "12345": false, "1234567": false, "12345a": false, "": false, " 12345": false} {
		if got := ValidTOTPCode(code); got != want {
			t.Errorf("ValidTOTPCode(%q) = %v", code, got)
		}
	}
}

func TestAddTOTP(t *testing.T) {
	t.Parallel()
	uri := "otpauth://totp/pve01:alice@pve?secret=" + strings.Repeat("A", 32)
	key := command.Key("pvesh", "create", "/access/tfa/alice@pve", "-type", "totp", "-totp", uri, "-value", "123456", "-description", "phone")

	t.Run("sends the factor", func(t *testing.T) {
		t.Parallel()
		runner := &command.MockRunner{Responses: map[string]command.Result{key: {}}}
		if err := NewClient(runner).AddTOTP(context.Background(), "alice@pve", uri, "123456", "phone"); err != nil {
			t.Fatal(err)
		}
		if len(runner.Calls) != 1 || runner.Calls[0] != key {
			t.Fatalf("calls = %v", runner.Calls)
		}
	})
	t.Run("wraps the refusal", func(t *testing.T) {
		t.Parallel()
		refused := errors.New("totp verify failed")
		runner := &command.MockRunner{Errors: map[string]error{key: refused}}
		err := NewClient(runner).AddTOTP(context.Background(), "alice@pve", uri, "123456", "phone")
		if !errors.Is(err, refused) {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("hides the secret and the code in the error", func(t *testing.T) {
		t.Parallel()
		failed := command.CommandError{
			Result: command.Result{Command: "pvesh", Args: []string{"create", "/access/tfa/alice@pve", "-type", "totp", "-totp", uri, "-value", "123456", "-description", "phone"}, Stderr: []byte("value: invalid token 123456")},
			Err:    errors.New("exit status 255"),
		}
		runner := &command.MockRunner{Errors: map[string]error{key: failed}}
		err := NewClient(runner).AddTOTP(context.Background(), "alice@pve", uri, "123456", "phone")
		var got command.CommandError
		if !errors.As(err, &got) {
			t.Fatalf("err = %T", err)
		}
		text := err.Error() + strings.Join(got.Result.Args, " ") + string(got.Result.Stderr)
		if strings.Contains(text, "123456") || strings.Contains(text, "AAAAAAAA") {
			t.Fatalf("a secret is left in %q", text)
		}
		if !strings.Contains(text, "phone") {
			t.Fatal("the other arguments must stay readable")
		}
	})
	t.Run("refuses bad input without running anything", func(t *testing.T) {
		t.Parallel()
		for name, call := range map[string]func(Client) error{
			"user": func(c Client) error { return c.AddTOTP(context.Background(), "alice", uri, "123456", "x") },
			"code": func(c Client) error { return c.AddTOTP(context.Background(), "alice@pve", uri, "12", "x") },
			"uri":  func(c Client) error { return c.AddTOTP(context.Background(), "alice@pve", "http://x", "123456", "x") },
		} {
			runner := &command.MockRunner{}
			if err := call(NewClient(runner)); err == nil || len(runner.Calls) != 0 {
				t.Errorf("%s: err = %v, calls = %v", name, err, runner.Calls)
			}
		}
	})
}

func TestAddRecoveryCodes(t *testing.T) {
	t.Parallel()
	key := command.Key("pvesh", "create", "/access/tfa/alice@pve", "-type", "recovery", "-description", "codes", "--output-format", "json")

	t.Run("returns the codes", func(t *testing.T) {
		t.Parallel()
		runner := &command.MockRunner{Responses: map[string]command.Result{key: {Stdout: []byte(`{"id":"x","recovery":["aaaa-bbbb","cccc-dddd"]}`)}}}
		codes, err := NewClient(runner).AddRecoveryCodes(context.Background(), "alice@pve", "codes")
		if err != nil || len(codes) != 2 || codes[1] != "cccc-dddd" {
			t.Fatalf("codes = %v, %v", codes, err)
		}
	})
	t.Run("errors", func(t *testing.T) {
		t.Parallel()
		refused := errors.New("already exists")
		tests := map[string]*command.MockRunner{
			"refused":  {Errors: map[string]error{key: refused}},
			"bad json": {Responses: map[string]command.Result{key: {Stdout: []byte("nope")}}},
			"no codes": {Responses: map[string]command.Result{key: {Stdout: []byte(`{"id":"x"}`)}}},
		}
		for name, runner := range tests {
			if _, err := NewClient(runner).AddRecoveryCodes(context.Background(), "alice@pve", "codes"); err == nil {
				t.Errorf("%s: want an error", name)
			}
		}
		_, err := NewClient(tests["refused"]).AddRecoveryCodes(context.Background(), "alice@pve", "codes")
		if !errors.Is(err, refused) {
			t.Errorf("err = %v", err)
		}
		if _, err := NewClient(&command.MockRunner{}).AddRecoveryCodes(context.Background(), "bad", "codes"); err == nil {
			t.Error("bad user must fail")
		}
	})
}

func TestTOTPIssuer(t *testing.T) {
	t.Parallel()
	tests := []struct{ node, want string }{
		{"pve01", "pve01"},
		{"pve01.example.test", "pve01"},
		{"a b", "PVE"},
		{".lan", "PVE"},
	}
	for _, tt := range tests {
		if got := (Client{Node: tt.node}).TOTPIssuer(); got != tt.want {
			t.Errorf("TOTPIssuer(%q) = %q, want %q", tt.node, got, tt.want)
		}
	}
	// Without a configured node, the hostname of the machine is used.
	if got := (Client{}).TOTPIssuer(); got == "" || strings.ContainsAny(got, ".:") {
		t.Errorf("TOTPIssuer() = %q", got)
	}
}
