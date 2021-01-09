package main

import (
	"fmt"

	"github.com/icza/gowut/gwu"
)

func main() {
	win := gwu.NewWindow("main", "Backer viewer")
	win.Style().SetFullWidth()

	// Create and start a GUI server (omitting error check)
	// TODO: port choice - randomize or take flag
	server := gwu.NewServer("backer-viewer", "localhost:8081")
	server.SetText("Backer viewer app")
	server.AddWin(win)
	server.Start("main")
}
