package source

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/emersion/go-imap"
	"github.com/emersion/go-imap/client"
)

const mailboxFetchLimit = 4 << 20

// MailMessage is one unread mailbox message. ID is the provider message id.
type MailMessage struct {
	ID         string
	ReceivedAt time.Time
	Raw        []byte
}

// Mailbox returns messages whose provider id is after the cursor.
// Fetch does not read the job pages linked from those messages.
type Mailbox interface {
	Fetch(ctx context.Context, afterID string) ([]MailMessage, error)
}

// MailOpener opens the mailbox named by a source's secret env var.
type MailOpener interface {
	Open(ctx context.Context, secretEnv string) (Mailbox, error)
}

// MailboxLocation is a parsed IMAP URL. The password is not logged.
type MailboxLocation struct {
	Address  string
	Server   string
	TLS      bool
	Username string
	Password string
	Mailbox  string
}

// ParseMailboxURL reads imaps://user:password@host:993/INBOX.
// imap:// uses STARTTLS. The returned error does not include the URL.
func ParseMailboxURL(raw string) (MailboxLocation, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return MailboxLocation{}, fmt.Errorf("mailbox url is empty")
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" {
		return MailboxLocation{}, fmt.Errorf("mailbox url is invalid")
	}
	location := MailboxLocation{Server: parsed.Hostname()}
	switch strings.ToLower(parsed.Scheme) {
	case "imaps":
		location.TLS = true
		location.Address = hostWithPort(parsed, "993")
	case "imap":
		location.Address = hostWithPort(parsed, "143")
	default:
		return MailboxLocation{}, fmt.Errorf("mailbox url scheme must be imap or imaps")
	}
	if location.Server == "" {
		return MailboxLocation{}, fmt.Errorf("mailbox url is invalid")
	}
	if parsed.User == nil || parsed.User.Username() == "" {
		return MailboxLocation{}, fmt.Errorf("mailbox url needs a username and password")
	}
	password, ok := parsed.User.Password()
	if !ok || password == "" {
		return MailboxLocation{}, fmt.Errorf("mailbox url needs a username and password")
	}
	location.Username = parsed.User.Username()
	location.Password = password
	location.Mailbox = strings.Trim(parsed.Path, "/")
	if location.Mailbox == "" {
		location.Mailbox = "INBOX"
	}
	return location, nil
}

func hostWithPort(parsed *url.URL, fallback string) string {
	port := parsed.Port()
	if port == "" {
		port = fallback
	}
	return net.JoinHostPort(parsed.Hostname(), port)
}

// EnvMailboxes opens the IMAP mailbox in the environment variable named by the source.
type EnvMailboxes struct{}

// Open reads the env var and connects when Fetch is called.
func (EnvMailboxes) Open(_ context.Context, secretEnv string) (Mailbox, error) {
	name := strings.TrimSpace(secretEnv)
	if name == "" {
		return nil, fmt.Errorf("email source has no secret_env")
	}
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return nil, fmt.Errorf("environment variable %s is empty", name)
	}
	location, err := ParseMailboxURL(raw)
	if err != nil {
		return nil, fmt.Errorf("environment variable %s is invalid", name)
	}
	return IMAP{Location: location}, nil
}

// IMAP reads one mailbox over IMAP. It does not request LinkedIn pages.
type IMAP struct {
	Location MailboxLocation
}

