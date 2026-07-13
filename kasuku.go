package main

import (
	"fmt"
	"io"
	"os"
	"syscall"
	"unsafe"
)

// Global variable to store the original terminal settings.
// We need this so we can restore the terminal to its normal state when our program exits.
var origTermios syscall.Termios

// ctrlKey converts a normal letter key into its control key counterpart by masking bits.
func ctrlKey(k byte) byte {
	return k & 0x1f
}

// readKey reads a single byte from the standard input (os.Stdin).
func readKey() (byte, error) {
	buf := make([]byte, 1)
	n, err := os.Stdin.Read(buf)
	if err != nil {
		return 0, err
	}
	if n == 0 {
		return 0, io.EOF
	}
	return buf[0], nil
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
	raw.Cc[syscall.VMIN] = 1  // Wait for at least 1 byte of input before returning from read.
	raw.Cc[syscall.VTIME] = 0 // No timeout; wait indefinitely for input.

	_, _, err = syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), uintptr(syscall.TCSETS), uintptr(unsafe.Pointer(&raw)))
	if err != 0 {
		return err
	}
	return nil

}

// disableRawMode restores the terminal back to its original "cooked" state.
func disableRawMode() error {
	// panic("TODO: implement disableRawMode")
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

func main() {
	err := clearScreen()
	if err != nil {
		fmt.Println("Error clearing screen:", err)
		return
	}
	err = enableRawMode()
	if err != nil {
		fmt.Println("Error enabling raw mode:", err)
		return
	}
	defer disableRawMode()

	for {
		c, err := readKey()
		if err != nil {
			fmt.Println("Error reading key:", err)
			break
		}
		if c == ctrlKey('q') { // Exit on Ctrl-Q
			break
		}
		fmt.Printf("You pressed: %c (byte value: %d)\n", c, c)
	}
	err = clearScreen()
	if err != nil {
		fmt.Println("Error clearing screen:", err)
		return
	}
}
