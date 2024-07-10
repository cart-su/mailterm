package ui

import (
	"cartsu/mailterm/api"
	"encoding/json"
	"fmt"
	"os"

	"github.com/rivo/tview"
)

var form *tview.Form
var baseDir string

func ShowWelcomePage(app *tview.Application) (*api.EmailClient, error) {
	welcomePage := tview.NewForm()
	form = welcomePage
	form.SetBorder(true)
	form.SetTitle("Welcome to MailTerm-Go")
	form.SetTitleAlign(tview.AlignCenter)

	var selectedService string

	form.AddDropDown("Select your email service:", []string{"Gmail", "Microsoft Graph", "IMAP"}, 0, func(option string, index int) {
		selectedService = option
	}).SetButtonsAlign(tview.AlignCenter)

	ctr := 0
	form.AddButton("Continue", func() {
		switch selectedService {
		case "Gmail":
			if !configExists("gmail") {
				if ctr == 1 {
					showWarning(app, "Trying to authenticate...")
					var config *api.Config
					f, err := os.ReadFile(fmt.Sprintf("%s/config.json", baseDir))
					if err != nil {
						config = &api.Config{
							SelectedService: "gmail",
						}
						if err := api.SaveConfig(config); err != nil {
							showError(app, fmt.Sprintf("Error saving config file: %v", err))
							return
						}
						return
					}
					err = json.Unmarshal(f, &config)
					if err != nil {
						showError(app, fmt.Sprintf("Error parsing config file: %v", err))
						return
					}
					config.SelectedService = "gmail"
					if err := api.SaveConfig(config); err != nil {
						showError(app, fmt.Sprintf("Error saving config file: %v", err))
						return
					}

					app.Stop()
				}
				showWarning(app, `Configuration file not found. Please set up your config.json file.
					For more information, visit: https://github.com/cartsu/mailterm-go
					Press continue again to automatically configure your credentials.
					Please sign in to your Gmail account when prompted.`)
				ctr++
				return
			}
		case "Microsoft Graph":
			if !configExists("graph") {
				showWarning(app, `Configuration file not found. Please set up your config.json file.
					For more information, visit: https://github.com/cartsu/mailterm-go
					This may be automatically done in the future.`)
				return
			}
		case "IMAP":
			if !configExists("imap") {
				if err := promptIMAPCredentials(app, form); err != nil {
					showError(app, fmt.Sprintf("Error saving IMAP credentials: %v", err))
					return
				}
			}
		}
		app.Stop()
	})

	form.AddButton("Quit", func() {
		app.Stop()
		os.Exit(0)
	})

	form.AddTextView("Info:", "For more information, visit: https://github.com/cartsu/mailterm-go", 0, 0, false, false)

	if err := app.SetRoot(form, true).EnableMouse(true).Run(); err != nil {
		return nil, fmt.Errorf("running welcome page: %w", err)
	}

	emailClient, err := api.NewEmailClient(selectedService)
	if err != nil {
		return nil, fmt.Errorf("creating email client: %w", err)
	}

	switch selectedService {
	case "Gmail":
		emailClient.SwitchToGmail()
	case "Microsoft Graph":
		emailClient.SwitchToGraph()
	case "IMAP":
		emailClient.SwitchToIMAP()
	}

	return emailClient, nil
}

func showWarning(app *tview.Application, message string) {
	modal := tview.NewModal().
		SetText(message).
		AddButtons([]string{"OK"}).
		SetDoneFunc(func(buttonIndex int, buttonLabel string) {
			app.SetRoot(form, true)
		})
	app.SetRoot(modal, false)
}

func showError(app *tview.Application, message string) {
	modal := tview.NewModal().
		SetText(message).
		AddButtons([]string{"OK"}).
		SetDoneFunc(func(buttonIndex int, buttonLabel string) {
			app.SetRoot(form, true)
		})
	app.SetRoot(modal, false)
}

func promptIMAPCredentials(app *tview.Application, welcomePage *tview.Form) error {
	form := tview.NewForm()
	form.SetBorder(true).SetTitle("IMAP Credentials").SetTitleAlign(tview.AlignCenter)

	var server, port, username, password string

	form.AddInputField("IMAP Server:", "", 30, nil, func(text string) { server = text })
	form.AddInputField("Port:", "", 5, nil, func(text string) { port = text })
	form.AddInputField("Username:", "", 30, nil, func(text string) { username = text })
	form.AddPasswordField("Password:", "", 30, '*', func(text string) { password = text })

	form.AddButton("Save", func() {
		var config *api.Config
		if configExists("imap") {
			config, _ = api.LoadConfig()
			if config == nil {
				showError(app, "Error loading config, please check your config.json file.")
				return
			}
		}

		config = &api.Config{}

		config.SelectedService = "imap"

		config.IMAP = api.IMAPConfig{
			Server:   server + ":" + port,
			Username: username,
			Password: password,
		}
		api.SaveConfig(config)
	})

	form.AddButton("Cancel", func() {
		app.SetRoot(welcomePage, true)
	})

	if err := app.SetRoot(form, true).Run(); err != nil {
		return fmt.Errorf("running IMAP credentials form: %w", err)
	}

	return nil
}

func configExists(activeService string) bool {
	var config api.Config
	switch activeService {
	case "gmail":
		err := api.CheckToken()
		if err != nil {
			return false
		}
		return true
	case "graph":
		if !fileExists(fmt.Sprintf("%s/config.json", baseDir)) {
			return false
		}
		if !credentialsExist(config.Graph) {
			return false
		}
	case "imap":
		if !fileExists(fmt.Sprintf("%s/config.json", baseDir)) {
			return false
		}
		if !credentialsExist(config.IMAP) {
			return false
		}
	default:
		return false
	}

	return true
}

func credentialsExist(credentials interface{}) bool {
	if credentials == nil {
		return false
	}
	switch c := credentials.(type) {
	case api.GmailConfig:
		if c.Installed.ClientID != "" ||
			c.Installed.ProjectID != "" ||
			c.Installed.AuthURI != "" ||
			c.Installed.ClientSecret != "" {
			return true
		}
		return false
	case api.GraphConfig:
		if c.ClientId != "" ||
			c.ClientSecret != "" ||
			c.TenantID != "" {
			return true
		}
		return false
	case api.IMAPConfig:
		if c.Server != "" ||
			c.Username != "" ||
			c.Password != "" {
			return true
		}
		return false
	}
	return false
}

func fileExists(dir string) bool {
	baseDir = os.Getenv("MAILTERM_HOME")
	_, err := os.Stat(dir)
	return !os.IsNotExist(err)
}
