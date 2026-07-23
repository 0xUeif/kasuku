package main

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

// Global variable to store the original terminal settings.
// We need this so we can restore the terminal to its normal state when our program exits.
var origTermios syscall.Termios

type editorRow struct {
	size  int
	chars string
	render string
}

type editorConfig struct {
	cx, cy     int
	rowoff     int
	coloff     int
	screenRows int
	screenCols int
	rows       []editorRow
	filename   string
	statusMsg string
	statusMsgTime time.Time
}

var E editorConfig

// initEditor initializes all the fields in the global editorConfig struct E.
func initEditor() {
	E.cx = 0
	E.cy = 0
	E.rowoff = 0
	E.coloff = 0
	cols, rows, err := getWindowSize()
	if err != nil {
		fmt.Println("Error getting window size:", err)
		os.Exit(1)
	}
	E.screenCols = cols
	E.screenRows = rows -2
}

// ctrlKey converts a normal letter key into its control key counterpart by masking bits.
func ctrlKey(k rune) rune {
	return k & 0x1f
}

type winsize struct {
	ws_row    uint16
	ws_col    uint16
	ws_xpixel uint16
	ws_ypixel uint16
}

// getWindowSize queries the terminal size (rows and columns) using the ioctl system call.
// `syscall.TIOCGWINSZ` (Terminal I/O Control Get Window Size), request code for window size
//   - `syscall.SYS_IOCTL` (to invoke the ioctl syscall)
//   - `uintptr(syscall.Stdout)` (representing the standard output file descriptor)
//   - `uintptr(syscall.TIOCGWINSZ)` (the command to get terminal window size)
//   - `uintptr(unsafe.Pointer(&ws))` (the memory address where the OS will write the size)

func getWindowSize() (int, int, error) {
	ws := winsize{}
	_, _, err := syscall.Syscall(syscall.SYS_IOCTL, uintptr(syscall.Stdout), uintptr(syscall.TIOCGWINSZ), uintptr(unsafe.Pointer(&ws)))
	if err != 0 {
		return 0, 0, err
	} else if ws.ws_col == 0 {
		return 0, 0, fmt.Errorf("invalid terminal width")
	}
	return int(ws.ws_col), int(ws.ws_row), nil

}

const (
	arrowLeft = iota + 1000
	arrowRight
	arrowUp
	arrowDown
	pageUp
	pageDown
	homeKey
	endKey
	delKey
)

func readByte() (byte, error) {
	buf := make([]byte, 1)
	for {
		n, err := os.Stdin.Read(buf)
		if err != nil {
			if err == io.EOF {
				continue // No data yet, keep waiting
			}
			return 0, err
		}
		if n == 0 {
			continue // No data yet, keep waiting
		}
		return buf[0], nil
	}
}

// readKey reads a single keypress, now parsing Page Up/Down, Home, End, and Delete.
func readKey() (rune, error) {
	b, err := readByte()
	if err != nil {
		return 0, err
	}
	if b != '\x1b' {
		return rune(b), nil
	}
	seq1, err := readByte()
	if err != nil {
		return 0, err
	}
	seq2, err := readByte()
	if err != nil {
		return 0, err
	}

	switch seq1 {
	case '[':
		if seq2 >= '0' && seq2 <= '9' {
			tilde, err := readByte()
			if err != nil || tilde != '~' { // Expecting a tilde after the number for certain keys
				return 0, err
			}
			switch seq2 {
			case '1', '7':
				return homeKey, nil
			case '3':
				return delKey, nil
			case '4', '8':
				return endKey, nil
			case '5':
				return pageUp, nil
			case '6':
				return pageDown, nil
			}

		} else {
			switch seq2 {
			case 'A':
				return arrowUp, nil
			case 'B':
				return arrowDown, nil
			case 'C':
				return arrowRight, nil
			case 'D':
				return arrowLeft, nil
			case 'H':
				return homeKey, nil
			case 'F':
				return endKey, nil
			}
		}
	case 'O':
		switch seq2 {
		case 'H':
			return homeKey, nil 
		case 'F':
			return endKey, nil
		}
	}

	return '\x1b', nil // Unrecognized escape sequence, default to returning ESC
}

