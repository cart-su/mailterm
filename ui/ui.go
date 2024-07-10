package ui

import (
	"cartsu/mailterm/api"
	"context"
	"fmt"
	"log"
	"strconv"
	"time"

	"github.com/emersion/go-imap"
	"github.com/gdamore/tcell/v2"
	graphmodels "github.com/microsoftgraph/msgraph-sdk-go/models"
	"github.com/rivo/tview"
	"google.golang.org/api/gmail/v1"
)

const (
	KeyQuit       = 'q'
	KeyReply      = 'r'
	KeyForward    = 'f'
	KeyDelete     = 'd'
	KeyNew        = 'n'
	KeySettings   = tcell.KeyTab
	RefreshPeriod = 10 * time.Second
)

var settingsVisible = false

var ui InterfaceConfig

type InterfaceConfig struct {
	App         *tview.Application
	Client      *api.EmailClient
	BaseDir     string
	AutoRefresh bool
}

func InitializeInterface(uiConf InterfaceConfig) error {
	ui = uiConf

	var emailClient *api.EmailClient
	var err error

	if !fileExists(fmt.Sprintf("%s/config.json", ui.BaseDir)) {
		emailClient, err = ShowWelcomePage(ui.App)
		if err != nil {
			return fmt.Errorf("showing welcome page: %w", err)
		}
	} else {
		config, _ := api.LoadConfig()
		emailClient, err = api.NewEmailClient(config.SelectedService)
		if err != nil {
			emailClient, err = ShowWelcomePage(ui.App)
			if err != nil {
				return fmt.Errorf("showing welcome page: %w", err)
			}
		}
	}

	ui = InterfaceConfig{
		App:         ui.App,
		Client:      emailClient,
		AutoRefresh: ui.AutoRefresh,
	}

	header := createHeader()
	statusBar := createFooter()
	emailList := createEmailList()
	messageBody := createMessageBody()
	settingsPane := createSettingsPane(emailList, statusBar)

	settingsPane.AddButton("Save", nil)
	settingsPane.SetBorder(true)
	settingsPane.SetTitle("Settings")
	settingsPane.SetBorderAttributes(tcell.AttrDim)

	leftPanel := createLeftPanel(emailList)
	rightPanel := createRightPanel(messageBody)
	contentFlex := tview.NewFlex().
		AddItem(leftPanel, 0, 2, true).
		AddItem(rightPanel, 0, 5, false)

	mainFlex := tview.NewFlex().
		AddItem(contentFlex, 0, 1, true)

	rootFlex := tview.NewFlex().
		SetDirection(tview.FlexRow).
		AddItem(header, 1, 0, false).
		AddItem(mainFlex, 0, 1, true).
		AddItem(statusBar, 1, 0, false)

	setupKeyBindings(emailList, messageBody, settingsPane, mainFlex, rootFlex)
	setupEvents(emailList, messageBody)

	populateEmailList(emailList)

	// redraw the screen every half second
	go func() {
		for {
			time.Sleep(time.Second / 2)
			ui.App.Draw()
		}
	}()

	return ui.App.SetRoot(rootFlex, true).Run()
}

func createHeader() *tview.TextView {
	return tview.NewTextView().
		SetTextAlign(tview.AlignCenter).
		SetText("MailTerm-Go")
}

func createFooter() *tview.TextView {
	statusbar := tview.NewTextView().
		SetTextAlign(tview.AlignLeft)

	switch ui.Client.ActiveService {
	case "graph", "gmail":
		statusbar.SetText("'q' quit | 'n' new | 'r' reply | 'f' forward | 'd' delete | 'tab' settings")
	case "imap":
		statusbar.SetText("'q' quit |'d' delete | 'tab' settings")
	}
	return statusbar
}

func createEmailList() *tview.List {
	return tview.NewList().
		SetSecondaryTextColor(tcell.ColorGray).
		SetMainTextColor(tcell.ColorIvory).
		SetWrapAround(true).
		ShowSecondaryText(false)
}

func createMessageBody() *tview.TextView {
	return tview.NewTextView().
		SetDynamicColors(true).
		SetRegions(true).
		SetWordWrap(true).
		SetScrollable(true)
}

