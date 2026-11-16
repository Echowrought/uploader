//go:build linux || (windows && console)

package main

import "errors"

const consoleBuild = true

func defaultSaveFolder() (string, error) {
	return "", errors.New("save folder is not configured; use --save-folder PATH with an absolute CWG save folder, or set save_folder in config.json")
}
