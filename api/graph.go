package api

import (
	"context"
	"encoding/json"
	"os"

	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	auth "github.com/microsoft/kiota-authentication-azure-go"
	msgraphsdk "github.com/microsoftgraph/msgraph-sdk-go"
	graphmodels "github.com/microsoftgraph/msgraph-sdk-go/models"
	"github.com/microsoftgraph/msgraph-sdk-go/users"
)

var userId string

func NewGraphClient() (*GraphHelper, error) {
	// TODO: Fix Graph –
	return &GraphHelper{}, nil

	g := NewGraphHelper()
	err := g.loadClient()
	if err != nil {
		return nil, err
	}

	return &GraphHelper{service: g.service}, nil
}

func (g *GraphHelper) getUserId() (graphmodels.UserCollectionResponseable, error) {
	if userId != "" {
		return nil, nil
	}

	var topValue int32 = 25
	query := users.UsersRequestBuilderGetQueryParameters{
		Select:  []string{"displayName", "mail", "id"},
		Top:     &topValue,
		Orderby: []string{"displayName"},
	}

	u, err := g.service.Users().Get(context.Background(),
		&users.UsersRequestBuilderGetRequestConfiguration{
			QueryParameters: &query,
		})
	if err != nil {
		return nil, err
	}

	userId = *u.GetValue()[0].GetId()

	return nil, nil
}

// GetFolders get all folders
func (g *GraphHelper) GetFolders() (graphmodels.MailFolderCollectionResponseable, error) {
	g.getUserId()

	query := users.ItemMailfoldersMailFoldersRequestBuilderGetRequestConfiguration{
		QueryParameters: &users.ItemMailfoldersMailFoldersRequestBuilderGetQueryParameters{
			Select: []string{"displayName", "id"},
		},
	}

	toReturn, err := g.service.Users().ByUserId(userId).MailFolders().
		Get(context.TODO(), &query)

	if err != nil {
		return nil, err
	}

	return toReturn, err
}

func (g *GraphHelper) GetMessages() (graphmodels.MessageCollectionResponseable, error) {
	_, err := g.getUserId()
	if err != nil {
		return nil, err
	}

	_, err = g.GetFolders()
	if err != nil {
		return nil, err
	}

	var topValue int32 = 25
	query := users.ItemMailfoldersItemMessagesRequestBuilderGetQueryParameters{
		// Only request specific properties
		Select: []string{"from", "isRead", "receivedDateTime", "subject"},
		// Get at most 25 results
		Top: &topValue,
		// Sort by received time, newest first
		Orderby: []string{"receivedDateTime DESC"},
	}

	toReturn, err := g.service.Users().ByUserId(userId).MailFolders().
		ByMailFolderId("inbox").
		Messages().
		Get(context.Background(),
			&users.ItemMailfoldersItemMessagesRequestBuilderGetRequestConfiguration{
				QueryParameters: &query,
			})

	if err != nil {
		return nil, err
	}

	return toReturn, err

}

func (g *GraphHelper) SendMessage(message *graphmodels.Message) error {
	_, err := g.getUserId()
	if err != nil {
		return err
	}

	sendMailBody := users.NewItemSendmailSendMailPostRequestBody()
	sendMailBody.SetMessage(message)

	err = g.service.Users().ByUserId(userId).SendMail().Post(context.Background(), sendMailBody, nil)
	if err != nil {
		return err
	}

	return nil
}

type GraphHelper struct {
	clientSecretCredential *azidentity.ClientSecretCredential
	service                *msgraphsdk.GraphServiceClient
}

func NewGraphHelper() *GraphHelper {
	g := &GraphHelper{}
	return g
}

func (g *GraphHelper) loadClient() error {
	file, err := os.ReadFile("client_secretms.json")
	if err != nil {
		return err
	}

	var tempJson struct {
		ClientID     string `json:"client_id"`
		TenantID     string `json:"tenant_id"`
		ClientSecret string `json:"client_secret"`
	}
	err = json.Unmarshal(file, &tempJson)
	if err != nil {
		return err
	}

	cred, err := azidentity.NewClientSecretCredential(
		tempJson.TenantID,
		tempJson.ClientID,
		tempJson.ClientSecret,
		nil,
	)
	if err != nil {
		return err
	}

	g.clientSecretCredential = cred

	authProvider, err := auth.NewAzureIdentityAuthenticationProviderWithScopes(g.clientSecretCredential, []string{
		"https://graph.microsoft.com/.default",
	})
	if err != nil {
		return err
	}

	adapter, err := msgraphsdk.NewGraphRequestAdapter(authProvider)
	if err != nil {
		return err
	}

	client := msgraphsdk.NewGraphServiceClient(adapter)
	g.service = client

	return nil
}