func createSettingsPane(emailList *tview.List, statusbar *tview.TextView) *tview.Form {
	var initialized = false
	form := tview.NewForm().
		AddDropDown("Email Service", []string{"Gmail", "Microsoft Graph", "IMAP"}, 0, func(option string, index int) {
			if !initialized {
				initialized = true
				return
			}

			switch option {
			case "Gmail":
				if !configExists("gmail") {
					showWarning(ui.App, `Configuration file not found. Please set up your config.json file.`)
					return
				}
				ui.Client.SwitchToGmail()
				emailList.Clear()
				statusbar.SetText("'q' quit | 'n' new | 'r' reply | 'f' forward | 'd' delete | 'tab' settings")
				populateEmailList(emailList)
			case "Microsoft Graph":
				if !configExists("graph") {
					showWarning(ui.App, `Configuration file not found. Please set up your config.json file.`)
					return
				}
				ui.Client.SwitchToGraph()
				emailList.Clear()
				statusbar.SetText("'q' quit | 'n' new | 'r' reply | 'f' forward | 'd' delete | 'tab' settings")
				populateEmailList(emailList)
			case "IMAP":
				ui.Client.SwitchToIMAP()
				emailList.Clear()
				statusbar.SetText("'q' quit |'d' delete | 'tab' settings")
				populateEmailList(emailList)
			}

		}).
		AddDropDown("Themes", []string{"coming", "soon"}, 0, nil).
		AddCheckbox("Auto-refresh", false, func(checked bool) {
			toggleAutoRefresh(checked)
		})
	return form
}

func createLeftPanel(emailList *tview.List) *tview.Flex {
	leftPanel := tview.NewFlex().SetDirection(tview.FlexRow)
	leftPanel.SetBorder(true).SetTitle("Messages")
	leftPanel.SetBorderAttributes(tcell.AttrDim)
	leftPanel.AddItem(emailList, 0, 1, true)
	return leftPanel
}

func createRightPanel(messageBody *tview.TextView) *tview.Flex {
	rightPanel := tview.NewFlex().SetDirection(tview.FlexRow)
	rightPanel.SetBorder(true)
	rightPanel.SetBorderAttributes(tcell.AttrDim)
	rightPanel.AddItem(messageBody, 0, 1, false)
	return rightPanel
}

func populateEmailList(emailList *tview.List) {
	var messages interface{}
	var err error

	switch ui.Client.ActiveService {
	case "gmail":
		messages, err = ui.Client.GmailClient.GetThreads()
	case "graph":
		messages, err = ui.Client.GraphClient.GetMessages()
	case "imap":
		messages, err = ui.Client.IMAP.FetchMessages(25)
	}
	if err != nil {
		log.Printf("Unable to retrieve messages: %v", err)
		return
	}

	switch m := messages.(type) {
	case []*gmail.Thread:
		for i, message := range m {
			emailList.AddItem(message.Snippet, message.Id, rune(i), nil)
		}
	case graphmodels.MessageCollectionResponseable:
		for i, message := range m.GetValue() {
			emailList.AddItem(*message.GetSubject(), *message.GetId(), rune(i), nil)
		}
	case []*imap.Message:
		for i, message := range m {
			emailList.AddItem(message.Envelope.Subject, strconv.Itoa(int(message.Uid)), rune(i), nil)
		}
	}
}

func setupEvents(emailList *tview.List, messageBody *tview.TextView) {
	emailList.SetSelectedFunc(func(i int, mainText, secondaryText string, r rune) {
		var newContent string
		var err error

		switch ui.Client.ActiveService {
		case "gmail":
			newContent, err = renderGmailMessage(ui.Client.GmailClient, secondaryText)
		case "graph":
			newContent, err = renderGraphMessage(ui.Client.GraphClient, secondaryText)
		case "imap":
			uid, err := strconv.Atoi(secondaryText)
			if err != nil {
				return
			}
			newContent, err = ui.Client.IMAP.GetMessageBody(uint32(uid))
			if err != nil {
				return
			}
		}

		if err != nil {
			newContent = fmt.Sprintf("Error displaying message: %v", err)
		}
		messageBody.Clear()
		messageBody.SetText(newContent)
		messageBody.ScrollToBeginning()
		ui.App.SetFocus(messageBody)
	})
}

func renderGmailMessage(client *api.GmailClient, messageId string) (string, error) {
	renderedMessage, err := client.GetMessageBody(messageId)
	if err != nil {
		return "", err
	}
	return renderedMessage, nil
}

func renderGraphMessage(client *api.GraphHelper, messageId string) (string, error) {
	// TODO: Add support for Graph
	return "", nil
}

