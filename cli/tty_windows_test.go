//go:build windows

package cli

import (
	"strings"
	"syscall"
	"testing"
	"unsafe"
)

// ═══════════════════════════════════════════════════
// Регрессия: LF (\n) должен возвращать каретку в колонку 0.
//
// Раньше enableVT() выставлял DISABLE_NEWLINE_AUTO_RETURN, и каждая строка
// начиналась с колонки, на которой закончилась предыдущая — в интерактиве
// help/логи печатались «лесенкой» с нарастающими отступами.
// ═══════════════════════════════════════════════════

var (
	tk32            = syscall.NewLazyDLL("kernel32.dll")
	procAllocCon    = tk32.NewProc("AllocConsole")
	procGetStd      = tk32.NewProc("GetStdHandle")
	procSetStd      = tk32.NewProc("SetStdHandle")
	procCreateFile  = tk32.NewProc("CreateFileW")
	procCloseHandle = tk32.NewProc("CloseHandle")
	procSetCursor   = tk32.NewProc("SetConsoleCursorPosition")
	procFillChars   = tk32.NewProc("FillConsoleOutputCharacterW")
	procWriteConW   = tk32.NewProc("WriteConsoleW")
	procReadConOutW = tk32.NewProc("ReadConsoleOutputCharacterW")
	procGetSBI      = tk32.NewProc("GetConsoleScreenBufferInfo")
)

const (
	stdOutputHandle = ^uintptr(10) // -11

	genericReadWrite = 0xC0000000
	fileShareRW      = 0x3
	openExisting     = 3
	invalidHandle    = ^uintptr(0)
)

type winCoord struct{ X, Y int16 }

type winSmallRect struct{ L, T, R, B int16 }

type winSBI struct {
	size   winCoord
	cursor winCoord
	attrs  uint16
	window winSmallRect
	max    winCoord
}

// openConsoleOut возвращает хэндл вывода текущей консоли процесса,
// создавая консоль, если её нет.
func openConsoleOut() (uintptr, error) {
	name, _ := syscall.UTF16PtrFromString("CONOUT$")
	h, _, err := procCreateFile.Call(
		uintptr(unsafe.Pointer(name)),
		genericReadWrite, fileShareRW, 0,
		openExisting, 0, 0,
	)
	if h != invalidHandle && h != 0 {
		return h, nil
	}
	if r, _, _ := procAllocCon.Call(); r == 0 {
		return 0, err
	}
	h, _, err = procCreateFile.Call(
		uintptr(unsafe.Pointer(name)),
		genericReadWrite, fileShareRW, 0,
		openExisting, 0, 0,
	)
	if h == invalidHandle || h == 0 {
		return 0, err
	}
	return h, nil
}

// coordVal упаковывает COORD (4 байта: X, Y) в слово для передачи по значению.
func coordVal(c winCoord) uintptr {
	return uintptr(uint32(uint16(c.X)) | uint32(uint16(c.Y))<<16)
}

func TestLFReturnsCarriage(t *testing.T) {
	conout, err := openConsoleOut()
	if err != nil || conout == 0 {
		t.Skipf("нет консоли: %v", err)
	}
	defer procCloseHandle.Call(conout)

	// enableVT читает STD_OUTPUT_HANDLE — если stdout перенаправлен в пайп,
	// подменяем хэндл на консоль, иначе режим не включится.
	prevOut, _, _ := procGetStd.Call(stdOutputHandle)
	procSetStd.Call(stdOutputHandle, conout)
	defer procSetStd.Call(stdOutputHandle, prevOut)

	enableVT() // ровно то, что делает newPrinter при старте в TTY

	var sbi winSBI
	retSBI, _, _ := procGetSBI.Call(conout, uintptr(unsafe.Pointer(&sbi)))
	t.Logf("GetSBI=%v size=%dx%d cursor=%d,%d", retSBI != 0, sbi.size.X, sbi.size.Y, sbi.cursor.X, sbi.cursor.Y)

	// Очищаем буфер и ставим курсор в начало, чтобы записи читались с (0,0).
	var filled uint32
	retFill, _, errFill := procFillChars.Call(
		conout,
		uintptr(' '),
		uintptr(sbi.size.X)*uintptr(sbi.size.Y),
		coordVal(winCoord{0, 0}),
		uintptr(unsafe.Pointer(&filled)),
	)
	t.Logf("Fill=%v filled=%d err=%v", retFill != 0, filled, errFill)
	retCur, _, errCur := procSetCursor.Call(conout, coordVal(winCoord{0, 0}))
	t.Logf("SetCursor=%v err=%v", retCur != 0, errCur)

	// Печатаем ровно так же, как interactiveHelp: строки через \n.
	text, _ := syscall.UTF16FromString("Commands:\n  connect x\n  disconnect y\n")
	var written uint32
	retW, _, errW := procWriteConW.Call(
		conout,
		uintptr(unsafe.Pointer(&text[0])),
		uintptr(len(text)-1),
		uintptr(unsafe.Pointer(&written)),
		0,
	)
	t.Logf("Write=%v written=%d err=%v", retW != 0, written, errW)

	readLine := func(y int16) string {
		buf := make([]uint16, sbi.size.X+1)
		var n uint32
		retR, _, errR := procReadConOutW.Call(
			conout,
			uintptr(unsafe.Pointer(&buf[0])),
			uintptr(len(buf)),
			coordVal(winCoord{0, y}),
			uintptr(unsafe.Pointer(&n)),
		)
		t.Logf("Read(y=%d)=%v n=%d err=%v", y, retR != 0, n, errR)
		out := make([]rune, 0, n)
		for _, r := range buf[:n] {
			out = append(out, rune(r))
		}
		return strings.TrimRight(string(out), "\x00 ")
	}

	line0 := readLine(0)
	line1 := readLine(1)

	if !strings.HasPrefix(line0, "Commands:") {
		t.Fatalf("строка 0: ожидалась %q, получена %q", "Commands:", line0)
	}

	// Ключевая проверка: "  connect" должна начинаться с колонки 2,
	// а не с колонки len("Commands:")+2 — как было с DISABLE_NEWLINE_AUTO_RETURN.
	idx := strings.Index(line1, "connect")
	if idx != 2 {
		t.Errorf("LF не вернул каретку: \"connect\" начался с колонки %d (ожидалось 2)\n"+
			"строка 0: %q\nстрока 1: %q", idx, line0, line1)
	}

	// И третья строка — тоже с колонки 2.
	idx2 := strings.Index(readLine(2), "disconnect")
	if idx2 != 2 {
		t.Errorf("LF не вернул каретку: \"disconnect\" начался с колонки %d (ожидалось 2)", idx2)
	}
}
