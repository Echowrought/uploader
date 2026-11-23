package windows

import (
	"context"
	"errors"
	"fmt"
	"html"
	"runtime"
	"strings"
	"sync/atomic"
	"time"
	"unsafe"

	"cwgrpguploader/internal/leaderboard"
	"golang.org/x/sys/windows"
)

const (
	dialogCancel        = 2 // IDCANCEL
	dialogOpen          = 100
	dialogCopy          = 101
	dialogStart         = 102
	dialogUseMainIcon   = 0x0002       // TDF_USE_HICON_MAIN
	dialogHyperlinks    = 0x0001       // TDF_ENABLE_HYPERLINKS
	dialogAllowCancel   = 0x0008       // TDF_ALLOW_DIALOG_CANCELLATION
	dialogCallbackTimer = 0x0800       // TDF_CALLBACK_TIMER
	dialogCancelButton  = 0x0008       // TDCBF_CANCEL_BUTTON
	dialogCreated       = 0            // TDN_CREATED
	dialogNavigated     = 1            // TDN_NAVIGATED
	dialogButtonClicked = 2            // TDN_BUTTON_CLICKED
	dialogLinkClicked   = 3            // TDN_HYPERLINK_CLICKED
	dialogTimer         = 4            // TDN_TIMER
	dialogNavigate      = 0x0400 + 101 // TDM_NAVIGATE_PAGE
	dialogClickButton   = 0x0400 + 102 // TDM_CLICK_BUTTON
	dialogSetText       = 0x0400 + 108 // TDM_SET_ELEMENT_TEXT
	dialogUpdateIcon    = 0x0400 + 116 // TDM_UPDATE_ICON
	dialogFooter        = 2            // TDE_FOOTER
	dialogMainIcon      = 0            // TDIE_ICON_MAIN
	dialogSetWindowIcon = 0x0080       // WM_SETICON
	approvalStartDelay  = 10 * time.Second
)

var (
	approvalUser32        = windows.NewLazySystemDLL("user32.dll")
	approvalKernel32      = windows.NewLazySystemDLL("kernel32.dll")
	approvalComctl32      = windows.NewLazySystemDLL("comctl32.dll")
	taskDialogIndirect    = approvalComctl32.NewProc("TaskDialogIndirect")
	postDialogMessage     = approvalUser32.NewProc("PostMessageW")
	sendDialogMessage     = approvalUser32.NewProc("SendMessageW")
	approvalCallback      = windows.NewCallback(handleApprovalDialog)
	activeApproval        atomic.Pointer[approvalDialog]
	approvalTitleSubclass uintptr
)

func init() {
	approvalTitleSubclass = windows.NewCallback(keepApprovalTitleIcon)
}

type approvalDialog struct {
	ctx          context.Context
	link         string
	approved     <-chan struct{}
	successPage  *taskDialogConfig
	titleIcon    uintptr
	spinnerIcons [spinnerFrameCount]uintptr
	// These fields are only accessed on the dialog thread.
	closing     bool
	spinner     int
	startAt     time.Time
	seconds     int
	startButton uintptr
	failure     error
}

