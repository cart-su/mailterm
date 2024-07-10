package api

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/http"
	"net/mail"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/gmail/v1"
	"google.golang.org/api/option"
)

// GmailClient for later access
type GmailClient struct {
	Service *gmail.Service
}

var GmailPage string

func NewGmailClient() (*GmailClient, error) {
	ctx := context.Background()
	b, err := os.ReadFile("client_secret.json")
	if err != nil {
		return nil, fmt.Errorf("unable to read client secret file %v", err)
	}

	config, err := google.ConfigFromJSON(b, gmail.GmailModifyScope)
	if err != nil {
		log.Fatalf("Unable to parse client secret file to config: %v", err)
	}
	client := getHttpClient(config)

	srv, err := gmail.NewService(ctx, option.WithHTTPClient(client))
	if err != nil {
		log.Fatalf("Unable to retrieve Gmail client: %v", err)
	}

	return &GmailClient{Service: srv}, nil
}

func (gc *GmailClient) PrepareMessageForSending(email Message) *gmail.Message {
	var message bytes.Buffer

	// Write headers
	_, _ = fmt.Fprintf(&message, "From: %s\r\n", email.From)
	_, _ = fmt.Fprintf(&message, "To: %s\r\n", email.To)
	_, _ = fmt.Fprintf(&message, "Subject: %s\r\n", email.Subject)
	_, _ = fmt.Fprintf(&message, "MIME-Version: 1.0\r\n")
	_, _ = fmt.Fprintf(&message, "Content-Type: text/plain; charset=\"utf-8\"\r\n")
	_, _ = fmt.Fprintf(&message, "\r\n")

	// Write body
	message.WriteString(email.Body)

	// Encode the entire message in base64url format
	rawMessage := base64.URLEncoding.EncodeToString(message.Bytes())

	return &gmail.Message{
		Raw:      rawMessage,
		ThreadId: email.ThreadId,
	}
}

func (gc *GmailClient) SendMessage(message *gmail.Message) error {
	_, err := gc.Service.Users.Messages.Send("me", message).Do()
	if err != nil {
		return fmt.Errorf("unable to send message: %v", err)
	}
	return nil
}

func (gc *GmailClient) TrashMessage(messageId string) error {
	_, err := gc.Service.Users.Messages.Trash("me", messageId).Do()
	if err != nil {
		return fmt.Errorf("unable to trash message: %v", err)
	}
	return nil
}

func (gc *GmailClient) GetThreads() ([]*gmail.Thread, error) {
	if GmailPage != "" {
		r, err := gc.Service.Users.Threads.List("me").
			PageToken(GmailPage).
			MaxResults(20).
			Do()
		if err != nil {
			return nil, err
		}
		GmailPage = r.NextPageToken
		return r.Threads, nil
	} else {
		r, err := gc.Service.Users.Threads.List("me").
			MaxResults(20).
			Do()
		if err != nil {
			return nil, err
		}
		GmailPage = r.NextPageToken
		return r.Threads, nil
	}
}

func (gc *GmailClient) GetMessageMetadata(user string, messageId string) (*gmail.Message, error) {
	msg, err := gc.Service.Users.Messages.Get(user, messageId).
		Format("metadata").
		Do()
	if err != nil {
		return nil, err
	}

	return msg, nil
}

func (gc *GmailClient) GetMessageBody(messageId string) (string, error) {
	msg, err := gc.Service.Users.Messages.Get("me", messageId).Format("raw").Do()
	if err != nil {
		return "", err
	}

	rawData, err := base64.URLEncoding.DecodeString(msg.Raw)
	if err != nil {
		return "", err
	}

	headers, body, err := parseMessage(rawData)
	if err != nil {
		return "", err
	}

	formattedHeaders := formatHeadersGmail(headers)
	return formattedHeaders + "\n\n" + body, nil
}

func parseMessage(rawData []byte) (map[string]string, string, error) {
	r := bytes.NewReader(rawData)
	msg, err := mail.ReadMessage(r)
	if err != nil {
		return nil, "", err
	}

	headers := make(map[string]string)
	for k, v := range msg.Header {
		headers[k] = strings.Join(v, ", ")
	}

	contentType, params, err := mime.ParseMediaType(msg.Header.Get("Content-Type"))
	if err != nil {
		contentType = "text/plain" // Default to plain text
	}

	var body string
	if strings.HasPrefix(contentType, "multipart/") {
		body, err = handleMultipartGmail(msg.Body, params["boundary"])
	} else {
		body, err = handleSinglePartGmail(msg.Body, contentType, msg.Header.Get("Content-Transfer-Encoding"))
	}

	if err != nil {
		return nil, "", err
	}

	return headers, body, nil
}