// Fetch returns messages with a UID greater than afterID, oldest first.
func (box IMAP) Fetch(ctx context.Context, afterID string) ([]MailMessage, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	after, err := cursorUID(afterID)
	if err != nil {
		return nil, err
	}
	conn, err := box.dial()
	if err != nil {
		return nil, err
	}
	defer conn.Logout()
	conn.ErrorLog = silentLog{}
	conn.Timeout = 30 * time.Second
	if err := conn.Login(box.Location.Username, box.Location.Password); err != nil {
		return nil, fmt.Errorf("imap login: %w", err)
	}
	if _, err := conn.Select(box.Location.Mailbox, true); err != nil {
		return nil, fmt.Errorf("imap select mailbox: %w", err)
	}
	criteria := imap.NewSearchCriteria()
	// Only job-alert senders. The inbox also holds invitations and newsletters.
	linkedIn := imap.NewSearchCriteria()
	linkedIn.Header.Add("From", "jobalerts-noreply@linkedin.com")
	hello := imap.NewSearchCriteria()
	hello.Header.Add("From", "noreply@helloworld.rs")
	criteria.Or = [][2]*imap.SearchCriteria{{linkedIn, hello}}
	if after > 0 {
		if after == ^uint32(0) {
			return nil, nil
		}
		set := new(imap.SeqSet)
		set.AddRange(after+1, 0)
		criteria.Uid = set
	}
	uids, err := conn.UidSearch(criteria)
	if err != nil {
		return nil, fmt.Errorf("imap search: %w", err)
	}
	if len(uids) == 0 {
		return nil, nil
	}
	sort.Slice(uids, func(i, j int) bool { return uids[i] < uids[j] })
	seqset := new(imap.SeqSet)
	seqset.AddNum(uids...)
	section := &imap.BodySectionName{Peek: true}
	items := []imap.FetchItem{imap.FetchUid, imap.FetchInternalDate, section.FetchItem()}
	ch := make(chan *imap.Message, 8)
	errCh := make(chan error, 1)
	go func() {
		errCh <- conn.UidFetch(seqset, items, ch)
	}()
	var messages []MailMessage
	var readErr error
	for msg := range ch {
		if readErr != nil || msg == nil || msg.Uid == 0 {
			continue
		}
		literal := msg.GetBody(section)
		if literal == nil {
			readErr = fmt.Errorf("imap message %d has no body", msg.Uid)
			continue
		}
		raw, err := io.ReadAll(io.LimitReader(literal, mailboxFetchLimit+1))
		if err != nil {
			readErr = fmt.Errorf("imap read message")
			continue
		}
		if len(raw) > mailboxFetchLimit {
			readErr = fmt.Errorf("imap message %d is too large", msg.Uid)
			continue
		}
		received := msg.InternalDate.UTC()
		if received.IsZero() {
			received = time.Now().UTC()
		}
		messages = append(messages, MailMessage{
			ID:         strconv.FormatUint(uint64(msg.Uid), 10),
			ReceivedAt: received,
			Raw:        raw,
		})
	}
	if err := <-errCh; err != nil {
		return nil, fmt.Errorf("imap fetch: %w", err)
	}
	if readErr != nil {
		return nil, readErr
	}
	sort.Slice(messages, func(i, j int) bool {
		return lessMessageID(messages[i].ID, messages[j].ID)
	})
	return messages, nil
}

func (box IMAP) dial() (*client.Client, error) {
	dialer := &net.Dialer{Timeout: 30 * time.Second}
	tlsConfig := &tls.Config{ServerName: box.Location.Server}
	if box.Location.TLS {
		conn, err := client.DialWithDialerTLS(dialer, box.Location.Address, tlsConfig)
		if err != nil {
			return nil, fmt.Errorf("imap connect: %w", err)
		}
		return conn, nil
	}
	conn, err := client.DialWithDialer(dialer, box.Location.Address)
	if err != nil {
		return nil, fmt.Errorf("imap connect: %w", err)
	}
	if err := conn.StartTLS(tlsConfig); err != nil {
		conn.Logout()
		return nil, fmt.Errorf("imap starttls: %w", err)
	}
	return conn, nil
}

func cursorUID(afterID string) (uint32, error) {
	afterID = strings.TrimSpace(afterID)
	if afterID == "" {
		return 0, nil
	}
	parsed, err := strconv.ParseUint(afterID, 10, 32)
	if err != nil || parsed == 0 {
		return 0, fmt.Errorf("mailbox cursor is not a message id")
	}
	return uint32(parsed), nil
}

func lessMessageID(left, right string) bool {
	a, aErr := strconv.ParseUint(left, 10, 64)
	b, bErr := strconv.ParseUint(right, 10, 64)
	if aErr == nil && bErr == nil {
		return a < b
	}
	return left < right
}

type silentLog struct{}

func (silentLog) Printf(string, ...interface{}) {}

func (silentLog) Println(...interface{}) {}
