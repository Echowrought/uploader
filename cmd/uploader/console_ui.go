package main

import (
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
	"time"
	"unicode"

	"golang.org/x/text/width"
)

type consoleUI struct {
	out         io.Writer
	interactive bool
	color       bool
	columns     int
}

func openConsoleUI(out *os.File, nonInteractive bool) (consoleUI, func()) {
	ui := consoleUI{out: out}
	if nonInteractive || os.Getenv("TERM") == "dumb" {
		return ui, func() {}
	}
	columns, restore := enableConsoleTerminal(out)
	ui.columns = columns
	ui.interactive = columns > 0
	ui.color = ui.interactive && os.Getenv("NO_COLOR") == ""
	return ui, restore
}

type consoleRow struct{ label, value string }

const consoleHeading = "\x1b[1;36m"
const consoleBorder = "\x1b[36m"
const consolePath = "\x1b[92m"

func (ui consoleUI) showPaths(paths ...consoleRow) error {
	var text string
	if ui.interactive {
		text = ui.panel("CWG RPG Uploader", paths)
	} else {
		var plain strings.Builder
		for _, path := range paths {
			fmt.Fprintf(&plain, "%s: %s\n", path.label, path.value)
		}
		text = plain.String()
	}
	_, err := io.WriteString(ui.out, text)
	return err
}

func (ui consoleUI) showApproval(link string) (stop func(), err error) {
	prompt := ui.panel("Connect your Battle.net account", []consoleRow{
		{value: "Open the link below and approve permission to upload saves."},
		{value: "Press Ctrl+C to cancel."},
	})
	// The URL stays outside the border so it remains uninterrupted and copyable.
	if _, err := fmt.Fprintf(ui.out, "\n%s\n%s\n\n", prompt, ui.paint("\x1b[94m", link)); err != nil {
		return nil, err
	}
	return ui.startAuthenticationSpinner()
}

func (ui consoleUI) panel(title string, rows []consoleRow) string {
	var text strings.Builder
	formatted := func(row consoleRow) string {
		if row.label == "" {
			return row.value
		}
		return ui.paint(consoleHeading, row.label+":") + " " + ui.paint(consolePath, row.value)
	}
	length := func(row consoleRow) int {
		if row.label == "" {
			return consoleCells(row.value)
		}
		return consoleCells(row.label) + 2 + consoleCells(row.value)
	}
	heading := ui.paint(consoleHeading, title)
	titleWidth := consoleCells(title)
	bodyWidth := titleWidth
	for _, row := range rows {
		bodyWidth = max(bodyWidth, length(row))
	}
	border := func(size int) string { return ui.paint(consoleBorder, "+"+strings.Repeat("-", size+2)+"+") }
	line := func(content string, padding int) {
		fmt.Fprintf(&text, "%s %s%s %s\n", ui.paint(consoleBorder, "|"), content, strings.Repeat(" ", padding), ui.paint(consoleBorder, "|"))
	}
	if ui.columns == 0 || bodyWidth+4 < ui.columns {
		fmt.Fprintln(&text, border(bodyWidth))
		line(heading, bodyWidth-titleWidth)
		for _, row := range rows {
			line(formatted(row), bodyWidth-length(row))
		}
		fmt.Fprintln(&text, border(bodyWidth))
		return text.String()
	}
	// A single long path should not make the frame wrap. Keep every path intact.
	if titleWidth+4 < ui.columns {
		fmt.Fprintln(&text, border(titleWidth))
		line(heading, 0)
		fmt.Fprintln(&text, border(titleWidth))
	} else {
		fmt.Fprintln(&text, heading)
	}
	for _, row := range rows {
		fmt.Fprintln(&text, formatted(row))
	}
	return text.String()
}

func (ui consoleUI) paint(color, text string) string {
	if !ui.color {
		return text
	}
	return color + text + "\x1b[0m"
}

func consoleCells(text string) int {
	cells := 0
	for _, r := range text {
		if unicode.Is(unicode.Mn, r) || unicode.Is(unicode.Me, r) || unicode.Is(unicode.Cf, r) {
			continue
		}
		cells++
		kind := width.LookupRune(r).Kind()
		if kind == width.EastAsianWide || kind == width.EastAsianFullwidth {
			cells++
		}
	}
	return cells
}

func (ui consoleUI) startAuthenticationSpinner() (func(), error) {
	const status = "Awaiting authentication..."
	if !ui.interactive {
		_, err := fmt.Fprintln(ui.out, status)
		return func() {}, err
	}
	frames := [...]string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
	if _, err := fmt.Fprintf(ui.out, "%s %s", frames[0], status); err != nil {
		return nil, err
	}
	stop, done := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		frame := 1
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				if _, err := fmt.Fprintf(ui.out, "\r%s %s", frames[frame], status); err != nil {
					return
				}
				frame = (frame + 1) % len(frames)
			}
		}
	}()
	return func() {
		close(stop)
		<-done
		fmt.Fprint(ui.out, "\r\x1b[2K")
	}, nil
}

func (ui consoleUI) logWriter() io.Writer {
	if !ui.color {
		return ui.out
	}
	return coloredConsoleLog{out: ui.out}
}

var consoleLogPrefix = regexp.MustCompile(`^([0-9]{4}/[0-9]{2}/[0-9]{2} [0-9]{2}:[0-9]{2}:[0-9]{2})( (\[[A-Za-z_-]+\]))?`)

type coloredConsoleLog struct{ out io.Writer }

func (w coloredConsoleLog) Write(data []byte) (int, error) {
	match := consoleLogPrefix.FindSubmatchIndex(data)
	if match == nil {
		return w.out.Write(data)
	}
	const timestampColor, loggerColor, reset = "\x1b[90m", "\x1b[36m", "\x1b[0m"
	var line strings.Builder
	line.WriteString(timestampColor)
	line.Write(data[:match[3]])
	line.WriteString(reset)
	end := match[3]
	if match[6] >= 0 {
		line.Write(data[end:match[6]])
		line.WriteString(loggerColor)
		line.Write(data[match[6]:match[7]])
		line.WriteString(reset)
		end = match[7]
	}
	line.Write(data[end:])
	n, err := io.WriteString(w.out, line.String())
	if err == nil && n != line.Len() {
		err = io.ErrShortWrite
	}
	if err != nil {
		return 0, err
	}
	return len(data), nil
}