func presentApproval(ctx context.Context, link string, approved <-chan struct{}) error {
	if ctx.Err() != nil {
		return leaderboard.ErrAuthCancelled
	}
	state := &approvalDialog{ctx: ctx, link: link, approved: approved}
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

	title, _ := windows.UTF16PtrFromString("CWG RPG Uploader")
	instruction, _ := windows.UTF16PtrFromString("Waiting for Battle.net approval")
	body, err := windows.UTF16PtrFromString(approvalContent(link))
	if err != nil {
		return errors.New("invalid authentication link")
	}
	footer, _ := windows.UTF16PtrFromString("Waiting for browser approval. Open the highlighted link or copy it into your browser. Cancel stops the attempt.")
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
	destroyIcon := approvalUser32.NewProc("DestroyIcon")
	state.titleIcon, err = createApprovalTitleIcon()
	if err != nil {
		return err
	}
	defer destroyIcon.Call(state.titleIcon)
	for frame := range state.spinnerIcons {
		icon, err := createApprovalImageIcon(approvalSpinnerImage(frame))
		if err != nil {
			return err
		}
		state.spinnerIcons[frame] = icon
		defer destroyIcon.Call(icon)
	}
	checkIcon, err := createApprovalImageIcon(approvalCheckImage())
	if err != nil {
		return err
	}
	defer destroyIcon.Call(checkIcon)
	state.successPage = approvalSuccessPage(&pinned, title, checkIcon)
	config := taskDialogConfig{
		size: uint32(unsafe.Sizeof(taskDialogConfig{})), flags: dialogHyperlinks | dialogUseMainIcon | dialogAllowCancel | dialogCallbackTimer,
		commonButtons: dialogCancelButton, title: packPointer(uintptr(unsafe.Pointer(title))),
		mainIcon:    packPointer(state.spinnerIcons[0]),
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
	if state.failure != nil {
		return state.failure
	}
	if choice == dialogStart && !state.startAt.IsZero() {
		return nil
	}
	return leaderboard.ErrAuthCancelled
}

func approvalSuccessPage(pinned *runtime.Pinner, title *uint16, checkIcon uintptr) *taskDialogConfig {
	instruction, _ := windows.UTF16PtrFromString("Authentication approved")
	content, _ := windows.UTF16PtrFromString("Your Battle.net account is ready. The uploader will start automatically.")
	start, _ := windows.UTF16PtrFromString(startButtonText(int(approvalStartDelay / time.Second)))
	for _, text := range []*uint16{instruction, content, start} {
		pinned.Pin(text)
	}
	button := &taskDialogButton{id: dialogStart, text: packPointer(uintptr(unsafe.Pointer(start)))}
	pinned.Pin(button)
	page := &taskDialogConfig{
		size: uint32(unsafe.Sizeof(taskDialogConfig{})), flags: dialogUseMainIcon | dialogCallbackTimer,
		title: packPointer(uintptr(unsafe.Pointer(title))), mainIcon: packPointer(checkIcon),
		instruction: packPointer(uintptr(unsafe.Pointer(instruction))), content: packPointer(uintptr(unsafe.Pointer(content))),
		buttonCount: 1, buttons: packPointer(uintptr(unsafe.Pointer(button))), defaultButton: dialogStart,
		callback: packPointer(approvalCallback), width: 350,
	}
	pinned.Pin(page)
	return page
}

func approvalContent(link string) string {
	const introduction = "Connect your Battle.net account so the uploader can back up your CWG saves. Sign in through the Leaderboard and approve permission to upload saves."
	label := html.EscapeString(strings.Replace(link, "?", "?\r\n", 1))
	// Use a fixed hyperlink target. The click handler only opens the validated approval URL.
	return introduction + "\r\n\r\n" + `<a href="approval">` + label + `</a>`
}

func handleApprovalDialog(window, notification, button, _, _ uintptr) uintptr {
	state := activeApproval.Load()
	if state == nil {
		return 0
	}
	switch notification {
	case dialogCreated, dialogNavigated:
		setApprovalTitleIcon(window, state.titleIcon, notification == dialogNavigated)
	case dialogTimer:
		state.tick(window, time.Now())
	case dialogLinkClicked:
		openApprovalInBrowser(window, state)
	case dialogButtonClicked:
		switch button {
		case dialogOpen:
			openApprovalInBrowser(window, state)
			return 1 // S_FALSE keeps the dialog open while the browser is in use.
		case dialogCopy:
			message := "Link copied. Paste it into your browser and approve upload access. Waiting for approval..."
			if !copyApprovalLink(window, state.link) {
				message = "Could not copy the link. Try again or choose Open in default browser."
			}
			setApprovalFooter(window, message)
			return 1 // S_FALSE leaves the dialog and Copy link button open.
		}
	}
	return 0
}

func (state *approvalDialog) tick(window uintptr, now time.Time) {
	if state.closing {
		return
	}
	if state.ctx.Err() != nil || state.failure != nil {
		button := uintptr(dialogCancel)
		if !state.startAt.IsZero() {
			button = dialogStart
		}
		state.closeWithButton(window, button)
		return
	}
	if !state.startAt.IsZero() {
		remaining := state.startAt.Sub(now)
		if remaining <= 0 {
			state.closeWithButton(window, dialogStart)
			return
		}
		seconds := int((remaining + time.Second - 1) / time.Second)
		if seconds != state.seconds {
			if state.startButton == 0 {
				state.startButton = findApprovalButton(window)
			}
			if state.startButton != 0 {
				text, _ := windows.UTF16PtrFromString(startButtonText(seconds))
				updated, _, _ := approvalUser32.NewProc("SetWindowTextW").Call(state.startButton, uintptr(unsafe.Pointer(text)))
				if updated != 0 {
					state.seconds = seconds
				}
			}
		}
		return
	}
	select {
	case <-state.approved:
		status, _, _ := sendDialogMessage.Call(window, dialogNavigate, 0, uintptr(unsafe.Pointer(state.successPage)))
		if int32(status) < 0 {
			state.failure = errors.New("could not display authentication confirmation")
			state.closeWithButton(window, dialogCancel)
			return
		}
		state.startAt, state.seconds = now.Add(approvalStartDelay), int(approvalStartDelay/time.Second)
	default:
		state.spinner = (state.spinner + 1) % len(state.spinnerIcons)
		sendDialogMessage.Call(window, dialogUpdateIcon, dialogMainIcon, state.spinnerIcons[state.spinner])
	}
}

func startButtonText(seconds int) string { return fmt.Sprintf("Start uploader (%ds)", seconds) }

// Recent Windows versions nest TaskDialog buttons inside DirectUI windows
// and do not expose their TaskDialog button IDs as Win32 control IDs.
func findApprovalButton(parent uintptr) uintptr {
	class, _ := windows.UTF16PtrFromString("Button")
	label, _ := windows.UTF16PtrFromString(startButtonText(int(approvalStartDelay / time.Second)))
	return findDialogChild(parent, class, label)
}

func findDialogChild(parent uintptr, class, label *uint16) uintptr {
	find := approvalUser32.NewProc("FindWindowExW")
	button, _, _ := find.Call(parent, 0, uintptr(unsafe.Pointer(class)), uintptr(unsafe.Pointer(label)))
	if button != 0 {
		return button
	}
	for child, _, _ := find.Call(parent, 0, 0, 0); child != 0; child, _, _ = find.Call(parent, child, 0, 0) {
		if button := findDialogChild(child, class, label); button != 0 {
			return button
		}
	}
	return 0
}

func setApprovalTitleIcon(window, icon uintptr, successPage bool) {
	// TaskDialog also sends WM_SETICON when its *content* icon is changed.
	// Keep those updates from replacing the app's title-bar and taskbar icon.
	approvalComctl32.NewProc("SetWindowSubclass").Call(window, approvalTitleSubclass, 1, icon)
	const (
		styleIndex   = ^uintptr(15) // GWL_STYLE = -16
		exStyleIndex = ^uintptr(19) // GWL_EXSTYLE = -20
		systemMenu   = 0x80000      // WS_SYSMENU
		modalFrame   = 1            // WS_EX_DLGMODALFRAME
	)
	getStyle := approvalUser32.NewProc("GetWindowLongW")
	setStyle := approvalUser32.NewProc("SetWindowLongW")
	style, _, _ := getStyle.Call(window, styleIndex)
	exStyle, _, _ := getStyle.Call(window, exStyleIndex)
	if style&systemMenu == 0 {
		// TaskDialog removes WS_SYSMENU when a page has no Cancel button.
		// Windows needs it to paint the icon in the caption.
		setStyle.Call(window, styleIndex, style|systemMenu)
	}
	if exStyle&modalFrame != 0 {
		setStyle.Call(window, exStyleIndex, exStyle&^modalFrame)
	}
	if successPage {
		// Keep the caption icon without re-enabling Close on the success page.
		menu, _, _ := approvalUser32.NewProc("GetSystemMenu").Call(window, 0)
		if menu != 0 {
			const closeCommand, grayed = 0xF060, 1 // SC_CLOSE, MF_GRAYED | MF_BYCOMMAND
			approvalUser32.NewProc("EnableMenuItem").Call(menu, closeCommand, grayed)
		}
	}
	if style&systemMenu == 0 || exStyle&modalFrame != 0 {
		const refreshFrame = 0x20 | 0x10 | 0x4 | 0x2 | 0x1 // FRAMECHANGED, NOACTIVATE, NOZORDER, NOMOVE, NOSIZE
		approvalUser32.NewProc("SetWindowPos").Call(window, 0, 0, 0, 0, 0, refreshFrame)
	}
	sendDialogMessage.Call(window, dialogSetWindowIcon, 0, icon) // ICON_SMALL
	sendDialogMessage.Call(window, dialogSetWindowIcon, 1, icon) // ICON_BIG
}

func keepApprovalTitleIcon(window, message, wParam, lParam, id, icon uintptr) uintptr {
	const destroy = 0x0082 // WM_NCDESTROY
	if message == dialogSetWindowIcon && lParam != icon {
		return 0 // Do not return our icon handle to TaskDialog as its previous icon.
	}
	if message == destroy {
		approvalComctl32.NewProc("RemoveWindowSubclass").Call(window, approvalTitleSubclass, id)
	}
	result, _, _ := approvalComctl32.NewProc("DefSubclassProc").Call(window, message, wParam, lParam)
	return result
}

func (state *approvalDialog) closeWithButton(window, button uintptr) {
	posted, _, _ := postDialogMessage.Call(window, dialogClickButton, button, 0)
	state.closing = posted != 0
}

func openApprovalInBrowser(window uintptr, state *approvalDialog) {
	if state.ctx.Err() != nil {
		return
	}
	if err := Open(state.link); err != nil {
		setApprovalFooter(window, "Could not open the default browser. Copy the link to open it yourself.")
		return
	}
	setApprovalFooter(window, "Waiting for approval. Sign in through your browser and approve upload access.")
}

func setApprovalFooter(window uintptr, message string) {
	text, _ := windows.UTF16PtrFromString(message)
	sendDialogMessage.Call(window, dialogSetText, dialogFooter, uintptr(unsafe.Pointer(text)))
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
