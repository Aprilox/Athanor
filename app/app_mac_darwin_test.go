//go:build darwin

package main

import (
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/ebitengine/purego/objc"
)

// On a Mac (the CI's macOS run): ATHANOR_ESSAI_COCOA=1 go test -run TestCocoa . builds Athanor.app's
// Dock icon, menus and delegate on the first thread (Cocoa wants it), then asks Cocoa for the Dock menu
// and a click on the icon, as the Dock would.
var resultatCocoa = "not run"

func TestMain(m *testing.M) {
	if os.Getenv("ATHANOR_ESSAI_COCOA") == "1" {
		resultatCocoa = essaiCocoa()
	}
	os.Exit(m.Run())
}

func essaiCocoa() string {
	ouvert := make(chan bool, 1)
	app, delegue, ok := preparerAppMac(actionsAppMac{
		ouvrir:  func() { ouvert <- true },
		pilotes: func() []string { return []string{"sky-runner"} },
		quitter: func(bool) {},
		langue:  func() string { return "fr" },
	})
	if !ok {
		return "Cocoa not reached"
	}
	menu := delegue.Send(sel("applicationDockMenu:"), app)
	if menu == 0 {
		return "no Dock menu"
	}
	if n := int(menu.Send(sel("numberOfItems"))); n != 2 {
		return fmt.Sprintf("Dock menu: %d items", n)
	}
	titre := menu.Send(sel("itemAtIndex:"), 0).Send(sel("title")).Send(sel("UTF8String"))
	if titre == 0 {
		return "no title"
	}
	if r := objc.Send[bool](delegue, sel("applicationShouldHandleReopen:hasVisibleWindows:"), app, false); r {
		return "reopen answered yes"
	}
	select {
	case <-ouvert:
	case <-time.After(3 * time.Second):
		return "the click on the icon opened nothing"
	}
	if app.Send(sel("mainMenu")) == 0 {
		return "no menu bar"
	}
	return "ok"
}

func TestCocoa(t *testing.T) {
	if os.Getenv("ATHANOR_ESSAI_COCOA") != "1" {
		t.Skip("ATHANOR_ESSAI_COCOA=1 on a Mac")
	}
	if resultatCocoa != "ok" {
		t.Fatal(resultatCocoa)
	}
}