func handleMultipartGmail(r io.Reader, boundary string) (string, error) {
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

func handleSinglePartGmail(r io.Reader, contentType, encoding string) (string, error) {
	body, err := io.ReadAll(r)
	if err != nil {
		return "", err
	}

	decodedBody, err := decodeBodyGmail(string(body), encoding)
	if err != nil {
		return "", err
	}

	renderedBody, err := renderMessageBodyGmail(decodedBody, contentType)
	if err != nil {
		return "", err
	}

	return renderedBody, nil
}

func decodeBodyGmail(body, encoding string) (string, error) {
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

func renderMessageBodyGmail(body, contentType string) (string, error) {
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
		return formatText(renderedHTML), nil
	}

	return formatText(body), nil
}

func formatText(text string) string {
	text = regexp.MustCompile(`\n{3,}`).ReplaceAllString(text, "\n\n")

	text = regexp.MustCompile(`[ \t]+`).ReplaceAllString(text, " ")

	lines := strings.Split(text, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimSpace(line)
	}
	text = strings.Join(lines, "\n")

	text = strings.Trim(text, "\n")

	// paragraph separation
	text = regexp.MustCompile(`\n{2,}`).ReplaceAllString(text, "\n\n")

	return text
}

func formatHeadersGmail(headers map[string]string) string {
	headerOrder := []string{"From", "To", "Cc", "Date", "Subject"}
	var formattedHeaders strings.Builder

	for _, header := range headerOrder {
		if value, ok := headers[header]; ok && value != "" {
			formattedHeaders.WriteString(fmt.Sprintf("%s: %s\n", header, value))
		}
	}

	return strings.TrimSpace(formattedHeaders.String())
}

func getHttpClient(config *oauth2.Config) *http.Client {
	if baseDir == "" {
		baseDir = os.Getenv("MAILTERM_HOME")
	}
	tokenFile := fmt.Sprintf("%s/gmail.json", baseDir)
	token, err := tokenFromFile(tokenFile)
	if err != nil {
		token = getTokenFromWeb(config)
		saveToken(tokenFile, token)
	}

	return config.Client(context.Background(), token)
}

func getTokenFromWeb(config *oauth2.Config) *oauth2.Token {
	channel := make(chan string)
	server := &http.Server{Addr: ":8080"}

	http.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		code := r.URL.Query().Get("code")
		channel <- code
		_, err := fmt.Fprintf(w, "Auth successful! You can now close this window.")
		if err != nil {
			_, _ = fmt.Fprintf(w, "Error: %v", err)
		}
	})

	go func() {
		if err := server.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("HTTP server ListenAndServe: %v", err)
		}
	}()

	config.RedirectURL = "http://localhost:8080/callback"

	url := config.AuthCodeURL("state-token", oauth2.AccessTypeOffline)
	fmt.Printf("Go to the following link in your browser then type the "+
		"authorization code: \n%v\n", url)

	authCode := <-channel

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(ctx); err != nil {
			log.Printf("Error shutting down server: %v", err)
		}
	}()

	token, err := config.Exchange(context.TODO(), authCode)
	if err != nil {
		log.Fatalf("Unable to retrieve token from web: %v", err)
	}
	return token
}

func CheckToken() error {
	_, err := tokenFromFile(fmt.Sprintf("%s/gmail.json", baseDir))
	return err
}

func tokenFromFile(file string) (*oauth2.Token, error) {
	f, err := os.Open(file)
	if err != nil {

		return nil, err
	}
	defer func(f *os.File) {
		err := f.Close()
		if err != nil {
			return
		}
	}(f)
	token := &oauth2.Token{}
	err = json.NewDecoder(f).Decode(token)
	return token, err
}

func saveToken(path string, token *oauth2.Token) {
	fmt.Printf("Saving credential file to: %s\n", path)
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		log.Fatalf("Unable to cache oauth token: %v", err)
	}
	defer func(f *os.File) {
		err := f.Close()
		if err != nil {
			return
		}
	}(f)
	err = json.NewEncoder(f).Encode(token)
	if err != nil {
		return
	}
}
