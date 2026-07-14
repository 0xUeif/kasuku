package main

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"syscall"
	"unsafe"
)

// Global variable to store the original terminal settings.
// We need this so we can restore the terminal to its normal state when our program exits.
var origTermios syscall.Termios

type editorRow struct {
	size  int
	chars string
}

type editorConfig struct {
	cx, cy     int
	screenRows int
	screenCols int
	rows       []editorRow
}

var E editorConfig

// initEditor initializes all the fields in the global editorConfig struct E.
func initEditor() {
	E.cx = 0
	E.cy = 0
	cols, rows, err := getWindowSize()
	if err != nil {
		fmt.Println("Error getting window size:", err)
		os.Exit(1)
	}
	E.screenCols = cols
	E.screenRows = rows
}

// ctrlKey converts a normal letter key into its control key counterpart by masking bits.
func ctrlKey(k byte) byte {
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
)


func readKey() (rune, error) {
    buf := make([]byte, 1)
    //wait for buffer to be filled with 1 byte
    for {
        n, err := os.Stdin.Read(buf)
        if err != nil && err != io.EOF {
            return 0, err
        }
        if n == 1 {
            break // process byte now
        }
    }

    if buf[0] == '\x1b' {
        seq := make([]byte, 2)
        
        n, err := os.Stdin.Read(seq[:1])
        if n != 1 || err != nil {
            return '\x1b', nil //escape sequence not complete, return ESC
		}
        
        n, err = os.Stdin.Read(seq[1:])
        if n != 1 || err != nil {
            return '\x1b', nil 
        }
        //move cursor based on the escape sequence
        if seq[0] == '[' {
            switch seq[1] {
            case 'A':
                return arrowUp, nil
            case 'B':
                return arrowDown, nil
            case 'C':
                return arrowRight, nil
            case 'D':
                return arrowLeft, nil
            }
        }
        
        // If it was an escape sequence we don't recognize, just return ESC
        return '\x1b', nil
    } 

    // Not an escape sequence, just return the standard character
    return rune(buf[0]), nil
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
	raw.Cc[syscall.VMIN] = 0
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

// refreshScreen clears the screen, draws the rows, and repositions the cursor.
func refreshScreen() error {
	var buf bytes.Buffer
	// Hide the cursor
	buf.WriteString("\x1b[?25l")
	// Reset cursor to top-left
	buf.WriteString("\x1b[H")

	// Draw rows (either file contents or tildes)
	editorDrawRows(&buf)

	// Position the cursor at the user's current coordinates
	fmt.Fprintf(&buf, "\x1b[%d;%dH", E.cy+1, E.cx+1) // ANSI escape sequences are 1-based

	// Show the cursor again
	buf.WriteString("\x1b[?25h")

	// Write the entire buffer to standard output
	_, err := os.Stdout.Write(buf.Bytes())
	return err
}

// moveCursor updates E.cx and E.cy to move the cursor based on the arrow key input.
func moveCursor(key rune) {
	switch key {
	case arrowLeft:
		if E.cx > 0 {
			E.cx--
		}
	case arrowRight:
		if E.cx < E.screenCols-1 {
			E.cx++
		}
	case arrowUp:
		if E.cy > 0 {
			E.cy--
		}
	case arrowDown:
		if E.cy < E.screenRows-1 {
			E.cy++
		}
	}
}

// editorOpen opens a file, reads its contents line-by-line, and appends them to the editor rows.
//
// ELI5 (Explain Like I'm 5):
// A file is like a notebook stored on your computer's hard drive. To read it, our program asks the operating
// system for a "file descriptor"—a temporary ticket that lets us look inside. We use a "scanner" to scan through 
// the file line-by-line. If we forget to close the file when we are done, the computer keeps that ticket active,
// which eventually wastes resources. That's why we use "defer" to close it automatically.
//
// Go vs Python / Go vs C Differences:
// 1. In Python, you open files using `with open(filename) as f:`, which closes the file automatically. 
//    In Go, we do this using the `defer` keyword (e.g. `defer file.Close()`) right after successfully opening the file. 
//    It ensures the file is closed the moment `editorOpen` returns.
// 2. Go handles line reading efficiently using `bufio.NewScanner`.
//
// Explicit Implementation Steps:
// 1. Call `os.Open(filename)`. If it returns an error, return that error.
// 2. Immediately call `defer file.Close()` to ensure the file gets cleaned up.
// 3. Create a scanner: `scanner := bufio.NewScanner(file)`.
// 4. Loop while `scanner.Scan()` returns true:
//    - Get the line string: `line := scanner.Text()`.
//    - Create an `editorRow` instance with `size: len(line)` and `chars: line`.
//    - Append the new row to the global slice: `E.rows = append(E.rows, row)`.
// 5. After the loop, check if `scanner.Err()` returned any error. If it did, return that error.
// 6. Return `nil` to signal success.
func editorOpen(filename string) error {
	//panic("TODO: implement editorOpen")
	file, err := os.Open(filename)
	if err != nil {
		return err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)

	for scanner.Scan() {
		line := scanner.Text()

		row := editorRow{
			size:  len(line),
			chars: line,
		}
		E.rows = append(E.rows, row)


	}
	if err := scanner.Err(); err != nil {
		return err
	}
	return nil
}

// editorDrawRows draws the file contents or tildes to the screen buffer.
func editorDrawRows(buf *bytes.Buffer) {
	for y := 0; y < E.screenRows; y++ {
		if y < len(E.rows) {
			line := E.rows[y].chars
			if len(line) > E.screenCols {
				line = line[:E.screenCols]
			}
			buf.WriteString(line)
		} else {
			buf.WriteString("~")
		}
		buf.WriteString("\x1b[K") // Clear to end of line
		if y < E.screenRows-1 {
			buf.WriteString("\r\n")
		}
	}
}

func main() {

	err := enableRawMode()
	if err != nil {
		fmt.Println("Error enabling raw mode:", err)
		return
	}
	defer disableRawMode()

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
		if c == rune(ctrlKey('q')) { // Exit on Ctrl-Q
			break
		}
		moveCursor(c)
	}
	err = clearScreen()
	if err != nil {
		fmt.Println("Error clearing screen:", err)
		return
	}
}
