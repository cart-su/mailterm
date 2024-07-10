package api

import (
	"encoding/json"
	"fmt"
	"os"
)

type EmailClient struct {
	GmailClient   *GmailClient
	GraphClient   *GraphHelper
	IMAP          *IMAP
	ActiveService string // "gmail" or "graph"
}

type Message struct {
	Subject  string
	Body     string
	From     string
	ThreadId string
	To       string
}

type Config struct {
	Gmail           GmailConfig
	Graph           GraphConfig
	IMAP            IMAPConfig
	SelectedService string `json:"selected_service"`
}

type GmailConfig struct {
	Installed struct {
		ClientID                string   `json:"client_id"`
		ProjectID               string   `json:"project_id"`
		AuthURI                 string   `json:"auth_uri"`
		TokenURI                string   `json:"token_uri"`
		AuthProviderX509CertURL string   `json:"auth_provider_x509_cert_url"`
		ClientSecret            string   `json:"client_secret"`
		RedirectURIs            []string `json:"redirect_uris"`
	} `json:"installed"`
}

type GraphConfig struct {
	ClientId     string `json:"client_id"`
	TenantID     string `json:"tenant_id"`
	ClientSecret string `json:"client_secret"`
}

type IMAPConfig struct {
	Server   string `json:"server"`
	Username string `json:"username"`
	Password string `json:"password"`
}

var baseDir string

func NewEmailClient(selectedService string) (*EmailClient, error) {
	gmailClient, err := NewGmailClient()
	if err != nil {
		if selectedService == "gmail" {
			return nil, fmt.Errorf("creating Gmail client: %w", err)
		}
	}

	graphClient, err := NewGraphClient()
	if err != nil {
		if selectedService == "graph" {
			return nil, fmt.Errorf("creating Graph client: %w", err)
		}
	}

	imapClient, err := NewIMAPClient()
	if err != nil {
		if selectedService == "imap" {
			return nil, fmt.Errorf("creating IMAP client: %w", err)
		}
	}

	return &EmailClient{
		GmailClient:   gmailClient,
		GraphClient:   graphClient,
		IMAP:          imapClient,
		ActiveService: selectedService, // default
	}, nil
}

func (c *EmailClient) SwitchToGmail() {
	GmailPage = ""
	c.ActiveService = "gmail"
}

func (c *EmailClient) SwitchToGraph() {
	c.ActiveService = "graph"
}

func (c *EmailClient) SwitchToIMAP() {
	c.ActiveService = "imap"
}

func LoadConfig() (*Config, error) {
	baseDir = os.Getenv("MAILTERM_HOME")
	file, err := os.ReadFile(fmt.Sprintf("%s/config.json", baseDir))
	if err != nil {
		return nil, err
	}

	var config Config
	err = json.Unmarshal(file, &config)
	if err != nil {
		return nil, err
	}

	return &config, nil
}

func SaveConfig(config *Config) error {
	baseDir = os.Getenv("MAILTERM_HOME")
	file, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(fmt.Sprintf("%s/config.json", baseDir), file, 0644)
}
