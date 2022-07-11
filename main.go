package main

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"github.com/fairdatasociety/swarm-mobile/internal/screens"
)

func main() {
	a := app.NewWithID("com.plur.beemobile")
	w := a.NewWindow("SwarmMobile")
	w.SetMaster()

	w.Resize(fyne.NewSize(390, 422))
	w.SetFixedSize(true)
	w.SetContent(screens.Make(a, w))
	w.ShowAndRun()
}
