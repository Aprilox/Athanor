//go:build darwin

package main

import (
	"fmt"
	"os"
	"testing"

	"github.com/ebitengine/purego/objc"
)

// On a Mac (the CI's macOS run): ATHANOR_ESSAI_COCOA=1 go test -run TestCocoa . builds Athanor.app's
// menus, its icon near the clock, its delegate and its WebKit window on the first thread (Cocoa wants it),
// then does what the Dock and the window's red button do.
var resultatCocoa = "not run"

func TestMain(m *testing.M) {
	if os.Getenv("ATHANOR_ESSAI_COCOA") == "1" {
		resultatCocoa = essaiCocoa()
	}
	os.Exit(m.Run())
}

func essaiCocoa() string {
	app, delegue, ok := preparerAppMac(actionsAppMac{
		lien:       func() string { return "about:blank" },
		navigateur: func() {},
		pilotes:    func() []string { return []string{"sky-runner"} },
		quitter:    func(bool) {},
		langue:     func() string { return "fr" },
	})
	if !ok {
		return "Cocoa not reached"
	}
	if app.Send(sel("mainMenu")) == 0 || app.Send(sel("mainMenu")).Send(sel("numberOfItems")) != 3 {
		return "menu bar: not Athanor, Edit, Window"
	}
	if mac.icone == 0 {
		return "no icon near the clock"
	}
	menu := delegue.Send(sel("applicationDockMenu:"), app)
	if menu == 0 || int(menu.Send(sel("numberOfItems"))) != 3 {
		return "Dock menu: not the pilots, a line, Open"
	}
	barre := classe("NSMenu").Send(sel("new"))
	delegue.Send(sel("menuNeedsUpdate:"), barre)
	if n := int(barre.Send(sel("numberOfItems"))); n != 5 {
		return fmt.Sprintf("menu near the clock: %d items", n)
	}
	if !mac.webkit {
		return "WebKit not reached"
	}
	// a click on the Dock icon: the window, in WebKit
	if objc.Send[bool](delegue, sel("applicationShouldHandleReopen:hasVisibleWindows:"), app, false) {
		return "reopen answered yes"
	}
	if mac.fenetre == 0 || mac.vue == 0 {
		return "no window"
	}
	if !objc.Send[bool](mac.fenetre, sel("isVisible")) {
		return "the window is not shown"
	}
	// the red button: hidden, not closed
	if objc.Send[bool](delegue, sel("windowShouldClose:"), mac.fenetre) {
		return "the red button closes the window"
	}
	if objc.Send[bool](mac.fenetre, sel("isVisible")) {
		return "the red button left the window shown"
	}
	// shown again from the icon near the clock
	delegue.Send(sel("ouvrirAthanor:"), objc.ID(0))
	if !objc.Send[bool](mac.fenetre, sel("isVisible")) {
		return "Open Athanor showed nothing"
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
