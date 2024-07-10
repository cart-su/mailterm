package api

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/mail"
	"os"
	"regexp"
	"sort"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"github.com/emersion/go-imap"
	"github.com/emersion/go-imap/client"
)

type IMAP struct {
	conn *client.Client
}

func NewIMAPClient() (*IMAP, error) {
	file, err := os.ReadFile("config.json")
	if err != nil {
		return nil, err
	}

	var credentials Config

	err = json.Unmarshal(file, &credentials)
	if err != nil {
		return nil, err
	}

	c, err := client.DialTLS(credentials.IMAP.Server, nil)
	if err != nil {
		return nil, err
	}

	if err := c.Login(credentials.IMAP.Username, credentials.IMAP.Password); err != nil {
		err := os.Remove(fmt.Sprintf("%s/gmail.json", baseDir))
		if err != nil {
			return nil, err
		}
		return nil, err
	}

	return &IMAP{conn: c}, nil
}

func (e *IMAP) Close() error {
	return e.conn.Logout()
}

func (e *IMAP) GetMailboxes() ([]*imap.MailboxInfo, error) {
	mailboxes := make(chan *imap.MailboxInfo, 10)
	done := make(chan error, 1)
	go func() {
		done <- e.conn.List("", "*", mailboxes)
	}()

	var boxes []*imap.MailboxInfo
	for m := range mailboxes {
		boxes = append(boxes, m)
	}

	if err := <-done; err != nil {
		return nil, err
	}
	return boxes, nil
}

func (e *IMAP) SelectMailbox(name string) error {
	_, err := e.conn.Select(name, false)
	return err
}

func (e *IMAP) FetchMessages(limit int) ([]*imap.Message, error) {
	criteria := imap.NewSearchCriteria()
	criteria.WithoutFlags = []string{imap.DeletedFlag}

	if e.conn.State() != imap.SelectedState {
		err := e.SelectMailbox("INBOX")
		if err != nil {
			return nil, err
		} // default to inbox
	}

	status, err := e.conn.Status("INBOX", []imap.StatusItem{imap.StatusMessages})
	if err != nil {
		return nil, err
	}
	totalMessages := status.Messages

	from := uint32(1)
	if totalMessages > uint32(limit) {
		from = totalMessages - uint32(limit) + 1
	}

	seqSet := new(imap.SeqSet)
	seqSet.AddRange(from, totalMessages)

	messages := make(chan *imap.Message, 10)
	done := make(chan error, 1)

	go func() {
		done <- e.conn.Fetch(seqSet, []imap.FetchItem{imap.FetchUid, imap.FetchEnvelope, imap.FetchBodyStructure}, messages)
	}()

	var msgs []*imap.Message
	for msg := range messages {
		msgs = append(msgs, msg)
	}

	if err := <-done; err != nil {
		return nil, err
	}

	sort.Slice(msgs, func(i, j int) bool {
		return msgs[i].Uid > msgs[j].Uid
	})

	if limit > 0 && len(msgs) > limit {
		msgs = msgs[:limit]
	}

	return msgs, nil
}

func (e *IMAP) GetMessageBody(uid uint32) (string, error) {
	seqSet := new(imap.SeqSet)
	seqSet.AddNum(uid)

	section := &imap.BodySectionName{}
	items := []imap.FetchItem{section.FetchItem()}

	messages := make(chan *imap.Message, 1)
	done := make(chan error, 1)
	go func() {
		done <- e.conn.UidFetch(seqSet, items, messages)
	}()

	msg := <-messages
	if err := <-done; err != nil {
		return "", err
	}

	r := msg.GetBody(section)
	if r == nil {
		return "", fmt.Errorf("no message body")
	}

	m, err := mail.ReadMessage(r)
	if err != nil {
		return "", err
	}

	headers := formatHeaders(m.Header)

	contentType, params, err := mime.ParseMediaType(m.Header.Get("Content-Type"))
	if err != nil {
		contentType = "text/plain" // Default to plain text
	}

	var body string
	if strings.HasPrefix(contentType, "multipart/") {
		body, err = handleMultipart(m.Body, params["boundary"])
	} else {
		body, err = handleSinglePart(m.Body, contentType, m.Header.Get("Content-Transfer-Encoding"))
	}

	if err != nil {
		return "", err
	}

	return headers + "\n\n" + body, nil
}

