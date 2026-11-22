package windows

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"sync/atomic"
	"unsafe"

	"cwgrpguploader/internal/leaderboard"
	"golang.org/x/sys/windows"
)

const (
	dialogCancel        = 2 // IDCANCEL
	dialogOpen          = 100
	dialogCopy          = 101
	dialogAllowCancel   = 0x0008       // TDF_ALLOW_DIALOG_CANCELLATION
	dialogCallbackTimer = 0x0800       // TDF_CALLBACK_TIMER
	dialogCancelButton  = 0x0008       // TDCBF_CANCEL_BUTTON
	dialogButtonClicked = 2            // TDN_BUTTON_CLICKED
	dialogTimer         = 4            // TDN_TIMER
	dialogClickButton   = 0x0400 + 102 // TDM_CLICK_BUTTON
	dialogSetText       = 0x0400 + 108 // TDM_SET_ELEMENT_TEXT
	dialogFooter        = 2            // TDE_FOOTER
)

var (
	approvalUser32     = windows.NewLazySystemDLL("user32.dll")
	approvalKernel32   = windows.NewLazySystemDLL("kernel32.dll")
	taskDialogIndirect = windows.NewLazySystemDLL("comctl32.dll").NewProc("TaskDialogIndirect")
	postDialogMessage  = approvalUser32.NewProc("PostMessageW")
	sendDialogMessage  = approvalUser32.NewProc("SendMessageW")
	approvalCallback   = windows.NewCallback(handleApprovalDialog)
	activeApproval     atomic.Pointer[approvalDialog]
)

type approvalDialog struct {
	ctx     context.Context
	link    string
	closing bool // Accessed only on the dialog thread.
}

func presentApproval(ctx context.Context, link string) error {
	if ctx.Err() != nil {
		return leaderboard.ErrAuthCancelled
	}
	state := &approvalDialog{ctx: ctx, link: link}
	if !activeApproval.CompareAndSwap(nil, state) {
		return errors.New("authentication window already open")
	}
	defer activeApproval.Store(nil)

	// TaskDialogIndirect owns the modal loop, including timer callbacks, on this thread.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if err := taskDialogIndirect.Find(); err != nil {
		return fmt.Errorf("open native authentication window: %w", err)
	}

	const introduction = "Connect your Battle.net account so the uploader can back up your CWG saves. Sign in through the Leaderboard and approve permission to upload saves."
	content := introduction + "\r\n\r\n" + strings.Replace(link, "?", "?\r\n", 1)
	title, _ := windows.UTF16PtrFromString("CWG RPG Uploader")
	instruction, _ := windows.UTF16PtrFromString("Connect your Battle.net account")
	body, err := windows.UTF16PtrFromString(content)
	if err != nil {
		return errors.New("invalid authentication link")
	}
	footer, _ := windows.UTF16PtrFromString("Open the link or copy it into your browser. Cancel stops the attempt.")
	open, _ := windows.UTF16PtrFromString("Open in default browser")
	copyLabel, _ := windows.UTF16PtrFromString("Copy link")
	var pinned runtime.Pinner
	defer pinned.Unpin()
	for _, text := range []*uint16{title, instruction, body, footer, open, copyLabel} {
		pinned.Pin(text)
	}
	buttons := [...]taskDialogButton{
		{id: dialogOpen, text: packPointer(uintptr(unsafe.Pointer(open)))},
		{id: dialogCopy, text: packPointer(uintptr(unsafe.Pointer(copyLabel)))},
	}
	// The native dialog can invoke Go callbacks while these packed addresses are in use.
	pinned.Pin(&buttons)
	config := taskDialogConfig{
		size: uint32(unsafe.Sizeof(taskDialogConfig{})), flags: dialogAllowCancel | dialogCallbackTimer,
		commonButtons: dialogCancelButton, title: packPointer(uintptr(unsafe.Pointer(title))),
		instruction: packPointer(uintptr(unsafe.Pointer(instruction))), content: packPointer(uintptr(unsafe.Pointer(body))),
		buttonCount: uint32(len(buttons)), buttons: packPointer(uintptr(unsafe.Pointer(&buttons[0]))), defaultButton: dialogOpen,
		footer: packPointer(uintptr(unsafe.Pointer(footer))), callback: packPointer(approvalCallback), width: 350,
	}
	var choice int32
	result, _, _ := taskDialogIndirect.Call(uintptr(unsafe.Pointer(&config)), uintptr(unsafe.Pointer(&choice)), 0, 0)
	if ctx.Err() != nil {
		return leaderboard.ErrAuthCancelled
	}
	if int32(result) < 0 {
		return fmt.Errorf("open native authentication window (HRESULT 0x%08X)", uint32(result))
	}
	if choice == dialogOpen {
		if err := Open(link); err != nil {
			return errors.New("could not open the default browser")
		}
		return nil
	}
	return leaderboard.ErrAuthCancelled
}