func setupKeyBindings(emailList *tview.List, messageBody *tview.TextView, settingsPane *tview.Form, mainFlex *tview.Flex, rootFlex *tview.Flex) {
	emailList.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case KeySettings:
			toggleSettingsPane(emailList, mainFlex, settingsPane)
		case tcell.KeyRune:
			switch event.Rune() {
			case KeyNew:
				composePage := createComposePage(rootFlex, "")
				ui.App.SetRoot(composePage, true)
			case KeyQuit:
				ui.App.Stop()
			case KeyReply:
				_, messageId := emailList.GetItemText(emailList.GetCurrentItem())
				composePage := createComposePage(rootFlex, messageId)
				ui.App.SetRoot(composePage, true)
			case KeyDelete:
				switch ui.Client.ActiveService {
				case "gmail":
					_, emailId := emailList.GetItemText(emailList.GetCurrentItem())
					if emailId != "" {
						ui.Client.GmailClient.TrashMessage(emailId)
					}
				}

			}
		}

		return event
	})

	settingsPane.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyEscape:
			toggleSettingsPane(emailList, mainFlex, settingsPane)
		case tcell.KeyDown:
			currentItem, _ := settingsPane.GetFocusedItemIndex()
			settingsPane.SetFocus(currentItem + 1)
		case tcell.KeyUp:
			currentItem, _ := settingsPane.GetFocusedItemIndex()
			settingsPane.SetFocus(currentItem - 1)
		case tcell.KeyRune:
			switch event.Rune() {
			case KeyQuit:
				ui.App.Stop()
			}
		}
		return event
	})

	messageBody.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyEscape:
			ui.App.SetFocus(emailList)
		case KeySettings:
			toggleSettingsPane(emailList, mainFlex, settingsPane)
		case tcell.KeyRune:
			switch event.Rune() {
			case KeyQuit:
				ui.App.Stop()
			case KeyReply:
				switch ui.Client.ActiveService {
				case "graph", "gmail":
					ui.App.SetFocus(emailList)
					_, messageId := emailList.GetItemText(emailList.GetCurrentItem())
					composePage := createComposePage(rootFlex, messageId)
					ui.App.SetRoot(composePage, true)
				}
			}
		default:
		}
		return event
	})
}

func toggleSettingsPane(emailList *tview.List, mainFlex *tview.Flex, settingsPane *tview.Form) {
	if !settingsVisible && !settingsPane.HasFocus() {
		mainFlex.AddItem(settingsPane, 0, 1, false)
		ui.App.SetFocus(settingsPane)
	} else {
		mainFlex.RemoveItem(settingsPane)
		ui.App.SetFocus(emailList)
	}
	settingsVisible = !settingsVisible

}

func createComposePage(rootFlex *tview.Flex, emailId string) *tview.Flex {
	composePage := tview.NewFlex().SetDirection(tview.FlexRow)

	var sender string
	var subject string
	if emailId != "" {
		// get sender
		sender, subject = getSenderAndSubject(emailId, ui.Client)
	}

	// Set up form
	form := tview.NewForm()

	toField := tview.NewInputField().SetLabel("To: ").SetFieldWidth(40)
	if sender != "" {
		toField.SetText(sender)
	}
	ccField := tview.NewInputField().SetLabel("Cc: ").SetFieldWidth(40)
	bccField := tview.NewInputField().SetLabel("Bcc: ").SetFieldWidth(40)

	subjectField := tview.NewInputField().SetLabel("Subject: ").SetFieldWidth(40)
	if subject != "" {
		subjectField.SetText(fmt.Sprintf("Re: %s", subject))
	}

	bodyField := tview.NewTextArea().
		SetLabel("Body: ").
		SetText("", false)

	form.AddFormItem(toField)
	form.AddFormItem(ccField)
	form.AddFormItem(bccField)
	form.AddFormItem(subjectField)
	form.AddFormItem(bodyField)

	// Add buttons
	form.AddButton("Send", func() {
		if ui.Client.ActiveService == "gmail" {
			var email = api.Message{
				To:      toField.GetText(),
				From:    "me",
				Subject: subjectField.GetText(),
				Body:    bodyField.GetText(),
			}

			message := ui.Client.GmailClient.PrepareMessageForSending(email)

			err := ui.Client.GmailClient.SendMessage(message)
			if err != nil {
				showWarning(ui.App, fmt.Sprintf("Error sending message: %s", err.Error()))
			}
		}
		ui.App.SetRoot(rootFlex, true)
	})

	form.AddButton("Cancel", func() {
		ui.App.SetRoot(rootFlex, true)
	})

	// Set up form appearance
	form.SetBorder(true).
		SetTitle("Compose Email").
		SetTitleAlign(tview.AlignLeft)

	composePage.AddItem(form, 0, 1, true)

	// Handle input
	composePage.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyEscape:
			ui.App.SetRoot(rootFlex, true)
			return nil
		}
		return event
	})

	return composePage
}

func getSenderAndSubject(id string, client *api.EmailClient) (string, string) {
	var sender string
	var subject string

	message, err := client.GmailClient.GetMessageMetadata("me", id)
	if err != nil {
		return sender, subject
	}

	for _, header := range message.Payload.Headers {
		if header.Name == "Reply-To" {
			sender = header.Value
		}
		if header.Name == "Subject" {
			subject = header.Value
		}
	}
	return sender, subject
}

func autoRefresh(ctx context.Context, emailList *tview.List) {
	ticker := time.NewTicker(RefreshPeriod)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			ui.App.QueueUpdateDraw(func() {
				populateEmailList(emailList)
			})
		}
	}
}

func toggleAutoRefresh(isAutoRefresh bool) {
	ui.AutoRefresh = isAutoRefresh
	if isAutoRefresh {
		go autoRefresh(context.Background(), createEmailList())
	}
}
