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

// readKey reads a single byte from the standard input (os.Stdin).
//
// ELI5 (Explain Like I'm 5):
// When you type on your keyboard, the operating system (OS) grabs those keypresses and puts
// them in a special stream called "standard input" (stdin). Stdin is like a pipe containing
// byte values. To know what you typed, our program needs to read from this pipe one byte at a time.
//
// Go vs Python vs C Differences:
// 1. In Python, you might use `sys.stdin.read(1)` which handles strings and buffers.
// 2. In C, you use the low-level `read(STDIN_FILENO, &c, 1)` system call.
// 3. In Go, we use `os.Stdin.Read(buf)`. To do this, we must pass a "slice" (a dynamically sized list)
//    of bytes. Go will modify this slice directly by filling it with the read byte.
// 4. In Go, errors are returned as explicit values (unlike Python exceptions). We must check
//    if the returned error is not `nil` (which is Go's equivalent of Python's `None` or C's `NULL`).
//
// Explicit Implementation Steps:
// 1. Create a byte slice buffer of size 1: `buf := make([]byte, 1)`
// 2. Call `os.Stdin.Read(buf)` to read from standard input. This returns two values:
//    - `n`: the number of bytes successfully read.
//    - `err`: any error encountered.
// 3. Check if `err` is not nil. If there is an error, return `0` and that `err`.
// 4. Check if `n == 0` (no bytes read). If so, return `0` and `io.EOF` (End Of File).
// 5. If successful (n == 1), return the read byte (which is at index `buf[0]`) and `nil` for the error.
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
//
// ELI5 (Explain Like I'm 5):
// By default, terminals operate in "cooked" mode: they wait for you to press Enter before sending
// any text to the program, and they automatically display what you type on the screen.
// To write a text editor, we want "raw" mode: we want to get every key press immediately,
// and we want to control exactly what gets shown on the screen (no automatic echoing of keys).
//
// Go vs Python vs C:
// 1. In C, you modify a `struct termios` using `tcgetattr` and `tcsetattr`.
// 2. In Python, you use the `termios` module, modifying lists of configuration values.
// 3. In Go, we use low-level Unix system calls via `syscall.Syscall` to talk to the kernel.
//    Because Go is a type-safe language, we must use the `unsafe` package. System calls expect
//    raw memory addresses (pointers). Go's standard compiler restricts arbitrary pointer arithmetic,
//    so we cast a pointer using `unsafe.Pointer` to tell Go: "Trust me, pass this raw memory address
//    directly to the operating system." This is exactly like C's `(void*)&termios`.
//
// Explicit Implementation Steps:
// 1. Get the file descriptor for standard input (stdin): `fd := int(os.Stdin.Fd())`
// 2. Query the current terminal state using `ioctl` with the request `syscall.TCGETS` (Terminal Get State).
//    Store this state in the global variable `origTermios`.
//    Use: `_, _, err := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), uintptr(syscall.TCGETS), uintptr(unsafe.Pointer(&origTermios)))`
//    If `err` is not 0 (meaning a syscall error occurred), return `err`.
//    Note: Syscall returns `(r1, r2, err)`. Since we only care about `err`, we check `if err != 0`.
// 3. Make a copy of `origTermios` to modify: `raw := origTermios`
// 4. Modify the flags in `raw` to disable terminal behaviors:
//    - Input Flags (`Iflag`): Disable `syscall.BRKINT`, `syscall.ICRNL`, `syscall.INPCK`, `syscall.ISTRIP`, and `syscall.IXON`.
//      In Go, we use the bitwise AND-NOT operator `&^`. E.g.:
//      `raw.Iflag = raw.Iflag &^ (syscall.BRKINT | syscall.ICRNL | syscall.INPCK | syscall.ISTRIP | syscall.IXON)`
//    - Output Flags (`Oflag`): Disable post-processing `syscall.OPOST`:
//      `raw.Oflag = raw.Oflag &^ syscall.OPOST`
//    - Control Flags (`Cflag`): Set character size to 8 bits by using bitwise OR:
//      `raw.Cflag = raw.Cflag | syscall.CS8`
//    - Local Flags (`Lflag`): Disable echoing `syscall.ECHO`, canonical mode `syscall.ICANON`, extended input `syscall.IEXTEN`, and signals `syscall.ISIG`:
//      `raw.Lflag = raw.Lflag &^ (syscall.ECHO | syscall.ICANON | syscall.IEXTEN | syscall.ISIG)`
// 5. Set read timeout parameters:
//    - Minimum number of bytes to read before returning: `raw.Cc[syscall.VMIN] = 0`
//    - Timeout in deciseconds (1 decisecond = 100ms) to wait before returning: `raw.Cc[syscall.VTIME] = 1`
// 6. Apply the new terminal state using `ioctl` with the request `syscall.TCSETS` (Terminal Set State).
//    Use: `_, _, err = syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), uintptr(syscall.TCSETS), uintptr(unsafe.Pointer(&raw)))`
//    If `err != 0`, return `err`. Otherwise, return `nil`.
func enableRawMode() error {
	//panic("TODO: implement enableRawMode")
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
  raw.Cc[syscall.VMIN] = 1 // Wait for at least 1 byte of input before returning from read.
  raw.Cc[syscall.VTIME] = 0 // No timeout; wait indefinitely for input.

  _, _, err = syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), uintptr(syscall.TCSETS), uintptr(unsafe.Pointer(&raw)))
  if err != 0 {
    return err
  }
  return nil  

}

// disableRawMode restores the terminal back to its original "cooked" state.
//
// ELI5:
// If our program exits and leaves the terminal in raw mode, your terminal session will feel
// broken (it won't display what you type, and Enter won't work normally). We must restore the original
// settings before the program ends.
//
// Explicit Implementation Steps:
// 1. Get the stdin file descriptor: `fd := int(os.Stdin.Fd())`
// 2. Call `ioctl` with request `syscall.TCSETS` and pass the address of `origTermios`:
//    `_, _, err := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), uintptr(syscall.TCSETS), uintptr(unsafe.Pointer(&origTermios)))`
// 3. If `err != 0`, return `err`. Otherwise, return `nil`.
func disableRawMode() error {
	// panic("TODO: implement disableRawMode")
  fd := int(os.Stdin.Fd())
  _, _, err := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), uintptr(syscall.TCSETS), uintptr(unsafe.Pointer(&origTermios)))
  if err != 0 {
    return err
  }
  return nil
}

func main() {
	// ELI5:
	// To run a terminal app, we enable raw mode at startup, defer restoring the original terminal state
	// so it cleans up when the program exits, and run our input processing loop.
	//
	// Go vs Python / Go vs C:
	// - C uses `atexit(disableRawMode)` to clean up.
	// - Python uses `try...finally`.
	// - Go uses `defer`. When you prefix a function call with `defer`, Go schedules it to run
	//   automatically right before the surrounding function (`main`) returns. This works even if
	//   the program crashes (panics) or exits early due to an error, making it incredibly robust.
	//
	// Explicit Implementation Steps:
	// 1. Call `enableRawMode()`. If it returns an error, print it using `fmt.Println` and exit (return).
	// 2. Schedule cleanup using `defer`: `defer disableRawMode()`
	// 3. Reuse your loop from Step 1.1 to read keys and print them until 'q' is pressed.
	// panic("TODO: implement main")
  err := enableRawMode()
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
    if c == 'q' {
      break
    }
    fmt.Printf("You pressed: %c (byte value: %d)\n", c, c)
  }
}
