//go:build windows && !console

package main

import ui "cwgrpguploader/internal/windows"

const consoleBuild = false

func defaultSaveFolder() (string, error) { return ui.DefaultSaveFolder() }
