package proxmox

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"pvc/internal/command"
)

func TestValidatePassword(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		password string
		wantErr  bool
	}{
		{"minimum", "12345678", false},
		{"maximum", strings.Repeat("a", 64), false},
		{"accents count as characters", "éééééééé", false},
		{"too short", "1234567", true},
		{"too long", strings.Repeat("a", 65), true},
		{"empty", "", true},
		{"newline", "abcdefgh\n", true},
		{"tab", "abcd\tefgh", true},
		{"spaces are fine", "a b c d e f", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if err := ValidatePassword(tt.password); (err != nil) != tt.wantErr {
				t.Fatalf("ValidatePassword(%q) = %v", tt.password, err)
			}
		})
	}
}

func TestPasswordRealm(t *testing.T) {
	t.Parallel()
	for userid, want := range map[string]bool{"a@pve": true, "root@pam": true, "a@ldap": false, "a@ad": false, "a": false, "a@PVE": false} {
		if _, ok := PasswordRealm(userid); ok != want {
			t.Errorf("PasswordRealm(%q) = %v", userid, ok)
		}
	}
	if realm, _ := PasswordRealm("a@ldap"); realm != "ldap" {
		t.Errorf("realm = %q", realm)
	}
}

func TestSetPassword(t *testing.T) {
	t.Parallel()
	const secret = "correct-horse-battery"
	key := command.Key("pvesh", "set", "/access/password", "-userid", "alice@pve", "-password", secret)

	t.Run("sends the password", func(t *testing.T) {
		t.Parallel()
		runner := &command.MockRunner{Responses: map[string]command.Result{key: {}}}
		if err := NewClient(runner).SetPassword(context.Background(), "alice@pve", secret); err != nil {
			t.Fatal(err)
		}
		if len(runner.Calls) != 1 || runner.Calls[0] != key {
			t.Fatalf("calls = %v", runner.Calls)
		}
	})
	t.Run("hides the password in the error", func(t *testing.T) {
		t.Parallel()
		cause := errors.New("exit status 255")
		runner := &command.MockRunner{Errors: map[string]error{key: command.CommandError{
			Result: command.Result{Command: "pvesh", Args: []string{"set", "/access/password", "-userid", "alice@pve", "-password", secret}, Stderr: []byte("user 'alice@pve' does not exist")},
			Err:    cause,
		}}}
		err := NewClient(runner).SetPassword(context.Background(), "alice@pve", secret)
		if !errors.Is(err, cause) {
			t.Fatalf("err = %v", err)
		}
		var failed command.CommandError
		if !errors.As(err, &failed) {
			t.Fatalf("err = %T", err)
		}
		if text := err.Error() + fmt.Sprint(failed.Result.Args); strings.Contains(text, secret) {
			t.Fatalf("the password is in %q", text)
		}
		if got := failed.Result.Args[5]; got != "***" {
			t.Fatalf("password argument = %q", got)
		}
	})
	t.Run("refuses before running anything", func(t *testing.T) {
		t.Parallel()
		for name, call := range map[string]func(Client) error{
			"user":  func(c Client) error { return c.SetPassword(context.Background(), "alice", secret) },
			"realm": func(c Client) error { return c.SetPassword(context.Background(), "alice@ldap", secret) },
			"short": func(c Client) error { return c.SetPassword(context.Background(), "alice@pve", "short") },
		} {
			runner := &command.MockRunner{}
			if err := call(NewClient(runner)); err == nil || len(runner.Calls) != 0 {
				t.Errorf("%s: err = %v, calls = %v", name, err, runner.Calls)
			}
		}
	})
}
