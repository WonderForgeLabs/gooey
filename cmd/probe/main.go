// probe reports which rendering modes this terminal supports.
package main

import (
	"fmt"
	"os"

	"github.com/WonderForgeLabs/gooey/term"
)

func main() {
	s, err := term.Open()
	if err != nil {
		fmt.Fprintln(os.Stderr, "no tty:", err)
		os.Exit(1)
	}
	caps, err := s.Detect()
	s.File().Close()
	if err != nil {
		fmt.Fprintln(os.Stderr, "detect:", err)
		os.Exit(1)
	}
	fmt.Printf("terminal: %s / %s\n", os.Getenv("TERM"), os.Getenv("TERM_PROGRAM"))
	fmt.Printf("size:     %d×%d cells", caps.Cols, caps.Rows)
	if caps.CellW > 0 {
		fmt.Printf(", cell %d×%d px", caps.CellW, caps.CellH)
	}
	fmt.Println()
	fmt.Printf("kitty:    %v\n", caps.Kitty)
	fmt.Printf("sixel:    %v\n", caps.Sixel)
	fmt.Printf("iterm2:   %v\n", caps.ITerm2)
	fmt.Printf("selected: %s\n", caps.Best())
}
