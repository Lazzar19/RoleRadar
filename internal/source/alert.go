package source

import (
	"fmt"
	"net/mail"
	"strings"
)

// ParseJobAlert reads a LinkedIn or HelloWorld job-alert email.
// Any other sender is refused. The LinkedIn or HelloWorld page is not fetched.
func ParseJobAlert(raw []byte) ([]DiscoveredPosting, error) {
	from := senderAddress(raw)
	switch {
	case strings.HasSuffix(from, "@helloworld.rs") || strings.HasSuffix(from, ".helloworld.rs"):
		return ParseHelloWorldAlert(raw)
	case strings.HasSuffix(from, "@linkedin.com") || strings.HasSuffix(from, ".linkedin.com"):
		return ParseLinkedInAlert(raw)
	default:
		return nil, fmt.Errorf("mailbox message is not a job alert")
	}
}

func senderAddress(raw []byte) string {
	msg, err := mail.ReadMessage(strings.NewReader(string(raw)))
	if err != nil {
		return ""
	}
	address, err := mail.ParseAddress(msg.Header.Get("From"))
	if err != nil {
		return strings.ToLower(strings.TrimSpace(msg.Header.Get("From")))
	}
	return strings.ToLower(strings.TrimSpace(address.Address))
}