func handleMultipart(r io.Reader, boundary string) (string, error) {
	mr := multipart.NewReader(r, boundary)
	var result strings.Builder

	for {
		p, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", err
		}

		contentType := p.Header.Get("Content-Type")
		if strings.HasPrefix(contentType, "text/plain") || strings.HasPrefix(contentType, "text/html") {
			partContent, err := handleSinglePart(p, contentType, p.Header.Get("Content-Transfer-Encoding"))
			if err != nil {
				return "", err
			}
			result.WriteString(partContent)
			result.WriteString("\n\n")
		}
	}

	return result.String(), nil
}

func handleSinglePart(r io.Reader, contentType, encoding string) (string, error) {
	body, err := io.ReadAll(r)
	if err != nil {
		return "", err
	}

	decodedBody, err := decodeBody(string(body), encoding)
	if err != nil {
		return "", err
	}

	renderedBody, err := renderImapMessage(decodedBody, contentType)
	if err != nil {
		return "", err
	}

	return renderedBody, nil
}

func decodeBody(body, encoding string) (string, error) {
	switch strings.ToLower(encoding) {
	case "base64":
		decoded, err := base64.StdEncoding.DecodeString(body)
		if err != nil {
			return "", err
		}
		return string(decoded), nil
	case "quoted-printable":
		reader := quotedprintable.NewReader(strings.NewReader(body))
		decoded, err := io.ReadAll(reader)
		if err != nil {
			return "", err
		}
		return string(decoded), nil
	default:
		return body, nil
	}
}

func renderImapMessage(body, contentType string) (string, error) {
	if strings.HasPrefix(contentType, "text/html") {
		doc, err := goquery.NewDocumentFromReader(strings.NewReader(body))
		if err != nil {
			return "", err
		}

		doc.Find("style").Each(func(i int, s *goquery.Selection) {
			s.Remove()
		})

		doc.Find("[style]").Each(func(i int, s *goquery.Selection) {
			s.RemoveAttr("style")
		})

		renderedHTML := doc.Text()

		renderedHTML = regexp.MustCompile(`\s+`).ReplaceAllString(renderedHTML, " ")
		renderedHTML = regexp.MustCompile(`\n\s*\n`).ReplaceAllString(renderedHTML, "\n\n")
		renderedHTML = strings.TrimSpace(renderedHTML)

		return renderedHTML, nil
	}

	return body, nil
}

func formatHeaders(header mail.Header) string {
	return fmt.Sprintf("From: %s\nTo: %s\nCc: %s\nDate: %s\nSubject: %s",
		header.Get("From"), header.Get("To"), header.Get("Cc"), header.Get("Date"), header.Get("Subject"))
}

func (e *IMAP) DeleteMessage(uid uint32) error {
	seqSet := new(imap.SeqSet)
	seqSet.AddNum(uid)

	item := imap.FormatFlagsOp(imap.AddFlags, true)
	flags := []interface{}{imap.DeletedFlag}

	return e.conn.Store(seqSet, item, flags, nil)
}

func (e *IMAP) SearchMessages(criteria *imap.SearchCriteria) ([]uint32, error) {
	return e.conn.Search(criteria)
}

func (e *IMAP) MarkAsRead(uid uint32) error {
	seqSet := new(imap.SeqSet)
	seqSet.AddNum(uid)

	item := imap.FormatFlagsOp(imap.AddFlags, true)
	flags := []interface{}{imap.SeenFlag}

	return e.conn.Store(seqSet, item, flags, nil)
}