func handleApprovalDialog(window, notification, button, _, _ uintptr) uintptr {
	state := activeApproval.Load()
	if state == nil {
		return 0
	}
	switch notification {
	case dialogTimer:
		if !state.closing && state.ctx.Err() != nil {
			posted, _, _ := postDialogMessage.Call(window, dialogClickButton, dialogCancel, 0)
			state.closing = posted != 0
		}
	case dialogButtonClicked:
		if button == dialogCopy {
			message := "Link copied. Paste it into your browser and approve upload access. Waiting for approval..."
			if !copyApprovalLink(window, state.link) {
				message = "Could not copy the link. Try again or choose Open in default browser."
			}
			text, _ := windows.UTF16PtrFromString(message)
			sendDialogMessage.Call(window, dialogSetText, dialogFooter, uintptr(unsafe.Pointer(text)))
			return 1 // S_FALSE leaves the dialog and Copy link button open.
		}
	}
	return 0
}

func copyApprovalLink(window uintptr, link string) bool {
	text, err := windows.UTF16FromString(link)
	if err != nil {
		return false
	}
	const moveable = 2 // GMEM_MOVEABLE
	bytes := uintptr(len(text)) * unsafe.Sizeof(text[0])
	memory, _, _ := approvalKernel32.NewProc("GlobalAlloc").Call(moveable, bytes)
	if memory == 0 {
		return false
	}
	defer func() {
		if memory != 0 {
			approvalKernel32.NewProc("GlobalFree").Call(memory)
		}
	}()
	address, _, _ := approvalKernel32.NewProc("GlobalLock").Call(memory)
	if address == 0 {
		return false
	}
	approvalKernel32.NewProc("RtlMoveMemory").Call(address, uintptr(unsafe.Pointer(&text[0])), bytes)
	approvalKernel32.NewProc("GlobalUnlock").Call(memory)
	opened, _, _ := approvalUser32.NewProc("OpenClipboard").Call(window)
	if opened == 0 {
		return false
	}
	defer approvalUser32.NewProc("CloseClipboard").Call()
	emptied, _, _ := approvalUser32.NewProc("EmptyClipboard").Call()
	if emptied == 0 {
		return false
	}
	const unicodeText = 13 // CF_UNICODETEXT
	set, _, _ := approvalUser32.NewProc("SetClipboardData").Call(unicodeText, memory)
	if set == 0 {
		return false
	}
	memory = 0 // The clipboard owns the allocation now.
	return true
}

// The Windows SDK packs TASKDIALOGCONFIG and TASKDIALOG_BUTTON at byte alignment.
// Every scalar is four bytes and every pointer slot starts on a four-byte boundary.
// Pointer-sized byte arrays prevent Go from adding eight-byte alignment padding.
type taskDialogConfig struct {
	size                                                      uint32
	parent, instance                                          packedPointer
	flags, commonButtons                                      uint32
	title, mainIcon, instruction, content                     packedPointer
	buttonCount                                               uint32
	buttons                                                   packedPointer
	defaultButton, radioCount                                 uint32
	radioButtons                                              packedPointer
	defaultRadio                                              uint32
	verification, expanded, expandedControl, collapsedControl packedPointer
	footerIcon, footer, callback, callbackData                packedPointer
	width                                                     uint32
}

type taskDialogButton struct {
	id   int32
	text packedPointer
}

type packedPointer [unsafe.Sizeof(uintptr(0))]byte

func packPointer(value uintptr) (packed packedPointer) {
	for i := range packed {
		packed[i] = byte(value >> (8 * i))
	}
	return packed
}