// enableRawMode saves the current terminal state and puts the terminal into "raw mode".
func enableRawMode() error {
	fd := int(os.Stdin.Fd())
	_, _, err := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), uintptr(syscall.TCGETS), uintptr(unsafe.Pointer(&origTermios)))
	if err != 0 {
		return err
	}
    
	raw := origTermios
	raw.Iflag = raw.Iflag &^ (syscall.BRKINT | syscall.ICRNL | syscall.INPCK | syscall.ISTRIP | syscall.IXON)
	raw.Oflag = raw.Oflag &^ syscall.OPOST
	raw.Cflag = raw.Cflag | syscall.CS8
	raw.Lflag = raw.Lflag &^ (syscall.ECHO | syscall.ICANON | syscall.IEXTEN | syscall.ISIG)
	raw.Cc[syscall.VMIN] = 0 // return as soon as there is input
	raw.Cc[syscall.VTIME] = 1 // wait for 100ms

	_, _, err = syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), uintptr(syscall.TCSETS), uintptr(unsafe.Pointer(&raw)))
	if err != 0 {
		return err
	}
	return nil

}

// disableRawMode restores the terminal back to its original "cooked" state.
func disableRawMode() error {
	fd := int(os.Stdin.Fd())
	_, _, err := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), uintptr(syscall.TCSETS), uintptr(unsafe.Pointer(&origTermios)))
	if err != 0 {
		return err
	}
	return nil
}

// clearScreen clears the terminal screen and repositions the cursor to the top-left.
func clearScreen() error {
	_, err := os.Stdout.Write([]byte("\x1b[2J"))
	if err != nil {
		return err
	}
	_, err = os.Stdout.Write([]byte("\x1b[H"))
	if err != nil {
		return err
	}
	return nil
}

// editorScroll calculates the vertical and horizontal scrolling offsets based on the cursor position.
func editorScroll() {
	if E.cy < E.rowoff { // If the cursor is above the visible window, scroll up
		E.rowoff = E.cy
	}
	if E.cy >= E.rowoff+E.screenRows { // If the cursor is below the visible window, scroll down
		E.rowoff = E.cy - E.screenRows + 1
	}
	if E.cx < E.coloff { // If the cursor is to the left of the visible window, scroll left
		E.coloff = E.cx
	}
	if E.cx >= E.coloff+E.screenCols { // If the cursor is to the right of the visible window, scroll right
		E.coloff = E.cx - E.screenCols + 1
	}
}

// refreshScreen clears the screen, draws the rows, and repositions the cursor.
func refreshScreen() error {
	editorScroll()

	var buf bytes.Buffer
	// Hide the cursor
	buf.WriteString("\x1b[?25l")
	// Reset cursor to top-left
	buf.WriteString("\x1b[H")

	// Draw rows (either file contents or tildes)
	editorDrawRows(&buf)
	editorDrawStatusBar(&buf)
	editorDrawMessageBar(&buf)

	// Position the cursor at the user's current coordinates relative to the scroll offsets
	fmt.Fprintf(&buf, "\x1b[%d;%dH", (E.cy-E.rowoff)+1, (E.cx-E.coloff)+1) // ANSI escape sequences are 1-based

	// Show the cursor again
	buf.WriteString("\x1b[?25h")

	// Write the entire buffer to standard output
	_, err := os.Stdout.Write(buf.Bytes())
	return err
}

// moveCursor updates E.cx and E.cy to move the cursor based on the arrow key input, supporting line wrapping.
func moveCursor(key rune) {
	switch key {
	case arrowLeft:
		if E.cx > 0 {
			E.cx--
		} else if E.cy > 0 { 
			E.cy--
			E.cx = len(E.rows[E.cy].chars)
			
		}
	case arrowRight:
		if E.cy < len(E.rows) { 
			xlen := len(E.rows[E.cy].chars)
			if E.cx < xlen {
				E.cx++
			} else if E.cx == xlen {
				E.cy++
				E.cx = 0
			}
		}
	case arrowUp:
		if E.cy > 0 {
			E.cy--
		}
	case arrowDown:
		if E.cy < len(E.rows) {
			E.cy++
		}
	}

	var rowLen int
	if E.cy < len(E.rows) {
		rowLen = len(E.rows[E.cy].chars)
	} else {
		rowLen = 0
	}
	if E.cx > rowLen {
		E.cx = rowLen
	}
}

