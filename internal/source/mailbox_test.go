package source

import (
	"strings"
	"testing"
)

func TestParseMailboxURLHidesThePassword(t *testing.T) {
	location, err := ParseMailboxURL("imaps://student:supersecret@imap.example:993/INBOX")
	if err != nil {
		t.Fatalf("ParseMailboxURL() error = %v", err)
	}
	if location.Username != "student" || location.Password != "supersecret" {
		t.Fatal("credentials were not read")
	}
	if !location.TLS || location.Address != "imap.example:993" || location.Mailbox != "INBOX" {
		t.Fatalf("location = %+v", location)
	}
	if location.Server != "imap.example" {
		t.Fatalf("server = %s", location.Server)
	}

	_, err = ParseMailboxURL("imaps://student:supersecret@")
	if err == nil {
		t.Fatal("ParseMailboxURL() error = nil, want an invalid url error")
	}
	if strings.Contains(err.Error(), "supersecret") || strings.Contains(err.Error(), "imap.example") {
		t.Fatalf("error includes the mailbox url: %v", err)
	}

	if _, err := ParseMailboxURL("https://student:supersecret@imap.example/INBOX"); err == nil {
		t.Fatal("ParseMailboxURL() error = nil, want a scheme error")
	}
}

func TestMessageIDsSortNumerically(t *testing.T) {
	if !lessMessageID("9", "10") {
		t.Fatal("message id 9 sorted after 10")
	}
}
