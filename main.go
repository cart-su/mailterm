package main

import (
	"cartsu/mailterm/ui"
	"fmt"
	"log"
	"os"

	"github.com/rivo/tview"
)

func main() {
	uiConfig := ui.InterfaceConfig{
		App:         tview.NewApplication(),
		AutoRefresh: true,
	}

	err := os.Setenv("MAILTERM_HOME", fmt.Sprintf("%s/.mailterm", os.Getenv("HOME")))
	if err != nil {
		log.Fatal(err)
	}
	// make directory if it doesn't exist
	baseDir := os.Getenv("MAILTERM_HOME")
	if _, err := os.Stat(baseDir); os.IsNotExist(err) {
		err = os.Mkdir(baseDir, 0755)
		if err != nil {
			log.Fatal(err)
		}
	}
	uiConfig.BaseDir = baseDir

	err = ui.InitializeInterface(uiConfig)
	if err != nil {
		log.Fatal(err)
	}
}