// editorProcessKeypress processes a keypress received from the keyboard.
// 3. Return `true` to continue the editor loop.
func editorProcessKeypress(key rune) bool {
	switch key {
	case ctrlKey('q'):
		return false
	case homeKey:
		E.cx = 0
	case endKey:
		if E.cy < len(E.rows) {
			E.cx = len(E.rows[E.cy].chars)
		}
	case pageUp, pageDown:
		var dir rune
		if key == pageUp {
			dir = arrowUp
		} else {
			dir = arrowDown
		}
		for i := 0; i < E.screenRows; i++ {
			moveCursor(dir)
		}
	case arrowLeft, arrowRight, arrowUp, arrowDown:
		moveCursor(key)
	}
	return true

}

// editorOpen opens a file, reads its contents line-by-line, and appends them to the editor rows.
func editorOpen(filename string) error {
	file, err := os.Open(filename)
	if err != nil {
		return err
	}
	defer file.Close()

	E.filename = filename

	scanner := bufio.NewScanner(file)

	for scanner.Scan() {
		line := scanner.Text()

		
		editorAppendRow(line)

	}
	if err := scanner.Err(); err != nil {
		return err
	}
	return nil
}

// editorDrawRows draws the file contents or tildes to the screen buffer, taking scroll offsets into account.
func editorDrawRows(buf *bytes.Buffer) {
	for y := 0; y < E.screenRows; y++ {
		fileRow := y + E.rowoff

		if fileRow < len(E.rows) {

			line := E.rows[fileRow].render
			if E.coloff < len(line) {
				line = line[E.coloff:]
			} else {
				line = ""
			}
			if len(line) > E.screenCols {
				line = line[:E.screenCols]
			}
			buf.WriteString(line)
		} else {
			buf.WriteString("~")
		}
		buf.WriteString("\x1b[K") // Clear to end of line
	
		buf.WriteString("\r\n")
		
	}
}

const tabWidth = 4

func editorUpdateRow(row *editorRow) {
	var builder strings.Builder

	builder.Grow(len(row.chars))
	
	idx := 0
	for _, c := range row.chars {
		if c == '\t' {
			builder.WriteByte(' ')
			idx++
			for idx%tabWidth != 0 {
				builder.WriteByte(' ')
				idx++
			}
		} else {
			builder.WriteRune(c)
			idx++
		}
	}
	row.render = builder.String()
	row.size = len(row.render)
}

func editorAppendRow(s string) {
	row := editorRow{
		size:  len(s),
		chars: s,
	}
	editorUpdateRow(&row)
	E.rows = append(E.rows, row)
}

func editorDrawStatusBar(buf *bytes.Buffer) {
	buf.WriteString("\x1b[7m") // Switch to inverted colors for the status bar\

	filename := E.filename
	if filename == "" {
		filename = "No File"
	}

	status := fmt.Sprintf("%.20s - kasuko.go - %d lines", filename, len(E.rows))
	rstatus := fmt.Sprintf("%d/%d", E.cy+1, len(E.rows))

	if len(status) > E.screenCols {
		status = status[:E.screenCols]
	}

	buf.WriteString(status)

	for len(status)+len(rstatus) < E.screenCols {
		buf.WriteByte(' ')
		status += " "
	}

	buf.WriteString(rstatus)
	buf.WriteString("\x1b[m") // Switch back to normal colors
	buf.WriteString("\r\n")
}

func editorDrawMessageBar(buf *bytes.Buffer) {	
	buf.WriteString("\x1b[K") // Clear the message bar line
	msgLen := len(E.statusMsg)
	if msgLen > E.screenCols {
		msgLen = E.screenCols
	}
	if msgLen > 0 && time.Since(E.statusMsgTime).Seconds() < 5 {
		buf.WriteString(E.statusMsg[:msgLen])
	}

}

func main() {

	err := enableRawMode()
	if err != nil {
		fmt.Println("Error enabling raw mode:", err)
		return
	}
	defer disableRawMode()

	E.statusMsg = "HELP: Ctrl-Q = quit"
    E.statusMsgTime = time.Now()

	initEditor()

	if len(os.Args) > 1 {
		err := editorOpen(os.Args[1])
		if err != nil {
			fmt.Println("Error opening file:", err)
			return
		}
	}

	for {
		refreshScreen()
		c, err := readKey()
		if err != nil {
			fmt.Println("Error reading key:", err)
			break
		}
		if !editorProcessKeypress(c) { // If editorProcessKeypress returns false, exit the loop
			break
		}
	}
	err = clearScreen()
	if err != nil {
		fmt.Println("Error clearing screen:", err)
		return
	}
}
